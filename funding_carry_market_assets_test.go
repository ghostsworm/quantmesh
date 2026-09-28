package main

import "testing"

func TestValidateFundingCarryPairAssets(t *testing.T) {
	tests := []struct {
		name       string
		spotBase   string
		spotQuote  string
		hedgeBase  string
		hedgeQuote string
		wantErr    bool
	}{
		{name: "matching USDT pair", spotBase: "btc", spotQuote: "usdt", hedgeBase: "BTC", hedgeQuote: "USDT"},
		{name: "base asset mismatch", spotBase: "BTC", spotQuote: "USDT", hedgeBase: "ETH", hedgeQuote: "USDT", wantErr: true},
		{name: "spot quote asset unsupported", spotBase: "BTC", spotQuote: "USDC", hedgeBase: "BTC", hedgeQuote: "USDT", wantErr: true},
		{name: "hedge quote asset unsupported", spotBase: "BTC", spotQuote: "USDT", hedgeBase: "BTC", hedgeQuote: "USDC", wantErr: true},
		{name: "missing asset identity", spotBase: "BTC", spotQuote: "USDT", hedgeBase: "BTC", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateFundingCarryPairAssets(test.spotBase, test.spotQuote, test.hedgeBase, test.hedgeQuote)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateFundingCarryPairAssets() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}
