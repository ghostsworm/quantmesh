package web

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"quantmesh/config"
	"quantmesh/exchange"
)

type fakeCapitalExchange struct {
	exchange.IExchange
	name    string
	quote   string
	account *exchange.Account
	err     error
}

func (f fakeCapitalExchange) GetName() string       { return f.name }
func (f fakeCapitalExchange) GetQuoteAsset() string { return f.quote }
func (f fakeCapitalExchange) GetAccount(ctx context.Context) (*exchange.Account, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.account, nil
}

type fakeCapitalDataSource struct {
	exchanges []exchange.IExchange
	strategy  map[string]config.StrategyConfig
	positions []PositionManagerInfo
	cfg       *config.Config
}

func (f fakeCapitalDataSource) GetExchanges() []exchange.IExchange {
	return f.exchanges
}
func (f fakeCapitalDataSource) GetStrategyConfigs() map[string]config.StrategyConfig {
	return f.strategy
}
func (f fakeCapitalDataSource) GetPositionManagers() []PositionManagerInfo {
	return f.positions
}
func (f fakeCapitalDataSource) GetConfig() *config.Config {
	return f.cfg
}

func TestCapitalHandlersReturnNotReadyWithoutDataSource(t *testing.T) {
	old := capitalDataSource
	t.Cleanup(func() { capitalDataSource = old })
	capitalDataSource = nil

	for _, tc := range []struct {
		name    string
		handler func(*gin.Context)
		path    string
	}{
		{"overview", getCapitalOverviewHandler, "/api/capital/overview"},
		{"usage", getCapitalUsageHandler, "/api/capital/usage"},
		{"allocation", getCapitalAllocationHandler, "/api/capital/allocation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, tc.path, nil)

			tc.handler(c)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", w.Code)
			}
			var body map[string]interface{}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body["success"] != false {
				t.Fatalf("expected success=false, got %s", w.Body.String())
			}
		})
	}
}

func TestCapitalOverviewAggregatesExchangeBalancesAndCache(t *testing.T) {
	old := capitalDataSource
	// 只備份資料欄位：整個結構體含 sync.RWMutex，
	// 直接賦值會複製鎖（go vet copylocks），還可能把鎖狀態一起搬走
	oldOverview := capitalOverviewCache.overview
	oldAt := capitalOverviewCache.at
	oldTTL := capitalOverviewCache.ttl
	t.Cleanup(func() {
		capitalDataSource = old
		capitalOverviewCache.overview = oldOverview
		capitalOverviewCache.at = oldAt
		capitalOverviewCache.ttl = oldTTL
	})
	capitalOverviewCache.at = time.Time{}
	capitalOverviewCache.ttl = time.Minute

	capitalDataSource = fakeCapitalDataSource{
		exchanges: []exchange.IExchange{
			fakeCapitalExchange{name: "binance", account: &exchange.Account{
				TotalWalletBalance: 900,
				TotalMarginBalance: 1000,
				AvailableBalance:   700,
			}},
			fakeCapitalExchange{name: "gate", err: errors.New("offline")},
			fakeCapitalExchange{name: "binance", account: &exchange.Account{TotalMarginBalance: 1}},
		},
		strategy: map[string]config.StrategyConfig{
			"grid": {Enabled: true, Weight: 0.25},
			"off":  {Enabled: false, Weight: 0.75},
		},
		cfg: &config.Config{Exchanges: map[string]config.ExchangeConfig{
			"binance": {Testnet: true},
			"gate":    {Testnet: false},
		}},
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/capital/overview", nil)
	getCapitalOverviewHandler(c)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var first struct {
		Success  bool            `json:"success"`
		Overview CapitalOverview `json:"overview"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &first); err != nil {
		t.Fatalf("decode first overview: %v", err)
	}
	if !first.Success || first.Overview.TotalBalance != 0 || first.Overview.AvailableCapital != 0 || first.Overview.ValuationComplete || first.Overview.ValuationError == "" {
		t.Fatalf("unexpected overview: %#v", first)
	}
	if first.Overview.AllocatedCapital != 0 || first.Overview.UnrealizedPnL != 0 {
		t.Fatalf("unexpected allocation/pnl: %#v", first.Overview)
	}
	if len(first.Overview.Exchanges) != 2 || first.Overview.Exchanges[1].Status != "error" {
		t.Fatalf("unexpected exchange summaries: %#v", first.Overview.Exchanges)
	}

	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/capital/overview", nil)
	getCapitalOverviewHandler(c)
	var second struct {
		Success  bool            `json:"success"`
		Overview CapitalOverview `json:"overview"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &second); err != nil {
		t.Fatalf("decode second overview: %v", err)
	}
	if !second.Success || second.Overview.TotalBalance != first.Overview.TotalBalance || second.Overview.ValuationComplete {
		t.Fatalf("cached overview mismatch: %#v", second)
	}
}

func TestGetCompleteExchangeBalanceRejectsIncompleteAndInvalidSnapshots(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name      string
		exchanges []exchange.IExchange
		wantErr   bool
	}{
		{name: "complete and deduplicated", exchanges: []exchange.IExchange{
			fakeCapitalExchange{name: "binance", quote: "USDT", account: &exchange.Account{TotalMarginBalance: 12, BalanceAsset: "USDT"}},
			fakeCapitalExchange{name: "BINANCE", quote: "USDT", account: &exchange.Account{TotalMarginBalance: 900, BalanceAsset: "USDT"}},
			fakeCapitalExchange{name: "okx", quote: "USDT", account: &exchange.Account{TotalMarginBalance: 8, BalanceAsset: "USDT"}},
		}},
		{name: "partial account failure", exchanges: []exchange.IExchange{
			fakeCapitalExchange{name: "binance", quote: "USDT", account: &exchange.Account{TotalMarginBalance: 12, BalanceAsset: "USDT"}},
			fakeCapitalExchange{name: "okx", quote: "USDT", err: errors.New("unavailable")},
		}, wantErr: true},
		{name: "nil snapshot", exchanges: []exchange.IExchange{
			fakeCapitalExchange{name: "binance", quote: "USDT"},
		}, wantErr: true},
		{name: "non finite balance", exchanges: []exchange.IExchange{
			fakeCapitalExchange{name: "binance", quote: "USDT", account: &exchange.Account{TotalMarginBalance: math.NaN(), BalanceAsset: "USDT"}},
		}, wantErr: true},
		{name: "different valuation assets", exchanges: []exchange.IExchange{
			fakeCapitalExchange{name: "binance", quote: "USDT", account: &exchange.Account{TotalMarginBalance: 12, BalanceAsset: "USDT"}},
			fakeCapitalExchange{name: "okx", quote: "USD", account: &exchange.Account{TotalMarginBalance: 8, BalanceAsset: "USD"}},
		}, wantErr: true},
		{name: "missing valuation asset", exchanges: []exchange.IExchange{
			fakeCapitalExchange{name: "binance", quote: "USDT", account: &exchange.Account{TotalMarginBalance: 12}},
		}, wantErr: true},
		{name: "no exchanges", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getCompleteExchangeBalance(ctx, tt.exchanges)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != 20 {
				t.Fatalf("balance = %v, want deduplicated total 20", got)
			}
			if tt.wantErr && got != 0 {
				t.Fatalf("failed snapshot exposed partial balance %v", got)
			}
		})
	}
}

func TestCapitalUsageIncludesOnlineOfflineAndErrorExchanges(t *testing.T) {
	old := capitalDataSource
	t.Cleanup(func() { capitalDataSource = old })
	capitalDataSource = fakeCapitalDataSource{
		exchanges: []exchange.IExchange{
			fakeCapitalExchange{name: "binance", account: &exchange.Account{
				TotalWalletBalance: 800,
				TotalMarginBalance: 1000,
				AvailableBalance:   600,
			}},
			fakeCapitalExchange{name: "gate", err: errors.New("down")},
		},
		cfg: &config.Config{Exchanges: map[string]config.ExchangeConfig{
			"binance": {Testnet: true},
			"gate":    {Testnet: false},
			"okx":     {Testnet: true},
		}},
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/capital/usage", nil)
	getCapitalUsageHandler(c)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var body struct {
		Success   bool                  `json:"success"`
		Exchanges []ExchangeUsageDetail `json:"exchanges"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode usage: %v", err)
	}
	if !body.Success || len(body.Exchanges) < 3 {
		t.Fatalf("unexpected usage body: %#v", body)
	}
	statuses := map[string]string{}
	for _, item := range body.Exchanges {
		statuses[item.ExchangeID] = item.Status
	}
	if statuses["binance"] != "online" || statuses["gate"] != "error" || statuses["okx"] != "offline" {
		t.Fatalf("unexpected statuses: %#v", statuses)
	}
}
