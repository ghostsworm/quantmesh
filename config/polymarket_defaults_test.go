package config

import (
	"encoding/json"
	"testing"
)

func TestApplyGammaRelatedDefaults_EmptyJSON(t *testing.T) {
	var cfg Config
	if err := json.Unmarshal([]byte(`{}`), &cfg); err != nil {
		t.Fatal(err)
	}
	ApplyGammaRelatedDefaults(&cfg)
	if cfg.MacroEvent.GammaAPIURL != DefaultGammaAPIURL {
		t.Fatalf("MacroEvent.GammaAPIURL: %q", cfg.MacroEvent.GammaAPIURL)
	}
	if cfg.AI.Modules.PolymarketSignal.APIURL != DefaultGammaAPIURL {
		t.Fatalf("PolymarketSignal.APIURL: %q", cfg.AI.Modules.PolymarketSignal.APIURL)
	}
	if cfg.AI.Modules.PolymarketSignal.AnalysisInterval != DefaultPolymarketAnalysisIntervalSec {
		t.Fatalf("AnalysisInterval: %d", cfg.AI.Modules.PolymarketSignal.AnalysisInterval)
	}
	sg := cfg.AI.Modules.PolymarketSignal.SignalGeneration
	if sg.BuyThreshold != 0.65 || sg.SellThreshold != 0.35 {
		t.Fatalf("signal_generation defaults: %+v", sg)
	}
}

func TestApplyGammaRelatedDefaults_PreservesExplicitGamma(t *testing.T) {
	cfg := &Config{}
	cfg.MacroEvent.GammaAPIURL = "https://example.com/gamma"
	cfg.AI.Modules.PolymarketSignal.APIURL = "https://example.com/gamma2"
	ApplyGammaRelatedDefaults(cfg)
	if cfg.MacroEvent.GammaAPIURL != "https://example.com/gamma" {
		t.Fatal("macro gamma overwritten")
	}
	if cfg.AI.Modules.PolymarketSignal.APIURL != "https://example.com/gamma2" {
		t.Fatal("polymarket api_url overwritten")
	}
}
