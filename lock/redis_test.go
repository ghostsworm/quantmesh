package lock

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// fakeRedis 記憶體版 Redis，僅實現 RedisLock 用到的命令語義
type fakeRedis struct {
	mu        sync.Mutex
	data      map[string]string
	extendCnt atomic.Int64
}

func newFakeRedis() *fakeRedis { return &fakeRedis{data: make(map[string]string)} }

func (f *fakeRedis) SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.BoolCmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.data[key]; ok {
		return redis.NewBoolResult(false, nil)
	}
	f.data[key] = fmt.Sprint(value)
	return redis.NewBoolResult(true, nil)
}

func (f *fakeRedis) Eval(ctx context.Context, script string, keys []string, args ...interface{}) *redis.Cmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.data[keys[0]] != fmt.Sprint(args[0]) {
		return redis.NewCmdResult(int64(0), nil)
	}
	switch {
	case strings.Contains(script, `"del"`):
		delete(f.data, keys[0])
	case strings.Contains(script, `"pexpire"`):
		f.extendCnt.Add(1)
	}
	return redis.NewCmdResult(int64(1), nil)
}

func (f *fakeRedis) Ping(ctx context.Context) *redis.StatusCmd {
	return redis.NewStatusResult("PONG", nil)
}
func (f *fakeRedis) Close() error { return nil }

// TestRedisLockConcurrentKeys 多槽位並發加解鎖不應觸發 concurrent map writes（需配合 -race）
func TestRedisLockConcurrentKeys(t *testing.T) {
	l := newRedisLock(newFakeRedis(), "test:")
	ctx := context.Background()
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				key := fmt.Sprintf("order:%d:%d", g, i%5)
				ok, err := l.TryLock(ctx, key, time.Second)
				if err != nil {
					t.Errorf("TryLock(%s) error = %v", key, err)
					return
				}
				if !ok {
					t.Errorf("TryLock(%s) = false, want true (key 未被佔用)", key)
					return
				}
				if err := l.Extend(ctx, key, time.Second); err != nil {
					t.Errorf("Extend(%s) error = %v", key, err)
					return
				}
				if err := l.Unlock(ctx, key); err != nil {
					t.Errorf("Unlock(%s) error = %v", key, err)
					return
				}
			}
		}(g)
	}
	wg.Wait()
}

func TestRedisLockSemantics(t *testing.T) {
	ctx := context.Background()
	fr := newFakeRedis()
	a := newRedisLock(fr, "p:")
	b := newRedisLock(fr, "p:")

	tests := []struct {
		name string
		run  func() error
	}{
		{name: "a 獲取鎖", run: func() error {
			ok, err := a.TryLock(ctx, "k", time.Second)
			if err != nil || !ok {
				return fmt.Errorf("TryLock = %v, %v", ok, err)
			}
			return nil
		}},
		{name: "b 無法獲取已佔用的鎖", run: func() error {
			ok, err := b.TryLock(ctx, "k", time.Second)
			if err != nil || ok {
				return fmt.Errorf("TryLock = %v, %v, want false", ok, err)
			}
			return nil
		}},
		{name: "b 未持有時 Unlock 報錯", run: func() error {
			if err := b.Unlock(ctx, "k"); err == nil {
				return fmt.Errorf("Unlock 應報錯")
			}
			return nil
		}},
		{name: "a 釋放鎖", run: func() error { return a.Unlock(ctx, "k") }},
		{name: "b 在釋放後可獲取", run: func() error {
			ok, err := b.TryLock(ctx, "k", time.Second)
			if err != nil || !ok {
				return fmt.Errorf("TryLock = %v, %v", ok, err)
			}
			return nil
		}},
	}
	for _, tt := range tests {
		if err := tt.run(); err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
	}
}

func TestRedisLockExpiredLeaseCannotBeReacquiredBeforeOldUnlock(t *testing.T) {
	ctx := context.Background()
	fr := newFakeRedis()
	l := newRedisLock(fr, "p:")

	if ok, err := l.TryLock(ctx, "k", time.Second); err != nil || !ok {
		t.Fatalf("initial TryLock = %v, %v", ok, err)
	}

	// Simulate Redis expiring the lease while its original caller is delayed.
	fr.mu.Lock()
	delete(fr.data, "p:k")
	fr.mu.Unlock()
	if ok, err := l.TryLock(ctx, "k", time.Second); err != nil || ok {
		t.Fatalf("same instance reacquired before old Unlock = %v, %v; want false, nil", ok, err)
	}

	if err := l.Unlock(ctx, "k"); err == nil {
		t.Fatal("Unlock of expired lease should report that ownership was lost")
	}
	if ok, err := l.TryLock(ctx, "k", time.Second); err != nil || !ok {
		t.Fatalf("TryLock after old lease was cleared = %v, %v; want true, nil", ok, err)
	}
}

func TestRedisLockRejectsNonPositiveTTL(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name string
		ttl  time.Duration
	}{
		{name: "zero", ttl: 0},
		{name: "negative", ttl: -time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fr := newFakeRedis()
			l := newRedisLock(fr, "p:")
			if ok, err := l.TryLock(ctx, "k", tt.ttl); err == nil || ok {
				t.Fatalf("TryLock = %v, %v; want false and error", ok, err)
			}
			if err := l.Lock(ctx, "blocking", tt.ttl); err == nil {
				t.Fatal("Lock accepted a non-positive TTL")
			}
			fr.mu.Lock()
			_, exists := fr.data["p:k"]
			fr.mu.Unlock()
			if exists {
				t.Fatal("invalid TTL created a Redis key")
			}
		})
	}

	fr := newFakeRedis()
	l := newRedisLock(fr, "p:")
	if ok, err := l.TryLock(ctx, "held", time.Second); err != nil || !ok {
		t.Fatalf("TryLock = %v, %v", ok, err)
	}
	for _, ttl := range []time.Duration{0, -time.Second} {
		if err := l.Extend(ctx, "held", ttl); err == nil {
			t.Fatalf("Extend(%s) accepted non-positive TTL", ttl)
		}
	}
	if got := fr.extendCnt.Load(); got != 0 {
		t.Fatalf("invalid extensions reached Redis %d times", got)
	}
}

func TestStartAutoRenewExtendsUntilStopped(t *testing.T) {
	fr := newFakeRedis()
	l := newRedisLock(fr, "p:")
	ctx := context.Background()
	if ok, err := l.TryLock(ctx, "k", 30*time.Millisecond); err != nil || !ok {
		t.Fatalf("TryLock = %v, %v", ok, err)
	}
	stop := StartAutoRenew(l, "k", 30*time.Millisecond, func(err error) { t.Errorf("unexpected renew error: %v", err) })
	time.Sleep(100 * time.Millisecond)
	stop()
	stop() // 可重複調用
	got := fr.extendCnt.Load()
	if got < 2 {
		t.Fatalf("extend count = %d, want >= 2", got)
	}
	time.Sleep(40 * time.Millisecond)
	if after := fr.extendCnt.Load(); after != got {
		t.Fatalf("stop 後仍在續期: %d -> %d", got, after)
	}
}

func TestStartAutoRenewReportsLostLock(t *testing.T) {
	l := newRedisLock(newFakeRedis(), "p:")
	errCh := make(chan error, 1)
	stop := StartAutoRenew(l, "never-held", 15*time.Millisecond, func(err error) { errCh <- err })
	defer stop()
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("want error")
		}
	case <-time.After(time.Second):
		t.Fatal("未回報續期失敗")
	}
}
