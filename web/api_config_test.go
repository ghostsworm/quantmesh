package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"quantmesh/config"
)

func setupTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	// 创建临时配置管理器
	tempDir, _ := os.MkdirTemp("", "config_test_*")
	testConfigPath := filepath.Join(tempDir, "test_config.yaml")
	
	// 创建测试配置（使用YAML内容）
	testConfigContent := `
app:
  current_exchange: "binance"
trading:
  symbol: "BTCUSDT"
  price_interval: 100
  order_quantity: 100
  buy_window_size: 10
  sell_window_size: 10
exchanges:
  binance:
    api_key: "test_key"
    secret_key: "test_secret"
    fee_rate: 0.0002
`

	// 保存测试配置
	os.WriteFile(testConfigPath, []byte(testConfigContent), 0644)
	
	// 加载配置以确保格式正确
	testConfig, err := config.LoadConfig(testConfigPath)
	if err != nil {
		// 如果加载失败，创建一个最小配置
		testConfig = &config.Config{}
		testConfig.App.CurrentExchange = "binance"
		testConfig.Exchanges = make(map[string]config.ExchangeConfig)
		testConfig.Exchanges["binance"] = config.ExchangeConfig{
			APIKey:    "test_key",
			SecretKey: "test_secret",
			FeeRate:   0.0002,
		}
		testConfig.Trading.Symbol = "BTCUSDT"
		testConfig.Trading.PriceInterval = 100
		testConfig.Trading.OrderQuantity = 100
		testConfig.Trading.BuyWindowSize = 10
		testConfig.Trading.SellWindowSize = 10
		testConfig.Validate()
		config.SaveConfig(testConfig, testConfigPath)
	}

	// 初始化配置管理器
	configManager := NewConfigManager(testConfigPath)
	configManager.UpdateConfig(testConfig)
	SetConfigManager(configManager)

	// 初始化备份管理器
	backupMgr := config.NewBackupManager()
	SetConfigBackupManager(backupMgr)

	// 初始化热更新器
	hotReloader := config.NewHotReloader(testConfig)
	SetConfigHotReloader(hotReloader)

	// 设置路由
	api := r.Group("/api")
	{
		api.GET("/config", getConfigHandler)
		api.GET("/config/json", getConfigJSONHandler)
		api.POST("/config/validate", validateConfigHandler)
		api.POST("/config/preview", previewConfigHandler)
		api.POST("/config/update", updateConfigHandler)
		api.GET("/config/backups", getBackupsHandler)
		api.POST("/config/restore/:backup_id", restoreBackupHandler)
		api.DELETE("/config/backup/:backup_id", deleteBackupHandler)
	}

	return r
}

// TestGetConfigJSON 测试获取配置JSON
func TestGetConfigJSON(t *testing.T) {
	router := setupTestRouter()

	req, _ := http.NewRequest("GET", "/api/config/json", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("期望状态码 %d，实际 %d", http.StatusOK, w.Code)
	}

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}

	// 验证响应包含配置字段
	if _, exists := response["app"]; !exists {
		t.Error("响应中缺少 app 字段")
	}
	if _, exists := response["trading"]; !exists {
		t.Error("响应中缺少 trading 字段")
	}
}

// TestValidateConfig 测试配置验证
func TestValidateConfig(t *testing.T) {
	router := setupTestRouter()

	validConfig := map[string]interface{}{
		"app": map[string]interface{}{
			"current_exchange": "binance",
		},
		"trading": map[string]interface{}{
			"symbol":        "BTCUSDT",
			"price_interval": 100,
			"order_quantity": 100,
			"buy_window_size": 10,
			"sell_window_size": 10,
		},
		"exchanges": map[string]interface{}{
			"binance": map[string]interface{}{
				"api_key":    "test_key",
				"secret_key": "test_secret",
				"fee_rate":   0.0002,
			},
		},
	}

	body, _ := json.Marshal(validConfig)
	req, _ := http.NewRequest("POST", "/api/config/validate", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("期望状态码 %d，实际 %d", http.StatusOK, w.Code)
	}

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}

	if valid, exists := response["valid"]; !exists || !valid.(bool) {
		t.Error("配置验证应该通过")
	}
}

// TestPreviewConfig 测试配置预览
func TestPreviewConfig(t *testing.T) {
	router := setupTestRouter()

	newConfig := map[string]interface{}{
		"app": map[string]interface{}{
			"current_exchange": "binance",
		},
		"trading": map[string]interface{}{
			"symbol":        "ETHUSDT", // 变更
			"price_interval": 50,       // 变更
			"order_quantity": 100,
			"buy_window_size": 10,
			"sell_window_size": 10,
		},
		"exchanges": map[string]interface{}{
			"binance": map[string]interface{}{
				"api_key":    "test_key",
				"secret_key": "test_secret",
				"fee_rate":   0.0002,
			},
		},
	}

	body, _ := json.Marshal(newConfig)
	req, _ := http.NewRequest("POST", "/api/config/preview", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("期望状态码 %d，实际 %d", http.StatusOK, w.Code)
	}

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}

	diff, exists := response["diff"]
	if !exists {
		t.Fatal("响应中缺少 diff 字段")
	}

	diffMap := diff.(map[string]interface{})
	changes, exists := diffMap["changes"]
	if !exists {
		t.Fatal("diff 中缺少 changes 字段")
	}

	changesArray := changes.([]interface{})
	if len(changesArray) == 0 {
		t.Error("应该检测到配置变更")
	}
}

// TestGetBackups 测试获取备份列表
func TestGetBackups(t *testing.T) {
	router := setupTestRouter()

	// 先创建一个备份
	configManager := configManager
	if configManager != nil {
		cfg, _ := configManager.GetConfig()
		if cfg != nil {
			backupMgr := configBackupMgr
			if backupMgr != nil {
				backupMgr.CreateBackup(configManager.GetConfigPath(), "测试备份")
			}
		}
	}

	req, _ := http.NewRequest("GET", "/api/config/backups", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("期望状态码 %d，实际 %d", http.StatusOK, w.Code)
	}

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}

	backups, exists := response["backups"]
	if !exists {
		t.Fatal("响应中缺少 backups 字段")
	}

	_ = backups.([]interface{}) // 验证是数组类型
}

// TestUpdateConfig 测试更新配置
func TestUpdateConfig(t *testing.T) {
	router := setupTestRouter()

	newConfig := map[string]interface{}{
		"app": map[string]interface{}{
			"current_exchange": "binance",
		},
		"trading": map[string]interface{}{
			"symbol":        "BTCUSDT",
			"price_interval": 200, // 变更
			"order_quantity": 100,
			"buy_window_size": 10,
			"sell_window_size": 10,
		},
		"exchanges": map[string]interface{}{
			"binance": map[string]interface{}{
				"api_key":    "test_key",
				"secret_key": "test_secret",
				"fee_rate":   0.0002,
			},
		},
	}

	body, _ := json.Marshal(newConfig)
	req, _ := http.NewRequest("POST", "/api/config/update", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("期望状态码 %d，实际 %d。响应: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}

	message, exists := response["message"]
	if !exists {
		t.Fatal("响应中缺少 message 字段")
	}

	if message.(string) != "配置更新成功" {
		t.Errorf("期望消息 '配置更新成功'，实际 '%s'", message)
	}

	// 验证备份ID存在
	if _, exists := response["backup_id"]; !exists {
		t.Error("响应中应该包含 backup_id")
	}
}

