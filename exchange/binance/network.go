package binance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"quantmesh/logger"

	"github.com/adshao/go-binance/v2/futures"
)

// 網絡選擇策略（X1 修復）
//
// go-binance 的 futures.UseTestnet 是進程全局變量，WsUserDataServe（用戶數據流）與
// GetAccountInfoWs（WS API）在「每次連接時」讀取它來選擇端點，庫本身沒有提供按實例指定
// WS 端點的入口。過去每次創建適配器（包括 Web 查詢公開 K 線時）都會改寫這個全局變量，
// 導致測試網 Bot 重連用戶數據流 / 查詢 WS 賬戶時被翻到主網。
//
// 現在的做法：
//  1. REST：每個 futures.Client 構造後顯式設置 BaseURL（按實例），不依賴全局變量；
//  2. 公開數據適配器（K 線、exchangeInfo）只走 REST，完全不觸碰全局變量；
//  3. 需要 WS 的交易適配器：由第一個交易適配器「認領」進程網絡並寫一次全局變量，
//     之後所有交易適配器必須使用同一網絡，否則構造直接報錯（拒絕混用主網/測試網）。

const (
	// errCodeTimestampOutsideRecvWindow Binance 錯誤碼 -1021：時間戳超出 recvWindow
	errCodeTimestampOutsideRecvWindow = -1021
	// serverTimeResyncMinInterval 兩次 -1021 觸發的時間重同步之間的最小間隔
	serverTimeResyncMinInterval = 10 * time.Second
	// serverTimeResyncTimeout 單次時間重同步超時
	serverTimeResyncTimeout = 5 * time.Second
	// maxAPIErrorBodyBytes 檢查錯誤響應時最多讀取的字節數
	maxAPIErrorBodyBytes = 64 * 1024
)

var (
	futuresNetworkMu      sync.Mutex
	futuresNetworkClaimed bool
	futuresNetworkTestnet bool

	// futuresRESTBaseURL 按網絡返回 REST 基址；測試中可替換為 httptest 服務地址
	futuresRESTBaseURL = func(useTestnet bool) string {
		if useTestnet {
			return futures.BaseApiTestnetUrl
		}
		return futures.BaseApiMainUrl
	}
)

// claimFuturesNetwork 為需要 WebSocket 的交易適配器認領進程級網絡。
// 首次調用時寫入 futures.UseTestnet；後續調用若網絡不一致則返回錯誤，不再改寫全局變量。
func claimFuturesNetwork(useTestnet bool) error {
	futuresNetworkMu.Lock()
	defer futuresNetworkMu.Unlock()
	if futuresNetworkClaimed {
		if futuresNetworkTestnet != useTestnet {
			return fmt.Errorf("Binance 合約網絡衝突: 進程已使用 testnet=%v，不能再創建 testnet=%v 的交易適配器（go-binance WS 端點為進程全局）",
				futuresNetworkTestnet, useTestnet)
		}
		return nil
	}
	futuresNetworkClaimed = true
	futuresNetworkTestnet = useTestnet
	futures.UseTestnet = useTestnet
	return nil
}

// newFuturesClient 創建按實例綁定 REST 基址的合約客戶端，並在遇到 -1021 時自動重同步服務器時間
func newFuturesClient(apiKey, secretKey string, useTestnet bool) *futures.Client {
	client := futures.NewClient(apiKey, secretKey)
	client.BaseURL = futuresRESTBaseURL(useTestnet)
	installTimeResync(client)
	return client
}

// timeResyncTransport 攔截 HTTP 響應，發現 -1021 時重同步 client.TimeOffset（X6）。
// 注意：TimeOffset 由 go-binance 在請求時無鎖讀取，並發請求下的寫入與庫自身 SetServerTimeService 行為一致。
type timeResyncTransport struct {
	base   http.RoundTripper
	client *futures.Client

	mu       sync.Mutex
	lastSync time.Time
	syncing  bool
}

func installTimeResync(client *futures.Client) {
	httpClient := client.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	cloned := *httpClient
	base := cloned.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	cloned.Transport = &timeResyncTransport{base: base, client: client}
	client.HTTPClient = &cloned
}

func (t *timeResyncTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil || resp == nil || resp.StatusCode < http.StatusBadRequest {
		return resp, err
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxAPIErrorBodyBytes))
	resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))
	if readErr != nil {
		return resp, nil
	}
	var apiErr struct {
		Code int64 `json:"code"`
	}
	if json.Unmarshal(body, &apiErr) == nil && apiErr.Code == errCodeTimestampOutsideRecvWindow {
		t.resync()
	}
	return resp, nil
}

// resync 同步重算時間偏移（限頻、防重入：重同步請求本身也經過此 transport）
func (t *timeResyncTransport) resync() {
	t.mu.Lock()
	if t.syncing || time.Since(t.lastSync) < serverTimeResyncMinInterval {
		t.mu.Unlock()
		return
	}
	t.syncing = true
	t.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), serverTimeResyncTimeout)
	offset, err := t.client.NewSetServerTimeService().Do(ctx)
	cancel()

	t.mu.Lock()
	t.syncing = false
	t.lastSync = time.Now()
	t.mu.Unlock()

	if err != nil {
		logger.Error("❌ [Binance] 收到 -1021 後重同步服務器時間失敗: %v", err)
		return
	}
	logger.Warn("⚠️ [Binance] 收到 -1021，已重同步服務器時間，偏移 %dms", offset)
}
