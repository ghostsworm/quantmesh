package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestGetVersionNormalizesBlankVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalVersion := appVersion
	defer func() { appVersion = originalVersion }()
	SetVersion("  ")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/version", nil)

	getVersion(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
	if w.Body.String() != `{"version":"unknown"}` {
		t.Fatalf("expected unknown version, got %s", w.Body.String())
	}
}

func TestVersionHeaderMiddlewareExposesServerVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalVersion := appVersion
	defer func() { appVersion = originalVersion }()
	SetVersion(" 3.105.0-rc43 ")

	r := gin.New()
	r.Use(versionHeaderMiddleware())
	r.GET("/api/ping", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/ping", nil)
	r.ServeHTTP(w, req)

	if got := w.Header().Get("X-App-Version"); got != "3.105.0-rc43" {
		t.Fatalf("expected X-App-Version 3.105.0-rc43, got %q", got)
	}
	if got := w.Header().Get("X-Server-Version"); got != "3.105.0-rc43" {
		t.Fatalf("expected X-Server-Version 3.105.0-rc43, got %q", got)
	}
}
