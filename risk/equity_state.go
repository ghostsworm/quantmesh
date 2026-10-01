package risk

import (
	"context"
	"fmt"
	"math"
	"time"

	"quantmesh/exchange/accounting"
)

const equityStateVersion = 1

// EquityCashFlow is an account-ledger entry, not an inferred balance change.
// IDs must be unique across every account in the observation scope.
// Amount is signed in the observation's valuation currency. Only external
// capital flows are neutralized; fees/funding/interest remain in performance.
type EquityCashFlow struct {
	Account         string    `json:"account,omitempty"`
	ExactAmount     string    `json:"exact_amount,omitempty"`
	WalletCurrency  string    `json:"wallet_currency,omitempty"`
	Symbol          string    `json:"symbol,omitempty"`
	ValuationRate   string    `json:"valuation_rate,omitempty"`
	ValuationSource string    `json:"valuation_source,omitempty"`
	ValuationAt     time.Time `json:"valuation_at,omitempty"`
	ID              string    `json:"id"`
	Kind            string    `json:"kind"`
	Currency        string    `json:"currency"`
	Amount          float64   `json:"amount"`
	At              time.Time `json:"at"`
}

// ArchivedEquityReceipts keeps exact wallet deltas and capital-flow totals
// after individual receipts age beyond the exchange cursor-overlap window.
// Through is an exclusive watermark: an unseen receipt strictly before it
// requires historical reconciliation.
type ArchivedEquityReceipts struct {
	Through      time.Time `json:"through"`
	BalanceDelta string    `json:"balance_delta"`
	ExternalFlow string    `json:"external_flow"`
}

// EquityObservation must refer to one stable account/market/valuation scope.
// Complete asserts all requested ledger pages were read. With Wallets, cursors
// are per-wallet exchange time and the checkpoint additionally reconciles exact
// balances in each wallet's currency. Legacy observations use local ObservedAt
// and FlowFrom/FlowThrough.
// Unsupported/empty APIs are not proof.
type EquityObservation struct {
	Scope            string
	Currency         string
	Equity           float64
	ObservedAt       time.Time
	CashFlowComplete bool
	FlowFrom         time.Time
	FlowThrough      time.Time
	Flows            []EquityCashFlow
	Wallets          map[string]accounting.Wallet
}

// AccountEquitySource accepts durable per-account exchange-clock cursors.
// It avoids interpreting a local publication timestamp as a remote ledger time.
type AccountEquitySource interface {
	ObserveAccountEquity(context.Context, map[string]time.Time) (EquityObservation, error)
}

// ReconciledEquitySource optionally augments EquitySource. since is zero for
// the initial observation, otherwise the last durable cursor minus overlap.
type ReconciledEquitySource interface {
	ObserveEquity(ctx context.Context, since time.Time) (EquityObservation, error)
}

// EquityCheckpoint is saved atomically BEFORE publishing a new risk metric.
// A reset is explicit (MetricsResetMarks.All), never inferred from restart,
// account membership change, corrupt data, or a failed database read.
type EquityCheckpoint struct {
	Version          int                               `json:"version"`
	Revision         int64                             `json:"revision"`
	Scope            string                            `json:"scope"`
	Currency         string                            `json:"currency"`
	CashFlowAdjusted bool                              `json:"cash_flow_adjusted"`
	BaseAt           time.Time                         `json:"base_at"`
	ResetAt          time.Time                         `json:"reset_at"`
	LastAt           time.Time                         `json:"last_at"`
	LastEquity       float64                           `json:"last_equity"`
	ExternalFlows    float64                           `json:"external_flows"`
	AdjustedEquity   float64                           `json:"adjusted_equity"`
	HighWater        float64                           `json:"high_water"`
	DrawdownPct      float64                           `json:"drawdown_pct"`
	Receipts         map[string]EquityCashFlow         `json:"receipts"`
	ArchivedReceipts map[string]ArchivedEquityReceipts `json:"archived_receipts,omitempty"`
	BaseWallets      map[string]accounting.Wallet      `json:"base_wallets,omitempty"`
	Wallets          map[string]accounting.Wallet      `json:"wallets,omitempty"`
}

// EquityStateStore returns nil only for a confirmed absent record. Save must
// compare expectedRevision atomically, rejecting concurrent/stale writers.
type EquityStateStore interface {
	LoadEquityState(context.Context) (*EquityCheckpoint, error)
	SaveEquityState(context.Context, int64, EquityCheckpoint) error
}

func finiteEquity(n float64) bool { return !math.IsNaN(n) && !math.IsInf(n, 0) }

func (s EquityCheckpoint) validate() error {
	if s.Version != equityStateVersion || s.Revision < 1 || s.Scope == "" || s.Currency == "" || s.BaseAt.IsZero() || s.LastAt.Before(s.BaseAt) {
		return fmt.Errorf("invalid equity checkpoint identity/version/time")
	}
	for _, n := range []float64{s.LastEquity, s.ExternalFlows, s.AdjustedEquity, s.HighWater, s.DrawdownPct} {
		if !finiteEquity(n) {
			return fmt.Errorf("non-finite equity checkpoint")
		}
	}
	if s.HighWater <= 0 || s.HighWater < s.AdjustedEquity || s.DrawdownPct < 0 {
		return fmt.Errorf("invalid equity checkpoint high water")
	}
	if !equityNumbersEqual(s.AdjustedEquity, s.LastEquity-s.ExternalFlows) || !equityNumbersEqual(s.DrawdownPct, (s.HighWater-s.AdjustedEquity)/s.HighWater*percentMultiplier) {
		return fmt.Errorf("inconsistent equity checkpoint calculation")
	}
	if len(s.Wallets) > 0 || len(s.BaseWallets) > 0 || len(s.ArchivedReceipts) > 0 {
		external, err := s.walletLedgerTotal()
		if err != nil {
			return err
		}
		if !equityNumbersEqual(external, s.ExternalFlows) {
			return fmt.Errorf("inconsistent wallet external capital")
		}
		return nil
	}
	flows := 0.0
	for id, flow := range s.Receipts {
		external, err := flow.externalAmount(s.Currency)
		if err != nil || flow.Account != "" || flow.ExactAmount != "" || id != flow.ID || !flow.At.After(s.BaseAt) || flow.At.After(s.LastAt) {
			return fmt.Errorf("invalid equity checkpoint ledger receipt")
		}
		flows += external
	}
	if !equityNumbersEqual(flows, s.ExternalFlows) || (!s.CashFlowAdjusted && len(s.Receipts) != 0) {
		return fmt.Errorf("inconsistent equity checkpoint cash flows")
	}
	return nil
}

func equityNumbersEqual(a, b float64) bool {
	return math.Abs(a-b) <= 1e-9*math.Max(1, math.Max(math.Abs(a), math.Abs(b)))
}

func (f EquityCashFlow) externalAmount(currency string) (float64, error) {
	if f.ID == "" || f.At.IsZero() || f.Currency != currency || !finiteEquity(f.Amount) {
		return 0, fmt.Errorf("invalid equity cash-flow identity/currency/amount")
	}
	switch f.Kind {
	case "deposit", "transfer_in":
		if f.Amount < 0 {
			return 0, fmt.Errorf("negative incoming capital")
		}
		return f.Amount, nil
	case "withdrawal", "transfer_out":
		if f.Amount > 0 {
			return 0, fmt.Errorf("positive outgoing capital")
		}
		return f.Amount, nil
	case "fee":
		if f.Amount > 0 {
			return 0, fmt.Errorf("positive expense without rebate classification")
		}
		return 0, nil
	case "interest", "funding", "realized_pnl", "rebate", "insurance_clear":
		return 0, nil
	default:
		return 0, fmt.Errorf("unclassified equity cash flow %q", f.Kind)
	}
}

func nextEquityCheckpoint(previous *EquityCheckpoint, observation EquityObservation, now, reset time.Time, maxAge time.Duration, requireFlows bool) (EquityCheckpoint, error) {
	o := observation
	if o.Scope == "" || o.Currency == "" || !finiteEquity(o.Equity) || o.ObservedAt.IsZero() || o.ObservedAt.After(now) || now.Sub(o.ObservedAt) > maxAge {
		return EquityCheckpoint{}, fmt.Errorf("invalid or stale account equity observation")
	}
	if requireFlows && !o.CashFlowComplete {
		return EquityCheckpoint{}, fmt.Errorf("account cash-flow coverage is incomplete")
	}
	if reset.After(o.ObservedAt) {
		return EquityCheckpoint{}, fmt.Errorf("equity observation predates requested reset")
	}
	if len(o.Wallets) > 0 {
		return nextWalletEquityCheckpoint(previous, o, now, reset, maxAge)
	}
	if previous != nil && len(previous.Wallets) > 0 {
		return EquityCheckpoint{}, fmt.Errorf("account wallet evidence disappeared")
	}
	for _, flow := range o.Flows {
		if _, err := flow.externalAmount(o.Currency); err != nil {
			return EquityCheckpoint{}, err
		}
		if flow.Account != "" || flow.ExactAmount != "" || flow.At.After(o.ObservedAt) {
			return EquityCheckpoint{}, fmt.Errorf("equity ledger entry is newer than account snapshot")
		}
	}
	s := EquityCheckpoint{Version: equityStateVersion, Scope: o.Scope, Currency: o.Currency, CashFlowAdjusted: o.CashFlowComplete, BaseAt: o.ObservedAt, ResetAt: reset,
		HighWater: o.Equity, Receipts: make(map[string]EquityCashFlow)}
	if previous != nil {
		if err := previous.validate(); err != nil {
			return s, err
		}
		if o.Scope != previous.Scope || o.Currency != previous.Currency {
			return s, fmt.Errorf("equity account/valuation scope changed; explicit reconciliation required")
		}
		if o.ObservedAt.Before(previous.LastAt) {
			return s, fmt.Errorf("equity observation regressed")
		}
		s.Revision = previous.Revision
		if !reset.After(previous.ResetAt) {
			if previous.CashFlowAdjusted != o.CashFlowComplete {
				return s, fmt.Errorf("equity cash-flow mode changed without explicit reset")
			}
			s = *previous
			s.Receipts = make(map[string]EquityCashFlow, len(previous.Receipts))
			for id, flow := range previous.Receipts {
				s.Receipts[id] = flow
			}
			if o.ObservedAt.Equal(s.LastAt) && o.Equity != s.LastEquity {
				return s, fmt.Errorf("equity changed without a new observation time")
			}
			if o.CashFlowComplete {
				if o.FlowFrom.IsZero() || o.FlowFrom.After(maxTime(previous.BaseAt, previous.LastAt.Add(-realizedCursorOverlap))) || o.FlowThrough.Before(o.ObservedAt) {
					return s, fmt.Errorf("equity ledger coverage has a gap")
				}
				observedReceipts := make(map[string]bool, len(o.Flows))
				for _, flow := range o.Flows {
					observedReceipts[flow.ID] = true
					external, err := flow.externalAmount(o.Currency)
					if err != nil {
						return s, err
					}
					if flow.At.Before(o.FlowFrom) || flow.At.After(o.ObservedAt) {
						return s, fmt.Errorf("equity ledger entry outside observed interval")
					}
					if !flow.At.After(s.BaseAt) {
						continue
					}
					if old, ok := s.Receipts[flow.ID]; ok {
						if old.Kind != flow.Kind || old.Currency != flow.Currency || old.Amount != flow.Amount || !old.At.Equal(flow.At) {
							return s, fmt.Errorf("equity ledger receipt changed; reconciliation required")
						}
						continue
					}
					if !flow.At.After(previous.LastAt) {
						return s, fmt.Errorf("late equity ledger entry requires historical reconciliation")
					}
					s.Receipts[flow.ID] = flow
					s.ExternalFlows += external
				}
				for id, flow := range previous.Receipts {
					if !flow.At.Before(o.FlowFrom) && !observedReceipts[id] {
						return s, fmt.Errorf("previously observed equity ledger receipt missing from complete overlap")
					}
				}
			}
		}
	}
	return finishEquityCheckpoint(s, o)
}

func finishEquityCheckpoint(s EquityCheckpoint, o EquityObservation) (EquityCheckpoint, error) {
	if s.HighWater <= 0 {
		return s, fmt.Errorf("cannot establish equity baseline from nonpositive equity")
	}
	s.LastAt, s.LastEquity = o.ObservedAt, o.Equity
	s.AdjustedEquity = o.Equity - s.ExternalFlows
	if s.AdjustedEquity > s.HighWater {
		s.HighWater = s.AdjustedEquity
	}
	s.DrawdownPct = (s.HighWater - s.AdjustedEquity) / s.HighWater * percentMultiplier
	s.Revision++
	return s, s.validate()
}
