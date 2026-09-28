package okx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHourlyHistoryIntervalForSpotAndFutures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("bar") != "1H" {
			t.Errorf("wrong hourly interval: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"0","msg":"","data":[["1790208000000","100","110","90","105","1","1","1","1"]]}`))
	}))
	defer server.Close()
	client := NewOKXClient("", "", "", false)
	client.baseURL = server.URL
	future := &OKXAdapter{client: client, instId: "BTC-USDT-SWAP"}
	spot := &OKXSpotAdapter{client: client, instId: "BTC-USDT"}
	for _, load := range []func(context.Context, string, string, int) ([]*Candle, error){future.GetHistoricalKlines, spot.GetHistoricalKlines} {
		bars, err := load(context.Background(), "BTCUSDT", "1h", 6)
		if err != nil || len(bars) != 1 || bars[0].Close != 105 {
			t.Fatalf("bars=%v error=%v", bars, err)
		}
	}
}
