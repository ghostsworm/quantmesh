package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/exchange"
)

type fundingCarryReturnedPrincipalExchange struct {
	*fundingCarryMarginBalanceExchange
	interest float64
}

func (e *fundingCarryReturnedPrincipalExchange) GetMarginTransactionByID(_ context.Context, asset, kind string, id int64) (exchange.MarginBorrowRecord, error) {
	amount, interest := e.borrowAmount, 0.0
	if kind == "REPAY" {
		amount, interest = e.repayAmount, e.interest
	}
	return exchange.MarginBorrowRecord{TransferID: id, Asset: asset, Amount: amount, Principal: amount - interest, Interest: interest, Status: "CONFIRMED", Timestamp: time.Now().UnixMilli()}, nil
}

func newFundingCarryReturnedPrincipalFixture(interest float64) (*FundingCarryStrategy, *fundingCarryReturnedPrincipalExchange, *memoryRuntimeStateStore) {
	s, futures, spot := newFundingCarryBudgetStrategy(false, 0, 300, 300)
	s.cfg = &config.Config{Exchanges: map[string]config.ExchangeConfig{"binance": {FeeRate: 0.0002}}}
	s.symbol, s.symCfg.Symbol, s.symCfg.TotalAllocatedCapital = "BTCUSDT", "BTCUSDT", 500
	futures.baseAsset, futures.latestPrice, futures.fundingRate = "BTC", 50000, -0.01
	spot.baseAsset, spot.latestPrice = "BTC", 50000
	futures.quantityDecimals, spot.quantityDecimals, futures.priceDecimals, spot.priceDecimals = 8, 8, 2, 2
	margin := &fundingCarryReturnedPrincipalExchange{fundingCarryMarginBalanceExchange: &fundingCarryMarginBalanceExchange{mockFCExchange: &mockFCExchange{baseAsset: "BTC", latestPrice: 50000, quantityDecimals: 8, priceDecimals: 2, placeOrderErr: errors.New("definitive sell rejection")}, balance: 300}, interest: interest}
	s.marginEx = margin
	store := &memoryRuntimeStateStore{}
	s.SetRuntimeStateStore(store)
	s.strategySpotKnown = true
	return s, margin, store
}

func TestFundingCarryRejectedSellCannotClearPrincipalUsingRepaymentTotal(t *testing.T) {
	s, margin, store := newFundingCarryReturnedPrincipalFixture(0.0001)
	if err := s.openReverseHedge(context.Background(), 50000, 50000, -0.01); err == nil {
		t.Fatal("sell rejection was hidden")
	}
	var saved fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(store.payload), &saved); err != nil {
		t.Fatal(err)
	}
	if math.Abs(s.marginDebt-0.0001) > 1e-12 || math.Abs(saved.MarginDebt-0.0001) > 1e-12 || !s.unownedExposure || !saved.ExposureUnknown || s.marginBorrowTransferID == 0 || len(s.fut.(*fundingCarryBudgetExchange).placedOrders) != 0 || margin.repayCalls != 1 {
		t.Fatalf("repayment total cleared residual principal: debt=%.8f saved=%.8f unknown=%v", s.marginDebt, saved.MarginDebt, saved.ExposureUnknown)
	}
}

func TestFundingCarryUnfilledBorrowReturnUsesConfirmedPrincipal(t *testing.T) {
	for _, filled := range []float64{0, 0.003} {
		s, margin, store := newFundingCarryReturnedPrincipalFixture(0.0001)
		margin.placeOrderErr = nil
		margin.getOrderStatus, margin.getOrderExecQty = exchange.OrderStatusFilled, filled
		if err := s.openReverseHedge(context.Background(), 50000, 50000, -0.01); err == nil {
			t.Fatal("unreturned principal was accepted as reconciled")
		}
		var saved fundingCarryRuntimeState
		if err := json.Unmarshal([]byte(store.payload), &saved); err != nil {
			t.Fatal(err)
		}
		want := filled + 0.0001
		if math.Abs(s.marginDebt-want) > 1e-12 || math.Abs(saved.MarginDebt-want) > 1e-12 || !saved.ExposureUnknown || len(s.fut.(*fundingCarryBudgetExchange).placedOrders) != 0 || margin.repayCalls != 1 {
			t.Fatalf("partial return discarded owned principal: filled=%.8f debt=%.8f", filled, s.marginDebt)
		}
	}
}

func TestFundingCarryRejectedSellFullPrincipalReturnStillClears(t *testing.T) {
	s, margin, store := newFundingCarryReturnedPrincipalFixture(0)
	if err := s.openReverseHedge(context.Background(), 50000, 50000, -0.01); err == nil {
		t.Fatal("sell rejection itself must still be reported")
	}
	var saved fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(store.payload), &saved); err != nil {
		t.Fatal(err)
	}
	if s.marginDebt != 0 || s.unownedExposure || s.marginBorrowTransferID != 0 || saved.Direction != DirectionNone || saved.ExposureUnknown || saved.IntentInFlight || len(saved.MarginDebtEvents) != 2 || margin.repayCalls != 1 {
		t.Fatalf("verified complete principal return did not clear: %+v", saved)
	}
}

func TestFundingCarryPartialSellExactPrincipalReturnContinuesHedge(t *testing.T) {
	s, margin, store := newFundingCarryReturnedPrincipalFixture(0)
	margin.placeOrderErr = nil
	margin.getOrderStatus, margin.getOrderExecQty = exchange.OrderStatusFilled, 0.003
	futures := s.fut.(*fundingCarryBudgetExchange)
	futures.getOrderStatus, futures.getOrderExecQty = exchange.OrderStatusFilled, 0.003
	if err := s.openReverseHedge(context.Background(), 50000, 50000, -0.01); err != nil {
		t.Fatal(err)
	}
	var saved fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(store.payload), &saved); err != nil {
		t.Fatal(err)
	}
	if math.Abs(s.marginDebt-0.003) > 1e-12 || s.unownedExposure || saved.ExposureUnknown || saved.IntentInFlight || len(futures.placedOrders) != 1 || margin.repayCalls != 1 || len(saved.MarginDebtEvents) != 2 {
		t.Fatalf("verified partial return blocked normal hedge: %+v", saved)
	}
}

func TestFundingCarryReturnedPrincipalSaveFailureRollsBackAndRetries(t *testing.T) {
	venue := &fundingCarryStableDebtExchange{row: exchange.MarginBorrowRecord{TransferID: 81, Asset: "BTC", Amount: 0.002, Principal: 0.002, Status: "CONFIRMED", Timestamp: time.Now().Add(-time.Second).UnixMilli()}}
	s := NewFundingCarryStrategy("funding_carry", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, &venue.mockFCExchange, &venue.mockFCExchange, venue, nil)
	store := &memoryRuntimeStateStore{}
	s.SetRuntimeStateStore(store)
	s.marginDebt, s.direction, s.strategySpotKnown = 0.005, DirectionReverse, true
	s.mu.Lock()
	err := s.persistRuntimeStateLocked()
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	original := store.payload
	store.err = errors.New("injected return save failure")
	if err := s.returnBorrowedPrincipal(context.Background(), 81, "BTC", 0.002, 0.003); err == nil {
		t.Fatal("save failure ignored")
	}
	if s.marginDebt != 0.005 || len(s.marginDebtEvents) != 0 || store.payload != original || !s.unownedExposure {
		t.Fatal("failed save advanced principal or ledger")
	}
	store.err = nil
	if err := s.returnBorrowedPrincipal(context.Background(), 81, "BTC", 0.002, 0.003); err != nil {
		t.Fatal(err)
	}
	if err := s.returnBorrowedPrincipal(context.Background(), 81, "BTC", 0.002, 0.003); err != nil {
		t.Fatal(err)
	}
	if math.Abs(s.marginDebt-0.003) > 1e-12 || len(s.marginDebtEvents) != 1 {
		t.Fatal("retry double-debited confirmed principal")
	}
}
