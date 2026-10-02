package bitget

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSpotConfirmedFlatSnapshotIsExplicitlyEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/spot/account/assets" {
			t.Errorf("unexpected position request: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"code":"00000","data":[{"coin":"BTC","available":"0","frozen":"0","locked":"0"}]}`))
	}))
	defer server.Close()
	client := NewClient("fixture-key", "fixture-secret", "fixture-pass", false)
	client.baseURL = server.URL
	adapter := &BitgetSpotAdapter{client: client, baseAsset: "BTC"}
	positions, err := adapter.GetPositions(context.Background(), "BTCUSDT")
	if err != nil || positions == nil || len(positions) != 0 {
		t.Fatalf("confirmed flat snapshot must be explicit empty data: positions=%v err=%v", positions, err)
	}
}
