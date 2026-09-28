package strategy

import (
	"context"
	"math"
	"testing"

	"quantmesh/config"
)

type fundingCarryBudgetExchange struct {
	*mockFCExchange
	balance        float64
	balanceErr     error
	transferAmount float64
	transfer       func(amount float64) (string, error)
}

func (e *fundingCarryBudgetExchange) GetBalance(_ context.Context, asset string) (float64, error) {
	if asset != "USDT" {
		return 0, nil
	}
	return e.balance, e.balanceErr
}

func (e *fundingCarryBudgetExchange) InternalTransfer(_ context.Context, _, _, asset string, amount float64) (string, error) {
	if asset != "USDT" {
		return "", nil
	}
	e.transferAmount = amount
	if e.transfer != nil {
		return e.transfer(amount)
	}
	return "mock-transfer", nil
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
