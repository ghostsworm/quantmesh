package exchange

import (
	"context"
	"strings"
	"testing"
)

func TestRejectUnsupportedReduceOnly(t *testing.T) {
	if err := rejectUnsupportedReduceOnly("test", &OrderRequest{}); err != nil {
		t.Fatalf("ordinary order rejected: %v", err)
	}
	err := rejectUnsupportedReduceOnly("test", &OrderRequest{ReduceOnly: true})
	if err == nil || !strings.Contains(err.Error(), "does not support reduce-only") {
		t.Fatalf("reduce-only error = %v, want explicit unsupported error", err)
	}
}

func TestLegacyAdaptersRejectReduceOnlyBeforeVenueCall(t *testing.T) {
	request := &OrderRequest{Symbol: "BTCUSDT", Side: SideSell, Quantity: 1, ReduceOnly: true}
	tests := []struct {
		name string
		call func() error
	}{
		{"AscendEX", func() error { _, err := (*ascendexWrapper)(nil).PlaceOrder(context.Background(), request); return err }},
		{"BTCC", func() error { _, err := (*btccWrapper)(nil).PlaceOrder(context.Background(), request); return err }},
		{"Poloniex", func() error { _, err := (*poloniexWrapper)(nil).PlaceOrder(context.Background(), request); return err }},
		{"Bitrue", func() error { _, err := (*bitrueWrapper)(nil).PlaceOrder(context.Background(), request); return err }},
		{"XT.COM", func() error { _, err := (*xtcomWrapper)(nil).PlaceOrder(context.Background(), request); return err }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.call()
			if err == nil || !strings.Contains(err.Error(), "does not support reduce-only") {
				t.Fatalf("PlaceOrder error = %v, want explicit unsupported reduce-only", err)
			}
		})
	}
}
