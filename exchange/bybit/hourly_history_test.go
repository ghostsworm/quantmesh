package bybit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHourlyHistoryIntervalForSpotAndFutures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("interval") != "60" {
			t.Errorf("wrong hourly interval: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"retCode":0,"retMsg":"OK","result":{"list":[["1790208000000","100","110","90","105","1"]]}}`))
	}))
	defer server.Close()
	client := NewBybitClient("", "", false)
	client.baseURL = server.URL
	future := &BybitAdapter{client: client}
	spot := &BybitSpotAdapter{client: client}
	for _, load := range []func(context.Context, string, string, int) ([]*Candle, error){future.GetHistoricalKlines, spot.GetHistoricalKlines} {
		bars, err := load(context.Background(), "BTCUSDT", "1h", 6)
		if err != nil || len(bars) != 1 || bars[0].Close != 105 {
			t.Fatalf("bars=%v error=%v", bars, err)
		}
	}
}
