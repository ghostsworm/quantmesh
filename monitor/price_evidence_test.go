package monitor

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

func TestQuoteEvidenceContextUsesSameAtomicReceivedQuote(t *testing.T) {
	pm := &PriceMonitor{}
	at := time.Now().Add(-time.Second)
	pm.lastQuote.Store(&PriceChange{NewPrice: 100, Timestamp: at})
	price, received, err := pm.GetQuoteEvidenceContext(t.Context())
	if err != nil || price != 100 || received != at {
		t.Fatalf("context quote refreshed evidence timestamp: %v %v %v", price, received, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := pm.GetQuoteEvidenceContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled quote accepted: %v", err)
	}
	if _, _, err := pm.GetQuoteEvidenceContext(nil); err == nil {
		t.Fatal("nil context accepted")
	}
	var missing *PriceMonitor
	if _, _, err := missing.GetQuoteEvidenceContext(t.Context()); err == nil {
		t.Fatal("nil monitor accepted")
	}
}

func TestQuoteEvidenceIncludesInitialUnchangedAndInvalidPrices(t *testing.T) {
	pm := NewPriceMonitor(&fakePriceExchange{}, "BTCUSDT", 100)
	defer pm.Stop()
	if _, at := pm.GetQuoteEvidence(); !at.IsZero() {
		t.Fatal("unreceived price is fresh")
	}
	pm.updatePrice(100)
	price, first := pm.GetQuoteEvidence()
	if price != 100 || first.IsZero() {
		t.Fatal("first quote missing")
	}
	pm.updatePrice(100)
	price, second := pm.GetQuoteEvidence()
	if price != 100 || second.Before(first) {
		t.Fatal("unchanged quote missing")
	}
	_, repeated := pm.GetQuoteEvidence()
	if !repeated.Equal(second) {
		t.Fatal("cache read fabricated quote time")
	}
	pm.updatePrice(-1)
	if price, _ := pm.GetQuoteEvidence(); price != -1 {
		t.Fatal("invalid price hidden by cached good quote")
	}
	pm.UpdatePriceWithOHLCV(math.NaN(), 0, 0, 0)
	if price, _ := pm.GetQuoteEvidence(); !math.IsNaN(price) {
		t.Fatal("invalid OHLCV quote hidden")
	}
}
