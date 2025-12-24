package safety

import (
	"context"
	"fmt"
	"opensqt/config"
	"opensqt/logger"
	"reflect"
	"time"
)

// IExchange 定义对账所需的交易所接口方法
type IExchange interface {
	GetPositions(ctx context.Context, symbol string) (interface{}, error)
	GetOpenOrders(ctx context.Context, symbol string) (interface{}, error)
	GetBaseAsset() string // 获取基础资产（交易币种）
}

// SlotInfo 槽位信息（避免直接依赖 position 包的内部结构）
type SlotInfo struct {
	Price          float64
	PositionStatus string
	PositionQty    float64
	OrderID        int64
	OrderSide      string
	OrderStatus    string
	OrderCreatedAt time.Time
}

// IPositionManager 定义对账所需的仓位管理器接口方法
type IPositionManager interface {
	// 遍历所有槽位（封装 sync.Map.Range）
	// 注意：slot 为 interface{} 类型，需要转换为 SlotInfo
	IterateSlots(fn func(price float64, slot interface{}) bool)
	// 获取统计数据
	GetTotalBuyQty() float64
	GetTotalSellQty() float64
	GetReconcileCount() int64
	// 更新统计数据
	IncrementReconcileCount()
	UpdateLastReconcileTime(t time.Time)
	// 获取配置信息
	GetSymbol() string
	GetPriceInterval() float64
}

// Reconciler 持仓对账器
type Reconciler struct {
	cfg          *config.Config
	exchange     IExchange
	pm           IPositionManager
	pauseChecker func() bool
}

// NewReconciler 创建对账器
func NewReconciler(cfg *config.Config, exchange IExchange, pm IPositionManager) *Reconciler {
	return &Reconciler{
		cfg:      cfg,
		exchange: exchange,
		pm:       pm,
	}
}

// SetPauseChecker 设置暂停检查函数（用于风控暂停）
func (r *Reconciler) SetPauseChecker(checker func() bool) {
	r.pauseChecker = checker
}

// Start 启动对账协程
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
				logger.Info("⏹️ 持仓对账协程已停止")
				return
			case <-ticker.C:
				if err := r.Reconcile(); err != nil {
					logger.Error("❌ [对账失败] %v", err)
				}
			}
		}
	}()
	logger.Info("✅ 持仓对账已启动 (间隔: %d秒)", r.cfg.Trading.ReconcileInterval)
}

// Reconcile 执行对账（通用实现，支持所有交易所）
func (r *Reconciler) Reconcile() error {
	// 检查是否暂停（风控触发时不输出日志）
	if r.pauseChecker != nil && r.pauseChecker() {
		return nil
	}

	logger.Debugln("🔍 ===== 开始持仓对账 =====")

	symbol := r.pm.GetSymbol()

	// 1. 查询交易所持仓信息（使用通用接口）
	positionsRaw, err := r.exchange.GetPositions(context.Background(), symbol)
	if err != nil {
		return fmt.Errorf("查询持仓失败: %w", err)
	}

	// 2. 查询所有挂单（使用通用接口）
	openOrdersRaw, err := r.exchange.GetOpenOrders(context.Background(), symbol)
	if err != nil {
		return fmt.Errorf("查询挂单失败: %w", err)
	}

	// 3. 解析持仓和挂单信息（通用处理）
	logger.Debug("📊 交易所持仓信息类型: %T", positionsRaw)
	logger.Debug("📊 交易所挂单信息类型: %T", openOrdersRaw)

	// 4. 计算本地持仓统计
	var localTotal float64
	var localPendingSellQty float64
	var localFilledPosition float64
	var activeBuyOrders int
	var activeSellOrders int

	// 订单状态常量（与 position 包保持一致）
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

		// 提取字段的辅助函数
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

	logger.Debug("📊 [对账统计] 本地持仓: %.4f, 挂单卖单: %d 个 (%.4f), 挂单买单: %d 个",
		localTotal, activeSellOrders, localPendingSellQty, activeBuyOrders)

	r.pm.IncrementReconcileCount()

	// 5. 输出对账统计（从交易所接口获取基础币种，支持U本位和币本位合约）
	baseCurrency := r.exchange.GetBaseAsset()
	logger.Info("✅ [对账完成] 本地持仓: %.4f %s, 挂单卖单: %d 个 (%.4f), 挂单买单: %d 个",
		localTotal, baseCurrency, activeSellOrders, localPendingSellQty, activeBuyOrders)

	r.pm.UpdateLastReconcileTime(time.Now())

	totalBuyQty := r.pm.GetTotalBuyQty()
	totalSellQty := r.pm.GetTotalSellQty()
	priceInterval := r.pm.GetPriceInterval()
	estimatedProfit := totalSellQty * priceInterval
	logger.Info("📊 [统计] 对账次数: %d, 累计买入: %.2f, 累计卖出: %.2f, 预计盈利: %.2f U",
		r.pm.GetReconcileCount(), totalBuyQty, totalSellQty, estimatedProfit)
	logger.Debugln("🔍 ===== 对账完成 =====")
	return nil
}
