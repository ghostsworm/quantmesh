package safety

import (
	"context"
	"fmt"
	"math"
	"quantmesh/config"
	"quantmesh/lock"
	"quantmesh/logger"
	"reflect"
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
	ForceSyncPositions(exchangePosition float64)
}

// ReconciliationStorage 對账存儲介面（避免循環匯入，使用函數類型）
type ReconciliationStorage interface {
	SaveReconciliationHistory(symbol string, reconcileTime time.Time, localPosition, exchangePosition, positionDiff float64,
		activeBuyOrders, activeSellOrders int, pendingSellQty, totalBuyQty, totalSellQty, estimatedProfit float64) error
}

// Reconciler 持倉對账器
type Reconciler struct {
	cfg                  *config.Config
	exchange             IExchange
	pm                   IPositionManager
	pauseChecker         func() bool
	storage              ReconciliationStorage // 可選的存儲服務
	lock                 lock.DistributedLock  // 分布式鎖
	lastReconcileTime    time.Time             // 上次對账時间
	reconcileMu          sync.Mutex            // 對账互斥鎖
	minReconcileInterval time.Duration         // 最小對账间隔（防止频繁調用）
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
				if err := r.Reconcile(); err != nil {
					logger.Error("❌ [對账失败] %v", err)
				}
			}
		}
	}()
	logger.Info("✅ 持倉對账已啟动 (间隔: %d秒)", r.cfg.Trading.ReconcileInterval)
}

// Reconcile 執行對账（通用實現，支援所有交易所）
func (r *Reconciler) Reconcile() error {
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
		time.Sleep(waitTime)
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
	lockKey := fmt.Sprintf("reconcile:%s:%s", exchangeName, symbol)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 使用阻塞鎖（Lock）而非 TryLock，确保對账一定執行
	err := r.lock.Lock(ctx, lockKey, 30*time.Second)
	if err != nil {
		logger.Warn("⚠️ [%s] 獲取對账鎖失败: %v，跳過本次對账", exchangeName, err)
		return nil // 鎖獲取失败不返回錯误，只是跳過
	}
	defer func() {
		if unlockErr := r.lock.Unlock(ctx, lockKey); unlockErr != nil {
			logger.Warn("⚠️ [%s] 释放對账鎖失败: %v", exchangeName, unlockErr)
		}
	}()

	logger.Debugln("🔍 ===== 开始持倉對账 =====")

	// 1. 查詢交易所持倉資訊（使用通用接口）
	positionsRaw, err := r.exchange.GetPositions(context.Background(), symbol)
	if err != nil {
		return fmt.Errorf("查詢持倉失败: %w", err)
	}

	// 2. 查詢所有挂單（使用通用接口）
	openOrdersRaw, err := r.exchange.GetOpenOrders(context.Background(), symbol)
	if err != nil {
		return fmt.Errorf("查詢挂單失败: %w", err)
	}

	// 3. 解析持倉和挂單信息（通用处理）
	logger.Debug("📊 交易所持倉資訊類型: %T", positionsRaw)
	logger.Debug("📊 交易所挂單信息類型: %T", openOrdersRaw)

	// 3a. 解析交易所持倉數量
	exchangePosition := 0.0
	vPositions := reflect.ValueOf(positionsRaw)
	if vPositions.Kind() == reflect.Slice {
		for i := 0; i < vPositions.Len(); i++ {
			pos := vPositions.Index(i)
			if pos.Kind() == reflect.Ptr {
				pos = pos.Elem()
			}
			if pos.Kind() == reflect.Struct {
				symbolField := pos.FieldByName("Symbol")
				sizeField := pos.FieldByName("Size")
				if symbolField.IsValid() && sizeField.IsValid() {
					if symbolField.String() == symbol {
						exchangePosition = sizeField.Float()
						break
					}
				}
			}
		}
	}

	// 4. 计算本地持倉统计
	var localTotal float64
	var localPendingSellQty float64
	var localFilledPosition float64
	var activeBuyOrders int
	var activeSellOrders int

	// 订單状態常量（與 position 包保持一致）
	const (
		OrderStatusPlaced          = "PLACED"
		OrderStatusConfirmed       = "CONFIRMED"
		OrderStatusPartiallyFilled = "PARTIALLY_FILLED"
		OrderStatusCancelRequested = "CANCEL_REQUESTED"
		PositionStatusFilled       = "FILLED"
	)

	r.pm.IterateSlots(func(price float64, slotRaw interface{}) bool {
		// 使用反射提取槽位字段
		v := reflect.ValueOf(slotRaw)
		if v.Kind() != reflect.Struct {
			return true
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
		orderSide := getStringField("OrderSide")
		orderStatus := getStringField("OrderStatus")

		if positionStatus == PositionStatusFilled {
			localFilledPosition += positionQty
			if orderSide == "SELL" && (orderStatus == OrderStatusPlaced || orderStatus == OrderStatusConfirmed ||
				orderStatus == OrderStatusPartiallyFilled || orderStatus == OrderStatusCancelRequested) {
				localPendingSellQty += positionQty
				activeSellOrders++
			}
		}

		if orderSide == "BUY" && (orderStatus == OrderStatusPlaced || orderStatus == OrderStatusConfirmed ||
			orderStatus == OrderStatusPartiallyFilled) {
			activeBuyOrders++
		}

		return true
	})

	localTotal = localFilledPosition

	logger.Debug("📊 [對账统计] 本地持倉: %.4f, 挂單賣單: %d 個 (%.4f), 挂單買單: %d 個",
		localTotal, activeSellOrders, localPendingSellQty, activeBuyOrders)

	r.pm.IncrementReconcileCount()

	// 5. 输出對账统计（從交易所接口獲取基础币种，支援U本位和币本位合約）
	baseCurrency := r.exchange.GetBaseAsset()
	logger.Info("✅ [對账完成] 本地持倉: %.4f %s, 挂單賣單: %d 個 (%.4f), 挂單買單: %d 個",
		localTotal, baseCurrency, activeSellOrders, localPendingSellQty, activeBuyOrders)

	r.pm.UpdateLastReconcileTime(time.Now())

	totalBuyQty := r.pm.GetTotalBuyQty()
	totalSellQty := r.pm.GetTotalSellQty()
	profitSpread := r.pm.GetProfitSpread()
	estimatedProfit := totalSellQty * profitSpread
	logger.Info("📊 [统计] 對账次數: %d, 累计買入: %.2f, 累计賣出: %.2f, 預计盈利: %.2f U",
		r.pm.GetReconcileCount(), totalBuyQty, totalSellQty, estimatedProfit)

	// 6. 保存對账历史到數據库（如果存儲服務可用）
	if r.storage != nil {
		reconcileTime := time.Now()
		positionDiff := localTotal - exchangePosition

		if err := r.storage.SaveReconciliationHistory(symbol, reconcileTime, localTotal, exchangePosition, positionDiff,
			activeBuyOrders, activeSellOrders, localPendingSellQty, totalBuyQty, totalSellQty, estimatedProfit); err != nil {
			logger.Warn("⚠️ 保存對账历史失败: %v", err)
		}
	}

	// 7. 检查持倉差异並執行同步
	diff := math.Abs(localTotal - exchangePosition)
	// 使用相對较小的阈值，但要考虑到浮点數精度
	if diff > 0.00000001 {
		logger.Warn("🚨 [對账預警] 持倉不一致! 本地: %.6f, 交易所: %.6f, 差异: %.6f",
			localTotal, exchangePosition, localTotal-exchangePosition)

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
				r.pm.ForceSyncPositions(0)
			}
		} else if localTotal > exchangePosition && exchangePosition > 0.00000001 {
			// 🔥 本地持倉超出交易所實際持倉：存在「幻影」槽位
			// 这会導致平倉委託总量超過實際持倉，必須修剪多餘的本地槽位
			logger.Warn("🚨 [對账同步] 本地持倉(%.6f) > 交易所持倉(%.6f)，存在幻影槽位，開始修剪...",
				localTotal, exchangePosition)
			r.pm.ForceSyncPositions(exchangePosition)
		} else if localTotal < exchangePosition && exchangePosition > 0.00000001 {
			// 🔥 本地持倉少於交易所：以交易所為準，補齊本地持倉差額
			logger.Warn("🚨 [對账同步] 本地持倉(%.6f) < 交易所持倉(%.6f)，以交易所為準補齊本地持倉...",
				localTotal, exchangePosition)
			r.pm.ForceSyncPositions(exchangePosition)
		}
	}

	logger.Debugln("🔍 ===== 對账完成 =====")
	return nil
}
