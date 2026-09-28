package strategy

import "time"

type VolatilityEvidenceStatus struct {
	Model          string    `json:"model"`
	RequiredBars   int       `json:"required_closed_hourly_bars"`
	HistoryThrough time.Time `json:"history_through"`
	LastQuoteAt    time.Time `json:"last_quote_at"`
	Ready          bool      `json:"ready"`
	Reason         string    `json:"reason,omitempty"`
}

// Reports data readiness, not permission to trade or a profitability claim.
func (da *DynamicAdjuster) GetVolatilityEvidenceStatus() VolatilityEvidenceStatus {
	da.mu.RLock()
	defer da.mu.RUnlock()
	n, err := da.volatilityAlert.RequiredHourlyHistory()
	s := VolatilityEvidenceStatus{Model: "closed_hourly_simple_return_population_stddev_percent", RequiredBars: n, HistoryThrough: da.historyThrough, LastQuoteAt: da.priceEvidenceAt}
	switch {
	case da.stopped:
		s.Reason = "stopped"
	case err != nil:
		s.Reason = "invalid_hourly_configuration"
	case da.historyFailed:
		s.Reason = "hourly_history_unverified"
	case !da.historyThrough.Equal(da.now().UTC().Truncate(time.Hour)) || da.observation == nil:
		s.Reason = "hourly_history_missing_or_stale"
	case !da.quoteValid || da.now().Sub(da.priceEvidenceAt) > volatilityEvidenceMaxAge:
		s.Reason = "quote_missing_or_stale"
	default:
		s.Ready = true
	}
	return s
}
