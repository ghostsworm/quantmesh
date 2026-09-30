package binance

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/adshao/go-binance/v2/futures"
)

func TestGetIncomeHistoryPaginatesAndVerifiesSignedQueries(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond).Add(-time.Second)
	start, end := now.Add(-time.Minute).UnixMilli(), now.UnixMilli()
	var calls int
	adapter := equityTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fapi/v1/time" {
			_ = json.NewEncoder(w).Encode(map[string]int64{"serverTime": now.UnixMilli()})
			return
		}
		calls++
		query := r.URL.Query()
		if r.Method != http.MethodGet || r.URL.Path != "/fapi/v1/income" || query.Get("symbol") != "BTCUSDT" ||
			query.Get("incomeType") != "FUNDING_FEE" || query.Get("startTime") != strconv.FormatInt(start, 10) ||
			query.Get("endTime") != strconv.FormatInt(end, 10) || query.Get("limit") != "1000" {
			t.Errorf("unexpected income query: %s %s", r.Method, r.URL.String())
		}
		page, err := strconv.Atoi(query.Get("page"))
		if err != nil || page != calls {
			t.Errorf("page=%q, call=%d", query.Get("page"), calls)
		}
		signature := query.Get("signature")
		query.Del("signature")
		mac := hmac.New(sha256.New, []byte("equity-test-secret"))
		_, _ = mac.Write([]byte(query.Encode()))
		if signature != hex.EncodeToString(mac.Sum(nil)) || r.Header.Get("X-MBX-APIKEY") != "equity-test-key" {
			t.Error("income request signature or API key is invalid")
		}
		rows := make([]*equityIncomeWire, 0, 1000)
		if page == 1 {
			for id := int64(1); id <= 1000; id++ {
				rows = append(rows, &equityIncomeWire{Type: "FUNDING_FEE", Symbol: "BTCUSDT", Amount: "-0.01", Asset: "USDT", Time: now.UnixMilli(), TransactionID: id})
			}
		} else {
			rows = append(rows, &equityIncomeWire{Type: "FUNDING_FEE", Symbol: "BTCUSDT", Amount: "0.02", Asset: "USDT", Info: "funding", TradeID: "trade-1001", Time: now.UnixMilli(), TransactionID: 1001})
		}
		_ = json.NewEncoder(w).Encode(rows)
	})
	entries, err := adapter.GetIncomeHistory(context.Background(), "BTCUSDT", "FUNDING_FEE", start, end)
	if err != nil || len(entries) != 1001 || calls != 2 {
		t.Fatalf("entries=%d calls=%d err=%v; want all 1001 rows from two pages", len(entries), calls, err)
	}
	if entries[1000].TransactionID != 1001 || entries[1000].Income != 0.02 || entries[1000].Info != "funding" {
		t.Fatalf("second-page record = %+v", entries[1000])
	}
}

func TestGetIncomeHistoryRejectsDuplicateAcrossPages(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond).Add(-time.Second)
	start, end := now.Add(-time.Minute).UnixMilli(), now.UnixMilli()
	adapter := equityTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fapi/v1/time" {
			_ = json.NewEncoder(w).Encode(map[string]int64{"serverTime": now.UnixMilli()})
			return
		}
		rows := make([]*equityIncomeWire, 0, 1000)
		if r.URL.Query().Get("page") == "1" {
			for id := int64(1); id <= 1000; id++ {
				rows = append(rows, &equityIncomeWire{Type: "FUNDING_FEE", Symbol: "BTCUSDT", Amount: "-0.01", Asset: "USDT", Time: now.UnixMilli(), TransactionID: id})
			}
		} else {
			rows = append(rows, &equityIncomeWire{Type: "FUNDING_FEE", Symbol: "BTCUSDT", Amount: "-0.01", Asset: "USDT", Time: now.UnixMilli(), TransactionID: 1})
		}
		_ = json.NewEncoder(w).Encode(rows)
	})
	if entries, err := adapter.GetIncomeHistory(context.Background(), "BTCUSDT", "FUNDING_FEE", start, end); err == nil || entries != nil {
		t.Fatalf("duplicate income history escaped: entries=%d err=%v", len(entries), err)
	}
}

func TestNormalizeIncomeRecordValidatesCashflowEvidence(t *testing.T) {
	const start, end = int64(1000), int64(2000)
	base := &futures.IncomeHistory{Asset: "USDT", Income: "-0.25", IncomeType: "FUNDING_FEE", Symbol: "BTCUSDT", Time: 1500, TranID: 42}
	got, err := normalizeIncomeRecord(base, "BTCUSDT", "FUNDING_FEE", start, end)
	if err != nil || got == nil || got.Income != -0.25 || got.TransactionID != 42 {
		t.Fatalf("valid income evidence rejected: record=%+v err=%v", got, err)
	}

	cases := []struct {
		name   string
		mutate func(*futures.IncomeHistory)
	}{
		{name: "invalid amount", mutate: func(row *futures.IncomeHistory) { row.Income = "not-a-number" }},
		{name: "nan", mutate: func(row *futures.IncomeHistory) { row.Income = "NaN" }},
		{name: "infinity", mutate: func(row *futures.IncomeHistory) { row.Income = "Inf" }},
		{name: "missing transaction ID", mutate: func(row *futures.IncomeHistory) { row.TranID = 0 }},
		{name: "missing asset", mutate: func(row *futures.IncomeHistory) { row.Asset = " " }},
		{name: "wrong symbol", mutate: func(row *futures.IncomeHistory) { row.Symbol = "ETHUSDT" }},
		{name: "wrong type", mutate: func(row *futures.IncomeHistory) { row.IncomeType = "COMMISSION" }},
		{name: "before interval", mutate: func(row *futures.IncomeHistory) { row.Time = start - 1 }},
		{name: "after interval", mutate: func(row *futures.IncomeHistory) { row.Time = end + 1 }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			row := *base
			test.mutate(&row)
			if got, err := normalizeIncomeRecord(&row, "BTCUSDT", "FUNDING_FEE", start, end); err == nil || got != nil {
				t.Fatalf("invalid income evidence accepted: record=%+v err=%v", got, err)
			}
		})
	}
	if _, err := normalizeIncomeRecord(nil, "BTCUSDT", "FUNDING_FEE", start, end); err == nil {
		t.Fatal("nil income evidence must be rejected")
	}
}
