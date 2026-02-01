package safety

import (
	"context"
	"fmt"
	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/logger"
	"sync"
	"time"
)

// DepthSnapshot 深度快照
type DepthSnapshot struct {
	Symbol        string
	BidDepth      float64 // 买盘深度（USDT）
	AskDepth      float64 // 卖盘深度（USDT）
	TotalDepth    float64 // 总深度（USDT）
	BidAskRatio   float64 // 买卖盘比例
	Timestamp     int64
}

// DepthMonitor 订单簿深度监控器
type DepthMonitor struct {
	cfg           *config.Config
	exchange      exchange.IExchange
	depthHistory  map[string][]*DepthSnapshot // 每个交易对的深度历史
	mu            sync.RWMutex
	triggered     bool
	triggeredTime time.Time
	recoveredTime time.Time
	lastMsg       string
}

// NewDepthMonitor 创建深度监控器
func NewDepthMonitor(cfg *config.Config, ex exchange.IExchange) *DepthMonitor {
	return &DepthMonitor{
		cfg:          cfg,
		exchange:     ex,
		depthHistory: make(map[string][]*DepthSnapshot),
	}
}

// Start 启动深度监控
func (d *DepthMonitor) Start(ctx context.Context) {
	if !d.cfg.RiskControl.DepthMonitor.Enabled {
		logger.Info("⚠️ 订单簿深度监控未启用")
		return
	}

	logger.Info("🛡️ 启动订单簿深度监控 (检查间隔: %d秒, 监控档位: %d, 下降阈值: %.1f%%, 最小深度: %.0f USDT)",
		d.cfg.RiskControl.DepthMonitor.CheckInterval,
		d.cfg.RiskControl.DepthMonitor.DepthLevels,
		d.cfg.RiskControl.DepthMonitor.DropThreshold*100,
		d.cfg.RiskControl.DepthMonitor.MinDepthUSDT)

	// 获取当前交易对（从配置中获取）
	symbols := d.getMonitorSymbols()

	// 启动监控协程
	go d.monitorLoop(ctx, symbols)
}

// getMonitorSymbols 获取需要监控的交易对
func (d *DepthMonitor) getMonitorSymbols() []string {
	// 优先使用风控配置的监控币种
	if len(d.cfg.RiskControl.MonitorSymbols) > 0 {
		return d.cfg.RiskControl.MonitorSymbols
	}

	// 如果没有配置，使用交易配置中的交易对
	if len(d.cfg.Trading.Symbols) > 0 {
		symbols := make([]string, 0, len(d.cfg.Trading.Symbols))
		for _, sc := range d.cfg.Trading.Symbols {
			if sc.IsEnabled() {
				symbols = append(symbols, sc.Symbol)
			}
		}
		return symbols
	}

	// 最后使用旧配置
	if d.cfg.Trading.Symbol != "" {
		return []string{d.cfg.Trading.Symbol}
	}

	return []string{}
}

// monitorLoop 监控循环
func (d *DepthMonitor) monitorLoop(ctx context.Context, symbols []string) {
	if len(symbols) == 0 {
		logger.Warn("⚠️ 深度监控：未找到需要监控的交易对")
		return
	}

	checkInterval := time.Duration(d.cfg.RiskControl.DepthMonitor.CheckInterval) * time.Second
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	// 初始化历史数据（先获取一次）
	for _, symbol := range symbols {
		d.checkDepth(ctx, symbol)
	}

	for {
		select {
		case <-ctx.Done():
			logger.Info("⏹️ 深度监控已停止")
			return
		case <-ticker.C:
			// 检查所有交易对的深度
			for _, symbol := range symbols {
				d.checkDepth(ctx, symbol)
			}
		}
	}
}

// checkDepth 检查单个交易对的深度
func (d *DepthMonitor) checkDepth(ctx context.Context, symbol string) {
	// 获取订单簿
	orderBook, err := d.exchange.GetOrderBook(ctx, symbol, d.cfg.RiskControl.DepthMonitor.DepthLevels)
	if err != nil {
		logger.Warn("⚠️ [深度监控] 获取 %s 订单簿失败: %v", symbol, err)
		return
	}

	// 计算当前深度指标
	snapshot := d.calculateDepthMetrics(symbol, orderBook)

	// 更新历史记录
	d.mu.Lock()
	history := d.depthHistory[symbol]
	if len(history) >= 20 {
		// 只保留最近20个快照
		history = history[len(history)-19:]
	}
	history = append(history, snapshot)
	d.depthHistory[symbol] = history

	// 检查是否需要触发风控
	shouldTrigger := d.shouldTriggerDepthRisk(symbol, snapshot, history)
	currentTriggered := d.triggered
	d.mu.Unlock()

	// 触发或恢复风控
	if shouldTrigger && !currentTriggered {
		d.triggerDepthRisk(symbol, snapshot)
	} else if !shouldTrigger && currentTriggered {
		d.recoverFromDepthRisk(symbol, snapshot)
	}
}

// calculateDepthMetrics 计算深度指标
func (d *DepthMonitor) calculateDepthMetrics(symbol string, orderBook *exchange.OrderBook) *DepthSnapshot {
	// 计算买盘深度（前N档的总金额）
	bidDepth := 0.0
	for i, bid := range orderBook.Bids {
		if i >= d.cfg.RiskControl.DepthMonitor.DepthLevels {
			break
		}
		bidDepth += bid.Price * bid.Quantity
	}

	// 计算卖盘深度（前N档的总金额）
	askDepth := 0.0
	for i, ask := range orderBook.Asks {
		if i >= d.cfg.RiskControl.DepthMonitor.DepthLevels {
			break
		}
		askDepth += ask.Price * ask.Quantity
	}

	totalDepth := bidDepth + askDepth
	bidAskRatio := 0.0
	if askDepth > 0 {
		bidAskRatio = bidDepth / askDepth
	}

	return &DepthSnapshot{
		Symbol:      symbol,
		BidDepth:    bidDepth,
		AskDepth:    askDepth,
		TotalDepth:  totalDepth,
		BidAskRatio: bidAskRatio,
		Timestamp:   orderBook.Timestamp,
	}
}

// shouldTriggerDepthRisk 判断是否应该触发深度风控
func (d *DepthMonitor) shouldTriggerDepthRisk(symbol string, current *DepthSnapshot, history []*DepthSnapshot) bool {
	if len(history) < 2 {
		// 数据不足，不触发
		return false
	}

	// 计算平均深度（使用历史数据）
	avgDepth := 0.0
	count := 0
	// 使用最近10个快照计算平均值（排除当前）
	for i := len(history) - 2; i >= 0 && count < 10; i-- {
		avgDepth += history[i].TotalDepth
		count++
	}
	if count == 0 {
		return false
	}
	avgDepth /= float64(count)

	// 检查1：深度下降超过阈值
	if avgDepth > 0 {
		depthDropRatio := (avgDepth - current.TotalDepth) / avgDepth
		if depthDropRatio >= d.cfg.RiskControl.DepthMonitor.DropThreshold {
			logger.Warn("🚨 [深度监控] %s 深度下降 %.1f%% (当前: %.0f USDT, 平均: %.0f USDT)",
				symbol, depthDropRatio*100, current.TotalDepth, avgDepth)
			return true
		}
	}

	// 检查2：绝对深度低于最小值
	if current.TotalDepth < d.cfg.RiskControl.DepthMonitor.MinDepthUSDT {
		logger.Warn("🚨 [深度监控] %s 深度过低: %.0f USDT (阈值: %.0f USDT)",
			symbol, current.TotalDepth, d.cfg.RiskControl.DepthMonitor.MinDepthUSDT)
		return true
	}

	return false
}

// triggerDepthRisk 触发深度风控
func (d *DepthMonitor) triggerDepthRisk(symbol string, snapshot *DepthSnapshot) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.triggered {
		return // 已经触发，避免重复
	}

	d.triggered = true
	d.triggeredTime = time.Now()
	d.lastMsg = fmt.Sprintf("深度风控触发: %s 深度 %.0f USDT", symbol, snapshot.TotalDepth)

	logger.Warn("🚨🚨🚨 [深度监控] 触发深度风控！交易对: %s, 当前深度: %.0f USDT (买盘: %.0f, 卖盘: %.0f)",
		symbol, snapshot.TotalDepth, snapshot.BidDepth, snapshot.AskDepth)
}

// recoverFromDepthRisk 从深度风控中恢复
func (d *DepthMonitor) recoverFromDepthRisk(symbol string, snapshot *DepthSnapshot) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.triggered {
		return // 未触发，无需恢复
	}

	// 检查是否达到恢复阈值
	history := d.depthHistory[symbol]
	if len(history) < 2 {
		return
	}

	// 计算平均深度
	avgDepth := 0.0
	count := 0
	for i := len(history) - 2; i >= 0 && count < 10; i-- {
		avgDepth += history[i].TotalDepth
		count++
	}
	if count == 0 {
		return
	}
	avgDepth /= float64(count)

	// 恢复条件：深度恢复到平均值的恢复阈值以上
	if avgDepth > 0 {
		recoveryRatio := snapshot.TotalDepth / avgDepth
		if recoveryRatio >= d.cfg.RiskControl.DepthMonitor.RecoveryThreshold {
			d.triggered = false
			d.recoveredTime = time.Now()
			d.lastMsg = fmt.Sprintf("深度已恢复: %s 深度 %.0f USDT", symbol, snapshot.TotalDepth)

			logger.Info("✅ [深度监控] 深度已恢复，解除风控限制。交易对: %s, 当前深度: %.0f USDT (恢复率: %.1f%%)",
				symbol, snapshot.TotalDepth, recoveryRatio*100)
		}
	}
}

// IsTriggered 返回是否触发深度风控
func (d *DepthMonitor) IsTriggered() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.triggered
}

// GetTriggeredTime 获取触发时间
func (d *DepthMonitor) GetTriggeredTime() time.Time {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.triggeredTime
}

// GetRecoveredTime 获取恢复时间
func (d *DepthMonitor) GetRecoveredTime() time.Time {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.recoveredTime
}

// GetLastMsg 获取最后一条消息
func (d *DepthMonitor) GetLastMsg() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.lastMsg
}
