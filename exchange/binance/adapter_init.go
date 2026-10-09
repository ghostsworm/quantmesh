package binance

import (
	"context"
	"errors"
	"fmt"
	"quantmesh/logger"
	"strings"
	"time"

	sdk "github.com/adshao/go-binance/v2"
)

var errStopEvidenceReadOnly = errors.New("Binance stop-evidence adapter is read-only")

// NewBinanceAdapter 創建币安适配器
func NewBinanceAdapter(cfg map[string]string, symbol string) (*BinanceAdapter, error) {
	apiKey := cfg["api_key"]
	secretKey := cfg["secret_key"]
	testnetStr := cfg["testnet"]

	// 解析測試網配置
	useTestnet := false
	if testnetStr == "true" {
		useTestnet = true
		logger.Info("🌐 [Binance] 使用測試網模式")
	}

	if apiKey == "" || secretKey == "" {
		return nil, fmt.Errorf("Binance API 配置不完整")
	}

	// 交易適配器需要 WS（用戶數據流 / WS API），其端點由 go-binance 進程全局變量決定：
	// 首個交易適配器認領網絡，之後拒絕混用（詳見 network.go）
	if err := claimFuturesNetwork(useTestnet); err != nil {
		return nil, err
	}

	return newBinanceAdapterWithKeys(apiKey, secretKey, symbol, useTestnet)
}

// NewBinanceAccountEvidenceAdapter creates a REST-only adapter for read-only
// account/ledger reconciliation. It does not claim the process-global futures
// WebSocket network or initialize trading metadata.
func NewBinanceAccountEvidenceAdapter(apiKey, secretKey string, useTestnet bool) (*BinanceAdapter, error) {
	if apiKey == "" || secretKey == "" {
		return nil, fmt.Errorf("Binance account evidence credentials are incomplete")
	}
	return &BinanceAdapter{
		client:         newFuturesClient(apiKey, secretKey, useTestnet),
		apiKey:         apiKey,
		secretKey:      secretKey,
		useTestnet:     useTestnet,
		minAPIInterval: 200 * time.Millisecond,
	}, nil
}

// NewBinanceFuturesStopEvidenceAdapter creates a REST-only futures adapter for
// stopped-runtime reconciliation. It loads exact symbol precision without
// claiming the process-global WebSocket network or creating stream managers.
func NewBinanceFuturesStopEvidenceAdapter(ctx context.Context, apiKey, secretKey string, useTestnet bool, symbol string) (*BinanceAdapter, error) {
	if ctx == nil || strings.TrimSpace(symbol) == "" {
		return nil, fmt.Errorf("Binance futures stop evidence requires context and symbol")
	}
	adapter, err := NewBinanceAccountEvidenceAdapter(apiKey, secretKey, useTestnet)
	if err != nil {
		return nil, err
	}
	adapter.symbol = normalizeBinanceSymbolTypo(symbol)
	adapter.stopEvidenceOnly = true
	if err := adapter.fetchExchangeInfo(ctx); err != nil {
		return nil, fmt.Errorf("load futures stop-evidence symbol metadata: %w", err)
	}
	return adapter, nil
}

// NewBinanceSpotStopEvidenceAdapter creates a REST-only spot adapter for
// stopped-runtime reconciliation. It initializes only public symbol metadata;
// user-data, price, and kline stream managers remain absent.
func NewBinanceSpotStopEvidenceAdapter(ctx context.Context, apiKey, secretKey string, useTestnet bool, symbol string) (*BinanceSpotAdapter, error) {
	if ctx == nil || strings.TrimSpace(apiKey) == "" || strings.TrimSpace(secretKey) == "" || strings.TrimSpace(symbol) == "" {
		return nil, fmt.Errorf("Binance spot stop evidence requires context, credentials, and symbol")
	}
	client := sdk.NewClient(apiKey, secretKey)
	if useTestnet {
		client.SetApiEndpoint("https://testnet.binance.vision")
	}
	return newBinanceSpotStopEvidenceAdapter(ctx, client, symbol, apiKey, secretKey, useTestnet)
}

func newBinanceSpotStopEvidenceAdapter(ctx context.Context, client *sdk.Client, symbol, apiKey, secretKey string, useTestnet bool) (*BinanceSpotAdapter, error) {
	if ctx == nil || client == nil || strings.TrimSpace(symbol) == "" {
		return nil, fmt.Errorf("Binance spot stop evidence requires context, client, and symbol")
	}
	adapter := &BinanceSpotAdapter{
		client: client, symbol: normalizeBinanceSymbolTypo(symbol), apiKey: apiKey,
		secretKey: secretKey, useTestnet: useTestnet, minAPIInterval: 200 * time.Millisecond, stopEvidenceOnly: true,
	}
	if err := adapter.fetchSpotExchangeInfo(ctx); err != nil {
		return nil, fmt.Errorf("load spot stop-evidence symbol metadata: %w", err)
	}
	return adapter, nil
}

// NewBinanceSpotMarginStopEvidenceAdapter creates a REST-only cross-margin
// adapter for stopped-runtime reconciliation. Its embedded spot adapter has
// no WebSocket managers; callers must expose it only to read-only verifiers.
func NewBinanceSpotMarginStopEvidenceAdapter(ctx context.Context, apiKey, secretKey string, useTestnet bool, symbol string) (*BinanceSpotMarginAdapter, error) {
	if ctx == nil || strings.TrimSpace(apiKey) == "" || strings.TrimSpace(secretKey) == "" {
		return nil, fmt.Errorf("Binance margin stop evidence requires context and credentials")
	}
	client := sdk.NewClient(apiKey, secretKey)
	if useTestnet {
		client.SetApiEndpoint("https://testnet.binance.vision")
	}
	return newBinanceSpotMarginStopEvidenceAdapter(ctx, client, apiKey, secretKey, useTestnet, symbol)
}

func newBinanceSpotMarginStopEvidenceAdapter(ctx context.Context, client *sdk.Client, apiKey, secretKey string, useTestnet bool, symbol string) (*BinanceSpotMarginAdapter, error) {
	spot, err := newBinanceSpotStopEvidenceAdapter(ctx, client, symbol, apiKey, secretKey, useTestnet)
	if err != nil {
		return nil, err
	}
	return &BinanceSpotMarginAdapter{BinanceSpotAdapter: spot, marginClient: NewMarginClient(spot.client)}, nil
}

// NewBinanceAdapterForPublicData 創建僅用於獲取公開數據（K 線、交易所信息）的適配器。
// 當 apiKey/secretKey 為空時使用占位符，適用於回測等無需交易權限的場景。Binance K 線為公開 API，無需認證。
func NewBinanceAdapterForPublicData(cfg map[string]string, symbol string) (*BinanceAdapter, error) {
	return NewBinanceAdapterForPublicDataContext(context.Background(), cfg, symbol)
}

// NewBinanceAdapterForPublicDataContext permits canceling initialization HTTP requests.
func NewBinanceAdapterForPublicDataContext(ctx context.Context, cfg map[string]string, symbol string) (*BinanceAdapter, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	apiKey := cfg["api_key"]
	secretKey := cfg["secret_key"]
	testnetStr := cfg["testnet"]

	useTestnet := false
	if testnetStr == "true" {
		useTestnet = true
		logger.Info("🌐 [Binance] 使用測試網模式（公開數據）")
	}

	// 公開數據只走 REST（按實例設置 BaseURL），不得改寫 futures.UseTestnet，
	// 否則會把同進程內測試網 Bot 的 WS 連接翻到主網（X1）

	// 公開 API 無需認證，使用占位符通過客戶端構造
	if apiKey == "" {
		apiKey = "backtest_public"
	}
	if secretKey == "" {
		secretKey = "backtest_public"
	}

	return newBinanceAdapterWithKeysContext(ctx, apiKey, secretKey, symbol, useTestnet)
}

// newBinanceAdapterWithKeys 內部實現，支持占位密鑰（用於僅拉取公開數據如 K 線）
func newBinanceAdapterWithKeys(apiKey, secretKey, symbol string, useTestnet bool) (*BinanceAdapter, error) {
	return newBinanceAdapterWithKeysContext(context.Background(), apiKey, secretKey, symbol, useTestnet)
}

func newBinanceAdapterWithKeysContext(ctx context.Context, apiKey, secretKey, symbol string, useTestnet bool) (*BinanceAdapter, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	symbol = normalizeBinanceSymbolTypo(symbol)

	client := newFuturesClient(apiKey, secretKey, useTestnet)

	// 同步服務器時间（失敗不阻斷構造；後續遇到 -1021 會自動重同步）
	syncCtx, syncCancel := context.WithTimeout(ctx, serverTimeResyncTimeout)
	if _, err := client.NewSetServerTimeService().Do(syncCtx); err != nil {
		logger.Warn("⚠️ [Binance] 初始同步服務器時間失敗 (testnet=%v): %v", useTestnet, err)
	}
	syncCancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	wsManager := NewWebSocketManager(apiKey, secretKey, useTestnet)
	wsManager.symbol = symbol

	adapter := &BinanceAdapter{
		client:                  client,
		symbol:                  symbol,
		apiKey:                  apiKey,
		secretKey:               secretKey,
		wsManager:               wsManager,
		useTestnet:              useTestnet,
		minAPIInterval:          200 * time.Millisecond, // 最小API調用间隔200ms，避免触发限流
		accountCacheTTL:         5 * time.Second,        // 賬戶緩存 5 秒，ACCOUNT_UPDATE 時失效
		accountCacheInvalidated: true,                   // 啟動時無緩存
	}

	// 獲取合約信息（價格精度、數量精度等）
	ctxInit, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := adapter.fetchExchangeInfo(ctxInit); err != nil {
		logger.Warn("⚠️ [Binance] 獲取合約信息失败: %v，使用默认精度", err)
		// 使用默认值
		adapter.priceDecimals = 2
		adapter.quantityDecimals = 3
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if adapter.baseAsset == "" || adapter.quoteAsset == "" {
		b, q := parseFuturesSymbolBaseQuote(adapter.symbol)
		if adapter.baseAsset == "" {
			adapter.baseAsset = b
		}
		if adapter.quoteAsset == "" {
			adapter.quoteAsset = q
		}
	}

	// 註冊 ACCOUNT_UPDATE 回調，WebSocket 收到賬戶變更時失效緩存
	adapter.wsManager.SetOnAccountUpdate(adapter.invalidateAccountCache)

	return adapter, nil
}
