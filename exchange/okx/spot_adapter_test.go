package okx

import "testing"

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
