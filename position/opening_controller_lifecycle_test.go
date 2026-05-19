package position

import (
	"testing"

	"quantmesh/config"
)

func TestOpeningControllerCanStopTwiceAndRestart(t *testing.T) {
	symbolCfg := &config.SymbolConfig{Exchange: "mock", Symbol: "BTCUSDT"}
	controller := NewOpeningController(nil, symbolCfg)

	controller.Start()
	controller.Stop()
	controller.Stop()

	controller.Start()
	controller.mu.RLock()
	running := controller.running
	controller.mu.RUnlock()
	if !running {
		t.Fatal("开仓控制器 Stop 后应允许重新 Start")
	}
	controller.Stop()
}
