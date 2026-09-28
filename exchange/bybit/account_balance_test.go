package bybit

import "testing"

func TestSummarizeBybitUnifiedBalance(t *testing.T) {
	tests := []struct {
		name       string
		rows       []Balance
		wantEquity float64
		wantAvail  float64
		wantMargin float64
		wantErr    bool
	}{
		{
			name:       "keeps documented USD valuation fields",
			rows:       []Balance{{TotalEquity: "120.5", TotalAvailableBalance: "80", TotalMarginBalance: "110"}},
			wantEquity: 120.5, wantAvail: 80, wantMargin: 110,
		},
		{name: "empty account is zero USD", rows: nil},
		{name: "rejects invalid equity", rows: []Balance{{TotalEquity: "NaN", TotalAvailableBalance: "1", TotalMarginBalance: "1"}}, wantErr: true},
		{name: "rejects missing available value", rows: []Balance{{TotalEquity: "1", TotalMarginBalance: "1"}}, wantErr: true},
		{name: "rejects multiple account rows", rows: []Balance{{}, {}}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			equity, available, margin, err := summarizeBybitUnifiedBalance(tt.rows)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && (equity != tt.wantEquity || available != tt.wantAvail || margin != tt.wantMargin) {
				t.Fatalf("balance = (%v, %v, %v), want (%v, %v, %v)", equity, available, margin, tt.wantEquity, tt.wantAvail, tt.wantMargin)
			}
		})
	}
}
