package web

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuditLocalDevModeMustRejectRemoteUnauthenticatedRequest(t *testing.T) {
	old := storageProvider
	SetStorageProvider(localDevSettingsProvider{})
	t.Cleanup(func() { SetStorageProvider(old) })
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/audit-protected", authMiddleware(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	req := httptest.NewRequest(http.MethodGet, "http://example.test/audit-protected", nil)
	req.RemoteAddr = "203.0.113.10:1234"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code == http.StatusNoContent {
		t.Fatal("non-loopback request without credentials passed local-dev authentication bypass")
	}
}
