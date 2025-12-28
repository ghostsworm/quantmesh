package exchange

import (
	"fmt"
	"quantmesh/config"
	"quantmesh/exchange/binance"
	"quantmesh/exchange/bitfinex"
	"quantmesh/exchange/bitget"
	"quantmesh/exchange/bybit"
	"quantmesh/exchange/gate"
	"quantmesh/exchange/huobi"
	"quantmesh/exchange/kraken"
	"quantmesh/exchange/kucoin"
	"quantmesh/exchange/okx"
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

	case "edgex":
		return nil, fmt.Errorf("edgeX 尚未实现")

	default:
		return nil, fmt.Errorf("不支持的交易所: %s", exchangeName)
	}
}
