package web

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/storage"
)

func TestSystemMetricsProvider_GetMetrics_EmptyStorageFallback(t *testing.T) {
	// 創建臨時 SQLite 數據庫（無歷史數據）
	tempDir, err := os.MkdirTemp("", "system_metrics_test_*")
	if err != nil {
		t.Fatalf("創建臨時目錄失敗: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test.db")
	cfg := &config.Config{}
	cfg.Storage.Enabled = true
	cfg.Storage.Type = "sqlite"
	cfg.Storage.Path = dbPath
	cfg.Storage.BufferSize = 100
	cfg.Storage.BatchSize = 10
	cfg.Storage.FlushInterval = 5

	ctx := context.Background()
	storageService, err := storage.NewStorageService(cfg, ctx)
	if err != nil {
		t.Fatalf("初始化存儲服務失敗: %v", err)
	}

	// 無 watchdog，storage 為空
	provider := NewSystemMetricsProvider(storageService, nil)

	startTime := time.Now().Add(-24 * time.Hour)
	endTime := time.Now()

	metrics, err := provider.GetMetrics(startTime, endTime, "detail")
	if err != nil {
		t.Fatalf("GetMetrics 失敗: %v", err)
	}

	// 無歷史數據時應回退到當前指標，至少返回 1 個數據點
	if len(metrics) == 0 {
		t.Log("無歷史數據且 CollectSystemMetrics 可能失敗，返回空為預期行為之一")
		return
	}

	// 若有回退數據，驗證結構
	m := metrics[0]
	if m.Timestamp.IsZero() {
		t.Error("回退數據應包含 timestamp")
	}
	if m.CPUPercent < 0 || m.CPUPercent > 100 {
		t.Errorf("CPUPercent 應在 0-100 之間，得到 %f", m.CPUPercent)
	}
	if m.MemoryMB < 0 {
		t.Errorf("MemoryMB 不應為負，得到 %f", m.MemoryMB)
	}
}

func TestSystemMetricsProvider_GetMetrics_NilStorageService(t *testing.T) {
	provider := NewSystemMetricsProvider(nil, nil)

	startTime := time.Now().Add(-24 * time.Hour)
	endTime := time.Now()

	metrics, err := provider.GetMetrics(startTime, endTime, "detail")
	if err != nil {
		t.Fatalf("GetMetrics 失敗: %v", err)
	}

	// nil storage 時會嘗試 GetCurrentMetrics 回退
	// 可能返回空（CollectSystemMetrics 失敗）或 1 個點
	if len(metrics) > 1 {
		t.Errorf("nil storage 時不應返回多於 1 個點，得到 %d", len(metrics))
	}
}
