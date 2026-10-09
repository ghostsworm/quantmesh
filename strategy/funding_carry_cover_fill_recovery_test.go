package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/lock"
)

type fundingCarryCoverFillVenue struct {
	*fundingCarryCoverQueryVenue
	fills       []*exchange.OrderFill
	fillErr     error
	fillQueries int
	afterFills  func()
}

type coverFillCleanupFailureLock struct {
	lock.DistributedLock
	afterFills *bool
}

func (l *coverFillCleanupFailureLock) Unlock(ctx context.Context, key string) error {
	if *l.afterFills {
		return errors.New("injected cover fill wallet cleanup failure")
	}
	return l.DistributedLock.Unlock(ctx, key)
}

func (v *fundingCarryCoverFillVenue) GetOrder(_ context.Context, _ string, _ int64) (*exchange.Order, error) {
	return v.order, nil
}
func (v *fundingCarryCoverFillVenue) GetOrderFills(context.Context, string, int64) ([]*exchange.OrderFill, error) {
	v.fillQueries++
	if v.afterFills != nil {
		v.afterFills()
	}
	return v.fills, v.fillErr
}

func TestFundingCarryCoverFillRequiresContextReaderBeforeAnyRead(t *testing.T) {
	recoveries := []struct {
		name string
		run  func(*FundingCarryStrategy) error
	}{
		{name: "prepared intent", run: func(s *FundingCarryStrategy) error {
			return s.recoverPreparedRuntimeIntentContext(context.Background())
		}},
		{name: "borrow receipt", run: func(s *FundingCarryStrategy) error { return s.reconcileSavedMarginBorrowReceipt(context.Background()) }},
		{name: "cover acknowledgement", run: func(s *FundingCarryStrategy) error { return s.reconcileSavedMarginCover(context.Background()) }},
		{name: "cover fills", run: func(s *FundingCarryStrategy) error { return s.reconcileSavedMarginCoverFills(context.Background()) }},
		{name: "repayment", run: func(s *FundingCarryStrategy) error { return s.reconcileSavedMarginRepayment(context.Background()) }},
		{name: "remaining assets", run: func(s *FundingCarryStrategy) error { return s.reconcileSavedMarginRemaining(context.Background()) }},
		{name: "complete state restore", run: func(s *FundingCarryStrategy) error { return s.restoreRuntimeStateContext(context.Background()) }},
	}
	for _, recovery := range recoveries {
		t.Run(recovery.name, func(t *testing.T) {
			strategy, _, memoryStore := newFundingCarryRepayIntentFixture()
			legacyStore := &borrowReceiptLegacyReadCounter{store: memoryStore}
			strategy.SetRuntimeStateStore(legacyStore)
			strategy.SetOpeningGate(&execution.OpeningGate{})

			err := recovery.run(strategy)
			if err == nil || !strings.Contains(err.Error(), "cancellable") {
				t.Fatalf("error=%v want context-reader requirement", err)
			}
			if legacyStore.reads != 0 {
				t.Fatalf("legacy non-cancellable reads=%d want=0", legacyStore.reads)
			}
		})
	}
}

func TestFundingCarryStartupRecoversNetCoverFillsWithoutSpending(t *testing.T) {
	for _, mode := range []string{"confirmed", "consistent_conversion", "inconsistent_conversion", "cancelled_partial", "expired_partial", "cancelled_zero_net", "cancelled_missing_fees", "cancelled_owner_lost", "cancelled_save_failure", "cancelled_cleanup_failure", "partial_then_filled", "missing", "wrong_order", "duplicate", "fee_shortfall", "query_error", "owner_lost", "cancelled", "save_failure", "changed_checkpoint", "cancelled_changed_checkpoint"} {
		t.Run(mode, func(t *testing.T) {
			s, margin, store := newFundingCarryRepayIntentFixture()
			s.fut.(*mockFCExchange).name = "binance"
			s.spot.(*mockFCExchange).name = "binance"
			s.strategySpotKnown, s.intentInFlight, s.unownedExposure = true, true, true
			s.marginBorrowedAt = time.UnixMilli(1000).UTC()
			s.marginDebtEvents = []fundingCarryMarginDebtEvent{{Action: "borrow", TransferID: 42, Asset: "BTC", Amount: 0.4, Principal: 0.4, AccountScope: "scope-a", OccurredAt: s.marginBorrowedAt}}
			req := &exchange.OrderRequest{Symbol: "BTCUSDT", Side: exchange.SideBuy, Type: exchange.OrderTypeLimit, Quantity: 0.401, Price: 50000}
			if err := s.prepareMarginCoverIntent(context.Background(), req, 0.4); err != nil {
				t.Fatal(err)
			}
			order := &exchange.Order{OrderID: 7, ClientOrderID: req.ClientOrderID, Symbol: req.Symbol, Side: req.Side, Type: req.Type, Price: req.Price, Quantity: req.Quantity, ExecutedQty: req.Quantity, Status: exchange.OrderStatusFilled, CreatedAt: time.Now().UTC()}
			fill := &exchange.OrderFill{OrderID: 7, TradeID: "trade-7", Symbol: req.Symbol, Side: req.Side, Price: req.Price, Quantity: 0.401, CommissionAsset: "BTC", Commission: 0.0005, BaseFeeQty: 0.0005, TradeTime: time.Now().UnixMilli()}
			venue := &fundingCarryCoverFillVenue{fundingCarryCoverQueryVenue: &fundingCarryCoverQueryVenue{fundingCarryRepayIntentExchange: margin, order: order}, fills: []*exchange.OrderFill{fill}}
			switch mode {
			case "partial_then_filled":
				order.Status, order.ExecutedQty = exchange.OrderStatusPartiallyFilled, 0.2
			case "cancelled_partial", "expired_partial", "cancelled_zero_net", "cancelled_missing_fees", "cancelled_owner_lost", "cancelled_save_failure", "cancelled_cleanup_failure", "cancelled_changed_checkpoint":
				order.Status, order.ExecutedQty = exchange.OrderStatusCanceled, 0.2
				fill.Quantity = 0.2
				if mode == "expired_partial" {
					order.Status = exchange.OrderStatusExpired
				}
				if mode == "cancelled_missing_fees" {
					venue.fills = nil
				}
				if mode == "cancelled_zero_net" {
					fill.Commission, fill.BaseFeeQty = 0.2, 0.2
				}
			case "missing":
				venue.fills = nil
			case "wrong_order":
				fill.OrderID = 8
			case "duplicate":
				venue.fills = append(venue.fills, fill)
			case "fee_shortfall":
				fill.Commission, fill.BaseFeeQty = 0.002, 0.002
			case "consistent_conversion":
				fill.CommissionQuoteKnown, fill.CommissionQuote, fill.CommissionQuoteRate = true, 25, 50000
			case "inconsistent_conversion":
				fill.Commission = 0.002
				fill.CommissionQuoteKnown, fill.CommissionQuote, fill.CommissionQuoteRate = true, 25, 50000
			case "query_error":
				venue.fillErr = errors.New("injected fill query failure")
			}
			restarted, _, _ := newFundingCarryRepayIntentFixture()
			restarted.fut.(*mockFCExchange).name = "binance"
			restarted.spot.(*mockFCExchange).name = "binance"
			restarted.marginDebt, restarted.marginBorrowTransferID = 0, 0
			restarted.marginEx = venue
			restarted.SetRuntimeStateStore(&borrowReceiptContextStore{store})
			gate := &execution.OpeningGate{}
			restarted.SetOpeningGate(gate)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "owner_lost" || mode == "cancelled_owner_lost" {
				venue.afterFills = func() { gate.Block(strategyWalletRuntimeOwnershipBlock) }
			}
			if mode == "cancelled" {
				venue.afterFills = cancel
			}
			if mode == "save_failure" || mode == "cancelled_save_failure" {
				venue.afterFills = func() { store.err = errors.New("injected fill save failure") }
			}
			if mode == "cancelled_cleanup_failure" {
				queried := false
				venue.afterFills = func() { queried = true }
				coordinator := &coverFillCleanupFailureLock{DistributedLock: lock.NewNopLock(), afterFills: &queried}
				if err := restarted.SetAccountWalletCoordinationLock(coordinator, "funding_carry_wallet:"+t.Name()); err != nil {
					t.Fatal(err)
				}
			}
			var changedPayload string
			if mode == "changed_checkpoint" || mode == "cancelled_changed_checkpoint" {
				venue.afterFills = func() {
					changedPayload = strings.Replace(store.payload, `"order_id":7`, `"order_id":8`, 1)
					if changedPayload == store.payload {
						t.Fatal("fixture did not change cover identity")
					}
					store.payload = changedPayload
				}
			}
			startErr := restarted.Start(ctx)
			if startErr == nil {
				t.Fatal("fill proof falsely resumed whole operation")
			}
			if mode == "partial_then_filled" {
				if venue.fillQueries != 0 {
					t.Fatal("open partial order treated as complete")
				}
				order.Status, order.ExecutedQty = exchange.OrderStatusFilled, 0.401
				if err := restarted.Start(ctx); err == nil {
					t.Fatal("whole operation falsely resumed")
				}
			}
			if margin.repayCalls != 0 || len(margin.placedOrders) != 0 || restarted.started {
				t.Fatal("fill recovery spent wallet or started trading")
			}
			if changedPayload != "" {
				var pending *FundingCarryReconciliationRequiredError
				if store.payload != changedPayload || errors.As(startErr, &pending) {
					t.Fatal("stale fill recovery overwrote new checkpoint or admitted managed recovery")
				}
				return
			}
			var state fundingCarryRuntimeState
			if err := json.Unmarshal([]byte(store.payload), &state); err != nil {
				t.Fatal(err)
			}
			r := state.MarginCoverOrders[0]
			if mode == "cancelled_cleanup_failure" {
				var pending *FundingCarryReconciliationRequiredError
				if errors.As(startErr, &pending) || !r.Verified || r.Net != 0.1995 || r.Gross != 0.2 || len(r.Fills) != 1 {
					t.Fatal("cleanup failure admitted recovery or lost committed partial evidence")
				}
				return
			}
			if state.MarginCoverIntent != nil || r.OrderID != 7 || !state.IntentInFlight || !state.ExposureUnknown || state.MarginDebt != 0.4 {
				t.Fatal("ACK or unresolved operation lost")
			}
			if mode == "cancelled_partial" || mode == "expired_partial" || mode == "cancelled_zero_net" {
				wantNet := 0.1995
				if mode == "cancelled_zero_net" {
					wantNet = 0
				}
				if !r.Verified || !fundingCarryFinancialAmountsMatch(r.Net, wantNet) || r.Gross != 0.2 || len(r.Fills) != 1 || r.Consumed != 0 {
					t.Fatal("terminal partial fill and fee evidence not durably recovered")
				}
				var pending *FundingCarryReconciliationRequiredError
				if !errors.As(startErr, &pending) || r.TerminalStatus != order.Status || store.version != fundingCarryRuntimeStateVersion {
					t.Fatal("partial cover did not remain managed reconciliation-only evidence")
				}
				if err := validateFundingCarryCoverSource(state.MarginCoverOrders, &fundingCarryRepayIntent{CoverOrderID: 7, Amount: 0.4, Asset: "BTC", AccountScope: "scope-a"}); err == nil {
					t.Fatal("partial cover accepted full debt repayment")
				}
				if _, err := decodeFundingCarryRuntimeStateForRecovery(6, store.payload, state.FuturesExchange, state.SpotExchange, state.Symbol, true); err == nil {
					t.Fatal("partial semantics accepted under legacy schema")
				}
				binding := FundingCarryRecoveryBinding{state.FuturesExchange, state.SpotExchange, state.Symbol, "BTC", state.MarginAccountScope}
				if err := VerifyFundingCarryRecoveryConfigState(store.version, store.payload, binding); !errors.Is(err, ErrRecoveryConfigRequired) {
					t.Fatalf("partial recovery config classification: %v", err)
				}
				before, queries := store.payload, venue.fillQueries
				if err := restarted.Start(ctx); !errors.As(err, &pending) || restarted.started || store.payload != before || venue.fillQueries != queries {
					t.Fatal("restart replayed fills or falsely resumed trading")
				}
			} else if mode == "confirmed" || mode == "consistent_conversion" || mode == "partial_then_filled" {
				if !r.Verified || r.Net != 0.4005 || r.Gross != 0.401 || len(r.Fills) != 1 || r.Consumed != 0 || venue.queries != 1 {
					t.Fatal("net fill evidence not recovered by exact order")
				}
			} else if r.Verified || r.Net != 0 || len(r.Fills) != 0 {
				t.Fatal("unverified fill evidence advanced durable quantities")
			}
		})
	}
}
