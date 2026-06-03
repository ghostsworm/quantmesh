package arbitrage

import "testing"

func TestParsePositionSize(t *testing.T) {
	tests := []struct {
		name      string
		positions []*Position
		want      float64
	}{
		{name: "nil positions", positions: nil, want: 0},
		{name: "empty positions", positions: []*Position{}, want: 0},
		{
			name: "sums long and short positions",
			positions: []*Position{
				{Symbol: "BTCUSDT", Size: 1.25},
				{Symbol: "BTCUSDT", Size: -0.5},
				{Symbol: "ETHUSDT", Size: 2},
			},
			want: 2.75,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parsePositionSize(tt.positions); got != tt.want {
				t.Fatalf("parsePositionSize() = %v, want %v", got, tt.want)
			}
		})
	}
}
