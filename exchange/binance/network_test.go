package binance

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/adshao/go-binance/v2/futures"
)

// resetFuturesNetworkForTest 保存並在測試結束時恢復進程級網絡狀態
func resetFuturesNetworkForTest(t *testing.T) {
	t.Helper()
	futuresNetworkMu.Lock()
	prevClaimed, prevTestnet, prevGlobal := futuresNetworkClaimed, futuresNetworkTestnet, futures.UseTestnet
	futuresNetworkClaimed, futuresNetworkTestnet = false, false
	futuresNetworkMu.Unlock()
	prevURL := futuresRESTBaseURL
	t.Cleanup(func() {
		futuresNetworkMu.Lock()
		futuresNetworkClaimed, futuresNetworkTestnet, futures.UseTestnet = prevClaimed, prevTestnet, prevGlobal
		futuresNetworkMu.Unlock()
		futuresRESTBaseURL = prevURL
	})
}

func TestClaimFuturesNetwork_RefusesMixing(t *testing.T) {
	resetFuturesNetworkForTest(t)

	if err := claimFuturesNetwork(true); err != nil {
		t.Fatalf("首次認領測試網失敗: %v", err)
	}
	if !futures.UseTestnet {
		t.Fatal("首次認領後 futures.UseTestnet 應為 true")
	}
	if err := claimFuturesNetwork(true); err != nil {
		t.Fatalf("同網絡再次認領不應報錯: %v", err)
	}
	if err := claimFuturesNetwork(false); err == nil {
		t.Fatal("混用主網應報錯")
	}
	if !futures.UseTestnet {
		t.Fatal("拒絕混用後不得改寫 futures.UseTestnet")
	}
}

func TestNewFuturesClient_PerInstanceBaseURL(t *testing.T) {
	resetFuturesNetworkForTest(t)
	futures.UseTestnet = false

	if got := newFuturesClient("k", "s", true).BaseURL; got != futures.BaseApiTestnetUrl {
		t.Fatalf("testnet BaseURL = %s, want %s", got, futures.BaseApiTestnetUrl)
	}
	futures.UseTestnet = true
	if got := newFuturesClient("k", "s", false).BaseURL; got != futures.BaseApiMainUrl {
		t.Fatalf("mainnet BaseURL = %s, want %s", got, futures.BaseApiMainUrl)
	}
}

// 公開 K 線適配器不得改寫進程全局網絡（X1：Web 查 K 線把測試網 Bot 翻到主網）
func TestNewBinanceAdapterForPublicData_DoesNotTouchGlobalNetwork(t *testing.T) {
	resetFuturesNetworkForTest(t)
	if err := claimFuturesNetwork(true); err != nil {
		t.Fatal(err)
	}

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if r.URL.Path == "/fapi/v1/time" {
			fmt.Fprintf(w, `{"serverTime":%d}`, time.Now().UnixMilli())
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	var requestedTestnet []bool
	futuresRESTBaseURL = func(useTestnet bool) string {
		requestedTestnet = append(requestedTestnet, useTestnet)
		return srv.URL
	}

	adapter, err := NewBinanceAdapterForPublicData(map[string]string{"testnet": "false"}, "BTCUSDT")
	if err != nil {
		t.Fatalf("創建公開數據適配器失敗: %v", err)
	}
	if !futures.UseTestnet {
		t.Fatal("公開數據適配器改寫了 futures.UseTestnet")
	}
	if adapter.client.BaseURL != srv.URL {
		t.Fatalf("client.BaseURL = %s, want %s", adapter.client.BaseURL, srv.URL)
	}
	if atomic.LoadInt32(&hits) == 0 {
		t.Fatal("請求未發往按實例設置的 BaseURL")
	}
	for _, tn := range requestedTestnet {
		if tn {
			t.Fatal("主網公開適配器不應請求測試網基址")
		}
	}
}

func TestNewBinanceAdapter_RefusesMixedNetwork(t *testing.T) {
	resetFuturesNetworkForTest(t)
	if err := claimFuturesNetwork(true); err != nil {
		t.Fatal(err)
	}
	_, err := NewBinanceAdapter(map[string]string{"api_key": "k", "secret_key": "s", "testnet": "false"}, "BTCUSDT")
	if err == nil {
		t.Fatal("測試網進程中創建主網交易適配器應報錯")
	}
}

// X6：收到 -1021 時自動重同步服務器時間，且限頻
func TestTimeResyncOn1021(t *testing.T) {
	const serverLagMs = 5000
	var timeHits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fapi/v1/time" {
			atomic.AddInt32(&timeHits, 1)
			fmt.Fprintf(w, `{"serverTime":%d}`, time.Now().UnixMilli()-serverLagMs)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"code":-1021,"msg":"Timestamp for this request is outside of the recvWindow."}`)
	}))
	defer srv.Close()

	client := futures.NewClient("k", "s")
	client.BaseURL = srv.URL
	installTimeResync(client)

	_, err := client.NewGetAccountService().Do(context.Background())
	if err == nil {
		t.Fatal("期望返回 -1021 錯誤")
	}
	if got := atomic.LoadInt32(&timeHits); got != 1 {
		t.Fatalf("time 端點調用次數 = %d, want 1", got)
	}
	if client.TimeOffset < serverLagMs-1000 || client.TimeOffset > serverLagMs+1000 {
		t.Fatalf("TimeOffset = %d, want ≈ %d", client.TimeOffset, serverLagMs)
	}

	// 限頻：短時間內再次 -1021 不重複同步
	_, _ = client.NewGetAccountService().Do(context.Background())
	if got := atomic.LoadInt32(&timeHits); got != 1 {
		t.Fatalf("限頻失效，time 端點調用次數 = %d", got)
	}
}
