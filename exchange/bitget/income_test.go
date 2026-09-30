package bitget

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestGetIncomeHistoryUsesSignedFundingBillPaginationAndChunks(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	start := now.Add(-45 * 24 * time.Hour)
	firstChunkEnd := start.Add(bitgetBillChunk)
	calls := 0
	signer := NewSigner("api-key", "secret-key", "passphrase")
	client, closeServer := newMockBitgetClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodGet || r.URL.Path != "/api/v2/mix/account/bill" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.RequestURI())
		}
		q := r.URL.Query()
		if q.Get("productType") != "USDC-FUTURES" || q.Get("onlyFunding") != "yes" || q.Get("limit") != "100" {
			t.Fatalf("unexpected bill filters: %v", q)
		}
		timestamp := r.Header.Get("ACCESS-TIMESTAMP")
		if r.Header.Get("ACCESS-KEY") != "api-key" || r.Header.Get("ACCESS-PASSPHRASE") != "passphrase" ||
			r.Header.Get("ACCESS-SIGN") != signer.Sign(timestamp, http.MethodGet, r.URL.RequestURI(), "") {
			t.Fatal("Bitget funding bill request was not signed over its request path")
		}
		var rows []*bitgetBill
		endID := ""
		switch calls {
		case 1:
			if q.Get("startTime") != strconv.FormatInt(start.UnixMilli(), 10) || q.Get("endTime") != strconv.FormatInt(firstChunkEnd.UnixMilli(), 10) || q.Get("idLessThan") != "" {
				t.Fatalf("unexpected first chunk/page: %v", q)
			}
			for id := 200; id > 100; id-- {
				rows = append(rows, testBitgetFundingBill(strconv.Itoa(id), "BTCUSDC", "USDC", start.Add(time.Hour)))
			}
			endID = "101"
		case 2:
			if q.Get("startTime") != strconv.FormatInt(start.UnixMilli(), 10) || q.Get("endTime") != strconv.FormatInt(firstChunkEnd.UnixMilli(), 10) || q.Get("idLessThan") != "101" {
				t.Fatalf("unexpected cursor page: %v", q)
			}
			rows = []*bitgetBill{testBitgetFundingBill("100", "BTCUSDC", "USDC", firstChunkEnd)}
			endID = "100"
		case 3:
			if q.Get("startTime") != strconv.FormatInt(firstChunkEnd.UnixMilli(), 10) || q.Get("endTime") != strconv.FormatInt(now.UnixMilli(), 10) || q.Get("idLessThan") != "" {
				t.Fatalf("unexpected second chunk: %v", q)
			}
			rows = []*bitgetBill{
				testBitgetFundingBill("100", "BTCUSDC", "USDC", firstChunkEnd),
				testBitgetFundingBill("99", "BTCUSDC", "USDC", now.Add(-time.Second)),
				testBitgetFundingBill("98", "ETHUSDC", "USDC", now.Add(-time.Second)),
			}
			endID = "98"
		default:
			t.Fatalf("unexpected request number %d", calls)
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"code": "00000", "data": bitgetBillPage{Bills: rows, EndID: endID}}); err != nil {
			t.Fatalf("encode bills: %v", err)
		}
	})
	defer closeServer()

	adapter := &BitgetAdapter{client: client, symbol: "BTCUSDC", productType: "usdc-futures", marginCoin: "USDC"}
	got, err := adapter.GetIncomeHistory(context.Background(), "BTCUSDC", "FUNDING_FEE", start.UnixMilli(), now.UnixMilli())
	if err != nil {
		t.Fatalf("GetIncomeHistory: %v", err)
	}
	if calls != 3 || len(got) != 102 {
		t.Fatalf("requests=%d entries=%d, want 3 requests and 102 unique funding entries", calls, len(got))
	}
	if got[0].Income != -0.26 || got[0].Asset != "USDC" || got[0].Symbol != "BTCUSDC" || got[0].IncomeType != "FUNDING_FEE" {
		t.Fatalf("first funding bill mapped incorrectly: %+v", got[0])
	}
	if got[101].TransactionID <= 0 || got[101].Info != "bitget_bill_id=99" || !got[101].TradeTime.Equal(now.Add(-time.Second)) {
		t.Fatalf("last funding bill identity/time invalid: %+v", got[101])
	}
}

func TestGetIncomeHistoryRejectsUnsupportedTypeSymbolAndRetention(t *testing.T) {
	adapter := &BitgetAdapter{client: NewClient("key", "secret", "pass", false), symbol: "BTCUSDT", productType: "usdt-futures"}
	now := time.Now().UTC().Truncate(time.Millisecond)
	if _, err := adapter.GetIncomeHistory(context.Background(), "BTCUSDT", "REALIZED_PNL", now.UnixMilli()-1, now.UnixMilli()); err == nil {
		t.Fatal("unsupported income type unexpectedly succeeded")
	}
	if _, err := adapter.GetIncomeHistory(context.Background(), "ETHUSDT", "FUNDING_FEE", now.UnixMilli()-1, now.UnixMilli()); err == nil {
		t.Fatal("mismatched symbol unexpectedly succeeded")
	}
	if _, err := adapter.GetIncomeHistory(context.Background(), "BTCUSDT", "FUNDING_FEE", now.Add(-91*24*time.Hour).UnixMilli(), now.UnixMilli()); err == nil {
		t.Fatal("interval older than API retention unexpectedly succeeded")
	}
}

func TestNormalizeBitgetFundingBillRejectsMissingSymbolOrCouponFee(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	base := testBitgetFundingBill("bill-1", "BTCUSDT", "USDT", now)
	for name, mutate := range map[string]func(*bitgetBill){
		"missing symbol": func(row *bitgetBill) { row.Symbol = "" },
		"coupon fee":     func(row *bitgetBill) { row.FeeByCoupon = "0.01" },
		"invalid amount": func(row *bitgetBill) { row.Amount = "NaN" },
		"missing fee":    func(row *bitgetBill) { row.Fee = "" },
	} {
		t.Run(name, func(t *testing.T) {
			row := *base
			mutate(&row)
			if _, relevant, err := normalizeBitgetFundingBill(&row, "BTCUSDT", now.Add(-time.Second), now.Add(time.Second), "USDT-FUTURES", make(map[int64]string)); err == nil || relevant {
				t.Fatalf("invalid bill accepted: relevant=%t err=%v row=%+v", relevant, err, row)
			}
		})
	}
}

func testBitgetFundingBill(id, symbol, coin string, at time.Time) *bitgetBill {
	return &bitgetBill{ID: id, Symbol: symbol, Amount: "-0.25", Fee: "-0.01", FeeByCoupon: "0", BusinessType: "contract_settle_fee", Coin: coin, Time: strconv.FormatInt(at.UnixMilli(), 10)}
}
