package feerate

import (
	"net/url"
	"testing"
)

func TestBitgetTradeRateQueryAndPreHash(t *testing.T) {
	requestPath := "/api/v2/common/trade-rate"
	q := url.Values{}
	q.Set("businessType", "mix")
	q.Set("symbol", "BTCUSDT")
	queryString := q.Encode()
	if queryString != "businessType=mix&symbol=BTCUSDT" {
		t.Fatalf("unexpected encode order/value: %q", queryString)
	}
	ts := "1684814440729"
	const method = "GET"
	preHash := ts + method + requestPath + "?" + queryString
	want := "1684814440729GET/api/v2/common/trade-rate?businessType=mix&symbol=BTCUSDT"
	if preHash != want {
		t.Fatalf("preHash\ngot  %q\nwant %q", preHash, want)
	}
}
