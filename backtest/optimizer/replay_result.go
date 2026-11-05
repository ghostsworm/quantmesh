package optimizer

import "quantmesh/backtest"

// ReplayEvaluation reports mark-to-market PnL after fees and settlement funding.
// Fills include partial fills. Slippage/impact is unknown, not a zero-cost claim.
type ReplayEvaluation struct {
	NetPnL        float64 `json:"net_pnl"`
	TotalReturn   float64 `json:"total_return"`
	MaxDrawdown   float64 `json:"max_drawdown"`
	Fills         int     `json:"fills"`
	ClosedGrids   int     `json:"closed_grids"`
	FeesMaker     float64 `json:"fees_maker"`
	FeesTaker     float64 `json:"fees_taker"`
	FeesTotal     float64 `json:"fees_total"`
	FundingPaid   float64 `json:"funding_paid"`
	UnrealizedPnL float64 `json:"unrealized_pnl"`
	FinalNetQty   float64 `json:"final_net_qty"`
	Score         float64 `json:"score"`
}

type ReplayCandidateResult struct {
	Params       map[string]interface{} `json:"params"`
	ParamsSHA256 string                 `json:"params_sha256"`
	Train        ReplayEvaluation       `json:"train"`
}

type ReplayFold struct {
	WalkForwardWindow
	TrainDataSHA256  string                  `json:"train_data_sha256"`
	TestDataSHA256   string                  `json:"test_data_sha256"`
	SelectedParams   map[string]interface{}  `json:"selected_params"`
	ParamsSHA256     string                  `json:"params_sha256"`
	Train            ReplayEvaluation        `json:"train"`
	Test             ReplayEvaluation        `json:"test"`
	BaselineTrain    ReplayEvaluation        `json:"baseline_train"`
	BaselineTest     ReplayEvaluation        `json:"baseline_test"`
	TestExcessReturn float64                 `json:"test_excess_return"`
	Candidates       []ReplayCandidateResult `json:"candidates"`
}

// ReplayWalkForwardResult deliberately has no global best-by-test ranking or
// compounded account curve: windows restart flat and mark residual inventory.
type ReplayWalkForwardResult struct {
	Protocol         string                           `json:"protocol"`
	Engine           string                           `json:"engine"`
	ScoreRule        string                           `json:"score_rule"`
	WarmupPolicy     string                           `json:"warmup_policy"`
	WindowPolicy     string                           `json:"window_policy"`
	Config           backtest.ReplayWalkForwardConfig `json:"config"`
	InitialCapital   float64                          `json:"initial_capital"`
	SearchRanges     map[string]ParamRange            `json:"search_ranges"`
	ConfigSHA256     string                           `json:"config_sha256"`
	DataSHA256       string                           `json:"data_sha256"`
	ExperimentSHA256 string                           `json:"experiment_sha256"`
	DataBars         int                              `json:"data_bars"`
	DataStart        int64                            `json:"data_start"`
	DataEnd          int64                            `json:"data_end"`
	CandleIntervalMs int64                            `json:"candle_interval_ms"`
	BaselineParams   map[string]interface{}           `json:"baseline_params"`
	BaselineSHA256   string                           `json:"baseline_sha256"`
	Limitations      []string                         `json:"limitations"`
	Folds            []ReplayFold                     `json:"folds"`
}
