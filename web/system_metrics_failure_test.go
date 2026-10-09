package web

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"quantmesh/monitor"
)

func TestSystemMetricsCollectionFailureIsNotHealthyZero(t *testing.T) {
	cause := errors.New("fixture CPU reader unavailable")
	for _, mode := range []string{"current", "empty_history"} {
		t.Run(mode, func(t *testing.T) {
			provider := NewSystemMetricsProvider(nil, nil)
			provider.collectMetrics = func() (*monitor.SystemMetrics, error) { return nil, cause }
			if mode == "current" {
				metrics, err := provider.GetCurrentMetrics()
				if metrics != nil || !errors.Is(err, cause) {
					t.Fatalf("collection failure became current data: metrics=%+v err=%v", metrics, err)
				}
			} else {
				metrics, err := provider.GetMetrics(time.Now().Add(-time.Hour), time.Now(), "detail")
				if len(metrics) != 0 || !errors.Is(err, cause) {
					t.Fatalf("collection failure became successful history: metrics=%+v err=%v", metrics, err)
				}
			}
		})
	}
}

func TestCurrentSystemMetricsHTTPRejectsUnavailableEvidence(t *testing.T) {
	resetProviderInfraGlobals(t)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/metrics/current", getCurrentSystemMetrics)
	router.GET("/metrics", getSystemMetrics)
	for _, mode := range []string{"missing_provider", "collection_error", "nil_sample"} {
		t.Run(mode, func(t *testing.T) {
			SetSystemMetricsProvider(nil)
			if mode != "missing_provider" {
				provider := NewSystemMetricsProvider(nil, nil)
				provider.collectMetrics = func() (*monitor.SystemMetrics, error) {
					if mode == "collection_error" {
						return nil, errors.New("fixture unavailable")
					}
					return nil, nil
				}
				SetSystemMetricsProvider(provider)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics/current", nil))
			if response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), "cpu_percent") {
				t.Fatalf("unavailable evidence became healthy HTTP response: %d %s", response.Code, response.Body.String())
			}
			if mode == "collection_error" {
				response = httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
				if response.Code != http.StatusInternalServerError {
					t.Fatalf("empty-history collection failure swallowed: %d", response.Code)
				}
			}
		})
	}
	provider := NewSystemMetricsProvider(nil, nil)
	provider.collectMetrics = func() (*monitor.SystemMetrics, error) {
		return &monitor.SystemMetrics{Timestamp: time.Now(), CPUPercent: 0, ProcessID: 42}, nil
	}
	SetSystemMetricsProvider(provider)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics/current", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"process_id":42`) || !strings.Contains(response.Body.String(), `"cpu_percent":0`) {
		t.Fatalf("healthy zero lost in actual HTTP route: %d %s", response.Code, response.Body.String())
	}
}

func TestSystemMetricsGenuineZeroRemainsValid(t *testing.T) {
	provider := NewSystemMetricsProvider(nil, nil)
	timestamp := time.Now()
	provider.collectMetrics = func() (*monitor.SystemMetrics, error) {
		return &monitor.SystemMetrics{Timestamp: timestamp, CPUPercent: 0, ProcessID: 42}, nil
	}
	metrics, err := provider.GetCurrentMetrics()
	if err != nil || metrics == nil || metrics.CPUPercent != 0 || metrics.ProcessID != 42 || !metrics.Timestamp.Equal(timestamp) {
		t.Fatalf("genuine zero rejected or timestamp invented: %+v %v", metrics, err)
	}
}
