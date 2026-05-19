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
						"outcomePrices": "[\"0.73\",\"0.27\"]",
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
	if out[0].YesProbability != 0.73 || len(out[0].OutcomePrices) != 2 {
		t.Fatalf("unexpected outcome prices: %+v", out[0])
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

func TestFetchActiveMarkets_KeywordFilterUsesQuestionAndDescription(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id":"a","title":"Macro events","markets":[{"id":"x","question":"Will Ethereum ETF inflows rise?","description":"crypto fund flows","outcomes":"[]","volume":"1","liquidity":"1"}]}
		]`))
	}))
	defer srv.Close()

	out, err := FetchActiveMarkets(srv.URL, []string{"ethereum"}, srv.Client(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].ID != "x" {
		t.Fatalf("got %+v", out)
	}
}
