package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"quantmesh/saas"
)

func TestSaaSInstanceCreationRequiresCompletedBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/saas/instances/create", createInstanceHandler)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/saas/instances/create", strings.NewReader(`{"plan":"enterprise"}`)))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("create status = %d, want %d; body=%s", response.Code, http.StatusServiceUnavailable, response.Body.String())
	}
}

func TestSaaSInstanceRoutesFailClosedWithoutManagerOrIdentity(t *testing.T) {
	original := instanceManagerV2
	SetInstanceManager(nil)
	t.Cleanup(func() { SetInstanceManager(original) })
	gin.SetMode(gin.TestMode)

	for _, tc := range []struct {
		path string
		code int
	}{
		{"/saas/instances", http.StatusServiceUnavailable},
		{"/saas/instances/example", http.StatusServiceUnavailable},
	} {
		router := gin.New()
		router.GET("/saas/instances", listInstancesHandler)
		router.GET("/saas/instances/:id", getInstanceHandler)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if response.Code != tc.code {
			t.Errorf("GET %s status = %d, want %d", tc.path, response.Code, tc.code)
		}
	}

	SetInstanceManager(saas.NewInstanceManagerV2(saas.NewInstanceManager(nil), nil))
	router := gin.New()
	router.GET("/saas/instances", listInstancesHandler)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/saas/instances", nil)
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("list without authenticated identity status = %d, want %d; body=%s", response.Code, http.StatusUnauthorized, response.Body.String())
	}
}

func TestSaaSGlobalMetricsRequireAdminSession(t *testing.T) {
	original := instanceManagerV2
	SetInstanceManager(saas.NewInstanceManagerV2(saas.NewInstanceManager(nil), nil))
	t.Cleanup(func() { SetInstanceManager(original) })
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/saas/metrics", func(c *gin.Context) {
		c.Set("session", &Session{Username: "alice", Role: "user"})
		getAllInstancesMetricsHandler(c)
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/saas/metrics", nil))
	if response.Code != http.StatusForbidden {
		t.Fatalf("non-admin metrics status = %d, want %d", response.Code, http.StatusForbidden)
	}
}
