package optimrun

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"time"

	"quantmesh/backtest"
	"quantmesh/backtest/optimizer"
	"quantmesh/exchange"
)

var ErrInvalidOptimTask = errors.New("invalid optimizer task")

var optimSymbolPattern = regexp.MustCompile(`^[A-Z0-9]{2,32}$`)

// Only fixed-duration Binance intervals supported by the public data loader.
var optimIntervals = map[string]time.Duration{
	"1m": time.Minute, "3m": 3 * time.Minute, "5m": 5 * time.Minute,
	"15m": 15 * time.Minute, "30m": 30 * time.Minute,
	"1h": time.Hour, "2h": 2 * time.Hour, "4h": 4 * time.Hour,
	"6h": 6 * time.Hour, "8h": 8 * time.Hour, "12h": 12 * time.Hour,
	"1d": 24 * time.Hour,
}

const (
	optimMillisecondsPerDay = int64(24 * time.Hour / time.Millisecond)
	// Match BuildWalkForwardWindows' minimum training/validation sample sizes.
	optimMinTrainBars = 50
	optimMinTestBars  = 10
)

type replayTaskPlan struct {
	first, end, barMs int64
	bars, total       int
}

// ValidateTask runs before persistence or downloads, including on task reload.
// Stored legacy results remain readable, but a grid run always opts into replay.
func ValidateTask(task *backtest.OptimTask) (int, error) {
	total, err := validateTask(task)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrInvalidOptimTask, err)
	}
	return total, nil
}

func validateTask(task *backtest.OptimTask) (int, error) {
	if task == nil {
		return 0, fmt.Errorf("task is required")
	}
	switch task.Strategy {
	case "grid", "momentum", "mean_reversion", "trend_following", "dca", "martingale":
	default:
		return 0, fmt.Errorf("unsupported strategy %q", task.Strategy)
	}
	if task.SearchSpace.Strategy != task.Strategy {
		return 0, fmt.Errorf("search_space.strategy must match strategy")
	}
	if !optimSymbolPattern.MatchString(task.Symbol) {
		return 0, fmt.Errorf("symbol must contain 2..32 uppercase letters or digits")
	}
	if _, ok := optimIntervals[task.Interval]; !ok {
		return 0, fmt.Errorf("unsupported interval %q", task.Interval)
	}
	if task.StartTime.IsZero() || task.StartTime.UnixMilli() < 0 || !task.EndTime.After(task.StartTime) || task.EndTime.After(time.Now()) {
		return 0, fmt.Errorf("start_time must be nonnegative and earlier than end_time; end_time cannot be in the future")
	}
	if math.IsNaN(task.TotalCapital) || math.IsInf(task.TotalCapital, 0) || task.TotalCapital <= 0 {
		return 0, fmt.Errorf("total_capital must be finite and positive")
	}
	space := toOptimizerSearchSpace(task.SearchSpace)
	if task.Strategy == "grid" && space.ReplayWalkForward == nil {
		return 0, fmt.Errorf("grid optimization requires explicit search_space.replay_walk_forward")
	}
	count, err := optimizer.CountUniversalParamCombos(space)
	if err != nil {
		return 0, err
	}
	if err := optimizer.ValidateReplaySearchSpace(space); err != nil {
		return 0, err
	}
	plan, err := planReplayTask(task, count)
	if err != nil {
		return 0, err
	}
	return plan.total, nil
}

// Plan on complete candles in [start,end), excluding a partial first/last bar.
// Requiring the loader to supply this coverage makes persisted totals exact.
func planReplayTask(task *backtest.OptimTask, candidates int) (replayTaskPlan, error) {
	barMs := optimIntervals[task.Interval].Milliseconds()
	first := task.StartTime.UnixMilli() / barMs * barMs
	if task.StartTime.After(time.UnixMilli(first)) {
		first += barMs
	}
	end := task.EndTime.UnixMilli() / barMs * barMs
	bars := (end - first) / barMs
	if bars < 2 || bars > optimizer.MaxReplayCandles {
		return replayTaskPlan{}, fmt.Errorf("requested period requires 2..%d complete candles", optimizer.MaxReplayCandles)
	}
	plan := replayTaskPlan{first: first, end: end, barMs: barMs, bars: int(bars), total: candidates}
	cfg := task.SearchSpace.ReplayWalkForward
	if cfg == nil {
		return plan, nil
	}
	trainMs := int64(cfg.TrainDays * float64(optimMillisecondsPerDay))
	testMs := int64(cfg.TestDays * float64(optimMillisecondsPerDay))
	if trainMs%barMs != 0 || testMs%barMs != 0 {
		return replayTaskPlan{}, fmt.Errorf("replay windows must align to complete %s candles", task.Interval)
	}
	trainBars, testBars := trainMs/barMs, testMs/barMs
	if trainBars < optimMinTrainBars || testBars < optimMinTestBars {
		return replayTaskPlan{}, fmt.Errorf("replay windows require at least %d training and %d test candles", optimMinTrainBars, optimMinTestBars)
	}
	if trainBars > bars || testBars > bars-trainBars {
		return replayTaskPlan{}, fmt.Errorf("requested period does not contain a complete replay fold")
	}
	folds := 1 + (bars-trainBars-testBars)/testBars
	if folds > optimizer.MaxReplayFolds {
		return replayTaskPlan{}, fmt.Errorf("replay permits at most %d folds", optimizer.MaxReplayFolds)
	}
	work := folds * (int64(candidates+1)*trainBars + 2*testBars)
	if work > optimizer.MaxReplayCandleEvaluations {
		return replayTaskPlan{}, fmt.Errorf("replay evaluation budget exceeds %d candle evaluations", optimizer.MaxReplayCandleEvaluations)
	}
	plan.total = int(folds) * (candidates + 3)
	return plan, nil
}

func replayCandlesForTask(task *backtest.OptimTask, candles []*exchange.Candle) ([]*exchange.Candle, error) {
	plan, err := planReplayTask(task, 1)
	if err != nil {
		return nil, err
	}
	out := make([]*exchange.Candle, 0, plan.bars)
	for _, candle := range candles {
		if candle == nil {
			return nil, fmt.Errorf("historical data contains a nil candle")
		}
		if candle.Timestamp >= plan.first && candle.Timestamp < plan.end {
			out = append(out, candle)
		}
	}
	if len(out) != plan.bars {
		return nil, fmt.Errorf("replay requires full requested coverage: got %d candles, want %d", len(out), plan.bars)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Timestamp < out[j].Timestamp })
	for i, candle := range out {
		if candle.Timestamp != plan.first+int64(i)*plan.barMs {
			return nil, fmt.Errorf("replay data is missing or duplicates candle at index %d", i)
		}
	}
	return out, nil
}
