// Mirrors backtest/replay_walkforward.go and backtest/optimizer/replay_result.go.
export interface ReplayWalkForwardConfig {
  train_days: number
  test_days: number
  step_days: number
  leverage: number
  base_params: Record<string, unknown>
}

export interface ReplayEvaluation {
  net_pnl: number
  total_return: number
  max_drawdown: number
  fills: number
  closed_grids: number
  fees_maker: number
  fees_taker: number
  fees_total: number
  funding_paid: number
  unrealized_pnl: number
  final_net_qty: number
  score: number
}

export interface ReplayCandidateResult {
  params: Record<string, unknown>
  params_sha256: string
  train: ReplayEvaluation
}

export interface ReplayFold {
  index: number
  train_start: number
  train_end: number
  test_start: number
  test_end: number
  train_data_sha256: string
  test_data_sha256: string
  selected_params: Record<string, unknown>
  params_sha256: string
  train: ReplayEvaluation
  test: ReplayEvaluation
  baseline_train: ReplayEvaluation
  baseline_test: ReplayEvaluation
  test_excess_return: number
  candidates: ReplayCandidateResult[]
}

export interface ReplayWalkForwardResult {
  protocol: string
  engine: string
  score_rule: string
  warmup_policy: string
  window_policy: string
  config: ReplayWalkForwardConfig
  config_sha256: string
  data_sha256: string
  experiment_sha256: string
  data_bars: number
  data_start: number
  data_end: number
  candle_interval_ms: number
  baseline_params: Record<string, unknown>
  baseline_sha256: string
  limitations: string[]
  folds: ReplayFold[]
}
