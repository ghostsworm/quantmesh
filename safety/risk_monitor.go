package safety

import (
	"context"
	"fmt"
	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/logger"
	"quantmesh/metrics"
	"quantmesh/monitor"
	"quantmesh/storage"
	"strings"
	"sync"
	"time"
)

// SymbolData 單個币种的K線數據缓存
type SymbolData struct {
	candles []*exchange.Candle
	mu      sync.RWMutex
}

// RiskMonitor 主动安全风控監視器
type RiskMonitor struct {
	cfg              *config.Config
	exchange         exchange.IExchange
	storage          storage.Storage
	newsMonitor      *monitor.NewsMonitor
	symbolDataMap    map[string]*SymbolData
	lastHealthStatus map[string]bool // 缓存每個币种的上一次健康状態
	mu               sync.RWMutex
	triggered        bool
	triggeredTime    time.Time
	recoveredTime    time.Time
	lastMsg          string
	now              func() time.Time // 可注入時鐘（測試用），默認 time.Now
}

// params 當前配置下的檢測參數（每次讀取，兼容熱更新）
func (r *RiskMonitor) params() detectorParams {
	return detectorParamsFromConfig(r.cfg)
}

// statsForRecord 為檢查記錄計算價格偏離與量比
func (r *RiskMonitor) statsForRecord(record *storage.RiskCheckRecord, useLatest bool) {
	symbolData, exists := r.symbolDataMap[record.Symbol]
	if !exists {
		return
	}
	symbolData.mu.RLock()
	candles := symbolData.candles
	symbolData.mu.RUnlock()
	if st, why := computeKlineStats(candles, r.cfg.RiskControl.AverageWindow, useLatest); why == "" {
		record.PriceDeviation = st.priceDevPct
		record.VolumeRatio = st.volRatio
	}
}

// NewRiskMonitor 創建风控監視器
func NewRiskMonitor(cfg *config.Config, ex exchange.IExchange) *RiskMonitor {
	symbolDataMap := make(map[string]*SymbolData)
	for _, symbol := range cfg.RiskControl.MonitorSymbols {
		symbolDataMap[symbol] = &SymbolData{
			candles: make([]*exchange.Candle, 0, cfg.RiskControl.AverageWindow+1),
		}
	}

	return &RiskMonitor{
		cfg:              cfg,
		exchange:         ex,
		symbolDataMap:    symbolDataMap,
		lastHealthStatus: make(map[string]bool),
		now:              time.Now,
	}
}

// SetStorage 設置存儲服務（用於保存检查历史）
func (r *RiskMonitor) SetStorage(storage storage.Storage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.storage = storage
}

// SetNewsMonitor 設置新聞監控器（用於新聞驅動的风控）
func (r *RiskMonitor) SetNewsMonitor(nm *monitor.NewsMonitor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.newsMonitor = nm
}

// Start 啟动監控
func (r *RiskMonitor) Start(ctx context.Context) {
	if !r.cfg.RiskControl.Enabled {
		logger.Info("⚠️ 主动安全风控未啟用")
		return
	}

	logger.Info("🛡️ 啟动主动安全风控監控 (周期: %s, 倍數: %.1f, 窗口: %d)",
		r.cfg.RiskControl.Interval, r.cfg.RiskControl.VolumeMultiplier, r.cfg.RiskControl.AverageWindow)
	p := r.params()
	logger.Info("🛡️ 監控币种: %v (恢複阈值: %d/%d, 集體異動需 %d 個, 最小跌幅 %.2f%%, 波動率倍數 %.1f, 恢復容忍 %.2f%%)",
		r.cfg.RiskControl.MonitorSymbols, p.resolved.RecoveryThreshold, len(r.cfg.RiskControl.MonitorSymbols),
		p.resolved.MinPanicSymbols, p.resolved.MinPriceDropPct, p.resolved.VolatilityMultiplier, p.resolved.RecoveryMaxDropPct)

	// 預加載历史K線數據
	logger.Info("📊 正在加載历史K線數據...")
	for _, symbol := range r.cfg.RiskControl.MonitorSymbols {
		candles, err := r.exchange.GetHistoricalKlines(ctx, symbol, r.cfg.RiskControl.Interval, r.cfg.RiskControl.AverageWindow+1)
		if err != nil {
			logger.Warn("⚠️ 加載 %s 历史K線失败: %v", symbol, err)
			continue
		}

		if len(candles) > 0 {
			markFormingHistorical(candles, r.cfg.RiskControl.Interval, r.now())
			r.mu.Lock()
			symbolData, exists := r.symbolDataMap[symbol]
			r.mu.Unlock()

			if exists {
				symbolData.mu.Lock()
				symbolData.candles = candles
				symbolData.mu.Unlock()
				logger.Info("✅ %s: 已加載 %d 根历史K線", symbol, len(candles))
			}
		}
	}
	logger.Info("✅ 历史K線數據加載完成，风控系统已就绪")

	// 啟動K線流
	if err := r.exchange.StartKlineStream(ctx, r.cfg.RiskControl.MonitorSymbols, r.cfg.RiskControl.Interval, r.onCandleUpdate); err != nil {
		logger.Error("❌ 啟動K線流失败: %v", err)
		return
	}

	// 啟动定期报告协程（每60秒）
	go r.reportLoop(ctx)
}

// onCandleUpdate K線更新回呼（實時检测）
func (r *RiskMonitor) onCandleUpdate(candle *exchange.Candle) {
	if candle == nil {
		logger.Warn("⚠️ 收到空K線數據")
		return
	}
	c := candle

	// 更新缓存
	r.mu.RLock()
	symbolData, exists := r.symbolDataMap[c.Symbol]
	r.mu.RUnlock()

	if !exists {
		logger.Warn("⚠️ 收到未監控的币种K線: %s", c.Symbol)
		return
	}

	symbolData.mu.Lock()
	// 按開盤時間去重合併（未完結→完結替換同一根、亂序舊數據忽略）並裁剪到窗口大小；
	// 在副本上修改，已被讀者取走的舊切片不被原地改寫
	cloned := make([]*exchange.Candle, len(symbolData.candles), len(symbolData.candles)+1)
	copy(cloned, symbolData.candles)
	symbolData.candles = upsertCandle(cloned, c, r.cfg.RiskControl.AverageWindow)
	currentCount := len(symbolData.candles)
	symbolData.mu.Unlock()

	// 只在完結K線時打印日志，避免日志過多
	if c.IsClosed {
		logger.Debug("📈 [K線收集] %s: 價格=%.4f, 成交量=%.0f, 完結=%v, 已缓存%d根",
			c.Symbol, c.Close, c.Volume, c.IsClosed, currentCount)
	}

	// 實時检测（使用最新數據，包括未完結的K線）
	r.checkMarket()
}

// checkMarket 執行市场检查（實時，無日志）
func (r *RiskMonitor) checkMarket() {
	checkTime := time.Now()
	var checkRecords []*storage.RiskCheckRecord
	exchangeName := ""
	if r.exchange != nil {
		exchangeName = r.exchange.GetName()
	}
	botID := r.cfg.Trading.BotID
	marketType := r.cfg.Trading.MarketType

	// 先检查當前状態（不持有鎖）
	r.mu.RLock()
	triggered := r.triggered
	r.mu.RUnlock()

	if triggered {
		// 已触发状態：检查是否可以解除
		canRecover, details := r.checkRecovery()

		// 收集恢複检查結果
		for _, symbol := range r.cfg.RiskControl.MonitorSymbols {
			isRecovered, reason := r.checkSymbolRecovery(symbol)
			record := &storage.RiskCheckRecord{
				CheckTime:  checkTime,
				BotID:      botID,
				Exchange:   exchangeName,
				MarketType: marketType,
				Symbol:     symbol,
				IsHealthy:  isRecovered,
				Reason:     reason,
			}
			// 恢復判定只用完結 K 線，記錄口徑保持一致
			r.statsForRecord(record, false)
			checkRecords = append(checkRecords, record)
		}

		r.mu.Lock()
		if canRecover {
			// 统计恢複的币种數量
			recoveredCount := 0
			for _, detail := range details {
				if !strings.Contains(detail, "未恢複") {
					recoveredCount++
				}
			}
			logger.Info("✅ 市场风險信号消失，解除风控限制。(%d/%d 币种已恢複正常，达到恢複阈值 %d)",
				recoveredCount, len(r.cfg.RiskControl.MonitorSymbols), r.params().resolved.RecoveryThreshold)
			logger.Info("详情: %s", strings.Join(details, ", "))
			r.triggered = false
			r.recoveredTime = time.Now()
			r.lastMsg = "已恢複正常"
		} else {
			r.lastMsg = fmt.Sprintf("风控中，等待恢複: %s", strings.Join(details, ","))
		}
		r.mu.Unlock()
	} else {
		// 未触发状態：检查是否需要触发
		panicCount := 0
		details := []string{}

		// 新聞驅動的风控检查
		if r.newsMonitor != nil && r.cfg.NewsMonitor.Enabled {
			if newsReason := r.checkNewsRisk(); newsReason != "" {
				panicCount = len(r.cfg.RiskControl.MonitorSymbols) // 等效於全部异常，直接触发
				details = append(details, newsReason)
			}
		}

		// 若新闻未触发，再检查 K 線
		if panicCount == 0 {
			for _, symbol := range r.cfg.RiskControl.MonitorSymbols {
				isPanic, reason := r.checkSymbol(symbol)

				// 收集检查結果
				record := &storage.RiskCheckRecord{
					CheckTime:  checkTime,
					BotID:      botID,
					Exchange:   exchangeName,
					MarketType: marketType,
					Symbol:     symbol,
					IsHealthy:  !isPanic,
					Reason:     reason,
				}
				r.statsForRecord(record, true)
				checkRecords = append(checkRecords, record)

				if isPanic {
					panicCount++
					details = append(details, fmt.Sprintf("%s(%s)", symbol, reason))
				}
			}
		}

		// 異常交易對數達到 min_panic_symbols（默認全部；多交易對時至少 2 個）或新闻触发時才触发
		r.mu.Lock()
		pm := metrics.GetPrometheusMetrics()
		if panicCount > 0 && panicCount >= r.params().resolved.MinPanicSymbols {
			logger.Warn("🚨🚨🚨 触发主动安全风控！市场出現集体异动！🚨🚨🚨")
			logger.Warn("详情: %s", strings.Join(details, ", "))
			r.triggered = true
			r.triggeredTime = time.Now()
			r.lastMsg = fmt.Sprintf("触发风控: %d/%d 币种异常 (%s)", panicCount, len(r.cfg.RiskControl.MonitorSymbols), strings.Join(details, ","))

			// 記錄风控触发指標
			for _, symbol := range r.cfg.RiskControl.MonitorSymbols {
				pm.SetRiskControlStatus(r.exchange.GetName(), symbol, true)
				pm.RecordRiskControlTrigger(r.exchange.GetName(), symbol, "market_anomaly")
			}
		} else {
			r.lastMsg = "監控正常"
			// 記錄风控正常状態
			for _, symbol := range r.cfg.RiskControl.MonitorSymbols {
				pm.SetRiskControlStatus(r.exchange.GetName(), symbol, false)
			}
		}
		r.mu.Unlock()
	}

	// 异步保存检查結果（只在状態变化時保存，减少數據量）
	if r.storage != nil && len(checkRecords) > 0 {
		go func() {
			r.mu.RLock()
			lastStatus := make(map[string]bool)
			for k, v := range r.lastHealthStatus {
				lastStatus[k] = v
			}
			r.mu.RUnlock()

			var recordsToSave []*storage.RiskCheckRecord
			for _, record := range checkRecords {
				// 检查状態是否发生变化
				lastHealthy, exists := lastStatus[record.Symbol]
				if !exists || lastHealthy != record.IsHealthy {
					// 状態发生变化，需要保存
					recordsToSave = append(recordsToSave, record)
					// 更新缓存
					r.mu.Lock()
					r.lastHealthStatus[record.Symbol] = record.IsHealthy
					r.mu.Unlock()
				}
			}

			// 只在状態变化時保存
			for _, record := range recordsToSave {
				if err := r.storage.SaveRiskCheck(record); err != nil {
					logger.Debug("保存风控检查記錄失败: %v", err)
				}
			}
		}()
	}
}

// checkNewsRisk 检查新聞驅動的风控，若应触发则返回原因，否则返回空字符串
// 遍历監控币种，任一币种對应资產的分析触发即返回
func (r *RiskMonitor) checkNewsRisk() string {
	stopProb := r.cfg.NewsMonitor.RiskThresholds.StopTradingProbability
	if stopProb <= 0 {
		stopProb = 0.7
	}
	for _, symbol := range r.cfg.RiskControl.MonitorSymbols {
		assessment := r.newsMonitor.GetRiskAssessmentBySymbol(symbol)
		if assessment == nil {
			continue
		}
		if assessment.Recommendation == "stop_trading" {
			return fmt.Sprintf("Gemini分析建议：%s 停止交易", symbol)
		}
		for _, pred := range assessment.PricePredictions {
			if pred.Timeframe != "2h" {
				continue
			}
			for _, s := range pred.Scenarios {
				if s.Direction == "down" && s.ChangePercent <= -5 && s.Probability >= stopProb {
					return fmt.Sprintf("新闻預测 %s：2h内跌5%%概率%.0f%%", symbol, s.Probability*100)
				}
			}
		}
	}
	return ""
}

// checkRecovery 检查是否可以解除风控（價格回到均線上方 + 成交量恢複正常）
func (r *RiskMonitor) checkRecovery() (bool, []string) {
	recoveredCount := 0
	details := []string{}

	for _, symbol := range r.cfg.RiskControl.MonitorSymbols {
		isRecovered, reason := r.checkSymbolRecovery(symbol)
		if isRecovered {
			recoveredCount++
			details = append(details, fmt.Sprintf("%s(%s)", symbol, reason))
		} else {
			details = append(details, fmt.Sprintf("%s(未恢複:%s)", symbol, reason))
		}
	}

	// 达到恢複阈值即可解除风控。阈值按監控數夾緊到 [1, n]：
	// 單交易對 runtime 沿用全局 recovery_threshold=3 時舊代碼永遠 1<3 無法恢復（測試網試跑實錘）
	threshold := r.params().resolved.RecoveryThreshold
	return recoveredCount >= threshold, details
}

// checkSymbolRecovery 检查單個币种是否恢複（價格>均價 且 成交量<均值×倍數）
// 解除风控必須使用完結的K線數據
func (r *RiskMonitor) checkSymbolRecovery(symbol string) (bool, string) {
	symbolData, exists := r.symbolDataMap[symbol]
	if !exists {
		return false, "無數據"
	}

	symbolData.mu.RLock()
	candles := symbolData.candles
	symbolData.mu.RUnlock()

	return r.params().evaluateRecovery(candles)
}

// checkSymbol 检查單個币种（基於移动平均線）
// 触发风控可以使用最新K線數據（包括未完結的K線），以便及時检测到异常
func (r *RiskMonitor) checkSymbol(symbol string) (bool, string) {
	r.mu.RLock()
	symbolData, exists := r.symbolDataMap[symbol]
	r.mu.RUnlock()

	if !exists {
		return false, ""
	}

	symbolData.mu.RLock()
	candles := symbolData.candles
	symbolData.mu.RUnlock()

	// 触发条件：跌破均線且跌幅 ≥ max(min_price_drop_pct[×單交易對係數], k×收益率標準差)，
	// 且量比 > volume_multiplier，且最新 K 線未過期
	return r.params().evaluatePanic(candles, r.now())
}

// IsTriggered 返回是否触发风控
func (r *RiskMonitor) IsTriggered() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.triggered
}

// GetTriggeredTime 獲取触发時间
func (r *RiskMonitor) GetTriggeredTime() time.Time {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.triggeredTime
}

// GetRecoveredTime 獲取恢複時间
func (r *RiskMonitor) GetRecoveredTime() time.Time {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.recoveredTime
}

// GetLastMsg 獲取最后一条风控消息（含触发原因）
func (r *RiskMonitor) GetLastMsg() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lastMsg
}

// GetKlineRiskScore 獲取 K 線異常風險評分 0-100（供複合風控因子使用）
func (r *RiskMonitor) GetKlineRiskScore() (score float64, reason string) {
	r.mu.RLock()
	triggered := r.triggered
	lastMsg := r.lastMsg
	r.mu.RUnlock()

	if triggered {
		return 100, lastMsg
	}
	var maxScore float64
	for _, symbol := range r.cfg.RiskControl.MonitorSymbols {
		s, _ := r.klineRiskScoreForSymbol(symbol)
		if s > maxScore {
			maxScore = s
		}
	}
	if maxScore > 100 {
		maxScore = 100
	}
	if maxScore <= 0 {
		return 0, "正常"
	}
	return maxScore, "K線偏離/放量"
}

func (r *RiskMonitor) klineRiskScoreForSymbol(symbol string) (score float64, reason string) {
	r.mu.RLock()
	symbolData, exists := r.symbolDataMap[symbol]
	r.mu.RUnlock()
	if !exists {
		return 0, ""
	}
	symbolData.mu.RLock()
	candles := symbolData.candles
	candleCount := len(candles)
	symbolData.mu.RUnlock()
	if candleCount < r.cfg.RiskControl.AverageWindow+1 {
		return 0, ""
	}
	currentCandle := candles[candleCount-1]
	currentPrice := currentCandle.Close
	var totalPrice, totalVol float64
	var validCount int
	window := r.cfg.RiskControl.AverageWindow
	for i := candleCount - 2; i >= 0 && validCount < window; i-- {
		if candles[i].IsClosed {
			totalPrice += candles[i].Close
			totalVol += candles[i].Volume
			validCount++
		}
	}
	if validCount < window {
		return 0, ""
	}
	avgPrice := totalPrice / float64(validCount)
	avgVol := totalVol / float64(validCount)
	volRatio := currentCandle.Volume / avgVol
	mult := r.cfg.RiskControl.VolumeMultiplier
	if mult <= 0 {
		mult = 3.0
	}
	score = 0
	if avgPrice > 0 && currentPrice < avgPrice {
		score += 50 * (1 - currentPrice/avgPrice)
	}
	if avgVol > 0 && volRatio > 0 {
		ratio := volRatio / mult
		if ratio > 1 {
			ratio = 1
		}
		score += 50 * ratio
	}
	if score > 100 {
		score = 100
	}
	return score, ""
}

// GetMonitorSymbols 獲取監控币种列表
func (r *RiskMonitor) GetMonitorSymbols() []string {
	return r.cfg.RiskControl.MonitorSymbols
}

// GetSymbolData 獲取币种數據（返回最新K線和统计信息）
func (r *RiskMonitor) GetSymbolData(symbol string) interface{} {
	r.mu.RLock()
	symbolData, exists := r.symbolDataMap[symbol]
	r.mu.RUnlock()

	if !exists {
		return nil
	}

	symbolData.mu.RLock()
	defer symbolData.mu.RUnlock()

	if len(symbolData.candles) == 0 {
		return nil
	}

	// 獲取最新K線
	latestCandle := symbolData.candles[len(symbolData.candles)-1]

	// 计算平均價格和平均成交量
	var totalPrice, totalVolume float64
	var count int
	window := r.cfg.RiskControl.AverageWindow

	for i := len(symbolData.candles) - 1; i >= 0 && count < window; i-- {
		if symbolData.candles[i].IsClosed {
			totalPrice += symbolData.candles[i].Close
			totalVolume += symbolData.candles[i].Volume
			count++
		}
	}

	avgPrice := 0.0
	avgVolume := 0.0
	if count > 0 {
		avgPrice = totalPrice / float64(count)
		avgVolume = totalVolume / float64(count)
	}

	// 返回結構化數據
	return &struct {
		CurrentPrice  float64
		AveragePrice  float64
		CurrentVolume float64
		AverageVolume float64
		LastUpdate    time.Time
	}{
		CurrentPrice:  latestCandle.Close,
		AveragePrice:  avgPrice,
		CurrentVolume: latestCandle.Volume,
		AverageVolume: avgVolume,
		LastUpdate:    time.Now(), // 使用當前時间
	}
}

// reportLoop 定期报告状態（每60秒）
func (r *RiskMonitor) reportLoop(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.reportStatus()
		}
	}
}

// reportStatus 报告状態
func (r *RiskMonitor) reportStatus() {
	r.mu.RLock()
	triggered := r.triggered
	r.mu.RUnlock()

	if triggered {
		logger.Warn("⚠️ [风控監测] 當前市場交易出現异动,触发主动安全风控,停止交易!")
	} else {
		logger.Info("🛡️ [风控監测] 市场环境正常。")
	}

	// 打印各币种的移动平均線數值
	r.printMovingAverages(triggered)
}

// printMovingAverages 打印各币种的移动平均線數值
func (r *RiskMonitor) printMovingAverages(inRiskControl bool) {
	logger.Info("📊 [移动平均線監测] 當前各币种數據:")

	// 检查K線數據是否過期
	hasStaleData := false

	for _, symbol := range r.cfg.RiskControl.MonitorSymbols {
		r.mu.RLock()
		symbolData, exists := r.symbolDataMap[symbol]
		r.mu.RUnlock()

		if !exists {
			logger.Info("  %s: 無數據", symbol)
			continue
		}

		symbolData.mu.RLock()
		candles := symbolData.candles
		candleCount := len(candles)
		symbolData.mu.RUnlock()

		if candleCount < r.cfg.RiskControl.AverageWindow+1 {
			logger.Info("  %s: 數據不足 (當前%d根, 需要%d根)", symbol, candleCount, r.cfg.RiskControl.AverageWindow+1)
			continue
		}

		var currentCandle *exchange.Candle
		var currentPrice float64
		var currentVol float64

		// 根據是否在风控中，选擇不同的K線
		if inRiskControl {
			// 风控中：使用最新的完結K線（與恢複判断逻辑一致）
			for i := candleCount - 1; i >= 0; i-- {
				if candles[i].IsClosed {
					currentCandle = candles[i]
					currentPrice = currentCandle.Close
					currentVol = currentCandle.Volume
					break
				}
			}
			if currentCandle == nil {
				logger.Info("  %s: 無完結K線", symbol)
				continue
			}
		} else {
			// 非风控状態：使用最新K線（包括未完結的）
			currentCandle = candles[candleCount-1]
			currentPrice = currentCandle.Close
			currentVol = currentCandle.Volume
		}

		// 计算移动平均價格和移动平均成交量（只使用完結的K線，排除當前用於判断的K線）
		var totalPrice float64
		var totalVol float64
		var validCount int
		window := r.cfg.RiskControl.AverageWindow

		for i := candleCount - 1; i >= 0 && validCount < window; i-- {
			if candles[i].IsClosed && candles[i] != currentCandle {
				totalPrice += candles[i].Close
				totalVol += candles[i].Volume
				validCount++
			}
		}

		if validCount < window {
			logger.Info("  %s: 完結K線不足 (當前%d根, 需要%d根)", symbol, validCount, window)
			continue
		}

		avgPrice := totalPrice / float64(validCount)
		avgVol := totalVol / float64(validCount)

		// 计算偏离度
		priceDeviation := (currentPrice - avgPrice) / avgPrice * 100
		volRatio := currentVol / avgVol

		// 判断各项指標状態（與 evaluateRecovery / evaluatePanic 同口徑）
		p := r.params()
		priceAboveMA := p.priceRecovered(klineStats{priceDevPct: priceDeviation})
		volNormal := volRatio < p.volumeMultiplier || (inRiskControl && priceDeviation >= 0)

		// 根據是否在风控中，显示不同的状態信息
		klineStatus := "完結"
		if !currentCandle.IsClosed {
			klineStatus = "未完結"
		}

		// 计算K線時间距离現在的時间差（帮助調試）
		// 自动判断時间戳單位：毫秒(>10000000000) 或 秒
		var klineTime time.Time
		if currentCandle.Timestamp > 10000000000 {
			// 毫秒時间戳（币安、Bitget）
			klineTime = time.Unix(currentCandle.Timestamp/1000, 0)
		} else {
			// 秒级時间戳（Gate.io）
			klineTime = time.Unix(currentCandle.Timestamp, 0)
		}

		klineAge := time.Since(klineTime)
		klineAgeStr := fmt.Sprintf("%.0f秒前", klineAge.Seconds())
		if klineAge > time.Minute {
			klineAgeStr = fmt.Sprintf("%.0f分前", klineAge.Minutes())
		}

		var statusMsg string
		if inRiskControl {
			// 风控中，显示详细的异常/恢複状態
			if priceAboveMA && volNormal {
				statusMsg = fmt.Sprintf("正常[%s|%s]: 當前價=%.4f, 均價=%.4f (偏离%.2f%%), 現價在均價上方已恢複, 當前量=%.0f, 均量=%.0f (倍數×%.2f) 成交量已恢複",
					klineStatus, klineAgeStr, currentPrice, avgPrice, priceDeviation, currentVol, avgVol, volRatio)
			} else {
				// 异常状態，說明未恢複的原因
				var priceStatus, volStatus string
				if priceAboveMA {
					priceStatus = "價格已回到均線附近"
				} else {
					priceStatus = fmt.Sprintf("價格低於均線超過%.2f%%未恢複", p.resolved.RecoveryMaxDropPct)
				}
				if volNormal {
					volStatus = "成交量已恢複"
				} else {
					volStatus = "成交量未恢複"
				}
				statusMsg = fmt.Sprintf("异常[%s|%s]: 當前價=%.4f, 均價=%.4f (偏离%.2f%%), %s, 當前量=%.0f, 均量=%.0f (倍數×%.2f) %s",
					klineStatus, klineAgeStr, currentPrice, avgPrice, priceDeviation, priceStatus, currentVol, avgVol, volRatio, volStatus)
			}
		} else {
			// 非风控状態：與觸發判定同口徑（跌幅門檻 + 量比 + 過期檢查）
			if isPanic, _ := p.evaluatePanic(candles, r.now()); isPanic {
				// 同時满足两個条件才是真正的异常
				statusMsg = fmt.Sprintf("🚨异常[%s|%s]: 當前價=%.4f, 均價=%.4f (偏离%.2f%%), 當前量=%.0f, 均量=%.0f (倍數×%.2f)",
					klineStatus, klineAgeStr, currentPrice, avgPrice, priceDeviation, currentVol, avgVol, volRatio)
			} else {
				// 否则显示正常（添加K線時间信息）
				statusMsg = fmt.Sprintf("✅正常[%s|%s]: 當前價=%.4f, 均價=%.4f (偏离%.2f%%), 當前量=%.0f, 均量=%.0f (倍數×%.2f)",
					klineStatus, klineAgeStr, currentPrice, avgPrice, priceDeviation, currentVol, avgVol, volRatio)
			}
		}

		logger.Info("  %s %s", symbol, statusMsg)

		// 检查數據是否過期（超過2分钟）
		if klineAge > 2*time.Minute {
			hasStaleData = true
		}
	}

	// 如果有過期數據，发出警告
	if hasStaleData {
		logger.Warn("⚠️ [K線數據] 部分币种的K線數據超過2分钟未更新，可能K線流断开或重连中")
	}
}

// Stop 停止監控
func (r *RiskMonitor) Stop() {
	if r.exchange != nil {
		r.exchange.StopKlineStream()
	}
}
