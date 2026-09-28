package gate

import "testing"

func TestSummarizeGateSpotQuoteBalance(t *testing.T) {
	tests := []struct {
		name      string
		rows      []gateSpotAccountBalance
		quote     string
		wantTotal float64
		wantAvail float64
		wantErr   bool
	}{
		{
			name: "ignores other asset quantities",
			rows: []gateSpotAccountBalance{
				{Currency: "BTC", Available: "2", Locked: "1"},
				{Currency: "USDT", Available: "40", Locked: "5"},
				{Currency: "USDC", Available: "70", Locked: "0"},
			},
			quote: "usdt", wantTotal: 45, wantAvail: 40,
		},
		{name: "rejects invalid available", rows: []gateSpotAccountBalance{{Currency: "USDT", Available: "NaN", Locked: "0"}}, quote: "USDT", wantErr: true},
		{name: "rejects negative locked", rows: []gateSpotAccountBalance{{Currency: "USDT", Available: "1", Locked: "-1"}}, quote: "USDT", wantErr: true},
		{name: "rejects duplicate quote rows", rows: []gateSpotAccountBalance{{Currency: "USDT", Available: "1", Locked: "0"}, {Currency: "usdt", Available: "2", Locked: "0"}}, quote: "USDT", wantErr: true},
		{name: "requires quote asset", quote: " ", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			total, available, err := summarizeGateSpotQuoteBalance(tt.rows, tt.quote)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && (total != tt.wantTotal || available != tt.wantAvail) {
				t.Fatalf("balance = (%v, %v), want (%v, %v)", total, available, tt.wantTotal, tt.wantAvail)
			}
		})
	}
}
