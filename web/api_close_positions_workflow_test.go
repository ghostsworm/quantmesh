package web

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"quantmesh/config"
	"quantmesh/position"
	"strings"
	"testing"
)

type managedCloseHTTPBot struct {
	BotExtended
	canceled bool
	result   *position.ClosePositionRecord
	err      error
}

func (b *managedCloseHTTPBot) ClosePositions(ctx context.Context, _ config.ClosePositionConfig) (*position.ClosePositionRecord, error) {
	b.canceled = ctx.Err() != nil
	return b.result, b.err
}

func TestManagedCloseHTTPRetainsUnknownRecordAndRequestCancellation(t *testing.T) {
	old := botExtendedProvider
	t.Cleanup(func() { botExtendedProvider = old })
	bot := &managedCloseHTTPBot{result: &position.ClosePositionRecord{RecordID: "close-stable", OrderID: 42, Status: position.CloseStatusUnknown}, err: errors.New("query unavailable")}
	botExtendedProvider = protectiveResumeProvider{bot: bot}
	r := httptest.NewRequest(http.MethodPost, "/api/v2/bots/a/close-positions", strings.NewReader(`{"method":"market","quantity_ratio":0.5}`))
	r.Header.Set("Content-Type", "application/json")
	ctx, cancel := context.WithCancel(r.Context())
	cancel()
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = r
	c.Params = gin.Params{{Key: "id", Value: "a"}}
	closePositionsV2(c)
	var response ClosePositionsV2Response
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if w.Code != 500 || response.Success || response.RecordID != "close-stable" || response.OrderID != 42 || response.Status != position.CloseStatusUnknown || !bot.canceled {
		t.Fatalf("unknown response lost identity/cancellation: %+v canceled=%v code=%d", response, bot.canceled, w.Code)
	}
}

func TestManagedCloseHTTPNilAcknowledgementIsNotSuccess(t *testing.T) {
	old := botExtendedProvider
	t.Cleanup(func() { botExtendedProvider = old })
	botExtendedProvider = protectiveResumeProvider{bot: &managedCloseHTTPBot{}}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"method":"market"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: "a"}}
	closePositionsV2(c)
	if w.Code != 500 {
		t.Fatalf("nil ACK accepted: %d %s", w.Code, w.Body.String())
	}
}
