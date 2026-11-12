package optimrun

import (
	"context"
	"fmt"

	"quantmesh/backtest"
	"quantmesh/backtest/optimizer"
	"quantmesh/backtest/replay"
	"quantmesh/backtest/visionfeed"
	"quantmesh/exchange"
	"quantmesh/strategy/regime"
)

// replayVisionLoader reads one verified window. Tests substitute it so no
// real market file is touched.
type replayVisionLoader func(context.Context, visionfeed.LoadRequest) (visionfeed.LoadResult, error)

// loadReplayData reads the checksum-verified 1m files for a grid walk-forward
// task and aggregates them to the requested interval. It never falls back to
// the live exchange.
func (m *OptimTaskManager) loadReplayData(ctx context.Context, task *backtest.OptimTask) ([]*exchange.Candle, optimizer.ReplayProvenance, error) {
	var none optimizer.ReplayProvenance
	plan, err := planReplayTask(task, 1)
	if err != nil {
		return nil, none, fmt.Errorf("replay historical coverage: %w", err)
	}
	loaded, err := m.replayVision(ctx, visionfeed.LoadRequest{
		Root: m.visionRoot, Symbol: task.Symbol, Interval: task.Interval, FirstMs: plan.first, EndMs: plan.end,
	})
	if err != nil {
		return nil, none, fmt.Errorf("获取历史数据失败: %w", err)
	}
	aggregated, err := aggregateReplayMinutes(loaded.Candles, task.Interval, plan.first, plan.end)
	if err != nil {
		return nil, none, fmt.Errorf("获取历史数据失败: %w", err)
	}
	candles, err := replayCandlesForTask(task, aggregated)
	if err != nil {
		return nil, none, fmt.Errorf("replay historical coverage: %w", err)
	}
	provenance := optimizer.ReplayProvenance{
		Source:       loaded.Provenance.Source,
		Files:        copyDigests(loaded.Provenance.Files),
		FundingFiles: copyDigests(loaded.Provenance.FundingFiles),
	}
	if loaded.FundingUsed {
		provenance.Funding = loaded.Funding
	}
	return candles, provenance, nil
}

// aggregateReplayMinutes keeps a 1m request unchanged and folds every other
// interval exactly. A partial bucket fails instead of emitting a short candle.
func aggregateReplayMinutes(minutes []*exchange.Candle, interval string, firstMs, endMs int64) ([]*exchange.Candle, error) {
	duration, err := regime.ParseKlineInterval(interval)
	if err != nil {
		return nil, fmt.Errorf("replay interval: %w", err)
	}
	intervalMs := duration.Milliseconds()
	if intervalMs == visionfeed.MinuteMs {
		return minutes, nil
	}
	return visionfeed.AggregateExact(minutes, intervalMs, firstMs, endMs)
}

func copyDigests(files []visionfeed.FileDigest) []optimizer.VerifiedFile {
	if len(files) == 0 {
		return nil
	}
	out := make([]optimizer.VerifiedFile, len(files))
	for i, file := range files {
		out[i] = optimizer.VerifiedFile{Name: file.Name, SHA256: file.SHA256}
	}
	return out
}

// applyReplayFunding attaches verified settlement funding only when the caller
// already asked to simulate funding. The points carry no mark price, so they
// settle cost and never become a pricing signal.
func applyReplayFunding(space *optimizer.UniversalSearchSpace, provenance optimizer.ReplayProvenance) {
	cfg := space.ReplayWalkForward
	if cfg == nil || len(provenance.Funding) == 0 || cfg.BaseParams["funding_enabled"] != true {
		return
	}
	cfg.BaseParams["funding_series"] = fundingPointsToParam(provenance.Funding)
}

func fundingPointsToParam(points []replay.FundingPoint) []map[string]interface{} {
	out := make([]map[string]interface{}, len(points))
	for i, point := range points {
		out[i] = map[string]interface{}{"timestamp": float64(point.Timestamp), "rate": point.Rate}
	}
	return out
}
