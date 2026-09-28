package binance

import (
	"context"
	"errors"
	"math"
	"testing"

	binancesdk "github.com/adshao/go-binance/v2"
)

func TestValueSpotBalancesUSDTValuesEachAssetAndLockedFunds(t *testing.T) {
	balances := []binancesdk.Balance{
		{Asset: "USDT", Free: "100.25", Locked: "1.75"},
		{Asset: "BTC", Free: "0.001", Locked: "0.002"},
		{Asset: "ETH", Free: "0", Locked: "1"},
		{Asset: "ZERO", Free: "0", Locked: "0"},
	}
	prices := map[string]float64{"BTCUSDT": 50000, "ETHUSDT": 2000}
	var requested []string
	equity, err := valueSpotBalancesUSDT(context.Background(), balances, func(_ context.Context, pair string) (float64, error) {
		requested = append(requested, pair)
		price, ok := prices[pair]
		if !ok {
			return 0, errors.New("missing pair")
		}
		return price, nil
	})
	if err != nil {
		t.Fatalf("value balances: %v", err)
	}
	if math.Abs(equity-2252) > 1e-9 {
		t.Fatalf("equity=%v want 2252", equity)
	}
	if len(requested) != 2 || requested[0] != "BTCUSDT" || requested[1] != "ETHUSDT" {
		t.Fatalf("price requests=%v", requested)
	}
}

func TestValueSpotBalancesUSDTRejectsIncompleteOrInvalidEvidence(t *testing.T) {
	tests := []struct {
		name     string
		balances []binancesdk.Balance
		price    float64
		priceErr error
	}{
		{name: "missing price", balances: []binancesdk.Balance{{Asset: "TOKEN", Free: "1", Locked: "0"}}, priceErr: errors.New("not listed")},
		{name: "non-finite price", balances: []binancesdk.Balance{{Asset: "TOKEN", Free: "1", Locked: "0"}}, price: math.NaN()},
		{name: "invalid amount", balances: []binancesdk.Balance{{Asset: "USDT", Free: "NaN", Locked: "0"}}},
		{name: "negative amount", balances: []binancesdk.Balance{{Asset: "USDT", Free: "1", Locked: "-1"}}},
		{name: "duplicate asset", balances: []binancesdk.Balance{{Asset: "USDT", Free: "1", Locked: "0"}, {Asset: "usdt", Free: "1", Locked: "0"}}},
		{name: "missing balance set", balances: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := valueSpotBalancesUSDT(context.Background(), test.balances, func(context.Context, string) (float64, error) {
				return test.price, test.priceErr
			})
			if err == nil {
				t.Fatal("incomplete or invalid account evidence was accepted")
			}
		})
	}
}
