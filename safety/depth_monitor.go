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
	Symbol      string
	BidDepth    float64 // 買盘深度（USDT）
	AskDepth    float64 // 賣盘深度（USDT）
	TotalDepth  float64 // 總深度（USDT）
	BidAskRatio float64 // 買賣盘比例
	Timestamp   int64
}

const (
	// depthHistoryMaxSnapshots 每個交易對保留的最大深度快照數
	depthHistoryMaxSnapshots = 20
	// depthBaselineSamples 計算基線平均深度時使用的最近快照數（不含當前）
	depthBaselineSamples = 10
)

// depthTriggerState 單個交易對的深度風控觸發狀態
type depthTriggerState struct {
	triggeredTime time.Time
	baselineDepth float64 // 觸發時凍結的基線平均深度，恢復判斷只與它比較
	msg           string
}

// DepthMonitor 订單簿深度監控器
type DepthMonitor struct {
	cfg              *config.Config
	exchange         exchange.IExchange
	depthHistory     map[string][]*DepthSnapshot   // 每個交易對的深度历史
	triggeredSymbols map[string]*depthTriggerState // 按交易對記錄的觸發狀態
	mu               sync.RWMutex
	triggeredTime    time.Time
	recoveredTime    time.Time
	lastMsg          string
}

// NewDepthMonitor 創建深度監控器
func NewDepthMonitor(cfg *config.Config, ex exchange.IExchange) *DepthMonitor {
	return &DepthMonitor{
		cfg:              cfg,
		exchange:         ex,
		depthHistory:     make(map[string][]*DepthSnapshot),
		triggeredSymbols: make(map[string]*depthTriggerState),
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
	if ctx == nil {
		ctx = context.Background()
	}
	if len(symbols) == 0 {
		logger.Warn("⚠️ 深度監控：未找到需要監控的交易對")
		return
	}

	checkInterval := time.Duration(d.cfg.RiskControl.DepthMonitor.CheckInterval) * time.Second
	if checkInterval <= 0 {
		checkInterval = 5 * time.Second
		logger.Warn("⚠️ 深度監控檢查間隔配置無效，使用默认值 %v", checkInterval)
	}
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
	d.processSnapshot(symbol, snapshot)
}

// processSnapshot 按交易對處理一個深度快照：已觸發則只判斷恢復，否則更新歷史並判斷是否觸發
func (d *DepthMonitor) processSnapshot(symbol string, snapshot *DepthSnapshot) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if state, ok := d.triggeredSymbols[symbol]; ok {
		// 風控期間凍結基線：崩盤後的快照不寫入歷史，避免拉低均值導致自動解除
		d.recoverFromDepthRiskLocked(symbol, snapshot, state)
		return
	}

	history := d.depthHistory[symbol]
	if len(history) >= depthHistoryMaxSnapshots {
		// 只保留最近 depthHistoryMaxSnapshots 個快照
		history = history[len(history)-(depthHistoryMaxSnapshots-1):]
	}
	candidate := append(history, snapshot)

	if d.shouldTriggerDepthRisk(symbol, snapshot, candidate) {
		baseline, _ := baselineAverageDepth(candidate)
		// 觸發快照不計入歷史，保持基線為觸發前的正常深度
		d.depthHistory[symbol] = history
		d.triggerDepthRiskLocked(symbol, snapshot, baseline)
		return
	}
	d.depthHistory[symbol] = candidate
}

// baselineAverageDepth 計算基線平均深度：history 最後一個元素視為當前快照並排除，
// 取其之前最近 depthBaselineSamples 個快照的平均值。
func baselineAverageDepth(history []*DepthSnapshot) (float64, bool) {
	sum := 0.0
	count := 0
	for i := len(history) - 2; i >= 0 && count < depthBaselineSamples; i-- {
		sum += history[i].TotalDepth
		count++
	}
	if count == 0 {
		return 0, false
	}
	return sum / float64(count), true
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

	// 计算平均深度（使用最近 depthBaselineSamples 個快照，排除當前）
	avgDepth, ok := baselineAverageDepth(history)
	if !ok {
		return false
	}

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

// triggerDepthRiskLocked 触发指定交易對的深度风控（調用方須持有 d.mu 寫鎖）
func (d *DepthMonitor) triggerDepthRiskLocked(symbol string, snapshot *DepthSnapshot, baseline float64) {
	if _, ok := d.triggeredSymbols[symbol]; ok {
		return // 已經触发，避免重複
	}

	now := time.Now()
	msg := fmt.Sprintf("深度风控触发: %s 深度 %.0f USDT", symbol, snapshot.TotalDepth)
	d.triggeredSymbols[symbol] = &depthTriggerState{
		triggeredTime: now,
		baselineDepth: baseline,
		msg:           msg,
	}
	d.triggeredTime = now
	d.lastMsg = msg

	logger.Warn("🚨🚨🚨 [深度監控] 触发深度风控！交易對: %s, 當前深度: %.0f USDT (買盘: %.0f, 賣盘: %.0f, 凍結基線: %.0f)",
		symbol, snapshot.TotalDepth, snapshot.BidDepth, snapshot.AskDepth, baseline)
}

// recoverFromDepthRiskLocked 判斷指定交易對是否從深度风控中恢複（調用方須持有 d.mu 寫鎖）。
// 只與觸發時凍結的基線比較，且當前深度須不低於最小深度。
func (d *DepthMonitor) recoverFromDepthRiskLocked(symbol string, snapshot *DepthSnapshot, state *depthTriggerState) {
	cfg := d.cfg.RiskControl.DepthMonitor
	if snapshot.TotalDepth < cfg.MinDepthUSDT {
		return
	}

	recoveryRatio := 1.0
	if state.baselineDepth > 0 {
		recoveryRatio = snapshot.TotalDepth / state.baselineDepth
		if recoveryRatio < cfg.RecoveryThreshold {
			return
		}
	}

	delete(d.triggeredSymbols, symbol)
	d.recoveredTime = time.Now()
	d.lastMsg = fmt.Sprintf("深度已恢複: %s 深度 %.0f USDT", symbol, snapshot.TotalDepth)
	// 其他交易對仍在觸發中時，保留其觸發信息供上層展示
	for _, other := range d.triggeredSymbols {
		d.lastMsg = other.msg
		break
	}

	logger.Info("✅ [深度監控] 深度已恢複，解除该交易對风控限制。交易對: %s, 當前深度: %.0f USDT (恢複率: %.1f%%, 仍觸發交易對數: %d)",
		symbol, snapshot.TotalDepth, recoveryRatio*100, len(d.triggeredSymbols))
}

// IsTriggered 返回是否有任一交易對触发深度风控
func (d *DepthMonitor) IsTriggered() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.triggeredSymbols) > 0
}

// IsSymbolTriggered 返回指定交易對是否触发深度风控
func (d *DepthMonitor) IsSymbolTriggered(symbol string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	_, ok := d.triggeredSymbols[symbol]
	return ok
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

// GetDepthRiskScore 獲取深度風險評分 0-100（供複合風控因子使用）
func (d *DepthMonitor) GetDepthRiskScore(symbol string) (score float64, reason string) {
	d.mu.RLock()
	history := d.depthHistory[symbol]
	_, triggered := d.triggeredSymbols[symbol]
	dropThreshold := d.cfg.RiskControl.DepthMonitor.DropThreshold
	minDepth := d.cfg.RiskControl.DepthMonitor.MinDepthUSDT
	d.mu.RUnlock()

	if triggered {
		return 100, "深度風控已觸發"
	}
	if len(history) < 2 {
		return 0, "數據不足"
	}
	current := history[len(history)-1].TotalDepth
	avgDepth, ok := baselineAverageDepth(history)
	if !ok {
		return 0, "數據不足"
	}
	if current < minDepth {
		return 100, "深度過低"
	}
	if avgDepth <= 0 {
		return 0, "正常"
	}
	depthDropRatio := (avgDepth - current) / avgDepth
	if depthDropRatio <= 0 {
		return 0, "正常"
	}
	if dropThreshold <= 0 {
		dropThreshold = 0.5
	}
	score = depthDropRatio / dropThreshold * 100
	if score > 100 {
		score = 100
	}
	reason = "深度下降"
	return score, reason
}
