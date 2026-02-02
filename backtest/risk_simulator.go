package backtest

import (
	"fmt"
	"time"

	"quantmesh/exchange"
)

// RiskSimulatorConfig 风控模拟器配置（与实盘 RiskMonitor 对齐）
type RiskSimulatorConfig struct {
	VolumeMultiplier float64 `json:"volume_multiplier"` // 成交量异常倍数（默认3.0）
	AverageWindow    int     `json:"average_window"`    // 移动平均窗口（默认20）
}

// RiskIntervention 风控介入记录
type RiskIntervention struct {
	Timestamp   int64  `json:"timestamp"`
	TimeStr     string `json:"time_str"`
	Reason      string `json:"reason"`
	RiskType    string `json:"risk_type"` // volume_spike, price_drop
	Duration    int    `json:"duration"`  // 持续K线数
	SkippedBuys int    `json:"skipped_buys"`
}

// RiskSimulator 回测风控模拟器
type RiskSimulator struct {
	cfg           RiskSimulatorConfig
	triggered     bool
	interventions []RiskIntervention
	currentInterv *RiskIntervention // 当前未结束的介入
	skippedBuys   int               // 当前介入期间跳过的买入数
	barCount      int               // 当前介入持续的K线数
}

// NewRiskSimulator 创建风控模拟器
func NewRiskSimulator(cfg *RiskSimulatorConfig) *RiskSimulator {
	c := RiskSimulatorConfig{VolumeMultiplier: 3.0, AverageWindow: 20}
	if cfg != nil {
		if cfg.VolumeMultiplier > 0 {
			c.VolumeMultiplier = cfg.VolumeMultiplier
		}
		if cfg.AverageWindow > 0 {
			c.AverageWindow = cfg.AverageWindow
		}
	}
	return &RiskSimulator{
		cfg:           c,
		interventions: make([]RiskIntervention, 0),
	}
}

// DefaultRiskSimulatorConfig 返回默认风控配置
func DefaultRiskSimulatorConfig() RiskSimulatorConfig {
	return RiskSimulatorConfig{
		VolumeMultiplier: 3.0,
		AverageWindow:    20,
	}
}

// Check 检查当前K线是否触发/恢复风控，返回是否应跳过买入
// candles: 从开始到当前的所有K线，candleIndex: 当前K线在 candles 中的索引
func (r *RiskSimulator) Check(candles []*exchange.Candle, candleIndex int) (skipBuy bool, reason string) {
	if candles == nil || candleIndex < 0 || candleIndex >= len(candles) {
		return false, ""
	}
	if len(candles) < r.cfg.AverageWindow+1 || candleIndex < r.cfg.AverageWindow {
		return false, ""
	}

	currentCandle := candles[candleIndex]
	// 计算移动平均价格和成交量（使用前 AverageWindow 根完結K線）
	var totalPrice, totalVol float64
	validCount := 0
	for i := candleIndex - 1; i >= 0 && validCount < r.cfg.AverageWindow; i-- {
		totalPrice += candles[i].Close
		totalVol += candles[i].Volume
		validCount++
	}
	if validCount < r.cfg.AverageWindow {
		return false, ""
	}
	avgPrice := totalPrice / float64(validCount)
	avgVol := totalVol / float64(validCount)

	priceBelowMA := currentCandle.Close < avgPrice
	volSpike := currentCandle.Volume > avgVol*r.cfg.VolumeMultiplier

	// 触发条件：价格低于均价 且 成交量放大
	shouldTrigger := priceBelowMA && volSpike

	// 恢复条件：价格高于均价 且 成交量正常
	shouldRecover := currentCandle.Close > avgPrice && currentCandle.Volume < avgVol*r.cfg.VolumeMultiplier

	if r.triggered {
		if shouldRecover {
			// 恢复
			r.triggered = false
			if r.currentInterv != nil {
				r.currentInterv.Duration = r.barCount
				r.currentInterv.SkippedBuys = r.skippedBuys
				r.currentInterv.TimeStr = formatTimestamp(r.currentInterv.Timestamp)
				r.interventions = append(r.interventions, *r.currentInterv)
				r.currentInterv = nil
			}
			r.barCount = 0
			r.skippedBuys = 0
			return false, ""
		}
		// 仍在风控中
		r.barCount++
		if r.currentInterv != nil {
			return true, r.currentInterv.Reason
		}
		return true, "风控中"
	}

	// 未触发状态
	if shouldTrigger {
		r.triggered = true
		r.barCount = 1
		r.skippedBuys = 0
		priceDeviation := (currentCandle.Close - avgPrice) / avgPrice * 100
		volRatio := currentCandle.Volume / avgVol
		reason = fmt.Sprintf("價格%.2f%%低於均線/量×%.1f", priceDeviation, volRatio)
		r.currentInterv = &RiskIntervention{
			Timestamp: currentCandle.Timestamp,
			Reason:    reason,
			RiskType:  "volume_spike",
		}
		return true, reason
	}
	return false, ""
}

// RecordSkippedBuy 记录跳过一次买入（在跳过买入时调用）
func (r *RiskSimulator) RecordSkippedBuy() {
	if r.triggered && r.currentInterv != nil {
		r.skippedBuys++
	}
}

// GetInterventions 获取所有介入记录
func (r *RiskSimulator) GetInterventions() []RiskIntervention {
	// 如果有未结束的介入，也加入
	if r.currentInterv != nil {
		r.currentInterv.Duration = r.barCount
		r.currentInterv.SkippedBuys = r.skippedBuys
		r.currentInterv.TimeStr = formatTimestamp(r.currentInterv.Timestamp)
		r.interventions = append(r.interventions, *r.currentInterv)
		r.currentInterv = nil
	}
	return r.interventions
}

func formatTimestamp(ts int64) string {
	var sec int64
	if ts > 10000000000 {
		sec = ts / 1000
	} else {
		sec = ts
	}
	return time.Unix(sec, 0).Format("2006-01-02 15:04:05")
}
