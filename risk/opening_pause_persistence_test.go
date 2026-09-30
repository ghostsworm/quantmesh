package risk

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"quantmesh/storage"
)

type sourceOpeningPauseTestBot struct {
	*safeMockBot
	mu    sync.Mutex
	gates map[string]bool
}

func (b *sourceOpeningPauseTestBot) PauseOpeningForSource(source, _ string) {
	b.mu.Lock()
	b.gates[source] = true
	b.mu.Unlock()
}

func (b *sourceOpeningPauseTestBot) ResumeOpeningForSource(source string) {
	b.mu.Lock()
	delete(b.gates, source)
	b.mu.Unlock()
}

func (b *sourceOpeningPauseTestBot) HasOpeningPauseSource(source string) bool {
	return b.hasGate(source)
}

func (b *sourceOpeningPauseTestBot) hasGate(source string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.gates[source]
}

type openingPauseStateTestStore struct {
	mu        sync.Mutex
	rows      map[string]string
	loadErr   error
	upsertErr error
	deleteErr error
}

func (s *openingPauseStateTestStore) LoadOpeningPauseHolders(context.Context) ([]storage.OpeningPauseHolder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	rows := make([]storage.OpeningPauseHolder, 0, len(s.rows))
	for key, reason := range s.rows {
		ownerID, source, found := strings.Cut(key, "\x00")
		if !found {
			ownerID = ""
			source = key
		}
		rows = append(rows, storage.OpeningPauseHolder{OwnerID: ownerID, Source: source, Reason: reason})
	}
	return rows, nil
}

func (s *openingPauseStateTestStore) UpsertOpeningPauseHolder(_ context.Context, holder storage.OpeningPauseHolder) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.upsertErr != nil {
		return s.upsertErr
	}
	s.rows[openingPauseTestStoreKey(holder)] = holder.Reason
	return nil
}

func (s *openingPauseStateTestStore) DeleteOpeningPauseHolder(_ context.Context, holder storage.OpeningPauseHolder) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deleteErr != nil {
		return s.deleteErr
	}
	delete(s.rows, openingPauseTestStoreKey(holder))
	return nil
}

func openingPauseTestStoreKey(holder storage.OpeningPauseHolder) string {
	if holder.OwnerID == "" {
		return holder.Source
	}
	return holder.OwnerID + "\x00" + holder.Source
}

func TestOpeningPauseCoordinatorRestoresPersistedOwnersBeforeBotStart(t *testing.T) {
	store := &openingPauseStateTestStore{rows: map[string]string{"circuit_breaker": "daily loss"}}
	coordinator, err := NewOpeningPauseCoordinatorWithStore(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	holders, release := coordinator.BeginBotStart()
	release()
	if len(holders) != 1 || holders[0] != (storage.OpeningPauseHolder{Source: "circuit_breaker", Reason: "daily loss"}) {
		t.Fatalf("restored holders = %+v", holders)
	}
	if !coordinator.IsHeldBy("circuit_breaker") {
		t.Fatal("persisted risk owner was not restored")
	}
}

func TestReleaseRestoredPauseClearsOnlyItsSourceGate(t *testing.T) {
	store := &openingPauseStateTestStore{rows: map[string]string{
		"circuit_breaker":  "daily loss",
		"emergency_center": "operator stop",
	}}
	coordinator, err := NewOpeningPauseCoordinatorWithStore(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	bot := &sourceOpeningPauseTestBot{safeMockBot: &safeMockBot{}, gates: make(map[string]bool)}
	holders, finish := coordinator.BeginBotStart()
	for _, holder := range holders {
		bot.PauseOpeningForSource(holder.Source, holder.Reason)
	}
	finish()

	if resumed, err := coordinator.ReleaseChecked("circuit_breaker", []BotController{bot}); err != nil || resumed {
		t.Fatalf("release circuit breaker: resumed=%v err=%v, want another owner to keep opening blocked", resumed, err)
	}
	if bot.hasGate("circuit_breaker") || !bot.hasGate("emergency_center") {
		t.Fatalf("source gates after partial release: circuit_breaker=%v emergency_center=%v", bot.hasGate("circuit_breaker"), bot.hasGate("emergency_center"))
	}
	if resumed, err := coordinator.ReleaseChecked("emergency_center", []BotController{bot}); err != nil || !resumed {
		t.Fatalf("release final owner: resumed=%v err=%v", resumed, err)
	}
	if bot.hasGate("emergency_center") || bot.resumes.Load() != 0 {
		t.Fatalf("restored source gate remained or generic resume was called: emergency_center=%v resumes=%d", bot.hasGate("emergency_center"), bot.resumes.Load())
	}
}

func TestRiskControllersRestoreTheirOwnPersistedPauseState(t *testing.T) {
	store := &openingPauseStateTestStore{rows: map[string]string{
		"circuit_breaker":  "daily loss",
		"composite_risk":   "stop trading",
		"emergency_center": "stop all",
	}}
	coordinator, err := NewOpeningPauseCoordinatorWithStore(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	gcb := NewGlobalCircuitBreaker(newCircuitBreakerTestConfig(), nil, &circuitBreakerMockProvider{})
	gcb.SetPauseCoordinator(coordinator)
	if gcb.GetStatus() != CircuitBreakerStatusTripped {
		t.Fatalf("restored circuit breaker status = %s", gcb.GetStatus())
	}
	guard := NewCompositeRiskGuard(&circuitBreakerMockProvider{}, coordinator, nil)
	if !guard.IsPaused() {
		t.Fatal("composite guard forgot its persisted pause owner")
	}
	emergency := &EmergencyCenter{}
	emergency.SetPauseCoordinator(coordinator)
	if !emergency.IsEmergencyMode() {
		t.Fatal("emergency center forgot its persisted pause owner")
	}
}

func TestOpeningPauseCoordinatorReleaseCannotDeleteAnotherInstancesOwner(t *testing.T) {
	store := &openingPauseStateTestStore{rows: make(map[string]string)}
	first, err := NewOpeningPauseCoordinatorWithStore(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Pause("emergency_center", "stop all", nil); err != nil {
		t.Fatal(err)
	}
	second, err := NewOpeningPauseCoordinatorWithStore(context.Background(), store)
	if err != nil || !second.IsHeldBy("emergency_center") {
		t.Fatalf("restored coordinator held=%v err=%v", second != nil && second.IsHeldBy("emergency_center"), err)
	}
	if second.Release("emergency_center", nil) {
		t.Fatal("second instance incorrectly reported global recovery while the first owner remained")
	}
	third, err := NewOpeningPauseCoordinatorWithStore(context.Background(), store)
	if err != nil || !third.IsHeldBy("emergency_center") {
		t.Fatalf("other process owner was lost: held=%v err=%v", third != nil && third.IsHeldBy("emergency_center"), err)
	}
	if !first.Release("emergency_center", nil) {
		t.Fatal("the owning instance could not release its own durable source")
	}
	fourth, err := NewOpeningPauseCoordinatorWithStore(context.Background(), store)
	if err != nil || fourth.IsHeldBy("emergency_center") {
		t.Fatalf("released owner remained: held=%v err=%v", fourth != nil && fourth.IsHeldBy("emergency_center"), err)
	}
}

func TestOpeningPauseStableInstanceIdentityRestoresOwnerAfterRestart(t *testing.T) {
	store := &openingPauseStateTestStore{rows: make(map[string]string)}
	first, err := NewOpeningPauseCoordinatorWithStore(context.Background(), store, "replica-a:/etc/quantmesh/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Pause("circuit_breaker", "daily loss", nil); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewOpeningPauseCoordinatorWithStore(context.Background(), store, "replica-a:/etc/quantmesh/config.yaml")
	if err != nil || !restarted.IsHeldBy("circuit_breaker") {
		t.Fatalf("stable instance did not restore its owner: held=%v err=%v", restarted != nil && restarted.IsHeldBy("circuit_breaker"), err)
	}
	if !restarted.Release("circuit_breaker", nil) {
		t.Fatal("restarted stable instance could not release its recovered owner")
	}
	other, err := NewOpeningPauseCoordinatorWithStore(context.Background(), store, "replica-b:/etc/quantmesh/config.yaml")
	if err != nil || other.IsHeldBy("circuit_breaker") {
		t.Fatalf("released stable owner remained: held=%v err=%v", other != nil && other.IsHeldBy("circuit_breaker"), err)
	}
}

func TestOpeningPauseStartupAndRecoveryRefreshSharedOwnersBeforeProceeding(t *testing.T) {
	store := &openingPauseStateTestStore{rows: make(map[string]string)}
	coordinator, err := NewOpeningPauseCoordinatorWithStore(context.Background(), store, "replica-a")
	if err != nil {
		t.Fatal(err)
	}
	remote := storage.OpeningPauseHolder{OwnerID: "replica-b", Source: "circuit_breaker", Reason: "daily loss"}
	if err := store.UpsertOpeningPauseHolder(context.Background(), remote); err != nil {
		t.Fatal(err)
	}
	openings, finish := coordinator.BeginBotStart()
	finish()
	wantGate := openingPauseGateSource(remote.OwnerID, remote.Source)
	if len(openings) != 1 || openings[0].Source != wantGate {
		t.Fatalf("startup did not refresh the shared owner before admitting a bot: %+v", openings)
	}
	actionRan := false
	if resumed, err := coordinator.RunIfUnheld(func() error { actionRan = true; return nil }); err != nil || resumed || actionRan {
		t.Fatalf("recovery bypassed a newly committed shared owner: resumed=%v actionRan=%v err=%v", resumed, actionRan, err)
	}
}

func TestOpeningPauseReleaseReadFailureRestoresOwnerAndFailsClosed(t *testing.T) {
	store := &openingPauseStateTestStore{rows: make(map[string]string)}
	coordinator, err := NewOpeningPauseCoordinatorWithStore(context.Background(), store, "replica-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := coordinator.Pause("emergency_center", "operator stop", nil); err != nil {
		t.Fatal(err)
	}
	store.loadErr = errors.New("read failed after delete")
	if resumed, err := coordinator.ReleaseChecked("emergency_center", nil); err == nil || resumed {
		t.Fatalf("unverified release succeeded: resumed=%v err=%v", resumed, err)
	}
	if !coordinator.IsHeldBy("emergency_center") {
		t.Fatal("unverified release dropped the local owner")
	}
	if got := store.rows[openingPauseTestStoreKey(storage.OpeningPauseHolder{OwnerID: coordinator.ownerID, Source: "emergency_center"})]; got == "" {
		t.Fatal("unverified release did not restore the durable fail-closed owner")
	}
}

func TestOpeningPauseInstanceOwnersSynchronizeWithoutReleasingEachOther(t *testing.T) {
	store := &openingPauseStateTestStore{rows: make(map[string]string)}
	first, err := NewOpeningPauseCoordinatorWithStore(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewOpeningPauseCoordinatorWithStore(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	firstBot := &sourceOpeningPauseTestBot{safeMockBot: &safeMockBot{}, gates: make(map[string]bool)}
	secondBot := &sourceOpeningPauseTestBot{safeMockBot: &safeMockBot{}, gates: make(map[string]bool)}
	if err := first.Pause("circuit_breaker", "instance one tripped", []BotController{firstBot}); err != nil {
		t.Fatal(err)
	}
	if err := second.SyncPersistentHolders(context.Background(), []BotController{secondBot}); err != nil {
		t.Fatal(err)
	}
	firstSource := openingPauseGateSource(first.ownerID, "circuit_breaker")
	if !secondBot.hasGate(firstSource) {
		t.Fatal("second process did not install the first process's durable hold")
	}
	if err := second.Pause("circuit_breaker", "instance two also tripped", []BotController{secondBot}); err != nil {
		t.Fatal(err)
	}
	secondSource := openingPauseGateSource(second.ownerID, "circuit_breaker")
	if err := first.SyncPersistentHolders(context.Background(), []BotController{firstBot}); err != nil {
		t.Fatal(err)
	}
	if !firstBot.hasGate(secondSource) {
		t.Fatal("first process did not install the second process's independent hold")
	}
	if resumed, err := first.ReleaseChecked("circuit_breaker", []BotController{firstBot}); err != nil || resumed {
		t.Fatalf("first process released while second owner remained: resumed=%v err=%v", resumed, err)
	}
	if firstBot.hasGate(firstSource) || !firstBot.hasGate(secondSource) {
		t.Fatalf("first process gates after releasing its owner: first=%v second=%v", firstBot.hasGate(firstSource), firstBot.hasGate(secondSource))
	}
	if err := second.SyncPersistentHolders(context.Background(), []BotController{secondBot}); err != nil {
		t.Fatal(err)
	}
	if secondBot.hasGate(firstSource) || !secondBot.hasGate(secondSource) {
		t.Fatalf("second process gates after remote release: first=%v second=%v", secondBot.hasGate(firstSource), secondBot.hasGate(secondSource))
	}
}

func TestOpeningPauseOwnerSyncFailureBlocksUntilAuthoritativeReadRecovers(t *testing.T) {
	store := &openingPauseStateTestStore{rows: make(map[string]string), loadErr: errors.New("database unavailable")}
	coordinator, err := NewOpeningPauseCoordinatorWithStore(context.Background(), store)
	if err == nil || coordinator == nil {
		t.Fatalf("expected a retained fail-closed coordinator, got coordinator=%v err=%v", coordinator != nil, err)
	}
	bot := &sourceOpeningPauseTestBot{safeMockBot: &safeMockBot{}, gates: make(map[string]bool)}
	if err := coordinator.SyncPersistentHolders(context.Background(), []BotController{bot}); err == nil {
		t.Fatal("unavailable shared state was accepted")
	}
	if !bot.hasGate(coordinator.syncHold) {
		t.Fatal("failed owner read did not block the running bot")
	}
	store.loadErr = nil
	if err := coordinator.SyncPersistentHolders(context.Background(), []BotController{bot}); err != nil {
		t.Fatal(err)
	}
	if bot.hasGate(coordinator.syncHold) || coordinator.IsHeldBy(coordinator.syncHold) {
		t.Fatal("successful authoritative read did not clear the temporary sync-failure gate")
	}
}

func TestOpeningPauseBackgroundSyncPropagatesRemoteHoldAndRelease(t *testing.T) {
	store := &openingPauseStateTestStore{rows: make(map[string]string)}
	consumer, err := NewOpeningPauseCoordinatorWithStore(context.Background(), store, "consumer")
	if err != nil {
		t.Fatal(err)
	}
	bot := &sourceOpeningPauseTestBot{safeMockBot: &safeMockBot{}, gates: make(map[string]bool)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	consumer.StartPersistentSync(ctx, func() []BotController { return []BotController{bot} })
	producer, err := NewOpeningPauseCoordinatorWithStore(context.Background(), store, "producer")
	if err != nil {
		t.Fatal(err)
	}
	if err := producer.Pause("circuit_breaker", "daily loss", nil); err != nil {
		t.Fatal(err)
	}
	remoteGate := openingPauseGateSource(producer.ownerID, "circuit_breaker")
	waitForOpeningPauseGate(t, bot, remoteGate, true)
	if !producer.Release("circuit_breaker", nil) {
		t.Fatal("producer could not release its own pause owner")
	}
	waitForOpeningPauseGate(t, bot, remoteGate, false)
}

func waitForOpeningPauseGate(t *testing.T, bot *sourceOpeningPauseTestBot, source string, wantHeld bool) {
	t.Helper()
	deadline := time.After(4 * time.Second)
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if bot.hasGate(source) == wantHeld {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("gate %q held=%v, want %v", source, bot.hasGate(source), wantHeld)
		case <-ticker.C:
		}
	}
}

func TestOpeningPauseCoordinatorFailsClosedOnRestoreOrReleaseError(t *testing.T) {
	store := &openingPauseStateTestStore{rows: map[string]string{"composite_risk": "stop"}, loadErr: errors.New("db unavailable")}
	if _, err := NewOpeningPauseCoordinatorWithStore(context.Background(), store); err == nil {
		t.Fatal("coordinator accepted an unverified persisted state")
	}
	store.loadErr = nil
	coordinator, err := NewOpeningPauseCoordinatorWithStore(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	store.deleteErr = errors.New("write unavailable")
	if coordinator.Release("composite_risk", nil) {
		t.Fatal("failed durable release reported an unheld state")
	}
	if !coordinator.IsHeldBy("composite_risk") {
		t.Fatal("failed durable release cleared the in-memory owner")
	}
}

func TestOpeningPauseCoordinatorKeepsLocalHoldWhenPersistenceFails(t *testing.T) {
	store := &openingPauseStateTestStore{rows: make(map[string]string), upsertErr: errors.New("write unavailable")}
	coordinator, err := NewOpeningPauseCoordinatorWithStore(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	if err := coordinator.Pause("circuit_breaker", "daily loss", nil); err == nil {
		t.Fatal("persistence failure was not reported")
	}
	if !coordinator.IsHeldBy("circuit_breaker") {
		t.Fatal("persistence failure discarded the in-process safety hold")
	}
}

func TestCircuitBreakerRecoveryFailsWhenPauseOwnerCannotBeDurablyReleased(t *testing.T) {
	store := &openingPauseStateTestStore{
		rows:      map[string]string{"circuit_breaker": "daily loss"},
		deleteErr: errors.New("write unavailable"),
	}
	coordinator, err := NewOpeningPauseCoordinatorWithStore(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	bot := &safeMockBot{}
	gcb := NewGlobalCircuitBreaker(newCircuitBreakerTestConfig(), nil, &circuitBreakerMockProvider{bots: []BotController{bot}})
	gcb.SetPauseCoordinator(coordinator)
	gcb.status = CircuitBreakerStatusTripped
	if err := gcb.ManualRecover("operator"); err == nil {
		t.Fatal("manual recovery hid durable source release failure")
	}
	if gcb.GetStatus() != CircuitBreakerStatusTripped || bot.resumes.Load() != 0 || !coordinator.IsHeldBy("circuit_breaker") {
		t.Fatalf("status=%s resumes=%d held=%v", gcb.GetStatus(), bot.resumes.Load(), coordinator.IsHeldBy("circuit_breaker"))
	}
}
