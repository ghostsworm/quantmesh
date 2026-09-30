package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/execution"
	"quantmesh/storage"
)

func TestSpecializedRiskDataGateDoesNotClearManualPause(t *testing.T) {
	gate := &execution.OpeningGate{}
	gate.Block("manual")
	cancelRequested := make(chan struct{}, 1)
	runtime := &BotRuntime{BotID: "specialized-bot", Inner: &SymbolRuntime{
		OpeningGate: gate,
		CancelOpeningOrders: func(context.Context) error {
			cancelRequested <- struct{}{}
			return nil
		},
	}}

	runtime.SetRiskDataUnavailable(true)
	select {
	case <-cancelRequested:
	case <-time.After(time.Second):
		t.Fatal("equity-data hold did not request cancellation of owned opening orders")
	}
	if !gate.HasBlock(equityDataUnavailableBlock) || !gate.HasBlock("manual") {
		t.Fatal("unavailable equity data did not add an independent hold")
	}
	runtime.SetRiskDataUnavailable(false)
	if gate.HasBlock(equityDataUnavailableBlock) || !gate.HasBlock("manual") || !gate.Blocked() {
		t.Fatal("equity recovery cleared or bypassed the user's manual pause")
	}

	gate.Unblock("manual")
	runtime.SetRiskDataUnavailable(true)
	if !gate.Blocked() || !gate.HasBlock(equityDataUnavailableBlock) {
		t.Fatal("specialized runtime remained open while equity data was unavailable")
	}
	runtime.SetRiskDataUnavailable(false)
	if gate.Blocked() {
		t.Fatal("equity recovery did not clear its own hold")
	}
}

func TestBotRemovalSerializesAgainstStaleStartRequest(t *testing.T) {
	bm := NewBotManager(&config.Config{}, nil, nil, nil, "")
	persistEntered := make(chan struct{})
	allowPersist := make(chan struct{})
	startAttempted := make(chan struct{})
	validationEntered := make(chan struct{})
	startResult := make(chan error, 1)
	var removed atomic.Bool
	removeResult := make(chan error, 1)
	bm.SetStartConfigValidator(func(config.BotConfig) error {
		close(validationEntered)
		if removed.Load() {
			return errors.New("Bot configuration was removed")
		}
		return nil
	})
	go func() {
		removeResult <- bm.StopBotsAndPersistRemoval([]string{"bot-1"}, func() error {
			close(persistEntered)
			<-allowPersist
			removed.Store(true)
			return nil
		})
	}()
	<-persistEntered
	go func() {
		close(startAttempted)
		_, err := bm.StartBot(context.Background(), config.BotConfig{ID: "bot-1"})
		startResult <- err
	}()
	<-startAttempted
	select {
	case <-validationEntered:
		t.Fatal("stale start validation ran during the removal persistence callback")
	case <-time.After(25 * time.Millisecond):
	}
	close(allowPersist)
	if err := <-removeResult; err != nil {
		t.Fatalf("StopBotsAndPersistRemoval: %v", err)
	}
	if err := <-startResult; err == nil {
		t.Fatal("stale start request succeeded after the Bot configuration was removed")
	}
}

func TestBotManagerKeepsSpecializedRuntimeWhenSafeStopFails(t *testing.T) {
	bus := event.NewEventBus(8)
	gate := &execution.OpeningGate{}
	stopFailure := errors.New("second leg remains exposed")
	stopCalls := 0
	runtime := &BotRuntime{
		BotID:  "spread-bot",
		Config: config.BotConfig{ID: "spread-bot", Exchange: "binance", Symbol: "BTCUSDT", MarketType: config.MarketTypeFundingPerpSpread},
		Inner: &SymbolRuntime{
			OpeningGate: gate,
			StopWithError: func() error {
				stopCalls++
				gate.Block("strategy_stop_unverified")
				return stopFailure
			},
		},
	}
	bm := &BotManager{runtimes: make(map[string]*BotRuntime), eventBus: bus}
	bm.AddRuntime(runtime)

	err := bm.StopBot(runtime.BotID)
	if !errors.Is(err, stopFailure) {
		t.Fatalf("StopBot error = %v, want wrapped close failure", err)
	}
	if got, ok := bm.Get(runtime.BotID); !ok || got != runtime {
		t.Fatal("runtime was removed after an unverified stop")
	}
	if !gate.HasBlock("strategy_stop_unverified") || runtime.Inner.shutdownCloseUnverifiedReason() == "" {
		t.Fatal("unverified stop did not preserve an opening hold and reconciliation reason")
	}
	if stopCalls != 1 {
		t.Fatalf("safe stop calls = %d, want 1", stopCalls)
	}
}

func TestBotManagerRemovesSpecializedRuntimeAfterSafeStopRetry(t *testing.T) {
	bus := event.NewEventBus(8)
	gate := &execution.OpeningGate{}
	stopFailure := errors.New("temporary close failure")
	stopCalls := 0
	runtime := &BotRuntime{
		BotID:  "spread-retry-bot",
		Config: config.BotConfig{ID: "spread-retry-bot", Exchange: "binance", Symbol: "ETHUSDT", MarketType: config.MarketTypeFundingPerpSpread},
		Inner: &SymbolRuntime{
			OpeningGate: gate,
			StopWithError: func() error {
				stopCalls++
				if stopCalls == 1 {
					gate.Block("strategy_stop_unverified")
					return stopFailure
				}
				gate.Unblock("strategy_stop_unverified")
				return nil
			},
		},
	}
	bm := &BotManager{runtimes: make(map[string]*BotRuntime), eventBus: bus}
	bm.AddRuntime(runtime)
	if err := bm.StopBot(runtime.BotID); !errors.Is(err, stopFailure) {
		t.Fatalf("first StopBot error = %v, want close failure", err)
	}
	if err := bm.StopBot(runtime.BotID); err != nil {
		t.Fatalf("retry StopBot: %v", err)
	}
	if _, ok := bm.Get(runtime.BotID); ok {
		t.Fatal("runtime remains registered after safe stop retry succeeded")
	}
	if gate.HasBlock("strategy_stop_unverified") || stopCalls != 2 {
		t.Fatalf("successful retry did not clear only its stop hold: blocked=%v calls=%d", gate.HasBlock("strategy_stop_unverified"), stopCalls)
	}
}

func TestBotManagerStopAllRetainsRuntimeWhenSafeStopFails(t *testing.T) {
	stopFailure := errors.New("spread leg close is unverified")
	gate := &execution.OpeningGate{}
	runtime := &BotRuntime{
		BotID:  "spread-stop-all-bot",
		Config: config.BotConfig{ID: "spread-stop-all-bot", Exchange: "binance", Symbol: "SOLUSDT", MarketType: config.MarketTypeFundingPerpSpread},
		Inner: &SymbolRuntime{
			OpeningGate: gate,
			StopWithError: func() error {
				return stopFailure
			},
		},
	}
	bm := &BotManager{runtimes: make(map[string]*BotRuntime)}
	bm.AddRuntime(runtime)

	if err := bm.StopAll(); !errors.Is(err, stopFailure) {
		t.Fatalf("StopAll() error = %v, want wrapped close failure", err)
	}
	if got, ok := bm.Get(runtime.BotID); !ok || got != runtime {
		t.Fatal("StopAll removed runtime with unverified exposure")
	}
	if !gate.HasBlock("strategy_stop_unverified") || runtime.Inner.shutdownCloseUnverifiedReason() == "" {
		t.Fatal("StopAll did not preserve failure hold and reconciliation reason")
	}
}

func TestBotRemovalDoesNotPersistWhenSafeStopFails(t *testing.T) {
	stopFailure := errors.New("position close remains unverified")
	runtime := &BotRuntime{
		BotID: "bot-removal-stop-failure",
		Config: config.BotConfig{
			ID:         "bot-removal-stop-failure",
			Exchange:   "binance",
			Symbol:     "BTCUSDT",
			MarketType: "futures",
		},
		Inner: &SymbolRuntime{StopWithError: func() error { return stopFailure }},
	}
	bm := &BotManager{runtimes: make(map[string]*BotRuntime)}
	bm.AddRuntime(runtime)
	persisted := false
	err := bm.StopBotsAndPersistRemoval([]string{runtime.BotID}, func() error {
		persisted = true
		return nil
	})
	if !errors.Is(err, stopFailure) {
		t.Fatalf("removal error = %v, want stop failure", err)
	}
	if persisted {
		t.Fatal("configuration persistence ran after an unverified stop")
	}
	if got, ok := bm.Get(runtime.BotID); !ok || got != runtime {
		t.Fatal("runtime was removed despite unverified stop")
	}
}

func TestBotManagerResolveLatestStartConfigUsesRefreshedBotSnapshot(t *testing.T) {
	botID := "bot-start-refresh"
	stale := config.BotConfig{
		ID:                  botID,
		Exchange:            "binance",
		Symbol:              "BTCUSDT",
		MarketType:          "futures",
		OrderQuantity:       100,
		PositionSafetyCheck: 5,
	}
	fresh := stale
	fresh.OrderQuantity = 250

	bm := &BotManager{
		cfg: &config.Config{
			Bots: []config.BotConfig{fresh},
		},
	}

	got, err := bm.resolveLatestStartConfig(stale)
	if err != nil {
		t.Fatalf("resolveLatestStartConfig: %v", err)
	}
	if got.OrderQuantity != 250 {
		t.Fatalf("expected refreshed order quantity 250, got %.2f", got.OrderQuantity)
	}
}

func TestBotManagerResolveLatestStartConfigPrefersBotConfigSnapshot(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "quantmesh.db")

	cfg := &config.Config{}
	cfg.Storage.Enabled = true
	cfg.Storage.Type = "sqlite"
	cfg.Storage.Path = dbPath
	cfg.Storage.BufferSize = 1
	cfg.Storage.BatchSize = 1

	storageService, err := storage.NewStorageService(cfg, context.Background())
	if err != nil {
		t.Fatalf("NewStorageService: %v", err)
	}
	defer storageService.Stop()

	botID := "bot-config-ssot"
	stale := config.BotConfig{
		ID:            botID,
		Exchange:      "binance",
		Symbol:        "BTCUSDT",
		MarketType:    "futures",
		OrderQuantity: 100,
	}
	if _, err := storage.SaveBotConfigSnapshot(
		context.Background(),
		storageService.GetStorage(),
		&config.BotConfigFile{
			BotID:      botID,
			Name:       "Test Bot",
			Exchange:   "binance",
			Symbol:     "BTCUSDT",
			MarketType: "futures",
			Grid: config.GridConfig{
				OrderQuantity: 250,
			},
		},
		"test",
		"unit",
	); err != nil {
		t.Fatalf("SaveBotConfigSnapshot: %v", err)
	}

	bm := &BotManager{
		cfg: &config.Config{
			Bots: []config.BotConfig{stale},
		},
		storageService: storageService,
	}

	got, err := bm.resolveLatestStartConfig(stale)
	if err != nil {
		t.Fatalf("resolveLatestStartConfig: %v", err)
	}
	if got.OrderQuantity != 250 {
		t.Fatalf("expected bot_configs order quantity 250, got %.2f", got.OrderQuantity)
	}
}

// bot_configs 快照不帶 Enabled：解析後必須沿用主配置中的值，而不是 nil
func TestBotManagerResolveLatestStartConfigPreservesEnabled(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.Enabled = true
	cfg.Storage.Type = "sqlite"
	cfg.Storage.Path = filepath.Join(t.TempDir(), "quantmesh.db")
	cfg.Storage.BufferSize = 1
	cfg.Storage.BatchSize = 1

	storageService, err := storage.NewStorageService(cfg, context.Background())
	if err != nil {
		t.Fatalf("NewStorageService: %v", err)
	}
	defer storageService.Stop()

	botID := "bot-config-enabled"
	if _, err := storage.SaveBotConfigSnapshot(
		context.Background(),
		storageService.GetStorage(),
		&config.BotConfigFile{
			BotID:      botID,
			Exchange:   "binance",
			Symbol:     "BTCUSDT",
			MarketType: "futures",
			Grid:       config.GridConfig{OrderQuantity: 250},
		},
		"test",
		"unit",
	); err != nil {
		t.Fatalf("SaveBotConfigSnapshot: %v", err)
	}

	mainCfg := config.BotConfig{
		ID:         botID,
		Exchange:   "binance",
		Symbol:     "BTCUSDT",
		MarketType: "futures",
		Enabled:    config.BoolPtr(false),
		CreatedAt:  "2026-01-01T00:00:00Z",
	}
	bm := &BotManager{
		cfg:            &config.Config{Bots: []config.BotConfig{mainCfg}},
		storageService: storageService,
	}

	got, err := bm.resolveLatestStartConfig(mainCfg)
	if err != nil {
		t.Fatalf("resolveLatestStartConfig: %v", err)
	}
	if got.OrderQuantity != 250 {
		t.Fatalf("expected bot_configs order quantity 250, got %.2f", got.OrderQuantity)
	}
	if got.Enabled == nil || *got.Enabled {
		t.Fatalf("Enabled must be preserved as false, got %v", got.Enabled)
	}
	if got.ID != botID {
		t.Fatalf("ID = %q, want %q", got.ID, botID)
	}
}

func TestBotManagerPrepareBotStartConfigRejectsStalePrimaryConfig(t *testing.T) {
	bm := &BotManager{
		cfg:             &config.Config{},
		primaryYAMLPath: filepath.Join(t.TempDir(), "missing-config.yaml"),
	}
	_, err := bm.prepareBotStartConfig(config.BotConfig{ID: "stale-bot"})
	if err == nil || !strings.Contains(err.Error(), "YAML") {
		t.Fatalf("prepareBotStartConfig error = %v, want fail-closed primary refresh error", err)
	}
}

func TestBotManagerResolveLatestStartConfigRejectsRemovedBot(t *testing.T) {
	stale := config.BotConfig{ID: "removed-bot", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"}
	bm := &BotManager{cfg: &config.Config{
		Bots: []config.BotConfig{{ID: "current-bot", Exchange: "binance", Symbol: "ETHUSDT", MarketType: "futures"}},
	}}
	if _, err := bm.resolveLatestStartConfig(stale); err == nil {
		t.Fatal("removed Bot was accepted from the stale caller snapshot")
	}
}

func TestBotManagerResolveLatestStartConfigUsesLegacySymbolInventory(t *testing.T) {
	cfg := &config.Config{Exchanges: map[string]config.ExchangeConfig{"binance": {Testnet: true}}}
	cfg.App.CurrentExchange = "binance"
	cfg.Trading.Symbols = []config.SymbolConfig{{
		Symbol: "BTCUSDT", MarketType: "futures", OrderQuantity: 250,
	}}
	bm := &BotManager{cfg: cfg}
	stale := config.BotConfig{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures", OrderQuantity: 100}
	got, err := bm.resolveLatestStartConfig(stale)
	if err != nil {
		t.Fatalf("resolveLatestStartConfig: %v", err)
	}
	if got.OrderQuantity != 250 || !got.Testnet {
		t.Fatalf("legacy symbol inventory result = %+v, want latest settings and testnet", got)
	}
}

func TestBotManagerResolveLatestStartConfigRejectsStorageReadFailure(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.Enabled = true
	cfg.Storage.Type = "sqlite"
	cfg.Storage.Path = filepath.Join(t.TempDir(), "quantmesh.db")
	cfg.Storage.BufferSize = 1
	cfg.Storage.BatchSize = 1
	storageService, err := storage.NewStorageService(cfg, context.Background())
	if err != nil {
		t.Fatalf("NewStorageService: %v", err)
	}
	storageService.Stop()

	bm := &BotManager{cfg: cfg, storageService: storageService}
	_, err = bm.resolveLatestStartConfig(config.BotConfig{ID: "db-unavailable"})
	if err == nil || !strings.Contains(err.Error(), "讀取 Bot 配置失敗") {
		t.Fatalf("resolveLatestStartConfig error = %v, want fail-closed storage error", err)
	}
}

// TestBotManagerConcurrentAccessNoPanic 驗證並發讀寫 runtimes 不會觸發 map 競態崩潰
func TestBotManagerConcurrentAccessNoPanic(t *testing.T) {
	bm := &BotManager{
		runtimes: make(map[string]*BotRuntime),
	}

	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				botID := fmt.Sprintf("bot-%d-%d", i, j)
				bm.AddRuntime(&BotRuntime{
					BotID:  botID,
					Config: config.BotConfig{ID: botID, Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"},
				})
			}
		}(i)
	}

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 600; j++ {
				_ = bm.List()
			}
		}()
	}

	wg.Wait()
}

func TestBotManagerWarnsOnSingleLegRunning(t *testing.T) {
	eb := event.NewEventBus(64)
	sub := eb.Subscribe()
	defer eb.Unsubscribe(sub)

	cfg := &config.Config{
		BotGroups: []config.BotGroup{
			{ID: "g1", Name: "hedge-btc", BotIDs: []string{"fut-bot", "spot-bot"}},
		},
	}
	bm := &BotManager{
		cfg:             cfg,
		runtimes:        make(map[string]*BotRuntime),
		eventBus:        eb,
		groupLegAlerted: make(map[string]bool),
		groupLegTimers:  make(map[string]*time.Timer),
	}

	bm.AddRuntime(&BotRuntime{BotID: "fut-bot", Config: config.BotConfig{ID: "fut-bot", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"}})
	bm.AddRuntime(&BotRuntime{BotID: "spot-bot", Config: config.BotConfig{ID: "spot-bot", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "spot"}})

	// 从双腿运行切到单腿运行，应触发告警
	if err := bm.StopBot("spot-bot"); err != nil {
		t.Fatalf("StopBot failed: %v", err)
	}

	gotAlert := false
	timeout := time.After(2 * time.Second)
	for !gotAlert {
		select {
		case evt := <-sub:
			if evt != nil && evt.Type == event.EventTypeError {
				if groupID, ok := evt.Data["group_id"].(string); ok && groupID == "g1" {
					gotAlert = true
				}
			}
		case <-timeout:
			t.Fatalf("expected single-leg warning event, but not received")
		}
	}

	// 恢复双腿运行，应触发恢复提示
	bm.AddRuntime(&BotRuntime{BotID: "spot-bot", Config: config.BotConfig{ID: "spot-bot", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "spot"}})
	bm.checkGroupLegConsistencyForBot("spot-bot")

	gotRecovered := false
	timeout2 := time.After(2 * time.Second)
	for !gotRecovered {
		select {
		case evt := <-sub:
			if evt != nil && evt.Type == event.EventTypeRiskRecovered {
				if groupID, ok := evt.Data["group_id"].(string); ok && groupID == "g1" {
					gotRecovered = true
				}
			}
		case <-timeout2:
			t.Fatalf("expected hedge group recovered event, but not received")
		}
	}
}

func TestBotManagerAutoPausesSingleLegAfterGrace(t *testing.T) {
	eb := event.NewEventBus(64)
	sub := eb.Subscribe()
	defer eb.Unsubscribe(sub)

	cfg := &config.Config{
		BotGroups: []config.BotGroup{
			{ID: "g2", Name: "hedge-eth", BotIDs: []string{"fut-bot2", "spot-bot2"}},
		},
	}
	bm := &BotManager{
		cfg:               cfg,
		runtimes:          make(map[string]*BotRuntime),
		eventBus:          eb,
		groupLegAlerted:   make(map[string]bool),
		groupLegTimers:    make(map[string]*time.Timer),
		singleLegGraceSec: 1,
	}

	fut := &BotRuntime{BotID: "fut-bot2", Config: config.BotConfig{ID: "fut-bot2", Exchange: "binance", Symbol: "ETHUSDT", MarketType: "futures"}}
	spot := &BotRuntime{BotID: "spot-bot2", Config: config.BotConfig{ID: "spot-bot2", Exchange: "binance", Symbol: "ETHUSDT", MarketType: "spot"}}
	bm.AddRuntime(fut)
	bm.AddRuntime(spot)

	// 触发单腿运行
	if err := bm.StopBot("spot-bot2"); err != nil {
		t.Fatalf("StopBot failed: %v", err)
	}

	gotTriggered := false
	timeout := time.After(3 * time.Second)
	for !gotTriggered {
		select {
		case evt := <-sub:
			if evt != nil && evt.Type == event.EventTypeRiskTriggered {
				if groupID, ok := evt.Data["group_id"].(string); ok && groupID == "g2" {
					gotTriggered = true
				}
			}
		case <-timeout:
			t.Fatalf("expected risk_triggered event for single leg auto-pause")
		}
	}

	fut.configMu.RLock()
	paused := fut.Config.OpenPositionControl.PauseOpening
	fut.configMu.RUnlock()
	if !paused {
		t.Fatalf("expected running leg to be paused after single-leg grace timeout")
	}
}

func TestAutoResumeTimerCannotClearNewerPause(t *testing.T) {
	gate := &execution.OpeningGate{}
	bot := &BotRuntime{
		BotID: "pause-generation-bot",
		Config: config.BotConfig{OpenPositionControl: config.OpenPositionControl{
			BotRiskControl: &config.BotRiskControl{AutoResumeAfter: 4},
		}},
		Inner: &SymbolRuntime{OpeningGate: gate},
	}

	bot.PauseOpeningWithAutoResume("first_pause", 4)
	time.Sleep(900 * time.Millisecond)
	bot.PauseOpeningWithAutoResume("newer_manual_pause", 4)
	time.Sleep(3500 * time.Millisecond)

	bot.configMu.RLock()
	stillPaused := bot.Config.OpenPositionControl.PauseOpening
	bot.configMu.RUnlock()
	if !stillPaused || !gate.HasBlock("manual") {
		t.Fatal("an older auto-resume timer cleared the newer pause")
	}
	time.Sleep(800 * time.Millisecond)
	bot.configMu.RLock()
	stillPaused = bot.Config.OpenPositionControl.PauseOpening
	bot.configMu.RUnlock()
	if stillPaused || gate.HasBlock("manual") {
		t.Fatal("the newest auto-resume timer did not resume its own pause")
	}
}

// TestBotManagerIsBotEnabledInDB_StorageUnavailable 驗證存儲不可用時：無文件記錄則保守返回禁用
func TestBotManagerIsBotEnabledInDB_StorageUnavailable(t *testing.T) {
	bm := &BotManager{storageService: nil}
	enabled, reason := bm.IsBotEnabledInDB("test-bot")
	if enabled {
		t.Fatalf("storage 不可用且無文件記錄時應保守返回 enabled=false，got enabled=true")
	}
	if reason != "storage_unavailable" {
		t.Fatalf("expected reason=storage_unavailable, got %q", reason)
	}
}

// TestBotManagerIsBotEnabledInDB_StorageNil_FileFallback 驗證存儲為 nil 時從文件讀取（修復 EnableBot 寫文件後 StartBot 仍拒絕啟動）
func TestBotManagerIsBotEnabledInDB_StorageNil_FileFallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bot_states.json")
	data := `{"enabled-bot":{"enabled":true,"reason":"用戶通過 Web UI 啟用"}}`
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	bm := &BotManager{storageService: nil, botStatesFileOverride: path}
	enabled, reason := bm.IsBotEnabledInDB("enabled-bot")
	if !enabled {
		t.Fatalf("文件中有 enabled=true 時應返回 enabled=true，got enabled=false")
	}
	if reason != "from_file" {
		t.Fatalf("expected reason=from_file, got %q", reason)
	}
}

// TestBotManagerBotStateFileFallback 驗證存儲不可用時從文件讀取已停止狀態
func TestBotManagerBotStateFileFallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bot_states.json")
	// 寫入已停止的 bot 狀態
	data := `{"stopped-bot":{"enabled":false,"reason":"用戶停止"}}`
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	bm := &BotManager{storageService: nil, botStatesFileOverride: path}
	enabled, found := bm.isBotEnabledFromFile("stopped-bot")
	if !found {
		t.Fatalf("應從文件讀取到 stopped-bot 的狀態")
	}
	if enabled {
		t.Fatalf("stopped-bot 應為 enabled=false")
	}

	// 不存在的 bot 應返回 found=false
	_, found2 := bm.isBotEnabledFromFile("nonexistent")
	if found2 {
		t.Fatalf("不存在的 bot 應返回 found=false")
	}
}

// TestBotManagerGetStoppedAtFromFile 驗證從文件讀取停止時間
func TestBotManagerGetStoppedAtFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bot_states.json")
	stoppedAt := "2026-03-21T10:30:00+08:00"
	data := fmt.Sprintf(`{"stopped-bot":{"enabled":false,"updated_at":"%s","reason":"用戶停止"}}`, stoppedAt)
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	bm := &BotManager{storageService: nil, botStatesFileOverride: path}
	got, ok := bm.GetStoppedAt("stopped-bot")
	if !ok {
		t.Fatalf("應從文件讀取到 stopped-bot 的停止時間")
	}
	if got != stoppedAt {
		t.Fatalf("expected stopped_at=%q, got %q", stoppedAt, got)
	}

	// 不存在的 bot 應返回 ok=false
	_, ok2 := bm.GetStoppedAt("nonexistent")
	if ok2 {
		t.Fatalf("不存在的 bot 應返回 ok=false")
	}

	// enabled=true 的 bot 不應返回停止時間（視為從未停止過，或已重新啟用）
	data2 := `{"running-bot":{"enabled":true,"updated_at":"2026-03-21T10:00:00+08:00","reason":""}}`
	if err := os.WriteFile(path, []byte(data2), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	_, ok3 := bm.GetStoppedAt("running-bot")
	if ok3 {
		t.Fatalf("enabled=true 的 bot 不應返回停止時間")
	}
}

func TestBotManager_LastStartFailureRoundTrip(t *testing.T) {
	eb := event.NewEventBus(16)
	bm := NewBotManager(&config.Config{}, eb, nil, nil, "")
	bid := "test-bot-fail"

	bm.recordStartFailure(bid, errors.New("账戶餘額不足"))
	msg, _, ok := bm.GetLastStartFailure(bid)
	if !ok || msg != "账戶餘額不足" {
		t.Fatalf("GetLastStartFailure: ok=%v msg=%q", ok, msg)
	}

	bm.clearStartFailure(bid)
	_, _, ok2 := bm.GetLastStartFailure(bid)
	if ok2 {
		t.Fatalf("clear 後應無記錄")
	}
}

func TestPeriodicFeeLookupReleasesConfigLockAndRejectsStaleCredentials(t *testing.T) {
	cfg := &config.Config{
		Exchanges: map[string]config.ExchangeConfig{
			"binance": {APIKey: "old-key", SecretKey: "old-secret", FeeRate: 0.001},
		},
		Bots: []config.BotConfig{{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"}},
	}
	bm := NewBotManager(cfg, nil, nil, nil, "")
	fetchStarted := make(chan struct{})
	finishFetch := make(chan struct{})
	bm.feeRateFetcher = func(_ *config.Config, _, _ string) (float64, float64, error) {
		close(fetchStarted)
		<-finishFetch
		return 0.0005, 0.02, nil
	}
	refreshDone := make(chan struct{})
	go func() {
		bm.runPeriodicFeeRefresh()
		close(refreshDone)
	}()
	select {
	case <-fetchStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("periodic fee lookup did not start")
	}

	configUpdateDone := make(chan struct{})
	go func() {
		bm.registerEquityScopeConfig(cfg)
		close(configUpdateDone)
	}()
	select {
	case <-configUpdateDone:
	case <-time.After(time.Second):
		t.Fatal("account scope registration blocked behind exchange fee network request")
	}

	bm.equityConfigRefreshMu.Lock()
	rotated := bm.cfg.Exchanges["binance"]
	rotated.APIKey = "rotated-key"
	rotated.SecretKey = "rotated-secret"
	bm.cfg.Exchanges["binance"] = rotated
	bm.equityConfigRefreshMu.Unlock()
	close(finishFetch)
	select {
	case <-refreshDone:
	case <-time.After(3 * time.Second):
		t.Fatal("periodic fee refresh did not finish")
	}
	if got := bm.cfg.Exchanges["binance"].FeeRate; got != 0.001 {
		t.Fatalf("stale API response overwrote fee after credential rotation: got %v", got)
	}
}
