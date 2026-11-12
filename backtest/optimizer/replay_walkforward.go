package optimizer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	"quantmesh/backtest"
	"quantmesh/backtest/replay"
	"quantmesh/exchange"
	"quantmesh/strategy/regime"
)

const replayWFProtocol = "grid-replay-walk-forward-v1"

func replayDigest(value interface{}) (string, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("replay experiment digest: %w", err)
	}
	hash := sha256.Sum256(b)
	return hex.EncodeToString(hash[:]), nil
}

// freezeReplayCandles makes a canonical, detached OHLCV snapshot. Source fields
// such as an exchange's live update flags cannot influence the fingerprint.
func freezeReplayCandles(candles []*exchange.Candle, interval string) ([]*exchange.Candle, int64, error) {
	if len(candles) < 2 || len(candles) > MaxReplayCandles {
		return nil, 0, fmt.Errorf("replay requires 2..%d candles", MaxReplayCandles)
	}
	d, err := regime.ParseKlineInterval(interval)
	if err != nil {
		return nil, 0, fmt.Errorf("replay interval: %w", err)
	}
	barMs := d.Milliseconds()
	out := make([]*exchange.Candle, len(candles))
	for i, c := range candles {
		if c == nil {
			return nil, 0, errWalkForwardInvalidCandles
		}
		out[i] = &exchange.Candle{Timestamp: c.Timestamp, Open: c.Open, High: c.High, Low: c.Low, Close: c.Close, Volume: c.Volume}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Timestamp < out[j].Timestamp })
	if err := validateWalkForwardCandles(out); err != nil {
		return nil, 0, err
	}
	for i, c := range out {
		if c.Timestamp > math.MaxInt64-barMs || c.Timestamp%barMs != 0 {
			return nil, 0, fmt.Errorf("replay candles must align to UTC %s boundaries: %w", interval, errWalkForwardInvalidCandles)
		}
		if i > 0 && c.Timestamp-out[i-1].Timestamp != barMs {
			return nil, 0, fmt.Errorf("replay interval %s does not match candle cadence", interval)
		}
	}
	return out, barMs, nil
}

func (u *UniversalOptimizer) runReplayWalkForward(ctx context.Context, taskID, symbol, interval string, candles []*exchange.Candle, space UniversalSearchSpace, capital float64, progress func(int, int)) (*UniversalOptimResult, error) {
	started := time.Now()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := ValidateReplaySearchSpace(space); err != nil {
		return nil, err
	}
	if !finiteNumber(capital) || capital <= 0 {
		return nil, fmt.Errorf("replay capital must be finite and positive")
	}
	data, barMs, err := freezeReplayCandles(candles, interval)
	if err != nil {
		return nil, err
	}
	// Deep-copy the entire protocol, including nested feature configuration and
	// settlement events, before the first train run or any progress callback.
	b, err := json.Marshal(space)
	if err != nil {
		return nil, err
	}
	var frozen UniversalSearchSpace
	if err := json.Unmarshal(b, &frozen); err != nil {
		return nil, err
	}
	cfg := *frozen.ReplayWalkForward
	if cfg.StepDays == 0 {
		cfg.StepDays = cfg.TestDays
	}
	base, err := normalizedReplayParams(cfg.BaseParams)
	if err != nil {
		return nil, err
	}
	cfg.BaseParams = base
	wfCfg := WalkForwardConfig{Enabled: true, TrainDays: cfg.TrainDays, TestDays: cfg.TestDays, StepDays: cfg.StepDays}
	trainMs, testMs, _, err := wfCfg.windowsMs()
	if err != nil {
		return nil, err
	}
	if trainMs%barMs != 0 || testMs%barMs != 0 {
		return nil, fmt.Errorf("replay windows must align to complete %s candles", interval)
	}
	span := data[len(data)-1].Timestamp + barMs - data[0].Timestamp
	if trainMs > span || testMs > span-trainMs {
		return nil, errWalkForwardNoFolds
	}
	foldCount := 1 + (span-trainMs-testMs)/testMs
	if foldCount > MaxReplayFolds {
		return nil, fmt.Errorf("replay permits at most %d folds; requested %d", MaxReplayFolds, foldCount)
	}
	combos := u.EnumerateParamCombos(frozen)
	workBars := foldCount * (int64(len(combos)+1)*(trainMs/barMs) + 2*(testMs/barMs))
	if workBars > MaxReplayCandleEvaluations {
		return nil, fmt.Errorf("replay evaluation budget exceeded: %d candle evaluations > %d", workBars, MaxReplayCandleEvaluations)
	}
	windows, err := BuildWalkForwardWindows(data, wfCfg)
	if err != nil {
		return nil, err
	}
	out := &ReplayWalkForwardResult{
		Protocol: replayWFProtocol, Engine: "replay", ScoreRule: "net_return_pct_minus_max_drawdown_pct",
		WarmupPolicy: "closed_candles_inside_each_window_only", WindowPolicy: "independent_flat_start_fixed_capital_mark_to_market",
		Config: cfg, InitialCapital: capital, SearchRanges: frozen.Ranges, DataBars: len(data), DataStart: data[0].Timestamp, DataEnd: data[len(data)-1].Timestamp + barMs, CandleIntervalMs: barMs,
		BaselineParams: base, Folds: make([]ReplayFold, 0, len(windows)),
		Limitations: []string{"synthetic_ohlc_path", "no_historical_order_book", "zero_latency_no_market_impact", "no_historical_predicted_funding", "settlement_funding_cost_only", "cold_start_each_window", "mark_to_market_without_terminal_liquidation", "synthetic_or_historical_replay_is_not_live_profit_evidence"},
	}
	if u.provenance != nil && u.provenance.Source != "" {
		out.DataSource = u.provenance.Source
		out.VerifiedFiles = append([]VerifiedFile(nil), u.provenance.Files...)
		out.FundingFiles = append([]VerifiedFile(nil), u.provenance.FundingFiles...)
		out.Limitations = append(out.Limitations, "binance_vision_um_1m_checksum_verified")
	}
	out.ConfigSHA256, err = replayDigest(cfg)
	if err != nil {
		return nil, err
	}
	out.DataSHA256, err = replayDigest(data)
	if err != nil {
		return nil, err
	}
	out.BaselineSHA256, err = replayDigest(base)
	if err != nil {
		return nil, err
	}
	digest := struct {
		Protocol, Symbol, Interval, Data, Config string
		Capital                                  float64
		Ranges                                   map[string]ParamRange
		Source                                   string         `json:"Source,omitempty"`
		Files                                    []VerifiedFile `json:"Files,omitempty"`
		FundingFiles                             []VerifiedFile `json:"FundingFiles,omitempty"`
	}{Protocol: replayWFProtocol, Symbol: symbol, Interval: interval, Data: out.DataSHA256, Config: out.ConfigSHA256, Capital: capital, Ranges: frozen.Ranges}
	if out.DataSource != "" {
		digest.Source = out.DataSource
		digest.Files = out.VerifiedFiles
		digest.FundingFiles = out.FundingFiles
	}
	out.ExperimentSHA256, err = replayDigest(digest)
	if err != nil {
		return nil, err
	}
	total := len(windows) * (len(combos) + 3)
	completed := 0
	evaluate := func(params map[string]interface{}, cs []*exchange.Candle, start, end int64) (ReplayEvaluation, error) {
		if err := ctx.Err(); err != nil {
			return ReplayEvaluation{}, err
		}
		p, err := cloneReplayParams(params)
		if err != nil {
			return ReplayEvaluation{}, err
		}
		task := &backtest.BacktestTask{ID: taskID, Strategy: "grid", Symbol: symbol, Interval: interval, StartTime: time.UnixMilli(start), EndTime: time.UnixMilli(end), TotalCapital: capital, Leverage: cfg.Leverage, Params: p}
		_, extra, err := replay.RunGridTaskContext(ctx, task, cs)
		if err != nil {
			return ReplayEvaluation{}, err
		}
		if err := ctx.Err(); err != nil {
			return ReplayEvaluation{}, err
		}
		metrics, ok := extra.(replay.Metrics)
		if !ok {
			return ReplayEvaluation{}, fmt.Errorf("replay returned invalid metric type %T", extra)
		}
		if metrics.StartTime < start || metrics.EndTime >= end {
			return ReplayEvaluation{}, fmt.Errorf("replay crossed evaluation boundary [%d,%d): [%d,%d]", start, end, metrics.StartTime, metrics.EndTime)
		}
		e := ReplayEvaluation{NetPnL: metrics.NetPnL, TotalReturn: metrics.NetPnL / capital * 100, MaxDrawdown: metrics.MaxDrawdownPct, Fills: metrics.Fills, ClosedGrids: metrics.ClosedGrids, FeesMaker: metrics.FeesMaker, FeesTaker: metrics.FeesTaker, FeesTotal: metrics.FeesTotal, FundingPaid: metrics.FundingPaid, UnrealizedPnL: metrics.UnrealizedPnL, FinalNetQty: metrics.Exposure.FinalNetQty}
		e.Score = e.TotalReturn - e.MaxDrawdown
		if _, err := json.Marshal(e); err != nil {
			return ReplayEvaluation{}, fmt.Errorf("non-finite replay evaluation: %w", err)
		}
		completed++
		if progress != nil {
			progress(completed, total)
		}
		if err := ctx.Err(); err != nil {
			return ReplayEvaluation{}, err
		}
		return e, nil
	}
	for _, w := range windows {
		fold := ReplayFold{WalkForwardWindow: w, Candidates: make([]ReplayCandidateResult, 0, len(combos))}
		fold.TrainDataSHA256, err = replayDigest(w.Train)
		if err != nil {
			return nil, err
		}
		fold.TestDataSHA256, err = replayDigest(w.Test)
		if err != nil {
			return nil, err
		}
		for i, overrides := range combos {
			params, cloneErr := cloneReplayParams(base)
			if cloneErr != nil {
				return nil, cloneErr
			}
			for key, value := range overrides {
				params[key] = value
			}
			train, err := evaluate(params, w.Train, w.TrainStart, w.TrainEnd)
			if err != nil {
				return nil, fmt.Errorf("replay fold %d candidate %d train: %w", w.Index, i, err)
			}
			hash, err := replayDigest(params)
			if err != nil {
				return nil, err
			}
			fold.Candidates = append(fold.Candidates, ReplayCandidateResult{Params: params, ParamsSHA256: hash, Train: train})
			// Strict > preserves canonical sorted-axis enumeration for ties.
			if i == 0 || train.Score > fold.Train.Score {
				fold.SelectedParams = params
				fold.ParamsSHA256 = hash
				fold.Train = train
			}
		}
		// Only the train winner sees test data. Test results never rank candidates.
		fold.Test, err = evaluate(fold.SelectedParams, w.Test, w.TestStart, w.TestEnd)
		if err != nil {
			return nil, fmt.Errorf("replay fold %d test: %w", w.Index, err)
		}
		fold.BaselineTrain, err = evaluate(base, w.Train, w.TrainStart, w.TrainEnd)
		if err != nil {
			return nil, fmt.Errorf("replay fold %d baseline train: %w", w.Index, err)
		}
		fold.BaselineTest, err = evaluate(base, w.Test, w.TestStart, w.TestEnd)
		if err != nil {
			return nil, fmt.Errorf("replay fold %d baseline test: %w", w.Index, err)
		}
		fold.TestExcessReturn = fold.Test.TotalReturn - fold.BaselineTest.TotalReturn
		out.Folds = append(out.Folds, fold)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &UniversalOptimResult{TaskID: taskID, Strategy: "grid", Symbol: symbol, Interval: interval, StartTime: time.UnixMilli(out.DataStart), EndTime: time.UnixMilli(out.DataEnd), AllResults: []UniversalParamResult{}, ReplayWalkForward: out, TotalCombos: total, Completed: completed, Elapsed: time.Since(started)}, nil
}
