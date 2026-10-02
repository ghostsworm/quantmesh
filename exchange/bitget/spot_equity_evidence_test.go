package bitget

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestBitgetSpotBillEvidenceRequiresClassifiedTypeAndPreservesBalanceChain(t *testing.T) {
	row := &bitgetSpotBill{ID: "14314844815812356565", Coin: "btc", GroupType: "transaction", BusinessType: "BUY", Size: "0.1", Balance: "0.6", Fees: "0.001", Time: "1790000000123"}
	entry, err := bitgetSpotBillEvidence(row, "USDT", nil, context.Background())
	if err != nil || entry.Kind != "realized_pnl" || entry.Currency != "BTC" || entry.Sequence != row.ID || entry.Amount != "" || entry.BalanceAfter != "0.6" {
		t.Fatalf("entry=%+v err=%v", entry, err)
	}
	for _, pair := range [][2]string{{"deposit", "WITHDRAW"}, {"withdraw", "DEPOSIT"}, {"loan", "BORROW"}, {"financial", "INTEREST"}, {"other", "UNKNOWN"}} {
		if _, err := bitgetSpotBillKind(pair[0], pair[1]); err == nil {
			t.Fatalf("unsupported bill type %q/%q accepted", pair[0], pair[1])
		}
	}
}

func TestBitgetSpotExternalBillRequiresCompletedHistoricalValuation(t *testing.T) {
	at := time.Date(2026, 10, 2, 12, 34, 45, 0, time.UTC)
	row := &bitgetSpotBill{ID: "42", Coin: "BTC", GroupType: "deposit", BusinessType: "DEPOSIT", Size: "1", Balance: "11", Fees: "0", Time: strconv.FormatInt(at.UnixMilli(), 10)}
	fetch := func(_ context.Context, symbol string, eventAt time.Time) (string, string, time.Time, error) {
		if symbol != "BTCUSDT" || !eventAt.Equal(at) {
			t.Fatalf("unexpected FX request: %s %s", symbol, eventAt)
		}
		return "65000", "fixture_completed_candle", at.Truncate(time.Minute), nil
	}
	entry, err := bitgetSpotBillEvidence(row, "USDT", fetch, context.Background())
	if err != nil || entry.ValuationRate != "65000.000000000000000000" || entry.ValuationSource != "fixture_completed_candle" || !entry.ValuationAt.Equal(at.Truncate(time.Minute)) {
		t.Fatalf("entry=%+v err=%v", entry, err)
	}
	if _, err := bitgetSpotBillEvidence(row, "USDT", nil, context.Background()); err == nil {
		t.Fatal("external non-USDT flow without historical valuation was accepted")
	}
	feeBearing := *row
	feeBearing.Fees = "0.001"
	if _, err := bitgetSpotBillEvidence(&feeBearing, "USDT", fetch, context.Background()); err == nil {
		t.Fatal("capital flow with a bundled fee was accepted and could hide performance loss")
	}
	negativeWallet := *row
	negativeWallet.Balance = "-1"
	if _, err := bitgetSpotBillEvidence(&negativeWallet, "USDT", fetch, context.Background()); err == nil {
		t.Fatal("negative Spot post-event balance was accepted")
	}
}

func TestBitgetSpotReadAccountEvidenceReturnsCompleteWalletAndBillEvidence(t *testing.T) {
	base := time.Now().UTC().Add(-2 * time.Second).Truncate(time.Millisecond)
	firstAt := base.UnixMilli()
	eventAt := base.Add(-time.Second).UnixMilli()
	var assetCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/spot/account/assets":
			assetCalls++
			_, _ = fmt.Fprintf(w, `{"code":"00000","requestTime":%d,"data":[{"coin":"USDT","available":"100","frozen":"2","locked":"3"}]}`, firstAt+int64(assetCalls-1)*50)
		case "/api/v2/spot/account/bills":
			if r.URL.Query().Get("limit") != "500" || r.URL.Query().Get("startTime") == "" || r.URL.Query().Get("endTime") == "" {
				t.Errorf("unexpected bills query: %s", r.URL.RawQuery)
			}
			_, _ = fmt.Fprintf(w, `{"code":"00000","requestTime":%d,"data":[{"billId":"9001","coin":"USDT","groupType":"transaction","businessType":"BUY","size":"1","balance":"105","fees":"0","cTime":"%d"}]}`, firstAt+75, eventAt)
		case "/api/v2/spot/market/tickers":
			_, _ = fmt.Fprintf(w, `{"code":"00000","requestTime":%d,"data":[{"symbol":"BTCUSDT","lastPr":"60000"}]}`, firstAt+100)
		default:
			t.Errorf("unexpected endpoint: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client := NewClient("key", "secret", "pass", false)
	client.baseURL = server.URL
	client.httpClient = server.Client()
	adapter := &BitgetSpotAdapter{client: client}
	snapshot, err := adapter.ReadAccountEvidence(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	wallet, ok := snapshot.Wallets["USDT"]
	if !ok || wallet.Balance != "105.000000000000000000" || snapshot.Equity != 105 || len(snapshot.Entries) != 1 {
		t.Fatalf("snapshot=%+v wallet=%+v", snapshot, wallet)
	}
	entry := snapshot.Entries[0]
	if entry.ID != "9001" || entry.Sequence != "9001" || entry.BalanceAfter != "105" || entry.Amount != "" || entry.Currency != "USDT" {
		t.Fatalf("spot bill evidence lost exact balance fields: %+v", entry)
	}
}

func TestBitgetSpotBillsFollowNumericIDLessThanCursor(t *testing.T) {
	at := time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond)
	timeText := strconv.FormatInt(at.UnixMilli(), 10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/spot/account/bills" {
			t.Errorf("unexpected request path: %s", r.URL.Path)
		}
		cursor := r.URL.Query().Get("idLessThan")
		rows := make([]bitgetSpotBill, 0, bitgetSpotBillsPageSize)
		if cursor == "" {
			for id := 1000; id >= 501; id-- {
				rows = append(rows, bitgetSpotBill{ID: strconv.Itoa(id), Coin: "USDT", GroupType: "transaction", BusinessType: "BUY", Size: "1", Balance: "1", Fees: "0", Time: timeText})
			}
		} else {
			if cursor != "501" {
				t.Errorf("cursor=%q, want 501", cursor)
			}
			rows = append(rows, bitgetSpotBill{ID: "500", Coin: "USDT", GroupType: "transaction", BusinessType: "BUY", Size: "1", Balance: "1", Fees: "0", Time: timeText})
		}
		_ = json.NewEncoder(w).Encode(struct {
			Code string           `json:"code"`
			Data []bitgetSpotBill `json:"data"`
		}{Code: "00000", Data: rows})
	}))
	defer server.Close()
	client := NewClient("key", "secret", "pass", false)
	client.baseURL = server.URL
	client.httpClient = server.Client()
	entries, err := (&BitgetSpotAdapter{client: client}).readSpotBills(context.Background(), at.Add(-time.Second), at, "USDT")
	if err != nil || len(entries) != 501 || entries[0].Sequence != "500" || entries[len(entries)-1].Sequence != "1000" {
		t.Fatalf("entries=%d first/last sequence=%v err=%v", len(entries), func() []string {
			if len(entries) == 0 {
				return nil
			}
			return []string{entries[0].Sequence, entries[len(entries)-1].Sequence}
		}(), err)
	}
}

func TestBitgetSpotBillsRejectNonDescendingIDsWithinPage(t *testing.T) {
	at := time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond)
	timeText := strconv.FormatInt(at.UnixMilli(), 10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		rows := make([]bitgetSpotBill, 0, bitgetSpotBillsPageSize)
		for id := bitgetSpotBillsPageSize; id >= 1; id-- {
			rows = append(rows, bitgetSpotBill{ID: strconv.Itoa(id), Coin: "USDT", GroupType: "transaction", BusinessType: "BUY", Size: "1", Balance: "1", Fees: "0", Time: timeText})
		}
		rows[0], rows[1] = rows[1], rows[0]
		_ = json.NewEncoder(w).Encode(struct {
			Code string           `json:"code"`
			Data []bitgetSpotBill `json:"data"`
		}{Code: "00000", Data: rows})
	}))
	defer server.Close()
	client := NewClient("key", "secret", "pass", false)
	client.baseURL = server.URL
	client.httpClient = server.Client()
	if _, err := (&BitgetSpotAdapter{client: client}).readSpotBills(context.Background(), at.Add(-time.Second), at, "USDT"); err == nil {
		t.Fatal("out-of-order bill IDs were accepted")
	}
}

func TestBitgetSpotHistoricalRateUsesOnlyTheImmediatelyCompletedCandle(t *testing.T) {
	at := time.Date(2026, 10, 2, 12, 34, 45, 0, time.UTC)
	open := at.Truncate(time.Minute).Add(-time.Minute)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/spot/market/candles" || r.URL.Query().Get("symbol") != "BTCUSDT" || r.URL.Query().Get("granularity") != "1min" || r.URL.Query().Get("startTime") != strconv.FormatInt(open.UnixMilli(), 10) {
			t.Errorf("unexpected historical candle request: %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(fmt.Sprintf(`{"code":"00000","data":[["%d","1","2","1","65000","1","65000","65000"]]}`, open.UnixMilli())))
	}))
	defer server.Close()
	client := NewClient("key", "secret", "pass", false)
	client.baseURL = server.URL
	client.httpClient = server.Client()
	adapter := &BitgetSpotAdapter{client: client}
	rate, source, valuationAt, err := adapter.spotHistoricalRate(context.Background(), "BTCUSDT", at)
	if err != nil || rate != "65000.000000000000000000" || source == "" || !valuationAt.Equal(at.Truncate(time.Minute)) {
		t.Fatalf("rate=%s source=%s at=%s err=%v", rate, source, valuationAt, err)
	}
}

func TestBitgetSpotHistoricalRateAcceptsCandleClosingAtEventBoundary(t *testing.T) {
	at := time.Date(2026, 10, 2, 12, 34, 0, 0, time.UTC)
	open := at.Add(-time.Minute)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"code":"00000","data":[["%d","1","2","1","65000","1","65000","65000"]]}`, open.UnixMilli())
	}))
	defer server.Close()
	client := NewClient("key", "secret", "pass", false)
	client.baseURL = server.URL
	client.httpClient = server.Client()
	rate, _, valuationAt, err := (&BitgetSpotAdapter{client: client}).spotHistoricalRate(context.Background(), "BTCUSDT", at)
	if err != nil || rate == "" || !valuationAt.Equal(at) {
		t.Fatalf("completed candle at the exact event boundary should be usable: rate=%q at=%s err=%v", rate, valuationAt, err)
	}
}
