package coinex

import "testing"

func TestSummarizeCoinExQuoteBalance(t *testing.T) {
	tests := []struct {
		name      string
		balance   Balance
		quote     string
		wantTotal float64
		wantAvail float64
		wantErr   bool
	}{
		{name: "uses configured quote only", balance: Balance{Available: map[string]string{"BTC": "2", "USDC": "30"}, Frozen: map[string]string{"USDC": "4"}}, quote: "usdc", wantTotal: 34, wantAvail: 30},
		{name: "missing quote means zero", balance: Balance{Available: map[string]string{"BTC": "2"}}, quote: "USDT"},
		{name: "rejects invalid value", balance: Balance{Available: map[string]string{"USDT": "NaN"}}, quote: "USDT", wantErr: true},
		{name: "requires quote", quote: " ", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			total, available, err := summarizeCoinExQuoteBalance(tt.balance, tt.quote)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && (total != tt.wantTotal || available != tt.wantAvail) {
				t.Fatalf("balance = (%v, %v), want (%v, %v)", total, available, tt.wantTotal, tt.wantAvail)
			}
		})
	}
}
