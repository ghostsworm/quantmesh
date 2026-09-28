package bitfinex

import "testing"

func TestSummarizeBitfinexQuoteWallets(t *testing.T) {
	tests := []struct {
		name      string
		wallets   []WalletInfo
		quote     string
		wantTotal float64
		wantAvail float64
		wantErr   bool
	}{
		{name: "uses exchange quote wallet", wallets: []WalletInfo{{Type: "exchange", Currency: "USD", Balance: 40, BalanceAvailable: 30}, {Type: "margin", Currency: "USD", Balance: 500, BalanceAvailable: 400}, {Type: "exchange", Currency: "BTC", Balance: 2, BalanceAvailable: 1}}, quote: "usd", wantTotal: 40, wantAvail: 30},
		{name: "rejects invalid available", wallets: []WalletInfo{{Type: "exchange", Currency: "USD", Balance: 5, BalanceAvailable: 6}}, quote: "USD", wantErr: true},
		{name: "rejects duplicate quote wallets", wallets: []WalletInfo{{Type: "exchange", Currency: "USD", Balance: 5, BalanceAvailable: 4}, {Type: "exchange", Currency: "USD", Balance: 3, BalanceAvailable: 2}}, quote: "USD", wantErr: true},
		{name: "requires quote", quote: " ", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			total, available, err := summarizeBitfinexQuoteWallets(tt.wallets, tt.quote)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && (total != tt.wantTotal || available != tt.wantAvail) {
				t.Fatalf("balance = (%v, %v), want (%v, %v)", total, available, tt.wantTotal, tt.wantAvail)
			}
		})
	}
}
