package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"quantmesh/config"
)

func setupBasisConfigTestRouter() *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	cfg := &config.Config{}
	cfg.BasisMonitor.Enabled = false
	cfg.BasisMonitor.IntervalMinutes = 1
	cfg.BasisMonitor.Symbols = []string{"BTCUSDT", "ETHUSDT"}
	SetGlobalConfig(cfg)
	api := r.Group("/api")
	{
		api.GET("/basis/config", getBasisConfig)
	}
	return r
}

func TestGetBasisConfig_NoController(t *testing.T) {
	SetBasisMonitorController(nil)
	router := setupBasisConfigTestRouter()

	req, _ := http.NewRequest("GET", "/api/basis/config", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("期望狀態碼 %d，實際 %d", http.StatusOK, w.Code)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("解析響應失敗: %v", err)
	}
	if _, ok := response["config"]; !ok {
		t.Error("響應中缺少 config 字段")
	}
	if _, ok := response["source"]; !ok {
		t.Error("響應中缺少 source 字段")
	}
}

func TestPutBasisConfig_NoProvider(t *testing.T) {
	// 臨時清空 systemSettingsProvider 以模擬未初始化
	orig := systemSettingsProvider
	systemSettingsProvider = nil
	t.Cleanup(func() { systemSettingsProvider = orig })

	r := gin.New()
	r.Use(gin.Recovery())
	api := r.Group("/api")
	api.PUT("/basis/config", putBasisConfig)

	body, _ := json.Marshal(map[string]interface{}{"enabled": true})
	req, _ := http.NewRequest("PUT", "/api/basis/config", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("當 provider 為 nil 時期望 503，實際 %d", w.Code)
	}
}
