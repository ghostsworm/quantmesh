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
