package okx

import "testing"

func TestSummarizeOKXSpotQuoteBalance(t *testing.T) {
	tests := []struct {
		name      string
		balances  []Balance
		quote     string
		wantTotal float64
		wantAvail float64
		wantErr   bool
	}{
		{
			name: "ignores equity in other currencies",
			balances: []Balance{{Details: []BalanceDetail{
				{Ccy: "BTC", Eq: "2", AvailBal: "1"},
				{Ccy: "USDT", Eq: "40", AvailBal: "30"},
				{Ccy: "USDC", Eq: "70", AvailBal: "60"},
			}}},
			quote: "usdt", wantTotal: 40, wantAvail: 30,
		},
		{name: "rejects invalid equity", balances: []Balance{{Details: []BalanceDetail{{Ccy: "USDT", Eq: "Inf", AvailBal: "0"}}}}, quote: "USDT", wantErr: true},
		{name: "rejects available greater than equity", balances: []Balance{{Details: []BalanceDetail{{Ccy: "USDT", Eq: "5", AvailBal: "6"}}}}, quote: "USDT", wantErr: true},
		{name: "rejects duplicate quote rows", balances: []Balance{{Details: []BalanceDetail{{Ccy: "USDT", Eq: "1", AvailBal: "1"}, {Ccy: "USDT", Eq: "2", AvailBal: "2"}}}}, quote: "USDT", wantErr: true},
		{name: "requires quote asset", quote: " ", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			total, available, err := summarizeOKXSpotQuoteBalance(tt.balances, tt.quote)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && (total != tt.wantTotal || available != tt.wantAvail) {
				t.Fatalf("balance = (%v, %v), want (%v, %v)", total, available, tt.wantTotal, tt.wantAvail)
			}
		})
	}
}
