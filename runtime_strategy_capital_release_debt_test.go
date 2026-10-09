package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/lock"
	"quantmesh/strategy"
)

type capitalReleaseDebtStateStore struct {
	version     int
	payload     string
	found       bool
	err         error
	readContext func(context.Context) error
}

func TestRuntimeStrategyCapitalReleaseWaitsForWalletMutation(t *testing.T) {
	venue := &capitalReleaseMarginVenue{capitalReleaseSpotVenue: &capitalReleaseSpotVenue{capitalReleaseRuntimeVenue: &capitalReleaseRuntimeVenue{runtimeJournalVenue: &runtimeJournalVenue{}}}}
	rt, _ := capitalReleaseRuntimeFixtureWithVenue(t, venue)
	err := strategy.WithAccountWalletCoordination(t.Context(), lock.NewNopLock(), "funding_carry_wallet:"+rt.capitalReleaseScope.Account, func(context.Context) error {
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		defer cancel()
		amounts, releaseErr := releaseRuntimeStrategyCapital(ctx, []*SymbolRuntime{rt}, "dca")
		if !errors.Is(releaseErr, context.DeadlineExceeded) || amounts["dca"] != 0 || rt.StrategyManager.GetCapitalAllocator().GetUsed("dca") != 200 || venue.debtCalls != 0 {
			t.Fatalf("wallet mutation bypassed: amounts=%v err=%v", amounts, releaseErr)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	amounts, err := releaseRuntimeStrategyCapital(t.Context(), []*SymbolRuntime{rt}, "dca")
	if err != nil || amounts["dca"] != 200 {
		t.Fatalf("wallet wait prevented later legal recovery: amounts=%v err=%v", amounts, err)
	}
}

type capitalReleaseUnlockFailure struct{ lock.DistributedLock }

func (capitalReleaseUnlockFailure) Unlock(context.Context, string) error {
	return errors.New("wallet unlock acknowledgement unavailable")
}

func TestRuntimeStrategyCapitalReleaseRetainsCommittedAmountOnWalletUnlockFailure(t *testing.T) {
	venue := &capitalReleaseMarginVenue{capitalReleaseSpotVenue: &capitalReleaseSpotVenue{capitalReleaseRuntimeVenue: &capitalReleaseRuntimeVenue{runtimeJournalVenue: &runtimeJournalVenue{}}}}
	rt, _ := capitalReleaseRuntimeFixtureWithVenue(t, venue)
	rt.capitalReleaseWalletLock = capitalReleaseUnlockFailure{DistributedLock: lock.NewNopLock()}
	amounts, err := releaseRuntimeStrategyCapital(t.Context(), []*SymbolRuntime{rt}, "dca")
	if err == nil || amounts["dca"] != 200 || rt.StrategyManager.GetCapitalAllocator().GetUsed("dca") != 0 {
		t.Fatalf("committed partial amount hidden: amounts=%v err=%v", amounts, err)
	}
}

func (s *capitalReleaseDebtStateStore) LoadRuntimeState(string) (int, string, bool, error) {
	return s.version, s.payload, s.found, s.err
}
func (s *capitalReleaseDebtStateStore) LoadRuntimeStateContext(ctx context.Context, name string) (int, string, bool, error) {
	if err := ctx.Err(); err != nil {
		return 0, "", false, err
	}
	if s.readContext != nil {
		if err := s.readContext(ctx); err != nil {
			return 0, "", false, err
		}
	}
	return s.LoadRuntimeState(name)
}
func (s *capitalReleaseDebtStateStore) SaveRuntimeState(_ string, version int, payload string) error {
	s.version, s.payload, s.found = version, payload, true
	return s.err
}

func TestRuntimeStrategyCapitalReleaseRejectsInvisibleBorrowIntent(t *testing.T) {
	venue := &capitalReleaseMarginVenue{capitalReleaseSpotVenue: &capitalReleaseSpotVenue{capitalReleaseRuntimeVenue: &capitalReleaseRuntimeVenue{runtimeJournalVenue: &runtimeJournalVenue{}}}}
	rt, _ := capitalReleaseRuntimeFixtureWithVenue(t, venue)
	cfg := &config.Config{}
	cfg.Trading.BotID, cfg.Trading.Symbol = rt.capitalReleaseScope.Bot, rt.capitalReleaseScope.Symbol
	short := strategy.NewSpotShortStrategy("spot_short", cfg, nil, nil, nil, nil)
	stateStore := &capitalReleaseDebtStateStore{version: 10, found: true, payload: `{"bot_id":"a","strategy":"spot_short","group_id":"","symbol":"BTCUSDT","base_asset":"BTC","pending_repay":{},"consumed_repay_transfers":{},"pending_borrow":{"pending":{"amount":1,"phase":"prepared","created_at_unix_milli":1}},"pending_buy":{}}`}
	short.SetRuntimeStateStore(stateStore)
	rt.StrategyManager.RegisterStrategy("spot_short", short, 1, 0)
	if len(short.GetPositions()) != 0 || len(short.GetOrders()) != 0 {
		t.Fatal("fixture must hide the borrow intent from generic inventory snapshots")
	}
	amounts, err := releaseRuntimeStrategyCapital(context.Background(), []*SymbolRuntime{rt}, "")
	if err == nil || amounts["dca"] != 0 || rt.StrategyManager.GetCapitalAllocator().GetUsed("dca") != 200 {
		t.Fatalf("pending borrow ignored despite flat venue: released=%v err=%v", amounts, err)
	}
	// Fixture represents completed debt reconciliation, not an implementation of
	// recovery. Once durable evidence is clean, release must be usable again.
	stateStore.payload = `{"bot_id":"a","strategy":"spot_short","group_id":"","symbol":"BTCUSDT","base_asset":"BTC","pending_repay":{},"consumed_repay_transfers":{},"pending_borrow":{},"pending_buy":{}}`
	amounts, err = releaseRuntimeStrategyCapital(t.Context(), []*SymbolRuntime{rt}, "")
	if err != nil || amounts["dca"] != 200 || rt.StrategyManager.GetCapitalAllocator().GetUsed("dca") != 0 {
		t.Fatalf("clean reconciled debt state did not permit recovery: released=%v err=%v", amounts, err)
	}
}

func TestRuntimeStrategyCapitalReleaseDebtReadCancellationReleasesWallet(t *testing.T) {
	venue := &capitalReleaseMarginVenue{capitalReleaseSpotVenue: &capitalReleaseSpotVenue{capitalReleaseRuntimeVenue: &capitalReleaseRuntimeVenue{runtimeJournalVenue: &runtimeJournalVenue{}}}}
	rt, _ := capitalReleaseRuntimeFixtureWithVenue(t, venue)
	cfg := &config.Config{}
	cfg.Trading.BotID, cfg.Trading.Symbol = rt.capitalReleaseScope.Bot, rt.capitalReleaseScope.Symbol
	short := strategy.NewSpotShortStrategy("spot_short", cfg, nil, nil, nil, nil)
	stateStore := &capitalReleaseDebtStateStore{readContext: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	short.SetRuntimeStateStore(stateStore)
	rt.StrategyManager.RegisterStrategy("spot_short", short, 1, 0)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	amounts, err := releaseRuntimeStrategyCapital(ctx, []*SymbolRuntime{rt}, "")
	if !errors.Is(err, context.DeadlineExceeded) || amounts["dca"] != 0 || rt.StrategyManager.GetCapitalAllocator().GetUsed("dca") != 200 {
		t.Fatalf("cancelled debt read cleared capital: amounts=%v err=%v", amounts, err)
	}
	stateStore.readContext = nil
	// A second complete release reacquires wallet, logical dispatch and physical
	// snapshot barriers. It must not inherit locks from the cancelled proof.
	retryCtx, retryCancel := context.WithTimeout(t.Context(), time.Second)
	defer retryCancel()
	amounts, err = releaseRuntimeStrategyCapital(retryCtx, []*SymbolRuntime{rt}, "")
	if err != nil || amounts["dca"] != 200 {
		t.Fatalf("cancelled proof retained coordination barriers: amounts=%v err=%v", amounts, err)
	}
}
