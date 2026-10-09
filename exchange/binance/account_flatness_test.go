package binance

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestReadAccountFuturesFlatnessEvidenceRequiresExplicitPositionAndOrderArrays(t *testing.T) {
	tests := []struct {
		name      string
		positions string
		orders    string
		wantFlat  bool
		wantErr   bool
	}{
		{name: "flat", positions: `[]`, orders: `[]`, wantFlat: true},
		{name: "tiny nonzero position", positions: `[{"symbol":"BTCUSDT","positionSide":"BOTH","positionAmt":"0.000000000000000001","isolatedWallet":"0"}]`, orders: `[]`},
		{name: "isolated wallet remains", positions: `[{"symbol":"BTCUSDT","positionSide":"BOTH","positionAmt":"0","isolatedWallet":"0.01"}]`, orders: `[]`},
		{name: "open order remains", positions: `[]`, orders: `[{"orderId":7,"symbol":"ETHUSDT"}]`},
		{name: "positions null", positions: `null`, orders: `[]`, wantErr: true},
		{name: "orders null", positions: `[]`, orders: `null`, wantErr: true},
		{name: "malformed position", positions: `[{"symbol":"BTCUSDT","positionSide":"BOTH","positionAmt":"NaN","isolatedWallet":"0"}]`, orders: `[]`, wantErr: true},
		{name: "malformed order", positions: `[]`, orders: `[{"orderId":0,"symbol":"ETHUSDT"}]`, wantErr: true},
		{name: "orders endpoint failure", positions: `[]`, orders: `api-error`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := equityTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/fapi/v1/time":
					_, _ = fmt.Fprintf(w, `{"serverTime":%d}`, time.Now().UnixMilli())
				case "/fapi/v2/positionRisk":
					if r.URL.Query().Get("symbol") != "" || r.URL.Query().Get("signature") == "" || r.Header.Get("X-MBX-APIKEY") == "" {
						t.Errorf("position query was not signed account-wide request: %s", r.URL.String())
					}
					_, _ = fmt.Fprint(w, tt.positions)
				case "/fapi/v1/openOrders":
					if r.URL.Query().Get("symbol") != "" || r.URL.Query().Get("signature") == "" || r.Header.Get("X-MBX-APIKEY") == "" {
						t.Errorf("open-order query was not signed account-wide request: %s", r.URL.String())
					}
					if tt.orders == "api-error" {
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					}
					_, _ = fmt.Fprint(w, tt.orders)
				default:
					t.Errorf("unexpected Binance flatness request: %s", r.URL.String())
					http.NotFound(w, r)
				}
			})
			evidence, err := adapter.ReadAccountFuturesFlatnessEvidence(context.Background())
			if (err != nil) != tt.wantErr {
				t.Fatalf("evidence=%+v err=%v wantErr=%v", evidence, err, tt.wantErr)
			}
			if tt.wantErr {
				if evidence.Complete {
					t.Fatal("incomplete exchange response returned complete evidence")
				}
				return
			}
			flat, err := evidence.IsFlat()
			if err != nil || flat != tt.wantFlat {
				t.Fatalf("flat=%v want=%v evidence=%+v err=%v", flat, tt.wantFlat, evidence, err)
			}
		})
	}
}

func TestBinanceFuturesFlatnessVerifierUsesRESTOnlyAdapterContract(t *testing.T) {
	adapter := &BinanceAdapter{}
	if err := adapter.VerifyAccountFuturesPositionsAndOrdersFlat(context.Background()); err == nil || !strings.Contains(err.Error(), "requires context and evidence client") {
		t.Fatalf("missing evidence client did not fail closed: %v", err)
	}
}
