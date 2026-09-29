package binance

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/adshao/go-binance/v2/futures"
)

func newPermissionTestAdapter(server *httptest.Server) *BinanceAdapter {
	client := futures.NewClient("test-key", "test-secret").SetApiEndpoint(server.URL)
	return &BinanceAdapter{client: client}
}

func TestCheckAPIPermissionsUsesAccountConfigEvidence(t *testing.T) {
	tests := []struct {
		name      string
		response  string
		wantTrade bool
		wantSend  bool
	}{
		{name: "trade only", response: `{"canTrade":true,"canWithdraw":false}`, wantTrade: true},
		{name: "withdraw enabled", response: `{"canTrade":true,"canWithdraw":true}`, wantTrade: true, wantSend: true},
		{name: "trade disabled", response: `{"canTrade":false,"canWithdraw":false}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/fapi/v1/accountConfig" {
					t.Errorf("request path = %q", r.URL.Path)
				}
				if r.Header.Get("X-MBX-APIKEY") != "test-key" {
					t.Errorf("API key header missing")
				}
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			permissions, err := newPermissionTestAdapter(server).CheckAPIPermissions(context.Background())
			if err != nil {
				t.Fatalf("CheckAPIPermissions() error = %v", err)
			}
			if permissions.CanTrade != tt.wantTrade || permissions.CanWithdraw != tt.wantSend || permissions.CanTransfer != tt.wantSend {
				t.Fatalf("permissions = %#v", permissions)
			}
		})
	}
}

func TestCheckAPIPermissionsPropagatesQueryFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "permission query unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	permissions, err := newPermissionTestAdapter(server).CheckAPIPermissions(context.Background())
	if err == nil || permissions != nil || !strings.Contains(err.Error(), "account permissions") {
		t.Fatalf("permissions=%#v error=%v; expected fail-closed query error", permissions, err)
	}
}
