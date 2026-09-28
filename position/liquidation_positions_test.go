package position

import (
	"math"
	"testing"

	"quantmesh/exchange"
)

func TestPositionSizesFromExchangeRequiresVerifiableEntries(t *testing.T) {
	tests := []struct {
		name      string
		positions []*exchange.Position
		want      []float64
		wantErr   bool
	}{
		{name: "empty", positions: []*exchange.Position{}},
		{name: "valid target and unrelated symbol", positions: []*exchange.Position{{Symbol: "BTCUSDT", Size: 1}, {Symbol: "ETHUSDT", Size: 9}}, want: []float64{1}},
		{name: "nil entry", positions: []*exchange.Position{nil}, wantErr: true},
		{name: "nan quantity", positions: []*exchange.Position{{Symbol: "BTCUSDT", Size: math.NaN()}}, wantErr: true},
		{name: "infinite quantity", positions: []*exchange.Position{{Symbol: "BTCUSDT", Size: math.Inf(1)}}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := positionSizesFromExchange(test.positions, "BTCUSDT")
			if (err != nil) != test.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, test.wantErr)
			}
			if test.wantErr {
				return
			}
			if len(got) != len(test.want) {
				t.Fatalf("sizes = %v, want %v", got, test.want)
			}
			for i := range got {
				if got[i] != test.want[i] {
					t.Fatalf("sizes = %v, want %v", got, test.want)
				}
			}
		})
	}
}
