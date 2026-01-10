package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/adshao/go-binance/v2/futures"
	"quantmesh/config"
	"quantmesh/logger"
)

// SetupStatusResponse 配置状态响应
type SetupStatusResponse struct {
	NeedsSetup bool   `json:"needs_setup"`
	ConfigPath string `json:"config_path"`
}

// getSetupStatusHandler 获取配置状态
// GET /api/setup/status
func getSetupStatusHandler(c *gin.Context) {
	configPath := "config.yaml"
	if configManager != nil {
		configPath = configManager.GetConfigPath()
	}

	// 检查配置文件是否存在
	_, err := os.Stat(configPath)
	if os.IsNotExist(err) {
		c.JSON(http.StatusOK, SetupStatusResponse{
			NeedsSetup: true,
			ConfigPath: configPath,
		})
		return
	}

	// 检查配置是否完整
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		c.JSON(http.StatusOK, SetupStatusResponse{
			NeedsSetup: true,
			ConfigPath: configPath,
		})
		return
	}

	// 检查配置是否完整
	needsSetup := cfg.App.CurrentExchange == "" ||
		len(cfg.Exchanges) == 0 ||
		cfg.Exchanges[cfg.App.CurrentExchange].APIKey == "" ||
		cfg.Exchanges[cfg.App.CurrentExchange].SecretKey == "" ||
		len(cfg.Trading.Symbols) == 0 ||
		cfg.Trading.Symbols[0].Symbol == ""

	c.JSON(http.StatusOK, SetupStatusResponse{
		NeedsSetup: needsSetup,
		ConfigPath: configPath,
	})
}

// SetupInitRequest 配置初始化请求
type SetupInitRequest struct {
	Exchange       string   `json:"exchange" binding:"required"`
	APIKey         string   `json:"api_key" binding:"required"`
	SecretKey      string   `json:"secret_key" binding:"required"`
	Passphrase     string   `json:"passphrase,omitempty"`
	Symbol         string   `json:"symbol,omitempty"`        // 向后兼容，但优先使用 Symbols
	Symbols        []string `json:"symbols,omitempty"`       // 多交易对支持
	PriceInterval  float64  `json:"price_interval" binding:"required,gt=0"`
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

// initSetupHandler 初始化配置
// POST /api/setup/init
func initSetupHandler(c *gin.Context) {
	var req SetupInitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, SetupInitResponse{
			Success: false,
			Message: "请求参数错误: " + err.Error(),
		})
		return
	}

	// 确定要使用的交易对列表
	var symbols []string
	if len(req.Symbols) > 0 {
		// 优先使用 Symbols 数组
		symbols = req.Symbols
	} else if req.Symbol != "" {
		// 向后兼容：使用单个 Symbol
		symbols = []string{req.Symbol}
	} else {
		c.JSON(http.StatusBadRequest, SetupInitResponse{
			Success: false,
			Message: "请至少指定一个交易对（使用 symbol 或 symbols 字段）",
		})
		return
	}

	// 如果卖单窗口大小未设置，使用买单窗口大小
	sellWindowSize := req.SellWindowSize
	if sellWindowSize <= 0 {
		sellWindowSize = req.BuyWindowSize
	}

	// 创建最小化配置作为基础
	cfg := config.CreateMinimalConfig()

	// 设置交易所
	cfg.App.CurrentExchange = req.Exchange

	// 设置交易所配置
	exchangeCfg := config.ExchangeConfig{
		APIKey:     req.APIKey,
		SecretKey:  req.SecretKey,
		Passphrase: req.Passphrase,
		Testnet:    req.Testnet,
		FeeRate:    req.FeeRate,
	}

	// 如果手续费率未设置，使用默认值
	if exchangeCfg.FeeRate <= 0 {
		exchangeCfg.FeeRate = 0.0002
	}

	cfg.Exchanges[req.Exchange] = exchangeCfg

	// 设置交易配置（兼容旧版）
	cfg.Trading.PriceInterval = req.PriceInterval
	cfg.Trading.OrderQuantity = req.OrderQuantity
	if req.MinOrderValue > 0 {
		cfg.Trading.MinOrderValue = req.MinOrderValue
	} else {
		cfg.Trading.MinOrderValue = 20
	}
	cfg.Trading.BuyWindowSize = req.BuyWindowSize
	cfg.Trading.SellWindowSize = sellWindowSize

	// 为每个交易对创建配置
	cfg.Trading.Symbols = make([]config.SymbolConfig, 0, len(symbols))
	for _, symbol := range symbols {
		symbolCfg := config.SymbolConfig{
			Exchange:              req.Exchange,
			Symbol:                symbol,
			PriceInterval:         req.PriceInterval,
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
		cfg.Trading.Symbols = append(cfg.Trading.Symbols, symbolCfg)
	}

	// 设置第一个交易对作为默认（向后兼容）
	if len(symbols) > 0 {
		cfg.Trading.Symbol = symbols[0]
	}

	// 获取配置文件路径
	configPath := "config.yaml"
	if configManager != nil {
		configPath = configManager.GetConfigPath()
	}

	// 检查配置文件是否已存在，如果存在则先备份
	var backupPath string
	_, err := os.Stat(configPath)
	if err == nil {
		// 配置文件存在，先创建备份
		backupManager := config.NewBackupManager()
		backupInfo, backupErr := backupManager.CreateBackup(configPath, "首次设置向导覆盖前自动备份")
		if backupErr != nil {
			logger.Warn("⚠️ 创建配置备份失败: %v，但继续保存配置", backupErr)
		} else {
			backupPath = backupInfo.FilePath
			logger.Info("✅ 已创建配置备份: %s", backupPath)
		}

		// 检查配置是否完整（用于日志记录，但不阻止覆盖）
		existingCfg, loadErr := config.LoadConfig(configPath)
		if loadErr == nil {
			isComplete := existingCfg.App.CurrentExchange != "" &&
				len(existingCfg.Exchanges) > 0 &&
				existingCfg.Exchanges[existingCfg.App.CurrentExchange].APIKey != "" &&
				existingCfg.Exchanges[existingCfg.App.CurrentExchange].SecretKey != "" &&
				len(existingCfg.Trading.Symbols) > 0 &&
				existingCfg.Trading.Symbols[0].Symbol != ""

			if isComplete {
				logger.Info("ℹ️ 检测到完整配置，已备份到: %s", backupPath)
			}
		}
	}

	// 保存配置
	if err := config.SaveConfig(cfg, configPath); err != nil {
		logger.Error("❌ 保存配置失败: %v", err)
		c.JSON(http.StatusInternalServerError, SetupInitResponse{
			Success: false,
			Message: "保存配置失败: " + err.Error(),
		})
		return
	}

	// 更新配置管理器中的配置
	if configManager != nil {
		configManager.mu.Lock()
		configManager.currentConfig = cfg
		configManager.mu.Unlock()
	}

	symbolsStr := ""
	if len(symbols) > 0 {
		symbolsStr = symbols[0]
		if len(symbols) > 1 {
			symbolsStr += fmt.Sprintf(" 等 %d 个", len(symbols))
		}
	}
	logger.Info("✅ 配置初始化成功: 交易所=%s, 交易对=%s", req.Exchange, symbolsStr)

	message := "配置已保存，请重启系统以应用配置"
	if backupPath != "" {
		message = fmt.Sprintf("配置已保存（原配置已备份到: %s），请重启系统以应用配置", backupPath)
	}

	c.JSON(http.StatusOK, SetupInitResponse{
		Success:         true,
		Message:         message,
		RequiresRestart: true,
		BackupPath:      backupPath,
	})
}

// ExchangeSymbolsRequest 获取交易所交易对请求
type ExchangeSymbolsRequest struct {
	Exchange   string `json:"exchange" binding:"required"`
	APIKey     string `json:"api_key" binding:"required"`
	SecretKey  string `json:"secret_key" binding:"required"`
	Passphrase string `json:"passphrase,omitempty"`
	Testnet    bool   `json:"testnet,omitempty"`
}

// ExchangeSymbolsResponse 交易所交易对响应
type ExchangeSymbolsResponse struct {
	Success bool     `json:"success"`
	Message string   `json:"message,omitempty"`
	Symbols []string `json:"symbols"`
}

// getExchangeSymbolsHandler 获取交易所的所有交易对
// POST /api/setup/exchange-symbols
func getExchangeSymbolsHandler(c *gin.Context) {
	var req ExchangeSymbolsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ExchangeSymbolsResponse{
			Success: false,
			Message: "请求参数错误: " + err.Error(),
			Symbols: []string{},
		})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var symbols []string
	var err error

	switch strings.ToLower(req.Exchange) {
	case "binance":
		symbols, err = getBinanceSymbols(ctx, req.APIKey, req.SecretKey, req.Testnet)
	case "bitget":
		symbols, err = getBitgetSymbols(ctx, req.APIKey, req.SecretKey, req.Passphrase, req.Testnet)
	case "bybit":
		symbols, err = getBybitSymbols(ctx, req.APIKey, req.SecretKey, req.Testnet)
	case "gate":
		symbols, err = getGateSymbols(ctx, req.APIKey, req.SecretKey, req.Testnet)
	case "okx":
		symbols, err = getOKXSymbols(ctx, req.APIKey, req.SecretKey, req.Passphrase, req.Testnet)
	case "huobi", "htx":
		symbols, err = getHuobiSymbols(ctx, req.APIKey, req.SecretKey, req.Testnet)
	case "kucoin":
		symbols, err = getKuCoinSymbols(ctx, req.APIKey, req.SecretKey, req.Passphrase, req.Testnet)
	default:
		c.JSON(http.StatusBadRequest, ExchangeSymbolsResponse{
			Success: false,
			Message: fmt.Sprintf("暂不支持从 %s 获取交易对列表", req.Exchange),
			Symbols: []string{},
		})
		return
	}

	if err != nil {
		logger.Error("获取 %s 交易对列表失败: %v", req.Exchange, err)
		c.JSON(http.StatusInternalServerError, ExchangeSymbolsResponse{
			Success: false,
			Message: "获取交易对列表失败: " + err.Error(),
			Symbols: []string{},
		})
		return
	}

	c.JSON(http.StatusOK, ExchangeSymbolsResponse{
		Success: true,
		Symbols: symbols,
	})
}

// getBinanceSymbols 获取 Binance 的所有交易对
func getBinanceSymbols(ctx context.Context, apiKey, secretKey string, testnet bool) ([]string, error) {
	// 设置测试网模式
	futures.UseTestnet = testnet
	client := futures.NewClient(apiKey, secretKey)

	exchangeInfo, err := client.NewExchangeInfoService().Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取交易所信息失败: %w", err)
	}

	// 重要交易对列表（按优先级排序）
	prioritySymbols := []string{
		"BTCUSDT",  // 比特币
		"ETHUSDT",  // 以太坊
		"BNBUSDT",  // 币安币
		"SOLUSDT",  // Solana
		"XRPUSDT",  // Ripple
		"ADAUSDT",  // Cardano
		"DOGEUSDT", // Dogecoin
		"MATICUSDT", // Polygon
		"DOTUSDT",  // Polkadot
		"AVAXUSDT", // Avalanche
		"LINKUSDT", // Chainlink
		"UNIUSDT",  // Uniswap
		"LTCUSDT",  // Litecoin
		"ATOMUSDT", // Cosmos
		"ETCUSDT",  // Ethereum Classic
		"XLMUSDT",  // Stellar
		"ALGOUSDT", // Algorand
		"VETUSDT",  // VeChain
		"ICPUSDT",  // Internet Computer
		"FILUSDT",  // Filecoin
		"TRXUSDT",  // Tron
		"EOSUSDT",  // EOS
		"AAVEUSDT", // Aave
		"APTUSDT",  // Aptos
		"ARBUSDT",  // Arbitrum
		"OPUSDT",   // Optimism
		"SUIUSDT",  // Sui
		"NEARUSDT", // NEAR Protocol
		"INJUSDT",  // Injective
		"TIAUSDT",  // Celestia
	}

	// 使用 map 来去重和快速查找
	symbolSet := make(map[string]bool)
	priorityList := make([]string, 0)
	otherList := make([]string, 0)

	for _, symbol := range exchangeInfo.Symbols {
		// 只返回 USDT 永续合约（U本位永续合约），且状态为 TRADING
		// 过滤条件：
		// 1. Status == "TRADING" - 正在交易中
		// 2. ContractType == "PERPETUAL" - 永续合约
		// 3. QuoteAsset == "USDT" - USDT 计价（U本位）
		// 4. BaseAsset != "" - 确保有基础资产
		// 5. 排除币本位合约（QuoteAsset 不是 USDT 的）
		if symbol.Status == "TRADING" &&
			symbol.ContractType == "PERPETUAL" &&
			symbol.QuoteAsset == "USDT" &&
			symbol.BaseAsset != "" {
			// 额外检查：确保不是币本位合约（如 BTCUSD_PERP）
			// USDT 永续合约的符号格式通常是：BTCUSDT, ETHUSDT 等
			// 币本位合约通常是：BTCUSD_PERP, ETHUSD_PERP 等
			if !strings.Contains(symbol.Symbol, "USD_PERP") && !strings.Contains(symbol.Symbol, "USD-") {
				symbolStr := symbol.Symbol
				if !symbolSet[symbolStr] {
					symbolSet[symbolStr] = true
					// 检查是否在优先级列表中
					isPriority := false
					for _, ps := range prioritySymbols {
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

	// 对优先级列表按预定义顺序排序
	priorityMap := make(map[string]int)
	for i, ps := range prioritySymbols {
		priorityMap[ps] = i
	}

	// 使用 sort.Slice 对优先级列表按预定义顺序排序
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

	// 对其他列表按字母顺序排序
	sort.Strings(otherList)

	// 合并结果：优先级列表在前，其他列表在后
	result := make([]string, 0, len(priorityList)+len(otherList))
	result = append(result, priorityList...)
	result = append(result, otherList...)

	logger.Info("📊 [Binance] 获取到 %d 个 USDT 永续合约交易对（其中 %d 个优先级交易对）", len(result), len(priorityList))
	return result, nil
}

// getBitgetSymbols 获取 Bitget 的所有交易对
func getBitgetSymbols(ctx context.Context, apiKey, secretKey, passphrase string, testnet bool) ([]string, error) {
	baseURL := "https://api.bitget.com"
	if testnet {
		baseURL = "https://testapi.bitget.com"
	}

	path := "/api/v2/mix/market/contracts?productType=usdt-futures"
	url := baseURL + path

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var result struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			Symbol string `json:"symbol"`
			State  string `json:"state"` // "online" 表示在线
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	if result.Code != "00000" {
		return nil, fmt.Errorf("API 错误: %s", result.Msg)
	}

	symbols := make([]string, 0)
	for _, contract := range result.Data {
		if contract.State == "online" {
			symbols = append(symbols, contract.Symbol)
		}
	}

	return symbols, nil
}

// getBybitSymbols 获取 Bybit 的所有交易对
func getBybitSymbols(ctx context.Context, apiKey, secretKey string, testnet bool) ([]string, error) {
	baseURL := "https://api.bybit.com"
	if testnet {
		baseURL = "https://api-testnet.bybit.com"
	}

	path := "/v5/market/instruments-info?category=linear&limit=1000"
	url := baseURL + path

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
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
		return nil, fmt.Errorf("API 错误: %s", result.RetMsg)
	}

	symbols := make([]string, 0)
	for _, item := range result.Result.List {
		if item.Status == "Trading" && item.QuoteCoin == "USDT" {
			symbols = append(symbols, item.Symbol)
		}
	}

	return symbols, nil
}

// getGateSymbols 获取 Gate.io 的所有交易对
func getGateSymbols(ctx context.Context, apiKey, secretKey string, testnet bool) ([]string, error) {
	baseURL := "https://api.gateio.ws"
	if testnet {
		baseURL = "https://fx-api-testnet.gateio.ws"
	}

	path := "/api/v4/futures/usdt/contracts"
	url := baseURL + path

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var contracts []struct {
		Name   string `json:"name"`
		Status string `json:"status"` // "active" 表示活跃
	}

	if err := json.NewDecoder(resp.Body).Decode(&contracts); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	symbols := make([]string, 0)
	for _, contract := range contracts {
		if contract.Status == "active" {
			symbols = append(symbols, contract.Name)
		}
	}

	return symbols, nil
}

// getOKXSymbols 获取 OKX 的所有交易对
func getOKXSymbols(ctx context.Context, apiKey, secretKey, passphrase string, testnet bool) ([]string, error) {
	baseURL := "https://www.okx.com"
	if testnet {
		baseURL = "https://www.okx.com"
	}

	path := "/api/v5/public/instruments?instType=SWAP"
	url := baseURL + path

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var result struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			InstID string `json:"instId"`
			State  string `json:"state"` // "live" 表示在线
			CtType string `json:"ctType"` // "linear" 表示线性合约
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	if result.Code != "0" {
		return nil, fmt.Errorf("API 错误: %s", result.Msg)
	}

	symbols := make([]string, 0)
	for _, item := range result.Data {
		if item.State == "live" && item.CtType == "linear" && strings.HasSuffix(item.InstID, "-USDT-SWAP") {
			// 转换格式：BTC-USDT-SWAP -> BTCUSDT
			symbol := strings.ReplaceAll(item.InstID, "-USDT-SWAP", "USDT")
			symbols = append(symbols, symbol)
		}
	}

	return symbols, nil
}

// getHuobiSymbols 获取 Huobi 的所有交易对
func getHuobiSymbols(ctx context.Context, apiKey, secretKey string, testnet bool) ([]string, error) {
	baseURL := "https://api.hbdm.com"
	if testnet {
		baseURL = "https://api.hbdm.vn"
	}

	path := "/linear-swap-api/v1/swap_contract_info"
	url := baseURL + path

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
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
		return nil, fmt.Errorf("API 错误: %s", result.Status)
	}

	symbols := make([]string, 0)
	for _, item := range result.Data {
		if item.ContractStatus == 1 {
			symbols = append(symbols, item.Symbol)
		}
	}

	return symbols, nil
}

// getKuCoinSymbols 获取 KuCoin 的所有交易对
func getKuCoinSymbols(ctx context.Context, apiKey, secretKey, passphrase string, testnet bool) ([]string, error) {
	baseURL := "https://api-futures.kucoin.com"
	if testnet {
		baseURL = "https://api-sandbox-futures.kucoin.com"
	}

	path := "/api/v1/contracts/active"
	url := baseURL + path

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
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
		return nil, fmt.Errorf("API 错误: %s", result.Code)
	}

	symbols := make([]string, 0)
	for _, item := range result.Data {
		symbols = append(symbols, item.Symbol)
	}

	return symbols, nil
}
