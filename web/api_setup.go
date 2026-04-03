package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/adshao/go-binance/v2/futures"
	"quantmesh/config"
	"quantmesh/logger"
)

// SetupStatusResponse 配置状態响应
type SetupStatusResponse struct {
	NeedsSetup bool                            `json:"needs_setup"`
	ConfigPath string                          `json:"config_path"`
	Exchanges  map[string]config.ExchangeConfig `json:"exchanges,omitempty"`
	Symbols    []config.SymbolConfig           `json:"symbols,omitempty"`
}

// getSetupStatusHandler 獲取配置状態
// GET /api/setup/status
func getSetupStatusHandler(c *gin.Context) {
	const configPathHint = "" // 主配置僅存於主庫 app_config，不再使用磁盤路徑

	if fileConfigManager == nil {
		c.JSON(http.StatusOK, SetupStatusResponse{
			NeedsSetup: true,
			ConfigPath: configPathHint,
		})
		return
	}

	cfg, err := fileConfigManager.GetConfig()
	if err != nil || cfg == nil {
		c.JSON(http.StatusOK, SetupStatusResponse{
			NeedsSetup: true,
			ConfigPath: configPathHint,
		})
		return
	}

	needsSetup := cfg.App.CurrentExchange == "" ||
		len(cfg.Exchanges) == 0 ||
		cfg.Exchanges[cfg.App.CurrentExchange].APIKey == "" ||
		cfg.Exchanges[cfg.App.CurrentExchange].SecretKey == "" ||
		len(cfg.Trading.Symbols) == 0 ||
		cfg.Trading.Symbols[0].Symbol == ""

	c.JSON(http.StatusOK, SetupStatusResponse{
		NeedsSetup: needsSetup,
		ConfigPath: configPathHint,
		Exchanges:  cfg.Exchanges,
		Symbols:    cfg.Trading.Symbols,
	})
}

// SetupInitRequest 配置初始化请求
type SetupInitRequest struct {
	Exchange       string   `json:"exchange" binding:"required"`
	APIKey         string   `json:"api_key" binding:"required"`
	SecretKey      string   `json:"secret_key" binding:"required"`
	Passphrase     string   `json:"passphrase,omitempty"`
	Symbol         string   `json:"symbol,omitempty"`        // 向后兼容，但优先使用 Symbols
	Symbols        []string `json:"symbols,omitempty"`       // 多交易對支援
	PriceInterval  float64  `json:"price_interval" binding:"required,gt=0"`
	ProfitSpread   float64  `json:"profit_spread,omitempty"`                  // 利潤間距（可選，為 0 時等於 PriceInterval）
	OrderQuantity  float64  `json:"order_quantity" binding:"required,gt=0"`
	MinOrderValue  float64  `json:"min_order_value,omitempty"`
	BuyWindowSize  int      `json:"buy_window_size" binding:"required,gt=0"`
	SellWindowSize int      `json:"sell_window_size,omitempty"`
	Testnet        bool     `json:"testnet,omitempty"`
	FeeRate        float64  `json:"fee_rate,omitempty"`
}

// SetupInitResponse 配置初始化响应
type SetupInitResponse struct {
	Success         bool   `json:"success"`
	Message         string `json:"message"`
	RequiresRestart bool   `json:"requires_restart"`
	BackupPath      string `json:"backup_path,omitempty"` // 备份文件路径（如果存在）
}

// initSetupHandler 初始化配置（僅首次設置或已认证用戶可用）
// POST /api/setup/init
func initSetupHandler(c *gin.Context) {
	// 🔒 安全检查：如果已經設置過密碼，则需要认证
	if globalPasswordManager != nil {
		username := "admin"
		hasPassword, err := globalPasswordManager.HasPassword(username)
		if err == nil && hasPassword {
			// 已設置密碼，检查是否已认证
			sm := GetSessionManager()
			if sm != nil {
				session, exists := sm.GetSessionFromRequest(c.Request)
				if !exists || session == nil {
					logger.Warn("⚠️ [SECURITY] 拒绝未认证的配置初始化请求，IP: %s", c.ClientIP())
					c.JSON(http.StatusUnauthorized, SetupInitResponse{
						Success: false,
						Message: "系统已初始化，需要登錄后才能修改配置",
					})
					return
				}
				logger.Info("✅ [SECURITY] 已认证用戶 %s 正在修改配置", session.Username)
			}
		}
	}
	
	var req SetupInitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, SetupInitResponse{
			Success: false,
			Message: "请求参數錯误: " + err.Error(),
		})
		return
	}

	// 确定要使用的交易對列表
	var symbols []string
	if len(req.Symbols) > 0 {
		// 优先使用 Symbols 數组
		symbols = req.Symbols
	} else if req.Symbol != "" {
		// 向后兼容：使用單個 Symbol
		symbols = []string{req.Symbol}
	} else {
		c.JSON(http.StatusBadRequest, SetupInitResponse{
			Success: false,
			Message: "请至少指定一個交易對（使用 symbol 或 symbols 字段）",
		})
		return
	}

	var cfg *config.Config
	if fileConfigManager != nil {
		if existingCfg, err := fileConfigManager.GetConfig(); err == nil && existingCfg != nil {
			cfg = existingCfg
		}
	}
	if cfg == nil {
		cfg = config.CreateMinimalConfig()
	}

	// 設置交易所
	if cfg.App.CurrentExchange == "" {
	cfg.App.CurrentExchange = req.Exchange
	}

	// 設置交易所配置
	exchangeCfg := config.ExchangeConfig{
		APIKey:     req.APIKey,
		SecretKey:  req.SecretKey,
		Passphrase: req.Passphrase,
		Testnet:    req.Testnet,
		FeeRate:    req.FeeRate,
	}

	// 如果手续费率未設置，使用默认值
	if exchangeCfg.FeeRate <= 0 {
		exchangeCfg.FeeRate = 0.0002
	}

	cfg.Exchanges[req.Exchange] = exchangeCfg

	// 如果賣單視窗大小未設置，使用買單窗口大小
	sellWindowSize := req.SellWindowSize
	if sellWindowSize <= 0 {
		sellWindowSize = req.BuyWindowSize
	}

	// 設置交易配置（兼容舊版）
	cfg.Trading.PriceInterval = req.PriceInterval
	cfg.Trading.ProfitSpread = req.ProfitSpread
	cfg.Trading.OrderQuantity = req.OrderQuantity
	if req.MinOrderValue > 0 {
		cfg.Trading.MinOrderValue = req.MinOrderValue
	} else if cfg.Trading.MinOrderValue <= 0 {
		cfg.Trading.MinOrderValue = 20
	}
	cfg.Trading.BuyWindowSize = req.BuyWindowSize
	cfg.Trading.SellWindowSize = sellWindowSize

	// 保留其他交易所的交易對配置，僅更新當前交易所的
	newSymbolConfigs := make([]config.SymbolConfig, 0)
	for _, sc := range cfg.Trading.Symbols {
		if !strings.EqualFold(sc.Exchange, req.Exchange) {
			newSymbolConfigs = append(newSymbolConfigs, sc)
		}
	}

	// 為每個新交易對創建配置
	for _, symbol := range symbols {
		symbolCfg := config.SymbolConfig{
			Enabled:               config.BoolPtr(true),
			Exchange:              req.Exchange,
			Symbol:                symbol,
			PriceInterval:         req.PriceInterval,
			ProfitSpread:          req.ProfitSpread,
			OrderQuantity:         req.OrderQuantity,
			MinOrderValue:         cfg.Trading.MinOrderValue,
			BuyWindowSize:         req.BuyWindowSize,
			SellWindowSize:        sellWindowSize,
			ReconcileInterval:     60,
			OrderCleanupThreshold: 50,
			CleanupBatchSize:      10,
			MarginLockDurationSec: 10,
			PositionSafetyCheck:   100,
		}
		newSymbolConfigs = append(newSymbolConfigs, symbolCfg)
	}
	cfg.Trading.Symbols = newSymbolConfigs

	// 設置第一個交易對作為默认（向后兼容）
	if len(cfg.Trading.Symbols) > 0 {
		cfg.Trading.Symbol = cfg.Trading.Symbols[0].Symbol
	}

	if fileConfigManager == nil {
		c.JSON(http.StatusInternalServerError, SetupInitResponse{
			Success: false,
			Message: "配置管理器未初始化",
		})
		return
	}
	if err := fileConfigManager.UpdateConfig(cfg); err != nil {
		logger.Error("❌ 保存配置失败: %v", err)
		c.JSON(http.StatusInternalServerError, SetupInitResponse{
			Success: false,
			Message: "保存配置失败: " + err.Error(),
		})
		return
	}
	SetGlobalConfig(cfg)
	if configHotReloader != nil {
		_, _ = configHotReloader.UpdateConfig(cfg)
	}

	symbolsStr := ""
	if len(symbols) > 0 {
		symbolsStr = symbols[0]
		if len(symbols) > 1 {
			symbolsStr += fmt.Sprintf(" 等 %d 個", len(symbols))
		}
	}
	logger.Info("✅ 配置初始化成功: 交易所=%s, 交易對=%s", req.Exchange, symbolsStr)

	message := "配置已寫入主庫，请重啟系统以應用配置"

	c.JSON(http.StatusOK, SetupInitResponse{
		Success:         true,
		Message:         message,
		RequiresRestart: true,
	})
}

// ExchangeSymbolsRequest 獲取交易所交易對请求
type ExchangeSymbolsRequest struct {
	Exchange   string `json:"exchange" binding:"required"`
	MarketType string `json:"market_type,omitempty"` // spot 現貨 / futures 合約，預設 futures
	APIKey     string `json:"api_key" binding:"required"`
	SecretKey  string `json:"secret_key" binding:"required"`
	Passphrase string `json:"passphrase,omitempty"`
	Testnet    bool   `json:"testnet,omitempty"`
}

// ExchangeSymbolsResponse 交易所交易對响应
type ExchangeSymbolsResponse struct {
	Success bool     `json:"success"`
	Message string   `json:"message,omitempty"`
	Symbols []string `json:"symbols"`
}

// getExchangeSymbolsHandler 獲取交易所的所有交易對
// POST /api/setup/exchange-symbols
func getExchangeSymbolsHandler(c *gin.Context) {
	var req ExchangeSymbolsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ExchangeSymbolsResponse{
			Success: false,
			Message: "请求参數錯误: " + err.Error(),
			Symbols: []string{},
		})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	marketType := strings.ToLower(strings.TrimSpace(req.MarketType))
	if marketType == "" {
		marketType = "futures"
	}
	if marketType != "spot" && marketType != "futures" {
		marketType = "futures"
	}

	var symbols []string
	var err error

	switch strings.ToLower(req.Exchange) {
	case "binance":
		if marketType == "spot" {
			symbols, err = getBinanceSpotSymbols(ctx, req.Testnet)
		} else {
			symbols, err = getBinanceSymbols(ctx, req.APIKey, req.SecretKey, req.Testnet)
		}
	case "bitget":
		if marketType == "spot" {
			symbols, err = getBitgetSpotSymbols(ctx, req.Testnet)
		} else {
			symbols, err = getBitgetSymbols(ctx, req.APIKey, req.SecretKey, req.Passphrase, req.Testnet)
		}
	case "bybit":
		if marketType == "spot" {
			symbols, err = getBybitSpotSymbols(ctx, req.Testnet)
		} else {
			symbols, err = getBybitSymbols(ctx, req.APIKey, req.SecretKey, req.Testnet)
		}
	case "gate":
		if marketType == "spot" {
			symbols, err = getGateSpotSymbols(ctx, req.Testnet)
		} else {
			symbols, err = getGateSymbols(ctx, req.APIKey, req.SecretKey, req.Testnet)
		}
	case "okx":
		if marketType == "spot" {
			symbols, err = getOkxSpotSymbols(ctx, req.Testnet)
		} else {
			symbols, err = getOKXSymbols(ctx, req.APIKey, req.SecretKey, req.Passphrase, req.Testnet)
		}
	case "huobi", "htx":
		if marketType == "spot" {
			c.JSON(http.StatusBadRequest, ExchangeSymbolsResponse{
				Success: false,
				Message: "Huobi 現貨交易對列表暂未支援",
				Symbols: []string{},
			})
			return
		}
		symbols, err = getHuobiSymbols(ctx, req.APIKey, req.SecretKey, req.Testnet)
	case "kucoin":
		if marketType == "spot" {
			c.JSON(http.StatusBadRequest, ExchangeSymbolsResponse{
				Success: false,
				Message: "KuCoin 現貨交易對列表暂未支援",
				Symbols: []string{},
			})
			return
		}
		symbols, err = getKuCoinSymbols(ctx, req.APIKey, req.SecretKey, req.Passphrase, req.Testnet)
	default:
		c.JSON(http.StatusBadRequest, ExchangeSymbolsResponse{
			Success: false,
			Message: fmt.Sprintf("暫不支援從 %s 獲取交易對列表", req.Exchange),
			Symbols: []string{},
		})
		return
	}

	if err != nil {
		logger.Error("獲取 %s 交易對列表失败: %v", req.Exchange, err)
		// 如果獲取失败且没有回傳 fallback 交易對，则返回錯误
		if len(symbols) == 0 {
			msg := err.Error()
			if strings.Contains(msg, "context deadline exceeded") {
				msg = "獲取交易對列表超時，请检查网络连接或代理設置"
			}
			c.JSON(http.StatusInternalServerError, ExchangeSymbolsResponse{
				Success: false,
				Message: "獲取交易對列表失败: " + msg,
				Symbols: []string{},
			})
			return
		}
		// 如果雖然报錯了但有 fallback 交易對，则继续返回成功的响应，但在 message 中提示
		logger.Warn("⚠️ 使用内置备选交易對列表")
	}

	c.JSON(http.StatusOK, ExchangeSymbolsResponse{
		Success: true,
		Symbols: symbols,
	})
}

// binancePrioritySymbols 币安常用交易對（按优先级排序）
var binancePrioritySymbols = []string{
	"BTCUSDT", "ETHUSDT", "BNBUSDT", "SOLUSDT", "XRPUSDT",
	"ADAUSDT", "DOGEUSDT", "MATICUSDT", "DOTUSDT", "AVAXUSDT",
	"LINKUSDT", "UNIUSDT", "LTCUSDT", "ATOMUSDT", "ETCUSDT",
	"XLMUSDT", "ALGOUSDT", "VETUSDT", "ICPUSDT", "FILUSDT",
	"TRXUSDT", "EOSUSDT", "AAVEUSDT", "APTUSDT", "ARBUSDT",
	"OPUSDT", "SUIUSDT", "NEARUSDT", "INJUSDT", "TIAUSDT",
}

// getBinanceSymbols 獲取 Binance 的所有交易對
func getBinanceSymbols(ctx context.Context, apiKey, secretKey string, testnet bool) ([]string, error) {
	// 設置測試網模式
	futures.UseTestnet = testnet
	client := futures.NewClient(apiKey, secretKey)

	exchangeInfo, err := client.NewExchangeInfoService().Do(ctx)
	if err != nil {
		logger.Error("⚠️ 獲取 Binance 交易所信息失败: %v, 使用内置备选交易對", err)
		return binancePrioritySymbols, err
	}

	// 使用 map 来去重和快速查找
	symbolSet := make(map[string]bool)
	priorityList := make([]string, 0)
	otherList := make([]string, 0)

	for _, symbol := range exchangeInfo.Symbols {
		// 只回傳 USDT 永续合約（U本位永续合約），且状態為 TRADING
		if symbol.Status == "TRADING" &&
			symbol.ContractType == "PERPETUAL" &&
			symbol.QuoteAsset == "USDT" &&
			symbol.BaseAsset != "" {
			if !strings.Contains(symbol.Symbol, "USD_PERP") && !strings.Contains(symbol.Symbol, "USD-") {
				symbolStr := symbol.Symbol
				if !symbolSet[symbolStr] {
					symbolSet[symbolStr] = true
					// 检查是否在优先级列表中
					isPriority := false
					for _, ps := range binancePrioritySymbols {
						if ps == symbolStr {
							isPriority = true
							break
						}
					}
					if isPriority {
						priorityList = append(priorityList, symbolStr)
					} else {
						otherList = append(otherList, symbolStr)
					}
				}
			}
		}
	}

	// 對优先级列表按預定义顺序排序
	priorityMap := make(map[string]int)
	for i, ps := range binancePrioritySymbols {
		priorityMap[ps] = i
	}

	// 使用 sort.Slice 對优先级列表按預定义顺序排序
	sort.Slice(priorityList, func(i, j int) bool {
		idxI, existsI := priorityMap[priorityList[i]]
		idxJ, existsJ := priorityMap[priorityList[j]]
		if !existsI {
			return false
		}
		if !existsJ {
			return false
		}
		return idxI < idxJ
	})

	// 對其他列表按字母顺序排序
	sort.Strings(otherList)

	// 合並結果：优先级列表在前，其他列表在后
	result := make([]string, 0, len(priorityList)+len(otherList))
	result = append(result, priorityList...)
	result = append(result, otherList...)

	logger.Info("📊 [Binance] 獲取到 %d 個 USDT 永续合約交易對（其中 %d 個优先级交易對）", len(result), len(priorityList))
	return result, nil
}

// getBinanceSpotSymbols 獲取 Binance 現貨交易對（公开 API，無需鉴权）
func getBinanceSpotSymbols(ctx context.Context, testnet bool) ([]string, error) {
	baseURL := "https://api.binance.com/api/v3/exchangeInfo"
	if testnet {
		baseURL = "https://testnet.binance.vision/api/v3/exchangeInfo"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API 回傳 %d", resp.StatusCode)
	}
	var info struct {
		Symbols []struct {
			Symbol      string `json:"symbol"`
			Status      string `json:"status"`
			QuoteAsset  string `json:"quoteAsset"`
			BaseAsset   string `json:"baseAsset"`
		} `json:"symbols"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	symbolSet := make(map[string]bool)
	priorityList := make([]string, 0)
	otherList := make([]string, 0)
	// 支持的计价币种（USDT + United Stables "U" 等）
	supportedQuoteAssets := map[string]bool{"USDT": true, "U": true}
	for _, s := range info.Symbols {
		if s.Status == "TRADING" && supportedQuoteAssets[s.QuoteAsset] && s.BaseAsset != "" {
			if !symbolSet[s.Symbol] {
				symbolSet[s.Symbol] = true
				isPriority := false
				for _, ps := range binancePrioritySymbols {
					if ps == s.Symbol {
						isPriority = true
						break
					}
				}
				if isPriority {
					priorityList = append(priorityList, s.Symbol)
				} else {
					otherList = append(otherList, s.Symbol)
				}
			}
		}
	}
	priorityMap := make(map[string]int)
	for i, ps := range binancePrioritySymbols {
		priorityMap[ps] = i
	}
	sort.Slice(priorityList, func(i, j int) bool {
		idxI, okI := priorityMap[priorityList[i]]
		idxJ, okJ := priorityMap[priorityList[j]]
		if !okI || !okJ {
			return priorityList[i] < priorityList[j]
		}
		return idxI < idxJ
	})
	sort.Strings(otherList)
	result := make([]string, 0, len(priorityList)+len(otherList))
	result = append(result, priorityList...)
	result = append(result, otherList...)
	logger.Info("📊 [Binance Spot] 獲取到 %d 個現貨交易對（含 USDT/U 计价）", len(result))
	return result, nil
}

// getBitgetSymbols 獲取 Bitget 的所有交易對
func getBitgetSymbols(ctx context.Context, apiKey, secretKey, passphrase string, testnet bool) ([]string, error) {
	baseURL := "https://api.bitget.com"
	if testnet {
		baseURL = "https://testnetapi.bitget.com"
	}

	path := "/api/v2/mix/market/contracts?productType=usdt-futures"
	url := baseURL + path

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("創建请求失败: %w", err)
	}

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		logger.Error("⚠️ 请求 Bitget 失败: %v, 使用内置备选交易對", err)
		return []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "XRPUSDT", "DOGEUSDT"}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Warn("Bitget API 回傳 HTTP %d, 使用内置备选交易對", resp.StatusCode)
		return []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "XRPUSDT", "DOGEUSDT"}, nil
	}

	var result struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			Symbol string `json:"symbol"`
			State  string `json:"state"` // "online" 表示在線
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	if result.Code != "00000" {
		return nil, fmt.Errorf("API 錯误: %s", result.Msg)
	}

	symbols := make([]string, 0)
	for _, contract := range result.Data {
		if contract.State == "online" {
			symbols = append(symbols, contract.Symbol)
		}
	}

	return symbols, nil
}

// getBitgetSpotSymbols 獲取 Bitget 現貨交易對（公开 API）
func getBitgetSpotSymbols(ctx context.Context, testnet bool) ([]string, error) {
	baseURL := "https://api.bitget.com"
	if testnet {
		baseURL = "https://testnetapi.bitget.com"
	}
	path := "/api/v2/spot/public/symbols"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API 回傳 %d", resp.StatusCode)
	}
	var result struct {
		Code string `json:"code"`
		Data []struct {
			Symbol      string `json:"symbol"`
			QuoteCoin   string `json:"quoteCoin"`
			Status      string `json:"status"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if result.Code != "00000" {
		return nil, fmt.Errorf("API 錯误: %s", result.Code)
	}
	symbols := make([]string, 0)
	for _, s := range result.Data {
		if s.QuoteCoin == "USDT" && (s.Status == "online" || s.Status == "gray") {
			symbols = append(symbols, s.Symbol)
		}
	}
	sort.Strings(symbols)
	return symbols, nil
}

// getBybitSymbols 獲取 Bybit 的所有交易對
func getBybitSymbols(ctx context.Context, apiKey, secretKey string, testnet bool) ([]string, error) {
	baseURL := "https://api.bybit.com"
	if testnet {
		baseURL = "https://api-testnet.bybit.com"
	}

	path := "/v5/market/instruments-info?category=linear&limit=1000"
	url := baseURL + path

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("創建请求失败: %w", err)
	}

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		logger.Error("⚠️ 请求 Bybit 失败: %v, 使用内置备选交易對", err)
		return []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "XRPUSDT", "DOGEUSDT"}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Warn("Bybit API 回傳 HTTP %d, 使用内置备选交易對", resp.StatusCode)
		return []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "XRPUSDT", "DOGEUSDT"}, nil
	}

	var result struct {
		RetCode int    `json:"retCode"`
		RetMsg  string `json:"retMsg"`
		Result  struct {
			List []struct {
				Symbol     string `json:"symbol"`
				Status     string `json:"status"`
				QuoteCoin  string `json:"quoteCoin"`
			} `json:"list"`
		} `json:"result"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	if result.RetCode != 0 {
		return nil, fmt.Errorf("API 錯误: %s", result.RetMsg)
	}

	symbols := make([]string, 0)
	for _, item := range result.Result.List {
		if item.Status == "Trading" && item.QuoteCoin == "USDT" {
			symbols = append(symbols, item.Symbol)
		}
	}

	return symbols, nil
}

// getBybitSpotSymbols 獲取 Bybit 現貨交易對（公开 API）
func getBybitSpotSymbols(ctx context.Context, testnet bool) ([]string, error) {
	baseURL := "https://api.bybit.com"
	if testnet {
		baseURL = "https://api-testnet.bybit.com"
	}
	path := "/v5/market/instruments-info?category=spot&limit=1000"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API 回傳 %d", resp.StatusCode)
	}
	var result struct {
		RetCode int    `json:"retCode"`
		Result  struct {
			List []struct {
				Symbol    string `json:"symbol"`
				Status    string `json:"status"`
				QuoteCoin string `json:"quoteCoin"`
			} `json:"list"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if result.RetCode != 0 {
		return nil, fmt.Errorf("API retCode %d", result.RetCode)
	}
	symbols := make([]string, 0)
	for _, item := range result.Result.List {
		if item.Status == "Trading" && item.QuoteCoin == "USDT" {
			symbols = append(symbols, item.Symbol)
		}
	}
	sort.Strings(symbols)
	return symbols, nil
}

// getGateSymbols 獲取 Gate.io 的所有交易對
func getGateSymbols(ctx context.Context, apiKey, secretKey string, testnet bool) ([]string, error) {
	baseURL := "https://api.gateio.ws"
	if testnet {
		baseURL = "https://api-testnet.gateapi.io"
	}

	path := "/api/v4/futures/usdt/contracts"
	url := baseURL + path

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		logger.Error("獲取 Gate.io 交易對列表失败: %v, 使用内置备选交易對", err)
		// 如果 API 調用失败，返回常用的内置交易對作為备选（特别是在測試網环境下）
		return []string{
			"BTC_USDT", "ETH_USDT", "SOL_USDT", "DOGE_USDT", "XRP_USDT",
			"ADA_USDT", "DOT_USDT", "LTC_USDT", "LINK_USDT", "TRX_USDT",
			"PEPE_USDT", "SHIB_USDT", "ARB_USDT", "OP_USDT", "SUI_USDT",
		}, nil
	}

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		logger.Error("请求 Gate.io 失败: %v, 使用内置备选交易對", err)
		return []string{
			"BTC_USDT", "ETH_USDT", "SOL_USDT", "DOGE_USDT", "XRP_USDT",
			"ADA_USDT", "DOT_USDT", "LTC_USDT", "LINK_USDT", "TRX_USDT",
		}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Warn("Gate.io API 回傳 HTTP %d, 使用内置备选交易對", resp.StatusCode)
		return []string{
			"BTC_USDT", "ETH_USDT", "SOL_USDT", "DOGE_USDT", "XRP_USDT",
			"ADA_USDT", "DOT_USDT", "LTC_USDT", "LINK_USDT", "TRX_USDT",
		}, nil
	}

	var contracts []struct {
		Name   string `json:"name"`
		Status string `json:"status"` // "active" 表示活跃
	}

	if err := json.NewDecoder(resp.Body).Decode(&contracts); err != nil {
		logger.Error("解析 Gate.io 响应失败: %v, 使用内置备选交易對", err)
		return []string{
			"BTC_USDT", "ETH_USDT", "SOL_USDT", "DOGE_USDT", "XRP_USDT",
			"ADA_USDT", "DOT_USDT", "LTC_USDT", "LINK_USDT", "TRX_USDT",
		}, nil
	}

	symbols := make([]string, 0)
	for _, contract := range contracts {
		if contract.Status == "active" {
			symbols = append(symbols, contract.Name)
		}
	}

	// 如果 API 返回為空，则返回内置备选
	if len(symbols) == 0 {
		logger.Warn("Gate.io API 未返回任何活跃合約，使用内置备选交易對")
		return []string{
			"BTC_USDT", "ETH_USDT", "SOL_USDT", "DOGE_USDT", "XRP_USDT",
			"ADA_USDT", "DOT_USDT", "LTC_USDT", "LINK_USDT", "TRX_USDT",
		}, nil
	}

	return symbols, nil
}

// getGateSpotSymbols 獲取 Gate.io 現貨交易對（公开 API）
func getGateSpotSymbols(ctx context.Context, testnet bool) ([]string, error) {
	baseURL := "https://api.gateio.ws"
	if testnet {
		baseURL = "https://api-testnet.gateapi.io"
	}
	path := "/api/v4/spot/currency_pairs"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API 回傳 %d", resp.StatusCode)
	}
	var pairs []struct {
		ID        string `json:"id"`
		Quote     string `json:"quote"`
		Tradeable bool   `json:"tradeable"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pairs); err != nil {
		return nil, err
	}
	symbols := make([]string, 0)
	for _, p := range pairs {
		if p.Quote == "usdt" && p.Tradeable {
			// Gate 現貨 id 格式如 "btc_usdt"，轉為 BTCUSDT
			symbols = append(symbols, strings.ToUpper(strings.ReplaceAll(p.ID, "_", "")))
		}
	}
	sort.Strings(symbols)
	return symbols, nil
}

// getOKXSymbols 獲取 OKX 的所有交易對
func getOKXSymbols(ctx context.Context, apiKey, secretKey, passphrase string, testnet bool) ([]string, error) {
	baseURL := "https://www.okx.com"
	if testnet {
		baseURL = "https://www.okx.com"
	}

	path := "/api/v5/public/instruments?instType=SWAP"
	url := baseURL + path

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("創建请求失败: %w", err)
	}

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		logger.Error("⚠️ 请求 OKX 失败: %v, 使用内置备选交易對", err)
		return []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "XRPUSDT", "DOGEUSDT"}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Warn("OKX API 回傳 HTTP %d, 使用内置备选交易對", resp.StatusCode)
		return []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "XRPUSDT", "DOGEUSDT"}, nil
	}

	var result struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			InstID string `json:"instId"`
			State  string `json:"state"` // "live" 表示在線
			CtType string `json:"ctType"` // "linear" 表示線性合約
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	if result.Code != "0" {
		return nil, fmt.Errorf("API 錯误: %s", result.Msg)
	}

	symbols := make([]string, 0)
	for _, item := range result.Data {
		if item.State == "live" && item.CtType == "linear" && strings.HasSuffix(item.InstID, "-USDT-SWAP") {
			// 轉换格式：BTC-USDT-SWAP -> BTCUSDT
			symbol := strings.ReplaceAll(item.InstID, "-USDT-SWAP", "USDT")
			symbols = append(symbols, symbol)
		}
	}

	return symbols, nil
}

// getOkxSpotSymbols 獲取 OKX 現貨交易對（公开 API）
func getOkxSpotSymbols(ctx context.Context, testnet bool) ([]string, error) {
	baseURL := "https://www.okx.com"
	if testnet {
		baseURL = "https://www.okx.com"
	}
	path := "/api/v5/public/instruments?instType=SPOT"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API 回傳 %d", resp.StatusCode)
	}
	var result struct {
		Code string `json:"code"`
		Data []struct {
			InstID string `json:"instId"`
			State  string `json:"state"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if result.Code != "0" {
		return nil, fmt.Errorf("API 錯误: %s", result.Code)
	}
	symbols := make([]string, 0)
	for _, item := range result.Data {
		if item.State == "live" && strings.HasSuffix(item.InstID, "-USDT") {
			symbol := strings.ReplaceAll(item.InstID, "-USDT", "USDT")
			symbols = append(symbols, symbol)
		}
	}
	sort.Strings(symbols)
	return symbols, nil
}

// getHuobiSymbols 獲取 Huobi 的所有交易對
func getHuobiSymbols(ctx context.Context, apiKey, secretKey string, testnet bool) ([]string, error) {
	baseURL := "https://api.hbdm.com"
	if testnet {
		baseURL = "https://api.hbdm.vn"
	}

	path := "/linear-swap-api/v1/swap_contract_info"
	url := baseURL + path

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("創建请求失败: %w", err)
	}

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		logger.Error("⚠️ 请求 Huobi 失败: %v, 使用内置备选交易對", err)
		return []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "XRPUSDT", "DOGEUSDT"}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Warn("Huobi API 回傳 HTTP %d, 使用内置备选交易對", resp.StatusCode)
		return []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "XRPUSDT", "DOGEUSDT"}, nil
	}

	var result struct {
		Status string `json:"status"`
		Data   []struct {
			Symbol    string `json:"symbol"`
			ContractStatus int `json:"contract_status"` // 1 表示正常交易
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	if result.Status != "ok" {
		return nil, fmt.Errorf("API 錯误: %s", result.Status)
	}

	symbols := make([]string, 0)
	for _, item := range result.Data {
		if item.ContractStatus == 1 {
			symbols = append(symbols, item.Symbol)
		}
	}

	return symbols, nil
}

// getKuCoinSymbols 獲取 KuCoin 的所有交易對
func getKuCoinSymbols(ctx context.Context, apiKey, secretKey, passphrase string, testnet bool) ([]string, error) {
	baseURL := "https://api-futures.kucoin.com"
	if testnet {
		baseURL = "https://api-sandbox-futures.kucoin.com"
	}

	path := "/api/v1/contracts/active"
	url := baseURL + path

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("創建请求失败: %w", err)
	}

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		logger.Error("⚠️ 请求 KuCoin 失败: %v, 使用内置备选交易對", err)
		return []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "XRPUSDT", "DOGEUSDT"}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Warn("KuCoin API 回傳 HTTP %d, 使用内置备选交易對", resp.StatusCode)
		return []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "XRPUSDT", "DOGEUSDT"}, nil
	}

	var result struct {
		Code string `json:"code"`
		Data []struct {
			Symbol string `json:"symbol"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	if result.Code != "200000" {
		return nil, fmt.Errorf("API 錯误: %s", result.Code)
	}

	symbols := make([]string, 0)
	for _, item := range result.Data {
		symbols = append(symbols, item.Symbol)
	}

	return symbols, nil
}
