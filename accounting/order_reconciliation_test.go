package accounting

import (
	"testing"
	"time"
)

func validRequest() ReconcileRequest {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	return ReconcileRequest{
		OperationID:      "manual-op-1",
		Owner:            OrderOwner{Exchange: "example", Market: "spot", AccountScope: "acct-1", Bot: "bot-1", Symbol: "BTCUSDT", StrategyName: "grid-a", StrategyType: "grid", ClientOrderID: "client-1", VenueOrderID: "venue-1"},
		ExpectedRevision: "rev-4",
		From:             Cursor{Sequence: 4, TradeID: "trade-4"},
		Target:           Cursor{Sequence: 6, TradeID: "trade-6"},
		Fills: []VerifiedFill{
			{TradeID: "trade-5", CursorSequence: 5, Side: "BUY", PositionSide: "LONG", OrderRole: "entry", Price: "100.25", Quantity: "0.2", CommissionAmount: "0.001", CommissionAsset: "BTC", TradeTime: now.Add(-time.Minute)},
			{TradeID: "trade-6", CursorSequence: 6, Side: "SELL", PositionSide: "LONG", OrderRole: "exit", Price: "101", Quantity: "0.1", CommissionAmount: "0", CommissionAsset: "BTC", TradeTime: now},
		},
		Fees:     []VerifiedFee{{FeeID: "fee-1", TradeID: "trade-5", Amount: "0.02", Asset: "USDT", ChargedAt: now}},
		Evidence: EvidenceSummary{Source: "exchange-export", Summary: "verified against account trade history", VerifiedBy: "operator-1", VerifiedAt: now},
	}
}

func TestReconcileRequestValidate(t *testing.T) {
	if err := validRequest().Validate(); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
}

func TestReconcileRequestValidateRejectsUnsafeOrIncompleteInput(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ReconcileRequest)
	}{
		{"missing owner scope", func(r *ReconcileRequest) { r.Owner.AccountScope = " " }},
		{"missing expected revision", func(r *ReconcileRequest) { r.ExpectedRevision = "" }},
		{"non-advancing cursor", func(r *ReconcileRequest) { r.Target = r.From }},
		{"missing fill source", func(r *ReconcileRequest) { r.Evidence.Source = "" }},
		{"duplicate trade id", func(r *ReconcileRequest) { r.Fills[1].TradeID = r.Fills[0].TradeID }},
		{"cursor sequence gap past target", func(r *ReconcileRequest) { r.Fills[1].CursorSequence = 7 }},
		{"cursor sequence gap inside target", func(r *ReconcileRequest) { r.Fills[0].CursorSequence = 6 }},
		{"fill does not reach target", func(r *ReconcileRequest) { r.Fills[1].TradeID = "other" }},
		{"float-like noncanonical decimal", func(r *ReconcileRequest) { r.Fills[0].Price = "1e2" }},
		{"redundant decimal zero", func(r *ReconcileRequest) { r.Fills[0].Quantity = "0.200" }},
		{"missing commission asset", func(r *ReconcileRequest) { r.Fills[0].CommissionAsset = "" }},
		{"missing trade time", func(r *ReconcileRequest) { r.Fills[0].TradeTime = time.Time{} }},
		{"missing order side", func(r *ReconcileRequest) { r.Fills[0].Side = "" }},
		{"missing position role", func(r *ReconcileRequest) { r.Fills[0].OrderRole = "" }},
		{"fee references unknown fill", func(r *ReconcileRequest) { r.Fees[0].TradeID = "unknown" }},
		{"noncanonical separate fee", func(r *ReconcileRequest) { r.Fees[0].Amount = "0.010" }},
		{"missing fee source identity", func(r *ReconcileRequest) { r.Fees[0].FeeID = "" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := validRequest()
			test.mutate(&request)
			if err := request.Validate(); err == nil {
				t.Fatal("invalid request unexpectedly accepted")
			}
		})
	}
}

func TestReconcileRequestAllowsSignedFeeOrRebate(t *testing.T) {
	request := validRequest()
	request.Fees[0].Amount = "-0.01"
	if err := request.Validate(); err != nil {
		t.Fatalf("canonical rebate rejected: %v", err)
	}
}

func TestValidateCanonicalDecimal(t *testing.T) {
	valid := []string{"0", "1", "-1", "0.01", "-0.01", "1200.000001"}
	for _, value := range valid {
		if err := ValidateCanonicalDecimal(value); err != nil {
			t.Errorf("%q rejected: %v", value, err)
		}
	}
	invalid := []string{"", ".1", "1.", "+1", "01", "-0", "0.0", "1.20", "1e2", " 1", "NaN"}
	for _, value := range invalid {
		if err := ValidateCanonicalDecimal(value); err == nil {
			t.Errorf("%q unexpectedly accepted", value)
		}
	}
}

func TestAccountingSnapshotValidate(t *testing.T) {
	snapshot := AccountingSnapshot{
		Owner:    validRequest().Owner,
		Revision: "revision-1",
		Cursor:   Cursor{Sequence: 6, TradeID: "trade-6"},
		Result:   EconomicResult{NetQuantity: "0.3", RealizedPnL: []AssetAmount{{Asset: "USDT", Amount: "-2.5"}}},
		ReadAt:   time.Now().UTC(),
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("valid snapshot rejected: %v", err)
	}
	snapshot.Result.NetQuantity = "0.30"
	if err := snapshot.Validate(); err == nil {
		t.Fatal("non-canonical economic amount unexpectedly accepted")
	}
}

func TestReconciliationReceiptValidate(t *testing.T) {
	request := validRequest()
	receipt := ReconciliationReceipt{
		OperationID: request.OperationID, Owner: request.Owner,
		ExpectedRevision: request.ExpectedRevision, From: request.From, Target: request.Target,
		Fills: request.Fills, Fees: request.Fees, Evidence: request.Evidence,
		Revision: "rev-5", Result: EconomicResult{NetQuantity: "0.3"},
		CompletedAt: time.Now().UTC(),
	}
	if err := receipt.Validate(); err != nil {
		t.Fatalf("valid durable receipt rejected: %v", err)
	}
	receipt.Fills = nil
	if err := receipt.Validate(); err == nil {
		t.Fatal("receipt without its verified fills unexpectedly accepted")
	}
}
