package binance

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sdk "github.com/adshao/go-binance/v2"
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

func TestStopEvidenceAdaptersAreRESTOnlyAndLoadExactSymbolMetadata(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/fapi/v1/exchangeInfo":
			fmt.Fprint(w, `{"symbols":[{"symbol":"BTCUSDT","baseAsset":"BTC","quoteAsset":"USDT","pricePrecision":2,"quantityPrecision":3,"filters":[{"filterType":"PRICE_FILTER","tickSize":"0.01"},{"filterType":"LOT_SIZE","stepSize":"0.001"}]}]}`)
		case "/api/v3/exchangeInfo":
			if r.URL.Query().Get("symbol") != "BTCUSDT" {
				t.Errorf("spot metadata symbol = %q", r.URL.Query().Get("symbol"))
			}
			fmt.Fprint(w, `{"symbols":[{"symbol":"BTCUSDT","baseAsset":"BTC","quoteAsset":"USDT","quotePrecision":8,"baseAssetPrecision":8,"filters":[{"filterType":"PRICE_FILTER","tickSize":"0.01"},{"filterType":"LOT_SIZE","stepSize":"0.001"}]}]}`)
		default:
			t.Errorf("unexpected non-metadata request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	previousBaseURL := futuresRESTBaseURL
	futuresRESTBaseURL = func(bool) string { return srv.URL }
	t.Cleanup(func() { futuresRESTBaseURL = previousBaseURL })
	ctx := context.Background()

	futuresAdapter, err := NewBinanceFuturesStopEvidenceAdapter(ctx, "key", "secret", false, "BTCUSDT")
	if err != nil {
		t.Fatal(err)
	}
	if futuresAdapter.GetBaseAsset() != "BTC" || futuresAdapter.GetQuoteAsset() != "USDT" || futuresAdapter.GetQuantityDecimals() != 3 || futuresAdapter.wsManager != nil || !futuresAdapter.stopEvidenceOnly {
		t.Fatalf("futures evidence adapter metadata or stream state invalid: base=%q quote=%q qtyDecimals=%d ws=%v readOnly=%v", futuresAdapter.GetBaseAsset(), futuresAdapter.GetQuoteAsset(), futuresAdapter.GetQuantityDecimals(), futuresAdapter.wsManager, futuresAdapter.stopEvidenceOnly)
	}

	spotClient := sdk.NewClient("key", "secret").SetApiEndpoint(srv.URL)
	spotAdapter, err := newBinanceSpotStopEvidenceAdapter(ctx, spotClient, "BTCUSDT", "key", "secret", false)
	if err != nil {
		t.Fatal(err)
	}
	if spotAdapter.GetBaseAsset() != "BTC" || spotAdapter.GetQuoteAsset() != "USDT" || spotAdapter.GetQuantityDecimals() != 3 || spotAdapter.wsManager != nil || spotAdapter.orderWS != nil || !spotAdapter.stopEvidenceOnly {
		t.Fatalf("spot evidence adapter metadata or stream state invalid: base=%q quote=%q qtyDecimals=%d ws=%v orderWS=%v readOnly=%v", spotAdapter.GetBaseAsset(), spotAdapter.GetQuoteAsset(), spotAdapter.GetQuantityDecimals(), spotAdapter.wsManager, spotAdapter.orderWS, spotAdapter.stopEvidenceOnly)
	}

	marginClient := sdk.NewClient("key", "secret").SetApiEndpoint(srv.URL)
	marginAdapter, err := newBinanceSpotMarginStopEvidenceAdapter(ctx, marginClient, "key", "secret", false, "BTCUSDT")
	if err != nil {
		t.Fatal(err)
	}
	if marginAdapter.GetBaseAsset() != "BTC" || marginAdapter.marginClient == nil || marginAdapter.wsManager != nil || marginAdapter.orderWS != nil || !marginAdapter.stopEvidenceOnly {
		t.Fatal("margin evidence adapter lacks scoped metadata or unexpectedly initialized streams")
	}
	if _, err := futuresAdapter.PlaceOrder(ctx, nil); err != errStopEvidenceReadOnly {
		t.Fatalf("futures evidence order was not rejected: %v", err)
	}
	if err := futuresAdapter.CancelOrder(ctx, "BTCUSDT", 1); err != errStopEvidenceReadOnly {
		t.Fatalf("futures evidence cancellation was not rejected: %v", err)
	}
	if _, accepted := futuresAdapter.BatchPlaceOrders(ctx, []*OrderRequest{{Symbol: "BTCUSDT"}}); accepted {
		t.Fatal("futures evidence batch order was accepted")
	}
	if err := futuresAdapter.BatchCancelOrders(ctx, "BTCUSDT", []int64{1}); err != errStopEvidenceReadOnly {
		t.Fatalf("futures evidence batch cancellation was not rejected: %v", err)
	}
	if _, err := spotAdapter.PlaceOrder(ctx, nil); err != errStopEvidenceReadOnly {
		t.Fatalf("spot evidence order was not rejected: %v", err)
	}
	if _, accepted := spotAdapter.BatchPlaceOrders(ctx, []*OrderRequest{{Symbol: "BTCUSDT"}}); accepted {
		t.Fatal("spot evidence batch order was accepted")
	}
	if err := spotAdapter.CancelAllOrders(ctx, "BTCUSDT"); err != errStopEvidenceReadOnly {
		t.Fatalf("spot evidence cancel-all was not rejected: %v", err)
	}
	if _, err := spotAdapter.InternalTransfer(ctx, "SPOT", "UMFUTURE", "USDT", 1); err != errStopEvidenceReadOnly {
		t.Fatalf("spot evidence transfer was not rejected: %v", err)
	}
	if _, err := marginAdapter.Borrow(ctx, "BTC", 1); err != errStopEvidenceReadOnly {
		t.Fatalf("margin evidence borrow was not rejected: %v", err)
	}
	if _, err := marginAdapter.Repay(ctx, "BTC", 1); err != errStopEvidenceReadOnly {
		t.Fatalf("margin evidence repayment was not rejected: %v", err)
	}
	if err := marginAdapter.CancelOrder(ctx, "BTCUSDT", 1); err != errStopEvidenceReadOnly {
		t.Fatalf("margin evidence cancellation was not rejected: %v", err)
	}
	if marginAdapter.GetMarginClient() != nil {
		t.Fatal("margin evidence adapter exposed its mutable margin client")
	}
	if requests != 3 {
		t.Fatalf("evidence construction or rejected mutations issued %d requests, want exactly 3 public metadata reads", requests)
	}
}
