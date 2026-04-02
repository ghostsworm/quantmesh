package optimizer

import (
	"math"
	"sort"

	"quantmesh/backtest"
	"quantmesh/exchange"
)

const (
	minTrainBars = 50
	minValBars   = 10
)

// DefaultFeeSlippage 返回回測用費率與滑點（與 ParamsFromSpace 默認一致）
func DefaultFeeSlippage(cfg OptimConfig) (feeRate, slippage float64) {
	feeRate = cfg.FeeRate
	if feeRate <= 0 {
		feeRate = 0.0004
	}
	slippage = cfg.SlippageRatio
	if slippage <= 0 {
		slippage = 0.0003
	}
	return feeRate, slippage
}

// SplitCandlesForValidation 按 Timestamp 排序後，將末尾 ratio 比例的 K 線劃為驗證集。
func SplitCandlesForValidation(candles []*exchange.Candle, ratio float64) (train, val []*exchange.Candle, holdOut bool, err error) {
	if ratio <= 0 {
		return candles, nil, false, nil
	}
	n := len(candles)
	if n < minTrainBars+minValBars {
		return nil, nil, false, errValidationNotEnoughBars
	}
	sorted := make([]*exchange.Candle, len(candles))
	copy(sorted, candles)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Timestamp < sorted[j].Timestamp })
	valCount := int(float64(n) * ratio)
	if valCount < minValBars {
		valCount = minValBars
	}
	if n-valCount < minTrainBars {
		return nil, nil, false, errValidationNotEnoughBars
	}
	split := n - valCount
	return sorted[:split], sorted[split:], true, nil
}

// EvalParamSet 單次評估。啟用 hold-out 時 Score 與 Metrics 為驗證集；TrainScore/TrainMetrics 為訓練集。
// 贝叶斯/遗传的 GP 與適應度應使用 TrainScore，最終最優參數按 Score（驗證）選取。
func EvalParamSet(symbol string, train, val []*exchange.Candle, holdOut bool, p backtest.GridBacktestParams, lambda, initialCapital float64) ParamResult {
	resTrain, err := BacktestRunner(symbol, train, p, initialCapital)
	if err != nil {
		return ParamResult{Params: p, Score: math.Inf(-1), TrainScore: math.Inf(-1)}
	}
	trainMet := resTrain.Metrics
	trainScore := CalculateScore(trainMet, lambda)
	if !holdOut || len(val) == 0 {
		return ParamResult{
			Params:       p,
			Score:        trainScore,
			TrainScore:   trainScore,
			Metrics:      trainMet,
			TrainMetrics: trainMet,
		}
	}
	resVal, err := BacktestRunner(symbol, val, p, initialCapital)
	if err != nil {
		return ParamResult{Params: p, Score: math.Inf(-1), TrainScore: trainScore, TrainMetrics: trainMet}
	}
	valMet := resVal.Metrics
	valScore := CalculateScore(valMet, lambda)
	return ParamResult{
		Params:            p,
		Score:             valScore,
		TrainScore:        trainScore,
		Metrics:           valMet,
		TrainMetrics:      trainMet,
		ValidationMetrics: valMet,
	}
}

// PickBestParamResult 按 Score（樣本外時為驗證集得分）選取最優。
func PickBestParamResult(results []ParamResult) (best ParamResult, ok bool) {
	bestScore := math.Inf(-1)
	for _, r := range results {
		if math.IsInf(r.Score, -1) {
			continue
		}
		if r.Score > bestScore {
			bestScore = r.Score
			best = r
			ok = true
		}
	}
	return best, ok
}
