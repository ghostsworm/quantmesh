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

type fundingCarryBudgetExchange struct {
	*mockFCExchange
	balance        float64
	balanceErr     error
	afterBalance   func()
	transferAmount float64
	transferCalls  int
	transfer       func(amount float64) (string, error)
}

type fundingCarryFreshAccountExchange struct {
	*fundingCarryBudgetExchange
	freshBalance float64
	freshErr     error
	freshCalls   int
}

func (e *fundingCarryFreshAccountExchange) GetAccountFresh(context.Context) (*exchange.Account, error) {
	e.freshCalls++
	if e.freshErr != nil {
		return nil, e.freshErr
	}
	return &exchange.Account{BalanceAsset: "USDT", AvailableBalance: e.freshBalance}, nil
}

type fundingCarryMarginBalanceExchange struct {
	*mockFCExchange
	balance     float64
	borrowCalls int
	hourlyRate  float64
	rateErr     error
	rateCalls   int
}

func (e *fundingCarryMarginBalanceExchange) GetNextHourlyBorrowRate(context.Context, string) (float64, error) {
	e.rateCalls++
	return e.hourlyRate, e.rateErr
}

func (e *fundingCarryMarginBalanceExchange) GetBalance(_ context.Context, asset string) (float64, error) {
	if asset == "USDT" {
		return e.balance, nil
	}
	return e.mockFCExchange.GetBalance(context.Background(), asset)
}

func (e *fundingCarryMarginBalanceExchange) Borrow(ctx context.Context, asset string, amount float64) (int64, error) {
	e.borrowCalls++
	return e.mockFCExchange.Borrow(ctx, asset, amount)
}

func (e *fundingCarryBudgetExchange) GetBalance(_ context.Context, asset string) (float64, error) {
	if asset != "USDT" {
		return 0, nil
	}
	if e.afterBalance != nil {
		e.afterBalance()
	}
	return e.balance, e.balanceErr
}

func (e *fundingCarryBudgetExchange) InternalTransfer(_ context.Context, _, _, asset string, amount float64) (string, error) {
	e.transferCalls++
	if asset != "USDT" {
		return "", nil
	}
	e.transferAmount = amount
	if e.transfer != nil {
		return e.transfer(amount)
	}
	return "mock-transfer", nil
}

func TestFundingCarryAmbiguousAutoTransferIsDurablyLatchedAndNeverRetried(t *testing.T) {
	strategy, _, spot := newFundingCarryBudgetStrategy(true, 0, 0, 500)
	strategy.strategySpotKnown = true
	store := &memoryRuntimeStateStore{}
	strategy.SetRuntimeStateStore(store)
	intentPersistedBeforeTransfer := false
	interruptedTransferWouldFailClosedOnRestart := false
	spot.transfer = func(float64) (string, error) {
		var state fundingCarryRuntimeState
		if json.Unmarshal([]byte(store.payload), &state) == nil && state.IntentInFlight && state.IntentPhase == fundingCarryIntentPhaseDispatching {
			intentPersistedBeforeTransfer = true
			_, restoreErr := decodeFundingCarryRuntimeState(store.version, store.payload,
				strategy.fut.GetName(), strategy.spot.GetName(), strategy.symbol)
			interruptedTransferWouldFailClosedOnRestart = restoreErr != nil
		}
		return "", errors.New("transfer acknowledgement lost")
	}

	if err := strategy.ensureFuturesMargin(context.Background(), 200, 0); err == nil {
		t.Fatal("ambiguous transfer result was accepted")
	}
	if !strategy.unownedExposure || !store.found {
		t.Fatalf("uncertain transfer was not durably blocked: blocked=%v persisted=%v", strategy.unownedExposure, store.found)
	}
	if !intentPersistedBeforeTransfer {
		t.Fatal("collateral transfer was submitted before durable in-flight intent was saved")
	}
	if !interruptedTransferWouldFailClosedOnRestart {
		t.Fatal("startup would accept the persisted in-flight collateral transfer after a crash")
	}
	var state fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(store.payload), &state); err != nil || !state.ExposureUnknown || state.IntentInFlight {
		t.Fatalf("persisted state does not retain transfer uncertainty: state=%+v err=%v", state, err)
	}
	if err := strategy.ensureFuturesMargin(context.Background(), 200, 0); err == nil {
		t.Fatal("retried transfer while previous transfer outcome is unknown")
	}
	if spot.transferCalls != 1 {
		t.Fatalf("transfer calls = %d, want exactly one", spot.transferCalls)
	}
}

func TestFundingCarryCanceledBalanceSnapshotNeverSubmitsTransfer(t *testing.T) {
	tests := []struct {
		name     string
		cancelAt string
	}{
		{name: "futures balance", cancelAt: "futures"},
		{name: "spot balance", cancelAt: "spot"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			strategy, futures, spot := newFundingCarryBudgetStrategy(true, 0, 0, 500)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.cancelAt == "futures" {
				futures.afterBalance = cancel
			} else {
				spot.afterBalance = cancel
			}

			if err := strategy.ensureFuturesMargin(ctx, 200, 0); err == nil {
				t.Fatal("accepted a balance snapshot returned after context cancellation")
			}
			if spot.transferCalls != 0 {
				t.Fatalf("submitted %d transfers after balance context cancellation", spot.transferCalls)
			}
		})
	}
}

func TestFundingCarryUsesFreshFuturesBalanceWhenAdapterProvidesIt(t *testing.T) {
	strategy, staleFutures, spot := newFundingCarryBudgetStrategy(true, 0, 0, 500)
	futures := &fundingCarryFreshAccountExchange{
		fundingCarryBudgetExchange: staleFutures,
		freshBalance:               200,
	}
	strategy.fut = futures
	if err := strategy.ensureFuturesMargin(context.Background(), 200, 0); err != nil {
		t.Fatalf("fresh futures balance should satisfy reserve: %v", err)
	}
	if futures.freshCalls != 1 || spot.transferCalls != 0 {
		t.Fatalf("did not use fresh balance to avoid unnecessary transfer: fresh reads=%d transfer calls=%d", futures.freshCalls, spot.transferCalls)
	}
}

func TestFundingCarryAmbiguousProfitHarvestIsDurablyLatchedAndNeverRetried(t *testing.T) {
	strategy, futures, _ := newFundingCarryBudgetStrategy(false, 0, 300, 500)
	strategy.profitHarvestEnabled = true
	strategy.profitHarvestMin = 10
	store := &memoryRuntimeStateStore{}
	strategy.SetRuntimeStateStore(store)
	intentPersistedBeforeTransfer := false
	futures.transfer = func(float64) (string, error) {
		var state fundingCarryRuntimeState
		if json.Unmarshal([]byte(store.payload), &state) == nil && state.IntentInFlight {
			intentPersistedBeforeTransfer = true
		}
		return "", errors.New("transfer acknowledgement lost")
	}

	strategy.harvestProfitUnderWalletLock(context.Background())
	if !strategy.unownedExposure || !store.found {
		t.Fatalf("uncertain profit transfer was not durably blocked: blocked=%v persisted=%v", strategy.unownedExposure, store.found)
	}
	if !intentPersistedBeforeTransfer {
		t.Fatal("profit transfer was submitted before durable in-flight intent was saved")
	}
	callsAfterFirstAttempt := futures.transferCalls
	strategy.harvestProfitUnderWalletLock(context.Background())
	if futures.transferCalls != callsAfterFirstAttempt || callsAfterFirstAttempt != 1 {
		t.Fatalf("profit transfer retried with unknown prior outcome: calls=%d", futures.transferCalls)
	}
	var state fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(store.payload), &state); err != nil || !state.ExposureUnknown || state.IntentInFlight {
		t.Fatalf("persisted state does not retain transfer uncertainty: state=%+v err=%v", state, err)
	}
}

func TestFundingCarryProfitHarvestRequiresVerifiedWalletMovement(t *testing.T) {
	strategy, futures, spot := newFundingCarryBudgetStrategy(false, 0, 300, 500)
	strategy.profitHarvestEnabled = true
	strategy.profitHarvestMin = 10
	store := &memoryRuntimeStateStore{}
	strategy.SetRuntimeStateStore(store)
	futures.transfer = func(float64) (string, error) { return "tx-no-movement", nil }

	strategy.harvestProfitUnderWalletLock(context.Background())
	if !strategy.unownedExposure || !store.found {
		t.Fatalf("unverified successful transfer was not durably blocked: blocked=%v persisted=%v", strategy.unownedExposure, store.found)
	}
	if spot.balance != 500 || futures.transferCalls != 1 {
		t.Fatalf("unexpected balances/calls after unverified transfer: spot=%.2f futures calls=%d", spot.balance, futures.transferCalls)
	}
}

func TestFundingCarryProfitHarvestCannotConsumeFuturesSafetyBuffer(t *testing.T) {
	strategy, futures, spot := newFundingCarryBudgetStrategy(false, 0, 300, 500)
	strategy.profitHarvestEnabled = true
	strategy.profitHarvestMin = 10
	store := &memoryRuntimeStateStore{}
	strategy.SetRuntimeStateStore(store)
	futures.transfer = func(amount float64) (string, error) {
		futures.balance = 49
		spot.balance += amount
		return "tx-below-safety-buffer", nil
	}

	strategy.harvestProfitUnderWalletLock(context.Background())
	if !strategy.unownedExposure || !store.found {
		t.Fatalf("safety-buffer breach was not durably blocked: blocked=%v persisted=%v", strategy.unownedExposure, store.found)
	}
}

func TestFundingCarryProfitHarvestVerifiesBothWalletBalances(t *testing.T) {
	strategy, futures, spot := newFundingCarryBudgetStrategy(false, 0, 300, 500)
	strategy.profitHarvestEnabled = true
	strategy.profitHarvestMin = 10
	store := &memoryRuntimeStateStore{}
	strategy.SetRuntimeStateStore(store)
	intentPersistedBeforeTransfer := false
	futures.transfer = func(amount float64) (string, error) {
		var state fundingCarryRuntimeState
		if json.Unmarshal([]byte(store.payload), &state) == nil && state.IntentInFlight {
			intentPersistedBeforeTransfer = true
		}
		futures.balance -= amount
		spot.balance += amount
		return "tx-verified-harvest", nil
	}

	strategy.harvestProfitUnderWalletLock(context.Background())
	if strategy.unownedExposure {
		t.Fatal("verified profit harvest unexpectedly latched exposure uncertainty")
	}
	if !intentPersistedBeforeTransfer {
		t.Fatal("profit transfer was submitted before durable in-flight intent was saved")
	}
	if futures.balance != 50 || spot.balance != 750 {
		t.Fatalf("unexpected verified balances: futures %.2f spot %.2f", futures.balance, spot.balance)
	}
	var state fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(store.payload), &state); err != nil || state.IntentInFlight || state.ExposureUnknown {
		t.Fatalf("verified transfer intent was not durably cleared: state=%+v err=%v", state, err)
	}
}

func TestFundingCarryProfitHarvestUsesFreshBalancesAcrossTransfer(t *testing.T) {
	strategy, staleFutures, spot := newFundingCarryBudgetStrategy(false, 0, 1200, 500)
	futures := &fundingCarryFreshAccountExchange{
		fundingCarryBudgetExchange: staleFutures,
		freshBalance:               300,
	}
	strategy.fut = futures
	strategy.profitHarvestEnabled = true
	strategy.profitHarvestMin = 10
	strategy.SetRuntimeStateStore(&memoryRuntimeStateStore{})
	futures.transfer = func(amount float64) (string, error) {
		futures.freshBalance -= amount
		spot.balance += amount
		return "tx-fresh-balance", nil
	}

	strategy.harvestProfitUnderWalletLock(context.Background())
	if futures.transferAmount != 250 || futures.freshCalls != 2 {
		t.Fatalf("harvest used cached/stale balance: amount=%.2f fresh reads=%d", futures.transferAmount, futures.freshCalls)
	}
	if strategy.unownedExposure || futures.freshBalance != 50 || spot.balance != 750 {
		t.Fatalf("fresh transfer did not reconcile: blocked=%v futures %.2f spot %.2f", strategy.unownedExposure, futures.freshBalance, spot.balance)
	}
}

func newFundingCarryBudgetStrategy(autoTransfer bool, reserve float64, futuresBalance, spotBalance float64) (*FundingCarryStrategy, *fundingCarryBudgetExchange, *fundingCarryBudgetExchange) {
	futures := &fundingCarryBudgetExchange{mockFCExchange: &mockFCExchange{}, balance: futuresBalance}
	spot := &fundingCarryBudgetExchange{mockFCExchange: &mockFCExchange{}, balance: spotBalance}
	strategy := NewFundingCarryStrategy("funding_carry", &config.Config{}, config.SymbolConfig{Exchange: "binance"}, futures, spot, nil,
		map[string]interface{}{"auto_transfer_enabled": autoTransfer, "transfer_reserve_spot": reserve})
	return strategy, futures, spot
}

func TestFundingCarryRequiresBothLegBalancesBeforeOpening(t *testing.T) {
	strategy, futures, spot := newFundingCarryBudgetStrategy(false, 50, 300, 100)
	err := strategy.ensureFuturesMargin(context.Background(), 150, 200)
	if err == nil {
		t.Fatal("accepted insufficient spot balance for the pending buy leg")
	}
	if spot.transferAmount != 0 {
		t.Fatalf("unexpected transfer amount %v", spot.transferAmount)
	}
	if futures.balance != 300 || spot.balance != 100 {
		t.Fatalf("balances changed after rejected opening: futures %.2f spot %.2f", futures.balance, spot.balance)
	}
}

func TestFundingCarryAutoTransferReservesSpotBuyFunds(t *testing.T) {
	strategy, _, spot := newFundingCarryBudgetStrategy(true, 50, 0, 400)
	err := strategy.ensureFuturesMargin(context.Background(), 200, 200)
	if err == nil {
		t.Fatal("transferred collateral that would consume the pending spot-buy funds")
	}
	if spot.transferAmount != 0 {
		t.Fatalf("transfer occurred before confirming both legs fit: %.2f", spot.transferAmount)
	}
}

func TestFundingCarryAutoTransferVerifiesBothPostTransferBalances(t *testing.T) {
	strategy, futures, spot := newFundingCarryBudgetStrategy(true, 50, 0, 500)
	strategy.strategySpotKnown = true
	store := &memoryRuntimeStateStore{}
	strategy.SetRuntimeStateStore(store)
	intentPersistedBeforeTransfer := false
	spot.transfer = func(amount float64) (string, error) {
		var state fundingCarryRuntimeState
		if json.Unmarshal([]byte(store.payload), &state) == nil && state.IntentInFlight {
			intentPersistedBeforeTransfer = true
		}
		spot.balance -= amount
		futures.balance += amount
		return "tx-verified", nil
	}
	if err := strategy.ensureFuturesMargin(context.Background(), 200, 200); err != nil {
		t.Fatalf("ensureFuturesMargin: %v", err)
	}
	if !intentPersistedBeforeTransfer {
		t.Fatal("collateral transfer was submitted before durable in-flight intent was saved")
	}
	if spot.transferAmount != 200 || futures.balance != 200 || spot.balance != 300 {
		t.Fatalf("unexpected post-transfer state: transfer %.2f futures %.2f spot %.2f", spot.transferAmount, futures.balance, spot.balance)
	}
	var state fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(store.payload), &state); err != nil || state.IntentInFlight || state.ExposureUnknown {
		t.Fatalf("verified collateral transfer intent was not cleared: state=%+v err=%v", state, err)
	}
}

func TestFundingCarryAutoTransferPreservesOtherBotsWalletReserves(t *testing.T) {
	strategy, futures, spot := newFundingCarryBudgetStrategy(true, 50, 250, 500)
	strategy.strategySpotKnown = true
	strategy.SetRuntimeStateStore(&memoryRuntimeStateStore{})
	if err := strategy.SetAccountCapitalReserves(300, 100, 500, 100); err != nil {
		t.Fatal(err)
	}
	spot.transfer = func(amount float64) (string, error) {
		spot.balance -= amount
		futures.balance += amount
		return "tx-reserved", nil
	}
	if err := strategy.ensureFuturesMargin(context.Background(), 200, 0); err != nil {
		t.Fatalf("ensureFuturesMargin: %v", err)
	}
	if spot.transferAmount != 50 || futures.balance != 300 || spot.balance != 450 {
		t.Fatalf("did not preserve external wallet reserves: transfer %.2f futures %.2f spot %.2f", spot.transferAmount, futures.balance, spot.balance)
	}
}

func TestFundingCarryAutoTransferRejectsUseOfOtherSpotBotReserve(t *testing.T) {
	strategy, _, spot := newFundingCarryBudgetStrategy(true, 50, 0, 500)
	if err := strategy.SetAccountCapitalReserves(500, 0, 500, 100); err != nil {
		t.Fatal(err)
	}
	if err := strategy.ensureFuturesMargin(context.Background(), 200, 200); err == nil {
		t.Fatal("transferred funds reserved for another Bot's spot budget")
	}
	if spot.transferAmount != 0 {
		t.Fatalf("unexpected transfer %.2f USDT", spot.transferAmount)
	}
}

func TestFundingCarrySpotBuyReserveIncludesQuoteFee(t *testing.T) {
	reserve, err := fundingCarrySpotBuyReserve(2, 100.5, 0.001)
	if err != nil {
		t.Fatal(err)
	}
	want := 2 * 100.5 * 1.001
	if math.Abs(reserve-want) > 1e-9 {
		t.Fatalf("spot buy reserve = %.12f, want %.12f", reserve, want)
	}
	if _, err := fundingCarrySpotBuyReserve(2, 100, 1.01); err != nil {
		t.Fatalf("finite fee reserve should remain supported: %v", err)
	}
}

func TestFundingCarrySingleMarketRoundTripBookCost(t *testing.T) {
	book := &exchange.OrderBook{
		Symbol: "BTCUSDT",
		Bids:   []exchange.OrderBookLevel{{Price: 100, Quantity: 1}, {Price: 99, Quantity: 1}},
		Asks:   []exchange.OrderBookLevel{{Price: 101, Quantity: 1}, {Price: 102, Quantity: 1}},
	}
	cost, err := fundingCarrySingleMarketRoundTripBookCost(book, "BTCUSDT", 2)
	if err != nil {
		t.Fatalf("estimate order book round-trip cost: %v", err)
	}
	if math.Abs(cost-4) > 1e-9 {
		t.Fatalf("round-trip book cost = %.8f, want 4", cost)
	}
}

func TestFundingCarrySingleMarketRoundTripBookCostRejectsUnsafeBook(t *testing.T) {
	tests := []struct {
		name string
		book *exchange.OrderBook
		qty  float64
	}{
		{name: "insufficient depth", book: &exchange.OrderBook{Symbol: "BTCUSDT", Bids: []exchange.OrderBookLevel{{Price: 100, Quantity: 1}}, Asks: []exchange.OrderBookLevel{{Price: 101, Quantity: 1}}}, qty: 2},
		{name: "crossed book", book: &exchange.OrderBook{Symbol: "BTCUSDT", Bids: []exchange.OrderBookLevel{{Price: 102, Quantity: 2}}, Asks: []exchange.OrderBookLevel{{Price: 101, Quantity: 2}}}, qty: 1},
		{name: "wrong symbol", book: &exchange.OrderBook{Symbol: "ETHUSDT", Bids: []exchange.OrderBookLevel{{Price: 100, Quantity: 2}}, Asks: []exchange.OrderBookLevel{{Price: 101, Quantity: 2}}}, qty: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := fundingCarrySingleMarketRoundTripBookCost(tt.book, "BTCUSDT", tt.qty); err == nil {
				t.Fatal("accepted unsafe order book")
			}
		})
	}
}

func TestFundingCarryExecutionCostRecoveryIncludesBookSpread(t *testing.T) {
	if err := validateFundingCarryFeeRecovery(0.000015, 0.0002, 30, 0); err != nil {
		t.Fatalf("fee-only recovery should pass: %v", err)
	}
	if err := validateFundingCarryExecutionCostRecovery(0.000015, 0.0002, 30, 0, 100, 100, 0.1); err == nil {
		t.Fatal("accepted a book-spread cost that pushes recovery beyond the configured horizon")
	}
}

func TestFundingCarryWorstCaseEntrySlippageCost(t *testing.T) {
	tests := []struct {
		name      string
		quantity  float64
		reference float64
		limit     float64
		side      exchange.Side
		want      float64
	}{
		{name: "buy cap above reference", quantity: 2, reference: 100, limit: 100.5, side: exchange.SideBuy, want: 1},
		{name: "sell floor below reference", quantity: 2, reference: 100, limit: 99.7, side: exchange.SideSell, want: 0.6},
		{name: "buy price improvement", quantity: 2, reference: 100, limit: 99.5, side: exchange.SideBuy, want: 0},
		{name: "sell price improvement", quantity: 2, reference: 100, limit: 100.5, side: exchange.SideSell, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := fundingCarryWorstCaseEntrySlippageCost(tt.quantity, tt.reference, tt.limit, tt.side)
			if err != nil {
				t.Fatalf("estimate worst-case entry slippage: %v", err)
			}
			if math.Abs(got-tt.want) > 1e-9 {
				t.Fatalf("slippage cost = %.8f, want %.8f", got, tt.want)
			}
		})
	}
}

func TestFundingCarryHarvestRetainsFullPositionNotional(t *testing.T) {
	surplus, ok := fundingCarryHarvestableSurplus(1200, 0.02, 50000, 100, 900)
	if !ok || math.Abs(surplus-150) > 1e-9 {
		t.Fatalf("harvestable surplus = %.8f, ok=%v; want 150, true", surplus, ok)
	}
	if _, ok := fundingCarryHarvestableSurplus(1040, 0.02, 50000, 10, 0); ok {
		t.Fatal("allowed harvest that would consume the full-notional reserve and safety buffer")
	}
}

func TestFundingCarryHarvestFailsClosedOnUnknownPositionPrice(t *testing.T) {
	for _, price := range []float64{0, math.NaN(), math.Inf(1)} {
		if _, ok := fundingCarryHarvestableSurplus(5000, 0.02, price, 10, 0); ok {
			t.Fatalf("allowed harvest with invalid position price %v", price)
		}
	}
	if _, ok := fundingCarryHarvestableSurplus(math.NaN(), 0, 0, 10, 0); ok {
		t.Fatal("allowed harvest with invalid futures balance")
	}
}

func TestFundingCarryHarvestDoesNotTransferWhenPositionPriceIsUnknown(t *testing.T) {
	strategy, futures, _ := newFundingCarryBudgetStrategy(false, 50, 5000, 0)
	strategy.profitHarvestEnabled = true
	strategy.direction = DirectionForward
	strategy.futQty = 0.02
	futures.latestPrice = 0
	strategy.harvestProfit(context.Background())
	if futures.transferAmount != 0 {
		t.Fatalf("transferred %.2f USDT without a verified position price", futures.transferAmount)
	}
}

func TestFundingCarryHarvestTransfersOnlyVerifiedExcess(t *testing.T) {
	strategy, futures, spot := newFundingCarryBudgetStrategy(false, 50, 1200, 0)
	strategy.profitHarvestEnabled = true
	strategy.profitHarvestMin = 100
	strategy.direction = DirectionForward
	strategy.futQty = 0.02
	futures.latestPrice = 50000
	strategy.SetRuntimeStateStore(&memoryRuntimeStateStore{})
	futures.transfer = func(amount float64) (string, error) {
		futures.balance -= amount
		spot.balance += amount
		return "tx-verified", nil
	}
	strategy.harvestProfit(context.Background())
	if math.Abs(futures.transferAmount-150) > 1e-9 {
		t.Fatalf("transferred %.8f USDT; want only 150.00 verified excess", futures.transferAmount)
	}
}

func TestFundingCarryHarvestPreservesAccountCapitalCommitment(t *testing.T) {
	strategy, futures, spot := newFundingCarryBudgetStrategy(false, 50, 1300, 0)
	strategy.profitHarvestEnabled = true
	strategy.profitHarvestMin = 100
	strategy.direction = DirectionForward
	strategy.futQty = 0.02
	strategy.SetRuntimeStateStore(&memoryRuntimeStateStore{})
	futures.transfer = func(amount float64) (string, error) {
		futures.balance -= amount
		spot.balance += amount
		return "tx-verified", nil
	}
	futures.latestPrice = 50000
	if err := strategy.SetAccountCapitalReserves(1100, 100, 0, 0); err != nil {
		t.Fatal(err)
	}
	strategy.harvestProfit(context.Background())
	if math.Abs(futures.transferAmount-150) > 1e-9 {
		t.Fatalf("harvest ignored account capital commitment: transferred %.8f, want 150", futures.transferAmount)
	}
}

func TestFundingCarryReverseOpeningChecksMarginBalanceBeforeBorrow(t *testing.T) {
	strategy, _, _ := newFundingCarryBudgetStrategy(false, 50, 300, 100)
	strategy.symCfg.TotalAllocatedCapital = 500
	margin := &fundingCarryMarginBalanceExchange{mockFCExchange: &mockFCExchange{}, balance: 249.99}
	strategy.marginEx = margin
	err := strategy.openReverseHedge(context.Background(), 50000, 50000, -0.001)
	if err == nil {
		t.Fatal("opened reverse carry with less margin USDT than one-leg notional")
	}
	if margin.borrowCalls != 0 {
		t.Fatalf("attempted %d borrow calls despite insufficient collateral", margin.borrowCalls)
	}
}

func TestFundingCarryReverseOpeningRejectsBorrowRateAboveDailyLimit(t *testing.T) {
	strategy, _, _ := newFundingCarryBudgetStrategy(false, 50, 300, 100)
	strategy.symCfg.TotalAllocatedCapital = 500
	strategy.marginInterestMax = 0.001
	margin := &fundingCarryMarginBalanceExchange{mockFCExchange: &mockFCExchange{}, balance: 300, hourlyRate: 0.00005}
	strategy.marginEx = margin
	err := strategy.openReverseHedge(context.Background(), 50000, 50000, -0.001)
	if err == nil {
		t.Fatal("opened reverse carry when estimated daily borrow interest exceeded its limit")
	}
	if margin.rateCalls != 1 || margin.borrowCalls != 0 {
		t.Fatalf("rate checks=%d, borrow calls=%d; want one check before any borrow", margin.rateCalls, margin.borrowCalls)
	}
}

func TestFundingCarryReverseOpeningChecksEconomicsBeforeFundingTransfer(t *testing.T) {
	strategy, futures, _ := newFundingCarryBudgetStrategy(true, 0, 0, 500)
	strategy.symCfg.TotalAllocatedCapital = 500
	strategy.marginInterestMax = 0.001
	margin := &fundingCarryMarginBalanceExchange{mockFCExchange: &mockFCExchange{}, balance: 300, hourlyRate: 0.00005}
	strategy.marginEx = margin
	if err := strategy.openReverseHedge(context.Background(), 50000, 50000, -0.001); err == nil {
		t.Fatal("opened reverse carry despite borrow rate exceeding daily limit")
	}
	if margin.borrowCalls != 0 || futures.transferCalls != 0 {
		t.Fatalf("economics rejection occurred after side effects: borrow calls=%d, transfers=%d", margin.borrowCalls, futures.transferCalls)
	}
}

func TestFundingCarryReverseOpeningRejectsShallowBookBeforeBorrowOrTransfer(t *testing.T) {
	strategy, futures, _ := newFundingCarryBudgetStrategy(true, 0, 0, 500)
	strategy.symCfg.TotalAllocatedCapital = 500
	strategy.cfg = &config.Config{Exchanges: map[string]config.ExchangeConfig{"binance": {FeeRate: 0.0002}}}
	futures.fundingRate = -0.001
	margin := &fundingCarryMarginBalanceExchange{mockFCExchange: &mockFCExchange{}, balance: 300, hourlyRate: 0.00001}
	margin.orderBookOverride = &exchange.OrderBook{
		Symbol: "BTCUSDT",
		Bids:   []exchange.OrderBookLevel{{Price: 49999, Quantity: 0.001}},
		Asks:   []exchange.OrderBookLevel{{Price: 50001, Quantity: 0.001}},
	}
	strategy.marginEx = margin

	if err := strategy.openReverseHedge(context.Background(), 50000, 50000, -0.001); err == nil {
		t.Fatal("opened reverse carry despite insufficient margin-market book depth")
	}
	if margin.borrowCalls != 0 || futures.transferCalls != 0 {
		t.Fatalf("book-depth rejection occurred after side effects: borrow calls=%d, transfers=%d", margin.borrowCalls, futures.transferCalls)
	}
}

func TestFundingCarryReverseNetFundingMustExceedBorrowInterest(t *testing.T) {
	tests := []struct {
		name        string
		fundingRate float64
		interval    time.Duration
		hourlyRate  float64
		wantErr     bool
	}{
		{name: "profitable before trading costs", fundingRate: -0.001, interval: 8 * time.Hour, hourlyRate: 0.00001},
		{name: "funding below borrow cost", fundingRate: -0.00001, interval: 8 * time.Hour, hourlyRate: 0.00001, wantErr: true},
		{name: "unknown interval", fundingRate: -0.001, hourlyRate: 0.00001, wantErr: true},
		{name: "wrong funding direction", fundingRate: 0.001, interval: 8 * time.Hour, hourlyRate: 0.00001, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &exchange.FundingInfo{Symbol: "BTCUSDT", Rate: tt.fundingRate, FundingInterval: tt.interval}
			err := validateFundingCarryReverseNetRate(info, "BTCUSDT", tt.hourlyRate)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateFundingCarryReverseNetRate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFundingCarryMarginBalanceValidation(t *testing.T) {
	for _, balance := range []float64{math.NaN(), math.Inf(1), -1, 99} {
		if err := validateFundingCarryMarginBalance(balance, 100); err == nil {
			t.Fatalf("accepted margin balance %.8g", balance)
		}
	}
	if err := validateFundingCarryMarginBalance(100, 100); err != nil {
		t.Fatalf("exact collateral should pass: %v", err)
	}
}
