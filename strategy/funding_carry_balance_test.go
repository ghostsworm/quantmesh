package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"

	"quantmesh/config"
)

type fundingCarryBudgetExchange struct {
	*mockFCExchange
	balance        float64
	balanceErr     error
	transferAmount float64
	transferCalls  int
	transfer       func(amount float64) (string, error)
}

type fundingCarryMarginBalanceExchange struct {
	*mockFCExchange
	balance     float64
	borrowCalls int
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
	store := &memoryRuntimeStateStore{}
	strategy.SetRuntimeStateStore(store)
	spot.transfer = func(float64) (string, error) { return "", errors.New("transfer acknowledgement lost") }

	if err := strategy.ensureFuturesMargin(context.Background(), 200, 0); err == nil {
		t.Fatal("ambiguous transfer result was accepted")
	}
	if !strategy.unownedExposure || !store.found {
		t.Fatalf("uncertain transfer was not durably blocked: blocked=%v persisted=%v", strategy.unownedExposure, store.found)
	}
	var state fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(store.payload), &state); err != nil || !state.ExposureUnknown {
		t.Fatalf("persisted state does not retain transfer uncertainty: state=%+v err=%v", state, err)
	}
	if err := strategy.ensureFuturesMargin(context.Background(), 200, 0); err == nil {
		t.Fatal("retried transfer while previous transfer outcome is unknown")
	}
	if spot.transferCalls != 1 {
		t.Fatalf("transfer calls = %d, want exactly one", spot.transferCalls)
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
	spot.transfer = func(amount float64) (string, error) {
		spot.balance -= amount
		futures.balance += amount
		return "tx-verified", nil
	}
	if err := strategy.ensureFuturesMargin(context.Background(), 200, 200); err != nil {
		t.Fatalf("ensureFuturesMargin: %v", err)
	}
	if spot.transferAmount != 200 || futures.balance != 200 || spot.balance != 300 {
		t.Fatalf("unexpected post-transfer state: transfer %.2f futures %.2f spot %.2f", spot.transferAmount, futures.balance, spot.balance)
	}
}

func TestFundingCarryAutoTransferPreservesOtherBotsWalletReserves(t *testing.T) {
	strategy, futures, spot := newFundingCarryBudgetStrategy(true, 50, 250, 500)
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
	strategy, futures, _ := newFundingCarryBudgetStrategy(false, 50, 1200, 0)
	strategy.profitHarvestEnabled = true
	strategy.profitHarvestMin = 100
	strategy.direction = DirectionForward
	strategy.futQty = 0.02
	futures.latestPrice = 50000
	strategy.harvestProfit(context.Background())
	if math.Abs(futures.transferAmount-150) > 1e-9 {
		t.Fatalf("transferred %.8f USDT; want only 150.00 verified excess", futures.transferAmount)
	}
}

func TestFundingCarryHarvestPreservesAccountCapitalCommitment(t *testing.T) {
	strategy, futures, _ := newFundingCarryBudgetStrategy(false, 50, 1300, 0)
	strategy.profitHarvestEnabled = true
	strategy.profitHarvestMin = 100
	strategy.direction = DirectionForward
	strategy.futQty = 0.02
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
