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
			rows = append(rows, &equityIncomeWire{Type: "FUNDING_FEE", Symbol: "BTCUSDT", Amount: "0.020000000000000001", Asset: "USDT", Info: "funding", TradeID: "trade-1001", Time: now.UnixMilli(), TransactionID: 1001})
		}
		_ = json.NewEncoder(w).Encode(rows)
	})
	entries, err := adapter.GetIncomeHistory(context.Background(), "BTCUSDT", "FUNDING_FEE", start, end)
	if err != nil || len(entries) != 1001 || calls != 2 {
		t.Fatalf("entries=%d calls=%d err=%v; want all 1001 rows from two pages", len(entries), calls, err)
	}
	if entries[1000].TransactionID != 1001 || entries[1000].Income != 0.02 || entries[1000].RawIncome != "0.020000000000000001" || entries[1000].Info != "funding" {
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
	base := &futures.IncomeHistory{Asset: "USDT", Income: "-0.250000000000000001", IncomeType: "FUNDING_FEE", Symbol: "BTCUSDT", Time: 1500, TranID: 42}
	got, err := normalizeIncomeRecord(base, "BTCUSDT", "FUNDING_FEE", start, end)
	if err != nil || got == nil || got.Income != -0.25 || got.RawIncome != "-0.250000000000000001" || got.TransactionID != 42 {
		t.Fatalf("valid income evidence rejected: record=%+v err=%v", got, err)
	}

	cases := []struct {
		name   string
		mutate func(*futures.IncomeHistory)
	}{
		{name: "invalid amount", mutate: func(row *futures.IncomeHistory) { row.Income = "not-a-number" }},
		{name: "missing amount", mutate: func(row *futures.IncomeHistory) { row.Income = "" }},
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

func TestGetFundingIncomeWindowFreezesHalfOpenBoundsAndRequiresStableFullScans(t *testing.T) {
	serverNow := time.Now().UTC().Truncate(time.Millisecond)
	from := serverNow.Add(-10 * time.Minute).Truncate(time.Millisecond)
	rowTime := serverNow.Add(-4 * time.Minute).UnixMilli()
	var scan int
	adapter := equityTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fapi/v1/time" {
			_ = json.NewEncoder(w).Encode(map[string]int64{"serverTime": serverNow.UnixMilli()})
			return
		}
		if r.URL.Path != "/fapi/v1/income" || r.URL.Query().Get("incomeType") != "FUNDING_FEE" || r.URL.Query().Get("symbol") != "BTCUSDT" {
			t.Errorf("unexpected funding scan request: %s", r.URL.String())
		}
		expectedThrough := time.UnixMilli(serverNow.Add(-fundingIncomeMinimumSafetyLag).UnixMilli()).UTC()
		if got, want := r.URL.Query().Get("endTime"), strconv.FormatInt(expectedThrough.UnixMilli()-1, 10); got != want {
			t.Errorf("inclusive API endTime = %q, want half-open conversion %q", got, want)
		}
		scan++
		_ = json.NewEncoder(w).Encode([]*equityIncomeWire{{Type: "FUNDING_FEE", Symbol: "BTCUSDT", Amount: "-0.010000000000000001", Asset: "USDT", Time: rowTime, TransactionID: 71}})
	})
	window, err := adapter.GetFundingIncomeWindow(context.Background(), "BTCUSDT", from, fundingIncomeMinimumSafetyLag)
	if err != nil {
		t.Fatal(err)
	}
	if scan != 2 || window.Symbol != "BTCUSDT" || !window.From.Equal(from) || window.Through.UnixMilli() != serverNow.Add(-fundingIncomeMinimumSafetyLag).UnixMilli() ||
		window.TotalRows != 1 || window.CompletionMarker != fundingIncomeCompletionMarker || window.Rows[0].RawIncome != "-0.010000000000000001" || window.EvidenceHash == "" {
		t.Fatalf("stable window evidence incomplete: scans=%d window=%+v", scan, window)
	}
}

func TestGetFundingIncomeWindowRejectsDriftAndUnsafeCutoff(t *testing.T) {
	serverNow := time.Now().UTC().Truncate(time.Millisecond)
	from := serverNow.Add(-10 * time.Minute).Truncate(time.Millisecond)
	var incomeReads int
	adapter := equityTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fapi/v1/time" {
			_ = json.NewEncoder(w).Encode(map[string]int64{"serverTime": serverNow.UnixMilli()})
			return
		}
		incomeReads++
		amount := "-0.01"
		if incomeReads > 1 {
			amount = "-0.02"
		}
		_ = json.NewEncoder(w).Encode([]*equityIncomeWire{{Type: "FUNDING_FEE", Symbol: "BTCUSDT", Amount: amount, Asset: "USDT", Time: serverNow.Add(-4 * time.Minute).UnixMilli(), TransactionID: 72}})
	})
	if window, err := adapter.GetFundingIncomeWindow(context.Background(), "BTCUSDT", from, fundingIncomeMinimumSafetyLag); err == nil || window.Rows != nil {
		t.Fatalf("drifting source scan was accepted: %+v", window)
	}
	if _, err := adapter.GetFundingIncomeWindow(context.Background(), "BTCUSDT", from, time.Minute); err == nil {
		t.Fatal("unsafe cutoff lag accepted")
	}
}

func TestGetFundingIncomeContinuationDoubleScansWholeHistoryAndSplitsCursorWindow(t *testing.T) {
	serverNow := time.Now().UTC().Truncate(time.Millisecond)
	initialFrom := serverNow.Add(-20 * time.Minute).Truncate(time.Millisecond)
	previousThrough := serverNow.Add(-10 * time.Minute).Truncate(time.Millisecond)
	historicalAt := serverNow.Add(-15 * time.Minute).UnixMilli()
	currentAt := serverNow.Add(-5 * time.Minute).UnixMilli()
	var incomeReads int
	adapter := equityTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fapi/v1/time" {
			_ = json.NewEncoder(w).Encode(map[string]int64{"serverTime": serverNow.UnixMilli()})
			return
		}
		incomeReads++
		query := r.URL.Query()
		wantEnd := serverNow.Add(-fundingIncomeMinimumSafetyLag).UnixMilli() - 1
		if query.Get("startTime") != strconv.FormatInt(initialFrom.UnixMilli(), 10) || query.Get("endTime") != strconv.FormatInt(wantEnd, 10) {
			t.Errorf("continuation query bounds start=%s end=%s", query.Get("startTime"), query.Get("endTime"))
		}
		_ = json.NewEncoder(w).Encode([]*equityIncomeWire{
			{Type: "FUNDING_FEE", Symbol: "BTCUSDT", Amount: "-0.010000000000000001", Asset: "USDT", Time: historicalAt, TransactionID: 81},
			{Type: "FUNDING_FEE", Symbol: "BTCUSDT", Amount: "0.020000000000000001", Asset: "USDT", Time: currentAt, TransactionID: 82},
		})
	})
	continuation, err := adapter.GetFundingIncomeContinuation(context.Background(), "BTCUSDT", initialFrom, previousThrough, fundingIncomeMinimumSafetyLag)
	if err != nil {
		t.Fatal(err)
	}
	if incomeReads != 2 || !continuation.InitialFrom.Equal(initialFrom) || len(continuation.HistoricalRows) != 1 || continuation.HistoricalRows[0].TransactionID != 81 ||
		!continuation.Window.From.Equal(previousThrough) || continuation.Window.TotalRows != 1 || len(continuation.Window.Rows) != 1 || continuation.Window.Rows[0].TransactionID != 82 ||
		continuation.PriorEvidenceHash == "" || continuation.CumulativeEvidenceHash == "" || continuation.Window.EvidenceHash == "" {
		t.Fatalf("continuation did not preserve prefix and new window: reads=%d value=%+v", incomeReads, continuation)
	}
}
