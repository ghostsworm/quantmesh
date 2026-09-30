package risk

import (
	"context"
	"errors"
	"sync"
	"testing"

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

func (b *sourceOpeningPauseTestBot) hasGate(source string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.gates[source]
}

type openingPauseStateTestStore struct {
	rows      map[string]string
	loadErr   error
	upsertErr error
	deleteErr error
}

func (s *openingPauseStateTestStore) LoadOpeningPauseHolders(context.Context) ([]storage.OpeningPauseHolder, error) {
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	rows := make([]storage.OpeningPauseHolder, 0, len(s.rows))
	for source, reason := range s.rows {
		rows = append(rows, storage.OpeningPauseHolder{Source: source, Reason: reason})
	}
	return rows, nil
}

func (s *openingPauseStateTestStore) UpsertOpeningPauseHolder(_ context.Context, holder storage.OpeningPauseHolder) error {
	if s.upsertErr != nil {
		return s.upsertErr
	}
	s.rows[holder.Source] = holder.Reason
	return nil
}

func (s *openingPauseStateTestStore) DeleteOpeningPauseHolder(_ context.Context, source string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	delete(s.rows, source)
	return nil
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

func TestOpeningPauseCoordinatorPersistsHoldAndReleaseAcrossInstances(t *testing.T) {
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
	if !second.Release("emergency_center", nil) {
		t.Fatal("durable source release was not applied")
	}
	third, err := NewOpeningPauseCoordinatorWithStore(context.Background(), store)
	if err != nil || third.IsHeldBy("emergency_center") {
		t.Fatalf("released coordinator held=%v err=%v", third != nil && third.IsHeldBy("emergency_center"), err)
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
