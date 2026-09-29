package bitget

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestBitgetSingleUSDTWalletRequiresProvableMode(t *testing.T) {
	base := bitgetEquityAccount{MarginCoin: "USDT", Equity: "999", UnrealizedPL: "-1", Coupon: "0", Grant: "0", AssetMode: "single"}
	wallet, err := bitgetSingleUSDTWallet(base)
	if err != nil || wallet != "1000.000000000000000000" {
		t.Fatalf("wallet=%q err=%v", wallet, err)
	}
	for _, mutate := range []func(*bitgetEquityAccount){
		func(a *bitgetEquityAccount) { a.AssetMode = "union" },
		func(a *bitgetEquityAccount) { a.MarginCoin = "USDC" },
		func(a *bitgetEquityAccount) { a.Coupon = "0.1" },
		func(a *bitgetEquityAccount) { a.Grant = "1" },
		func(a *bitgetEquityAccount) { a.Equity = "NaN" },
	} {
		account := base
		mutate(&account)
		if _, err := bitgetSingleUSDTWallet(account); err == nil {
			t.Fatalf("unsupported account accepted: %+v", account)
		}
	}
}

func TestBitgetBillEvidenceClassifiesAndRejectsUnknownTypes(t *testing.T) {
	row := &bitgetBill{ID: "19", Amount: "-0.25", Fee: "-0.01", BusinessType: "trans_to_exchange", Coin: "USDT", Time: "1790000000000"}
	entry, err := bitgetBillEvidence(row)
	if err != nil || entry.ID != "trans_to_exchange:19" || entry.Kind != "transfer_out" || entry.Amount != "-0.260000000000000000" {
		t.Fatalf("entry=%+v err=%v", entry, err)
	}
	for _, mutate := range []func(*bitgetBill){
		func(b *bitgetBill) { b.BusinessType = "unknown" },
		func(b *bitgetBill) { b.BusinessType = "append_margin" },
		func(b *bitgetBill) { b.BusinessType = "adjust_down_lever_append_margin" },
		func(b *bitgetBill) { b.BusinessType = "reduce_margin" },
		func(b *bitgetBill) { b.BusinessType = "auto_append_margin" },
		func(b *bitgetBill) { b.BusinessType = "risk_captital_user_transfer" },
		func(b *bitgetBill) { b.BusinessType = "user_exchange_buy" },
		func(b *bitgetBill) { b.BusinessType = "user_exchange_sell" },
		func(b *bitgetBill) { b.Coin = "BTC" },
		func(b *bitgetBill) { b.FeeByCoupon = "0.01" },
		func(b *bitgetBill) { b.ID = "" },
		func(b *bitgetBill) { b.Time = "bad" },
	} {
		invalid := *row
		mutate(&invalid)
		if _, err := bitgetBillEvidence(&invalid); err == nil {
			t.Fatalf("invalid bill accepted: %+v", invalid)
		}
	}
}

func TestBitgetReadAccountEvidenceCapturesUSDTBills(t *testing.T) {
	serverTime := time.Now().UTC().Truncate(time.Millisecond)
	client, closeServer := newMockBitgetClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("ACCESS-SIGN") == "" {
			t.Fatalf("unsigned/non-GET evidence request: %s %s", r.Method, r.URL.String())
		}
		switch r.URL.Path {
		case "/api/v2/mix/account/accounts":
			if r.URL.Query().Get("productType") != "USDT-FUTURES" {
				t.Fatalf("productType=%s", r.URL.Query().Get("productType"))
			}
			_, _ = w.Write([]byte(`{"code":"00000","data":[{"marginCoin":"USDT","accountEquity":"999","unrealizedPL":"-1","coupon":"0","grant":"0","assetMode":"single"}],"requestTime":` + strconv.FormatInt(serverTime.UnixMilli(), 10) + `}`))
		case "/api/v2/mix/account/bill":
			q := r.URL.Query()
			if q.Get("productType") != "USDT-FUTURES" || q.Get("onlyFunding") != "yes" || q.Get("limit") != "100" {
				t.Fatalf("bill query=%v", q)
			}
			if _, err := url.ParseQuery(q.Encode()); err != nil {
				t.Fatal(err)
			}
			billTime := serverTime.Add(-time.Second).UnixMilli()
			_, _ = w.Write([]byte(`{"code":"00000","data":{"bills":[{"billId":"77","amount":"-0.25","fee":"-0.01","feeByCoupon":"0","businessType":"contract_settle_fee","coin":"USDT","cTime":"` + strconv.FormatInt(billTime, 10) + `"}],"endId":"77"},"requestTime":` + strconv.FormatInt(serverTime.UnixMilli(), 10) + `}`))
		default:
			t.Errorf("unexpected evidence endpoint %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer closeServer()
	adapter := &BitgetAdapter{client: client}
	snapshot, err := adapter.ReadAccountEvidence(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Currency != "USDT" || snapshot.Equity != 999 || snapshot.Wallet.Balance != "1000.000000000000000000" || len(snapshot.Entries) != 1 {
		t.Fatalf("unexpected account evidence: %+v", snapshot)
	}
	entry := snapshot.Entries[0]
	if entry.ID != "contract_settle_fee:77" || entry.Kind != "funding" || entry.Amount != "-0.260000000000000000" || !strings.EqualFold(entry.Currency, "USDT") {
		t.Fatalf("unexpected bill evidence: %+v", entry)
	}
}

func TestBitgetBillPaginationUsesVerifiedOlderCursor(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	calls := 0
	client, closeServer := newMockBitgetClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		q := r.URL.Query()
		var rows []*bitgetBill
		endID := ""
		if calls == 1 {
			if q.Get("idLessThan") != "" {
				t.Fatalf("first page unexpectedly had cursor %q", q.Get("idLessThan"))
			}
			for id := 200; id > 100; id-- {
				rows = append(rows, &bitgetBill{ID: strconv.Itoa(id), Amount: "1", Fee: "0", BusinessType: "trans_from_exchange", Coin: "USDT", Time: strconv.FormatInt(now.UnixMilli()-1000, 10)})
			}
			endID = "101"
		} else {
			if q.Get("idLessThan") != "101" {
				t.Fatalf("next cursor=%q", q.Get("idLessThan"))
			}
			rows = []*bitgetBill{{ID: "100", Amount: "1", Fee: "0", BusinessType: "trans_from_exchange", Coin: "USDT", Time: strconv.FormatInt(now.UnixMilli()-1000, 10)}}
			endID = "100"
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"code": "00000", "requestTime": now.UnixMilli(), "data": bitgetBillPage{Bills: rows, EndID: endID}})
	})
	defer closeServer()
	adapter := &BitgetAdapter{client: client}
	entries, err := adapter.readEquityBills(context.Background(), now.Add(-time.Minute), now)
	if err != nil || calls != 2 || len(entries) != 101 {
		t.Fatalf("entries=%d calls=%d err=%v", len(entries), calls, err)
	}
	if entries[0].ID != "trans_from_exchange:100" || entries[len(entries)-1].ID != "trans_from_exchange:200" {
		t.Fatalf("entries not ordered or missing: first=%s last=%s", entries[0].ID, entries[len(entries)-1].ID)
	}
}

func TestBitgetFullBillPageRejectsWrongEndID(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	client, closeServer := newMockBitgetClient(t, func(w http.ResponseWriter, r *http.Request) {
		rows := make([]*bitgetBill, bitgetBillPageSize)
		for i := range rows {
			rows[i] = &bitgetBill{ID: strconv.Itoa(i + 1), Amount: "1", Fee: "0", BusinessType: "trans_from_exchange", Coin: "USDT", Time: strconv.FormatInt(now.UnixMilli()-1000, 10)}
		}
		_, _ = fmt.Fprintf(w, `{"code":"00000","data":{"bills":%s,"endId":"wrong"},"requestTime":%d}`, mustJSON(t, rows), now.UnixMilli())
	})
	defer closeServer()
	adapter := &BitgetAdapter{client: client}
	if _, err := adapter.readEquityBills(context.Background(), now.Add(-time.Minute), now); err == nil {
		t.Fatal("full page with an unverified cursor was accepted")
	}
}

func mustJSON(t *testing.T, value interface{}) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
