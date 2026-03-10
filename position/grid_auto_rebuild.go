package position

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/logger"
)

// GridAutoRebuilder 网格自动重建管理器
// 当价格长期偏离网格区间时，自动撤销旧订单并按新价格重建网格
type GridAutoRebuilder struct {
	spm    *SuperPositionManager
	config config.GridAutoRebuildConfig

	// 状态追踪
	lastRebuildTime    time.Time // 最后一次重建时间
	rebuildCountInHour int       // 当前小时内重建次数
	lastHourReset      time.Time // 上次小时计数重置时间

	// 运行控制
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// 订单创建时间追踪（用于检测过期订单）
	orderCreationTimes sync.Map // map[int64]time.Time
	mu                  sync.RWMutex
}

// NewGridAutoRebuilder 创建自动重建管理器
func NewGridAutoRebuilder(spm *SuperPositionManager, cfg config.GridAutoRebuildConfig) *GridAutoRebuilder {
	ctx, cancel := context.WithCancel(context.Background())

	return &GridAutoRebuilder{
		spm:    spm,
		config: cfg,
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start 启动自动重建检查
func (gar *GridAutoRebuilder) Start() {
	if !gar.config.Enabled {
		logger.Info("🔄 [%s] 网格自动重建未启用", gar.spm.logPrefix())
		return
	}

	// 设置默认值
	if gar.config.CheckIntervalMinutes <= 0 {
		gar.config.CheckIntervalMinutes = 5
	}
	if gar.config.PriceDeviationLayers <= 0 {
		gar.config.PriceDeviationLayers = 5
	}
	if gar.config.OrderExpireMinutes <= 0 {
		gar.config.OrderExpireMinutes = 30
	}
	if gar.config.ExpiredOrderRatio <= 0 {
		gar.config.ExpiredOrderRatio = 0.7
	}
	if gar.config.MaxRebuildsPerHour <= 0 {
		gar.config.MaxRebuildsPerHour = 2
	}
	if gar.config.MinRebuildInterval <= 0 {
		gar.config.MinRebuildInterval = 15
	}
	if gar.config.RebuildMode == "" {
		gar.config.RebuildMode = "smart"
	}

	gar.wg.Add(1)
	go gar.run()

	logger.Info("✅ [%s] 网格自动重建已启动 (检查间隔: %d分钟, 价格偏离: %d层, 订单过期: %d分钟, 过期比例: %.0f%%)",
		gar.spm.logPrefix(),
		gar.config.CheckIntervalMinutes,
		gar.config.PriceDeviationLayers,
		gar.config.OrderExpireMinutes,
		gar.config.ExpiredOrderRatio*100)
}

// Stop 停止自动重建检查
func (gar *GridAutoRebuilder) Stop() {
	if gar.cancel != nil {
		gar.cancel()
	}
	gar.wg.Wait()
	logger.Info("🛑 [%s] 网格自动重建已停止", gar.spm.logPrefix())
}

// run 主循环
func (gar *GridAutoRebuilder) run() {
	defer gar.wg.Done()

	ticker := time.NewTicker(time.Duration(gar.config.CheckIntervalMinutes) * time.Minute)
	defer ticker.Stop()

	// 启动时立即检查一次
	gar.checkAndRebuild()

	for {
		select {
		case <-gar.ctx.Done():
			return
		case <-ticker.C:
			gar.checkAndRebuild()
		}
	}
}

// checkAndRebuild 检查是否需要重建，如果需要则执行重建
func (gar *GridAutoRebuilder) checkAndRebuild() {
	// 检查保护机制
	if !gar.checkProtection() {
		return
	}

	// 检查触发条件
	shouldRebuild, reason := gar.shouldTriggerRebuild()
	if !shouldRebuild {
		return
	}

	logger.Info("🔍 [%s] 网格自动重建触发: %s", gar.spm.logPrefix(), reason)

	// 执行重建
	if err := gar.rebuild(); err != nil {
		logger.Error("❌ [%s] 网格自动重建失败: %v", gar.spm.logPrefix(), err)
		return
	}

	// 更新状态
	gar.updateRebuildStats()
	logger.Info("✅ [%s] 网格自动重建完成", gar.spm.logPrefix())
}

// checkProtection 检查保护机制
func (gar *GridAutoRebuilder) checkProtection() bool {
	gar.mu.Lock()
	defer gar.mu.Unlock()

	now := time.Now()

	// 重置小时计数
	if now.Sub(gar.lastHourReset) >= time.Hour {
		gar.rebuildCountInHour = 0
		gar.lastHourReset = now
	}

	// 检查每小时最大重建次数
	if gar.rebuildCountInHour >= gar.config.MaxRebuildsPerHour {
		logger.Debug("⏸️ [%s] 网格自动重建: 已达到每小时最大重建次数 (%d)",
			gar.spm.logPrefix(), gar.config.MaxRebuildsPerHour)
		return false
	}

	// 检查两次重建最小间隔
	if !gar.lastRebuildTime.IsZero() && now.Sub(gar.lastRebuildTime) < time.Duration(gar.config.MinRebuildInterval)*time.Minute {
		logger.Debug("⏸️ [%s] 网格自动重建: 距离上次重建时间不足 %d 分钟",
			gar.spm.logPrefix(), gar.config.MinRebuildInterval)
		return false
	}

	return true
}

// shouldTriggerRebuild 检查是否满足重建条件
func (gar *GridAutoRebuilder) shouldTriggerRebuild() (bool, string) {
	currentPrice := gar.spm.lastMarketPrice.Load().(float64)
	priceInterval := gar.spm.config.Trading.PriceInterval

	// 条件 1: 价格偏离网格中心超过指定层数
	if deviationReason := gar.checkPriceDeviation(currentPrice, priceInterval); deviationReason != "" {
		return true, deviationReason
	}

	// 条件 2: 超过指定比例的订单过期
	if expireReason := gar.checkExpiredOrders(); expireReason != "" {
		return true, expireReason
	}

	return false, ""
}

// checkPriceDeviation 检查价格偏离条件
func (gar *GridAutoRebuilder) checkPriceDeviation(currentPrice, priceInterval float64) string {
	// 计算当前价格与锚点的偏差
	anchorPrice := gar.spm.anchorPrice
	if anchorPrice <= 0 {
		return ""
	}

	// 计算偏差层数
	deviationLayers := int(math.Abs(currentPrice-anchorPrice)/priceInterval) - gar.spm.config.Trading.BuyWindowSize/2

	if deviationLayers >= gar.config.PriceDeviationLayers {
		direction := "上" // 偏离方向
		if currentPrice < anchorPrice {
			direction = "下"
		}
		return fmt.Sprintf("价格%s偏离 %d 层网格 (当前价格: %.2f, 锚点: %.2f, 偏离层数: %d)",
			direction, deviationLayers, currentPrice, anchorPrice, deviationLayers)
	}

	return ""
}

// checkExpiredOrders 检查订单过期条件
func (gar *GridAutoRebuilder) checkExpiredOrders() string {
	now := time.Now()
	expireDuration := time.Duration(gar.config.OrderExpireMinutes) * time.Minute

	var totalOrders, expiredOrders int

	gar.spm.slots.Range(func(key, value interface{}) bool {
		slot := value.(*InventorySlot)
		slot.mu.RLock()
		defer slot.mu.RUnlock()

		// 只统计开仓方向的订单
		openSide := "BUY"
		if gar.spm.isShort() {
			openSide = "SELL"
		}

		if slot.OrderSide == openSide && slot.OrderStatus == OrderStatusPlaced {
			totalOrders++

			// 检查订单是否过期
			if !slot.OrderCreatedAt.IsZero() && now.Sub(slot.OrderCreatedAt) > expireDuration {
				expiredOrders++
			}
		}
		return true
	})

	if totalOrders == 0 {
		return ""
	}

	expiredRatio := float64(expiredOrders) / float64(totalOrders)
	if expiredRatio >= gar.config.ExpiredOrderRatio {
		return fmt.Sprintf("订单过期比例 %.0f%% (过期: %d/%d, 阈值: %.0f%%)",
			expiredRatio*100, expiredOrders, totalOrders, gar.config.ExpiredOrderRatio*100)
	}

	return ""
}

// rebuild 执行网格重建
func (gar *GridAutoRebuilder) rebuild() error {
	// 撤销所有开仓订单
	gar.spm.CancelAllOpenOrders()

	// 更新网格锚点到当前价格
	currentPrice := gar.spm.lastMarketPrice.Load().(float64)
	gar.spm.mu.Lock()
	gar.spm.anchorPrice = currentPrice
	gar.spm.mu.Unlock()

	logger.Info("📊 [%s] 网格锚点已更新: %.2f -> %.2f",
		gar.spm.logPrefix(), gar.spm.anchorPrice, currentPrice)

	// 系统会在下一轮价格更新时自动按新锚点挂单
	return nil
}

// updateRebuildStats 更新重建统计
func (gar *GridAutoRebuilder) updateRebuildStats() {
	gar.mu.Lock()
	defer gar.mu.Unlock()

	gar.lastRebuildTime = time.Now()
	gar.rebuildCountInHour++
}

// RecordOrderCreated 记录订单创建时间（由 SuperPositionManager 调用）
func (gar *GridAutoRebuilder) RecordOrderCreated(orderID int64) {
	if gar == nil || !gar.config.Enabled {
		return
	}
	gar.orderCreationTimes.Store(orderID, time.Now())
}

// RecordOrderFilled 记录订单成交（由 SuperPositionManager 调用）
func (gar *GridAutoRebuilder) RecordOrderFilled(orderID int64) {
	if gar == nil || !gar.config.Enabled {
		return
	}
	gar.orderCreationTimes.Delete(orderID)
}
