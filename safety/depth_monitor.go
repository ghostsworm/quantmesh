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
	BidDepth      float64 // 買盘深度（USDT）
	AskDepth      float64 // 賣盘深度（USDT）
	TotalDepth    float64 // 總深度（USDT）
	BidAskRatio   float64 // 買賣盘比例
	Timestamp     int64
}

// DepthMonitor 订單簿深度監控器
type DepthMonitor struct {
	cfg           *config.Config
	exchange      exchange.IExchange
	depthHistory  map[string][]*DepthSnapshot // 每個交易對的深度历史
	mu            sync.RWMutex
	triggered     bool
	triggeredTime time.Time
	recoveredTime time.Time
	lastMsg       string
}

// NewDepthMonitor 創建深度監控器
func NewDepthMonitor(cfg *config.Config, ex exchange.IExchange) *DepthMonitor {
	return &DepthMonitor{
		cfg:          cfg,
		exchange:     ex,
		depthHistory: make(map[string][]*DepthSnapshot),
	}
}

// Start 啟动深度監控
func (d *DepthMonitor) Start(ctx context.Context) {
	if !d.cfg.RiskControl.DepthMonitor.Enabled {
		logger.Info("⚠️ 订單簿深度監控未啟用")
		return
	}

	logger.Info("🛡️ 啟動訂單簿深度監控 (检查间隔: %d秒, 監控檔位: %d, 下降阈值: %.1f%%, 最小深度: %.0f USDT)",
		d.cfg.RiskControl.DepthMonitor.CheckInterval,
		d.cfg.RiskControl.DepthMonitor.DepthLevels,
		d.cfg.RiskControl.DepthMonitor.DropThreshold*100,
		d.cfg.RiskControl.DepthMonitor.MinDepthUSDT)

	// 獲取當前交易對（從配置中獲取）
	symbols := d.getMonitorSymbols()

	// 啟动監控协程
	go d.monitorLoop(ctx, symbols)
}

// getMonitorSymbols 獲取需要監控的交易對
func (d *DepthMonitor) getMonitorSymbols() []string {
	// 优先使用風控配置的監控币种
	if len(d.cfg.RiskControl.MonitorSymbols) > 0 {
		return d.cfg.RiskControl.MonitorSymbols
	}

	// 如果没有配置，使用交易配置中的交易對
	if len(d.cfg.Trading.Symbols) > 0 {
		symbols := make([]string, 0, len(d.cfg.Trading.Symbols))
		for _, sc := range d.cfg.Trading.Symbols {
			if sc.IsEnabled() {
				symbols = append(symbols, sc.Symbol)
			}
		}
		return symbols
	}

	// 最后使用舊配置
	if d.cfg.Trading.Symbol != "" {
		return []string{d.cfg.Trading.Symbol}
	}

	return []string{}
}

// monitorLoop 監控循环
func (d *DepthMonitor) monitorLoop(ctx context.Context, symbols []string) {
	if len(symbols) == 0 {
		logger.Warn("⚠️ 深度監控：未找到需要監控的交易對")
		return
	}

	checkInterval := time.Duration(d.cfg.RiskControl.DepthMonitor.CheckInterval) * time.Second
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	// 初始化历史數據（先獲取一次）
	for _, symbol := range symbols {
		d.checkDepth(ctx, symbol)
	}

	for {
		select {
		case <-ctx.Done():
			logger.Info("⏹️ 深度監控已停止")
			return
		case <-ticker.C:
			// 检查所有交易對的深度
			for _, symbol := range symbols {
				d.checkDepth(ctx, symbol)
			}
		}
	}
}

// checkDepth 检查單個交易對的深度
func (d *DepthMonitor) checkDepth(ctx context.Context, symbol string) {
	// 獲取訂單簿
	orderBook, err := d.exchange.GetOrderBook(ctx, symbol, d.cfg.RiskControl.DepthMonitor.DepthLevels)
	if err != nil {
		logger.Warn("⚠️ [深度監控] 獲取 %s 订單簿失败: %v", symbol, err)
		return
	}

	// 计算當前深度指標
	snapshot := d.calculateDepthMetrics(symbol, orderBook)

	// 更新历史記錄
	d.mu.Lock()
	history := d.depthHistory[symbol]
	if len(history) >= 20 {
		// 只保留最近20個快照
		history = history[len(history)-19:]
	}
	history = append(history, snapshot)
	d.depthHistory[symbol] = history

	// 检查是否需要触发风控
	shouldTrigger := d.shouldTriggerDepthRisk(symbol, snapshot, history)
	currentTriggered := d.triggered
	d.mu.Unlock()

	// 触发或恢複风控
	if shouldTrigger && !currentTriggered {
		d.triggerDepthRisk(symbol, snapshot)
	} else if !shouldTrigger && currentTriggered {
		d.recoverFromDepthRisk(symbol, snapshot)
	}
}

// calculateDepthMetrics 计算深度指標
func (d *DepthMonitor) calculateDepthMetrics(symbol string, orderBook *exchange.OrderBook) *DepthSnapshot {
	// 计算買盘深度（前N檔的總金額）
	bidDepth := 0.0
	for i, bid := range orderBook.Bids {
		if i >= d.cfg.RiskControl.DepthMonitor.DepthLevels {
			break
		}
		bidDepth += bid.Price * bid.Quantity
	}

	// 计算賣盘深度（前N檔的總金額）
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

// shouldTriggerDepthRisk 判断是否应該触发深度风控
func (d *DepthMonitor) shouldTriggerDepthRisk(symbol string, current *DepthSnapshot, history []*DepthSnapshot) bool {
	if len(history) < 2 {
		// 數據不足，不触发
		return false
	}

	// 计算平均深度（使用历史數據）
	avgDepth := 0.0
	count := 0
	// 使用最近10個快照计算平均值（排除當前）
	for i := len(history) - 2; i >= 0 && count < 10; i-- {
		avgDepth += history[i].TotalDepth
		count++
	}
	if count == 0 {
		return false
	}
	avgDepth /= float64(count)

	// 检查1：深度下降超過阈值
	if avgDepth > 0 {
		depthDropRatio := (avgDepth - current.TotalDepth) / avgDepth
		if depthDropRatio >= d.cfg.RiskControl.DepthMonitor.DropThreshold {
			logger.Warn("🚨 [深度監控] %s 深度下降 %.1f%% (當前: %.0f USDT, 平均: %.0f USDT)",
				symbol, depthDropRatio*100, current.TotalDepth, avgDepth)
			return true
		}
	}

	// 检查2：绝對深度低於最小值
	if current.TotalDepth < d.cfg.RiskControl.DepthMonitor.MinDepthUSDT {
		logger.Warn("🚨 [深度監控] %s 深度過低: %.0f USDT (阈值: %.0f USDT)",
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
		return // 已經触发，避免重複
	}

	d.triggered = true
	d.triggeredTime = time.Now()
	d.lastMsg = fmt.Sprintf("深度风控触发: %s 深度 %.0f USDT", symbol, snapshot.TotalDepth)

	logger.Warn("🚨🚨🚨 [深度監控] 触发深度风控！交易對: %s, 當前深度: %.0f USDT (買盘: %.0f, 賣盘: %.0f)",
		symbol, snapshot.TotalDepth, snapshot.BidDepth, snapshot.AskDepth)
}

// recoverFromDepthRisk 從深度风控中恢複
func (d *DepthMonitor) recoverFromDepthRisk(symbol string, snapshot *DepthSnapshot) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.triggered {
		return // 未触发，無需恢複
	}

	// 检查是否达到恢複阈值
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

	// 恢複条件：深度恢複到平均值的恢複阈值以上
	if avgDepth > 0 {
		recoveryRatio := snapshot.TotalDepth / avgDepth
		if recoveryRatio >= d.cfg.RiskControl.DepthMonitor.RecoveryThreshold {
			d.triggered = false
			d.recoveredTime = time.Now()
			d.lastMsg = fmt.Sprintf("深度已恢複: %s 深度 %.0f USDT", symbol, snapshot.TotalDepth)

			logger.Info("✅ [深度監控] 深度已恢複，解除风控限制。交易對: %s, 當前深度: %.0f USDT (恢複率: %.1f%%)",
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

// GetTriggeredTime 獲取触发時间
func (d *DepthMonitor) GetTriggeredTime() time.Time {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.triggeredTime
}

// GetRecoveredTime 獲取恢複時间
func (d *DepthMonitor) GetRecoveredTime() time.Time {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.recoveredTime
}

// GetLastMsg 獲取最后一条消息
func (d *DepthMonitor) GetLastMsg() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.lastMsg
}
