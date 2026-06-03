package exchange

import (
	"strings"
	"testing"

	"quantmesh/config"
)

func TestNewExchangeInternalConfigurationAndMarketTypeErrors(t *testing.T) {
	base := &config.Config{
		Exchanges: map[string]config.ExchangeConfig{},
	}
	base.App.CurrentExchange = "binance"
	base.Trading.Symbol = "BTCUSDT"

	tests := []struct {
		name         string
		exchangeName string
		marketType   string
		cfg          *config.Config
		want         string
	}{
		{"default exchange missing config", "", "", base, "binance 配置不存在"},
		{"bitget missing config", "bitget", "futures", base, "bitget 配置不存在"},
		{"binance missing config", "binance", "futures", base, "binance 配置不存在"},
		{"gate missing config", "gate", "futures", base, "gate 配置不存在"},
		{"okx missing config", "okx", "futures", base, "okx 配置不存在"},
		{"bybit missing config", "bybit", "futures", base, "bybit 配置不存在"},
		{"huobi missing config", "huobi", "futures", base, "huobi 配置不存在"},
		{"kucoin missing config", "kucoin", "futures", base, "kucoin 配置不存在"},
		{"kraken missing config", "kraken", "futures", base, "kraken 配置不存在"},
		{"bitfinex missing config", "bitfinex", "futures", base, "bitfinex 配置不存在"},
		{"mexc missing config", "mexc", "futures", base, "mexc 配置不存在"},
		{"bingx missing config", "bingx", "futures", base, "bingx 配置不存在"},
		{"deribit missing config", "deribit", "futures", base, "deribit 配置不存在"},
		{"bitmex missing config", "bitmex", "futures", base, "bitmex 配置不存在"},
		{"phemex spot unsupported before config use", "phemex", "spot", withExchange("phemex"), "不支援現貨交易"},
		{"woox spot unsupported before config use", "woox", "spot", withExchange("woox"), "不支援現貨交易"},
		{"coinex spot unsupported before config use", "coinex", "spot", withExchange("coinex"), "不支援現貨交易"},
		{"bitrue spot unsupported before config use", "bitrue", "spot", withExchange("bitrue"), "不支援現貨交易"},
		{"btcc spot unsupported before config use", "btcc", "spot", withExchange("btcc"), "不支援現貨交易"},
		{"ascendex spot unsupported before config use", "ascendex", "spot", withExchange("ascendex"), "不支援現貨交易"},
		{"poloniex spot unsupported before config use", "poloniex", "spot", withExchange("poloniex"), "不支援現貨交易"},
		{"cryptocom spot unsupported before config use", "cryptocom", "spot", withExchange("cryptocom"), "不支援現貨交易"},
		{"whitebit spot unsupported before config use", "whitebit", "spot", withExchange("whitebit"), "不支援現貨交易"},
		{"bitkub futures unsupported", "bitkub", "futures", withExchange("bitkub"), "Bitkub 交易所僅支援現貨交易"},
		{"coinsph futures unsupported", "coinsph", "futures", withExchange("coinsph"), "Coins.ph 交易所僅支援現貨交易"},
		{"spot exchange unsupported", "huobi", "spot", withExchange("huobi"), "不支援現貨交易"},
		{"spot margin unsupported", "okx", "spot_margin", withExchange("okx"), "不支援現貨槓桿"},
		{"funding carry maps to futures", "binance", config.MarketTypeFundingCarry, base, "binance 配置不存在"},
		{"funding perp spread maps to futures", "binance", config.MarketTypeFundingPerpSpread, base, "binance 配置不存在"},
		{"edgex placeholder", "edgex", "futures", base, "edgeX 尚未實現"},
		{"unknown exchange", "unknown", "futures", base, "不支援的交易所"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newExchangeInternal(tt.cfg, tt.exchangeName, "", tt.marketType)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want contains %q", err.Error(), tt.want)
			}
		})
	}
}

func TestNewExchangeDryRunRequiresValidUnderlyingExchange(t *testing.T) {
	cfg := &config.Config{Exchanges: map[string]config.ExchangeConfig{}}
	cfg.System.DryRun = true
	if _, err := NewExchange(cfg, "binance", "BTCUSDT", "futures"); err == nil || !strings.Contains(err.Error(), "binance 配置不存在") {
		t.Fatalf("dry-run invalid underlying exchange err = %v", err)
	}
}

func TestClonePublicConfigCopiesMap(t *testing.T) {
	src := map[string]string{"api_key": "a", "secret_key": "b"}
	got := clonePublicConfig(src)
	got["api_key"] = "changed"
	if src["api_key"] != "a" || got["secret_key"] != "b" {
		t.Fatalf("clonePublicConfig did not isolate maps: src=%#v got=%#v", src, got)
	}
}

func withExchange(name string) *config.Config {
	cfg := &config.Config{Exchanges: map[string]config.ExchangeConfig{name: {APIKey: "k", SecretKey: "s"}}}
	cfg.Trading.Symbol = "BTCUSDT"
	return cfg
}
