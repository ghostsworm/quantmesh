package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/storage"
)

type fundingCarryRepayIntentExchange struct {
	*mockFCExchange
	queryErr   error
	afterRepay func()
	repayErr   error
	noID       bool
}

func (e *fundingCarryRepayIntentExchange) Repay(ctx context.Context, asset string, amount float64) (int64, error) {
	id, err := e.mockFCExchange.Repay(ctx, asset, amount)
	if e.afterRepay != nil {
		e.afterRepay()
	}
	if e.noID {
		id = 0
	}
	return id, errors.Join(err, e.repayErr)
}

func (e *fundingCarryRepayIntentExchange) GetMarginTransactionByID(ctx context.Context, asset, kind string, id int64) (exchange.MarginBorrowRecord, error) {
	if kind == "REPAY" && e.queryErr != nil {
		return exchange.MarginBorrowRecord{}, e.queryErr
	}
	return e.mockFCExchange.GetMarginTransactionByID(ctx, asset, kind, id)
}

func TestFundingCarryRepaymentRetryQueriesAckWithoutResubmitting(t *testing.T) {
	s, margin, store := newFundingCarryRepayIntentFixture()
	if err := s.closeReverse(context.Background(), "lookup_failure"); err == nil {
		t.Fatal("query failure ignored")
	}
	var saved fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(store.payload), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.MarginRepayIntent == nil || saved.MarginRepayIntent.TransferID != 1 || saved.MarginRepayIntent.Amount != 0.4 || saved.MarginRepayIntent.BorrowTransferID != 42 || saved.MarginRepayIntent.AccountScope != "scope-a" {
		t.Fatal("exact accepted intent lost")
	}
	margin.queryErr = nil
	if err := s.repayMarginPrincipal(context.Background(), "BTC", 0.4, 0); err != nil {
		t.Fatal(err)
	}
	if margin.repayCalls != 1 || s.marginDebt != 0 || s.marginRepayIntent != nil || len(s.marginDebtEvents) != 1 || !s.unownedExposure || !s.intentInFlight {
		t.Fatal("ACK recovery resubmitted RPC or falsely completed whole operation")
	}
}

func TestFundingCarryRepayIntentFaultsNeverRepeatRPC(t *testing.T) {
	for _, mode := range []string{"ack_error", "no_id", "save_failure", "owner_lost"} {
		t.Run(mode, func(t *testing.T) {
			s, margin, store := newFundingCarryRepayIntentFixture()
			s.intentInFlight = true
			gate := &execution.OpeningGate{}
			s.SetOpeningGate(gate)
			var before string
			margin.afterRepay = func() {
				before = store.payload
				if mode == "save_failure" {
					store.err = errors.New("injected ACK save failure")
				}
				if mode == "owner_lost" {
					gate.Block(strategyWalletRuntimeOwnershipBlock)
				}
			}
			if mode == "ack_error" || mode == "no_id" {
				margin.repayErr = errors.New("uncertain accepted repayment")
			}
			margin.noID = mode == "no_id"
			if err := s.repayMarginPrincipal(context.Background(), "BTC", 0.4, 0); err == nil {
				t.Fatal("fault ignored")
			}
			if s.marginRepayIntent == nil || margin.repayCalls != 1 || s.marginDebt != 0.4 {
				t.Fatal("accepted request lost or debt advanced")
			}
			if mode != "no_id" && s.marginRepayIntent.TransferID != 1 {
				t.Fatal("accepted ACK lost")
			}
			if (mode == "save_failure" || mode == "owner_lost") && store.payload != before {
				t.Fatal("failed or stale owner overwrote durable state")
			}
			if err := s.repayMarginPrincipal(context.Background(), "BTC", 0.4, 0); err == nil {
				t.Fatal("uncertain request unexpectedly resolved")
			}
			if margin.repayCalls != 1 {
				t.Fatal("uncertain RPC repeated")
			}
		})
	}
}

func newFundingCarryRepayIntentFixture() (*FundingCarryStrategy, *fundingCarryRepayIntentExchange, *memoryRuntimeStateStore) {
	spot := &mockFCExchange{baseAsset: "BTC", latestPrice: 50000, quantityDecimals: 3, priceDecimals: 2}
	futures := &mockFCExchange{quantityDecimals: 3}
	margin := &fundingCarryRepayIntentExchange{mockFCExchange: &mockFCExchange{quantityDecimals: 3, positions: []*exchange.Position{{Symbol: "BTCUSDT", Size: -0.4, MarginBorrowed: 0.4, MarginDebtKnown: true}}, getOrderStatus: exchange.OrderStatusFilled, getOrderExecQty: 0.4, fillEvidence: true, clearDebtOnRepay: true, repayPrincipal: 0.4}, queryErr: errors.New("injected repayment lookup failure")}
	s := NewFundingCarryStrategy("fc", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, futures, spot, margin, nil)
	store := &memoryRuntimeStateStore{}
	s.SetRuntimeStateStore(&borrowReceiptContextStore{store})
	s.direction, s.marginDebt, s.marginBorrowTransferID = DirectionReverse, 0.4, 42
	s.marginAccountScope = "scope-a"
	return s, margin, store
}

func TestFundingCarryRepaymentLookupFailureRetainsExactIntent(t *testing.T) {
	s, margin, store := newFundingCarryRepayIntentFixture()
	if err := s.closeReverse(context.Background(), "repayment_lookup_failure"); err == nil {
		t.Fatal("query failure ignored")
	}
	var saved map[string]json.RawMessage
	if err := json.Unmarshal([]byte(store.payload), &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved["margin_repay_intent"]) == 0 || !s.intentInFlight || margin.repayCalls != 1 || s.marginDebt != 0.4 {
		t.Fatal("accepted repayment identity or pending operation lost")
	}
}

func TestFundingCarryRepaySchemaKeepsResolvedLegacyButRejectsPending(t *testing.T) {
	s, _, _ := newFundingCarryRepayIntentFixture()
	s.direction, s.marginDebt, s.marginBorrowTransferID, s.strategySpotKnown = DirectionNone, 0, 0, true
	state := s.runtimeStateSnapshotLocked()
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeFundingCarryRuntimeState(1, string(payload), s.fut.GetName(), s.spot.GetName(), s.symbol); err != nil {
		t.Fatal("resolved legacy snapshot rejected:", err)
	}
	state.MarginRepayIntent = &fundingCarryRepayIntent{Asset: "BTC", Amount: 0.4, TransferID: 1}
	payload, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeFundingCarryRuntimeState(fundingCarryRuntimeStateVersion, string(payload), s.fut.GetName(), s.spot.GetName(), s.symbol); err == nil {
		t.Fatal("pending repayment treated as resolved")
	}
}

func TestFundingCarryClearedRepayIntentConfirmedCanceledKeepsCommittedMemory(t *testing.T) {
	s, _, base := newFundingCarryReturnedPrincipalFixture(0)
	pending := &fundingCarryRepayIntent{Asset: "BTC", AccountScope: "scope-a", Amount: 0.4, ExpectedRemaining: 0, BorrowTransferID: 42, TransferID: 7}
	s.intentInFlight = true
	s.marginRepayIntent = cloneFundingCarryRepayIntent(pending)
	if err := s.persistRuntimeStateLocked(); err != nil {
		t.Fatal("persist pending repayment fixture:", err)
	}
	s.SetRuntimeStateStore(&fundingCarryConfirmedCanceledDebtStore{memoryRuntimeStateStore: base})

	s.mu.Lock()
	err := s.clearMarginRepayIntentLocked(context.Background(), pending)
	s.mu.Unlock()
	if !errors.Is(err, storage.ErrFundingCarryRuntimeStateCommitConfirmedCanceled) {
		t.Fatalf("clear repayment error = %v, want confirmed canceled commit", err)
	}
	if s.marginRepayIntent != nil || !s.intentInFlight || s.unownedExposure || s.runtimeStateErr != nil {
		t.Fatalf("memory did not preserve durable cleared intent: repay=%+v intent=%v unknown=%v state_err=%v", s.marginRepayIntent, s.intentInFlight, s.unownedExposure, s.runtimeStateErr)
	}
	var committed fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(base.payload), &committed); err != nil {
		t.Fatal("decode durable repayment checkpoint:", err)
	}
	if committed.MarginRepayIntent != nil || !committed.IntentInFlight {
		t.Fatalf("durable repayment checkpoint = %+v", committed)
	}
}
