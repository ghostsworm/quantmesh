package sync

import (
	"context"
	"testing"
	"time"
)

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
