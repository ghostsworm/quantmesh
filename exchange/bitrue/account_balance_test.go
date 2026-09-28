package bitrue

import "testing"

func TestSummarizeBitrueQuoteBalance(t *testing.T) {
	tests := []struct {
		name      string
		balances  []Balance
		quote     string
		wantTotal float64
		wantAvail float64
		wantErr   bool
	}{
		{name: "uses configured quote only", balances: []Balance{{Asset: "BTC", Free: "2", Locked: "0"}, {Asset: "USDC", Free: "30", Locked: "4"}}, quote: "usdc", wantTotal: 34, wantAvail: 30},
		{name: "rejects invalid value", balances: []Balance{{Asset: "USDT", Free: "1", Locked: "NaN"}}, quote: "USDT", wantErr: true},
		{name: "rejects duplicate quote", balances: []Balance{{Asset: "USDT", Free: "1", Locked: "0"}, {Asset: "USDT", Free: "2", Locked: "0"}}, quote: "USDT", wantErr: true},
		{name: "requires quote", quote: " ", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			total, available, err := summarizeBitrueQuoteBalance(tt.balances, tt.quote)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && (total != tt.wantTotal || available != tt.wantAvail) {
				t.Fatalf("balance = (%v, %v), want (%v, %v)", total, available, tt.wantTotal, tt.wantAvail)
			}
		})
	}
}
