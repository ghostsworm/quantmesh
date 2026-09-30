package bitget

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

type bitgetRoundTripFunc func(*http.Request) (*http.Response, error)

func (f bitgetRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestGetOrderFillsUsesAuthenticatedCursorAndMapsLedgerFields(t *testing.T) {
	client := NewClient("test-key", "test-secret", "test-passphrase", false)
	client.baseURL = "https://bitget.test"
	requests := 0
	client.httpClient.Transport = bitgetRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		query := req.URL.Query()
		if req.URL.Path != "/api/v2/mix/order/fills" || query.Get("productType") != "usdt-futures" ||
			query.Get("symbol") != "BTCUSDT" || query.Get("orderId") != "71" || query.Get("limit") != "100" {
			return nil, fmt.Errorf("unexpected Bitget fill request: %s", req.URL.String())
		}
		if req.Header.Get("ACCESS-KEY") != "test-key" || req.Header.Get("ACCESS-SIGN") == "" || req.Header.Get("ACCESS-TIMESTAMP") == "" {
			return nil, fmt.Errorf("Bitget fill request was not signed")
		}
		page := map[string]any{"fillList": []map[string]any{}}
		if requests == 1 {
			if query.Get("idLessThan") != "" {
				return nil, fmt.Errorf("first page unexpectedly had a cursor")
			}
			rows := make([]map[string]any, 100)
			for i := range rows {
				rows[i] = bitgetFillFixture("trade-"+strconv.Itoa(i+1), "open", "0", "maker")
			}
			rows[0] = bitgetFillFixture("trade-1", "close", "2.5", "taker")
			page["fillList"] = rows
			page["endId"] = "trade-100"
		} else {
			if query.Get("idLessThan") != "trade-100" {
				return nil, fmt.Errorf("second page did not use endId cursor: %s", req.URL.RawQuery)
			}
			page["fillList"] = []map[string]any{bitgetFillFixture("trade-101", "open", "0", "maker")}
			page["endId"] = "trade-101"
		}
		data, err := json.Marshal(map[string]any{"code": "00000", "data": page, "msg": "success"})
		if err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(data))), Request: req, ContentLength: int64(len(data))}, nil
	})
	adapter := &BitgetAdapter{client: client, productType: "usdt-futures", marginCoin: "USDT"}
	fills, err := adapter.GetOrderFills(context.Background(), "BTCUSDT", 71)
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 || len(fills) != 101 {
		t.Fatalf("pagination returned requests=%d fills=%d; want 2 pages and 101 fills", requests, len(fills))
	}
	closeFill := fills[0]
	if closeFill.RealizedPnL != 2.5 || !closeFill.RealizedPnLKnown || closeFill.RealizedPnLAsset != "USDT" ||
		closeFill.Commission != 0.001 || closeFill.CommissionAsset != "USDT" || closeFill.IsMaker {
		t.Fatalf("close fill mapping lost fee/PnL evidence: %+v", closeFill)
	}
	if !fills[1].RealizedPnLKnown || fills[1].RealizedPnL != 0 || fills[1].RealizedPnLAsset != "USDT" || fills[1].Commission != 0.001 || !fills[1].IsMaker {
		t.Fatalf("open fill or taker/maker evidence mapped incorrectly: %+v", fills[1])
	}
}

func bitgetFillFixture(tradeID, tradeSide, profit, tradeScope string) map[string]any {
	return map[string]any{
		"tradeId": tradeID, "symbol": "btcusdt", "orderId": "71", "price": "100",
		"baseVolume": "0.1", "quoteVolume": "10", "feeDetail": []map[string]string{{"feeCoin": "USDT", "totalFee": "-0.001"}},
		"side": "buy", "profit": profit, "tradeSide": tradeSide, "tradeScope": tradeScope, "cTime": "1790503200000",
	}
}

func TestGetOrderFillsRejectsMixedFeeCurrencies(t *testing.T) {
	row := bitgetFillFixture("trade-1", "close", "1", "taker")
	row["feeDetail"] = []map[string]string{{"feeCoin": "USDT", "totalFee": "-0.1"}, {"feeCoin": "BGB", "totalFee": "-0.01"}}
	encoded, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	var decoded bitgetOrderFillResponse
	if err := json.Unmarshal([]byte(`{"fillList":[`+string(encoded)+`]}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if _, err := parseBitgetOrderFill(decoded.FillList[0], 71, "BTCUSDT", "USDT"); err == nil || !strings.Contains(err.Error(), "mixed fee currencies") {
		t.Fatalf("mixed fee currencies must be rejected: %v", err)
	}
}
