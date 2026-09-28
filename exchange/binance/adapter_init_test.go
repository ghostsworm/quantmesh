package binance

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPublicAdapterInitializationCancellation(t *testing.T) {
	for _, blockedPath := range []string{"/fapi/v1/time", "/fapi/v1/exchangeInfo"} {
		t.Run(blockedPath, func(t *testing.T) {
			resetFuturesNetworkForTest(t)
			entered := make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == blockedPath {
					close(entered)
					<-r.Context().Done()
					return
				}
				if r.URL.Path == "/fapi/v1/time" {
					fmt.Fprintf(w, `{"serverTime":%d}`, time.Now().UnixMilli())
					return
				}
				http.NotFound(w, r)
			}))
			defer srv.Close()
			futuresRESTBaseURL = func(bool) string { return srv.URL }
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				adapter, err := NewBinanceAdapterForPublicDataContext(ctx, nil, "BTCUSDT")
				if adapter != nil {
					done <- errors.New("canceled constructor returned usable adapter")
					return
				}
				done <- err
			}()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("initialization request not observed")
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancel error=%v", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("constructor ignored task cancellation")
			}
		})
	}
}

func TestPublicAdapterRejectsAlreadyCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	adapter, err := NewBinanceAdapterForPublicDataContext(ctx, nil, "BTCUSDT")
	if adapter != nil || err != context.Canceled {
		t.Fatalf("adapter=%v err=%v", adapter, err)
	}
}
