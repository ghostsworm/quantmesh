package backtest

import (
	"fmt"
	"time"

	"quantmesh/exchange"
)

// RiskSimulatorConfig 风控模拟器配置（与实盘 RiskMonitor 对齐）
type RiskSimulatorConfig struct {
	VolumeMultiplier   float64 `json:"volume_multiplier"`    // 成交量异常倍数（預設3.0）
	AverageWindow      int     `json:"average_window"`       // 移动平均窗口（預設20）
	// 深度风控参数
	MinDepthUSDT       float64 `json:"min_depth_usdt"`       // 最小深度阈值(USDT)，預設 10000
	DepthDropThreshold float64 `json:"depth_drop_threshold"` // 深度下降阈值，預設 0.5
	DepthWindow        int     `json:"depth_window"`         // 深度移动平均窗口，預設 10
}

// RiskIntervention 风控介入記錄
type RiskIntervention struct {
	Timestamp   int64  `json:"timestamp"`
	TimeStr     string `json:"time_str"`
	Reason      string `json:"reason"`
	RiskType    string `json:"risk_type"` // volume_spike, price_drop
	Duration    int    `json:"duration"`  // 持续K線数
	SkippedBuys int    `json:"skipped_buys"`
}

// RiskSimulator 回测风控模拟器
type RiskSimulator struct {
	cfg            RiskSimulatorConfig
	triggered      bool
	interventions  []RiskIntervention
	currentInterv  *RiskIntervention               // 當前未结束的介入
	skippedBuys    int                             // 當前介入期间跳过的买入数
	barCount       int                             // 當前介入持续的K線数
	depthSnapshots []*DepthSnapshotForBacktest     // 深度快照數據（与 candles 一一对应）
}

// NewRiskSimulator 創建风控模拟器
func NewRiskSimulator(cfg *RiskSimulatorConfig) *RiskSimulator {
	c := RiskSimulatorConfig{
		VolumeMultiplier:   3.0,
		AverageWindow:      20,
		MinDepthUSDT:       10000.0,
		DepthDropThreshold: 0.5,
		DepthWindow:        10,
	}
	if cfg != nil {
		if cfg.VolumeMultiplier > 0 {
			c.VolumeMultiplier = cfg.VolumeMultiplier
		}
		if cfg.AverageWindow > 0 {
			c.AverageWindow = cfg.AverageWindow
		}
		if cfg.MinDepthUSDT > 0 {
			c.MinDepthUSDT = cfg.MinDepthUSDT
		}
		if cfg.DepthDropThreshold > 0 {
			c.DepthDropThreshold = cfg.DepthDropThreshold
		}
		if cfg.DepthWindow > 0 {
			c.DepthWindow = cfg.DepthWindow
		}
	}
	return &RiskSimulator{
		cfg:           c,
		interventions: make([]RiskIntervention, 0),
	}
}

// NewRiskSimulatorWithDepth 創建带深度數據的风控模拟器
func NewRiskSimulatorWithDepth(cfg *RiskSimulatorConfig, depthSnapshots []*DepthSnapshotForBacktest) *RiskSimulator {
	rs := NewRiskSimulator(cfg)
	rs.depthSnapshots = depthSnapshots
	return rs
}

// DefaultRiskSimulatorConfig 返回預設风控配置
func DefaultRiskSimulatorConfig() RiskSimulatorConfig {
	return RiskSimulatorConfig{
		VolumeMultiplier:   3.0,
		AverageWindow:      20,
		MinDepthUSDT:       10000.0,
		DepthDropThreshold: 0.5,
		DepthWindow:        10,
	}
}

// Check 檢查當前K線是否触发/恢复风控，返回是否应跳过买入
// candles: 从开始到當前的所有K線，candleIndex: 當前K線在 candles 中的索引
func (r *RiskSimulator) Check(candles []*exchange.Candle, candleIndex int) (skipBuy bool, reason string) {
	if candles == nil || candleIndex < 0 || candleIndex >= len(candles) {
		return false, ""
	}
	if len(candles) < r.cfg.AverageWindow+1 || candleIndex < r.cfg.AverageWindow {
		return false, ""
	}

	currentCandle := candles[candleIndex]
	// 計算移动平均價格和成交量（使用前 AverageWindow 根完結K線）
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

	// 传统风控：價格低于均价 且 成交量放大
	tradPriceVolRisk := priceBelowMA && volSpike
	tradPriceVolRecover := currentCandle.Close > avgPrice && currentCandle.Volume < avgVol*r.cfg.VolumeMultiplier

	// 深度风控檢查
	depthRisk := false
	depthRecover := true
	var depthReason string
	
	if r.depthSnapshots != nil && candleIndex < len(r.depthSnapshots) && r.cfg.MinDepthUSDT > 0 {
		currentDepth := r.depthSnapshots[candleIndex]
		
		// 檢查绝对深度不足
		if currentDepth.TotalDepth < r.cfg.MinDepthUSDT {
			depthRisk = true
			depthReason = fmt.Sprintf("深度不足: %.0f USDT < %.0f", currentDepth.TotalDepth, r.cfg.MinDepthUSDT)
		} else {
			// 檢查深度相对下降
			if candleIndex >= r.cfg.DepthWindow {
				var avgDepth float64
				validDepthCount := 0
				for i := candleIndex - 1; i >= 0 && validDepthCount < r.cfg.DepthWindow; i-- {
					if i < len(r.depthSnapshots) {
						avgDepth += r.depthSnapshots[i].TotalDepth
						validDepthCount++
					}
				}
				
				if validDepthCount > 0 {
					avgDepth /= float64(validDepthCount)
					if avgDepth > 0 {
						depthDropRatio := (avgDepth - currentDepth.TotalDepth) / avgDepth
						if depthDropRatio >= r.cfg.DepthDropThreshold {
							depthRisk = true
							depthReason = fmt.Sprintf("深度骤降: %.1f%% (當前: %.0f, 平均: %.0f)", 
								depthDropRatio*100, currentDepth.TotalDepth, avgDepth)
						}
					}
				}
			}
		}
		
		// 深度恢复条件：深度回到阈值以上
		depthRecover = currentDepth.TotalDepth >= r.cfg.MinDepthUSDT
		if depthRecover && candleIndex >= r.cfg.DepthWindow {
			var avgDepth float64
			validDepthCount := 0
			for i := candleIndex - 1; i >= 0 && validDepthCount < r.cfg.DepthWindow; i-- {
				if i < len(r.depthSnapshots) {
					avgDepth += r.depthSnapshots[i].TotalDepth
					validDepthCount++
				}
			}
			if validDepthCount > 0 {
				avgDepth /= float64(validDepthCount)
				if avgDepth > 0 {
					recoveryRatio := currentDepth.TotalDepth / avgDepth
					depthRecover = recoveryRatio >= 0.7 // 70% 恢复阈值
				}
			}
		}
	}

	// 综合触发条件：传统风控 或 深度风控
	shouldTrigger := tradPriceVolRisk || depthRisk
	
	// 综合恢复条件：传统恢复 且 深度恢复
	shouldRecover := tradPriceVolRecover && depthRecover

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

	// 未触发狀態
	if shouldTrigger {
		r.triggered = true
		r.barCount = 1
		r.skippedBuys = 0
		
		// 确定触发原因和類型
		var riskType string
		if depthRisk {
			reason = depthReason
			riskType = "depth_risk"
		} else {
			priceDeviation := (currentCandle.Close - avgPrice) / avgPrice * 100
			volRatio := currentCandle.Volume / avgVol
			reason = fmt.Sprintf("價格%.2f%%低於均線/量×%.1f", priceDeviation, volRatio)
			riskType = "volume_spike"
		}
		
		r.currentInterv = &RiskIntervention{
			Timestamp: currentCandle.Timestamp,
			Reason:    reason,
			RiskType:  riskType,
		}
		return true, reason
	}
	return false, ""
}

// RecordSkippedBuy 記錄跳过一次买入（在跳过买入时调用）
func (r *RiskSimulator) RecordSkippedBuy() {
	if r.triggered && r.currentInterv != nil {
		r.skippedBuys++
	}
}

// GetInterventions 獲取所有介入記錄
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
