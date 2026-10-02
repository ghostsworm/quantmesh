package binance

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	binancesdk "github.com/adshao/go-binance/v2"
)

func TestSpotConfirmedFlatSnapshotIsExplicitlyEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/account" {
			t.Errorf("unexpected position request: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"balances":[{"asset":"BTC","free":"0","locked":"0"}]}`))
	}))
	defer server.Close()
	client := binancesdk.NewClient("fixture-key", "fixture-secret")
	client.BaseURL = server.URL
	adapter := &BinanceSpotAdapter{client: client, baseAsset: "BTC"}
	positions, err := adapter.GetPositions(context.Background(), "BTCUSDT")
	if err != nil || positions == nil || len(positions) != 0 {
		t.Fatalf("confirmed flat snapshot must be explicit empty data: positions=%v err=%v", positions, err)
	}
}
