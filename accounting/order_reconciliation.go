// Package accounting defines the shared contract for explicitly authorized,
// durable order-accounting reconciliation. It contains no persistence or
// automatic order-update replay behavior.
package accounting

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// OrderOwner identifies exactly one strategy-owned order across restarts.
type OrderOwner struct {
	Exchange      string `json:"exchange"`
	Market        string `json:"market"`
	AccountScope  string `json:"account_scope"`
	Bot           string `json:"bot"`
	Symbol        string `json:"symbol"`
	StrategyName  string `json:"strategy_name"`
	StrategyType  string `json:"strategy_type"`
	ClientOrderID string `json:"client_order_id"`
	VenueOrderID  string `json:"venue_order_id"`
}

func (o OrderOwner) Validate() error {
	fields := []struct{ name, value string }{
		{"exchange", o.Exchange}, {"market", o.Market}, {"account_scope", o.AccountScope},
		{"bot", o.Bot}, {"symbol", o.Symbol}, {"strategy_name", o.StrategyName},
		{"strategy_type", o.StrategyType}, {"client_order_id", o.ClientOrderID},
		{"venue_order_id", o.VenueOrderID},
	}
	for _, field := range fields {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("order owner %s is required", field.name)
		}
	}
	return nil
}

// Cursor is a durable, monotonically increasing per-order accounting sequence.
// Every applied fill advances it by exactly one. Sequence zero is the only
// valid empty cursor and must have no TradeID.
type Cursor struct {
	Sequence uint64 `json:"sequence"`
	TradeID  string `json:"trade_id,omitempty"`
}

func (c Cursor) Validate() error {
	if (c.Sequence == 0) != (c.TradeID == "") {
		return fmt.Errorf("cursor trade_id must be empty exactly when sequence is zero")
	}
	return nil
}

// AssetAmount stores an exact canonical decimal amount for one asset.
type AssetAmount struct {
	Asset  string `json:"asset"`
	Amount string `json:"amount"`
}

// EconomicResult is the durable strategy result read back with its cursor.
type EconomicResult struct {
	NetQuantity string        `json:"net_quantity"`
	RealizedPnL []AssetAmount `json:"realized_pnl"`
}

// AccountingSnapshot is a single durable readback; revision identifies the
// exact state against which a manual reconciliation is authorized.
type AccountingSnapshot struct {
	Owner    OrderOwner     `json:"owner"`
	Revision string         `json:"revision"`
	Cursor   Cursor         `json:"cursor"`
	Result   EconomicResult `json:"result"`
	ReadAt   time.Time      `json:"read_at"`
}

// VerifiedFill is exchange evidence explicitly checked by an operator.
type VerifiedFill struct {
	TradeID          string    `json:"trade_id"`
	CursorSequence   uint64    `json:"cursor_sequence"`
	Side             string    `json:"side"`
	PositionSide     string    `json:"position_side"`
	OrderRole        string    `json:"order_role"`
	Price            string    `json:"price"`
	Quantity         string    `json:"quantity"`
	CommissionAmount string    `json:"commission_amount"`
	CommissionAsset  string    `json:"commission_asset"`
	TradeTime        time.Time `json:"trade_time"`
}

// VerifiedFee captures a separately reconciled fee or rebate, including fees
// that are not represented by a fill's commission field.
type VerifiedFee struct {
	FeeID     string    `json:"fee_id"`
	TradeID   string    `json:"trade_id"`
	Amount    string    `json:"amount"`
	Asset     string    `json:"asset"`
	ChargedAt time.Time `json:"charged_at"`
}

// EvidenceSummary records where the supplied fills came from and who verified
// them. It supplements, but never replaces, the fill-level evidence.
type EvidenceSummary struct {
	Source     string    `json:"source"`
	Summary    string    `json:"summary"`
	VerifiedBy string    `json:"verified_by"`
	VerifiedAt time.Time `json:"verified_at"`
}

// ReconcileRequest is an explicit manual request. Implementations must enforce
// operation idempotency and expected-revision compare-and-swap durably.
type ReconcileRequest struct {
	OperationID      string          `json:"operation_id"`
	Owner            OrderOwner      `json:"owner"`
	ExpectedRevision string          `json:"expected_revision"`
	From             Cursor          `json:"from"`
	Target           Cursor          `json:"target"`
	Fills            []VerifiedFill  `json:"fills"`
	Fees             []VerifiedFee   `json:"fees"`
	Evidence         EvidenceSummary `json:"evidence"`
}

// ReconciliationReceipt is the durable proof of a completed manual operation.
type ReconciliationReceipt struct {
	OperationID      string          `json:"operation_id"`
	Owner            OrderOwner      `json:"owner"`
	ExpectedRevision string          `json:"expected_revision"`
	From             Cursor          `json:"from"`
	Target           Cursor          `json:"target"`
	Fills            []VerifiedFill  `json:"fills"`
	Fees             []VerifiedFee   `json:"fees"`
	Evidence         EvidenceSummary `json:"evidence"`
	Revision         string          `json:"revision"`
	Result           EconomicResult  `json:"result"`
	CompletedAt      time.Time       `json:"completed_at"`
}

// OrderAccountingReader reads strategy-owned durable accounting state.
type OrderAccountingReader interface {
	ReadOrderAccounting(ctx context.Context, owner OrderOwner) (AccountingSnapshot, error)
}

// OrderAccountingReconciler applies only explicitly submitted manual requests;
// it is not an OnOrderUpdate replay interface.
type OrderAccountingReconciler interface {
	ReconcileOrderAccounting(ctx context.Context, request ReconcileRequest) (ReconciliationReceipt, error)
}

// OrderAccounting combines durable readback with explicit reconciliation.
type OrderAccounting interface {
	OrderAccountingReader
	OrderAccountingReconciler
}

var canonicalDecimalPattern = regexp.MustCompile(`^(0|-?[1-9][0-9]*(\.[0-9]*[1-9])?|-?0\.[0-9]*[1-9])$`)

// ValidateCanonicalDecimal rejects exponent notation, redundant zeros, plus
// signs, whitespace, and other non-canonical representations.
func ValidateCanonicalDecimal(value string) error {
	if !canonicalDecimalPattern.MatchString(value) {
		return fmt.Errorf("%q is not a canonical decimal", value)
	}
	return nil
}

func validateEconomicResult(result EconomicResult) error {
	if err := ValidateCanonicalDecimal(result.NetQuantity); err != nil {
		return fmt.Errorf("net_quantity: %w", err)
	}
	seen := make(map[string]struct{}, len(result.RealizedPnL))
	for i, amount := range result.RealizedPnL {
		if strings.TrimSpace(amount.Asset) == "" {
			return fmt.Errorf("realized_pnl[%d].asset is required", i)
		}
		if _, ok := seen[amount.Asset]; ok {
			return fmt.Errorf("realized_pnl asset %q is duplicated", amount.Asset)
		}
		seen[amount.Asset] = struct{}{}
		if err := ValidateCanonicalDecimal(amount.Amount); err != nil {
			return fmt.Errorf("realized_pnl[%d].amount: %w", i, err)
		}
	}
	return nil
}

func (s AccountingSnapshot) Validate() error {
	if err := s.Owner.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(s.Revision) == "" {
		return fmt.Errorf("revision is required")
	}
	if err := s.Cursor.Validate(); err != nil {
		return err
	}
	if err := validateEconomicResult(s.Result); err != nil {
		return err
	}
	if s.ReadAt.IsZero() {
		return fmt.Errorf("read_at is required")
	}
	return nil
}

func (r ReconcileRequest) Validate() error {
	if strings.TrimSpace(r.OperationID) == "" {
		return fmt.Errorf("operation_id is required")
	}
	if err := r.Owner.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(r.ExpectedRevision) == "" {
		return fmt.Errorf("expected_revision is required")
	}
	if err := r.From.Validate(); err != nil {
		return fmt.Errorf("from: %w", err)
	}
	if err := r.Target.Validate(); err != nil {
		return fmt.Errorf("target: %w", err)
	}
	if r.Target.Sequence <= r.From.Sequence {
		return fmt.Errorf("target cursor must be strictly after from cursor")
	}
	if len(r.Fills) == 0 {
		return fmt.Errorf("at least one verified fill is required")
	}
	if strings.TrimSpace(r.Evidence.Source) == "" || strings.TrimSpace(r.Evidence.Summary) == "" || strings.TrimSpace(r.Evidence.VerifiedBy) == "" || r.Evidence.VerifiedAt.IsZero() {
		return fmt.Errorf("evidence source, summary, verifier, and verification time are required")
	}
	seen := make(map[string]struct{}, len(r.Fills))
	lastSequence := r.From.Sequence
	for i, fill := range r.Fills {
		if strings.TrimSpace(fill.TradeID) == "" {
			return fmt.Errorf("fills[%d].trade_id is required", i)
		}
		if side := strings.ToUpper(strings.TrimSpace(fill.Side)); side != "BUY" && side != "SELL" {
			return fmt.Errorf("fills[%d].side must be BUY or SELL", i)
		}
		if strings.TrimSpace(fill.PositionSide) == "" || strings.TrimSpace(fill.OrderRole) == "" {
			return fmt.Errorf("fills[%d].position_side and order_role are required", i)
		}
		if _, ok := seen[fill.TradeID]; ok {
			return fmt.Errorf("fill trade_id %q is duplicated", fill.TradeID)
		}
		seen[fill.TradeID] = struct{}{}
		if fill.CursorSequence != lastSequence+1 || fill.CursorSequence > r.Target.Sequence {
			return fmt.Errorf("fills[%d].cursor_sequence leaves a gap or exceeds the requested cursor range", i)
		}
		lastSequence = fill.CursorSequence
		for name, value := range map[string]string{"price": fill.Price, "quantity": fill.Quantity, "commission_amount": fill.CommissionAmount} {
			if err := ValidateCanonicalDecimal(value); err != nil {
				return fmt.Errorf("fills[%d].%s: %w", i, name, err)
			}
		}
		if strings.HasPrefix(fill.Price, "-") || fill.Price == "0" {
			return fmt.Errorf("fills[%d].price must be positive", i)
		}
		if strings.HasPrefix(fill.Quantity, "-") || fill.Quantity == "0" {
			return fmt.Errorf("fills[%d].quantity must be positive", i)
		}
		if strings.TrimSpace(fill.CommissionAsset) == "" {
			return fmt.Errorf("fills[%d].commission_asset is required", i)
		}
		if fill.TradeTime.IsZero() {
			return fmt.Errorf("fills[%d].trade_time is required", i)
		}
	}
	if lastSequence != r.Target.Sequence || r.Fills[len(r.Fills)-1].TradeID != r.Target.TradeID {
		return fmt.Errorf("verified fills must end exactly at the target cursor")
	}
	seenFees := make(map[string]struct{}, len(r.Fees))
	for i, fee := range r.Fees {
		if strings.TrimSpace(fee.FeeID) == "" {
			return fmt.Errorf("fees[%d].fee_id is required", i)
		}
		if _, ok := seenFees[fee.FeeID]; ok {
			return fmt.Errorf("fee_id %q is duplicated", fee.FeeID)
		}
		seenFees[fee.FeeID] = struct{}{}
		if strings.TrimSpace(fee.TradeID) == "" {
			return fmt.Errorf("fees[%d].trade_id is required", i)
		}
		if _, ok := seen[fee.TradeID]; !ok {
			return fmt.Errorf("fees[%d].trade_id does not reference a supplied fill", i)
		}
		if err := ValidateCanonicalDecimal(fee.Amount); err != nil {
			return fmt.Errorf("fees[%d].amount: %w", i, err)
		}
		if strings.TrimSpace(fee.Asset) == "" {
			return fmt.Errorf("fees[%d].asset is required", i)
		}
		if fee.ChargedAt.IsZero() {
			return fmt.Errorf("fees[%d].charged_at is required", i)
		}
	}
	return nil
}

func (r ReconciliationReceipt) Validate() error {
	if err := (ReconcileRequest{
		OperationID: r.OperationID, Owner: r.Owner, ExpectedRevision: r.ExpectedRevision,
		From: r.From, Target: r.Target, Fills: r.Fills, Fees: r.Fees, Evidence: r.Evidence,
	}).Validate(); err != nil {
		return fmt.Errorf("receipt request: %w", err)
	}
	if strings.TrimSpace(r.Revision) == "" {
		return fmt.Errorf("revision is required")
	}
	if err := validateEconomicResult(r.Result); err != nil {
		return err
	}
	if r.CompletedAt.IsZero() {
		return fmt.Errorf("completed_at is required")
	}
	return nil
}
