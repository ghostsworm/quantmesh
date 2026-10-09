package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type scopedCloseProbe struct {
	SymbolManagerProvider
	exchange, symbol, market, bot string
	canceled                      bool
	result                        *ClosePositionsResponse
	err                           error
}

func (p *scopedCloseProbe) ClosePositionsScoped(ctx context.Context, ex, sym, mt, id string) (*ClosePositionsResponse, error) {
	p.exchange, p.symbol, p.market, p.bot = ex, sym, mt, id
	p.canceled = ctx.Err() != nil
	return p.result, p.err
}

func TestScopedManualCloseHTTPForwardsIdentityAndCancellation(t *testing.T) {
	previous := symbolManagerProvider
	t.Cleanup(func() { symbolManagerProvider = previous })
	p := &scopedCloseProbe{result: &ClosePositionsResponse{SuccessCount: 1}}
	symbolManagerProvider = p
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	r := httptest.NewRequest(http.MethodPost, "/api/trading/close-positions?exchange=binance&symbol=BTCUSDT&market_type=spot&bot_id=target", nil)
	ctx, cancel := context.WithCancel(r.Context())
	cancel()
	c.Request = r.WithContext(ctx)
	closeAllPositionsScoped(c)
	if w.Code != 200 || p.market != "spot" || p.bot != "target" || p.exchange != "binance" || p.symbol != "BTCUSDT" || !p.canceled {
		t.Fatal("scope or request context lost")
	}
}

func TestScopedManualCloseHTTPRejectsLegacyProviderAndUnverifiedResults(t *testing.T) {
	previous := symbolManagerProvider
	t.Cleanup(func() { symbolManagerProvider = previous })
	for _, tc := range []struct {
		provider SymbolManagerProvider
		status   int
	}{
		{manualCloseHTTPProvider{result: &ClosePositionsResponse{SuccessCount: 1}}, 503},
		{&scopedCloseProbe{err: ErrManualCloseScopeAmbiguous}, 409},
		{&scopedCloseProbe{err: ErrManualCloseScopeUnavailable}, 503},
		{&scopedCloseProbe{err: errors.New("private provider detail")}, 500},
		{&scopedCloseProbe{}, 503},
		{&scopedCloseProbe{result: &ClosePositionsResponse{SuccessCount: 1, FailCount: 1}}, 503},
	} {
		symbolManagerProvider = tc.provider
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/?exchange=binance&symbol=BTCUSDT&market_type=spot", nil)
		closeAllPositionsScoped(c)
		if w.Code != tc.status || strings.Contains(w.Body.String(), "private") || strings.Contains(w.Body.String(), "success_count") {
			t.Fatalf("unverified/legacy close reported success: %d", w.Code)
		}
	}
}
