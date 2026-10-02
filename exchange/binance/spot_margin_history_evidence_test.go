package binance

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	sdk "github.com/adshao/go-binance/v2"
)

func TestSpotMarginHistoryRequiresCrossMarginAssetAndRepayComponents(t *testing.T) {
	for _, mode := range []string{"valid_borrow", "valid_repay", "isolated_borrow", "isolated_repay", "foreign_asset", "missing_principal", "inconsistent_components", "negative_interest"} {
		t.Run(mode, func(t *testing.T) {
			row := map[string]interface{}{"txId": int64(81), "asset": "BTC", "amount": "0.399", "principal": "0.398", "interest": "0.001", "status": "CONFIRMED", "timestamp": int64(1790503199500)}
			kind := "REPAY"
			switch mode {
			case "valid_borrow", "isolated_borrow":
				kind = "BORROW"
				delete(row, "principal")
				delete(row, "interest")
			}
			switch mode {
			case "isolated_borrow", "isolated_repay":
				row["isolatedSymbol"] = "BTCUSDT"
			case "foreign_asset":
				row["asset"] = "ETH"
			case "missing_principal":
				delete(row, "principal")
			case "inconsistent_components":
				row["principal"] = "0.1"
			case "negative_interest":
				row["interest"] = "-0.001"
			}
			body, err := json.Marshal(map[string]interface{}{"rows": []interface{}{row}, "total": 1})
			if err != nil {
				t.Fatal(err)
			}
			client := sdk.NewClient("test-key", "test-secret").SetApiEndpoint("https://margin.test")
			client.HTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body))), Request: req}, nil
			})}
			adapter := &BinanceSpotMarginAdapter{BinanceSpotAdapter: &BinanceSpotAdapter{client: client}, marginClient: NewMarginClient(client)}
			records, total, err := adapter.GetMarginTransactionHistory(context.Background(), "BTC", kind, 1790503199000, 1790503200000, 1, 100)
			valid := mode == "valid_borrow" || mode == "valid_repay"
			if valid {
				if err != nil || total != 1 || len(records) != 1 {
					t.Fatalf("valid evidence rejected: %v", err)
				}
			} else if err == nil || len(records) != 0 {
				t.Fatal("invalid scope or repayment components accepted as authoritative history")
			}
		})
	}
}
