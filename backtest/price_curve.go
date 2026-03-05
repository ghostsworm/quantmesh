package backtest

import (
	"math"
	"sort"

	"quantmesh/exchange"
)

// PriceCurveSummary 回测期间價格曲線摘要：拐点、起止价、最大连续涨跌价差
type PriceCurveSummary struct {
	StartPrice            float64   `json:"start_price"`             // 开始价（首根K線收盘）
	EndPrice              float64   `json:"end_price"`               // 结束价（末根K線收盘）
	Top3Valleys           []float64 `json:"top3_valleys"`            // 期间最重要的 3 个谷底价（由低到高）
	Top3Peaks             []float64 `json:"top3_peaks"`              // 期间最重要的 3 个峰值价（由高到低）
	MaxConsecutiveDecline float64   `json:"max_consecutive_decline"` // 最大连续下跌价差（从某高点到后续低点的最大跌幅）
	MaxConsecutiveRise    float64   `json:"max_consecutive_rise"`    // 最大连续上涨价差（从某低点到后续高点的最大涨幅）
}

const topN = 3

// ComputePriceCurveSummary 从 K 线序列計算價格曲線摘要（拐点、起止价、最大连续涨跌）
func ComputePriceCurveSummary(candles []*exchange.Candle) *PriceCurveSummary {
	if len(candles) == 0 {
		return nil
	}

	out := &PriceCurveSummary{
		StartPrice:            candles[0].Close,
		EndPrice:              candles[len(candles)-1].Close,
		Top3Valleys:           make([]float64, 0, topN),
		Top3Peaks:             make([]float64, 0, topN),
		MaxConsecutiveDecline: 0,
		MaxConsecutiveRise:    0,
	}

	// 1. 拐点：局部谷底与峰值（基于收盘价）
	var valleys, peaks []float64
	for i := 1; i < len(candles)-1; i++ {
		c := candles[i].Close
		prev := candles[i-1].Close
		next := candles[i+1].Close
		if c <= prev && c <= next {
			valleys = append(valleys, c)
		}
		if c >= prev && c >= next {
			peaks = append(peaks, c)
		}
	}
	// 最重要的 3 个谷底：取最低的 3 个（升序）
	sort.Float64s(valleys)
	for i := 0; i < topN && i < len(valleys); i++ {
		out.Top3Valleys = append(out.Top3Valleys, valleys[i])
	}
	// 最重要的 3 个峰值：取最高的 3 个（降序）
	sort.Slice(peaks, func(i, j int) bool { return peaks[i] > peaks[j] })
	for i := 0; i < topN && i < len(peaks); i++ {
		out.Top3Peaks = append(out.Top3Peaks, peaks[i])
	}

	// 2. 最大连续下跌价差：任一连跌段中 (起点价 - 终点价) 的最大值
	// 连续下跌 = 每根 K 线收盘价严格小于前一根
	var runStart float64
	inDecline := false
	for i := 1; i < len(candles); i++ {
		prev, cur := candles[i-1].Close, candles[i].Close
		if cur < prev {
			if !inDecline {
				inDecline = true
				runStart = prev
			}
			drop := runStart - cur
			if drop > out.MaxConsecutiveDecline {
				out.MaxConsecutiveDecline = drop
			}
		} else {
			inDecline = false
		}
	}

	// 3. 最大连续上涨价差：任一连涨段中 (终点价 - 起点价) 的最大值
	inRise := false
	for i := 1; i < len(candles); i++ {
		prev, cur := candles[i-1].Close, candles[i].Close
		if cur > prev {
			if !inRise {
				inRise = true
				runStart = prev
			}
			rise := cur - runStart
			if rise > out.MaxConsecutiveRise {
				out.MaxConsecutiveRise = rise
			}
		} else {
			inRise = false
		}
	}

	// 避免浮点噪音
	out.MaxConsecutiveDecline = math.Round(out.MaxConsecutiveDecline*1e8) / 1e8
	out.MaxConsecutiveRise = math.Round(out.MaxConsecutiveRise*1e8) / 1e8
	return out
}
