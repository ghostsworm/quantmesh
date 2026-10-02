package strategy

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/lock"
)

func TestCapitalProofLockWaitRespectsRequestDeadline(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.BotID, cfg.Trading.Symbol = "bot-a", "BTCUSDT"
	s := NewTrendFollowingStrategy("trend", cfg, nil, nil, nil)
	s.SetRuntimeStateStore(&capitalReleaseContextStateStore{memoryRuntimeStateStore: &memoryRuntimeStateStore{}})
	s.mu.Lock()
	locked := true
	defer func() {
		if locked {
			s.mu.Unlock()
		}
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.VerifyCapitalReleaseState(ctx) }()
	var result error
	returned := false
	select {
	case result = <-done:
		returned = true
	case <-time.After(time.Second):
	}
	s.mu.Unlock()
	locked = false
	if !returned {
		result = <-done
	}
	if !returned || !errors.Is(result, context.DeadlineExceeded) {
		t.Fatalf("proof waited beyond deadline for writer lock: returnedWhileLocked=%v err=%v", returned, result)
	}
	if err := s.VerifyCapitalReleaseState(t.Context()); err != nil {
		t.Fatalf("cancelled waiter broke later proof: %v", err)
	}
}

func TestCapitalProofLockWaitAcrossStrategies(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.BotID, cfg.Trading.Symbol = "bot-a", "BTCUSDT"
	store := &capitalReleaseContextStateStore{memoryRuntimeStateStore: &memoryRuntimeStateStore{}}
	trend := NewTrendFollowingStrategy("trend", cfg, nil, nil, nil)
	trend.SetRuntimeStateStore(store)
	mean := NewMeanReversionStrategy("mean_reversion", cfg, nil, nil, nil)
	mean.SetRuntimeStateStore(store)
	momentum := NewMomentumStrategy("momentum", cfg, nil, nil, nil)
	momentum.SetRuntimeStateStore(store)
	dca := NewDCAEnhancedStrategy("dca", "BTCUSDT", cfg, nil, nil, nil)
	dca.SetRuntimeStateStore(store)
	martin := NewMartingaleStrategy("martingale", "BTCUSDT", cfg, nil, nil, nil)
	martin.SetRuntimeStateStore(store)
	spotLong := NewSpotLongStrategy("spot_long", cfg, nil, nil, nil)
	spotLong.SetRuntimeStateStore(store)
	spotShort := NewSpotShortStrategy("spot_short", cfg, nil, nil, nil, nil)
	spotShort.SetRuntimeStateStore(store)
	futuresLong := NewFuturesLongStrategy("futures_long", cfg, nil, nil, nil)
	futuresLong.SetRuntimeStateStore(store)
	futuresShort := NewFuturesShortStrategy("futures_short", cfg, nil, nil, nil)
	futuresShort.SetRuntimeStateStore(store)
	combo, _, _ := comboCapitalFixture(t, "trend")
	for _, test := range []struct {
		name   string
		mutex  sync.Locker
		verify func(context.Context) error
	}{
		{"trend", &trend.mu, trend.VerifyCapitalReleaseState},
		{"mean_reversion", &mean.mu, mean.VerifyCapitalReleaseState},
		{"momentum", &momentum.mu, momentum.VerifyCapitalReleaseState},
		{"dca", &dca.mu, dca.VerifyCapitalReleaseState},
		{"martingale", &martin.mu, martin.VerifyCapitalReleaseState},
		{"spot_long", &spotLong.mu, spotLong.VerifyCapitalReleaseState},
		{"spot_short", &spotShort.mu, spotShort.VerifyCapitalReleaseState},
		{"futures_long", &futuresLong.orderTracker.mu, futuresLong.VerifyCapitalReleaseState},
		{"futures_short", &futuresShort.orderTracker.mu, futuresShort.VerifyCapitalReleaseState},
		{"combo", &combo.mu, combo.VerifyCapitalReleaseState},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.mutex.Lock()
			locked := true
			defer func() {
				if locked {
					test.mutex.Unlock()
				}
			}()
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- test.verify(ctx) }()
			var result error
			returned := false
			select {
			case result = <-done:
				returned = true
			case <-time.After(time.Second):
			}
			test.mutex.Unlock()
			locked = false
			if !returned {
				result = <-done
			}
			if !returned || !errors.Is(result, context.DeadlineExceeded) {
				t.Fatalf("lock waiter ignored deadline: early=%v err=%v", returned, result)
			}
			if err := test.verify(t.Context()); err != nil {
				t.Fatalf("cancelled waiter retained lock: %v", err)
			}
		})
	}
}

func TestCapitalProofLockCancelledAcquisitionUnlocks(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	unlocks := 0
	err := acquireCapitalProofLock(ctx, func() bool { cancel(); return true }, func() { unlocks++ })
	if !errors.Is(err, context.Canceled) || unlocks != 1 {
		t.Fatalf("cancelled acquisition leaked lock: err=%v unlocks=%d", err, unlocks)
	}
	if err := acquireCapitalProofLock(nil, func() bool { t.Fatal("nil context attempted lock"); return false }, func() {}); err == nil {
		t.Fatal("nil context accepted")
	}
}

func TestCapitalProofCancelledLockWaitRestoresWalletAndDispatch(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.BotID, cfg.Trading.Symbol = "bot-a", "BTCUSDT"
	s := NewSpotShortStrategy("spot_short", cfg, nil, nil, nil, nil)
	s.SetRuntimeStateStore(&capitalReleaseContextStateStore{memoryRuntimeStateStore: &memoryRuntimeStateStore{}})
	allocator := NewCapitalAllocator(cfg, 1000)
	allocator.RegisterStrategy("spot_short", 1, 0)
	allocator.Allocate()
	if !allocator.Reserve("spot_short", 200) {
		t.Fatal("fixture reserve failed")
	}
	mse := NewMultiStrategyExecutor(nil, allocator)
	s.mu.Lock()
	locked := true
	defer func() {
		if locked {
			s.mu.Unlock()
		}
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	key := "capital-proof-test:" + t.Name()
	done := make(chan error, 1)
	go func() {
		done <- WithAccountWalletCoordination(ctx, lock.NewNopLock(), key, func(proofCtx context.Context) error {
			finish, err := mse.BeginCapitalReconciliation(proofCtx)
			if err != nil {
				return err
			}
			defer finish()
			_, err = allocator.ReleaseVerified(proofCtx, "spot_short", s.VerifyCapitalReleaseState)
			return err
		})
	}()
	var result error
	returned := false
	select {
	case result = <-done:
		returned = true
	case <-time.After(time.Second):
	}
	s.mu.Unlock()
	locked = false
	if !returned {
		result = <-done
	}
	if !returned || !errors.Is(result, context.DeadlineExceeded) || allocator.GetUsed("spot_short") != 200 {
		t.Fatalf("cancelled lock proof cleared capital/kept barriers: early=%v err=%v", returned, result)
	}
	retryCtx, retryCancel := context.WithTimeout(t.Context(), time.Second)
	defer retryCancel()
	err := WithAccountWalletCoordination(retryCtx, lock.NewNopLock(), key, func(proofCtx context.Context) error {
		finish, err := mse.BeginCapitalReconciliation(proofCtx)
		if err != nil {
			return err
		}
		defer finish()
		amounts, err := allocator.ReleaseVerified(proofCtx, "spot_short", s.VerifyCapitalReleaseState)
		if err == nil && amounts["spot_short"] != 200 {
			t.Errorf("wrong legal release amount: %v", amounts)
		}
		return err
	})
	if err != nil {
		t.Fatalf("cancelled proof prevented later wallet/dispatch recovery: %v", err)
	}
}
