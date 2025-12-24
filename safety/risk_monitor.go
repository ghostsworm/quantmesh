package safety

import (
	"context"
	"fmt"
	"opensqt/config"
	"opensqt/exchange"
	"opensqt/logger"
	"strings"
	"sync"
	"time"
)

// SymbolData 单个币种的K线数据缓存
type SymbolData struct {
	candles []*exchange.Candle
	mu      sync.RWMutex
}

// RiskMonitor 主动安全风控监视器
type RiskMonitor struct {
	cfg           *config.Config
	exchange      exchange.IExchange
	symbolDataMap map[string]*SymbolData
	mu            sync.RWMutex
	triggered     bool
	lastMsg       string
}

// NewRiskMonitor 创建风控监视器
func NewRiskMonitor(cfg *config.Config, ex exchange.IExchange) *RiskMonitor {
	symbolDataMap := make(map[string]*SymbolData)
	for _, symbol := range cfg.RiskControl.MonitorSymbols {
		symbolDataMap[symbol] = &SymbolData{
			candles: make([]*exchange.Candle, 0, cfg.RiskControl.AverageWindow+1),
		}
	}

	return &RiskMonitor{
		cfg:           cfg,
		exchange:      ex,
		symbolDataMap: symbolDataMap,
	}
}

// Start 启动监控
func (r *RiskMonitor) Start(ctx context.Context) {
	if !r.cfg.RiskControl.Enabled {
		logger.Info("⚠️ 主动安全风控未启用")
		return
	}

	logger.Info("🛡️ 启动主动安全风控监控 (周期: %s, 倍数: %.1f, 窗口: %d)",
		r.cfg.RiskControl.Interval, r.cfg.RiskControl.VolumeMultiplier, r.cfg.RiskControl.AverageWindow)
	logger.Info("🛡️ 监控币种: %v (恢复阈值: %d/%d)", r.cfg.RiskControl.MonitorSymbols,
		r.cfg.RiskControl.RecoveryThreshold, len(r.cfg.RiskControl.MonitorSymbols))

	// 预加载历史K线数据
	logger.Info("📊 正在加载历史K线数据...")
	for _, symbol := range r.cfg.RiskControl.MonitorSymbols {
		candles, err := r.exchange.GetHistoricalKlines(ctx, symbol, r.cfg.RiskControl.Interval, r.cfg.RiskControl.AverageWindow+1)
		if err != nil {
			logger.Warn("⚠️ 加载 %s 历史K线失败: %v", symbol, err)
			continue
		}

		if len(candles) > 0 {
			r.mu.Lock()
			symbolData, exists := r.symbolDataMap[symbol]
			r.mu.Unlock()

			if exists {
				symbolData.mu.Lock()
				symbolData.candles = candles
				symbolData.mu.Unlock()
				logger.Info("✅ %s: 已加载 %d 根历史K线", symbol, len(candles))
			}
		}
	}
	logger.Info("✅ 历史K线数据加载完成，风控系统已就绪")

	// 启动K线流
	if err := r.exchange.StartKlineStream(ctx, r.cfg.RiskControl.MonitorSymbols, r.cfg.RiskControl.Interval, r.onCandleUpdate); err != nil {
		logger.Error("❌ 启动K线流失败: %v", err)
		return
	}

	// 启动定期报告协程（每60秒）
	go r.reportLoop(ctx)
}

// onCandleUpdate K线更新回调（实时检测）
func (r *RiskMonitor) onCandleUpdate(candle *exchange.Candle) {
	if candle == nil {
		logger.Warn("⚠️ 收到空K线数据")
		return
	}
	c := candle

	// 更新缓存
	r.mu.RLock()
	symbolData, exists := r.symbolDataMap[c.Symbol]
	r.mu.RUnlock()

	if !exists {
		logger.Warn("⚠️ 收到未监控的币种K线: %s", c.Symbol)
		return
	}

	symbolData.mu.Lock()

	if c.IsClosed {
		// 完结的K线：追加到列表
		symbolData.candles = append(symbolData.candles, c)

		// 保留足够数量的完结K线（窗口大小）+ 可能的1根未完结K线
		// 只保留最近的完结K线，删除过旧的
		requiredClosedCount := r.cfg.RiskControl.AverageWindow
		closedCount := 0
		for i := len(symbolData.candles) - 1; i >= 0; i-- {
			if symbolData.candles[i].IsClosed {
				closedCount++
			}
		}

		// 如果完结K线超过需要的数量，从前面删除旧的
		if closedCount > requiredClosedCount+1 {
			// 找到需要保留的起始位置（从后往前数requiredClosedCount+1根完结K线）
			keepClosedCount := requiredClosedCount + 1
			foundCount := 0
			startIdx := len(symbolData.candles) - 1
			for i := len(symbolData.candles) - 1; i >= 0; i-- {
				if symbolData.candles[i].IsClosed {
					foundCount++
					if foundCount >= keepClosedCount {
						startIdx = i
						break
					}
				}
			}
			symbolData.candles = symbolData.candles[startIdx:]
		}
	} else {
		// 未完结的K线
		if len(symbolData.candles) > 0 && !symbolData.candles[len(symbolData.candles)-1].IsClosed {
			// 最后一根也是未完结的：更新它
			symbolData.candles[len(symbolData.candles)-1] = c
		} else {
			// 最后一根是完结的或列表为空：追加这个未完结K线
			symbolData.candles = append(symbolData.candles, c)
		}
	}
	currentCount := len(symbolData.candles)
	symbolData.mu.Unlock()

	// 只在完结K线时打印日志，避免日志过多
	if c.IsClosed {
		logger.Debug("📈 [K线收集] %s: 价格=%.4f, 成交量=%.0f, 完结=%v, 已缓存%d根",
			c.Symbol, c.Close, c.Volume, c.IsClosed, currentCount)
	}

	// 实时检测（使用最新数据，包括未完结的K线）
	r.checkMarket()
}

// checkMarket 执行市场检查（实时，无日志）
func (r *RiskMonitor) checkMarket() {
	// 先检查当前状态（不持有锁）
	r.mu.RLock()
	triggered := r.triggered
	r.mu.RUnlock()

	if triggered {
		// 已触发状态：检查是否可以解除
		canRecover, details := r.checkRecovery()

		r.mu.Lock()
		if canRecover {
			// 统计恢复的币种数量
			recoveredCount := 0
			for _, detail := range details {
				if !strings.Contains(detail, "未恢复") {
					recoveredCount++
				}
			}
			logger.Info("✅ 市场风险信号消失，解除风控限制。(%d/%d 币种已恢复正常，达到恢复阈值 %d)",
				recoveredCount, len(r.cfg.RiskControl.MonitorSymbols), r.cfg.RiskControl.RecoveryThreshold)
			logger.Info("详情: %s", strings.Join(details, ", "))
			r.triggered = false
			r.lastMsg = "已恢复正常"
		} else {
			r.lastMsg = fmt.Sprintf("风控中，等待恢复: %s", strings.Join(details, ","))
		}
		r.mu.Unlock()
	} else {
		// 未触发状态：检查是否需要触发
		panicCount := 0
		details := []string{}

		for _, symbol := range r.cfg.RiskControl.MonitorSymbols {
			isPanic, reason := r.checkSymbol(symbol)
			if isPanic {
				panicCount++
				details = append(details, fmt.Sprintf("%s(%s)", symbol, reason))
			}
		}

		// 全部币种都出现异常时才触发
		r.mu.Lock()
		if panicCount > 0 && panicCount >= len(r.cfg.RiskControl.MonitorSymbols) {
			logger.Warn("🚨🚨🚨 触发主动安全风控！市场出现集体异动！🚨🚨🚨")
			logger.Warn("详情: %s", strings.Join(details, ", "))
			r.triggered = true
			r.lastMsg = fmt.Sprintf("触发风控: %d/%d 币种异常 (%s)", panicCount, len(r.cfg.RiskControl.MonitorSymbols), strings.Join(details, ","))
		} else {
			r.lastMsg = "监控正常"
		}
		r.mu.Unlock()
	}
}

// checkRecovery 检查是否可以解除风控（价格回到均线上方 + 成交量恢复正常）
func (r *RiskMonitor) checkRecovery() (bool, []string) {
	recoveredCount := 0
	details := []string{}

	for _, symbol := range r.cfg.RiskControl.MonitorSymbols {
		isRecovered, reason := r.checkSymbolRecovery(symbol)
		if isRecovered {
			recoveredCount++
			details = append(details, fmt.Sprintf("%s(%s)", symbol, reason))
		} else {
			details = append(details, fmt.Sprintf("%s(未恢复:%s)", symbol, reason))
		}
	}

	// 达到恢复阈值即可解除风控
	threshold := r.cfg.RiskControl.RecoveryThreshold
	return recoveredCount >= threshold, details
}

// checkSymbolRecovery 检查单个币种是否恢复（价格>均价 且 成交量<均值×倍数）
// 解除风控必须使用完结的K线数据
func (r *RiskMonitor) checkSymbolRecovery(symbol string) (bool, string) {
	symbolData, exists := r.symbolDataMap[symbol]
	if !exists {
		return false, "无数据"
	}

	symbolData.mu.RLock()
	candles := symbolData.candles
	candleCount := len(candles)
	symbolData.mu.RUnlock()

	if candleCount < r.cfg.RiskControl.AverageWindow+1 {
		return false, "数据不足"
	}

	// 找到最新的完结K线用于判断（如果最后一根是未完结的，使用倒数第二根）
	var currentCandle *exchange.Candle
	var currentPrice float64

	for i := candleCount - 1; i >= 0; i-- {
		if candles[i].IsClosed {
			currentCandle = candles[i]
			currentPrice = currentCandle.Close
			break
		}
	}

	if currentCandle == nil {
		return false, "无完结K线"
	}

	// 计算移动平均价格和移动平均成交量（只使用完结的K线，排除当前用于判断的这根）
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
		return false, fmt.Sprintf("完结K线不足(%d<%d)", validCount, window)
	}

	avgPrice := totalPrice / float64(validCount)
	avgVol := totalVol / float64(validCount)

	// 恢复条件：价格 > 均价 且 成交量 < 均值×倍数（与触发条件对应）
	priceAboveMA := currentPrice > avgPrice
	volNormal := currentCandle.Volume < avgVol*r.cfg.RiskControl.VolumeMultiplier

	if priceAboveMA && volNormal {
		return true, "价格回归均线/量正常"
	}

	// 返回未恢复原因
	if !priceAboveMA {
		return false, fmt.Sprintf("价格%.2f<均价%.2f", currentPrice, avgPrice)
	}
	return false, fmt.Sprintf("量%.0f>均量×%.1f", currentCandle.Volume, r.cfg.RiskControl.VolumeMultiplier)
}

// checkSymbol 检查单个币种（基于移动平均线）
// 触发风控可以使用最新K线数据（包括未完结的K线），以便及时检测到异常
func (r *RiskMonitor) checkSymbol(symbol string) (bool, string) {
	r.mu.RLock()
	symbolData, exists := r.symbolDataMap[symbol]
	r.mu.RUnlock()

	if !exists {
		return false, ""
	}

	symbolData.mu.RLock()
	candles := symbolData.candles
	candleCount := len(candles)
	symbolData.mu.RUnlock()

	if candleCount < r.cfg.RiskControl.AverageWindow+1 {
		return false, ""
	}

	// 最新K线（可以是未完结的，用于实时检测）
	currentCandle := candles[candleCount-1]
	currentPrice := currentCandle.Close

	// 计算移动平均价格和移动平均成交量（使用历史完结的K线）
	var totalPrice float64
	var totalVol float64
	var validCount int
	window := r.cfg.RiskControl.AverageWindow

	// 从倒数第二根K线开始往前计算（排除当前可能未完结的K线）
	for i := candleCount - 2; i >= 0 && validCount < window; i-- {
		if candles[i].IsClosed {
			totalPrice += candles[i].Close
			totalVol += candles[i].Volume
			validCount++
		}
	}

	if validCount < window {
		return false, ""
	}

	avgPrice := totalPrice / float64(validCount)
	avgVol := totalVol / float64(validCount)

	// 计算当前价格偏离均线的百分比
	priceDeviation := (currentPrice - avgPrice) / avgPrice * 100
	volRatio := currentCandle.Volume / avgVol

	// 触发条件：当前价格 < 均价 且 成交量放大（使用最新数据，包括未完结K线）
	if currentPrice < avgPrice && currentCandle.Volume > avgVol*r.cfg.RiskControl.VolumeMultiplier {
		return true, fmt.Sprintf("价格%.2f%%低于均线/量×%.1f", priceDeviation, volRatio)
	}

	return false, ""
}

// IsTriggered 返回是否触发风控
func (r *RiskMonitor) IsTriggered() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.triggered
}

// reportLoop 定期报告状态（每60秒）
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

// reportStatus 报告状态
func (r *RiskMonitor) reportStatus() {
	r.mu.RLock()
	triggered := r.triggered
	r.mu.RUnlock()

	if triggered {
		logger.Warn("⚠️ [风控监测] 当前市场交易出现异动,触发主动安全风控,停止交易!")
	} else {
		logger.Info("🛡️ [风控监测] 市场环境正常。")
	}

	// 打印各币种的移动平均线数值
	r.printMovingAverages(triggered)
}

// printMovingAverages 打印各币种的移动平均线数值
func (r *RiskMonitor) printMovingAverages(inRiskControl bool) {
	logger.Info("📊 [移动平均线监测] 当前各币种数据:")

	// 检查K线数据是否过期
	hasStaleData := false

	for _, symbol := range r.cfg.RiskControl.MonitorSymbols {
		r.mu.RLock()
		symbolData, exists := r.symbolDataMap[symbol]
		r.mu.RUnlock()

		if !exists {
			logger.Info("  %s: 无数据", symbol)
			continue
		}

		symbolData.mu.RLock()
		candles := symbolData.candles
		candleCount := len(candles)
		symbolData.mu.RUnlock()

		if candleCount < r.cfg.RiskControl.AverageWindow+1 {
			logger.Info("  %s: 数据不足 (当前%d根, 需要%d根)", symbol, candleCount, r.cfg.RiskControl.AverageWindow+1)
			continue
		}

		var currentCandle *exchange.Candle
		var currentPrice float64
		var currentVol float64

		// 根据是否在风控中，选择不同的K线
		if inRiskControl {
			// 风控中：使用最新的完结K线（与恢复判断逻辑一致）
			for i := candleCount - 1; i >= 0; i-- {
				if candles[i].IsClosed {
					currentCandle = candles[i]
					currentPrice = currentCandle.Close
					currentVol = currentCandle.Volume
					break
				}
			}
			if currentCandle == nil {
				logger.Info("  %s: 无完结K线", symbol)
				continue
			}
		} else {
			// 非风控状态：使用最新K线（包括未完结的）
			currentCandle = candles[candleCount-1]
			currentPrice = currentCandle.Close
			currentVol = currentCandle.Volume
		}

		// 计算移动平均价格和移动平均成交量（只使用完结的K线，排除当前用于判断的K线）
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
			logger.Info("  %s: 完结K线不足 (当前%d根, 需要%d根)", symbol, validCount, window)
			continue
		}

		avgPrice := totalPrice / float64(validCount)
		avgVol := totalVol / float64(validCount)

		// 计算偏离度
		priceDeviation := (currentPrice - avgPrice) / avgPrice * 100
		volRatio := currentVol / avgVol

		// 判断各项指标状态
		priceAboveMA := currentPrice > avgPrice
		volNormal := currentVol < avgVol*r.cfg.RiskControl.VolumeMultiplier

		// 根据是否在风控中，显示不同的状态信息
		klineStatus := "完结"
		if !currentCandle.IsClosed {
			klineStatus = "未完结"
		}

		// 计算K线时间距离现在的时间差（帮助调试）
		// 自动判断时间戳单位：毫秒(>10000000000) 或 秒
		var klineTime time.Time
		if currentCandle.Timestamp > 10000000000 {
			// 毫秒时间戳（币安、Bitget）
			klineTime = time.Unix(currentCandle.Timestamp/1000, 0)
		} else {
			// 秒级时间戳（Gate.io）
			klineTime = time.Unix(currentCandle.Timestamp, 0)
		}

		klineAge := time.Since(klineTime)
		klineAgeStr := fmt.Sprintf("%.0f秒前", klineAge.Seconds())
		if klineAge > time.Minute {
			klineAgeStr = fmt.Sprintf("%.0f分前", klineAge.Minutes())
		}

		var statusMsg string
		if inRiskControl {
			// 风控中，显示详细的异常/恢复状态
			if priceAboveMA && volNormal {
				statusMsg = fmt.Sprintf("正常[%s|%s]: 当前价=%.4f, 均价=%.4f (偏离%.2f%%), 现价在均价上方已恢复, 当前量=%.0f, 均量=%.0f (倍数×%.2f) 成交量已恢复",
					klineStatus, klineAgeStr, currentPrice, avgPrice, priceDeviation, currentVol, avgVol, volRatio)
			} else {
				// 异常状态，说明未恢复的原因
				var priceStatus, volStatus string
				if priceAboveMA {
					priceStatus = "现价在均价上方已恢复"
				} else {
					priceStatus = "现价在均价下方未恢复"
				}
				if volNormal {
					volStatus = "成交量已恢复"
				} else {
					volStatus = "成交量未恢复"
				}
				statusMsg = fmt.Sprintf("异常[%s|%s]: 当前价=%.4f, 均价=%.4f (偏离%.2f%%), %s, 当前量=%.0f, 均量=%.0f (倍数×%.2f) %s",
					klineStatus, klineAgeStr, currentPrice, avgPrice, priceDeviation, priceStatus, currentVol, avgVol, volRatio, volStatus)
			}
		} else {
			// 非风控状态，判断异常需要同时满足两个条件：价格低于均价 且 成交量超过配置倍数
			isPriceBelow := !priceAboveMA
			isVolHigh := !volNormal

			if isPriceBelow && isVolHigh {
				// 同时满足两个条件才是真正的异常
				statusMsg = fmt.Sprintf("🚨异常[%s|%s]: 当前价=%.4f, 均价=%.4f (偏离%.2f%%), 当前量=%.0f, 均量=%.0f (倍数×%.2f)",
					klineStatus, klineAgeStr, currentPrice, avgPrice, priceDeviation, currentVol, avgVol, volRatio)
			} else {
				// 否则显示正常（添加K线时间信息）
				statusMsg = fmt.Sprintf("✅正常[%s|%s]: 当前价=%.4f, 均价=%.4f (偏离%.2f%%), 当前量=%.0f, 均量=%.0f (倍数×%.2f)",
					klineStatus, klineAgeStr, currentPrice, avgPrice, priceDeviation, currentVol, avgVol, volRatio)
			}
		}

		logger.Info("  %s %s", symbol, statusMsg)

		// 检查数据是否过期（超过2分钟）
		if klineAge > 2*time.Minute {
			hasStaleData = true
		}
	}

	// 如果有过期数据，发出警告
	if hasStaleData {
		logger.Warn("⚠️ [K线数据] 部分币种的K线数据超过2分钟未更新，可能K线流断开或重连中")
	}
}

// Stop 停止监控
func (r *RiskMonitor) Stop() {
	if r.exchange != nil {
		r.exchange.StopKlineStream()
	}
}
