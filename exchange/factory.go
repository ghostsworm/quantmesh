package exchange

import (
	"fmt"
	"quantmesh/config"
	"quantmesh/exchange/ascendex"
	"quantmesh/exchange/binance"
	"quantmesh/exchange/bingx"
	"quantmesh/exchange/bitfinex"
	"quantmesh/exchange/bitget"
	"quantmesh/exchange/bitmex"
	"quantmesh/exchange/bitrue"
	"quantmesh/exchange/btcc"
	"quantmesh/exchange/bybit"
	"quantmesh/exchange/coinex"
	"quantmesh/exchange/cryptocom"
	"quantmesh/exchange/deribit"
	"quantmesh/exchange/gate"
	"quantmesh/exchange/huobi"
	"quantmesh/exchange/kraken"
	"quantmesh/exchange/kucoin"
	"quantmesh/exchange/mexc"
	"quantmesh/exchange/okx"
	"quantmesh/exchange/phemex"
	"quantmesh/exchange/poloniex"
	"quantmesh/utils"
	"quantmesh/exchange/whitebit"
	"quantmesh/exchange/woox"
	"quantmesh/exchange/xtcom"
)

// NewExchange 創建交易所實例
// exchangeName/symbol 允許覆盖配置中的當前交易所和交易對，便於多交易對场景
// marketType: "spot" 現貨 / "futures" 合約，空時默认為 "futures"
// 如果配置了 system.dry_run = true，會自动包装為 DryRunWrapper
func NewExchange(cfg *config.Config, exchangeName, symbol, marketType string) (IExchange, error) {
	if marketType == "" {
		marketType = "futures"
	}
	// 創建真實的交易所實例
	ex, err := newExchangeInternal(cfg, exchangeName, symbol, marketType)
	if err != nil {
		return nil, err
	}

	// 追踪交易所使用情况（异步，不阻塞）
	// 注意：Version 需要从 main 包导入，这里先使用空字符串，实际版本号会在发送时从 main.Version 获取
	go func() {
		// 使用空字符串作为版本号占位符，实际版本号会在 telemetry 函数中从环境变量或配置获取
		utils.TrackExchangeUsage("", exchangeName, symbol)
	}()

	// 如果啟用了 DryRun 模式，包装交易所
	if cfg.System.DryRun {
		return NewDryRunWrapper(ex), nil
	}

	return ex, nil
}

// newExchangeInternal 内部函數：創建真實的交易所實例
func newExchangeInternal(cfg *config.Config, exchangeName, symbol, marketType string) (IExchange, error) {
	if exchangeName == "" {
		exchangeName = cfg.App.CurrentExchange
	}
	if symbol == "" {
		symbol = cfg.Trading.Symbol
	}
	if marketType == "" {
		marketType = "futures"
	}
	supportedSpotExchanges := map[string]bool{
		"binance": true, "bitget": true, "gate": true, "okx": true, "bybit": true,
	}
	if marketType == "spot" && !supportedSpotExchanges[exchangeName] {
		return nil, fmt.Errorf("交易所 %s 不支援現貨交易，请使用 market_type: futures 或选擇已支援現貨的交易所（Binance/OKX/Bybit/Bitget/Gate）", exchangeName)
	}

	switch exchangeName {
	case "bitget":
		exchangeCfg, exists := cfg.Exchanges["bitget"]
		if !exists {
			return nil, fmt.Errorf("bitget 配置不存在")
		}
		cfgMap := map[string]string{
			"api_key":    exchangeCfg.APIKey,
			"secret_key": exchangeCfg.SecretKey,
			"passphrase": exchangeCfg.Passphrase,
			"testnet":    fmt.Sprintf("%v", exchangeCfg.Testnet),
		}
		if marketType == "spot" {
			adapter, err := bitget.NewBitgetSpotAdapter(cfgMap, symbol)
			if err != nil {
				return nil, err
			}
			return &bitgetSpotWrapper{adapter: adapter}, nil
		}
		adapter, err := bitget.NewBitgetAdapter(cfgMap, symbol)
		if err != nil {
			return nil, err
		}
		return &bitgetWrapper{adapter: adapter}, nil

	case "binance":
		exchangeCfg, exists := cfg.Exchanges["binance"]
		if !exists {
			return nil, fmt.Errorf("binance 配置不存在")
		}
		cfgMap := map[string]string{
			"api_key":    exchangeCfg.APIKey,
			"secret_key": exchangeCfg.SecretKey,
			"testnet":    fmt.Sprintf("%v", exchangeCfg.Testnet), // 傳遞測試網配置
		}
		if marketType == "spot" {
			adapter, err := binance.NewBinanceSpotAdapter(cfgMap, symbol)
			if err != nil {
				return nil, err
			}
			return &binanceSpotWrapper{adapter: adapter}, nil
		}
		adapter, err := binance.NewBinanceAdapter(cfgMap, symbol)
		if err != nil {
			return nil, err
		}
		return &binanceWrapper{adapter: adapter}, nil

	case "gate":
		exchangeCfg, exists := cfg.Exchanges["gate"]
		if !exists {
			return nil, fmt.Errorf("gate 配置不存在")
		}
		cfgMap := map[string]string{
			"api_key":    exchangeCfg.APIKey,
			"secret_key": exchangeCfg.SecretKey,
			"settle":     "usdt",
			"testnet":    fmt.Sprintf("%v", exchangeCfg.Testnet),
		}
		if exchangeCfg.Leverage > 0 {
			cfgMap["leverage"] = fmt.Sprintf("%d", exchangeCfg.Leverage)
		}
		if marketType == "spot" {
			adapter, err := gate.NewGateSpotAdapter(cfgMap, symbol)
			if err != nil {
				return nil, err
			}
			return &gateSpotWrapper{adapter: adapter}, nil
		}
		adapter, err := gate.NewGateAdapter(cfgMap, symbol)
		if err != nil {
			return nil, err
		}
		return &gateWrapper{adapter: adapter}, nil

	case "okx":
		exchangeCfg, exists := cfg.Exchanges["okx"]
		if !exists {
			return nil, fmt.Errorf("okx 配置不存在")
		}
		cfgMap := map[string]string{
			"api_key":    exchangeCfg.APIKey,
			"secret_key": exchangeCfg.SecretKey,
			"passphrase": exchangeCfg.Passphrase,
			"testnet":    fmt.Sprintf("%v", exchangeCfg.Testnet),
		}
		if marketType == "spot" {
			adapter, err := okx.NewOKXSpotAdapter(cfgMap, symbol)
			if err != nil {
				return nil, err
			}
			return &okxSpotWrapper{adapter: adapter}, nil
		}
		adapter, err := okx.NewOKXAdapter(cfgMap, symbol)
		if err != nil {
			return nil, err
		}
		return &okxWrapper{adapter: adapter}, nil

	case "bybit":
		exchangeCfg, exists := cfg.Exchanges["bybit"]
		if !exists {
			return nil, fmt.Errorf("bybit 配置不存在")
		}
		cfgMap := map[string]string{
			"api_key":    exchangeCfg.APIKey,
			"secret_key": exchangeCfg.SecretKey,
			"testnet":    fmt.Sprintf("%v", exchangeCfg.Testnet),
		}
		if marketType == "spot" {
			adapter, err := bybit.NewBybitSpotAdapter(cfgMap, symbol)
			if err != nil {
				return nil, err
			}
			return &bybitSpotWrapper{adapter: adapter}, nil
		}
		adapter, err := bybit.NewBybitAdapter(cfgMap, symbol)
		if err != nil {
			return nil, err
		}
		return &bybitWrapper{adapter: adapter}, nil

	case "huobi":
		exchangeCfg, exists := cfg.Exchanges["huobi"]
		if !exists {
			return nil, fmt.Errorf("huobi 配置不存在")
		}
		cfgMap := map[string]string{
			"api_key":    exchangeCfg.APIKey,
			"secret_key": exchangeCfg.SecretKey,
		}
		adapter, err := huobi.NewHuobiAdapter(cfgMap, symbol)
		if err != nil {
			return nil, err
		}
		return &huobiWrapper{adapter: adapter}, nil

	case "kucoin":
		exchangeCfg, exists := cfg.Exchanges["kucoin"]
		if !exists {
			return nil, fmt.Errorf("kucoin 配置不存在")
		}
		cfgMap := map[string]string{
			"api_key":    exchangeCfg.APIKey,
			"secret_key": exchangeCfg.SecretKey,
			"passphrase": exchangeCfg.Passphrase,
		}
		adapter, err := kucoin.NewKuCoinAdapter(cfgMap, symbol)
		if err != nil {
			return nil, err
		}
		return &kucoinWrapper{adapter: adapter}, nil

	case "kraken":
		exchangeCfg, exists := cfg.Exchanges["kraken"]
		if !exists {
			return nil, fmt.Errorf("kraken 配置不存在")
		}
		cfgMap := map[string]string{
			"api_key":    exchangeCfg.APIKey,
			"secret_key": exchangeCfg.SecretKey,
		}
		adapter, err := kraken.NewKrakenAdapter(cfgMap, symbol)
		if err != nil {
			return nil, err
		}
		return &krakenWrapper{adapter: adapter}, nil

	case "bitfinex":
		exchangeCfg, exists := cfg.Exchanges["bitfinex"]
		if !exists {
			return nil, fmt.Errorf("bitfinex 配置不存在")
		}
		cfgMap := map[string]string{
			"api_key":    exchangeCfg.APIKey,
			"secret_key": exchangeCfg.SecretKey,
		}
		adapter, err := bitfinex.NewBitfinexAdapter(cfgMap, symbol)
		if err != nil {
			return nil, err
		}
		return &bitfinexWrapper{adapter: adapter}, nil

	case "mexc":
		exchangeCfg, exists := cfg.Exchanges["mexc"]
		if !exists {
			return nil, fmt.Errorf("mexc 配置不存在")
		}
		cfgMap := map[string]string{
			"api_key":    exchangeCfg.APIKey,
			"secret_key": exchangeCfg.SecretKey,
			"testnet":    fmt.Sprintf("%v", exchangeCfg.Testnet),
		}
		adapter, err := mexc.NewAdapter(cfgMap, symbol)
		if err != nil {
			return nil, err
		}
		return &mexcWrapper{adapter: adapter}, nil

	case "bingx":
		exchangeCfg, exists := cfg.Exchanges["bingx"]
		if !exists {
			return nil, fmt.Errorf("bingx 配置不存在")
		}
		cfgMap := map[string]string{
			"api_key":    exchangeCfg.APIKey,
			"secret_key": exchangeCfg.SecretKey,
			"testnet":    fmt.Sprintf("%v", exchangeCfg.Testnet),
		}
		adapter, err := bingx.NewAdapter(cfgMap, symbol)
		if err != nil {
			return nil, err
		}
		return &bingxWrapper{adapter: adapter}, nil

	case "deribit":
		exchangeCfg, exists := cfg.Exchanges["deribit"]
		if !exists {
			return nil, fmt.Errorf("deribit 配置不存在")
		}
		cfgMap := map[string]string{
			"api_key":    exchangeCfg.APIKey,
			"secret_key": exchangeCfg.SecretKey,
			"testnet":    fmt.Sprintf("%v", exchangeCfg.Testnet),
		}
		adapter, err := deribit.NewAdapter(cfgMap, symbol)
		if err != nil {
			return nil, err
		}
		return &deribitWrapper{adapter: adapter}, nil

	case "bitmex":
		exchangeCfg, exists := cfg.Exchanges["bitmex"]
		if !exists {
			return nil, fmt.Errorf("bitmex 配置不存在")
		}
		cfgMap := map[string]string{
			"api_key":    exchangeCfg.APIKey,
			"secret_key": exchangeCfg.SecretKey,
			"testnet":    fmt.Sprintf("%v", exchangeCfg.Testnet),
		}
		adapter, err := bitmex.NewAdapter(cfgMap, symbol)
		if err != nil {
			return nil, err
		}
		return &bitmexWrapper{adapter: adapter}, nil

	case "phemex":
		exchangeCfg, exists := cfg.Exchanges["phemex"]
		if !exists {
			return nil, fmt.Errorf("phemex 配置不存在")
		}
		if marketType == "spot" {
			return nil, fmt.Errorf("Phemex 交易所暫時不支援現貨交易，請使用合約模式或選擇其他交易所")
		}
		cfgMap := map[string]string{
			"api_key":    exchangeCfg.APIKey,
			"secret_key": exchangeCfg.SecretKey,
			"testnet":    fmt.Sprintf("%v", exchangeCfg.Testnet),
		}
		adapter, err := phemex.NewAdapter(cfgMap, symbol)
		if err != nil {
			return nil, err
		}
		return &phemexWrapper{adapter: adapter}, nil

	case "woox":
		exchangeCfg, exists := cfg.Exchanges["woox"]
		if !exists {
			return nil, fmt.Errorf("woox 配置不存在")
		}
		if marketType == "spot" {
			return nil, fmt.Errorf("WOO X 交易所暫時不支援現貨交易，請使用合約模式或選擇其他交易所")
		}
		cfgMap := map[string]string{
			"api_key":    exchangeCfg.APIKey,
			"secret_key": exchangeCfg.SecretKey,
			"testnet":    fmt.Sprintf("%v", exchangeCfg.Testnet),
		}
		adapter, err := woox.NewAdapter(cfgMap, symbol)
		if err != nil {
			return nil, err
		}
		return &wooxWrapper{adapter: adapter}, nil

	case "coinex":
		exchangeCfg, exists := cfg.Exchanges["coinex"]
		if !exists {
			return nil, fmt.Errorf("coinex 配置不存在")
		}
		if marketType == "spot" {
			return nil, fmt.Errorf("CoinEx 交易所暫時不支援現貨交易，請使用合約模式或選擇其他交易所")
		}
		cfgMap := map[string]string{
			"api_key":    exchangeCfg.APIKey,
			"secret_key": exchangeCfg.SecretKey,
			"testnet":    fmt.Sprintf("%v", exchangeCfg.Testnet),
		}
		adapter, err := coinex.NewAdapter(cfgMap, symbol)
		if err != nil {
			return nil, err
		}
		return &coinexWrapper{adapter: adapter}, nil

	case "bitrue":
		exchangeCfg, exists := cfg.Exchanges["bitrue"]
		if !exists {
			return nil, fmt.Errorf("bitrue 配置不存在")
		}
		if marketType == "spot" {
			return nil, fmt.Errorf("Bitrue 交易所暫時不支援現貨交易，請使用合約模式或選擇其他交易所")
		}
		cfgMap := map[string]string{
			"api_key":    exchangeCfg.APIKey,
			"secret_key": exchangeCfg.SecretKey,
			"testnet":    fmt.Sprintf("%v", exchangeCfg.Testnet),
		}
		adapter, err := bitrue.NewAdapter(cfgMap, symbol)
		if err != nil {
			return nil, err
		}
		return &bitrueWrapper{adapter: adapter}, nil

	case "xtcom":
		exchangeCfg, exists := cfg.Exchanges["xtcom"]
		if !exists {
			return nil, fmt.Errorf("xtcom 配置不存在")
		}
		cfgMap := map[string]string{
			"api_key":    exchangeCfg.APIKey,
			"secret_key": exchangeCfg.SecretKey,
			"testnet":    fmt.Sprintf("%v", exchangeCfg.Testnet),
		}
		adapter, err := xtcom.NewAdapter(cfgMap, symbol)
		if err != nil {
			return nil, err
		}
		return &xtcomWrapper{adapter: adapter}, nil

	case "btcc":
		exchangeCfg, exists := cfg.Exchanges["btcc"]
		if !exists {
			return nil, fmt.Errorf("btcc 配置不存在")
		}
		if marketType == "spot" {
			return nil, fmt.Errorf("BTCC 交易所暫時不支援現貨交易，請使用合約模式或選擇其他交易所")
		}
		cfgMap := map[string]string{
			"api_key":    exchangeCfg.APIKey,
			"secret_key": exchangeCfg.SecretKey,
			"testnet":    fmt.Sprintf("%v", exchangeCfg.Testnet),
		}
		adapter, err := btcc.NewAdapter(cfgMap, symbol)
		if err != nil {
			return nil, err
		}
		return &btccWrapper{adapter: adapter}, nil

	case "ascendex":
		exchangeCfg, exists := cfg.Exchanges["ascendex"]
		if !exists {
			return nil, fmt.Errorf("ascendex 配置不存在")
		}
		if marketType == "spot" {
			return nil, fmt.Errorf("AscendEX 交易所暫時不支援現貨交易，請使用合約模式或選擇其他交易所")
		}
		cfgMap := map[string]string{
			"api_key":    exchangeCfg.APIKey,
			"secret_key": exchangeCfg.SecretKey,
			"testnet":    fmt.Sprintf("%v", exchangeCfg.Testnet),
		}
		adapter, err := ascendex.NewAdapter(cfgMap, symbol)
		if err != nil {
			return nil, err
		}
		return &ascendexWrapper{adapter: adapter}, nil

	case "poloniex":
		exchangeCfg, exists := cfg.Exchanges["poloniex"]
		if !exists {
			return nil, fmt.Errorf("poloniex 配置不存在")
		}
		if marketType == "spot" {
			return nil, fmt.Errorf("Poloniex 交易所暫時不支援現貨交易，請使用合約模式或選擇其他交易所")
		}
		cfgMap := map[string]string{
			"api_key":    exchangeCfg.APIKey,
			"secret_key": exchangeCfg.SecretKey,
			"testnet":    fmt.Sprintf("%v", exchangeCfg.Testnet),
		}
		adapter, err := poloniex.NewAdapter(cfgMap, symbol)
		if err != nil {
			return nil, err
		}
		return &poloniexWrapper{adapter: adapter}, nil

	case "cryptocom":
		exchangeCfg, exists := cfg.Exchanges["cryptocom"]
		if !exists {
			return nil, fmt.Errorf("cryptocom 配置不存在")
		}
		if marketType == "spot" {
			return nil, fmt.Errorf("Crypto.com 交易所暫時不支援現貨交易，請使用合約模式或選擇其他交易所")
		}
		cfgMap := map[string]string{
			"api_key":    exchangeCfg.APIKey,
			"secret_key": exchangeCfg.SecretKey,
			"testnet":    fmt.Sprintf("%v", exchangeCfg.Testnet),
		}
		adapter, err := cryptocom.NewAdapter(cfgMap, symbol)
		if err != nil {
			return nil, err
		}
		return &cryptocomWrapper{adapter: adapter}, nil

	case "whitebit":
		exchangeCfg, exists := cfg.Exchanges["whitebit"]
		if !exists {
			return nil, fmt.Errorf("whitebit 配置不存在")
		}
		if marketType == "spot" {
			return nil, fmt.Errorf("WhiteBIT 交易所暫時不支援現貨交易，請使用合約模式或選擇其他交易所")
		}
		cfgMap := map[string]string{
			"api_key":    exchangeCfg.APIKey,
			"secret_key": exchangeCfg.SecretKey,
			"testnet":    fmt.Sprintf("%v", exchangeCfg.Testnet),
		}
		adapter, err := whitebit.NewWhiteBITAdapter(cfgMap, symbol)
		if err != nil {
			return nil, err
		}
		return &whitebitWrapper{adapter: adapter}, nil

	case "edgex":
		return nil, fmt.Errorf("edgeX 尚未實現")

	default:
		return nil, fmt.Errorf("不支援的交易所: %s", exchangeName)
	}
}
