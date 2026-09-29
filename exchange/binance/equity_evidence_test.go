package binance

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/adshao/go-binance/v2/futures"
)

func equityTestAdapter(t *testing.T, handler http.HandlerFunc) *BinanceAdapter {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := futures.NewClient("equity-test-key", "equity-test-secret")
	client.BaseURL = server.URL
	client.HTTPClient = server.Client()
	return &BinanceAdapter{client: client}
}

func equityTestAccount(at time.Time) equityAccountWire {
	mode := false
	updated := at.Add(-time.Minute).UnixMilli()
	return equityAccountWire{MultiAsset: &mode, Wallet: "1000", Unrealized: "-20", Margin: "980",
		Assets: []equityAssetWire{{Asset: "USDT", Wallet: "1000", Available: "930", MaxWithdraw: "900", Unrealized: "-20", Margin: "980", InitialMargin: "50", UpdatedAt: &updated}}}
}

func equityTestIncome(at time.Time, id int64) *equityIncomeWire {
	return &equityIncomeWire{Type: "COMMISSION", Amount: "-0.01", Asset: "USDT", Time: at.UnixMilli(), TransactionID: id}
}

func TestEquityIncomePagesAndTypeScopedIdentity(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond).Add(-time.Second)
	var calls atomic.Int32
	a := equityTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		q := r.URL.Query()
		if r.Method != http.MethodGet || r.URL.Path != "/fapi/v1/income" || q.Get("symbol") != "" || q.Get("incomeType") != "" || q.Get("limit") != "1000" {
			t.Error("unexpected ledger request")
		}
		if q.Get("startTime") != strconv.FormatInt(now.Add(-time.Minute).UnixMilli(), 10) || q.Get("endTime") != strconv.FormatInt(now.UnixMilli(), 10) {
			t.Error("paging changed coverage")
		}
		signedAt, err := strconv.ParseInt(q.Get("timestamp"), 10, 64)
		if err != nil || signedAt < now.UnixMilli() || signedAt > now.Add(800*time.Millisecond).UnixMilli() {
			t.Error("signing discarded remote clock anchor")
		}
		signature := q.Get("signature")
		q.Del("signature")
		mac := hmac.New(sha256.New, []byte("equity-test-secret"))
		_, _ = mac.Write([]byte(q.Encode()))
		if signature != hex.EncodeToString(mac.Sum(nil)) || r.Header.Get("X-MBX-APIKEY") != "equity-test-key" {
			t.Error("invalid signed request")
		}
		rows := make([]*equityIncomeWire, 0)
		switch q.Get("page") {
		case "1":
			for i := int64(1); i <= 1000; i++ {
				rows = append(rows, equityTestIncome(now, i))
			}
		case "2":
			row := equityTestIncome(now, 1)
			row.Type = "FUNDING_FEE"
			rows = append(rows, row)
		default:
			t.Error("unexpected extra page")
		}
		_ = json.NewEncoder(w).Encode(rows)
	})
	entries, err := a.readEquityIncome(context.Background(), now.Add(-time.Minute), now, now)
	if err != nil || len(entries) != 1001 || calls.Load() != 2 {
		t.Fatalf("entries=%d calls=%d err=%v", len(entries), calls.Load(), err)
	}
}

func TestIncomeEvidencePreservesTradingSymbol(t *testing.T) {
	row := equityTestIncome(time.Now().UTC(), 731)
	row.Type = "REALIZED_PNL"
	row.Symbol = "BTCUSDT"
	entry, err := incomeEvidence(row)
	if err != nil {
		t.Fatal(err)
	}
	if entry.Symbol != "BTCUSDT" {
		t.Fatalf("income symbol=%q, want BTCUSDT", entry.Symbol)
	}
}

func TestEquityIncomeRejectsIncompleteEvidence(t *testing.T) {
	for _, name := range []string{"repeat_page", "conflict", "null", "null_row", "bad_amount", "oversized_amount", "unknown_type", "foreign_asset", "missing_id", "outside_interval", "positive_fee", "http_error", "too_many_rows", "invalid_json"} {
		t.Run(name, func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Millisecond)
			a := equityTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
				row := equityTestIncome(now, 1)
				rows := []*equityIncomeWire{row}
				switch name {
				case "repeat_page", "conflict":
					rows = nil
					for i := int64(1); i <= 1000; i++ {
						rows = append(rows, equityTestIncome(now, i))
					}
					if name == "conflict" && r.URL.Query().Get("page") == "2" {
						rows[0].Amount = "-1"
					}
				case "null":
					rows = nil
				case "null_row":
					rows[0] = nil
				case "bad_amount":
					row.Amount = "NaN"
				case "oversized_amount":
					row.Amount = strings.Repeat("9", 100)
				case "unknown_type":
					row.Type = "UNSUPPORTED_CONVERSION"
				case "foreign_asset":
					row.Asset = "BNB"
				case "missing_id":
					row.TransactionID = 0
				case "outside_interval":
					row.Time = now.Add(time.Second).UnixMilli()
				case "positive_fee":
					row.Amount = "1"
				case "http_error":
					w.WriteHeader(http.StatusTooManyRequests)
					return
				case "too_many_rows":
					rows = make([]*equityIncomeWire, 1001)
				case "invalid_json":
					_, _ = fmt.Fprint(w, "[")
					return
				}
				_ = json.NewEncoder(w).Encode(rows)
			})
			entries, err := a.readEquityIncome(context.Background(), now.Add(-time.Minute), now, now)
			if err == nil || entries != nil {
				t.Fatalf("partial evidence escaped: entries=%d err=%v", len(entries), err)
			}
		})
	}
}

func TestEquityIncomeCancellationAndRetention(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	a := equityTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		rows := make([]*equityIncomeWire, 1000)
		for i := range rows {
			rows[i] = equityTestIncome(now, int64(i+1))
		}
		_ = json.NewEncoder(w).Encode(rows)
		cancel()
	})
	if _, err := a.readEquityIncome(ctx, now.Add(-time.Minute), now, now); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel err=%v", err)
	}
	if calls.Load() != 1 {
		t.Fatal("request after cancellation")
	}
	for _, from := range []time.Time{{}, now.Add(-100 * 24 * time.Hour), now.Add(time.Second)} {
		if _, err := a.readEquityIncome(context.Background(), from, now, now); err == nil {
			t.Fatal("invalid interval accepted")
		}
	}
	if calls.Load() != 1 {
		t.Fatal("invalid coverage accessed transport")
	}
}

func TestEquityAccountEvidenceStableSnapshot(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	var accounts atomic.Int32
	a := equityTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/fapi/v1/time":
			if r.Header.Get("X-MBX-APIKEY") != "" {
				t.Error("public clock received credential")
			}
			_ = json.NewEncoder(w).Encode(map[string]int64{"serverTime": now.UnixMilli()})
		case "/fapi/v2/account":
			account := equityTestAccount(now)
			if accounts.Add(1) == 2 {
				account.Margin = "979"
				account.Unrealized = "-21"
				account.Assets[0].Margin = "979"
				account.Assets[0].Unrealized = "-21"
			}
			_ = json.NewEncoder(w).Encode(account)
		case "/fapi/v1/income":
			_ = json.NewEncoder(w).Encode([]*equityIncomeWire{equityTestIncome(now.Add(-time.Minute), 1)})
		default:
			t.Error("unexpected endpoint")
		}
	})
	before := time.Now()
	snapshot, err := a.ReadAccountEvidence(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if accounts.Load() != 2 || snapshot.Equity != 979 || snapshot.Currency != "USDT" || len(snapshot.Entries) != 1 || snapshot.Wallet.Balance != "1000.000000000000000000" {
		t.Fatalf("unexpected fresh account snapshot: %+v", snapshot)
	}
	if !snapshot.Wallet.Through.Equal(now.Add(-time.Millisecond)) || !snapshot.Wallet.From.Equal(now.Add(-equityInitialOverlap)) || snapshot.ObservedAt.Before(before) || snapshot.ObservedAt.After(time.Now()) {
		t.Fatal("mixed local/exchange cursors")
	}
}

func TestEquityAccountEvidenceRejectsUnstableCapture(t *testing.T) {
	for _, name := range []string{"wallet_changed", "cursor_changed", "recent_update", "ledger_during_capture", "clock_skew", "clock_regressed"} {
		t.Run(name, func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Millisecond)
			var accounts, clocks atomic.Int32
			a := equityTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/fapi/v1/time":
					clock := now
					n := clocks.Add(1)
					if name == "clock_skew" {
						clock = clock.Add(-time.Minute)
					}
					if name == "clock_regressed" && n == 2 {
						clock = clock.Add(-time.Millisecond)
					}
					_ = json.NewEncoder(w).Encode(map[string]int64{"serverTime": clock.UnixMilli()})
				case "/fapi/v2/account":
					account := equityTestAccount(now)
					n := accounts.Add(1)
					if name == "recent_update" {
						*account.Assets[0].UpdatedAt = now.UnixMilli()
					}
					if n == 2 && name == "cursor_changed" {
						*account.Assets[0].UpdatedAt++
					}
					if n == 2 && name == "wallet_changed" {
						account.Wallet = "999"
						account.Margin = "979"
						account.Assets[0].Wallet = "999"
						account.Assets[0].Margin = "979"
					}
					_ = json.NewEncoder(w).Encode(account)
				case "/fapi/v1/income":
					_ = json.NewEncoder(w).Encode([]*equityIncomeWire{equityTestIncome(now, 1)})
				default:
					t.Error("unexpected endpoint")
				}
			})
			if _, err := a.ReadAccountEvidence(context.Background(), time.Time{}); err == nil {
				t.Fatal("unstable evidence accepted")
			}
		})
	}
}

func TestEquityAccountEvidenceRejectsUnsupportedValuation(t *testing.T) {
	for _, name := range []string{"multi_asset", "missing_mode", "bad_decimal", "wrong_arithmetic", "wrong_totals", "missing_usdt", "duplicate_asset", "missing_cursor", "foreign_balance", "foreign_initial_margin", "foreign_available", "foreign_max_withdraw", "invalid_initial_margin", "invalid_available", "invalid_max_withdraw", "max_withdraw_above_available"} {
		t.Run(name, func(t *testing.T) {
			account := equityTestAccount(time.Now())
			switch name {
			case "multi_asset":
				*account.MultiAsset = true
			case "missing_mode":
				account.MultiAsset = nil
			case "bad_decimal":
				account.Margin = "NaN"
			case "wrong_arithmetic":
				account.Margin = "900"
			case "wrong_totals":
				account.Assets[0].Wallet = "999"
				account.Assets[0].Margin = "979"
			case "missing_usdt":
				account.Assets = nil
			case "duplicate_asset":
				account.Assets = append(account.Assets, account.Assets[0])
			case "missing_cursor":
				account.Assets[0].UpdatedAt = nil
			case "foreign_balance":
				account.Assets = append(account.Assets, equityAssetWire{Asset: "BNB", Wallet: "1", Margin: "1", Unrealized: "0", InitialMargin: "0"})
			case "foreign_initial_margin":
				account.Assets = append(account.Assets, equityAssetWire{Asset: "USDC", Wallet: "0", Margin: "0", Unrealized: "0", InitialMargin: "1"})
			case "foreign_available":
				account.Assets = append(account.Assets, equityAssetWire{Asset: "BNB", Wallet: "0", Available: "1", MaxWithdraw: "0", Margin: "0", Unrealized: "0", InitialMargin: "0"})
			case "foreign_max_withdraw":
				account.Assets = append(account.Assets, equityAssetWire{Asset: "BNB", Wallet: "0", Available: "0", MaxWithdraw: "1", Margin: "0", Unrealized: "0", InitialMargin: "0"})
			case "invalid_initial_margin":
				account.Assets[0].InitialMargin = "-1"
			case "invalid_available":
				account.Assets[0].Available = "2000"
			case "invalid_max_withdraw":
				account.Assets[0].MaxWithdraw = "NaN"
			case "max_withdraw_above_available":
				account.Assets[0].MaxWithdraw = "931"
			}
			if _, err := account.sample(); err == nil {
				t.Fatal("unsupported valuation accepted")
			}
		})
	}
}

func TestIncomeEvidenceClassifiesPerformanceEntries(t *testing.T) {
	for _, test := range []struct {
		typ, want string
	}{
		{typ: "REALIZED_PNL", want: "realized_pnl"},
		{typ: "INSURANCE_CLEAR", want: "insurance_clear"},
		{typ: "COMMISSION", want: "fee"},
		{typ: "POSITION_LIMIT_INCREASE_FEE", want: "fee"},
	} {
		t.Run(test.typ, func(t *testing.T) {
			entry, err := incomeEvidence(&equityIncomeWire{Type: test.typ, Amount: "-1", Asset: "USDT", Time: 1, TransactionID: 1})
			if err != nil || entry.Kind != test.want {
				t.Fatalf("entry=%+v err=%v, want kind %q", entry, err, test.want)
			}
		})
	}
}

func TestIncomeEvidenceClassifiesWelcomeBonusAsCapitalFlow(t *testing.T) {
	for _, test := range []struct {
		name, amount, want string
	}{
		{name: "credit", amount: "10", want: "transfer_in"},
		{name: "clawback", amount: "-10", want: "transfer_out"},
	} {
		t.Run(test.name, func(t *testing.T) {
			entry, err := incomeEvidence(&equityIncomeWire{Type: "WELCOME_BONUS", Amount: test.amount, Asset: "USDT", Time: 1, TransactionID: 1})
			if err != nil || entry.Kind != test.want {
				t.Fatalf("entry=%+v err=%v, want kind %q", entry, err, test.want)
			}
		})
	}
}

func TestIncomeEvidenceRejectsPositivePositionLimitFee(t *testing.T) {
	_, err := incomeEvidence(&equityIncomeWire{Type: "POSITION_LIMIT_INCREASE_FEE", Amount: "1", Asset: "USDT", Time: 1, TransactionID: 1})
	if err == nil {
		t.Fatal("positive fee must not be treated as an expense or capital flow")
	}
}

func TestIncomeEvidenceRejectsNegativeRebateClawback(t *testing.T) {
	_, err := incomeEvidence(&equityIncomeWire{Type: "COMMISSION_REBATE", Amount: "-1", Asset: "USDT", Time: 1, TransactionID: 1})
	if err == nil {
		t.Fatal("negative rebate cannot be silently treated as zero-impact income")
	}
}

func TestGetAccountFreshReturnsOnlySingleUSDTAvailableBalance(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	for _, multiAsset := range []bool{false, true} {
		t.Run(fmt.Sprintf("multi_asset_%v", multiAsset), func(t *testing.T) {
			var accountCalls atomic.Int32
			a := equityTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/fapi/v1/time":
					_ = json.NewEncoder(w).Encode(map[string]int64{"serverTime": now.UnixMilli()})
				case "/fapi/v2/account":
					accountCalls.Add(1)
					account := equityTestAccount(now)
					*account.MultiAsset = multiAsset
					_ = json.NewEncoder(w).Encode(account)
				default:
					t.Errorf("unexpected endpoint %s", r.URL.Path)
				}
			})
			got, err := a.GetAccountFresh(context.Background())
			if multiAsset {
				if err == nil || got != nil {
					t.Fatal("multi-asset available balance must not be treated as USDT")
				}
				return
			}
			if err != nil || got == nil || got.BalanceAsset != "USDT" || got.AvailableBalance != 930 || got.MaxWithdrawAmount != 900 || accountCalls.Load() != 1 {
				t.Fatalf("fresh USDT available balance=%+v calls=%d err=%v", got, accountCalls.Load(), err)
			}
		})
	}
}

func TestEquityHTTPRejectsRedirectAndRedactsErrors(t *testing.T) {
	var leaked atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Add(1) }))
	defer other.Close()
	a := equityTestAdapter(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL, http.StatusFound) })
	var result interface{}
	err := a.equityGET(context.Background(), "/fapi/v2/account", url.Values{}, time.Now().UnixMilli(), &result)
	if err == nil || leaked.Load() != 0 {
		t.Fatal("signed redirect was followed")
	}
	a.client.BaseURL = "http://127.0.0.1:1"
	err = a.equityGET(context.Background(), "/fapi/v2/account", url.Values{}, time.Now().UnixMilli(), &result)
	if err == nil || strings.Contains(err.Error(), "signature") || strings.Contains(err.Error(), "equity-test") || strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatal("transport details leaked")
	}
}
