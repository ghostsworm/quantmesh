package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"quantmesh/config"

	"github.com/gin-gonic/gin"
)

func TestParseIntParam(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		min     int
		max     int
		want    int
		wantErr bool
	}{
		{name: "valid", input: "42", min: 1, max: 100, want: 42},
		{name: "clamps low", input: "0", min: 1, max: 100, want: 1},
		{name: "clamps high", input: "200", min: 1, max: 100, want: 100},
		{name: "invalid", input: "abc", min: 1, max: 100, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseIntParam(tt.input, tt.min, tt.max)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("parseIntParam(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestSetupRoutesWithConfigRegistersCoreRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	sharedDir := t.TempDir()
	cfg := &config.Config{}
	cfg.Web.SharedDir = sharedDir
	cfg.Web.Pprof.Enabled = true
	cfg.Web.Pprof.RequireAuth = false
	cfg.Web.Pprof.AllowedIPs = []string{"*"}

	r := gin.New()
	SetupRoutesWithConfig(r, cfg)

	requests := []struct {
		name string
		path string
		want int
	}{
		{name: "version", path: "/api/version", want: http.StatusOK},
		{name: "metrics", path: "/metrics", want: http.StatusOK},
		{name: "pprof", path: "/debug/pprof/", want: http.StatusOK},
		{name: "missing api", path: "/api/not-found", want: http.StatusNotFound},
	}

	for _, tc := range requests {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			r.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("%s status = %d, want %d, body=%s", tc.path, w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestSetupRoutesWithoutConfigAndIPWhitelist(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	SetupRoutes(r)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/version", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("/api/version status = %d", w.Code)
	}

	allowedRouter := gin.New()
	allowedRouter.Use(ipWhitelistMiddleware([]string{"192.0.2.1"}))
	allowedRouter.GET("/probe", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	allowedRecorder := httptest.NewRecorder()
	allowedRouter.ServeHTTP(allowedRecorder, httptest.NewRequest(http.MethodGet, "/probe", nil))
	if allowedRecorder.Code != http.StatusOK {
		t.Fatalf("allowed whitelist status = %d", allowedRecorder.Code)
	}

	deniedRouter := gin.New()
	deniedRouter.Use(ipWhitelistMiddleware([]string{"10.0.0.1"}))
	deniedRouter.GET("/probe", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	deniedRecorder := httptest.NewRecorder()
	deniedRouter.ServeHTTP(deniedRecorder, httptest.NewRequest(http.MethodGet, "/probe", nil))
	if deniedRecorder.Code != http.StatusForbidden {
		t.Fatalf("denied whitelist status = %d", deniedRecorder.Code)
	}
}
