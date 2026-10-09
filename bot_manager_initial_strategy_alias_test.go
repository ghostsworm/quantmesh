package main

import (
	"testing"

	"quantmesh/config"
)

func TestRegisteredBotStrategyContractDoesNotAliasInput(t *testing.T) {
	bm := NewBotManager(&config.Config{}, nil, nil, nil, "")
	input := config.BotConfig{ID: "owner", Exchange: "binance", Symbol: "BTCUSDT", Strategies: []config.StrategyInstance{{Type: "grid", Config: map[string]interface{}{"steps": []interface{}{int(2)}}}}}
	br := &BotRuntime{BotID: input.ID, Config: input, Inner: &SymbolRuntime{Config: config.BotConfigToSymbolConfig(input)}}
	bm.AddRuntime(br)
	registered := &br.Config.Strategies[0]
	bm.AddRuntime(br)
	if &br.Config.Strategies[0] != registered {
		t.Fatal("repeated registration rewrote published strategy references")
	}
	input.Strategies[0].Type = "dca"
	input.Strategies[0].Config["steps"].([]interface{})[0] = int(999)
	if br.Config.Strategies[0].Type != "grid" || br.Config.Strategies[0].Config["steps"].([]interface{})[0] != int(2) {
		t.Fatal("original configuration mutation rewrote registered strategy recovery contract")
	}
}
