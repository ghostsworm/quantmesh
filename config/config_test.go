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
	cfg.Trading.OrderQuantity = 30.0
	cfg.Trading.BuyWindowSize = 10
	cfg.Trading.MinOrderValue = 6.0

	// 初始化热更新和备份相关的默认值
	cfg.Storage.Path = "./test_data/quantmesh.db"
	cfg.Web.Port = 28888

	return cfg
}

func TestConfigValidate(t *testing.T) {
	// 测试有效配置
	cfg := createValidConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("有效配置验证失败: %v", err)
	}

	// 测试缺失交易所配置
	invalidCfg1 := createValidConfig()
	invalidCfg1.App.CurrentExchange = ""
	if err := invalidCfg1.Validate(); err == nil {
		t.Error("未指定交易所应该报错")
	}

	// 测试无效的手续费率
	invalidCfg2 := createValidConfig()
	binanceCfg := invalidCfg2.Exchanges["binance"]
	binanceCfg.FeeRate = -0.01
	invalidCfg2.Exchanges["binance"] = binanceCfg
	if err := invalidCfg2.Validate(); err == nil {
		t.Error("负数手续费率应该报错")
	}

	// 测试默认值设置
	cfgWithDefaults := createValidConfig()
	cfgWithDefaults.Timing.WebSocketReconnectDelay = 0
	if err := cfgWithDefaults.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfgWithDefaults.Timing.WebSocketReconnectDelay != 5 {
		t.Errorf("期望默认重连时间为5, 得到 %d", cfgWithDefaults.Timing.WebSocketReconnectDelay)
	}
}

func TestConfigDiff(t *testing.T) {
	oldCfg := createValidConfig()
	newCfg := createValidConfig()

	// 1. 无变更
	diff := DiffConfig(oldCfg, newCfg)
	if len(diff.Changes) != 0 {
		t.Errorf("预期无变更，得到 %d 个", len(diff.Changes))
	}

	// 2. 修改热更新项 (price_interval)
	newCfg.Trading.PriceInterval = 5.0
	diff = DiffConfig(oldCfg, newCfg)
	if len(diff.Changes) != 1 {
		t.Errorf("预期1个变更，得到 %d 个", len(diff.Changes))
	}
	if diff.RequiresRestart {
		t.Error("修改 price_interval 不应需要重启")
	}

	// 3. 修改需要重启的项 (web.port)
	newCfg.Web.Port = 9999
	diff = DiffConfig(oldCfg, newCfg)
	foundRestart := false
	for _, c := range diff.Changes {
		if c.Path == "web.port" && c.RequiresRestart {
			foundRestart = true
		}
	}
	if !foundRestart {
		t.Error("修改 web.port 应该标记为需要重启")
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
		t.Error("热更新回调未被触发")
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

	backupInfo, err := bm.CreateBackup(testConfigPath, "测试备份")
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
		t.Errorf("备份列表数量不正确: 期望1个，实际%d个", len(backups))
		// 列出所有文件以便调试
		entries, _ := os.ReadDir(backupDir)
		for _, entry := range entries {
			t.Logf("备份目录中的文件: %s (isDir: %v)", entry.Name(), entry.IsDir())
		}
	}
}
