package okx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSpotConfirmedFlatSnapshotIsExplicitlyEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v5/account/balance" {
			t.Errorf("unexpected position request: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"code":"0","data":[{"details":[{"ccy":"BTC","availBal":"0","eq":"0"}]}]}`))
	}))
	defer server.Close()
	adapter := newTestOKXSpotAdapter(t, server.URL)
	positions, err := adapter.GetPositions(context.Background(), "BTCUSDT")
	if err != nil || positions == nil || len(positions) != 0 {
		t.Fatalf("confirmed flat snapshot must be explicit empty data: positions=%v err=%v", positions, err)
	}
}
