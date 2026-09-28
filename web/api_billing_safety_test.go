package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestStripeWebhookDoesNotAcknowledgeUnprocessedEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/billing/webhook/stripe", stripeWebhookHandler)
	request := httptest.NewRequest(http.MethodPost, "/billing/webhook/stripe", strings.NewReader(`{"type":"invoice.paid"}`))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("webhook status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}

func TestSubscriptionHandlerFailsWhenBillingServiceUnavailable(t *testing.T) {
	original := billingService
	billingService = nil
	t.Cleanup(func() { billingService = original })

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/billing/subscriptions/create", createSubscriptionHandler)
	request := httptest.NewRequest(http.MethodPost, "/billing/subscriptions/create", strings.NewReader(`{"plan":"enterprise","email":"alice@example.com"}`))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("subscription status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}
