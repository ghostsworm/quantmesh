package config

import "testing"

func TestBotsSameSymbolMarketConflict(t *testing.T) {
	tests := []struct {
		name string
		exA  string
		symA string
		mtA  string
		exB  string
		symB string
		mtB  string
		want bool
	}{
		{
			name: "different_exchange",
			exA:  "binance", symA: "BTCUSDT", mtA: "futures",
			exB:  "okx", symB: "BTCUSDT", mtB: "futures",
			want: false,
		},
		{
			name: "same_spot_futures_no_conflict",
			exA:  "binance", symA: "BTCUSDT", mtA: "spot",
			exB:  "binance", symB: "BTCUSDT", mtB: "futures",
			want: false,
		},
		{
			name: "funding_vs_futures_conflict",
			exA:  "binance", symA: "BTCUSDT", mtA: MarketTypeFundingCarry,
			exB:  "binance", symB: "BTCUSDT", mtB: "futures",
			want: true,
		},
		{
			name: "funding_vs_spot_conflict",
			exA:  "binance", symA: "ETHUSDT", mtA: "spot",
			exB:  "binance", symB: "ETHUSDT", mtB: MarketTypeFundingCarry,
			want: true,
		},
		{
			name: "same_market_futures_conflict",
			exA:  "binance", symA: "BTCUSDT", mtA: "futures",
			exB:  "binance", symB: "BTCUSDT", mtB: "futures",
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BotsSameSymbolMarketConflict(tt.exA, tt.symA, tt.mtA, tt.exB, tt.symB, tt.mtB); got != tt.want {
				t.Errorf("BotsSameSymbolMarketConflict() = %v, want %v", got, tt.want)
			}
		})
	}
}
