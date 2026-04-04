package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"quantmesh/storage"
)

func TestListKlineFilesWithoutCollector(t *testing.T) {
	gin.SetMode(gin.TestMode)

	dbPath := "./test_api_kline_files.db"
	defer os.Remove(dbPath)
	defer os.Remove(dbPath + "-shm")
	defer os.Remove(dbPath + "-wal")

	st, err := storage.NewSQLStorage(dbPath)
	if err != nil {
		t.Fatalf("创建存储失败: %v", err)
	}
	defer st.Close()

	origStorage := storageServiceProvider
	SetStorageServiceProvider(&testStorageProvider{st: st})
	t.Cleanup(func() { SetStorageServiceProvider(origStorage) })

	origKlineCollector := klineCollector
	klineCollector = nil
	t.Cleanup(func() { klineCollector = origKlineCollector })

	// listKlineFilesHandler 使用 monitor.DefaultKlineDataDir 即 "./data/kline"
	// 我们的测试文件在 ./test_kline_data_dir，所以默认扫描会得到空列表（或不存在目录返回空）
	// 为了验证 fallback 逻辑，我们只检查：当 storage 可用、klineCollector 为 nil 时，不返回 503
	// 若 ./data/kline 不存在，ListKlineFilesFromDir 返回 []，应返回 200 + success + files: []
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/kline-files", nil)

	listKlineFilesHandler(c)

	if w.Code == http.StatusServiceUnavailable {
		t.Fatalf("当 storage 可用时，不应返回 503，got %d body=%s", w.Code, w.Body.String())
	}
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestListKlineFilesWithoutCollectorAndStorage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origStorage := storageServiceProvider
	SetStorageServiceProvider(nil)
	t.Cleanup(func() { SetStorageServiceProvider(origStorage) })

	origKlineCollector := klineCollector
	klineCollector = nil
	t.Cleanup(func() { klineCollector = origKlineCollector })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/kline-files", nil)

	listKlineFilesHandler(c)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("当 storage 为 nil 且 klineCollector 为 nil 时，应返回 503，got %d body=%s", w.Code, w.Body.String())
	}
}
