package config

import "testing"

const backtestDataFixture = "" +
	"app:\n" +
	"  current_exchange: binance\n" +
	"exchanges:\n" +
	"  binance:\n" +
	"    api_key: k\n" +
	"    secret_key: s\n" +
	"    fee_rate: 0.0002\n" +
	"trading:\n" +
	"  symbol: ETHUSDT\n" +
	"  price_interval: 50\n" +
	"  order_quantity: 10\n" +
	"  buy_window_size: 1\n"

func TestBacktestDataDefaults(t *testing.T) {
	fromYAML, err := LoadConfigFromBytes([]byte(backtestDataFixture))
	if err != nil {
		t.Fatal(err)
	}
	if fromYAML.BacktestData.BinanceVisionDir != DefaultBinanceVisionDir {
		t.Fatalf("yaml default = %q", fromYAML.BacktestData.BinanceVisionDir)
	}

	fromJSON, err := LoadConfigFromJSON([]byte(`{"App":{"CurrentExchange":"binance"},"Exchanges":{"binance":{"api_key":"k","secret_key":"s","fee_rate":0.0002}},"Trading":{"Symbol":"ETHUSDT","PriceInterval":50,"OrderQuantity":10,"BuyWindowSize":1}}`))
	if err != nil {
		t.Fatal(err)
	}
	if fromJSON.BacktestData.BinanceVisionDir != DefaultBinanceVisionDir {
		t.Fatalf("json default = %q", fromJSON.BacktestData.BinanceVisionDir)
	}

	explicit, err := LoadConfigFromBytes([]byte(backtestDataFixture + "backtest_data:\n  binance_vision_dir: /tmp/vision\n"))
	if err != nil {
		t.Fatal(err)
	}
	if explicit.BacktestData.BinanceVisionDir != "/tmp/vision" {
		t.Fatalf("explicit dir = %q", explicit.BacktestData.BinanceVisionDir)
	}

	blank := &Config{}
	ApplyBacktestDataDefaults(blank)
	if blank.BacktestData.BinanceVisionDir != DefaultBinanceVisionDir {
		t.Fatalf("blank default = %q", blank.BacktestData.BinanceVisionDir)
	}
	blank.BacktestData.BinanceVisionDir = "  keep  "
	ApplyBacktestDataDefaults(blank)
	if blank.BacktestData.BinanceVisionDir != "  keep  " {
		t.Fatalf("non-empty dir overwritten: %q", blank.BacktestData.BinanceVisionDir)
	}
	ApplyBacktestDataDefaults(nil)
}
