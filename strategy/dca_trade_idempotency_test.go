package strategy

import (
	"testing"
	"time"

	"quantmesh/storage"
)

type dcaLegacyOnlyTradeStorage struct{ calls int }

func (s *dcaLegacyOnlyTradeStorage) SaveTrade(int64, int64, string, string, float64, float64, float64, float64, float64, string, time.Time, string) error {
	s.calls++
	return nil
}

type dcaIdempotentTradeStorage struct {
	dcaLegacyOnlyTradeStorage
	trade *storage.Trade
}

func (s *dcaIdempotentTradeStorage) SaveTradeIdempotent(trade *storage.Trade) error {
	copy := *trade
	s.trade = &copy
	return nil
}

func TestDCACloseRequiresIdempotentTradeStorage(t *testing.T) {
	strategy := NewDCAEnhancedStrategy("dca", "BTCUSDT", nil, nil, &hedgeExchange{}, nil)
	legacy := &dcaLegacyOnlyTradeStorage{}
	strategy.tradeStorage = legacy
	if strategy.saveCloseTrade("execution-1", 42, 100, 110, 1, 10, 0.1, 9.9, "USDT") {
		t.Fatal("non-idempotent ledger must not confirm a close")
	}
	if legacy.calls != 0 {
		t.Fatalf("legacy SaveTrade called %d times; expected no fallback", legacy.calls)
	}
}

func TestDCACloseWritesStableExecutionIdentityIdempotently(t *testing.T) {
	strategy := NewDCAEnhancedStrategy("dca", "BTCUSDT", nil, nil, &hedgeExchange{}, nil)
	ledger := &dcaIdempotentTradeStorage{}
	strategy.tradeStorage = ledger
	if !strategy.saveCloseTrade("execution-2", 43, 100, 110, 1, 10, 0.1, 9.9, "USDT") {
		t.Fatal("idempotent ledger should confirm a saved close")
	}
	if ledger.trade == nil || ledger.trade.ExecutionKey != "execution-2" || ledger.trade.SellOrderID != 43 {
		t.Fatalf("unexpected idempotent ledger payload: %+v", ledger.trade)
	}
}
