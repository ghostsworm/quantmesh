package sync

import (
	"context"
	"testing"
	"time"

	"quantmesh/exchange"
	"quantmesh/storage"
)

type syncExchangeWithoutAdapter struct{ exchange.IExchange }

type syncExchangeWithoutTradeHistory struct{ exchange.IExchange }

func (syncExchangeWithoutTradeHistory) GetAdapter() interface{} { return struct{}{} }

func TestOrderSyncServiceCanRestartAfterContextCancel(t *testing.T) {
	service := NewOrderSyncService(nil, nil, "BTCUSDT", "acct", "mock", 0)
	ctx, cancel := context.WithCancel(context.Background())

	service.Start(ctx)
	if !service.isRunning {
		t.Fatal("Start() 后订单同步服务应处于运行状态")
	}
	if service.syncInterval != defaultOrderSyncInterval {
		t.Fatalf("无效同步间隔应使用默认值，got %s", service.syncInterval)
	}

	cancel()
	deadline := time.After(time.Second)
	for {
		service.mu.RLock()
		isRunning := service.isRunning
		service.mu.RUnlock()
		if !isRunning {
			break
		}

		select {
		case <-deadline:
			t.Fatal("context 取消后订单同步服务应自动标记为停止")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	service.Start(nil)
	if !service.isRunning || service.stopC == nil {
		t.Fatal("context 取消退出后应允许 Start(nil) 安全重启")
	}
	service.Stop()
}

func TestOrderSyncServiceNilDependenciesSkipSafely(t *testing.T) {
	service := NewOrderSyncService(nil, nil, "BTCUSDT", "acct", "mock", time.Second)

	if err := service.Sync(nil); err != nil {
		t.Fatalf("依赖为空时应安全跳过，got %v", err)
	}
}

func TestOrderSyncServiceUnsupportedExchangeFailsInsteadOfReportingSuccess(t *testing.T) {
	t.Run("no adapter access", func(t *testing.T) {
		service := NewOrderSyncService(syncExchangeWithoutAdapter{}, nil, "BTCUSDT", "acct", "venue-x", time.Second)
		service.storage = &syncStorageStub{}
		if err := service.Sync(context.Background()); err == nil {
			t.Fatal("unsupported adapter access must not report success")
		}
	})
	t.Run("adapter without paginated trade history", func(t *testing.T) {
		service := NewOrderSyncService(syncExchangeWithoutTradeHistory{}, nil, "BTCUSDT", "acct", "venue-y", time.Second)
		service.storage = &syncStorageStub{}
		if err := service.Sync(context.Background()); err == nil {
			t.Fatal("missing trade-history capability must not report success")
		}
	})
}

type syncStorageStub struct{ storage.Storage }

type orderFillOrderReaderStub struct{ called bool }

func (r *orderFillOrderReaderStub) GetExistingOrderIDsForScope(string, string, string, string, []int64) (map[int64]bool, error) {
	r.called = true
	return map[int64]bool{}, nil
}

func TestPersistTradePageRejectsInvalidSideBeforeAnyPersistence(t *testing.T) {
	service := NewOrderSyncService(nil, nil, "BTCUSDT", "acct", "binance", time.Minute)
	service.marketType = "futures"
	service.accountScope = "scope"
	reader := &orderFillOrderReaderStub{}
	writer := &captureFillWriter{}
	_, err := service.persistTradePage([]*exchange.OrderFill{{
		OrderID: 1, TradeID: "trade", Symbol: "BTCUSDT", Side: "BID", Price: 100, Quantity: 1, TradeTime: 1_790_000_000_000,
	}}, reader, writer)
	if err == nil {
		t.Fatal("invalid execution side must be rejected")
	}
	if reader.called || len(writer.fills) != 0 {
		t.Fatalf("invalid execution must be rejected before storage access: reader=%v persisted=%d", reader.called, len(writer.fills))
	}
}

type futureFillHistoryExchange struct {
	exchange.IExchange
	futureFill *exchange.OrderFill
}

type limitedFillHistoryExchange struct {
	exchange.IExchange
	calls [][2]int64
}

func (e *limitedFillHistoryExchange) MaxOrderHistoryRange() time.Duration {
	return 7*24*time.Hour - time.Second
}

func (e *limitedFillHistoryExchange) GetOrderHistoryPage(_ context.Context, _ string, startTime, endTime int64, _ string, _ int) (exchange.OrderHistoryPage, error) {
	e.calls = append(e.calls, [2]int64{startTime, endTime})
	return exchange.OrderHistoryPage{}, nil
}

func (e futureFillHistoryExchange) GetOrderHistoryPage(context.Context, string, int64, int64, string, int) (exchange.OrderHistoryPage, error) {
	return exchange.OrderHistoryPage{Fills: []*exchange.OrderFill{e.futureFill}, HasMore: true, NextCursor: "older-page"}, nil
}

type orderSyncCoverageStorage struct {
	syncStorageStub
	coverageAdvanced bool
	fillsSaved       int
}

func (*orderSyncCoverageStorage) GetOrderFillCoverage(string, string, string, string) (*storage.OrderFillCoverage, error) {
	return nil, nil
}

func (s *orderSyncCoverageStorage) AdvanceOrderFillCoverage(string, string, string, string, time.Time, time.Time) error {
	s.coverageAdvanced = true
	return nil
}

func (*orderSyncCoverageStorage) GetExistingOrderIDsForScope(string, string, string, string, []int64) (map[int64]bool, error) {
	return map[int64]bool{}, nil
}

func (s *orderSyncCoverageStorage) SaveOrderFill(*storage.OrderFill) error {
	s.fillsSaved++
	return nil
}

func (*orderSyncCoverageStorage) SaveOrder(*storage.Order) error { return nil }

func TestOrderSyncRejectsOutOfRangeFillWithoutAdvancingCoverage(t *testing.T) {
	store := &orderSyncCoverageStorage{}
	service := NewOrderSyncService(futureFillHistoryExchange{futureFill: &exchange.OrderFill{
		OrderID: 1, TradeID: "future-trade", Symbol: "BTCUSDT", Side: exchange.SideBuy,
		Price: 100, Quantity: 1, TradeTime: time.Now().Add(time.Minute).UnixMilli(),
	}}, store, "BTCUSDT", "acct", "mock", time.Minute)
	service.SetTradeScope("futures", "scope")
	err := service.Sync(context.Background())
	if err == nil {
		t.Fatalf("out-of-range execution must fail synchronization, got %v", err)
	}
	if store.coverageAdvanced {
		t.Fatal("out-of-range execution must not advance complete-history coverage")
	}
	if store.fillsSaved != 0 {
		t.Fatalf("out-of-range execution must not be persisted, got %d fills", store.fillsSaved)
	}
}

func TestOrderSyncSplitsVenueLimitedHistoryRangeBeforeAdvancingCoverage(t *testing.T) {
	store := &orderSyncCoverageStorage{}
	source := &limitedFillHistoryExchange{}
	service := NewOrderSyncService(source, store, "BTCUSDT", "acct", "limited", time.Minute)
	service.SetTradeScope("futures", "scope")
	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if len(source.calls) < 4 {
		t.Fatalf("30-day history should split into multiple bounded requests, got %d", len(source.calls))
	}
	for index, request := range source.calls {
		if request[1]-request[0] > source.MaxOrderHistoryRange().Milliseconds() {
			t.Fatalf("request %d exceeds venue range: %v", index, request)
		}
		if index > 0 && source.calls[index-1][1]+1 != request[0] {
			t.Fatalf("history windows are not contiguous: previous=%v current=%v", source.calls[index-1], request)
		}
	}
	if !store.coverageAdvanced {
		t.Fatal("complete bounded history windows should advance coverage")
	}
}
