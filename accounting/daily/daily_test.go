package daily

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

const testDate = "2026-11-03"

var (
	testStart = mustTime("2026-11-02T16:00:00Z")
	testEnd   = mustTime("2026-11-03T16:00:00Z")
	testNow   = mustTime("2026-11-03T16:10:00Z")
)

func mustTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return parsed
}

func testScope() Scope {
	return Scope{
		Exchange:         "binance",
		AccountScope:     "fixture-scope",
		Testnet:          false,
		Revision:         7,
		Wallets:          []string{WalletFutures, WalletSpot, WalletMargin},
		HistoryStart:     testStart.AddDate(0, 0, -30),
		EffectiveFrom:    testStart.AddDate(0, 0, -1),
		EffectiveThrough: testEnd.AddDate(0, 0, 1),
	}
}

func testCoverage(source, wallet, symbol string, observed time.Time) Coverage {
	digest := strings.Repeat("a", 64)
	if source == SourceInterest {
		digest = strings.Repeat("b", 64)
	}
	return Coverage{
		Source:        source,
		Wallet:        wallet,
		Symbol:        symbol,
		From:          testStart.Add(-time.Hour),
		Through:       testEnd,
		ObservedAt:    observed,
		Complete:      true,
		Pages:         1,
		ExpectedCount: 1,
		ActualCount:   1,
		Digest:        digest,
	}
}

func usdtBalance(wallet, quantity, principal, interest string, at time.Time) Balance {
	return Balance{
		Wallet:      wallet,
		Asset:       Asset,
		Quantity:    quantity,
		Principal:   principal,
		Interest:    interest,
		Price:       "1",
		PriceSource: "fixture-usdt",
		PriceAt:     at,
	}
}

func snapshotAt(sourceID string, at, observed time.Time, balances []Balance, positions []Position) Snapshot {
	return Snapshot{
		SourceID:      sourceID,
		At:            at,
		ObservedAt:    observed,
		ScopeRevision: testScope().Revision,
		Complete:      true,
		Balances:      balances,
		Positions:     positions,
	}
}

func minimalInputs() Inputs {
	evidence := Evidence{
		Scope:    testScope(),
		Date:     testDate,
		Timezone: Timezone,
		Method:   MethodVersion,
		Opening: snapshotAt("open-snapshot", testStart, testStart.Add(time.Hour), []Balance{
			usdtBalance(WalletSpot, "500", "0", "0", testStart),
			usdtBalance(WalletMargin, "400", "0", "0", testStart),
			usdtBalance(WalletFutures, "100", "0", "0", testStart),
		}, nil),
		Closing: snapshotAt("close-snapshot", testEnd, testEnd.Add(time.Minute), []Balance{
			usdtBalance(WalletSpot, "500", "0", "0", testEnd),
			usdtBalance(WalletMargin, "400", "0", "0", testEnd),
			usdtBalance(WalletFutures, "100", "0", "0", testEnd),
		}, nil),
		Coverage: []Coverage{
			testCoverage(SourceCashflow, WalletSpot, "", testEnd.Add(time.Minute)),
			testCoverage(SourceCashflow, WalletMargin, "", testEnd.Add(time.Minute)),
			testCoverage(SourceCashflow, WalletFutures, "", testEnd.Add(time.Minute)),
			testCoverage(SourcePositions, WalletFutures, "", testEnd.Add(time.Minute)),
			testCoverage(SourceExecution, WalletSpot, "", testEnd.Add(time.Minute)),
			testCoverage(SourceExecution, WalletMargin, "", testEnd.Add(time.Minute)),
			testCoverage(SourceExecution, WalletFutures, "", testEnd.Add(time.Minute)),
			testCoverage(SourceFunding, WalletFutures, "", testEnd.Add(time.Minute)),
		},
		FuturesSymbols: nil,
	}
	return Inputs{
		Evidence:         evidence,
		EvidenceRevision: 3,
		LedgerCoverage: []Coverage{
			testCoverage(SourceInterest, WalletMargin, "*", testEnd.Add(6*time.Minute)),
		},
	}
}

func goldenInputs() Inputs {
	inputs := minimalInputs()
	mid := testStart.Add(12 * time.Hour)
	intTime := mid
	fillTime := mid.Add(time.Minute)
	feeTime := mid.Add(2 * time.Minute)
	fundTime := mid.Add(3 * time.Minute)

	inputs.Evidence.FuturesSymbols = []string{"ETHUSDT", "BTCUSDT"}
	inputs.Evidence.Opening.Balances = []Balance{
		usdtBalance(WalletSpot, "500", "0", "0", testStart),
		usdtBalance(WalletMargin, "400", "0", "0", testStart),
		usdtBalance(WalletFutures, "100", "0", "0", testStart),
		{
			Wallet: WalletMargin, Asset: "XYZ", Quantity: "0.1", Principal: "0", Interest: "0",
			Price: "200", PriceSource: "fixture-price", PriceAt: testStart,
		},
	}
	inputs.Evidence.Closing.Balances = []Balance{
		usdtBalance(WalletSpot, "548", "0", "0", testEnd),
		usdtBalance(WalletMargin, "700", "300", "1", testEnd),
		usdtBalance(WalletFutures, "231", "0", "0", testEnd),
		{
			Wallet: WalletMargin, Asset: "XYZ", Quantity: "0.1", Principal: "0", Interest: "0",
			Price: "240", PriceSource: "fixture-price", PriceAt: testEnd,
		},
	}
	inputs.Evidence.Opening.Positions = []Position{
		{Symbol: "BTCUSDT", Quantity: "1", EntryPrice: "100", MarkPrice: "100", Unrealized: "0", PriceSource: "fixture-mark", PriceAt: testStart},
		{Symbol: "ETHUSDT", Quantity: "2", EntryPrice: "50", MarkPrice: "50", Unrealized: "0", PriceSource: "fixture-mark", PriceAt: testStart},
	}
	inputs.Evidence.Closing.Positions = []Position{
		{Symbol: "ETHUSDT", Quantity: "2", EntryPrice: "50", MarkPrice: "54", Unrealized: "8", PriceSource: "fixture-mark", PriceAt: testEnd},
	}
	inputs.Evidence.Events = []Event{
		{Source: SourceCashflow, SourceID: "dep-1", EconomicID: "eco-dep", Wallet: WalletSpot, Asset: Asset, At: mid, Kind: Deposit, Amount: "200"},
		{Source: SourceCashflow, SourceID: "wd-1", EconomicID: "eco-wd", Wallet: WalletSpot, Asset: Asset, At: mid, Kind: Withdrawal, Amount: "-50"},
		{Source: SourceCashflow, SourceID: "fee-1", EconomicID: "eco-other-fee", Wallet: WalletSpot, Asset: Asset, At: mid, Kind: OtherFee, Amount: "-2"},
		{Source: SourceCashflow, SourceID: "tr-out", EconomicID: "eco-transfer", Wallet: WalletSpot, Asset: Asset, At: mid, Kind: Transfer, Amount: "-100", TransferID: "transfer-1", CounterpartyWallet: WalletFutures},
		{Source: SourceCashflow, SourceID: "tr-in", EconomicID: "eco-transfer", Wallet: WalletFutures, Asset: Asset, At: mid, Kind: Transfer, Amount: "100", TransferID: "transfer-1", CounterpartyWallet: WalletSpot},
		{Source: SourceCashflow, SourceID: "borrow-1", EconomicID: "eco-borrow", Wallet: WalletMargin, Asset: Asset, At: mid, Kind: BorrowPrincipal, Amount: "500", PrincipalDelta: "500"},
		{Source: SourceCashflow, SourceID: "repay-1", EconomicID: "eco-repay", Wallet: WalletMargin, Asset: Asset, At: mid, Kind: RepayPrincipal, Amount: "-200", PrincipalDelta: "-200"},
		{Source: "margin_interest", SourceID: "interest-event-1", EconomicID: "eco-interest", Wallet: WalletMargin, Asset: Asset, At: intTime, Kind: InterestAccrual, Amount: "0", InterestDelta: "1", LedgerRef: "interest-1"},
		{Source: SourceExecution, SourceID: "fill-event-1", EconomicID: "eco-fill", Wallet: WalletFutures, Asset: Asset, At: fillTime, Kind: RealizedPnL, Amount: "30", LedgerRef: "fill-1"},
		{Source: SourceExecution, SourceID: "trading-fee-event-1", EconomicID: "eco-fee", Wallet: WalletFutures, Asset: Asset, At: feeTime, Kind: TradingFee, Amount: "-2", LedgerRef: "fee-1"},
		{Source: SourceExecution, SourceID: "funding-event-1", EconomicID: "eco-funding", Wallet: WalletFutures, Asset: Asset, At: fundTime, Kind: Funding, Amount: "3", LedgerRef: "funding-1"},
	}
	inputs.Entries = []LedgerEntry{
		{ID: "interest-1", Kind: InterestAccrual, Market: "margin", Asset: Asset, At: intTime, Amount: "-1", Known: true},
		{ID: "fill-1", Kind: RealizedPnL, Market: "futures", Symbol: "BTCUSDT", Asset: Asset, At: fillTime, Amount: "30", Known: true, Quantity: "1", Side: "sell"},
		{ID: "fee-1", Kind: TradingFee, Market: "futures", Symbol: "BTCUSDT", Asset: Asset, At: feeTime, Amount: "-2", Known: true},
		{ID: "funding-1", Kind: Funding, Market: "futures", Symbol: "BTCUSDT", Asset: Asset, At: fundTime, Amount: "3", Known: true},
	}
	inputs.LedgerCoverage = append(inputs.LedgerCoverage,
		testCoverage(SourceExecution, WalletFutures, "BTCUSDT", testEnd.Add(time.Minute)),
		testCoverage(SourceExecution, WalletFutures, "ETHUSDT", testEnd.Add(time.Minute)),
		testCoverage(SourceFunding, WalletFutures, "BTCUSDT", testEnd.Add(time.Minute)),
		testCoverage(SourceFunding, WalletFutures, "ETHUSDT", testEnd.Add(time.Minute)),
	)
	return inputs
}

func mustEvaluate(t *testing.T, inputs Inputs, now time.Time) Result {
	t.Helper()
	result, err := Evaluate(inputs, now)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	return result
}

func requireComplete(t *testing.T, result Result) {
	t.Helper()
	if !result.EquityReconciliationComplete {
		t.Fatalf("expected complete reconciliation, reasons=%v amounts=%+v", result.Reasons, result.Amounts)
	}
	if len(result.Reasons) != 0 {
		t.Fatalf("expected no reasons, got %v", result.Reasons)
	}
}

func requireReason(t *testing.T, result Result, reason string) {
	t.Helper()
	if result.EquityReconciliationComplete {
		t.Fatalf("expected incomplete result carrying %q", reason)
	}
	for _, candidate := range result.Reasons {
		if candidate == reason {
			return
		}
	}
	t.Fatalf("reason %q not found in %v", reason, result.Reasons)
}

func TestDayBounds(t *testing.T) {
	start, end, err := DayBounds(testDate)
	if err != nil {
		t.Fatalf("DayBounds: %v", err)
	}
	if !start.Equal(testStart) || !end.Equal(testEnd) {
		t.Fatalf("bounds = %s..%s, want %s..%s", start, end, testStart, testEnd)
	}
	if end.Sub(start) != 24*time.Hour {
		t.Fatalf("day length = %s", end.Sub(start))
	}
	for _, invalid := range []string{"", "2026-11-3", "2026-13-01", "03/11/2026", "2026-11-03T00:00:00Z"} {
		if _, _, err := DayBounds(invalid); !errors.Is(err, ErrInvalid) {
			t.Fatalf("DayBounds(%q) error = %v, want ErrInvalid", invalid, err)
		}
	}
}

func TestPrepareEvidenceCanonicalizesAndDedupes(t *testing.T) {
	first := Event{
		Source: SourceCashflow, SourceID: "dep-1", EconomicID: "eco", Wallet: WalletSpot,
		Asset: Asset, At: testStart.Add(2 * time.Hour), Kind: Deposit, Amount: "001.5000",
	}
	duplicate := first
	second := first
	second.SourceID = "dep-2"
	evidence := Evidence{
		Scope: testScope(), Date: testDate, Timezone: Timezone, Method: MethodVersion,
		Opening: snapshotAt("open", testStart, testStart, []Balance{usdtBalance(WalletSpot, "1.0", "", "", testStart)}, nil),
		Closing: snapshotAt("close", testEnd, testEnd, []Balance{usdtBalance(WalletSpot, "1.0", "", "", testEnd)}, nil),
		Events:  []Event{second, duplicate, first},
	}
	normalized, err := PrepareEvidence(evidence)
	if err != nil {
		t.Fatalf("PrepareEvidence: %v", err)
	}
	if len(normalized.Events) != 2 {
		t.Fatalf("expected identical duplicate removed, got %d events", len(normalized.Events))
	}
	if normalized.Events[0].Amount != "1.5" {
		t.Fatalf("amount not canonical: %q", normalized.Events[0].Amount)
	}
	if normalized.Events[0].PrincipalDelta != "0" || normalized.Events[0].InterestDelta != "0" {
		t.Fatalf("empty deltas were not canonicalized to zero: %+v", normalized.Events[0])
	}
	if normalized.Opening.Balances[0].Principal != "0" {
		t.Fatalf("empty principal was not canonicalized: %q", normalized.Opening.Balances[0].Principal)
	}

	conflicting := duplicate
	conflicting.Amount = "2"
	evidence.Events = append(evidence.Events, conflicting)
	if _, err := PrepareEvidence(evidence); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting duplicate error = %v, want ErrConflict", err)
	}
}

func TestEvidenceHashIsOrderIndependent(t *testing.T) {
	one := goldenInputs().Evidence
	hashed, err := EvidenceHash(one)
	if err != nil {
		t.Fatalf("EvidenceHash: %v", err)
	}
	shuffled := one
	shuffled.Scope.Wallets = []string{WalletSpot, WalletFutures, WalletMargin}
	shuffled.Events = append([]Event(nil), one.Events...)
	shuffled.Events[0], shuffled.Events[len(shuffled.Events)-1] = shuffled.Events[len(shuffled.Events)-1], shuffled.Events[0]
	shuffled.Coverage = append([]Coverage(nil), one.Coverage...)
	shuffled.Coverage[0], shuffled.Coverage[len(shuffled.Coverage)-1] = shuffled.Coverage[len(shuffled.Coverage)-1], shuffled.Coverage[0]
	shuffled.FuturesSymbols = []string{"BTCUSDT", "ETHUSDT", "BTCUSDT"}
	again, err := EvidenceHash(shuffled)
	if err != nil {
		t.Fatalf("EvidenceHash shuffled: %v", err)
	}
	if hashed != again {
		t.Fatalf("evidence hash changed under permutation: %s vs %s", hashed, again)
	}
	if len(hashed) != 64 {
		t.Fatalf("hash is not 64 hex chars: %q", hashed)
	}
}

func TestInputDigestBindsMutableLedgerFacts(t *testing.T) {
	inputs := minimalInputs()
	first, err := InputDigest(inputs)
	if err != nil {
		t.Fatalf("InputDigest: %v", err)
	}
	changed := minimalInputs()
	changed.EvidenceRevision = inputs.EvidenceRevision + 1
	second, err := InputDigest(changed)
	if err != nil {
		t.Fatalf("InputDigest changed: %v", err)
	}
	if first == second {
		t.Fatal("digest did not change with evidence revision")
	}
	changed = minimalInputs()
	changed.LedgerCoverage[0].Digest = strings.Repeat("c", 64)
	third, err := InputDigest(changed)
	if err != nil {
		t.Fatalf("InputDigest ledger coverage: %v", err)
	}
	if first == third {
		t.Fatal("digest did not change with ledger coverage")
	}
}

func TestDailyEquityFixtureReconcilesCompleteDay(t *testing.T) {
	result := mustEvaluate(t, goldenInputs(), testNow)
	requireComplete(t, result)

	want := Amounts{
		OpeningEquity:      "1020",
		ClosingEquity:      "1210",
		NetExternalCapital: "150",
		TradingGross:       "30",
		TradingFees:        "2",
		TradingNet:         "28",
		FundingNet:         "3",
		InterestCost:       "1",
		UnrealizedChange:   "8",
		ValuationChange:    "4",
		OtherCosts:         "2",
		EquityProfit:       "40",
		ExplainedProfit:    "40",
		Residual:           "0",
	}
	if result.Amounts != want {
		t.Fatalf("amounts mismatch:\n got %+v\nwant %+v", result.Amounts, want)
	}
	if !result.PeriodStart.Equal(testStart) || !result.PeriodEnd.Equal(testEnd) {
		t.Fatalf("period = %s..%s", result.PeriodStart, result.PeriodEnd)
	}
	if result.Timezone != Timezone || result.Asset != Asset || result.Tolerance != Tolerance {
		t.Fatalf("fixed contract fields missing: %+v", result)
	}
	if len(result.InputDigest) != 64 {
		t.Fatalf("input digest missing: %q", result.InputDigest)
	}
}

func TestDailyEquityFixtureFailsClosedOnUnsupportedActivity(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Inputs)
		now    time.Time
		reason string
	}{
		{
			name: "internal transfer missing leg",
			mutate: func(inputs *Inputs) {
				inputs.Evidence.Events = withoutEvent(inputs.Evidence.Events, "tr-in")
			},
			reason: reasonEventTransferLeg,
		},
		{
			name: "unknown fee blocks",
			mutate: func(inputs *Inputs) {
				for index := range inputs.Entries {
					if inputs.Entries[index].ID == "fee-1" {
						inputs.Entries[index].Known = false
					}
				}
			},
			reason: reasonLedgerUnknown,
		},
		{
			name: "non USDT liability blocks",
			mutate: func(inputs *Inputs) {
				for index := range inputs.Evidence.Closing.Balances {
					if inputs.Evidence.Closing.Balances[index].Asset == "XYZ" {
						inputs.Evidence.Closing.Balances[index].Principal = "1"
					}
				}
			},
			reason: reasonBalanceNonUSDTLiability,
		},
		{
			name: "spot trading inventory unsupported",
			mutate: func(inputs *Inputs) {
				for index := range inputs.Entries {
					if inputs.Entries[index].ID == "fill-1" {
						inputs.Entries[index].Market = "spot"
					}
				}
			},
			reason: reasonLedgerMarket,
		},
		{
			name: "scope revision changes within day",
			mutate: func(inputs *Inputs) {
				inputs.Evidence.Closing.ScopeRevision++
			},
			reason: reasonSnapshotRevision,
		},
		{
			name: "missing interest ledger window",
			mutate: func(inputs *Inputs) {
				inputs.LedgerCoverage = inputs.LedgerCoverage[1:]
			},
			reason: reasonInterestCoverage,
		},
		{
			name:   "before safety lag",
			now:    testEnd.Add(SafetyLag - time.Second),
			reason: reasonCoverageNow,
		},
		{
			name: "event at exclusive end",
			mutate: func(inputs *Inputs) {
				inputs.Evidence.Events = append(inputs.Evidence.Events, Event{
					Source: SourceCashflow, SourceID: "late", EconomicID: "eco-late",
					Wallet: WalletSpot, Asset: Asset, At: testEnd, Kind: Deposit, Amount: "5",
				})
			},
			reason: reasonEventOutside,
		},
		{
			name: "ledger fact at exclusive end",
			mutate: func(inputs *Inputs) {
				for index := range inputs.Entries {
					if inputs.Entries[index].ID == "interest-1" {
						inputs.Entries[index].At = testEnd
					}
				}
				for index := range inputs.Evidence.Events {
					if inputs.Evidence.Events[index].LedgerRef == "interest-1" {
						inputs.Evidence.Events[index].At = testEnd
					}
				}
			},
			reason: reasonLedgerTime,
		},
		{
			name: "conflicting economic identity",
			mutate: func(inputs *Inputs) {
				inputs.Evidence.Events = append(inputs.Evidence.Events, Event{
					Source: SourceExecution, SourceID: "funding-event-duplicate", EconomicID: "eco-funding",
					Wallet: WalletFutures, Asset: Asset, At: testStart.Add(13 * time.Hour), Kind: Funding,
					Amount: "4", LedgerRef: "funding-1",
				})
			},
			reason: reasonEventEconomicConflict,
		},
		{
			name: "unsupported activity kind",
			mutate: func(inputs *Inputs) {
				inputs.Evidence.Events = append(inputs.Evidence.Events, Event{
					Source: SourceCashflow, SourceID: "unknown-1", EconomicID: "eco-unknown",
					Wallet: WalletSpot, Asset: Asset, At: testStart.Add(3 * time.Hour), Kind: Unknown, Amount: "0",
				})
			},
			reason: reasonEventUnknown,
		},
		{
			name: "non USDT flow unsupported",
			mutate: func(inputs *Inputs) {
				inputs.Evidence.Events = append(inputs.Evidence.Events, Event{
					Source: SourceCashflow, SourceID: "xyz-dep", EconomicID: "eco-xyz",
					Wallet: WalletMargin, Asset: "XYZ", At: testStart.Add(3 * time.Hour), Kind: Deposit, Amount: "1",
				})
			},
			reason: reasonEventAsset,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			inputs := goldenInputs()
			if test.mutate != nil {
				test.mutate(&inputs)
			}
			now := testNow
			if !test.now.IsZero() {
				now = test.now
			}
			result := mustEvaluate(t, inputs, now)
			requireReason(t, result, test.reason)
		})
	}
}

func TestEvaluateTimeBoundaries(t *testing.T) {
	inputs := goldenInputs()
	for index := range inputs.Evidence.Events {
		if inputs.Evidence.Events[index].SourceID == "dep-1" {
			inputs.Evidence.Events[index].At = testStart
		}
	}
	requireComplete(t, mustEvaluate(t, inputs, testNow))

	inputs = goldenInputs()
	for index := range inputs.LedgerCoverage {
		if inputs.LedgerCoverage[index].Source == SourceInterest {
			inputs.LedgerCoverage[index].ObservedAt = testEnd.Add(SafetyLag)
		}
	}
	boundaryNow := testEnd.Add(SafetyLag)
	requireComplete(t, mustEvaluate(t, inputs, boundaryNow))
}

func TestEvaluateCrossSourceDuplicateIsEvidenceOnly(t *testing.T) {
	inputs := goldenInputs()
	fundTime := testStart.Add(12*time.Hour + 3*time.Minute)
	inputs.Entries = append(inputs.Entries, LedgerEntry{
		ID: "funding-2", Kind: Funding, Market: "futures", Symbol: "BTCUSDT",
		Asset: Asset, At: fundTime, Amount: "3", Known: true,
	})
	inputs.Evidence.Events = append(inputs.Evidence.Events, Event{
		Source: "secondary_source", SourceID: "funding-event-2", EconomicID: "eco-funding",
		Wallet: WalletFutures, Asset: Asset, At: fundTime, Kind: Funding,
		Amount: "3", LedgerRef: "funding-2",
	})
	inputs.Evidence.Events = append(inputs.Evidence.Events, Event{
		Source: "tertiary_source", SourceID: "funding-event-3", EconomicID: "eco-funding-other",
		Wallet: WalletFutures, Asset: Asset, At: fundTime, Kind: Funding,
		Amount: "3", LedgerRef: "funding-1",
	})
	result := mustEvaluate(t, inputs, testNow)
	requireComplete(t, result)
	if result.Amounts.FundingNet != "3" {
		t.Fatalf("cross-source duplicate double counted funding: %+v", result.Amounts)
	}
}

func TestEvaluateRepaymentComponentsMayShareEconomicID(t *testing.T) {
	inputs := minimalInputs()
	mid := testStart.Add(12 * time.Hour)
	inputs.Evidence.Opening.Balances = []Balance{
		usdtBalance(WalletSpot, "500", "0", "0", testStart),
		usdtBalance(WalletMargin, "901", "500", "1", testStart),
		usdtBalance(WalletFutures, "100", "0", "0", testStart),
	}
	inputs.Evidence.Closing.Balances = []Balance{
		usdtBalance(WalletSpot, "500", "0", "0", testEnd),
		usdtBalance(WalletMargin, "700", "300", "0", testEnd),
		usdtBalance(WalletFutures, "100", "0", "0", testEnd),
	}
	inputs.Evidence.Events = []Event{
		{
			Source: SourceCashflow, SourceID: "repay-component", EconomicID: "loan-1",
			Wallet: WalletMargin, Asset: Asset, At: mid, Kind: RepayPrincipal,
			Amount: "-200", PrincipalDelta: "-200",
		},
		{
			Source: SourceInterest, SourceID: "settle-component", EconomicID: "loan-1",
			Wallet: WalletMargin, Asset: Asset, At: mid, Kind: InterestSettlement,
			Amount: "-1", InterestDelta: "-1",
		},
	}
	result := mustEvaluate(t, inputs, testNow)
	requireComplete(t, result)
	if result.Amounts.InterestCost != "0" || result.Amounts.Residual != "0" {
		t.Fatalf("repayment components were mishandled: %+v", result.Amounts)
	}
}

func TestEvaluateInterestHistoryLimit(t *testing.T) {
	inputs := goldenInputs()
	observed := testStart.Add(91 * 24 * time.Hour)
	for index := range inputs.LedgerCoverage {
		if inputs.LedgerCoverage[index].Source == SourceInterest {
			inputs.LedgerCoverage[index].ObservedAt = observed
		}
	}
	result := mustEvaluate(t, inputs, testStart.Add(92*24*time.Hour))
	requireReason(t, result, reasonCoverageInterestHistory)
}

func TestEvaluateAccruedInterestSettlementIsNotDoubleCharged(t *testing.T) {
	inputs := minimalInputs()
	mid := testStart.Add(12 * time.Hour)
	inputs.Evidence.Opening.Balances = []Balance{
		usdtBalance(WalletSpot, "500", "0", "0", testStart),
		usdtBalance(WalletMargin, "401", "0", "1", testStart),
		usdtBalance(WalletFutures, "100", "0", "0", testStart),
	}
	inputs.Evidence.Closing.Balances = []Balance{
		usdtBalance(WalletSpot, "500", "0", "0", testEnd),
		usdtBalance(WalletMargin, "400", "0", "0", testEnd),
		usdtBalance(WalletFutures, "100", "0", "0", testEnd),
	}
	inputs.Evidence.Events = []Event{{
		Source: SourceCashflow, SourceID: "settle-1", EconomicID: "eco-settle",
		Wallet: WalletMargin, Asset: Asset, At: mid, Kind: InterestSettlement,
		Amount: "-1", InterestDelta: "-1",
	}}
	result := mustEvaluate(t, inputs, testNow)
	requireComplete(t, result)
	if result.Amounts.InterestCost != "0" || result.Amounts.Residual != "0" {
		t.Fatalf("accrued interest was charged twice: %+v", result.Amounts)
	}
	if result.Amounts.OpeningEquity != "1000" || result.Amounts.ClosingEquity != "1000" {
		t.Fatalf("equity changed on settlement: %+v", result.Amounts)
	}
}

func TestEvaluateBorrowAndRepayDoNotBecomeExternalFlow(t *testing.T) {
	inputs := minimalInputs()
	mid := testStart.Add(12 * time.Hour)
	inputs.Evidence.Closing.Balances = []Balance{
		usdtBalance(WalletSpot, "500", "0", "0", testEnd),
		usdtBalance(WalletMargin, "900", "500", "0", testEnd),
		usdtBalance(WalletFutures, "100", "0", "0", testEnd),
	}
	inputs.Evidence.Events = []Event{{
		Source: SourceCashflow, SourceID: "borrow-only", EconomicID: "eco-borrow-only",
		Wallet: WalletMargin, Asset: Asset, At: mid, Kind: BorrowPrincipal,
		Amount: "500", PrincipalDelta: "500",
	}}
	result := mustEvaluate(t, inputs, testNow)
	requireComplete(t, result)
	if result.Amounts.NetExternalCapital != "0" || result.Amounts.Residual != "0" {
		t.Fatalf("borrow became external capital: %+v", result.Amounts)
	}

	inputs = minimalInputs()
	inputs.Evidence.Events = []Event{{
		Source: SourceCashflow, SourceID: "borrow-bad", EconomicID: "eco-borrow-bad",
		Wallet: WalletMargin, Asset: Asset, At: mid, Kind: BorrowPrincipal,
		Amount: "500", PrincipalDelta: "400",
	}}
	result = mustEvaluate(t, inputs, testNow)
	requireReason(t, result, reasonEventPrincipalDelta)
}

func TestEvaluateRejectsMalformedAndHugeNumbers(t *testing.T) {
	for _, value := range []string{"NaN", "Inf", "1e309", strings.Repeat("9", 100), "1/2", "0x10", "1.2.3"} {
		inputs := minimalInputs()
		inputs.Evidence.Opening.Balances[0].Quantity = value
		if _, err := Evaluate(inputs, testNow); !errors.Is(err, ErrInvalid) {
			t.Fatalf("Evaluate quantity %q error = %v, want ErrInvalid", value, err)
		}
	}
}

func TestEvaluateDepositFeeIsCostNotExternalFlow(t *testing.T) {
	inputs := minimalInputs()
	mid := testStart.Add(12 * time.Hour)
	inputs.Evidence.Closing.Balances = []Balance{
		usdtBalance(WalletSpot, "599", "0", "0", testEnd),
		usdtBalance(WalletMargin, "400", "0", "0", testEnd),
		usdtBalance(WalletFutures, "100", "0", "0", testEnd),
	}
	inputs.Evidence.Events = []Event{
		{
			Source: SourceCashflow, SourceID: "deposit-with-fee", EconomicID: "eco-deposit-fee",
			Wallet: WalletSpot, Asset: Asset, At: mid, Kind: Deposit, Amount: "100",
		},
		{
			Source: SourceCashflow, SourceID: "deposit-fee", EconomicID: "eco-deposit-fee-cost",
			Wallet: WalletSpot, Asset: Asset, At: mid, Kind: OtherFee, Amount: "-1",
		},
	}
	result := mustEvaluate(t, inputs, testNow)
	requireComplete(t, result)
	if result.Amounts.NetExternalCapital != "100" || result.Amounts.OtherCosts != "1" ||
		result.Amounts.EquityProfit != "-1" || result.Amounts.Residual != "0" {
		t.Fatalf("deposit fee classification mismatch: %+v", result.Amounts)
	}
}

func TestEvaluateRepayOnlyDoesNotCancelBorrow(t *testing.T) {
	inputs := minimalInputs()
	mid := testStart.Add(12 * time.Hour)
	inputs.Evidence.Opening.Balances = []Balance{
		usdtBalance(WalletSpot, "500", "0", "0", testStart),
		usdtBalance(WalletMargin, "900", "500", "0", testStart),
		usdtBalance(WalletFutures, "100", "0", "0", testStart),
	}
	inputs.Evidence.Closing.Balances = []Balance{
		usdtBalance(WalletSpot, "500", "0", "0", testEnd),
		usdtBalance(WalletMargin, "700", "300", "0", testEnd),
		usdtBalance(WalletFutures, "100", "0", "0", testEnd),
	}
	inputs.Evidence.Events = []Event{{
		Source: SourceCashflow, SourceID: "repay-only", EconomicID: "eco-repay-only",
		Wallet: WalletMargin, Asset: Asset, At: mid, Kind: RepayPrincipal,
		Amount: "-200", PrincipalDelta: "-200",
	}}
	result := mustEvaluate(t, inputs, testNow)
	requireComplete(t, result)
	if result.Amounts.NetExternalCapital != "0" || result.Amounts.EquityProfit != "0" {
		t.Fatalf("repay-only changed profit: %+v", result.Amounts)
	}
}

func TestEvaluateMergesInputBlockers(t *testing.T) {
	inputs := minimalInputs()
	requireComplete(t, mustEvaluate(t, inputs, testNow))

	inputs.Blockers = []string{"pending_fee_correction"}
	result := mustEvaluate(t, inputs, testNow)
	requireReason(t, result, "pending_fee_correction")
}

func TestPrepareEvidenceDedupesExactCoverageReceipts(t *testing.T) {
	inputs := minimalInputs()
	duplicate := inputs.Evidence.Coverage[0]
	inputs.Evidence.Coverage = append(inputs.Evidence.Coverage, duplicate)
	normalized, err := PrepareEvidence(inputs.Evidence)
	if err != nil {
		t.Fatalf("PrepareEvidence: %v", err)
	}
	if len(normalized.Coverage) != len(minimalInputs().Evidence.Coverage) {
		t.Fatalf("exact duplicate coverage was not deduplicated: %d", len(normalized.Coverage))
	}
}

func withoutEvent(events []Event, sourceID string) []Event {
	out := make([]Event, 0, len(events))
	for _, event := range events {
		if event.SourceID == sourceID {
			continue
		}
		out = append(out, event)
	}
	return out
}

type fakeRepository struct {
	inputs       Inputs
	inputsErr    error
	result       Result
	resultErr    error
	commitDigest string
	commitCalls  int
	commitErr    error
}

func (repository *fakeRepository) ImportDailyEquityEvidence(context.Context, Evidence, int64) (int64, error) {
	return 0, errors.New("not implemented")
}

func (repository *fakeRepository) LoadDailyEquityInputs(context.Context, Query) (Inputs, error) {
	if repository.inputsErr != nil {
		return Inputs{}, repository.inputsErr
	}
	return repository.inputs, nil
}

func (repository *fakeRepository) CommitDailyEquityResult(_ context.Context, _ Query, digest string, result Result) (Result, error) {
	repository.commitCalls++
	repository.commitDigest = digest
	if repository.commitErr != nil {
		return Result{}, repository.commitErr
	}
	repository.result = result
	return result, nil
}

func (repository *fakeRepository) LoadDailyEquityResult(context.Context, Query) (Result, error) {
	if repository.resultErr != nil {
		return Result{}, repository.resultErr
	}
	return repository.result, nil
}

func TestServiceReconcileCommitsExpectedFullDigest(t *testing.T) {
	inputs := minimalInputs()
	expected := mustEvaluate(t, inputs, testNow)
	repository := &fakeRepository{inputs: inputs}
	service := NewService(repository)
	result, err := service.Reconcile(context.Background(), expected.Query, testNow)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	requireComplete(t, result)
	if repository.commitCalls != 1 {
		t.Fatalf("commit calls = %d", repository.commitCalls)
	}
	if repository.commitDigest != expected.InputDigest || result.InputDigest != expected.InputDigest {
		t.Fatalf("commit digest = %q, want %q", repository.commitDigest, expected.InputDigest)
	}

	repository.commitErr = ErrConflict
	if _, err := service.Reconcile(context.Background(), expected.Query, testNow); !errors.Is(err, ErrConflict) {
		t.Fatalf("commit conflict error = %v, want ErrConflict", err)
	}
}

func TestServiceReadFlagsChangedInputsWithoutOverwriting(t *testing.T) {
	inputs := minimalInputs()
	expected := mustEvaluate(t, inputs, testNow)
	requireComplete(t, expected)
	repository := &fakeRepository{inputs: inputs, result: expected}
	service := NewService(repository)

	read, err := service.Read(context.Background(), expected.Query, testNow)
	if err != nil {
		t.Fatalf("Read current: %v", err)
	}
	requireComplete(t, read)

	repository.inputs.LedgerCoverage[0].Digest = strings.Repeat("c", 64)
	read, err = service.Read(context.Background(), expected.Query, testNow)
	if err != nil {
		t.Fatalf("Read changed: %v", err)
	}
	requireReason(t, read, reasonSourceChanged)
	if !repository.result.EquityReconciliationComplete {
		t.Fatal("Read mutated the persisted historical result")
	}
}

func TestServiceReadMissingReport(t *testing.T) {
	service := NewService(&fakeRepository{resultErr: ErrNotFound})
	_, err := service.Read(context.Background(), Query{Exchange: "binance", AccountScope: "fixture-scope", Date: testDate}, testNow)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Read error = %v, want ErrNotFound", err)
	}
}

func TestServiceRejectsMismatchedEvidence(t *testing.T) {
	inputs := minimalInputs()
	inputs.Evidence.Scope.AccountScope = "other-scope"
	service := NewService(&fakeRepository{inputs: inputs})
	query := Query{Exchange: "binance", AccountScope: "fixture-scope", Date: testDate}
	if _, err := service.Reconcile(context.Background(), query, testNow); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Reconcile error = %v, want ErrInvalid", err)
	}
}
