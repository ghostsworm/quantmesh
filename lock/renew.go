package lock

import (
	"context"
	"sync"
	"time"
)

// StartAutoRenew 在持有鎖期間按 ttl/3 週期續期，避免長耗時操作（如下單重試）期間鎖過期被他人獲取。
// 回傳的 stop 函數會停止續期並等待續期協程退出，可重複調用。
// onError 可為 nil；續期失敗時回調（鎖可能已過期），續期協程隨即退出。
func StartAutoRenew(l DistributedLock, key string, ttl time.Duration, onError func(error)) (stop func()) {
	interval := ttl / 3
	if interval <= 0 {
		interval = time.Millisecond
	}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				extCtx, extCancel := context.WithTimeout(ctx, interval)
				err := l.Extend(extCtx, key, ttl)
				extCancel()
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					if onError != nil {
						onError(err)
					}
					return
				}
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			cancel()
			wg.Wait()
		})
	}
}
