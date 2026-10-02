package safety

import (
	"context"
	"fmt"
	"math"
	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/lock"
	"quantmesh/logger"
	"reflect"
	"strings"
	"sync"
	"time"
)

// IExchange 定义對账所需的交易所接口方法
type IExchange interface {
	GetPositions(ctx context.Context, symbol string) (interface{}, error)
	GetOpenOrders(ctx context.Context, symbol string) (interface{}, error)
	GetBaseAsset() string // 獲取基础资產（交易币种）
}

// SlotInfo 槽位信息（避免直接依赖 position 包的内部結構）
type SlotInfo struct {
	Price          float64
	PositionStatus string
	PositionQty    float64
	OrderID        int64
	OrderSide      string
	OrderStatus    string
	OrderCreatedAt time.Time
}

// IPositionManager 定义對账所需的倉位管理器接口方法
type IPositionManager interface {
	// BeginReconciliation freezes this manager's physical order submissions
	// and drains already-admitted submissions until the returned release runs.
	BeginReconciliation(ctx context.Context) (release func(), err error)
	// FailReconciliation holds physical submissions closed when position
	// evidence cannot be trusted.
	FailReconciliation(err error)
	// CompleteReconciliation clears only the dedicated unverified-position
	// gate source after a fully successful reconciliation.
	CompleteReconciliation()
	// 遍历所有槽位（封装 sync.Map.Range）
	// 注意：slot 為 interface{} 類型，需要轉换為 SlotInfo
	IterateSlots(fn func(price float64, slot interface{}) bool)
	// 獲取统计數據
	GetTotalBuyQty() float64
	GetTotalSellQty() float64
	GetReconcileCount() int64
	// 更新统计數據
	IncrementReconcileCount()
	UpdateLastReconcileTime(t time.Time)
	// 獲取配置信息
	GetSymbol() string
	GetPriceInterval() float64
	GetProfitSpread() float64

	// 强制同步持倉
	ForceSyncPositions(exchangePosition float64) error
}

// ReconciliationStorage 對账存儲介面（避免循環匯入，使用函數類型）
type ReconciliationStorage interface {
	SaveReconciliationHistory(symbol string, reconcileTime time.Time, localPosition, exchangePosition, positionDiff float64,
		activeBuyOrders, activeSellOrders int, pendingSellQty, totalBuyQty, totalSellQty, estimatedProfit float64) error
}

type exchangePositionSnapshot struct {
	netSize     float64
	longQty     float64
	shortQty    float64
	hasNet      bool
	hasLong     bool
	hasShort    bool
	directional bool
}

// Reconciler 持倉對账器
type Reconciler struct {
	cfg                        *config.Config
	exchange                   IExchange
	pm                         IPositionManager
	pauseChecker               func() bool
	storage                    ReconciliationStorage // 可選的存儲服務
	lock                       lock.DistributedLock  // 分布式鎖
	lastReconcileTime          time.Time             // 上次對账時间
	reconcileMu                sync.Mutex            // 對账互斥鎖
	minReconcileInterval       time.Duration         // 最小對账间隔（防止频繁調用）
	openOrderOwnershipVerifier func(*exchange.Order) bool
}

// NewReconciler 創建對账器
func NewReconciler(cfg *config.Config, exchange IExchange, pm IPositionManager, distributedLock lock.DistributedLock) *Reconciler {
	// 設置最小對账间隔，默认30秒（即使配置更短也要保证最小间隔）
	minInterval := 30 * time.Second
	reconcileInterval := time.Duration(cfg.Trading.ReconcileInterval) * time.Second
	if reconcileInterval > 0 && reconcileInterval < minInterval {
		minInterval = reconcileInterval
	}

	return &Reconciler{
		cfg:                  cfg,
		exchange:             exchange,
		pm:                   pm,
		lock:                 distributedLock,
		minReconcileInterval: minInterval,
	}
}

// SetStorage 設置存儲服務（可選）
func (r *Reconciler) SetStorage(storage ReconciliationStorage) {
	r.storage = storage
}

// SetPauseChecker 設置暂停检查函數（用於风控暂停）
func (r *Reconciler) SetPauseChecker(checker func() bool) {
	r.pauseChecker = checker
}

// SetOpenOrderOwnershipVerifier installs exact runtime-intent ownership
// verification for every active venue order found during reconciliation.
func (r *Reconciler) SetOpenOrderOwnershipVerifier(verifier func(*exchange.Order) bool) {
	r.openOrderOwnershipVerifier = verifier
}

// Start 啟动對账协程
func (r *Reconciler) Start(ctx context.Context) {
	go func() {
		interval := time.Duration(r.cfg.Trading.ReconcileInterval) * time.Second
		if interval <= 0 {
			interval = 30 * time.Second
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				logger.Info("⏹️ 持倉對账协程已停止")
				return
			case <-ticker.C:
				if err := r.ReconcileContext(ctx); err != nil {
					logger.Error("❌ [對账失败] %v", err)
				}
			}
		}
	}()
	logger.Info("✅ 持倉對账已啟动 (间隔: %d秒)", r.cfg.Trading.ReconcileInterval)
}

// Reconcile 執行對账（通用實現，支援所有交易所）
func (r *Reconciler) Reconcile() error {
	return r.ReconcileContext(context.Background())
}

// ReconcileContext executes one reconciliation using the caller's lifecycle
// context so shutdown/deadline cancellation reaches throttling and venue IO.
func (r *Reconciler) ReconcileContext(parent context.Context) error {
	if parent == nil {
		parent = context.Background()
	}
	failUnverified := func(err error) error {
		if parent.Err() == nil {
			r.pm.FailReconciliation(err)
		}
		return err
	}
	// 检查是否暂停（风控触发時不输出日志）
	if r.pauseChecker != nil && r.pauseChecker() {
		return nil
	}

	// 速率限制：确保最小對账间隔
	r.reconcileMu.Lock()
	elapsed := time.Since(r.lastReconcileTime)
	if elapsed < r.minReconcileInterval {
		waitTime := r.minReconcileInterval - elapsed
		r.reconcileMu.Unlock()
		logger.Debug("⏳ [對账] 等待 %v 后執行（最小间隔限制）", waitTime)
		timer := time.NewTimer(waitTime)
		defer timer.Stop()
		select {
		case <-parent.Done():
			return parent.Err()
		case <-timer.C:
		}
		r.reconcileMu.Lock()
	}
	r.lastReconcileTime = time.Now()
	r.reconcileMu.Unlock()

	symbol := r.pm.GetSymbol()
	exchangeName := "unknown"
	if r.exchange != nil {
		// 尝試獲取交易所名称（如果接口支援）
		if named, ok := r.exchange.(interface{ GetName() string }); ok {
			exchangeName = named.GetName()
		}
	}

	// 分布式鎖：防止多實例同時對账造成數據不一致
	lockKey := execution.PositionReconciliationLockKey(exchangeName, symbol)

	operationCtx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	ctx, cancelOnLockLoss := context.WithCancel(operationCtx)
	defer cancelOnLockLoss()
	unlockLocal, err := execution.AcquireLocalPositionCoordination(ctx, lockKey)
	if err != nil {
		return failUnverified(fmt.Errorf("等待本进程持仓/下单协调屏障失败: %w", err))
	}

	// 使用阻塞鎖（Lock）而非 TryLock，确保對账一定執行
	err = r.lock.Lock(ctx, lockKey, execution.PositionReconciliationLockTTL)
	if err != nil {
		unlockLocal()
		return failUnverified(fmt.Errorf("获取持仓对账分布式锁失败，拒绝继续开仓: %w", err))
	}
	stopRenew := lock.StartAutoRenew(r.lock, lockKey, execution.PositionReconciliationLockTTL, func(renewErr error) {
		logger.Error("[%s] 持倉對账協調鎖續期失败: %v", exchangeName, renewErr)
		r.pm.FailReconciliation(renewErr)
		cancelOnLockLoss()
	})
	var releaseSubmissions func()
	var criticalSectionOnce sync.Once
	releaseCriticalSection := func() {
		criticalSectionOnce.Do(func() {
			if releaseSubmissions != nil {
				releaseSubmissions()
			}
			stopRenew()
			// The operation context may already be canceled. Use a bounded cleanup
			// context so cancellation does not silently strand the distributed lock.
			unlockCtx, unlockCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer unlockCancel()
			if unlockErr := r.lock.Unlock(unlockCtx, lockKey); unlockErr != nil {
				logger.Warn("⚠️ [%s] 释放對账鎖失败: %v", exchangeName, unlockErr)
			}
			unlockLocal()
		})
	}
	defer releaseCriticalSection()
	releaseSubmissions, err = r.pm.BeginReconciliation(ctx)
	if err != nil {
		return failUnverified(fmt.Errorf("等待下单提交排空后開始對账失败: %w", err))
	}
	defer releaseSubmissions()

	logger.Debugln("🔍 ===== 开始持倉對账 =====")

	// 1. 查詢交易所持倉資訊（使用通用接口）
	positionsRaw, err := r.exchange.GetPositions(ctx, symbol)
	if err != nil {
		return failUnverified(fmt.Errorf("查詢持倉失败: %w", err))
	}
	exchangeSnapshot, err := parseExchangePositionSnapshot(positionsRaw, symbol)
	if err != nil {
		return failUnverified(fmt.Errorf("核實交易所持倉响应失败: %w", err))
	}

	// 2. 查詢所有挂單（使用通用接口）
	openOrdersRaw, err := r.exchange.GetOpenOrders(ctx, symbol)
	if err != nil {
		return failUnverified(fmt.Errorf("查詢挂單失败: %w", err))
	}
	exchangeOpenOrders, err := parseExchangeOpenOrders(openOrdersRaw)
	if err != nil {
		return failUnverified(fmt.Errorf("核實交易所挂單响应失败: %w", err))
	}
	for _, venueOrder := range exchangeOpenOrders {
		if r.openOrderOwnershipVerifier == nil || !r.openOrderOwnershipVerifier(venueOrder) {
			return failUnverified(fmt.Errorf("交易所活动委托 %d 无法核实属于当前运行时；拒绝继续开仓", venueOrder.OrderID))
		}
	}
	if err := ctx.Err(); err != nil {
		return failUnverified(fmt.Errorf("持倉對账协调锁已失效或操作已取消: %w", err))
	}

	// 3. 解析持倉和挂單信息（通用处理）
	logger.Debug("📊 交易所持倉資訊類型: %T", positionsRaw)
	logger.Debug("📊 交易所未完成挂單數: %d", len(exchangeOpenOrders))

	// 3a. 持仓快照必须明确返回可解析的 slice；nil/坏数据不能当成空仓。

	// 4. 计算本地持倉统计
	var localTotal float64
	var localPendingSellQty float64
	var localFilledPosition float64
	var localLongPosition float64
	var localShortPosition float64
	var exchangePosition float64
	var localInventoryErr error
	var activeLocalOrders int
	var activeBuyOrders int  // 開倉方向挂單數（LONG=BUY，SHORT=SELL）
	var activeSellOrders int // 平倉方向挂單數（LONG=SELL，SHORT=BUY）

	// 订單状態常量（與 position 包保持一致）
	const (
		OrderStatusPlaced          = "PLACED"
		OrderStatusConfirmed       = "CONFIRMED"
		OrderStatusPartiallyFilled = "PARTIALLY_FILLED"
		OrderStatusCancelRequested = "CANCEL_REQUESTED"
		OrderStatusUnknown         = "UNKNOWN"
		OrderStatusFilled          = "FILLED"
		PositionStatusFilled       = "FILLED"
	)

	// 平倉方向：LONG 持倉以 SELL 平倉；SHORT 持倉以 BUY 平倉（本地 PositionQty 恆為正數）
	direction := config.NormalizeDirection(r.cfg.Trading.Direction)
	isSpot := config.IsSpotMarketType(r.cfg.Trading.MarketType)
	closeSide, openSide := "SELL", "BUY"
	if direction == "SHORT" {
		closeSide, openSide = "BUY", "SELL"
	}

	r.pm.IterateSlots(func(price float64, slotRaw interface{}) bool {
		if localInventoryErr != nil {
			return false
		}
		// 使用反射提取槽位字段
		v := reflect.ValueOf(slotRaw)
		if v.Kind() == reflect.Pointer {
			if v.IsNil() {
				localInventoryErr = fmt.Errorf("槽位价格 %.8f 的本地台账记录为 nil", price)
				return false
			}
			v = v.Elem()
		}
		if v.Kind() != reflect.Struct {
			localInventoryErr = fmt.Errorf("槽位价格 %.8f 的本地台账类型无效: %T", price, slotRaw)
			return false
		}
		if math.IsNaN(price) || math.IsInf(price, 0) || price <= 0 {
			localInventoryErr = fmt.Errorf("槽位价格无效: %v", price)
			return false
		}
		positionStatusField := v.FieldByName("PositionStatus")
		positionQtyField := v.FieldByName("PositionQty")
		orderSideField := v.FieldByName("OrderSide")
		orderStatusField := v.FieldByName("OrderStatus")
		if !positionStatusField.IsValid() || positionStatusField.Kind() != reflect.String ||
			!positionQtyField.IsValid() || !positionQtyField.CanFloat() ||
			!orderSideField.IsValid() || orderSideField.Kind() != reflect.String ||
			!orderStatusField.IsValid() || orderStatusField.Kind() != reflect.String {
			localInventoryErr = fmt.Errorf("槽位价格 %.8f 的本地台账字段缺失或类型无效", price)
			return false
		}

		// 提取字段的辅助函數
		getStringField := func(name string) string {
			field := v.FieldByName(name)
			if field.IsValid() && field.Kind() == reflect.String {
				return field.String()
			}
			return ""
		}

		getFloat64Field := func(name string) float64 {
			field := v.FieldByName(name)
			if field.IsValid() && field.CanFloat() {
				return field.Float()
			}
			return 0.0
		}

		positionStatus := getStringField("PositionStatus")
		positionQty := getFloat64Field("PositionQty")
		positionLeg := getStringField("PositionLeg")
		orderSide := getStringField("OrderSide")
		orderStatus := getStringField("OrderStatus")
		if positionStatus != "EMPTY" && positionStatus != PositionStatusFilled {
			localInventoryErr = fmt.Errorf("槽位价格 %.8f 的持仓状态未知: %q", price, positionStatus)
			return false
		}
		if math.IsNaN(positionQty) || math.IsInf(positionQty, 0) || positionQty < 0 {
			localInventoryErr = fmt.Errorf("槽位价格 %.8f 的库存数量无效: %v", price, positionQty)
			return false
		}
		if orderSide != "" && orderSide != "BUY" && orderSide != "SELL" {
			localInventoryErr = fmt.Errorf("槽位价格 %.8f 的订单方向无效: %q", price, orderSide)
			return false
		}
		switch orderStatus {
		case "", "NOT_PLACED", OrderStatusPlaced, OrderStatusConfirmed, OrderStatusPartiallyFilled,
			OrderStatusCancelRequested, OrderStatusUnknown, OrderStatusFilled, "CANCELED", "EXPIRED", "REJECTED":
		default:
			localInventoryErr = fmt.Errorf("槽位价格 %.8f 的订单状态未知: %q", price, orderStatus)
			return false
		}
		switch orderStatus {
		case OrderStatusPlaced, OrderStatusConfirmed, OrderStatusPartiallyFilled, OrderStatusCancelRequested,
			OrderStatusUnknown, OrderStatusFilled:
			if orderSide == "" {
				localInventoryErr = fmt.Errorf("槽位价格 %.8f 的活跃订单缺少方向", price)
				return false
			}
		}

		orderMayAffectPosition := orderStatus == OrderStatusPlaced || orderStatus == OrderStatusConfirmed ||
			orderStatus == OrderStatusPartiallyFilled || orderStatus == OrderStatusCancelRequested ||
			orderStatus == OrderStatusUnknown || orderStatus == OrderStatusFilled
		if orderMayAffectPosition {
			activeLocalOrders++
		}

		if positionStatus == PositionStatusFilled {
			localFilledPosition += positionQty
			if math.IsNaN(localFilledPosition) || math.IsInf(localFilledPosition, 0) {
				localInventoryErr = fmt.Errorf("汇总已成交库存数量溢出")
				return false
			}
			if direction == "BOTH" && !isSpot && positionQty > 0 {
				switch positionLeg {
				case "LONG":
					localLongPosition += positionQty
				case "SHORT":
					localShortPosition += positionQty
				default:
					localInventoryErr = fmt.Errorf("双向模式槽位 %.8f 缺少有效 PositionLeg: %q", price, positionLeg)
					return false
				}
				if math.IsNaN(localLongPosition) || math.IsInf(localLongPosition, 0) ||
					math.IsNaN(localShortPosition) || math.IsInf(localShortPosition, 0) {
					localInventoryErr = fmt.Errorf("汇总双向模式逐腿持仓数量溢出")
					return false
				}
			}
			if orderSide == closeSide && (orderStatus == OrderStatusPlaced || orderStatus == OrderStatusConfirmed ||
				orderStatus == OrderStatusPartiallyFilled || orderStatus == OrderStatusCancelRequested ||
				orderStatus == OrderStatusUnknown) {
				localPendingSellQty += positionQty
				activeSellOrders++
			}
		}

		if orderSide == openSide && positionStatus != PositionStatusFilled && (orderStatus == OrderStatusPlaced || orderStatus == OrderStatusConfirmed ||
			orderStatus == OrderStatusPartiallyFilled || orderStatus == OrderStatusUnknown) {
			activeBuyOrders++
		}

		return true
	})
	if localInventoryErr != nil {
		r.pm.FailReconciliation(localInventoryErr)
		return fmt.Errorf("本地持仓台账无法核实，已阻断后续开仓: %w", localInventoryErr)
	}

	localTotal = localFilledPosition

	logger.Debug("📊 [對账统计] 本地持倉: %.4f, 挂單賣單: %d 個 (%.4f), 挂單買單: %d 個",
		localTotal, activeSellOrders, localPendingSellQty, activeBuyOrders)

	// 5. 输出已验证快照统计（從交易所接口獲取基础币种，支援U本位和币本位合約）
	baseCurrency := r.exchange.GetBaseAsset()
	logger.Info("📊 [對账快照] 本地持倉: %.4f %s, 挂單賣單: %d 個 (%.4f), 挂單買單: %d 個",
		localTotal, baseCurrency, activeSellOrders, localPendingSellQty, activeBuyOrders)

	totalBuyQty := r.pm.GetTotalBuyQty()
	totalSellQty := r.pm.GetTotalSellQty()
	profitSpread := r.pm.GetProfitSpread()
	estimatedProfit := totalSellQty * profitSpread
	logger.Info("📊 [统计] 對账次數: %d, 累计買入: %.2f, 累计賣出: %.2f, 預计盈利: %.2f U",
		r.pm.GetReconcileCount(), totalBuyQty, totalSellQty, estimatedProfit)

	// Persist after the position/order critical section: storage has no context
	// contract and must not stall physical order submissions while holding the
	// cross-process reconciliation lock.
	reconcileTime := time.Now()
	markReconciled := func() {
		r.pm.IncrementReconcileCount()
		r.pm.UpdateLastReconcileTime(reconcileTime)
	}
	saveReconciliationHistory := func() {
		if r.storage == nil {
			return
		}
		positionDiff := localTotal - exchangePosition

		if err := r.storage.SaveReconciliationHistory(symbol, reconcileTime, localTotal, exchangePosition, positionDiff,
			activeBuyOrders, activeSellOrders, localPendingSellQty, totalBuyQty, totalSellQty, estimatedProfit); err != nil {
			logger.Warn("⚠️ 保存對账历史失败: %v", err)
		}
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("持倉對账协调锁已失效或操作已取消，拒绝同步槽位: %w", err)
	}

	// 7. 检查持倉差异並執行同步
	// 交易所 Position.Size 帶符號（多倉為正、空倉為負），本地 PositionQty 恆為正數，需按方向歸一化後再比較
	spotInvPolicy := config.NormalizeSpotInventoryPolicy(r.cfg.Trading.SpotInventoryPolicy)
	if direction == "BOTH" && !isSpot {
		if exchangeSnapshot.hasNet || !exchangeSnapshot.directional || !exchangeSnapshot.hasLong || !exchangeSnapshot.hasShort {
			err := fmt.Errorf("双向模式需要交易所同时提供 LONG/SHORT 逐腿快照，当前响应仅能证明净仓或缺少一侧")
			r.pm.FailReconciliation(err)
			return err
		}
		const legTolerance = 0.00000001
		if math.Abs(localLongPosition-exchangeSnapshot.longQty) > legTolerance ||
			math.Abs(localShortPosition-exchangeSnapshot.shortQty) > legTolerance {
			err := fmt.Errorf("双向持仓逐腿不一致：本地 LONG %.12g/SHORT %.12g，交易所 LONG %.12g/SHORT %.12g；拒绝按净仓修剪或收编",
				localLongPosition, localShortPosition, exchangeSnapshot.longQty, exchangeSnapshot.shortQty)
			r.pm.FailReconciliation(err)
			return err
		}
		exchangePosition = exchangeSnapshot.longQty + exchangeSnapshot.shortQty
	} else {
		if exchangeSnapshot.directional {
			err := fmt.Errorf("单向/现货模式收到逐腿交易所快照，无法与当前配置安全比较")
			r.pm.FailReconciliation(err)
			return err
		}
		exchangePosition = exchangeSnapshot.netSize
	}
	exchangePosition, syncAllowed := normalizeExchangePositionForSync(direction, isSpot, exchangePosition)
	if !syncAllowed {
		err := fmt.Errorf("交易所净持仓 %g 无法与本地 %s 方向持仓安全比较；本轮对账未完成", exchangePosition, direction)
		r.pm.FailReconciliation(err)
		return err
	}
	diff := math.Abs(localTotal - exchangePosition)
	// 使用相對较小的阈值，但要考虑到浮点數精度
	if diff > 0.00000001 {
		logger.Warn("🚨 [對账預警] 持倉不一致! 本地: %.6f, 交易所: %.6f, 差异: %.6f",
			localTotal, exchangePosition, localTotal-exchangePosition)

		// A single position snapshot is not safe to apply while any order can
		// still change account exposure. This includes venue orders not owned by
		// this grid and local orders awaiting their venue acknowledgement.
		if len(exchangeOpenOrders) > 0 || activeLocalOrders > 0 {
			logger.Warn("⚠️ [對账同步] 存在未完成/未核实挂單（交易所: %d, 本地: %d，開倉方向: %d，平倉方向: %d），跳過持倉同步",
				len(exchangeOpenOrders), activeLocalOrders, activeBuyOrders, activeSellOrders)
			err := fmt.Errorf("持仓差异无法在存在未决委托时安全核实；交易所活动委托 %d，本地活动委托 %d", len(exchangeOpenOrders), activeLocalOrders)
			r.pm.FailReconciliation(err)
			return err
		}

		// 🔥 自动同步逻辑：如果交易所持倉為0，但本地认為有持倉
		// 这种情况通常发生在手动平倉、重啟程序或訂單流丢失時
		// ⚠️ 重要：如果有挂單（特别是賣單），不应该清空持仓，因為挂單意味着持仓正在被卖出
		if math.Abs(exchangePosition) < 0.00000001 && math.Abs(localTotal) > 0.00000001 {
			// 检查是否有挂單：如果有挂單，说明持仓正在交易中，不应该强制清空
			if activeBuyOrders > 0 || activeSellOrders > 0 {
				logger.Warn("⚠️ [對账同步] 检测到挂單（買單: %d, 賣單: %d），跳過强制同步以避免誤清空持仓",
					activeBuyOrders, activeSellOrders)
				logger.Warn("💡 [對账说明] 本地持仓: %.6f, 交易所持仓: %.6f, 待卖数量: %.6f",
					localTotal, exchangePosition, localPendingSellQty)
			} else {
				logger.Warn("⚠️ [對账同步] 交易所持倉已清空且無挂單，正在强制同步本地状態...")
				if err := r.pm.ForceSyncPositions(0); err != nil {
					r.pm.FailReconciliation(err)
					return fmt.Errorf("清理本地持仓状态失败，拒绝将本轮记为对账成功: %w", err)
				}
			}
		} else if localTotal > exchangePosition && exchangePosition > 0.00000001 {
			// 🔥 本地持倉超出交易所實際持倉：存在「幻影」槽位
			// 这会導致平倉委託总量超過實際持倉，必須修剪多餘的本地槽位
			logger.Warn("🚨 [對账同步] 本地持倉(%.6f) > 交易所持倉(%.6f)，存在幻影槽位，開始修剪...",
				localTotal, exchangePosition)
			if err := r.pm.ForceSyncPositions(exchangePosition); err != nil {
				r.pm.FailReconciliation(err)
				return fmt.Errorf("修剪本地持仓状态失败，拒绝将本轮记为对账成功: %w", err)
			}
		} else if localTotal < exchangePosition && exchangePosition > 0.00000001 {
			// Account-level derivatives positions do not prove ownership by this
			// Bot. Never adopt the unexplained difference into strategy slots.
			if !isSpot {
				ownershipErr := fmt.Errorf("交易所合约持仓 %.8f 大于本 Bot 台账 %.8f，无法证明差额归属；拒绝自动收编并保持交易门控", exchangePosition, localTotal)
				r.pm.FailReconciliation(ownershipErr)
				return ownershipErr
			}
			// 現貨 conservative 時不自動收編外部基礎幣。
			if spotInvPolicy != config.SpotInventoryPolicyAdoptAll {
				logger.Debug("ℹ️ [對账同步] 現貨庫存策略為 conservative，跳過從交易所補齊本地網格庫存（本地: %.6f, 交易所: %.6f）",
					localTotal, exchangePosition)
			} else {
				logger.Warn("🚨 [對账同步] 本地持倉(%.6f) < 交易所持倉(%.6f)，以交易所為準補齊本地持倉...",
					localTotal, exchangePosition)
				if err := r.pm.ForceSyncPositions(exchangePosition); err != nil {
					r.pm.FailReconciliation(err)
					return fmt.Errorf("补齐本地持仓状态失败，拒绝将本轮记为对账成功: %w", err)
				}
			}
		}
	}

	logger.Debugln("🔍 ===== 對账完成 =====")
	r.pm.CompleteReconciliation()
	markReconciled()
	releaseCriticalSection()
	saveReconciliationHistory()
	return nil
}

func parseExchangeOpenOrders(raw interface{}) ([]*exchange.Order, error) {
	if raw == nil {
		return nil, fmt.Errorf("挂單响应为 nil，无法确认不存在未完成订单")
	}
	orders, ok := raw.([]*exchange.Order)
	if !ok {
		return nil, fmt.Errorf("挂單响应类型不可解析: %T", raw)
	}
	if orders == nil {
		return nil, fmt.Errorf("挂單响应为 nil 切片，无法确认不存在未完成订单")
	}
	for i, order := range orders {
		if order == nil {
			return nil, fmt.Errorf("挂單响应第 %d 项为 nil", i)
		}
	}
	return orders, nil
}

func parseExchangePositionSnapshot(raw interface{}, symbol string) (exchangePositionSnapshot, error) {
	var snapshot exchangePositionSnapshot
	if raw == nil {
		return snapshot, fmt.Errorf("持仓响应为 nil，无法证明账户为空仓")
	}
	positions := reflect.ValueOf(raw)
	if positions.Kind() != reflect.Slice && positions.Kind() != reflect.Array {
		return snapshot, fmt.Errorf("持仓响应类型不可解析: %T", raw)
	}
	if positions.Kind() == reflect.Slice && positions.IsNil() {
		return snapshot, fmt.Errorf("持仓响应为 nil 切片，无法证明账户为空仓")
	}
	for i := 0; i < positions.Len(); i++ {
		position := positions.Index(i)
		for position.IsValid() && (position.Kind() == reflect.Interface || position.Kind() == reflect.Ptr) {
			if position.IsNil() {
				return snapshot, fmt.Errorf("持仓响应包含 nil 项")
			}
			position = position.Elem()
		}
		if !position.IsValid() || position.Kind() != reflect.Struct {
			return snapshot, fmt.Errorf("持仓响应第 %d 项不是结构体", i)
		}
		symbolField := position.FieldByName("Symbol")
		sizeField := position.FieldByName("Size")
		if !symbolField.IsValid() || !symbolField.CanInterface() || symbolField.Kind() != reflect.String || !sizeField.IsValid() || !sizeField.CanInterface() || !sizeField.CanFloat() {
			return snapshot, fmt.Errorf("持仓响应第 %d 项缺少有效 Symbol/Size 字段", i)
		}
		if symbolField.String() != symbol {
			continue
		}
		current := sizeField.Float()
		if math.IsNaN(current) || math.IsInf(current, 0) {
			return snapshot, fmt.Errorf("交易所持仓 %s 数量不是有限值", symbol)
		}
		sideField := position.FieldByName("PositionSide")
		side := ""
		if sideField.IsValid() && sideField.Kind() == reflect.String && sideField.CanInterface() {
			side = strings.ToUpper(strings.TrimSpace(sideField.String()))
		}
		switch side {
		case "LONG":
			if snapshot.hasNet || snapshot.hasLong {
				return snapshot, fmt.Errorf("持仓响应存在混合模式或重复 LONG 逐腿快照 %s", symbol)
			}
			snapshot.longQty = math.Abs(current)
			snapshot.hasLong = true
			snapshot.directional = true
		case "SHORT":
			if snapshot.hasNet || snapshot.hasShort {
				return snapshot, fmt.Errorf("持仓响应存在混合模式或重复 SHORT 逐腿快照 %s", symbol)
			}
			snapshot.shortQty = math.Abs(current)
			snapshot.hasShort = true
			snapshot.directional = true
		case "", "BOTH", "NET":
			if snapshot.directional || snapshot.hasNet {
				return snapshot, fmt.Errorf("持仓响应包含重复或混合模式交易对 %s", symbol)
			}
			snapshot.netSize = current
			snapshot.hasNet = true
		default:
			return snapshot, fmt.Errorf("交易所持仓 %s 方向字段无效: %q", symbol, side)
		}
	}
	return snapshot, nil
}

// normalizeExchangePositionForSync 將交易所帶符號淨持倉轉換為可與本地（正數）持倉比較的數量
// 回傳 (歸一化持倉, 是否允許自動同步)：
//   - LONG / 現貨：要求淨持倉 >= 0，出現空倉時方向不符，跳過同步
//   - SHORT：取絕對值；出現多倉時方向不符，跳過同步
//   - BOTH：呼叫方先證明 LONG/SHORT 兩腿逐一相等；此處收到的是已核實的毛額之和
func normalizeExchangePositionForSync(direction string, isSpot bool, signedSize float64) (float64, bool) {
	const eps = 0.00000001
	if isSpot {
		return signedSize, true
	}
	switch direction {
	case "BOTH":
		// The caller has already validated both exchange legs against the local
		// PositionLeg totals; this is their gross sum, not a signed net snapshot.
		return signedSize, true
	case "SHORT":
		if signedSize > eps {
			logger.Warn("🚨 [對账同步] SHORT 模式但交易所持倉為多倉 %.6f，方向不符，跳過自動同步（請人工核對）", signedSize)
			return signedSize, false
		}
		return math.Abs(signedSize), true
	default:
		if signedSize < -eps {
			logger.Warn("🚨 [對账同步] LONG 模式但交易所持倉為空倉 %.6f，方向不符，跳過自動同步（請人工核對）", signedSize)
			return signedSize, false
		}
		return signedSize, true
	}
}
