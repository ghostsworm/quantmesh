package backtest

import (
	"fmt"
	"math"
	"time"

	"quantmesh/exchange"
)

type HedgePairEquityPoint struct {
	Timestamp   int64   `json:"timestamp"`
	TotalEquity float64 `json:"total_equity"`
	LongEquity  float64 `json:"long_equity"`
	ShortEquity float64 `json:"short_equity"`
}

type HedgePairResult struct {
	StartTime         time.Time               `json:"start_time"`
	EndTime           time.Time               `json:"end_time"`
	InitialCapital    float64                 `json:"initial_capital"`
	FinalEquity       float64                 `json:"final_equity"`
	TotalReturnPct    float64                 `json:"total_return_pct"`
	MaxDrawdownPct    float64                 `json:"max_drawdown_pct"`
	RebalanceCount    int                     `json:"rebalance_count"`
	AlignedPoints     int                     `json:"aligned_points"`
	LongSymbol        string                  `json:"long_symbol"`
	ShortSymbol       string                  `json:"short_symbol"`
	EquityCurve       []HedgePairEquityPoint  `json:"equity_curve"`
}

func RunHedgePairTask(task *BacktestTask, legA, legB []*exchange.Candle) (*HedgePairResult, error) {
	if task == nil {
		return nil, fmt.Errorf("task is nil")
	}
	alignedA, alignedB := alignCandlesByTimestamp(legA, legB)
	if len(alignedA) < 2 || len(alignedB) < 2 {
		return nil, fmt.Errorf("not enough aligned candles for hedge backtest")
	}

	hedgeRatio := getFloat(task.Params, "hedge_ratio", 1.0)
	if hedgeRatio <= 0 {
		hedgeRatio = 1.0
	}
	rebalanceThreshold := getFloat(task.Params, "rebalance_threshold", 0.15)
	if rebalanceThreshold <= 0 {
		rebalanceThreshold = 0.15
	}
	rebalanceInterval := getInt(task.Params, "rebalance_interval", 24)
	if rebalanceInterval <= 0 {
		rebalanceInterval = 24
	}

	initialCapital := task.TotalCapital
	if initialCapital <= 0 {
		return nil, fmt.Errorf("total capital must be positive")
	}

	longWeight := 1.0 / (1.0 + hedgeRatio)
	shortWeight := hedgeRatio / (1.0 + hedgeRatio)
	longEquity := initialCapital * longWeight
	shortEquity := initialCapital * shortWeight
	peakEquity := initialCapital
	maxDrawdownPct := 0.0
	rebalanceCount := 0
	lastRebalanceIdx := 0

	equityCurve := make([]HedgePairEquityPoint, 0, len(alignedA))
	equityCurve = append(equityCurve, HedgePairEquityPoint{
		Timestamp:   alignedA[0].Timestamp,
		TotalEquity: initialCapital,
		LongEquity:  longEquity,
		ShortEquity: shortEquity,
	})

	for i := 1; i < len(alignedA); i++ {
		prevA := alignedA[i-1].Close
		prevB := alignedB[i-1].Close
		curA := alignedA[i].Close
		curB := alignedB[i].Close
		if prevA <= 0 || prevB <= 0 || curA <= 0 || curB <= 0 {
			continue
		}

		retA := (curA - prevA) / prevA
		retB := (curB - prevB) / prevB

		// Leg A default long, leg B default short (true pair in simplified model).
		longEquity *= (1 + retA)
		shortEquity *= (1 - retB)
		if longEquity < 0 {
			longEquity = 0
		}
		if shortEquity < 0 {
			shortEquity = 0
		}

		totalEquity := longEquity + shortEquity
		if totalEquity > peakEquity {
			peakEquity = totalEquity
		}
		if peakEquity > 0 {
			dd := (peakEquity - totalEquity) / peakEquity * 100
			if dd > maxDrawdownPct {
				maxDrawdownPct = dd
			}
		}

		if shouldRebalance(longEquity, shortEquity, hedgeRatio, rebalanceThreshold, i-lastRebalanceIdx >= rebalanceInterval) {
			total := longEquity + shortEquity
			longEquity = total * longWeight
			shortEquity = total * shortWeight
			lastRebalanceIdx = i
			rebalanceCount++
		}

		equityCurve = append(equityCurve, HedgePairEquityPoint{
			Timestamp:   alignedA[i].Timestamp,
			TotalEquity: longEquity + shortEquity,
			LongEquity:  longEquity,
			ShortEquity: shortEquity,
		})
	}

	if len(equityCurve) == 0 {
		return nil, fmt.Errorf("no valid equity points generated")
	}
	finalEquity := equityCurve[len(equityCurve)-1].TotalEquity
	return &HedgePairResult{
		StartTime:      time.UnixMilli(equityCurve[0].Timestamp),
		EndTime:        time.UnixMilli(equityCurve[len(equityCurve)-1].Timestamp),
		InitialCapital: initialCapital,
		FinalEquity:    finalEquity,
		TotalReturnPct: (finalEquity - initialCapital) / initialCapital * 100,
		MaxDrawdownPct: maxDrawdownPct,
		RebalanceCount: rebalanceCount,
		AlignedPoints:  len(equityCurve),
		LongSymbol:     firstNonEmpty(task.Symbol, getString(task.Params, "leg_a_symbol", "")),
		ShortSymbol:    getString(task.Params, "leg_b_symbol", ""),
		EquityCurve:    equityCurve,
	}, nil
}

func shouldRebalance(longEquity, shortEquity, hedgeRatio, threshold float64, byInterval bool) bool {
	if byInterval {
		return true
	}
	if longEquity <= 0 || shortEquity <= 0 {
		return false
	}
	targetLongToShort := 1.0 / hedgeRatio
	currentLongToShort := longEquity / shortEquity
	deviation := math.Abs(currentLongToShort-targetLongToShort) / targetLongToShort
	return deviation >= threshold
}

func alignCandlesByTimestamp(a, b []*exchange.Candle) ([]*exchange.Candle, []*exchange.Candle) {
	if len(a) == 0 || len(b) == 0 {
		return nil, nil
	}
	bMap := make(map[int64]*exchange.Candle, len(b))
	for _, candle := range b {
		if candle == nil {
			continue
		}
		bMap[candle.Timestamp] = candle
	}
	outA := make([]*exchange.Candle, 0, len(a))
	outB := make([]*exchange.Candle, 0, len(a))
	for _, candleA := range a {
		if candleA == nil {
			continue
		}
		if candleB, ok := bMap[candleA.Timestamp]; ok && candleB != nil {
			outA = append(outA, candleA)
			outB = append(outB, candleB)
		}
	}
	return outA, outB
}

func getString(params map[string]interface{}, key, def string) string {
	if params == nil {
		return def
	}
	v, ok := params[key]
	if !ok {
		return def
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return def
	}
	return s
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
