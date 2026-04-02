package config

import (
	"os"
	"path/filepath"
	"testing"
)

func createValidConfig() *Config {
	cfg := &Config{}
	cfg.App.CurrentExchange = "binance"
	cfg.Exchanges = make(map[string]ExchangeConfig)
	cfg.Exchanges["binance"] = ExchangeConfig{
		APIKey:    "test_key",
		SecretKey: "test_secret",
		FeeRate:   0.0002,
	}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.PriceInterval = 2.0
	cfg.Trading.OrderQuantity = 30.0
	cfg.Trading.BuyWindowSize = 10
	cfg.Trading.MinOrderValue = 6.0

	// 初始化热更新和备份相关的默认值
	cfg.Storage.Type = "sqlite"
	cfg.Storage.Path = "./test_data/quantmesh.db"
	cfg.Web.Port = 28888

	return cfg
}

func TestConfigValidate(t *testing.T) {
	// 测試有效配置
	cfg := createValidConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("有效配置驗证失败: %v", err)
	}

	// 测試缺失交易所配置
	invalidCfg1 := createValidConfig()
	invalidCfg1.App.CurrentExchange = ""
	if err := invalidCfg1.Validate(); err == nil {
		t.Error("未指定交易所应該报錯")
	}

	// 测試無效的手续费率
	invalidCfg2 := createValidConfig()
	binanceCfg := invalidCfg2.Exchanges["binance"]
	binanceCfg.FeeRate = -0.01
	invalidCfg2.Exchanges["binance"] = binanceCfg
	if err := invalidCfg2.Validate(); err == nil {
		t.Error("负數手续费率应該报錯")
	}

	// 测試默认值設置
	cfgWithDefaults := createValidConfig()
	cfgWithDefaults.Timing.WebSocketReconnectDelay = 0
	if err := cfgWithDefaults.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfgWithDefaults.Timing.WebSocketReconnectDelay != 5 {
		t.Errorf("期望默认重连時间為5, 得到 %d", cfgWithDefaults.Timing.WebSocketReconnectDelay)
	}
}

func TestStorageDefaults(t *testing.T) {
	// SQLite 且 Path 為空時應設置默認路徑
	cfg1 := createValidConfig()
	cfg1.Storage.Type = "sqlite"
	cfg1.Storage.Path = ""
	if err := cfg1.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg1.Storage.Path != "./data/quantmesh.db" {
		t.Errorf("SQLite Path 空時應默認 ./data/quantmesh.db，得到 %s", cfg1.Storage.Path)
	}

	// MySQL 且 Path 為空時應保持空（使用 database.dsn）
	cfg2 := createValidConfig()
	cfg2.Storage.Type = "mysql"
	cfg2.Storage.Path = ""
	if err := cfg2.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg2.Storage.Path != "" {
		t.Errorf("MySQL Path 空時應保持空，得到 %s", cfg2.Storage.Path)
	}
}

func TestLogCleanupDefaults(t *testing.T) {
	// 未配置 log_cleanup 時應設置默認值
	cfg := createValidConfig()
	cfg.System.LogCleanup = LogCleanupConfig{} // 零值
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	lc := &cfg.System.LogCleanup
	if !lc.Enabled {
		t.Error("期望 LogCleanup.Enabled 默認為 true")
	}
	if lc.Schedule != "02:00" {
		t.Errorf("期望 LogCleanup.Schedule 默認為 02:00, 得到 %s", lc.Schedule)
	}
	if lc.RetentionDays != 7 {
		t.Errorf("期望 LogCleanup.RetentionDays 默認為 7, 得到 %d", lc.RetentionDays)
	}
	if len(lc.LevelsToClean) != 2 || lc.LevelsToClean[0] != "INFO" || lc.LevelsToClean[1] != "WARN" {
		t.Errorf("期望 LogCleanup.LevelsToClean 默認為 [INFO WARN], 得到 %v", lc.LevelsToClean)
	}

	// 部分配置時應補全其餘默認值
	cfg2 := createValidConfig()
	cfg2.System.LogCleanup = LogCleanupConfig{Enabled: false, Schedule: "03:00"}
	if err := cfg2.Validate(); err != nil {
		t.Fatal(err)
	}
	lc2 := &cfg2.System.LogCleanup
	if lc2.Schedule != "03:00" {
		t.Errorf("期望保留用戶設置的 Schedule 03:00, 得到 %s", lc2.Schedule)
	}
	if lc2.RetentionDays != 7 {
		t.Errorf("期望 RetentionDays 補全為 7, 得到 %d", lc2.RetentionDays)
	}
}

func TestConfigDiff(t *testing.T) {
	oldCfg := createValidConfig()
	newCfg := createValidConfig()

	// 1. 無变更
	diff := DiffConfig(oldCfg, newCfg)
	if len(diff.Changes) != 0 {
		t.Errorf("預期無变更，得到 %d 個", len(diff.Changes))
	}

	// 2. 修改热更新项 (price_interval)
	newCfg.Trading.PriceInterval = 5.0
	diff = DiffConfig(oldCfg, newCfg)
	if len(diff.Changes) != 1 {
		t.Errorf("預期1個变更，得到 %d 個", len(diff.Changes))
	}
	if diff.RequiresRestart {
		t.Error("修改 price_interval 不应需要重啟")
	}

	// 3. 修改需要重啟的项 (web.port)
	newCfg.Web.Port = 9999
	diff = DiffConfig(oldCfg, newCfg)
	foundRestart := false
	for _, c := range diff.Changes {
		if c.Path == "web.port" && c.RequiresRestart {
			foundRestart = true
		}
	}
	if !foundRestart {
		t.Error("修改 web.port 应該標記為需要重啟")
	}
}

func TestHotReloader(t *testing.T) {
	initialCfg := createValidConfig()
	reloader := NewHotReloader(initialCfg)

	callbackCalled := false
	reloader.RegisterCallback(func(old, new *Config, changes []ConfigChange) error {
		callbackCalled = true
		return nil
	})

	newCfg := createValidConfig()
	newCfg.Trading.PriceInterval = 10.0

	_, err := reloader.UpdateConfig(newCfg)
	if err != nil {
		t.Fatalf("更新配置失败: %v", err)
	}

	if !callbackCalled {
		t.Error("热更新回呼未被触发")
	}

	if reloader.GetCurrentConfig().Trading.PriceInterval != 10.0 {
		t.Errorf("配置未更新: %.2f", reloader.GetCurrentConfig().Trading.PriceInterval)
	}
}

func TestConfigBackup(t *testing.T) {
	tempDir := t.TempDir()
	backupDir := filepath.Join(tempDir, "backups")

	bm := &BackupManager{
		backupDir:  backupDir,
		maxBackups: 5,
	}

	testConfigPath := filepath.Join(tempDir, "test_config.yaml")
	testConfigContent := "app:\n  current_exchange: \"binance\"\n"
	err := os.WriteFile(testConfigPath, []byte(testConfigContent), 0644)
	if err != nil {
		t.Fatal(err)
	}

	backupInfo, err := bm.CreateBackup(testConfigPath, "测試备份")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(backupInfo.FilePath); os.IsNotExist(err) {
		t.Fatal("备份文件不存在")
	}

	backups, err := bm.ListBackups()
	if err != nil {
		t.Fatalf("列出备份失败: %v", err)
	}

	if len(backups) != 1 {
		t.Errorf("备份列表數量不正确: 期望1個，實際%d個", len(backups))
		// 列出所有文件以便調試
		entries, _ := os.ReadDir(backupDir)
		for _, entry := range entries {
			t.Logf("备份目錄中的文件: %s (isDir: %v)", entry.Name(), entry.IsDir())
		}
	}
}

// TestMigrateToBots_SymbolKeyDeduplication 驗證 MigrateToBots 按 symbolKey 去重，避免同交易所同幣同市場類型重複
func TestMigrateToBots_SymbolKeyDeduplication(t *testing.T) {
	cfg := &Config{}
	cfg.App.CurrentExchange = "binance"
	cfg.Exchanges = map[string]ExchangeConfig{
		"binance": {APIKey: "k", SecretKey: "s", FeeRate: 0.0002},
	}

	// Bots 已有 binance:BTCUSDT:futures (id=existing-bot)
	cfg.Bots = []BotConfig{
		{
			ID:         "existing-bot",
			Exchange:   "binance",
			Symbol:     "BTCUSDT",
			MarketType: "futures",
		},
	}

	// Symbols 中有同 exchange+symbol+marketType 但不同 ID 的項
	cfg.Trading.Symbols = []SymbolConfig{
		{ID: "new-bot-different-id", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"},
	}

	cfg.MigrateToBots()

	// 應只保留 1 個 bot（按 symbolKey 去重，不應新增重複項）
	if len(cfg.Bots) != 1 {
		t.Errorf("同 symbolKey 應去重：期望 1 個 bot，實際 %d 個", len(cfg.Bots))
	}
	if cfg.Bots[0].ID != "existing-bot" {
		t.Errorf("應保留原有 bot，期望 id=existing-bot，實際 id=%s", cfg.Bots[0].ID)
	}

	// 不同 marketType 的應可並存
	cfg.Trading.Symbols = append(cfg.Trading.Symbols, SymbolConfig{
		ID: "spot-bot", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "spot",
	})
	cfg.MigrateToBots()
	if len(cfg.Bots) != 2 {
		t.Errorf("不同 marketType 應並存：期望 2 個 bot，實際 %d 個", len(cfg.Bots))
	}
}

func TestResolveGlobalAI_FlatOnly(t *testing.T) {
	cfg := createValidConfig()
	cfg.AI.Provider = "gemini"
	cfg.AI.GeminiAPIKey = "gk-test"
	r := ResolveGlobalAI(cfg)
	if r.Provider != "gemini" || r.APIKey != "gk-test" || r.Source != "flat" {
		t.Fatalf("flat: %+v", r)
	}
}

func TestResolveGlobalAI_DefaultUpstream(t *testing.T) {
	cfg := createValidConfig()
	cfg.AI.Upstreams = map[string]AIUpstreamProfile{
		"p1": {Provider: "openai", Model: "gpt-4o", APIKey: "sk-xxx", BaseURL: "https://api.openai.com/v1"},
	}
	cfg.AI.DefaultUpstream = "p1"
	r := ResolveGlobalAI(cfg)
	if r.Provider != "openai" || r.APIKey != "sk-xxx" || r.Source != "profile:p1" {
		t.Fatalf("default_upstream: %+v", r)
	}
}

func TestResolveGlobalAI_UpstreamsNoDefaultUsesFlat(t *testing.T) {
	cfg := createValidConfig()
	cfg.AI.Upstreams = map[string]AIUpstreamProfile{"x": {Provider: "openai", APIKey: "k"}}
	cfg.AI.GeminiAPIKey = "flat-g"
	r := ResolveGlobalAI(cfg)
	if r.APIKey != "flat-g" || r.Source != "flat" {
		t.Fatalf("expected flat when default_upstream empty: %+v", r)
	}
}

func TestValidateAIUpstreamRefs_InvalidDefault(t *testing.T) {
	cfg := createValidConfig()
	cfg.AI.DefaultUpstream = "missing"
	cfg.AI.Upstreams = map[string]AIUpstreamProfile{"other": {APIKey: "a"}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for invalid default_upstream")
	}
}

func TestApplyNewsMonitorAIFromUpstreamRef(t *testing.T) {
	cfg := createValidConfig()
	cfg.AI.Upstreams = map[string]AIUpstreamProfile{
		"nm": {Provider: "openai", Model: "gpt-4o-mini", APIKey: "sk-nm", BaseURL: ""},
	}
	cfg.NewsMonitor.AIProvider.UpstreamRef = "nm"
	if err := ApplyNewsMonitorAIFromUpstreamRef(cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.NewsMonitor.AIProvider.Provider != "openai" || cfg.NewsMonitor.AIProvider.APIKey != "sk-nm" {
		t.Fatalf("news merge: %+v", cfg.NewsMonitor.AIProvider)
	}
}
