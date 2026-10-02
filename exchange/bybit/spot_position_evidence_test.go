package bybit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSpotConfirmedFlatSnapshotIsExplicitlyEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v5/asset/transfer/query-account-coins-balance" {
			t.Errorf("unexpected position request: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"retCode":0,"result":{"accountType":"SPOT","balance":[{"coin":"BTC","walletBalance":"0","transferBalance":"0"}]}}`))
	}))
	defer server.Close()
	adapter := newTestBybitSpotAdapter(t, server.URL)
	positions, err := adapter.GetPositions(context.Background(), "BTCUSDT")
	if err != nil || positions == nil || len(positions) != 0 {
		t.Fatalf("confirmed flat snapshot must be explicit empty data: positions=%v err=%v", positions, err)
	}
}
