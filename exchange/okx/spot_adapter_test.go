package okx

import (
	"math"
	"testing"
)

func TestRoundToOKXTick(t *testing.T) {
	if got := roundToOKXTick(70052.94, 0.1); math.Abs(got-70052.9) > 1e-6 {
		t.Fatalf("roundToOKXTick = %v, want 70052.9", got)
	}
	if got := roundToOKXTick(70052.95, 0.1); math.Abs(got-70053.0) > 1e-6 {
		t.Fatalf("roundToOKXTick = %v, want 70053.0", got)
	}
}

func TestFloorToOKXLot(t *testing.T) {
	lot := 0.00000001
	qty := 0.004279999
	if got := floorToOKXLot(qty, lot); got < 0.00427999 || got > 0.00428 {
		t.Fatalf("floorToOKXLot = %.12f, want ~0.00427999", got)
	}
}

func TestSymbolToSpotInstId(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"BTCUSDT", "BTC-USDT"},
		{"ETHUSDT", "ETH-USDT"},
		{"  BTCUSDT  ", "BTC-USDT"},
	}
	for _, c := range cases {
		if got := symbolToSpotInstId(c.in); got != c.want {
			t.Errorf("symbolToSpotInstId(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
