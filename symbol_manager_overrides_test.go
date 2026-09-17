package main

import (
	"context"
	"strings"
	"testing"

	"quantmesh/config"
)

// TestStartSymbolRuntimeRejectsInvalidTradingOverrides 單個 Bot 的 trading_overrides 非法時在創建交易所實例前拒絕啟動，不影響全局配置
func TestStartSymbolRuntimeRejectsInvalidTradingOverrides(t *testing.T) {
	base := &config.Config{}
	base.Trading.InventorySkew = config.InventorySkewConfig{Enabled: true, Strength: 0.5}

	symCfg := config.SymbolConfig{
		ID:         "bad-bot",
		Exchange:   "binance",
		Symbol:     "BTCUSDT",
		MarketType: "futures",
		TradingOverrides: &config.BotTradingOverrides{
			InventorySkew: &config.InventorySkewConfig{Enabled: true, Strength: 2},
		},
	}
	rt, err := startSymbolRuntime(context.Background(), base, symCfg, nil, nil, nil, nil)
	if err == nil || rt != nil {
		t.Fatalf("expected start failure, got rt=%v err=%v", rt, err)
	}
	if !strings.Contains(err.Error(), "trading_overrides") || !strings.Contains(err.Error(), "BTCUSDT") {
		t.Fatalf("error should name bot and trading_overrides: %v", err)
	}
	if base.Trading.InventorySkew.Strength != 0.5 {
		t.Fatal("global config must not be mutated")
	}
}

// TestGridRegimeRuntimeUsesBotOverrides 合併後的局部配置決定是否創建 K 線檢測器（全局關閉、Bot 開啟）
func TestGridRegimeRuntimeUsesBotOverrides(t *testing.T) {
	base := &config.Config{}
	local := config.MergeBotTradingOverrides(base, &config.BotTradingOverrides{
		UpperBoundFreeze: &config.UpperBoundFreezeConfig{Enabled: true},
	})
	rt, err := newGridRegimeRuntime(&local, "BTCUSDT", &fakeKlineSource{})
	if err != nil || rt == nil {
		t.Fatalf("expected detector from bot override, rt=%v err=%v", rt, err)
	}
	if gridRegimeNeeded(base) {
		t.Fatal("global config should remain disabled")
	}

	bad := config.MergeBotTradingOverrides(base, &config.BotTradingOverrides{
		RegimeFilter: &config.RegimeFilterConfig{Enabled: true, KlineInterval: "1w"},
	})
	if _, err := newGridRegimeRuntime(&bad, "BTCUSDT", &fakeKlineSource{}); err == nil {
		t.Fatal("invalid bot regime override should be rejected")
	}
}
