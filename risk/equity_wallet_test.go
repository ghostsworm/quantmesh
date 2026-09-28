package risk

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"quantmesh/exchange/accounting"
)

func testWalletObservation(local time.Time, skew time.Duration, balance string, equity float64, from time.Time) EquityObservation {
	through := local.Add(skew - time.Millisecond)
	if from.IsZero() {
		from = through.Add(-realizedCursorOverlap)
	}
	return EquityObservation{Scope: "wallet-a:futures", Currency: "USDT", Equity: equity, ObservedAt: local, CashFlowComplete: true,
		Wallets: map[string]accounting.Wallet{"a": {Balance: balance, ObservedAt: local, From: from, Through: through}}}
}

func testWalletFlow(id, kind, amount string, at time.Time) EquityCashFlow {
	n, _ := accounting.Decimal(amount)
	f, _ := n.Float64()
	return EquityCashFlow{ID: id, Account: "a", ExactAmount: amount, Kind: kind, Currency: "USDT", Amount: f, At: at}
}

func TestEquityWalletExactReconciliationAndRestart(t *testing.T) {
	for _, skew := range []time.Duration{-time.Second, time.Second} {
		t.Run(skew.String(), func(t *testing.T) {
			base := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
			o := testWalletObservation(base, skew, "1000", 1000, time.Time{})
			oldFee := testWalletFlow("old", "fee", "-1", o.Wallets["a"].Through.Add(-30*time.Second))
			o.Flows = []EquityCashFlow{oldFee}
			s, err := nextEquityCheckpoint(nil, o, base, time.Time{}, time.Minute, true)
			if err != nil {
				t.Fatal(err)
			}
			now := base.Add(time.Minute)
			o = testWalletObservation(now, skew, "1390", 1380, s.BaseWallets["a"].From)
			deposit := testWalletFlow("deposit", "transfer_in", "500", o.Wallets["a"].Through.Add(-2*time.Second))
			loss := testWalletFlow("loss", "realized_pnl", "-100", o.Wallets["a"].Through.Add(-time.Second))
			fee := testWalletFlow("fee", "fee", "-10", o.Wallets["a"].Through)
			o.Flows = []EquityCashFlow{oldFee, deposit, fee, loss, deposit}
			s, err = nextEquityCheckpoint(&s, o, now, time.Time{}, time.Minute, true)
			if err != nil || s.ExternalFlows != 500 || s.AdjustedEquity != 880 || s.DrawdownPct != 12 || len(s.Receipts) != 4 {
				t.Fatalf("wallet checkpoint=%+v err=%v", s, err)
			}
			encoded, err := json.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			var restored EquityCheckpoint
			if err = json.Unmarshal(encoded, &restored); err != nil {
				t.Fatal(err)
			}
			now = now.Add(time.Minute)
			o = testWalletObservation(now, skew, "1090", 1080, restored.BaseWallets["a"].From)
			withdraw := testWalletFlow("withdraw", "transfer_out", "-300", o.Wallets["a"].Through)
			o.Flows = []EquityCashFlow{oldFee, deposit, fee, loss, withdraw}
			next, err := nextEquityCheckpoint(&restored, o, now, time.Time{}, time.Minute, true)
			if err != nil || next.DrawdownPct != 12 || next.ExternalFlows != 200 || next.HighWater != 1000 {
				t.Fatalf("restart/withdrawal=%+v err=%v", next, err)
			}
		})
	}
}

func TestEquityWalletRejectsIncompleteOrConflictingProof(t *testing.T) {
	base := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	o := testWalletObservation(base, 0, "1000", 1000, time.Time{})
	old := testWalletFlow("prior-fee", "fee", "-1", o.Wallets["a"].Through.Add(-time.Second))
	o.Flows = []EquityCashFlow{old}
	previous, err := nextEquityCheckpoint(nil, o, base, time.Time{}, time.Minute, true)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(previous)
	for _, name := range []string{"missing_new_fee", "missing_old", "changed_old", "late_unseen", "duplicate_conflict", "wrong_account", "wrong_exact", "wrong_currency", "missing_exact", "future_receipt", "coverage_gap", "reversed_coverage", "raw_downgrade", "account_changed", "account_added", "cursor_regressed", "bad_clock", "capture_identity", "future_capture", "unclassified", "invalid_amount", "incomplete"} {
		t.Run(name, func(t *testing.T) {
			now := base.Add(time.Minute)
			o := testWalletObservation(now, 0, "990", 990, previous.BaseWallets["a"].From)
			fee := testWalletFlow("fee", "fee", "-10", o.Wallets["a"].Through)
			o.Flows = []EquityCashFlow{old, fee}
			wallet := o.Wallets["a"]
			switch name {
			case "missing_new_fee":
				o.Flows = o.Flows[:1]
			case "missing_old":
				o.Flows = o.Flows[1:]
			case "changed_old":
				o.Flows[0].Amount = -2
				o.Flows[0].ExactAmount = "-2"
			case "late_unseen":
				o.Flows = append(o.Flows, testWalletFlow("late", "fee", "0", previous.Wallets["a"].Through))
			case "duplicate_conflict":
				fee.Kind = "funding"
				o.Flows = append(o.Flows, fee)
			case "wrong_account":
				o.Flows[1].Account = "other"
			case "wrong_exact":
				o.Flows[1].ExactAmount = "-9"
			case "wrong_currency":
				o.Flows[1].Currency = "BNB"
			case "missing_exact":
				o.Flows[1].ExactAmount = ""
			case "future_receipt":
				o.Flows[1].At = wallet.Through.Add(time.Millisecond)
			case "coverage_gap":
				wallet.From = previous.Wallets["a"].Through
			case "reversed_coverage":
				wallet.From = wallet.Through.Add(time.Second)
			case "raw_downgrade":
				o.CashFlowComplete = false
			case "account_changed":
				delete(o.Wallets, "a")
				o.Wallets["b"] = wallet
			case "account_added":
				o.Wallets["b"] = wallet
			case "cursor_regressed":
				wallet.Through = previous.Wallets["a"].Through.Add(-time.Millisecond)
			case "bad_clock":
				wallet.Through = now.Add(time.Minute)
			case "capture_identity":
				wallet.ObservedAt = now.Add(-time.Second)
			case "future_capture":
				wallet.ObservedAt = now.Add(time.Second)
			case "unclassified":
				o.Flows[1].Kind = "unclassified"
			case "invalid_amount":
				wallet.Balance = "NaN"
			case "incomplete":
				o.Wallets = nil
			}
			if _, ok := o.Wallets["a"]; ok {
				o.Wallets["a"] = wallet
			}
			if _, err := nextEquityCheckpoint(&previous, o, now, time.Time{}, time.Minute, true); err == nil {
				t.Fatal("invalid proof accepted")
			}
			after, _ := json.Marshal(previous)
			if string(after) != string(before) {
				t.Fatal("failed observation mutated durable baseline")
			}
		})
	}
}

func TestEquityWalletNoToleranceForMissingDecimalDebit(t *testing.T) {
	base := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	o := testWalletObservation(base, 0, "1000", 1000, time.Time{})
	s, err := nextEquityCheckpoint(nil, o, base, time.Time{}, time.Minute, true)
	if err != nil {
		t.Fatal(err)
	}
	now := base.Add(time.Minute)
	o = testWalletObservation(now, 0, "999.999999999999999999", 1000, s.BaseWallets["a"].From)
	if _, err := nextEquityCheckpoint(&s, o, now, time.Time{}, time.Minute, true); err == nil {
		t.Fatal("float rounding hid missing debit")
	}
	o.Flows = []EquityCashFlow{testWalletFlow("tiny-fee", "fee", "-0.000000000000000001", o.Wallets["a"].Through)}
	if _, err := nextEquityCheckpoint(&s, o, now, time.Time{}, time.Minute, true); err != nil {
		t.Fatal(err)
	}
}

func TestEquityWalletModeRequiresExplicitReset(t *testing.T) {
	base := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	o := testEquityObservation(base, 1000)
	o.Scope = "wallet-a:futures"
	s, err := nextEquityCheckpoint(nil, o, base, time.Time{}, time.Minute, true)
	if err != nil {
		t.Fatal(err)
	}
	now := base.Add(time.Minute)
	o = testWalletObservation(now, 0, "900", 900, time.Time{})
	if _, err := nextEquityCheckpoint(&s, o, now, time.Time{}, time.Minute, true); err == nil {
		t.Fatal("silent wallet rebaseline")
	}
	reset, err := nextEquityCheckpoint(&s, o, now, now, time.Minute, true)
	if err != nil || reset.HighWater != 900 || reset.Revision != 2 {
		t.Fatalf("explicit reset=%+v err=%v", reset, err)
	}
	now = now.Add(time.Second)
	o = testWalletObservation(now, 0, "900", 900, reset.Wallets["a"].From)
	wallet := o.Wallets["a"]
	delete(o.Wallets, "a")
	o.Wallets["b"] = wallet
	if _, err := nextEquityCheckpoint(&reset, o, now, now, time.Minute, true); err == nil {
		t.Fatal("reset bypassed account identity")
	}
}

func TestEquityWalletReconcilesEachAccountNotJustAggregate(t *testing.T) {
	base := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	o := testWalletObservation(base, -time.Second, "1000", 1200, time.Time{})
	b := testWalletObservation(base, time.Second, "200", 200, time.Time{}).Wallets["a"]
	o.Wallets["b"] = b
	s, err := nextEquityCheckpoint(nil, o, base, time.Time{}, time.Minute, true)
	if err != nil {
		t.Fatal(err)
	}
	now := base.Add(time.Minute)
	o = testWalletObservation(now, -time.Second, "990", 1200, s.BaseWallets["a"].From)
	o.Wallets["b"] = testWalletObservation(now, time.Second, "210", 210, s.BaseWallets["b"].From).Wallets["a"]
	if _, err := nextEquityCheckpoint(&s, o, now, time.Time{}, time.Minute, true); err == nil {
		t.Fatal("offsetting missing records hidden by aggregate balance")
	}
	out := testWalletFlow("a:transfer", "transfer_out", "-10", o.Wallets["a"].Through)
	in := testWalletFlow("b:transfer", "transfer_in", "10", o.Wallets["b"].Through)
	in.Account = "b"
	o.Flows = []EquityCashFlow{out, in}
	next, err := nextEquityCheckpoint(&s, o, now, time.Time{}, time.Minute, true)
	if err != nil || next.DrawdownPct != 0 || next.ExternalFlows != 0 {
		t.Fatalf("account transfer=%+v err=%v", next, err)
	}
}

func TestEquityWalletCorruptDurableProofRejected(t *testing.T) {
	base := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	o := testWalletObservation(base, 0, "1000", 1000, time.Time{})
	s, err := nextEquityCheckpoint(nil, o, base, time.Time{}, time.Minute, true)
	if err != nil {
		t.Fatal(err)
	}
	now := base.Add(time.Minute)
	o = testWalletObservation(now, 0, "990", 990, s.BaseWallets["a"].From)
	o.Flows = []EquityCashFlow{testWalletFlow("fee", "fee", "-10", o.Wallets["a"].Through)}
	s, err = nextEquityCheckpoint(&s, o, now, time.Time{}, time.Minute, true)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(s)
	for _, name := range []string{"receipt_removed", "balance_changed", "exact_amount_changed", "external_changed", "baseline_missing", "unadjusted", "observation_changed"} {
		t.Run(name, func(t *testing.T) {
			var broken EquityCheckpoint
			if err := json.Unmarshal(data, &broken); err != nil {
				t.Fatal(err)
			}
			switch name {
			case "receipt_removed":
				delete(broken.Receipts, "fee")
			case "balance_changed":
				w := broken.Wallets["a"]
				w.Balance = "991"
				broken.Wallets["a"] = w
			case "exact_amount_changed":
				f := broken.Receipts["fee"]
				f.ExactAmount = "-9"
				broken.Receipts["fee"] = f
			case "external_changed":
				broken.ExternalFlows = 1
			case "baseline_missing":
				broken.BaseWallets = nil
			case "unadjusted":
				broken.CashFlowAdjusted = false
			case "observation_changed":
				broken.LastAt = broken.LastAt.Add(time.Second)
			}
			if err := broken.validate(); err == nil {
				t.Fatal("corrupt proof accepted")
			}
		})
	}
}

func TestEquityWalletHealthGateReleasesOnlyAfterReconciliationAndSave(t *testing.T) {
	cfg := newCircuitBreakerTestConfig()
	cfg.Triggers.MaxDrawdown.Enabled = true
	bot := &ownedEquityBot{}
	gcb := NewGlobalCircuitBreaker(cfg, nil, &circuitBreakerMockProvider{bots: []BotController{bot}})
	cfg.Enabled = true
	now := time.Now().Add(-time.Second)
	source := &fakeAccountLedgerSource{observation: testWalletObservation(now, 0, "1000", 1000, time.Time{})}
	store := &memoryEquityStore{saveErr: errors.New("unavailable")}
	feeder := NewMetricsFeeder(gcb, nil, source, nil, MetricsFeederOptions{Now: func() time.Time { return now }, EquityStore: store, RequirePersistence: true, RequireCashFlowReconciliation: true})
	if _, err := feeder.Tick(t.Context()); err == nil || !bot.held {
		t.Fatal("missing checkpoint did not hold opening")
	}
	store.saveErr = nil
	if _, err := feeder.Tick(t.Context()); err != nil || bot.held || !gcb.GetMetricsHealth().verifiedDrawdown() {
		t.Fatalf("verified baseline not released: %v", err)
	}
	from := source.observation.Wallets["a"].From
	now = now.Add(100 * time.Millisecond)
	source.observation = testWalletObservation(now, 0, "990", 990, from)
	if _, err := feeder.Tick(t.Context()); err == nil || !bot.held || gcb.GetMetricsHealth().Available {
		t.Fatal("unreconciled wallet was healthy")
	}
	source.observation.Flows = []EquityCashFlow{testWalletFlow("fee", "fee", "-10", source.observation.Wallets["a"].Through)}
	if _, err := feeder.Tick(t.Context()); err != nil || bot.held || bot.resumeCount != 0 {
		t.Fatalf("verified recovery did not clear only its own hold: %v", err)
	}
}

type fakeAccountLedgerSource struct {
	fakeEquitySource
	observation EquityObservation
	cursors     map[string]time.Time
}

func (s *fakeAccountLedgerSource) ObserveAccountEquity(_ context.Context, cursors map[string]time.Time) (EquityObservation, error) {
	s.cursors = cursors
	return s.observation, s.err
}

func TestEquityWalletFeederPersistsCursorsAndRetainsStateOnFailure(t *testing.T) {
	base := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	now := base
	source := &fakeAccountLedgerSource{observation: testWalletObservation(now, time.Second, "1000", 1000, time.Time{})}
	store := &memoryEquityStore{}
	opts := MetricsFeederOptions{Now: func() time.Time { return now }, EquityStore: store, RequirePersistence: true, RequireCashFlowReconciliation: true}
	feeder := NewMetricsFeeder(&fakeSink{}, nil, source, nil, opts)
	if _, err := feeder.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(source.cursors) != 0 {
		t.Fatal("initial read received invented cursors")
	}
	initialFrom := source.observation.Wallets["a"].From
	now = base.Add(time.Minute)
	source.observation = testWalletObservation(now, time.Second, "990", 990, initialFrom)
	fee := testWalletFlow("fee", "fee", "-10", source.observation.Wallets["a"].Through)
	source.observation.Flows = []EquityCashFlow{fee}
	store.saveErr = errors.New("write failed")
	if _, err := feeder.Tick(t.Context()); err == nil {
		t.Fatal("unpersisted evidence published")
	}
	if !source.cursors["a"].Equal(initialFrom) || store.revision != 1 || feeder.equityState.HighWater != 1000 {
		t.Fatal("cursor/baseline changed after failed save")
	}
	store.saveErr = nil
	feeder = NewMetricsFeeder(&fakeSink{}, nil, source, nil, opts)
	snap, err := feeder.Tick(t.Context())
	if err != nil || snap.MaxDrawdownPct != 1 || !snap.CashFlowAdjusted || store.revision != 2 {
		t.Fatalf("restart=%+v err=%v", snap, err)
	}
	encoded := string(store.data)
	now = now.Add(time.Minute)
	source.observation = testWalletObservation(now, time.Second, "989", 989, initialFrom)
	source.observation.Flows = []EquityCashFlow{fee} // missing debit
	if _, err := feeder.Tick(t.Context()); err == nil || string(store.data) != encoded {
		t.Fatal("unreconciled ledger advanced durable state")
	}
}

func TestEquityWalletSourceCannotCertifyWithoutWalletProof(t *testing.T) {
	now := time.Now()
	source := &fakeAccountLedgerSource{observation: testEquityObservation(now, 1000)}
	store := &memoryEquityStore{}
	feeder := NewMetricsFeeder(&fakeSink{}, nil, source, nil, MetricsFeederOptions{Now: func() time.Time { return now }, EquityStore: store})
	if _, err := feeder.Tick(t.Context()); err == nil || store.revision != 0 {
		t.Fatal("account source bypassed wallet proof")
	}
}
