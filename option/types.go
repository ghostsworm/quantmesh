package option

import "time"

// OptionHedgePosition 外部期权对冲仓位（标准化）
type OptionHedgePosition struct {
	Exchange   string    `json:"exchange"`   // binance, deribit
	Symbol     string    `json:"symbol"`     // 标的，如 BTCUSDT
	Instrument string    `json:"instrument"` // 合约名，如 BTC-28MAR25-90000-P
	Right      string    `json:"right"`     // PUT / CALL
	Strike     float64   `json:"strike"`
	Expiry     time.Time `json:"expiry"`
	Qty        float64   `json:"qty"`        // 张数（正=多头）
	MarkPrice  float64   `json:"mark_price"`
	Delta      float64   `json:"delta"`     // 用于覆盖率计算
	Vega       float64   `json:"vega"`
	Theta      float64   `json:"theta"`
	Premium    float64   `json:"premium"`    // 权利金成本
	Source     string    `json:"source"`    // api / manual
	UpdatedAt  time.Time `json:"updated_at"`
}

// CoverageSnapshot 覆盖率快照
type CoverageSnapshot struct {
	BotID              string    `json:"bot_id"`
	GridNotional       float64   `json:"grid_notional"`        // 网格名义敞口
	GridPositionQty    float64   `json:"grid_position_qty"`    // 网格持仓数量
	OptionNotional     float64   `json:"option_notional"`      // 期权名义敞口（Put 保护）
	OptionDeltaHedge   float64   `json:"option_delta_hedge"`   // 期权 Delta 对冲量
	NominalCoverage    float64   `json:"nominal_coverage"`     // 名义覆盖率 0-1
	DeltaCoverage      float64   `json:"delta_coverage"`       // Delta 覆盖率 0-1
	MinDTE             int      `json:"min_dte"`               // 最近到期天数
	TotalPremium       float64   `json:"total_premium"`         // 总权利金
	BelowMinCoverage   bool     `json:"below_min_coverage"`    // 是否低于最小覆盖率
	DTEWarning         bool     `json:"dte_warning"`           // 是否 DTE 告警
	SnapshotAt         time.Time `json:"snapshot_at"`
}

// RollSuggestion 展期建议
type RollSuggestion struct {
	Rank             int     `json:"rank"`               // 1=保守 2=中性 3=进攻
	Label            string  `json:"label"`              // conservative / neutral / aggressive
	Instrument       string  `json:"instrument"`          // 推荐合约
	Strike           float64 `json:"strike"`
	Expiry           string  `json:"expiry"`              // ISO date
	DTE              int     `json:"dte"`
	EstimatedPremium float64 `json:"estimated_premium"`
	ExpectedCoverage float64 `json:"expected_coverage"`   // 展期后预期覆盖率
	RiskIfSkip       string  `json:"risk_if_skip"`       // 若不展期的风险描述
}

// OptionHedgeStatus API 响应：期权对冲状态
type OptionHedgeStatus struct {
	BotID           string               `json:"bot_id"`
	Enabled         bool                 `json:"enabled"`
	Positions       []OptionHedgePosition `json:"positions"`
	Coverage        *CoverageSnapshot    `json:"coverage,omitempty"`
	SyncStatus      string               `json:"sync_status"`       // ok / degraded / failed
	LastSyncAt      *time.Time           `json:"last_sync_at,omitempty"`
	Alerts          []string             `json:"alerts,omitempty"`
}

// OptionHedgeConfig 期权对冲配置（用于 Bot 或风控）
type OptionHedgeConfig struct {
	Enabled             bool    `json:"enabled"`
	Exchange            string  `json:"exchange"`              // binance / deribit
	TargetCoverageRatio float64 `json:"target_coverage_ratio"` // 目标覆盖率 0-1，默认 0.25
	MinCoverageRatio    float64 `json:"min_coverage_ratio"`    // 最小覆盖率，低于则联动风控
	DTEWarningDays      int     `json:"dte_warning_days"`      // DTE 低于此值告警
	RebalanceInterval   int     `json:"rebalance_interval"`    // 同步间隔（秒）
}
