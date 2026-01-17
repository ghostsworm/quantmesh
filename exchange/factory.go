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
	"quantmesh/exchange/woox"
	"quantmesh/exchange/xtcom"
)

// NewExchange 创建交易所实例
// exchangeName/symbol 允许覆盖配置中的当前交易所和交易对，便于多交易对场景
func NewExchange(cfg *config.Config, exchangeName, symbol string) (IExchange, error) {
	if exchangeName == "" {
		exchangeName = cfg.App.CurrentExchange
	}
	if symbol == "" {
		symbol = cfg.Trading.Symbol
	}

	switch exchangeName {
	case "bitget":
		exchangeCfg, exists := cfg.Exchanges["bitget"]
		if !exists {
			return nil, fmt.Errorf("bitget 配置不存在")
		}
		// 将 ExchangeConfig 转换为 map[string]string
		cfgMap := map[string]string{
			"api_key":    exchangeCfg.APIKey,
			"secret_key": exchangeCfg.SecretKey,
			"passphrase": exchangeCfg.Passphrase,
			"testnet":    fmt.Sprintf("%v", exchangeCfg.Testnet), // 传递测试网配置
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
			"testnet":    fmt.Sprintf("%v", exchangeCfg.Testnet), // 传递测试网配置
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
			"settle":     "usdt", // 默认 USDT 永续合约
			"testnet":    fmt.Sprintf("%v", exchangeCfg.Testnet), // 传递测试网配置
		}
		// 如果配置了杠杆，传递杠杆配置
		if exchangeCfg.Leverage > 0 {
			cfgMap["leverage"] = fmt.Sprintf("%d", exchangeCfg.Leverage)
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

	case "edgex":
		return nil, fmt.Errorf("edgeX 尚未实现")

	default:
		return nil, fmt.Errorf("不支持的交易所: %s", exchangeName)
	}
}
