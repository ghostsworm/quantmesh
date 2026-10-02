package gate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSpotConfirmedFlatSnapshotIsExplicitlyEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/spot/accounts" {
			t.Errorf("unexpected position request: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`[{"currency":"BTC","available":"0","locked":"0"}]`))
	}))
	defer server.Close()
	client := NewClient("fixture-key", "fixture-secret", false)
	client.baseURL = server.URL
	adapter := &GateSpotAdapter{client: client, baseAsset: "BTC"}
	positions, err := adapter.GetPositions(context.Background(), "BTCUSDT")
	if err != nil || positions == nil || len(positions) != 0 {
		t.Fatalf("confirmed flat snapshot must be explicit empty data: positions=%v err=%v", positions, err)
	}
}
