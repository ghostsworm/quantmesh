package polymarket

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchActiveMarkets_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/events" {
			t.Fatalf("path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{
				"id": "e1",
				"title": "Bitcoin price 2026",
				"markets": [
					{
						"id": "m1",
						"question": "BTC above 100k?",
						"description": "d",
						"outcomes": "[\"Yes\",\"No\"]",
						"volume": "1000",
						"liquidity": "500",
						"endDate": "2026-12-31T00:00:00Z"
					}
				]
			}
		]`))
	}))
	defer srv.Close()

	out, err := FetchActiveMarkets(srv.URL, nil, srv.Client(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].ID != "m1" || out[0].EventTitle != "Bitcoin price 2026" {
		t.Fatalf("unexpected: %+v", out)
	}
}

func TestFetchActiveMarkets_KeywordFilter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id":"a","title":"Fed rate","markets":[{"id":"x","question":"q","outcomes":"[]","volume":"1","liquidity":"1"}]},
			{"id":"b","title":"Bitcoin ETF flows","markets":[{"id":"y","question":"q2","outcomes":"[]","volume":"2","liquidity":"2"}]}
		]`))
	}))
	defer srv.Close()

	out, err := FetchActiveMarkets(srv.URL, []string{"bitcoin"}, srv.Client(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].ID != "y" {
		t.Fatalf("got %+v", out)
	}
}
