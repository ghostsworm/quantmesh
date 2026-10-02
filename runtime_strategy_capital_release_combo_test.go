package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"quantmesh/config"
	"quantmesh/strategy"
)

type comboReleaseStateStore struct {
	states map[string]capitalReleaseDebtStateStore
}

func (s *comboReleaseStateStore) LoadRuntimeState(name string) (int, string, bool, error) {
	state, found := s.states[name]
	return state.version, state.payload, found, state.err
}
func (s *comboReleaseStateStore) LoadRuntimeStateContext(ctx context.Context, name string) (int, string, bool, error) {
	if err := ctx.Err(); err != nil {
		return 0, "", false, err
	}
	return s.LoadRuntimeState(name)
}
func (s *comboReleaseStateStore) SaveRuntimeState(name string, version int, payload string) error {
	s.states[name] = capitalReleaseDebtStateStore{version: version, payload: payload}
	return nil
}

func TestRuntimeStrategyCapitalReleaseRejectsInvisibleComboChild(t *testing.T) {
	rt, venue, _ := capitalReleaseRuntimeFixture(t)
	cfg := &config.Config{}
	cfg.Trading.BotID, cfg.Trading.Symbol = rt.capitalReleaseScope.Bot, rt.capitalReleaseScope.Symbol
	combo := strategy.NewComboStrategy("combo", cfg.Trading.Symbol, cfg, nil, nil, map[string]interface{}{
		"strategies": []interface{}{map[string]interface{}{"name": "child", "type": "dca", "weight": 1.0}},
	})
	digest := sha256.Sum256([]byte("combo"))
	key := "combo:" + hex.EncodeToString(digest[:16]) + ":child"
	clean := `{"bot_id":"a","strategy_name":"child","symbol":"BTCUSDT","close_layer_index":-1,"layers":[]}`
	dirty := `{"bot_id":"a","strategy_name":"child","symbol":"BTCUSDT","close_layer_index":-1,"layers":[{"Index":0,"ClientOrderID":"pending","RequestedQuantity":1,"Status":"UNKNOWN"}],"current_layer":1}`
	store := &comboReleaseStateStore{states: map[string]capitalReleaseDebtStateStore{
		"combo": {version: 1, payload: `{"bot_id":"a","strategy_name":"combo","symbol":"BTCUSDT","peak_equity":1000}`},
		key:     {version: 2, payload: dirty},
	}}
	if err := combo.SetRuntimeStateStore(store); err != nil {
		t.Fatal(err)
	}
	rt.StrategyManager.RegisterStrategy("combo", combo, 1, 0)
	if len(combo.GetPositions()) != 0 || len(combo.GetOrders()) != 0 {
		t.Fatal("fixture must omit child durable intent from memory")
	}
	amounts, err := releaseRuntimeStrategyCapital(t.Context(), []*SymbolRuntime{rt}, "")
	if err == nil || amounts["dca"] != 0 || rt.StrategyManager.GetCapitalAllocator().GetUsed("dca") != 200 {
		t.Fatalf("Combo child intent ignored: amounts=%v err=%v", amounts, err)
	}
	store.states[key] = capitalReleaseDebtStateStore{version: 2, payload: clean}
	venue.onRead = func(context.Context) { store.states[key] = capitalReleaseDebtStateStore{version: 2, payload: dirty} }
	amounts, err = releaseRuntimeStrategyCapital(t.Context(), []*SymbolRuntime{rt}, "dca")
	if err == nil || venue.lastReadSymbol != cfg.Trading.Symbol || amounts["dca"] != 0 || rt.StrategyManager.GetCapitalAllocator().GetUsed("dca") != 200 {
		t.Fatalf("new Combo child state ignored: amounts=%v err=%v", amounts, err)
	}
	venue.onRead = nil
	store.states[key] = capitalReleaseDebtStateStore{version: 2, payload: clean}
	amounts, err = releaseRuntimeStrategyCapital(t.Context(), []*SymbolRuntime{rt}, "")
	if err != nil || amounts["dca"] != 200 {
		t.Fatalf("clean Combo child blocked legal release: amounts=%v err=%v", amounts, err)
	}
}
