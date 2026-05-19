package risk

import (
	"testing"
	"time"

	"quantmesh/config"
)

func TestDynamicStopLossManagerCanRestartAfterStop(t *testing.T) {
	manager := NewDynamicStopLossManager(&config.DynamicStopLossConfig{}, nil, &circuitBreakerMockProvider{})

	manager.Start()
	if !manager.isRunning {
		t.Fatal("Start() 后管理器应处于运行状态")
	}
	if manager.stopChan == nil {
		t.Fatal("Start() 应创建新的停止通道")
	}

	firstStopChan := manager.stopChan
	manager.Stop()
	if manager.isRunning {
		t.Fatal("Stop() 后管理器应停止运行")
	}
	if manager.stopChan != nil {
		t.Fatal("Stop() 后应清空停止通道，避免复用已关闭通道")
	}

	manager.Start()
	if !manager.isRunning {
		t.Fatal("二次 Start() 后管理器应重新运行")
	}
	if manager.stopChan == nil || manager.stopChan == firstStopChan {
		t.Fatal("二次 Start() 应创建新的停止通道")
	}
	manager.Stop()
}

func TestDynamicStopLossManagerNilConfigDoesNotStart(t *testing.T) {
	manager := NewDynamicStopLossManager(nil, nil, &circuitBreakerMockProvider{})

	manager.Start()

	if manager.isRunning {
		t.Fatal("配置为空时不应启动动态止损管理器")
	}
}

func TestDynamicStopLossManagerUsesSafeIntervalsForZeroConfig(t *testing.T) {
	cfg := &config.DynamicStopLossConfig{}
	cfg.VolatilityBased.Enabled = true
	cfg.VolatilityBased.CheckInterval = 0
	cfg.ProfitBasedTrailing.Enabled = true
	cfg.ProfitBasedTrailing.UpdateFrequency = 0

	manager := NewDynamicStopLossManager(cfg, nil, nil)

	manager.Start()
	if !manager.isRunning {
		t.Fatal("启用子检查器时即使间隔为 0 也应安全启动")
	}
	if got := manager.checkIntervalDuration("test", 0, time.Second); got != time.Second {
		t.Fatalf("无效间隔应使用兜底值，got %s", got)
	}
	manager.Stop()
}

func TestDynamicStopLossManagerNilBotProviderDoesNotPanic(t *testing.T) {
	manager := NewDynamicStopLossManager(&config.DynamicStopLossConfig{}, nil, nil)

	manager.checkProfitTrailing()
	manager.applyTimeBasedAdjustment(1.2)
}

func TestDynamicStopLossGetActiveSlotsReturnsDefensiveCopy(t *testing.T) {
	manager := NewDynamicStopLossManager(&config.DynamicStopLossConfig{}, nil, &circuitBreakerMockProvider{})
	manager.activeSlots["bot-1"] = &DynamicStopLossSlot{
		BotID:       "bot-1",
		Symbol:      "BTCUSDT",
		CurrentStop: 100,
		Activated:   true,
	}

	slots := manager.GetActiveSlots()
	slots["bot-1"].CurrentStop = 1
	delete(slots, "bot-1")

	internal := manager.GetActiveSlots()
	slot, ok := internal["bot-1"]
	if !ok {
		t.Fatal("修改返回 map 不应删除内部槽位")
	}
	if slot.CurrentStop != 100 {
		t.Fatalf("修改返回槽位不应篡改内部状态，CurrentStop=%f", slot.CurrentStop)
	}
}
