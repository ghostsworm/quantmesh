package position

import (
	"math"
	"testing"
	"time"

	"quantmesh/config"
)

func positionAllocationConfig() *config.Config {
	cfg := &config.Config{}
	cfg.PositionAllocation.Enabled = true
	alloc := config.SymbolAllocation{
		Exchange:      "binance",
		Symbol:        "BTCUSDT",
		MaxAmountUSDT: 1000,
		MaxPercentage: 50,
	}
	alloc.TieredLimits.Enabled = true
	alloc.TieredLimits.EmergencyLimit = 1500
	alloc.TieredLimits.Triggers.PriceDropPercent = 10
	alloc.TieredLimits.Triggers.PositionLayers = 3
	alloc.TieredLimits.Triggers.UnrealizedLossUSD = 200
	alloc.TieredLimits.Recovery.PriceRecoverPercent = 3
	alloc.TieredLimits.Recovery.CooldownSeconds = 1
	cfg.PositionAllocation.Allocations = []config.SymbolAllocation{alloc}
	return cfg
}

func TestAllocationManagerReserveLimitsStatusesAndTieredRecovery(t *testing.T) {
	cfg := positionAllocationConfig()
	am := NewAllocationManager(cfg)
	am.SetEventBus(nil)

	if am.getConfigAllocation("binance", "BTCUSDT") == nil || am.getConfigAllocation("okx", "BTCUSDT") != nil {
		t.Fatalf("config allocation lookup mismatch")
	}
	if status := am.GetStatus("binance", "BTCUSDT"); status == nil || status.MaxAmount != 1000 || status.LimitMode != "normal" {
		t.Fatalf("initial status = %#v", status)
	}
	if am.GetStatus("missing", "BTCUSDT") != nil {
		t.Fatalf("missing status should be nil")
	}

	if err := am.CheckAndReserve("binance", "BTCUSDT", 400, 1000); err != nil {
		t.Fatalf("reserve within percentage limit: %v", err)
	}
	if status := am.GetStatus("binance", "BTCUSDT"); status.UsedAmount != 400 || status.MaxAmount != 500 {
		t.Fatalf("reserved percentage status = %#v", status)
	}
	if err := am.CheckAndReserve("binance", "BTCUSDT", 200, 1000); err == nil {
		t.Fatalf("reserve above limit should fail")
	}
	am.Release("binance", "BTCUSDT", 1000)
	if status := am.GetStatus("binance", "BTCUSDT"); status.UsedAmount != 0 || status.AvailableAmount != status.MaxAmount {
		t.Fatalf("release status = %#v", status)
	}

	if err := am.SetMaxAmount("binance", "BTCUSDT", -1); err != nil {
		t.Fatalf("set max amount: %v", err)
	}
	if status := am.GetStatus("binance", "BTCUSDT"); status.MaxAmount != 0 {
		t.Fatalf("negative max should clamp to 0: %#v", status)
	}
	if err := am.SetMaxAmount("missing", "BTCUSDT", 1); err == nil {
		t.Fatalf("missing set max should fail")
	}
	am.SetUsedAmount("binance", "BTCUSDT", -1)
	if status := am.GetStatus("binance", "BTCUSDT"); status.UsedAmount != 0 {
		t.Fatalf("negative used should clamp to 0: %#v", status)
	}

	if err := am.SetMaxAmount("binance", "BTCUSDT", 1000); err != nil {
		t.Fatalf("restore max amount: %v", err)
	}
	am.CheckAndAdjustLimit("binance", "BTCUSDT", 85, 100, 1, 0)
	emergency := am.GetStatus("binance", "BTCUSDT")
	if emergency == nil || !emergency.IsEmergencyMode || emergency.MaxAmount != 1500 || emergency.LimitMode != "emergency" {
		t.Fatalf("emergency status = %#v", emergency)
	}
	am.allocations["binance:BTCUSDT"].EmergencyTriggeredAt = time.Now().Add(-2 * time.Second)
	am.CheckAndAdjustLimit("binance", "BTCUSDT", 98, 100, 1, 0)
	normal := am.GetStatus("binance", "BTCUSDT")
	if normal.IsEmergencyMode || normal.MaxAmount != 1000 || normal.LimitMode != "normal" {
		t.Fatalf("recovered status = %#v", normal)
	}

	am.CheckAndAdjustLimit("binance", "BTCUSDT", 100, 100, 3, 0)
	if !am.GetStatus("binance", "BTCUSDT").IsEmergencyMode {
		t.Fatalf("position layers should trigger emergency")
	}
	am.allocations["binance:BTCUSDT"].IsEmergencyMode = false
	am.allocations["binance:BTCUSDT"].MaxAmount = 1000
	am.CheckAndAdjustLimit("binance", "BTCUSDT", 100, 100, 1, -250)
	if !am.GetStatus("binance", "BTCUSDT").IsEmergencyMode {
		t.Fatalf("unrealized loss should trigger emergency")
	}

	statuses := am.GetAllStatuses()
	if len(statuses) != 1 || statuses[0].Exchange != "binance" {
		t.Fatalf("all statuses = %#v", statuses)
	}

	disabled := NewAllocationManager(&config.Config{})
	if err := disabled.CheckAndReserve("binance", "BTCUSDT", 1, 0); err != nil {
		t.Fatalf("disabled allocation should pass: %v", err)
	}
	if err := disabled.SetMaxAmount("binance", "BTCUSDT", 1); err != nil {
		t.Fatalf("disabled set max should pass: %v", err)
	}
	disabled.SetUsedAmount("binance", "BTCUSDT", 1)
	disabled.Release("binance", "BTCUSDT", 1)
}

func TestSmartOrderSlotSelectionAndSorting(t *testing.T) {
	cfg := &config.SmartOrderConfig{
		Enabled:           true,
		MaxOpenOrders:     2,
		OpenOrderDistance: 3,
	}
	globalCfg := &config.Config{}
	globalCfg.Trading.PriceInterval = 10
	spm := &SuperPositionManager{config: globalCfg}
	som := NewSmartOrderManager(spm, cfg)

	if som.getMaxOpenOrders() != 2 {
		t.Fatalf("max open orders = %d", som.getMaxOpenOrders())
	}
	if som.getMaxDistance() != 30 {
		t.Fatalf("max distance = %v", som.getMaxDistance())
	}

	longSlots := som.CalculateOpenSlots(100, []float64{50, 70, 80, 90, 95, 110}, "LONG")
	if len(longSlots) != 2 || longSlots[0] != 90 || longSlots[1] != 95 {
		t.Fatalf("long slots = %#v", longSlots)
	}
	shortSlots := som.CalculateOpenSlots(100, []float64{90, 105, 110, 120, 140}, "SHORT")
	if len(shortSlots) != 2 || shortSlots[0] != 120 || shortSlots[1] != 110 {
		t.Fatalf("short slots = %#v", shortSlots)
	}

	disabled := NewSmartOrderManager(spm, &config.SmartOrderConfig{})
	all := []float64{3, 1, 2}
	if got := disabled.CalculateOpenSlots(2, all, "LONG"); len(got) != len(all) {
		t.Fatalf("disabled should return all slots: %#v", got)
	}

	arr := []float64{3, 1, 2}
	som.sortFloat64s(arr, "LONG")
	if arr[0] != 1 || arr[2] != 3 {
		t.Fatalf("long sort = %#v", arr)
	}
	som.sortFloat64s(arr, "SHORT")
	if arr[0] != 3 || arr[2] != 1 {
		t.Fatalf("short sort = %#v", arr)
	}

	filteredLong := FilterSlotsByMaxOpenOrders([]float64{50, 70, 80, 90, 95}, 100, 10, 2, 3, "LONG")
	if len(filteredLong) != 2 || filteredLong[0] != 90 || filteredLong[1] != 95 {
		t.Fatalf("filtered long = %#v", filteredLong)
	}
	filteredShort := FilterSlotsByMaxOpenOrders([]float64{105, 110, 120, 150}, 100, 10, 2, 3, "SHORT")
	if len(filteredShort) != 2 || filteredShort[0] != 110 || filteredShort[1] != 105 {
		t.Fatalf("filtered short = %#v", filteredShort)
	}
	if got := FilterSlotsByMaxOpenOrders([]float64{1, 2}, 10, 1, 0, 0, "LONG"); len(got) != 2 {
		t.Fatalf("unlimited filter = %#v", got)
	}

	if minutes, ok := parseTimeHHMM("09:30"); !ok || minutes != 570 {
		t.Fatalf("parse time = %d %v", minutes, ok)
	}
	for _, input := range []string{"bad", "24:00", "12:60", "12"} {
		if _, ok := parseTimeHHMM(input); ok {
			t.Fatalf("invalid time %q parsed successfully", input)
		}
	}

	if math.Abs(som.getMaxDistance()-30) > 0.0001 {
		t.Fatalf("max distance drift")
	}
}
