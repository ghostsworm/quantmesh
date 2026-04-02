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

	// 創建临時配置管理器
	tempDir, _ := os.MkdirTemp("", "config_test_*")
	testConfigPath := filepath.Join(tempDir, "test_config.yaml")

	// 創建测試配置（使用YAML内容）
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

	// 保存测試配置
	os.WriteFile(testConfigPath, []byte(testConfigContent), 0644)

	// 加載配置以确保格式正确
	testConfig, err := config.LoadConfig(testConfigPath)
	if err != nil {
		// 如果加載失败，創建一個最小配置
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
	fileConfigMgr := NewFileConfigManager(testConfigPath)
	fileConfigMgr.UpdateConfig(testConfig)
	SetFileConfigManager(fileConfigMgr)

	// 初始化备份管理器
	backupMgr := config.NewBackupManager(testConfigPath)
	SetConfigBackupManager(backupMgr)

	// 初始化热更新器
	hotReloader := config.NewHotReloader(testConfig)
	SetConfigHotReloader(hotReloader)

	// 設置路由
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
		api.GET("/config/security/status", getConfigSecurityStatusHandler)
		api.POST("/config/security/generate-key", postConfigSecurityGenerateKeyHandler)
	}

	return r
}

// TestGetConfigJSON 测試獲取配置JSON
func TestGetConfigJSON(t *testing.T) {
	router := setupTestRouter()

	req, _ := http.NewRequest("GET", "/api/config/json", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("期望状態碼 %d，實際 %d", http.StatusOK, w.Code)
	}

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}

	// 驗证响应包含配置字段
	if _, exists := response["app"]; !exists {
		t.Error("响应中缺少 app 字段")
	}
	if _, exists := response["trading"]; !exists {
		t.Error("响应中缺少 trading 字段")
	}
}

// TestValidateConfig 测試配置驗证
func TestValidateConfig(t *testing.T) {
	router := setupTestRouter()

	validConfig := map[string]interface{}{
		"app": map[string]interface{}{
			"current_exchange": "binance",
		},
		"trading": map[string]interface{}{
			"symbol":           "BTCUSDT",
			"price_interval":   100,
			"order_quantity":   100,
			"buy_window_size":  10,
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
		t.Errorf("期望状態碼 %d，實際 %d", http.StatusOK, w.Code)
	}

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}

	if valid, exists := response["valid"]; !exists || !valid.(bool) {
		t.Error("配置驗证应該通過")
	}
}

// TestPreviewConfig 测試配置預览
func TestPreviewConfig(t *testing.T) {
	router := setupTestRouter()

	newConfig := map[string]interface{}{
		"app": map[string]interface{}{
			"current_exchange": "binance",
		},
		"trading": map[string]interface{}{
			"symbol":           "ETHUSDT", // 变更
			"price_interval":   50,        // 变更
			"order_quantity":   100,
			"buy_window_size":  10,
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
		t.Errorf("期望状態碼 %d，實際 %d", http.StatusOK, w.Code)
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
		t.Error("应該检测到配置变更")
	}
}

// TestGetBackups 测試獲取备份列表
func TestGetBackups(t *testing.T) {
	router := setupTestRouter()

	// 先創建一個备份
	configManager := configManager
	if configManager != nil {
		cfg, _ := fileConfigManager.GetConfig()
		if cfg != nil {
			backupMgr := configBackupMgr
			if backupMgr != nil {
				backupMgr.CreateBackup(fileConfigManager.GetConfigPath(), "测試备份")
			}
		}
	}

	req, _ := http.NewRequest("GET", "/api/config/backups", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("期望状態碼 %d，實際 %d", http.StatusOK, w.Code)
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

	// 允許 nil（無備份時）或 []interface{}
	if backups != nil {
		if _, ok := backups.([]interface{}); !ok {
			t.Fatal("backups 應為數組類型")
		}
	}
}

// TestUpdateConfig 测試更新配置
func TestUpdateConfig(t *testing.T) {
	router := setupTestRouter()

	newConfig := map[string]interface{}{
		"app": map[string]interface{}{
			"current_exchange": "binance",
		},
		"trading": map[string]interface{}{
			"symbol":           "BTCUSDT",
			"price_interval":   200, // 变更
			"order_quantity":   100,
			"buy_window_size":  10,
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
		t.Errorf("期望状態碼 %d，實際 %d。响应: %s", http.StatusOK, w.Code, w.Body.String())
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
		t.Errorf("期望消息 '配置更新成功'，實際 '%s'", message)
	}

	// 驗证备份ID存在
	if _, exists := response["backup_id"]; !exists {
		t.Error("响应中应該包含 backup_id")
	}
}

func TestNormalizeNumericStrings(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected interface{}
	}{
		{
			name:     "string float to float64",
			input:    "70.000000",
			expected: 70.0,
		},
		{
			name:     "string int to int64",
			input:    "42",
			expected: int64(42),
		},
		{
			name:     "non-numeric string unchanged",
			input:    "BTCUSDT",
			expected: "BTCUSDT",
		},
		{
			name:     "empty string unchanged",
			input:    "",
			expected: "",
		},
		{
			name:     "actual number unchanged",
			input:    3.14,
			expected: 3.14,
		},
		{
			name:     "bool unchanged",
			input:    true,
			expected: true,
		},
		{
			name:     "nil unchanged",
			input:    nil,
			expected: nil,
		},
		{
			name: "nested map with string numbers",
			input: map[string]interface{}{
				"price_interval": "70.000000",
				"order_quantity": "700",
				"symbol":         "BTCUSDT",
				"nested": map[string]interface{}{
					"value": "3.14",
				},
			},
			expected: map[string]interface{}{
				"price_interval": 70.0,
				"order_quantity": int64(700),
				"symbol":         "BTCUSDT",
				"nested": map[string]interface{}{
					"value": 3.14,
				},
			},
		},
		{
			name:     "slice with string numbers",
			input:    []interface{}{"1.5", "hello", "42"},
			expected: []interface{}{1.5, "hello", int64(42)},
		},
		{
			name:     "negative string number",
			input:    "-0.05",
			expected: -0.05,
		},
		{
			name:     "scientific notation",
			input:    "1e-5",
			expected: 1e-5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeNumericStrings(tt.input)

			resultJSON, _ := json.Marshal(result)
			expectedJSON, _ := json.Marshal(tt.expected)
			if string(resultJSON) != string(expectedJSON) {
				t.Errorf("normalizeNumericStrings(%v) = %v (%T), want %v (%T)",
					tt.input, result, result, tt.expected, tt.expected)
			}
		})
	}
}

func TestUpdateConfigWithStringNumbers(t *testing.T) {
	router := setupTestRouter()

	configWithStrings := map[string]interface{}{
		"app": map[string]interface{}{
			"current_exchange": "binance",
		},
		"trading": map[string]interface{}{
			"symbol":           "BTCUSDT",
			"price_interval":   "70.000000",
			"order_quantity":    "700",
			"buy_window_size":  10,
			"sell_window_size": 10,
		},
		"exchanges": map[string]interface{}{
			"binance": map[string]interface{}{
				"api_key":    "test_key",
				"secret_key": "test_secret",
				"fee_rate":   "0.0002",
			},
		},
	}

	body, _ := json.Marshal(configWithStrings)
	req, _ := http.NewRequest("POST", "/api/config/update", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("期望状態碼 %d，實際 %d。响应: %s", http.StatusOK, w.Code, w.Body.String())
	}
}

func TestGetConfigSecurityStatus(t *testing.T) {
	t.Setenv(config.MasterKeyEnvVar, "")
	gin.SetMode(gin.TestMode)
	tempDir := t.TempDir()
	testConfigPath := filepath.Join(tempDir, "test_config.yaml")
	// 指定唯一路徑，避免與開發機 cwd 下 ./data/master.key 衝突
	mkPath := filepath.Join(tempDir, "nope", "not_created_yet.key")
	testConfigContent := `app:
  current_exchange: "binance"
security:
  encryption_enabled: false
  master_key_path: "` + mkPath + `"
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
	if err := os.WriteFile(testConfigPath, []byte(testConfigContent), 0644); err != nil {
		t.Fatal(err)
	}
	testConfig, err := config.LoadConfig(testConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	fileConfigMgr := NewFileConfigManager(testConfigPath)
	SetFileConfigManager(fileConfigMgr)
	SetConfigBackupManager(config.NewBackupManager(testConfigPath))
	SetConfigHotReloader(config.NewHotReloader(testConfig))

	r := gin.New()
	r.GET("/api/config/security/status", getConfigSecurityStatusHandler)

	req, _ := http.NewRequest("GET", "/api/config/security/status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 %d，實際 %d: %s", http.StatusOK, w.Code, w.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["encryption_enabled"] != false {
		t.Errorf("encryption_enabled 期望 false，得到 %v", body["encryption_enabled"])
	}
	if body["master_key_path"] != mkPath {
		t.Errorf("master_key_path 期望 %q，得到 %v", mkPath, body["master_key_path"])
	}
	if body["master_key_exists"] != false {
		t.Errorf("master_key_exists 期望 false，得到 %v", body["master_key_exists"])
	}
}

func TestPostConfigSecurityGenerateKey(t *testing.T) {
	t.Setenv(config.MasterKeyEnvVar, "")
	gin.SetMode(gin.TestMode)
	tempDir := t.TempDir()
	testConfigPath := filepath.Join(tempDir, "test_config.yaml")
	mkPath := filepath.Join(tempDir, "data", "master.key")
	testConfigContent := `app:
  current_exchange: "binance"
security:
  encryption_enabled: true
  master_key_path: "` + mkPath + `"
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
	if err := os.WriteFile(testConfigPath, []byte(testConfigContent), 0644); err != nil {
		t.Fatal(err)
	}
	testConfig, err := config.LoadConfig(testConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	fileConfigMgr := NewFileConfigManager(testConfigPath)
	// 勿調用 UpdateConfig：SaveConfig 會覆寫 YAML 並丟失不在 config.Config 中的 security 段
	SetFileConfigManager(fileConfigMgr)
	SetConfigBackupManager(config.NewBackupManager(testConfigPath))
	SetConfigHotReloader(config.NewHotReloader(testConfig))

	r := gin.New()
	r.GET("/api/config/security/status", getConfigSecurityStatusHandler)
	r.POST("/api/config/security/generate-key", postConfigSecurityGenerateKeyHandler)

	reqGen, _ := http.NewRequest("POST", "/api/config/security/generate-key", nil)
	wGen := httptest.NewRecorder()
	r.ServeHTTP(wGen, reqGen)
	if wGen.Code != http.StatusOK {
		t.Fatalf("生成主密鑰期望 %d，實際 %d: %s", http.StatusOK, wGen.Code, wGen.Body.String())
	}
	var genBody map[string]interface{}
	if err := json.Unmarshal(wGen.Body.Bytes(), &genBody); err != nil {
		t.Fatal(err)
	}
	if genBody["master_key_path"] != mkPath {
		t.Errorf("master_key_path 期望 %q，得到 %v", mkPath, genBody["master_key_path"])
	}

	reqGen2, _ := http.NewRequest("POST", "/api/config/security/generate-key", nil)
	wGen2 := httptest.NewRecorder()
	r.ServeHTTP(wGen2, reqGen2)
	if wGen2.Code != http.StatusBadRequest {
		t.Errorf("重複生成期望 %d，實際 %d: %s", http.StatusBadRequest, wGen2.Code, wGen2.Body.String())
	}

	reqSt, _ := http.NewRequest("GET", "/api/config/security/status", nil)
	wSt := httptest.NewRecorder()
	r.ServeHTTP(wSt, reqSt)
	if wSt.Code != http.StatusOK {
		t.Fatalf("status 期望 %d，實際 %d", http.StatusOK, wSt.Code)
	}
	var stBody map[string]interface{}
	_ = json.Unmarshal(wSt.Body.Bytes(), &stBody)
	if stBody["master_key_exists"] != true {
		t.Errorf("生成後 master_key_exists 期望 true，得到 %v", stBody["master_key_exists"])
	}
}
