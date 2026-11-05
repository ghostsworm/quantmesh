package backtest

// ReplayWalkForwardConfig is persisted inside OptimSearchSpace, so task restarts
// retain the same training protocol and the predeclared baseline. Each window
// starts flat with the same capital; no state or warm-up crosses its boundary.
type ReplayWalkForwardConfig struct {
	TrainDays  float64                `json:"train_days"`
	TestDays   float64                `json:"test_days"`
	StepDays   float64                `json:"step_days"`
	Leverage   float64                `json:"leverage"`
	BaseParams map[string]interface{} `json:"base_params"`
}
