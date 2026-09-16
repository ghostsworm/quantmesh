package main

import (
	"errors"
	"testing"

	"quantmesh/config"
	"quantmesh/position"
)

func TestResolveGridFeeRates(t *testing.T) {
	tests := []struct {
		name          string
		marketType    string
		allowExchange bool
		fetchMaker    float64
		fetchTaker    float64
		fetchErr      error
		wantMaker     float64
		wantTaker     float64
		wantSource    string
		wantFetch     bool
	}{
		{name: "合約優先交易所費率", marketType: "futures", allowExchange: true, fetchMaker: 0.0002, fetchTaker: 0.0005, wantMaker: 0.0002, wantTaker: 0.0005, wantSource: gridFeeRateSourceExchange, wantFetch: true},
		{name: "拉取失敗回退配置", marketType: "futures", allowExchange: true, fetchErr: errors.New("no key"), wantMaker: 0.0004, wantTaker: 0.0004, wantSource: gridFeeRateSourceConfig, wantFetch: true},
		{name: "交易所返回無效 taker 回退配置", marketType: "futures", allowExchange: true, fetchTaker: 0, wantMaker: 0.0004, wantTaker: 0.0004, wantSource: gridFeeRateSourceConfig, wantFetch: true},
		{name: "跳過交易所拉取", marketType: "futures", allowExchange: false, wantMaker: 0.0004, wantTaker: 0.0004, wantSource: gridFeeRateSourceConfig},
		{name: "現貨不調合約費率端點", marketType: "spot", allowExchange: true, wantMaker: 0.0004, wantTaker: 0.0004, wantSource: gridFeeRateSourceConfig},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := fetchExchangeFeeRates
			t.Cleanup(func() { fetchExchangeFeeRates = orig })
			fetched := false
			fetchExchangeFeeRates = func(cfg *config.Config, exchangeName, symbol string) (float64, float64, error) {
				fetched = true
				return tt.fetchMaker, tt.fetchTaker, tt.fetchErr
			}
			symCfg := config.SymbolConfig{Exchange: "binance", Symbol: "ETHUSDT", MarketType: tt.marketType}
			maker, taker, source := resolveGridFeeRates(&config.Config{}, symCfg, 0.0004, tt.allowExchange)
			if maker != tt.wantMaker || taker != tt.wantTaker || source != tt.wantSource {
				t.Fatalf("resolveGridFeeRates = %v %v %s, want %v %v %s", maker, taker, source, tt.wantMaker, tt.wantTaker, tt.wantSource)
			}
			if fetched != tt.wantFetch {
				t.Fatalf("fetched = %v, want %v", fetched, tt.wantFetch)
			}
		})
	}
}

func TestApplyGridFeeRatesRespectsSkipFlag(t *testing.T) {
	orig := fetchExchangeFeeRates
	t.Cleanup(func() { fetchExchangeFeeRates = orig })
	fetchExchangeFeeRates = func(cfg *config.Config, exchangeName, symbol string) (float64, float64, error) {
		return 0.0001, 0.0003, nil
	}
	cfg := &config.Config{}
	cfg.Trading.Symbol = "ETHUSDT"
	cfg.Timing.SkipExchangeFeeOnBotStart = true
	spm := position.NewSuperPositionManager(cfg, nil, nil, 2, 3)
	symCfg := config.SymbolConfig{Exchange: "binance", Symbol: "ETHUSDT", MarketType: "futures"}

	applyGridFeeRates(t.Context(), cfg, symCfg, 0.0004, spm)
	if maker, taker, ok := spm.GetFeeRates(); !ok || maker != 0.0004 || taker != 0.0004 {
		t.Fatalf("skip flag: fees = %v %v %v, want config 0.0004", maker, taker, ok)
	}

	cfg.Timing.SkipExchangeFeeOnBotStart = false
	applyGridFeeRates(t.Context(), cfg, symCfg, 0.0004, spm)
	if maker, taker, ok := spm.GetFeeRates(); !ok || maker != 0.0001 || taker != 0.0003 {
		t.Fatalf("exchange fees = %v %v %v, want 0.0001 0.0003", maker, taker, ok)
	}

	stop := startGridFeeRateRefresh(t.Context(), cfg, symCfg, 0.0004, spm) // 未配置刷新間隔：返回空操作
	stop()
	stop()
}
