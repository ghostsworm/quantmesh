package strategy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"quantmesh/accounting"
	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/storage"
)

type durableRecoveryFinancialRPCSpy struct {
	*mockFCExchange
	borrowRPCs int
	repayRPCs  int
}

func (s *durableRecoveryFinancialRPCSpy) Borrow(ctx context.Context, asset string, amount float64) (int64, error) {
	s.borrowRPCs++
	return s.mockFCExchange.Borrow(ctx, asset, amount)
}

func (s *durableRecoveryFinancialRPCSpy) Repay(ctx context.Context, asset string, amount float64) (int64, error) {
	s.repayRPCs++
	return s.mockFCExchange.Repay(ctx, asset, amount)
}

type durableRecoveryProviderFixture struct {
	repository storage.FundingRecoveryStore
	generation storage.FundingCarryRuntimeGeneration
	bot        string
	scopes     []string
	err        error
}

type recoveryBindingRuntimeStateStore struct {
	RuntimeStateStore
	FundingCarryRecoveryProvider
}

type fundingCarryRecoveryLookupExchange struct {
	*mockFCExchange
	order *exchange.Order
	err   error
	calls int
}

func (e *fundingCarryRecoveryLookupExchange) GetOrderByClientOrderID(context.Context, string, string) (*exchange.Order, error) {
	e.calls++
	return e.order, e.err
}

func (p *durableRecoveryProviderFixture) FundingCarryRecoveryBinding() (storage.FundingRecoveryStore, storage.FundingCarryRuntimeGeneration, string, []string, error) {
	return p.repository, p.generation, p.bot, append([]string(nil), p.scopes...), p.err
}

func TestCurrentFundingCarryRecoveryBindingReadsExistingProviderWithoutClaiming(t *testing.T) {
	provider := &durableRecoveryProviderFixture{bot: "bot-1", scopes: []string{"scope-1"}}
	strategy := &FundingCarryStrategy{runtimeStateStore: recoveryBindingRuntimeStateStore{FundingCarryRecoveryProvider: provider}}
	store, generation, botID, scopes, err := strategy.CurrentFundingCarryRecoveryBinding()
	if err != nil || store != nil || generation.ID() != "" || botID != "bot-1" || len(scopes) != 1 || scopes[0] != "scope-1" {
		t.Fatalf("read current binding = (%v, %q, %q, %v, %v), want provider snapshot", store, generation.ID(), botID, scopes, err)
	}
}

func TestCurrentFundingCarryRecoveryBindingFailsClosedWithoutProvider(t *testing.T) {
	if _, _, _, _, err := (*FundingCarryStrategy)(nil).CurrentFundingCarryRecoveryBinding(); !errors.Is(err, ErrFundingCarryDurableRecoveryUnavailable) {
		t.Fatalf("nil strategy binding error = %v, want unavailable", err)
	}
	strategy := &FundingCarryStrategy{}
	if _, _, _, _, err := strategy.CurrentFundingCarryRecoveryBinding(); !errors.Is(err, ErrFundingCarryDurableRecoveryUnavailable) {
		t.Fatalf("missing provider binding error = %v, want unavailable", err)
	}
}

type durableRecoveryRepositoryFixture struct {
	storage.FundingRecoveryStore
	mu                  sync.Mutex
	episode             accounting.RecoveryEpisode
	outbox              accounting.RecoveryOutbox
	holds               []accounting.RecoveryHold
	listErr             error
	episodeErr          error
	markErr             error
	markCommitThenError bool
	readbackErr         error
	marks               int
}

type multiEpisodeProjectionRepository struct {
	storage.FundingRecoveryStore
	mu      sync.Mutex
	episode map[string]accounting.RecoveryEpisode
	outbox  map[string]accounting.RecoveryOutbox
	marks   int
}

func (r *multiEpisodeProjectionRepository) GetRecoveryEpisode(_ context.Context, episodeID string) (accounting.RecoveryEpisode, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	episode, found := r.episode[episodeID]
	return episode, found, nil
}

func (r *multiEpisodeProjectionRepository) ListRecoveryOutboxes(_ context.Context, episodeID string) ([]accounting.RecoveryOutbox, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var result []accounting.RecoveryOutbox
	for _, outbox := range r.outbox {
		if outbox.EpisodeID == episodeID {
			result = append(result, outbox)
		}
	}
	return result, nil
}

func (r *multiEpisodeProjectionRepository) GetRecoveryOutbox(_ context.Context, episodeID, operationID string) (accounting.RecoveryOutbox, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	outbox, found := r.outbox[episodeID+"\x00"+operationID]
	return outbox, found, nil
}

func (r *multiEpisodeProjectionRepository) GetRecoveryReceipt(_ context.Context, episodeID, operationID string) (accounting.RecoveryReceipt, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	outbox, found := r.outbox[episodeID+"\x00"+operationID]
	if !found {
		return accounting.RecoveryReceipt{}, false, nil
	}
	state, revision := accounting.RecoveryLedgerCommitted, outbox.TargetEpisodeRevision
	if outbox.State == accounting.RecoveryProjected {
		state, revision = accounting.RecoveryProjected, outbox.TargetEpisodeRevision+1
	}
	var projection fundingCarryRecoveryProjectionPayload
	if err := json.Unmarshal(outbox.ProjectionPayload, &projection); err != nil {
		return accounting.RecoveryReceipt{}, false, err
	}
	return accounting.RecoveryReceipt{EpisodeID: episodeID, OperationID: operationID, State: state, EpisodeRevision: revision,
		ProjectionHash: outbox.ProjectionHash, OwnerGeneration: projection.Episode.OwnerGeneration,
		RecoveryGeneration: projection.Episode.RecoveryGeneration, RequestHash: "test-multi-episode-request-hash"}, true, nil
}

func (r *multiEpisodeProjectionRepository) MarkRecoveryProjected(_ context.Context, generation storage.FundingCarryRuntimeGeneration, outbox accounting.RecoveryOutbox) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := outbox.EpisodeID + "\x00" + outbox.OperationID
	current, found := r.outbox[key]
	if !found || generation.ID() == "" || current.State != accounting.RecoveryProjectionPending ||
		outbox.State != accounting.RecoveryProjected || outbox.AppliedOperationID != current.OperationID ||
		outbox.AppliedRevision != current.TargetEpisodeRevision || outbox.AppliedHash != current.ProjectionHash {
		return errors.New("invalid per-episode projected receipt")
	}
	current.State = outbox.State
	current.AppliedOperationID = outbox.AppliedOperationID
	current.AppliedRevision = outbox.AppliedRevision
	current.AppliedHash = outbox.AppliedHash
	r.outbox[key] = current
	r.marks++
	return nil
}

func (r *durableRecoveryRepositoryFixture) ListPendingRecoveryOutbox(context.Context) ([]accounting.RecoveryOutbox, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.listErr != nil {
		return nil, r.listErr
	}
	if r.outbox.State != accounting.RecoveryProjectionPending {
		return nil, nil
	}
	return []accounting.RecoveryOutbox{r.outbox}, nil
}

func (r *durableRecoveryRepositoryFixture) ListActiveRecoveryHolds(context.Context) ([]accounting.RecoveryHold, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]accounting.RecoveryHold(nil), r.holds...), nil
}

func (r *durableRecoveryRepositoryFixture) GetRecoveryEpisode(context.Context, string) (accounting.RecoveryEpisode, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.episodeErr != nil {
		return accounting.RecoveryEpisode{}, false, r.episodeErr
	}
	return r.episode, r.episode.ID != "", nil
}

func (r *durableRecoveryRepositoryFixture) GetRecoveryReceipt(_ context.Context, episodeID, operationID string) (accounting.RecoveryReceipt, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, takeover := range r.episode.OwnerTakeovers {
		if takeover.OperationID == operationID && r.episode.ID == episodeID {
			authorization := accounting.RecoveryOwnerTakeoverAuthorization{EpisodeID: episodeID, OperationID: operationID, Actor: takeover.Actor,
				AuditRef: takeover.AuditRef, ExpectedOwnerGeneration: takeover.PreviousOwnerGeneration,
				ExpectedEpisodeRevision: takeover.EpisodeRevision - 1, Confirmed: true}
			requestHash, err := storage.ExpectedFundingRecoveryOwnerTakeoverRequestHash(takeover.OwnerGeneration, r.episode.AccountScopes, authorization)
			if err != nil {
				return accounting.RecoveryReceipt{}, false, err
			}
			return accounting.RecoveryReceipt{EpisodeID: episodeID, OperationID: operationID, State: accounting.RecoveryBlocked,
				EpisodeRevision: takeover.EpisodeRevision, PreviousOwnerGeneration: takeover.PreviousOwnerGeneration, OwnerGeneration: takeover.OwnerGeneration,
				RecoveryGeneration: r.episode.RecoveryGeneration, RequestHash: requestHash, Actor: takeover.Actor, AuditRef: takeover.AuditRef,
				EvidenceHash: takeover.EvidenceHash, ProjectionHash: takeover.EvidenceHash, CreatedAt: takeover.CreatedAt.Add(time.Nanosecond)}, true, nil
		}
	}
	if r.outbox.EpisodeID == episodeID && r.outbox.OperationID == operationID {
		var projection fundingCarryRecoveryProjectionPayload
		if err := json.Unmarshal(r.outbox.ProjectionPayload, &projection); err != nil {
			return accounting.RecoveryReceipt{}, false, err
		}
		state, revision := accounting.RecoveryLedgerCommitted, r.outbox.TargetEpisodeRevision
		if r.outbox.State == accounting.RecoveryProjected {
			state, revision = accounting.RecoveryProjected, r.outbox.TargetEpisodeRevision+1
		}
		return accounting.RecoveryReceipt{EpisodeID: episodeID, OperationID: operationID, State: state, EpisodeRevision: revision,
			ProjectionHash: r.outbox.ProjectionHash, OwnerGeneration: projection.Episode.OwnerGeneration,
			RecoveryGeneration: projection.Episode.RecoveryGeneration, RequestHash: "test-outbox-request-hash"}, true, nil
	}
	return accounting.RecoveryReceipt{}, false, nil
}

func (r *durableRecoveryRepositoryFixture) ListRecoveryOutboxes(_ context.Context, episodeID string) ([]accounting.RecoveryOutbox, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if episodeID != r.outbox.EpisodeID || r.outbox.OperationID == "" {
		return nil, nil
	}
	return []accounting.RecoveryOutbox{r.outbox}, nil
}

func (r *durableRecoveryRepositoryFixture) GetRecoveryHold(_ context.Context, episodeID string, generation uint64) (accounting.RecoveryHold, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, hold := range r.holds {
		if hold.EpisodeID == episodeID && hold.Generation == generation {
			return hold, true, nil
		}
	}
	return accounting.RecoveryHold{}, false, nil
}

func (r *durableRecoveryRepositoryFixture) ListRecoverySubmissionAttempts(context.Context, string) ([]accounting.RecoverySubmissionAttempt, error) {
	return nil, nil
}

func (r *durableRecoveryRepositoryFixture) MarkRecoveryProjected(_ context.Context, generation storage.FundingCarryRuntimeGeneration, outbox accounting.RecoveryOutbox) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.markErr != nil {
		return r.markErr
	}
	if generation.ID() == "" || outbox.State != accounting.RecoveryProjected {
		return errors.New("invalid projected receipt")
	}
	if r.outbox.State != accounting.RecoveryProjectionPending {
		if r.outbox.State == outbox.State && r.outbox.AppliedOperationID == outbox.AppliedOperationID &&
			r.outbox.AppliedRevision == outbox.AppliedRevision && r.outbox.AppliedHash == outbox.AppliedHash {
			return errors.New("projection CAS already committed")
		}
		return errors.New("projection receipt conflicts with committed outbox")
	}
	r.marks++
	r.outbox = outbox
	r.episode.Revision++
	r.episode.State = accounting.RecoveryProjected
	r.episode.UpdatedAt = time.Now().UTC()
	if r.markCommitThenError {
		r.markCommitThenError = false
		return errors.New("projection commit response lost")
	}
	return nil
}

func (r *durableRecoveryRepositoryFixture) GetRecoveryOutbox(context.Context, string, string) (accounting.RecoveryOutbox, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.readbackErr != nil {
		return accounting.RecoveryOutbox{}, false, r.readbackErr
	}
	return r.outbox, r.outbox.OperationID != "", nil
}

type durableRecoveryRuntimeFixture struct {
	mu                 sync.Mutex
	version            int
	payload            string
	found              bool
	readErr            error
	casErr             error
	readbackErr        error
	conflict           bool
	casCommitThenError error
	compareCalls       int
	readCalls          int
	casEntered         chan struct{}
	casRelease         chan struct{}
}

func (r *durableRecoveryRuntimeFixture) LoadRuntimeState(string) (int, string, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.version, r.payload, r.found, r.readErr
}

func (r *durableRecoveryRuntimeFixture) SaveRuntimeState(string, int, string) error {
	return errors.New("unexpected non-conditional runtime write")
}

func (r *durableRecoveryRuntimeFixture) LoadRuntimeStateContext(context.Context, string) (int, string, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.readCalls++
	if r.readCalls > 1 && r.readbackErr != nil {
		return 0, "", false, r.readbackErr
	}
	return r.version, r.payload, r.found, r.readErr
}

func (r *durableRecoveryRuntimeFixture) CompareAndSwapRuntimeState(_ context.Context, _ string, expectedVersion int, expectedPayload string, nextVersion int, nextPayload string) (bool, error) {
	if r.casEntered != nil {
		r.casEntered <- struct{}{}
		<-r.casRelease
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.compareCalls++
	if r.casErr != nil {
		return false, r.casErr
	}
	if r.conflict || r.version != expectedVersion || r.payload != expectedPayload {
		return false, nil
	}
	r.version, r.payload, r.found = nextVersion, nextPayload, true
	if r.casCommitThenError != nil {
		return true, r.casCommitThenError
	}
	return true, nil
}

type durableRecoveryFixture struct {
	strategy   *FundingCarryStrategy
	repository *durableRecoveryRepositoryFixture
	runtime    *durableRecoveryRuntimeFixture
	ctx        context.Context
}

func newDurableRecoveryFixture(t *testing.T) durableRecoveryFixture {
	t.Helper()
	ctx := context.Background()
	scope := fmt.Sprintf("%064x", 31)
	sqlStore, err := storage.NewSQLStorage(t.TempDir() + "/generation.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlStore.Close() })
	generation, err := sqlStore.ClaimFundingCarryRuntimeGeneration(ctx, []string{scope})
	if err != nil {
		t.Fatalf("claim test runtime generation: %v", err)
	}

	bot := "bot-funding-carry"
	episode := accounting.RecoveryEpisode{
		ID: "episode-startup-1", Bot: bot, Strategy: "funding_carry", AccountScopes: []string{scope},
		OwnerGeneration: generation.ID(), Revision: 2, RecoveryGeneration: 1,
		State: accounting.RecoveryLedgerCommitted,
	}
	event := accounting.RecoveryEvent{
		ID: "event-fee-1", Source: "venue", Exchange: "binance", Market: "spot", AccountScope: scope,
		Kind: accounting.RecoveryFeeEvent, ExternalID: "commission-1", EpisodeID: episode.ID,
		Asset: "USDT", Amount: "0", SourceRevision: "r1", SourceHash: "hash-r1",
		ObservedAt: time.Date(2026, 10, 12, 1, 0, 0, 0, time.UTC),
	}
	from := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	through := from.Add(time.Hour)
	cursor := &accounting.RecoverySourceCursor{RecoverySourceWindow: accounting.RecoverySourceWindow{
		Source: "binance_futures_income_v1", Exchange: "binance", Market: "futures", AccountScope: scope,
		Kind: accounting.RecoveryFundingEvent, Symbol: "BTCUSDT", Asset: "USDT", From: from, Through: through,
		Rows: 0, TotalRows: 0, CompletionMarker: accounting.RecoverySourceWindowCompleteV1,
		CollectorEvidenceHash: fmt.Sprintf("%064x", 8), InitialFrom: from, HistoricalEvidenceHash: fmt.Sprintf("%064x", 9),
		EvidenceHash: fmt.Sprintf("%064x", 7),
	}, EpisodeID: episode.ID, Revision: 1, UpdatedAt: through}
	baseState := fundingCarryRuntimeState{
		Strategy: "funding_carry", FuturesExchange: "binance", SpotExchange: "binance", Symbol: "BTCUSDT",
		OwnershipReady: true, RecoveryProjectionSchema: fundingCarryRecoveryProjectionSchemaVersion,
	}
	statePayload, err := json.Marshal(baseState)
	if err != nil {
		t.Fatal(err)
	}
	expectedRuntimeRevision := fundingCarryRuntimeRevision(fundingCarryRuntimeStateVersion, string(statePayload))
	payload, err := json.Marshal(fundingCarryRecoveryProjectionPayload{Episode: episode, Events: []accounting.RecoveryEvent{event}, SourceCursor: cursor,
		ExpectedRuntimeRevision: expectedRuntimeRevision})
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(payload)
	runtime := &durableRecoveryRuntimeFixture{version: fundingCarryRuntimeStateVersion, payload: string(statePayload), found: true}
	outbox := accounting.RecoveryOutbox{
		EpisodeID: episode.ID, OperationID: "operation-1", ProjectionPayload: payload,
		ExpectedRuntimeRevision: expectedRuntimeRevision,
		TargetEpisodeRevision:   episode.Revision, ProjectionHash: hex.EncodeToString(hash[:]),
		State: accounting.RecoveryProjectionPending,
	}
	repository := &durableRecoveryRepositoryFixture{episode: episode, outbox: outbox}
	provider := &durableRecoveryProviderFixture{repository: repository, generation: generation, bot: bot, scopes: []string{scope}}
	futures := &mockFCExchange{name: "binance", marketType: "futures"}
	spot := &mockFCExchange{name: "binance", marketType: "spot"}
	strategy := NewFundingCarryStrategy("funding_carry", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, futures, spot, nil, nil)
	strategy.SetRuntimeStateStore(&durableRecoveryRuntimeBindingFixture{RuntimeStateStore: runtime, provider: provider})
	return durableRecoveryFixture{strategy: strategy, repository: repository, runtime: runtime, ctx: ctx}
}

type durableRecoveryRuntimeBindingFixture struct {
	RuntimeStateStore
	provider FundingCarryRecoveryProvider
}

func (r *durableRecoveryRuntimeBindingFixture) FundingCarryRecoveryBinding() (storage.FundingRecoveryStore, storage.FundingCarryRuntimeGeneration, string, []string, error) {
	return r.provider.FundingCarryRecoveryBinding()
}

func (r *durableRecoveryRuntimeBindingFixture) LoadRuntimeStateContext(ctx context.Context, name string) (int, string, bool, error) {
	return r.RuntimeStateStore.(RuntimeStateContextReader).LoadRuntimeStateContext(ctx, name)
}

func (r *durableRecoveryRuntimeBindingFixture) CompareAndSwapRuntimeState(ctx context.Context, name string, expectedVersion int, expectedPayload string, nextVersion int, nextPayload string) (bool, error) {
	return r.RuntimeStateStore.(RuntimeStateConditionalWriter).CompareAndSwapRuntimeState(ctx, name, expectedVersion, expectedPayload, nextVersion, nextPayload)
}

func TestFundingCarryStartupProjectsDurableOutboxAndReadsBackBeforeMarking(t *testing.T) {
	fixture := newDurableRecoveryFixture(t)
	var originalProjection fundingCarryRecoveryProjectionPayload
	if err := json.Unmarshal(fixture.repository.outbox.ProjectionPayload, &originalProjection); err != nil || originalProjection.SourceCursor == nil {
		t.Fatalf("fixture outbox did not contain its source cursor: projection=%+v err=%v", originalProjection, err)
	}
	expectedCursor := *originalProjection.SourceCursor
	if err := fixture.strategy.reconcileDurableFundingCarryRecovery(fixture.ctx); err != nil {
		t.Fatalf("reconcile durable projection: %v", err)
	}
	var projected fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(fixture.runtime.payload), &projected); err != nil {
		t.Fatal(err)
	}
	if len(projected.RecoveryProjectionMarkers) != 1 || projected.RecoveryProjectionMarkers[0].OperationID != "operation-1" {
		t.Fatalf("projection marker not persisted: %+v", projected.RecoveryProjectionMarkers)
	}
	if len(projected.RecoveryFacts) != 1 || projected.RecoveryFacts[0].ID != "event-fee-1" {
		t.Fatalf("projection event not persisted: %+v", projected.RecoveryFacts)
	}
	if len(projected.RecoverySourceCursors) != 1 || projected.RecoverySourceCursors[0] != expectedCursor {
		t.Fatalf("source cursor was not atomically projected: %+v", projected.RecoverySourceCursors)
	}
	if fixture.repository.marks != 1 || fixture.repository.outbox.State != accounting.RecoveryProjected ||
		fixture.repository.outbox.AppliedOperationID != "operation-1" || fixture.repository.outbox.AppliedRevision != 2 ||
		fixture.repository.outbox.AppliedHash != fixture.repository.outbox.ProjectionHash {
		t.Fatalf("projected receipt not marked after readback: marks=%d outbox=%+v", fixture.repository.marks, fixture.repository.outbox)
	}
	if err := fixture.strategy.reconcileDurableFundingCarryRecovery(fixture.ctx); err != nil {
		t.Fatalf("restart retry must accept the matching cursor projection: %v", err)
	}
	var retried fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(fixture.runtime.payload), &retried); err != nil || len(retried.RecoverySourceCursors) != 1 || retried.RecoverySourceCursors[0] != expectedCursor {
		t.Fatalf("restart retry duplicated or lost source cursor: cursors=%+v err=%v", retried.RecoverySourceCursors, err)
	}
}

func TestFundingCarryRecoverySourceCursorCannotBeResetAcrossEpisodes(t *testing.T) {
	from := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	first := accounting.RecoverySourceCursor{RecoverySourceWindow: accounting.RecoverySourceWindow{
		Source: "source", Exchange: "binance", Market: "futures", AccountScope: "scope",
		Kind: accounting.RecoveryFundingEvent, Symbol: "BTCUSDT", Asset: "USDT", From: from, Through: from.Add(time.Hour),
		Rows: 0, TotalRows: 0, CompletionMarker: accounting.RecoverySourceWindowCompleteV1, EvidenceHash: fmt.Sprintf("%064x", 8),
	}, EpisodeID: "episode-old", Revision: 1, UpdatedAt: from.Add(time.Hour)}
	state, err := appendFundingCarryRecoveryCursor(fundingCarryRuntimeState{}, &first)
	if err != nil {
		t.Fatal(err)
	}
	second := first
	second.EpisodeID = "episode-new"
	second.RecoverySourceWindow.From = first.Through
	second.RecoverySourceWindow.Through = first.Through.Add(time.Hour)
	second.RecoverySourceWindow.ExpectedRevision = first.Revision
	second.RecoverySourceWindow.EvidenceHash = fmt.Sprintf("%064x", 9)
	second.Revision++
	second.UpdatedAt = second.Through
	if _, err := appendFundingCarryRecoveryCursor(state, &second); err == nil {
		t.Fatal("new episode silently reset or took over an existing source cursor")
	}
	if len(state.RecoverySourceCursors) != 1 || state.RecoverySourceCursors[0] != first {
		t.Fatalf("failed lifecycle handoff mutated the prior cursor: %+v", state.RecoverySourceCursors)
	}
}

func TestFundingCarrySameEpisodePendingPredecessorBlocksNewerOutbox(t *testing.T) {
	fixture := newDurableRecoveryFixture(t)
	baseRevision := fundingCarryRuntimeRevision(fixture.runtime.version, fixture.runtime.payload)
	episode, newer := makeSharedSnapshotProjection(fixture, "episode-startup-1", "operation-newer", 3, baseRevision)
	older := accounting.RecoveryOutbox{
		EpisodeID: episode.ID, OperationID: "operation-older", TargetEpisodeRevision: 2,
		ProjectionHash: fmt.Sprintf("%064x", 99), State: accounting.RecoveryProjectionPending,
	}
	repository := &multiEpisodeProjectionRepository{episode: map[string]accounting.RecoveryEpisode{episode.ID: episode}, outbox: map[string]accounting.RecoveryOutbox{
		older.EpisodeID + "\x00" + older.OperationID: older,
		newer.EpisodeID + "\x00" + newer.OperationID: newer,
	}}
	binding := sharedSnapshotBinding(fixture, repository)
	if err := fixture.strategy.projectFundingCarryRecoveryOutbox(fixture.ctx, binding, episode, newer); err == nil {
		t.Fatal("newer outbox projected while an earlier same-episode outbox remained pending")
	}
	if fixture.runtime.compareCalls != 0 || repository.marks != 0 {
		t.Fatalf("predecessor fence attempted projection: CAS=%d marks=%d", fixture.runtime.compareCalls, repository.marks)
	}
}

func TestFundingCarryDifferentEpisodesRebaseSharedRuntimeSnapshotIdempotently(t *testing.T) {
	fixture := newDurableRecoveryFixture(t)
	baseRevision := fundingCarryRuntimeRevision(fixture.runtime.version, fixture.runtime.payload)
	episodeA, outboxA := makeSharedSnapshotProjection(fixture, "episode-a", "operation-a", 2, baseRevision)
	episodeB, outboxB := makeSharedSnapshotProjection(fixture, "episode-b", "operation-b", 2, baseRevision)
	repository := &multiEpisodeProjectionRepository{episode: map[string]accounting.RecoveryEpisode{
		episodeA.ID: episodeA, episodeB.ID: episodeB,
	}, outbox: map[string]accounting.RecoveryOutbox{
		outboxA.EpisodeID + "\x00" + outboxA.OperationID: outboxA,
		outboxB.EpisodeID + "\x00" + outboxB.OperationID: outboxB,
	}}
	binding := sharedSnapshotBinding(fixture, repository)
	if err := fixture.strategy.projectFundingCarryRecoveryOutbox(fixture.ctx, binding, episodeA, outboxA); err != nil {
		t.Fatalf("project first episode: %v", err)
	}
	fixture.runtime.casCommitThenError = errors.New("runtime CAS response lost after commit")
	if err := fixture.strategy.projectFundingCarryRecoveryOutbox(fixture.ctx, binding, episodeB, outboxB); err == nil {
		t.Fatal("response loss should leave the second outbox pending until explicit retry")
	}
	fixture.runtime.casCommitThenError = nil
	if err := fixture.strategy.projectFundingCarryRecoveryOutbox(fixture.ctx, binding, episodeB, outboxB); err != nil {
		t.Fatalf("retry cross-episode projection after response loss: %v", err)
	}
	var state fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(fixture.runtime.payload), &state); err != nil {
		t.Fatal(err)
	}
	if len(state.RecoveryProjectionMarkers) != 2 || len(state.RecoveryFacts) != 2 || repository.marks != 2 {
		t.Fatalf("cross-episode retry duplicated or lost projection: markers=%d events=%d marks=%d", len(state.RecoveryProjectionMarkers), len(state.RecoveryFacts), repository.marks)
	}
	for _, marker := range state.RecoveryProjectionMarkers {
		if marker.ExpectedRuntimeRevision != baseRevision || marker.RuntimeBaseRevision == "" {
			t.Fatalf("projection did not preserve original revision and audit actual CAS base: %+v", marker)
		}
	}
	if outboxB.ExpectedRuntimeRevision != baseRevision || repository.outbox[outboxB.EpisodeID+"\x00"+outboxB.OperationID].ExpectedRuntimeRevision != baseRevision {
		t.Fatal("rebase mutated immutable outbox expected runtime revision")
	}
}

func TestFundingCarryConcurrentEpisodesCompetingForSameSnapshotRebaseAfterCASLoss(t *testing.T) {
	fixture := newDurableRecoveryFixture(t)
	baseRevision := fundingCarryRuntimeRevision(fixture.runtime.version, fixture.runtime.payload)
	episodeA, outboxA := makeSharedSnapshotProjection(fixture, "episode-concurrent-a", "operation-concurrent-a", 2, baseRevision)
	episodeB, outboxB := makeSharedSnapshotProjection(fixture, "episode-concurrent-b", "operation-concurrent-b", 2, baseRevision)
	repository := &multiEpisodeProjectionRepository{episode: map[string]accounting.RecoveryEpisode{
		episodeA.ID: episodeA, episodeB.ID: episodeB,
	}, outbox: map[string]accounting.RecoveryOutbox{
		outboxA.EpisodeID + "\x00" + outboxA.OperationID: outboxA,
		outboxB.EpisodeID + "\x00" + outboxB.OperationID: outboxB,
	}}
	binding := sharedSnapshotBinding(fixture, repository)
	fixture.runtime.casEntered = make(chan struct{}, 2)
	fixture.runtime.casRelease = make(chan struct{})
	results := make(chan error, 2)
	go func() {
		results <- fixture.strategy.projectFundingCarryRecoveryOutbox(fixture.ctx, binding, episodeA, outboxA)
	}()
	go func() {
		results <- fixture.strategy.projectFundingCarryRecoveryOutbox(fixture.ctx, binding, episodeB, outboxB)
	}()
	<-fixture.runtime.casEntered
	<-fixture.runtime.casEntered
	close(fixture.runtime.casRelease)
	for range 2 {
		if err := <-results; err != nil {
			t.Errorf("competing episode projection did not safely complete after CAS retry: %v", err)
		}
	}
	var state fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(fixture.runtime.payload), &state); err != nil {
		t.Fatal(err)
	}
	if len(state.RecoveryProjectionMarkers) != 2 || len(state.RecoveryFacts) != 2 || repository.marks != 2 {
		t.Fatalf("CAS race lost or duplicated one episode: markers=%d events=%d marks=%d", len(state.RecoveryProjectionMarkers), len(state.RecoveryFacts), repository.marks)
	}
}

func TestFundingCarryDifferentEpisodeRebaseRejectsUntrackedRuntimeMutation(t *testing.T) {
	fixture := newDurableRecoveryFixture(t)
	baseRevision := fundingCarryRuntimeRevision(fixture.runtime.version, fixture.runtime.payload)
	episodeA, outboxA := makeSharedSnapshotProjection(fixture, "episode-drift-a", "operation-drift-a", 2, baseRevision)
	episodeB, outboxB := makeSharedSnapshotProjection(fixture, "episode-drift-b", "operation-drift-b", 2, baseRevision)
	repository := &multiEpisodeProjectionRepository{episode: map[string]accounting.RecoveryEpisode{
		episodeA.ID: episodeA, episodeB.ID: episodeB,
	}, outbox: map[string]accounting.RecoveryOutbox{
		outboxA.EpisodeID + "\x00" + outboxA.OperationID: outboxA,
		outboxB.EpisodeID + "\x00" + outboxB.OperationID: outboxB,
	}}
	binding := sharedSnapshotBinding(fixture, repository)
	if err := fixture.strategy.projectFundingCarryRecoveryOutbox(fixture.ctx, binding, episodeA, outboxA); err != nil {
		t.Fatalf("project first episode: %v", err)
	}
	var changed fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(fixture.runtime.payload), &changed); err != nil {
		t.Fatal(err)
	}
	changed.OwnershipReady = !changed.OwnershipReady
	changedPayload, err := json.Marshal(changed)
	if err != nil {
		t.Fatal(err)
	}
	fixture.runtime.mu.Lock()
	fixture.runtime.payload = string(changedPayload)
	fixture.runtime.mu.Unlock()

	if err := fixture.strategy.projectFundingCarryRecoveryOutbox(fixture.ctx, binding, episodeB, outboxB); err == nil {
		t.Fatal("projection rebased across an untracked runtime mutation")
	}
	if repository.marks != 1 {
		t.Fatalf("untracked runtime mutation must leave the second outbox pending; marks=%d", repository.marks)
	}
}

func TestFundingCarryDifferentEpisodeRebaseRejectsCursorIdentityConflict(t *testing.T) {
	fixture := newDurableRecoveryFixture(t)
	baseRevision := fundingCarryRuntimeRevision(fixture.runtime.version, fixture.runtime.payload)
	episodeA, outboxA := makeSharedSnapshotProjection(fixture, "episode-cursor-a", "operation-cursor-a", 2, baseRevision)
	episodeB, outboxB := makeSharedSnapshotProjection(fixture, "episode-cursor-b", "operation-cursor-b", 2, baseRevision)
	from := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	window := accounting.RecoverySourceWindow{
		Source: "binance_margin_interest", Exchange: "binance", Market: "spot_margin", AccountScope: episodeA.AccountScopes[0],
		Kind: accounting.RecoveryInterestEvent, Asset: "USDT", From: from, Through: from.Add(time.Hour),
		Rows: 0, TotalRows: 0, CompletionMarker: accounting.RecoverySourceWindowCompleteV1, EvidenceHash: fmt.Sprintf("%064x", 77),
	}
	projectionA := fundingCarryRecoveryProjectionPayload{Episode: episodeA, SourceCursor: &accounting.RecoverySourceCursor{
		RecoverySourceWindow: window, EpisodeID: episodeA.ID, Revision: 1, UpdatedAt: window.Through,
	}}
	projectionB := fundingCarryRecoveryProjectionPayload{Episode: episodeB, SourceCursor: &accounting.RecoverySourceCursor{
		RecoverySourceWindow: window, EpisodeID: episodeB.ID, Revision: 1, UpdatedAt: window.Through,
	}}
	outboxA = replaceSharedProjection(outboxA, projectionA)
	outboxB = replaceSharedProjection(outboxB, projectionB)
	repository := &multiEpisodeProjectionRepository{episode: map[string]accounting.RecoveryEpisode{
		episodeA.ID: episodeA, episodeB.ID: episodeB,
	}, outbox: map[string]accounting.RecoveryOutbox{
		outboxA.EpisodeID + "\x00" + outboxA.OperationID: outboxA,
		outboxB.EpisodeID + "\x00" + outboxB.OperationID: outboxB,
	}}
	binding := sharedSnapshotBinding(fixture, repository)
	if err := fixture.strategy.projectFundingCarryRecoveryOutbox(fixture.ctx, binding, episodeA, outboxA); err != nil {
		t.Fatalf("project first source cursor: %v", err)
	}
	if err := fixture.strategy.projectFundingCarryRecoveryOutbox(fixture.ctx, binding, episodeB, outboxB); err == nil {
		t.Fatal("different episode reused a source cursor identity")
	}
	if repository.marks != 1 {
		t.Fatalf("cursor identity conflict must keep second outbox pending; marks=%d", repository.marks)
	}
}

func TestFundingCarryRuntimeRevisionRequiresCanonicalToken(t *testing.T) {
	hash := fmt.Sprintf("%064x", 1)
	valid := fmt.Sprintf("%d:%s", fundingCarryRuntimeStateVersion, hash)
	for _, token := range []string{
		valid,
		"+" + strconv.Itoa(fundingCarryRuntimeStateVersion) + ":" + hash,
		"0" + strconv.Itoa(fundingCarryRuntimeStateVersion) + ":" + hash,
		strconv.Itoa(fundingCarryRuntimeStateVersion-1) + ":" + hash,
		strconv.Itoa(fundingCarryRuntimeStateVersion) + ":" + hash[:63],
		strconv.Itoa(fundingCarryRuntimeStateVersion) + ":" + strings.ToUpper(hash),
	} {
		wantValid := token == valid
		if got := validFundingCarryRuntimeRevision(token); got != wantValid {
			t.Errorf("validFundingCarryRuntimeRevision(%q)=%v, want %v", token, got, wantValid)
		}
	}
}

func replaceSharedProjection(outbox accounting.RecoveryOutbox, projection fundingCarryRecoveryProjectionPayload) accounting.RecoveryOutbox {
	projection.ExpectedRuntimeRevision = outbox.ExpectedRuntimeRevision
	payload, _ := json.Marshal(projection)
	hash := sha256.Sum256(payload)
	outbox.ProjectionPayload = payload
	outbox.ProjectionHash = hex.EncodeToString(hash[:])
	return outbox
}

func TestFundingCarryRecoveryProjectionRetryAcceptsOnlySameEpisodeCursorAdvance(t *testing.T) {
	from := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	first := accounting.RecoverySourceCursor{RecoverySourceWindow: accounting.RecoverySourceWindow{
		Source: "source", Exchange: "binance", Market: "futures", AccountScope: "scope",
		Kind: accounting.RecoveryFundingEvent, Symbol: "BTCUSDT", Asset: "USDT", From: from, Through: from.Add(time.Hour),
		Rows: 0, TotalRows: 0, CompletionMarker: accounting.RecoverySourceWindowCompleteV1, EvidenceHash: fmt.Sprintf("%064x", 10),
	}, EpisodeID: "episode-a", Revision: 1, UpdatedAt: from.Add(time.Hour)}
	second := first
	second.RecoverySourceWindow.From = first.Through
	second.RecoverySourceWindow.Through = first.Through.Add(time.Hour)
	second.RecoverySourceWindow.ExpectedRevision = first.Revision
	second.RecoverySourceWindow.EvidenceHash = fmt.Sprintf("%064x", 11)
	second.Revision++
	second.UpdatedAt = second.Through
	if !fundingCarryRecoveryCursorProjectedAtLeast([]accounting.RecoverySourceCursor{second}, &first) {
		t.Fatal("retry of an already projected operation must accept its same-episode durable cursor successor")
	}
	second.EpisodeID = "episode-b"
	if fundingCarryRecoveryCursorProjectedAtLeast([]accounting.RecoverySourceCursor{second}, &first) {
		t.Fatal("cursor advancement across episode ownership must not be treated as an idempotent retry")
	}
}

func TestFundingCarryOldOwnerOutboxProjectsOnlyAfterAuditedTakeover(t *testing.T) {
	fixture := newDurableRecoveryFixture(t)
	prior := fixture.repository.episode
	prior.OwnerGeneration = "prior-runtime-generation"
	fixture.repository.episode = prior
	projection := fundingCarryRecoveryProjectionPayload{
		Episode: prior,
		Events: []accounting.RecoveryEvent{{
			ID: "event-borrow-1", Source: "operator", Exchange: "binance", Market: "spot_margin", AccountScope: prior.AccountScopes[0],
			Kind: accounting.RecoveryBorrowEvent, ExternalID: "borrow-1", EpisodeID: prior.ID, Asset: "BTC", Amount: "0.1",
			SourceRevision: "r1", SourceHash: fmt.Sprintf("%064x", 1), ObservedAt: time.Date(2026, 10, 12, 1, 0, 0, 0, time.UTC),
			DebtEvidence: &accounting.RecoveryDebtEvidence{PrincipalRaw: "0.1", InterestRaw: "0"},
		}},
	}
	fixture.setProjection(projection)
	fixture.repository.holds = []accounting.RecoveryHold{{
		ID: prior.ID + ":1", EpisodeID: prior.ID, Bot: prior.Bot, Strategy: prior.Strategy,
		AccountScopes: append([]string(nil), prior.AccountScopes...), Generation: prior.RecoveryGeneration, Revision: 1, State: "held",
	}}
	if err := fixture.strategy.reconcileDurableFundingCarryRecovery(fixture.ctx); err == nil {
		t.Fatal("old owner outbox must retain a recovery-only startup instead of projecting before explicit takeover")
	} else {
		var required *FundingCarryDurableRecoveryRequiredError
		if !errors.As(err, &required) {
			t.Fatalf("startup with valid old-owner outbox = %v, want retained recovery-only runtime", err)
		}
	}
	if fixture.repository.marks != 0 || fixture.runtime.compareCalls != 0 || fixture.repository.outbox.State != accounting.RecoveryProjectionPending {
		t.Fatal("startup projected old-owner outbox before audited takeover")
	}

	provider := fixture.strategy.runtimeStateStore.(*durableRecoveryRuntimeBindingFixture).provider.(*durableRecoveryProviderFixture)
	takeover := accounting.RecoveryOwnerTakeoverRecord{
		OperationID: "takeover-old-outbox", Actor: "admin", AuditRef: "incident-old-outbox",
		EvidenceHash: fmt.Sprintf("%064x", 992), PreviousOwnerGeneration: prior.OwnerGeneration,
		OwnerGeneration: provider.generation.ID(),
		EpisodeRevision: prior.Revision + 1, CreatedAt: time.Now().UTC(),
	}
	current := prior
	current.OwnerGeneration = takeover.OwnerGeneration
	current.OwnerTakeovers = []accounting.RecoveryOwnerTakeoverRecord{takeover}
	current.Revision = takeover.EpisodeRevision
	current.State = accounting.RecoveryBlocked
	current.UpdatedAt = time.Now().UTC()
	fixture.repository.episode = current
	fixture.repository.outbox.TargetEpisodeRevision++
	if projected, err := fixture.strategy.ProjectRecoveryOutboxAfterTakeover(fixture.ctx, current.ID, takeover.OperationID); err == nil || projected {
		t.Fatalf("pending outbox with mismatched revision must fail closed: projected=%t err=%v", projected, err)
	}
	fixture.repository.outbox.TargetEpisodeRevision--
	fixture.repository.markCommitThenError = true
	projected, err := fixture.strategy.ProjectRecoveryOutboxAfterTakeover(fixture.ctx, current.ID, takeover.OperationID)
	if err != nil || !projected {
		t.Fatalf("project explicit-takeover outbox: projected=%t err=%v", projected, err)
	}
	projectedAgain, err := fixture.strategy.ProjectRecoveryOutboxAfterTakeover(fixture.ctx, current.ID, takeover.OperationID)
	if err != nil || projectedAgain || fixture.runtime.compareCalls != 1 || fixture.repository.marks != 1 {
		t.Fatalf("idempotent response-loss projection retry: projected=%t CAS=%d marks=%d err=%v", projectedAgain, fixture.runtime.compareCalls, fixture.repository.marks, err)
	}
	var runtime fundingCarryRuntimeState
	if err = json.Unmarshal([]byte(fixture.runtime.payload), &runtime); err != nil {
		t.Fatal(err)
	}
	if len(runtime.RecoveryProjectionMarkers) != 1 || runtime.RecoveryProjectionMarkers[0].ProjectionHash != fixture.repository.outbox.ProjectionHash ||
		len(runtime.RecoveryFacts) != 1 || runtime.RecoveryFacts[0].Kind != accounting.RecoveryBorrowEvent {
		t.Fatalf("hash-validated existing outbox was not read back in runtime: markers=%+v events=%+v", runtime.RecoveryProjectionMarkers, runtime.RecoveryFacts)
	}
	if fixture.repository.marks != 1 || fixture.repository.outbox.State != accounting.RecoveryProjected || fixture.repository.episode.OwnerGeneration != takeover.OwnerGeneration ||
		fixture.repository.episode.State != accounting.RecoveryProjected || len(fixture.repository.episode.OwnerTakeovers) != 1 {
		t.Fatalf("projection lost takeover/hold state: marks=%d outbox=%+v episode=%+v", fixture.repository.marks, fixture.repository.outbox, fixture.repository.episode)
	}
	if len(fixture.repository.holds) != 1 || fixture.repository.holds[0].State != "held" {
		t.Fatal("outbox projection released the recovery hold")
	}
}

func TestFundingCarryTakeoverReceiptMustMatchAuditRecordExactly(t *testing.T) {
	takeover := accounting.RecoveryOwnerTakeoverRecord{OperationID: "takeover-op", Actor: "admin", AuditRef: "incident",
		EvidenceHash: fmt.Sprintf("%064x", 123), PreviousOwnerGeneration: "owner-old", OwnerGeneration: "owner-new", EpisodeRevision: 9,
		CreatedAt: time.Date(2026, 10, 13, 8, 0, 0, 0, time.UTC)}
	episode := accounting.RecoveryEpisode{ID: "episode", RecoveryGeneration: 4, AccountScopes: []string{fmt.Sprintf("%064x", 1)}}
	authorization := accounting.RecoveryOwnerTakeoverAuthorization{EpisodeID: episode.ID, OperationID: takeover.OperationID,
		Actor: takeover.Actor, AuditRef: takeover.AuditRef, ExpectedOwnerGeneration: takeover.PreviousOwnerGeneration,
		ExpectedEpisodeRevision: takeover.EpisodeRevision - 1, Confirmed: true}
	requestHash, err := storage.ExpectedFundingRecoveryOwnerTakeoverRequestHash(takeover.OwnerGeneration, episode.AccountScopes, authorization)
	if err != nil {
		t.Fatal("build takeover receipt request hash:", err)
	}
	receipt := accounting.RecoveryReceipt{EpisodeID: episode.ID, OperationID: takeover.OperationID, RequestHash: "request-hash",
		RecoveryGeneration: episode.RecoveryGeneration, State: accounting.RecoveryBlocked, EpisodeRevision: takeover.EpisodeRevision,
		Actor: takeover.Actor, AuditRef: takeover.AuditRef, EvidenceHash: takeover.EvidenceHash, ProjectionHash: takeover.EvidenceHash,
		PreviousOwnerGeneration: takeover.PreviousOwnerGeneration, OwnerGeneration: takeover.OwnerGeneration, CreatedAt: takeover.CreatedAt.Add(time.Nanosecond)}
	receipt.RequestHash = requestHash
	if err := validateFundingCarryTakeoverReceipt(episode, takeover, receipt); err != nil {
		t.Fatalf("valid takeover receipt rejected: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*accounting.RecoveryReceipt)
	}{
		{name: "episode id", mutate: func(r *accounting.RecoveryReceipt) { r.EpisodeID = "other" }},
		{name: "operation id", mutate: func(r *accounting.RecoveryReceipt) { r.OperationID = "other" }},
		{name: "actor", mutate: func(r *accounting.RecoveryReceipt) { r.Actor = "other" }},
		{name: "audit reference", mutate: func(r *accounting.RecoveryReceipt) { r.AuditRef = "other" }},
		{name: "evidence hash", mutate: func(r *accounting.RecoveryReceipt) { r.EvidenceHash = "other" }},
		{name: "projection hash", mutate: func(r *accounting.RecoveryReceipt) { r.ProjectionHash = "other" }},
		{name: "recovery generation", mutate: func(r *accounting.RecoveryReceipt) { r.RecoveryGeneration++ }},
		{name: "state", mutate: func(r *accounting.RecoveryReceipt) { r.State = accounting.RecoveryProjected }},
		{name: "owner generation", mutate: func(r *accounting.RecoveryReceipt) { r.OwnerGeneration = "other-owner" }},
		{name: "previous owner generation", mutate: func(r *accounting.RecoveryReceipt) { r.PreviousOwnerGeneration = "other-owner" }},
		{name: "episode revision", mutate: func(r *accounting.RecoveryReceipt) { r.EpisodeRevision++ }},
		{name: "request hash absent", mutate: func(r *accounting.RecoveryReceipt) { r.RequestHash = "" }},
		{name: "request hash changed", mutate: func(r *accounting.RecoveryReceipt) { r.RequestHash = "different-non-empty-hash" }},
		{name: "created at absent", mutate: func(r *accounting.RecoveryReceipt) { r.CreatedAt = time.Time{} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			changed := receipt
			test.mutate(&changed)
			if err := validateFundingCarryTakeoverReceipt(episode, takeover, changed); err == nil {
				t.Fatal("mismatched takeover receipt was accepted")
			}
		})
	}
}

func TestFundingCarryOldOwnerOutboxRejectsHashDriftAndFillFeeProjection(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*durableRecoveryFixture, fundingCarryRecoveryProjectionPayload)
	}{
		{
			name: "hash drift",
			mutate: func(f *durableRecoveryFixture, projection fundingCarryRecoveryProjectionPayload) {
				f.setProjection(projection)
				f.repository.outbox.ProjectionPayload = append(f.repository.outbox.ProjectionPayload, ' ')
			},
		},
		{
			name: "fill event",
			mutate: func(f *durableRecoveryFixture, projection fundingCarryRecoveryProjectionPayload) {
				projection.Events[0].Kind = accounting.RecoveryFillEvent
				f.setProjection(projection)
			},
		},
		{
			name: "fee event",
			mutate: func(f *durableRecoveryFixture, projection fundingCarryRecoveryProjectionPayload) {
				projection.Events[0].Kind = accounting.RecoveryFeeEvent
				f.setProjection(projection)
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newDurableRecoveryFixture(t)
			prior := fixture.repository.episode
			prior.OwnerGeneration = "prior-runtime-generation"
			fixture.repository.episode = prior
			projection := fundingCarryRecoveryProjectionPayload{Episode: prior, Events: []accounting.RecoveryEvent{{
				ID: "event-safe-kind", Source: "operator", Exchange: "binance", Market: "spot_margin", AccountScope: prior.AccountScopes[0],
				Kind: accounting.RecoveryBorrowEvent, ExternalID: "borrow-1", EpisodeID: prior.ID, Asset: "BTC", Amount: "0.1",
				SourceRevision: "r1", SourceHash: fmt.Sprintf("%064x", 1), ObservedAt: time.Date(2026, 10, 12, 1, 0, 0, 0, time.UTC),
				DebtEvidence: &accounting.RecoveryDebtEvidence{PrincipalRaw: "0.1", InterestRaw: "0"},
			}}}
			testCase.mutate(&fixture, projection)
			fixture.repository.holds = []accounting.RecoveryHold{{
				ID: prior.ID + ":1", EpisodeID: prior.ID, Bot: prior.Bot, Strategy: prior.Strategy,
				AccountScopes: append([]string(nil), prior.AccountScopes...), Generation: prior.RecoveryGeneration, Revision: 1, State: "held",
			}}
			err := fixture.strategy.reconcileDurableFundingCarryRecovery(fixture.ctx)
			var retained *FundingCarryDurableRecoveryRequiredError
			if err == nil || errors.As(err, &retained) {
				t.Fatalf("unsafe old-owner outbox error=%v, want fail-closed startup rejection", err)
			}
			if fixture.runtime.compareCalls != 0 || fixture.repository.marks != 0 || fixture.repository.outbox.State != accounting.RecoveryProjectionPending {
				t.Fatal("unsafe old-owner outbox caused runtime or durable projection side effects")
			}
		})
	}
}

func TestFundingCarryStartupActiveDurableHoldBlocksStart(t *testing.T) {
	fixture := newDurableRecoveryFixture(t)
	fixture.repository.outbox = accounting.RecoveryOutbox{}
	fixture.repository.holds = []accounting.RecoveryHold{{
		EpisodeID: fixture.repository.episode.ID, Bot: fixture.repository.episode.Bot,
		Strategy: fixture.repository.episode.Strategy, AccountScopes: fixture.repository.episode.AccountScopes,
		Generation: 1, Revision: 1, State: "active",
	}}
	err := fixture.strategy.Start(fixture.ctx)
	if err == nil {
		t.Fatal("Start succeeded with an active durable FundingCarry hold")
	}
	fixture.strategy.mu.RLock()
	started := fixture.strategy.started
	fixture.strategy.mu.RUnlock()
	if started {
		t.Fatal("strategy marked started while a durable recovery hold is active")
	}
}

func TestFundingCarryStartupHoldPrecedesPendingRepaymentRecovery(t *testing.T) {
	fixture := newDurableRecoveryFixture(t)
	scope := fixture.repository.episode.AccountScopes[0]
	fixture.repository.outbox = accounting.RecoveryOutbox{}
	fixture.repository.holds = []accounting.RecoveryHold{{
		EpisodeID: fixture.repository.episode.ID, Bot: fixture.repository.episode.Bot,
		Strategy: fixture.repository.episode.Strategy, AccountScopes: []string{scope},
		Generation: 1, Revision: 1, State: "active",
	}}
	margin := &durableRecoveryFinancialRPCSpy{mockFCExchange: &mockFCExchange{
		name: "binance", marketType: "spot_margin", baseAsset: "BTC", repayAmount: 0.4, repayPrincipal: 0.4,
	}}
	fixture.strategy.marginEx = margin
	fixture.strategy.marginAccountScope = scope
	fixture.strategy.spot.(*mockFCExchange).baseAsset = "BTC"
	borrowedAt := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	pendingState := fundingCarryRuntimeState{
		Strategy: "funding_carry", FuturesExchange: "binance", SpotExchange: "binance", Symbol: "BTCUSDT",
		MarginAccountScope: scope, OwnershipReady: true, IntentInFlight: true,
		IntentPhase: fundingCarryIntentPhaseDispatching, Direction: DirectionReverse,
		OwnedSpot: 0.4, OwnedFutures: 0.4, MarginDebt: 0.4, MarginBorrowTransferID: 71,
		MarginBorrowedAt: borrowedAt, RecoveryProjectionSchema: fundingCarryRecoveryProjectionSchemaVersion,
		MarginDebtEvents: []fundingCarryMarginDebtEvent{{
			Action: "borrow", TransferID: 71, Asset: "BTC", Amount: 0.4, Principal: 0.4,
			OccurredAt: borrowedAt, AccountScope: scope,
		}},
		MarginRepayIntent: &fundingCarryRepayIntent{
			Asset: "BTC", AccountScope: scope, Amount: 0.4, ExpectedRemaining: 0,
			BorrowTransferID: 71, TransferID: 72,
		},
	}
	payload, err := json.Marshal(pendingState)
	if err != nil {
		t.Fatal(err)
	}
	fixture.runtime.mu.Lock()
	fixture.runtime.version, fixture.runtime.payload, fixture.runtime.found = fundingCarryRuntimeStateVersion, string(payload), true
	fixture.runtime.readCalls = 0
	fixture.runtime.mu.Unlock()

	startErr := fixture.strategy.Start(fixture.ctx)
	if margin.borrowRPCs != 0 || margin.repayRPCs != 0 || margin.repayCalls != 0 || len(margin.placedOrders) != 0 {
		t.Fatalf("active hold allowed financial RPCs: borrow=%d repay=%d mockRepay=%d orders=%d",
			margin.borrowRPCs, margin.repayRPCs, margin.repayCalls, len(margin.placedOrders))
	}
	if fixture.runtime.compareCalls != 0 {
		t.Fatalf("active hold allowed recovery snapshot side effects: CAS calls=%d", fixture.runtime.compareCalls)
	}
	fixture.strategy.mu.RLock()
	started := fixture.strategy.started
	fixture.strategy.mu.RUnlock()
	if started {
		t.Fatal("strategy started while durable recovery hold remained active")
	}
	if startErr == nil || !strings.Contains(startErr.Error(), "durable recovery remains held") {
		t.Fatalf("Start error=%v, want durable active-hold rejection before recovery side effects", startErr)
	}
}

type durableRecoveryOrderExecutorFixture struct {
	calls int
	order *exchange.Order
}

func (e *durableRecoveryOrderExecutorFixture) PlaceOrderContext(context.Context, *exchange.OrderRequest) (*exchange.Order, error) {
	return nil, errors.New("ordinary order path must not be used for recovery")
}
func (e *durableRecoveryOrderExecutorFixture) PlaceRecoveryOrderContext(_ context.Context, _ *exchange.OrderRequest, _ accounting.OneShotPermit) (*exchange.Order, error) {
	e.calls++
	return e.order, nil
}
func (*durableRecoveryOrderExecutorFixture) CancelOrderContext(context.Context, int64) error {
	return nil
}
func (*durableRecoveryOrderExecutorFixture) SettleIntent(context.Context, string) error { return nil }

func TestFundingCarryFoundAdjudicationUsesVenueCIDAndPreservesUnknown(t *testing.T) {
	ctx := context.Background()
	scope := fmt.Sprintf("%064x", 777)
	store, err := storage.NewSQLStorage(t.TempDir() + "/found-adjudication.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err = store.MigrateFundingRecovery(ctx); err != nil {
		t.Fatal(err)
	}
	oldOwner, err := store.ClaimFundingCarryRuntimeGeneration(ctx, []string{scope})
	if err != nil {
		t.Fatal(err)
	}
	intent := accounting.RecoveryOrderIntent{SchemaVersion: 1, Exchange: "binance", Market: "futures", Symbol: "BTCUSDT", Side: "BUY", OrderType: "LIMIT", TimeInForce: "GTC", Price: "100", Quantity: "0.25", ClientOrderID: "found-original-cid"}
	hash, err := intent.CanonicalHash()
	if err != nil {
		t.Fatal(err)
	}
	action := accounting.RecoveryAction{ID: "found-action", Ordinal: 1, Kind: accounting.RecoveryActionOrder, Role: "open_futures", AccountScope: scope, Market: intent.Market, Symbol: intent.Symbol, OwnerGeneration: oldOwner.ID(), OrderIntentHash: hash, Status: "authorized"}
	episode := accounting.RecoveryEpisode{ID: "found-episode", Bot: "found-bot", Strategy: "funding_carry", AccountScopes: []string{scope}, OwnerGeneration: oldOwner.ID(), Revision: 1, RecoveryGeneration: 1, NextActionOrdinal: 1, State: accounting.RecoveryPrepared, ActionPlan: []accounting.RecoveryAction{action}}
	if _, err = store.CreateRecoveryEpisode(ctx, oldOwner, episode); err != nil {
		t.Fatal(err)
	}
	permit := accounting.OneShotPermit{ID: "found-permit", EpisodeID: episode.ID, Generation: 1, ActionID: action.ID, ActionOrdinal: 1, Kind: accounting.RecoveryActionOrder, AccountScope: scope, OwnerGeneration: oldOwner.ID(), LeaseID: 1, OrderIntentHash: hash, OrderIntent: &intent, ExpectedRevision: 1, State: accounting.PermitAuthorized}
	if _, err = store.CreateRecoveryPermit(ctx, oldOwner, permit); err != nil {
		t.Fatal(err)
	}
	if _, maySubmit, err := store.ConsumeRecoveryPermit(ctx, oldOwner, permit); err != nil || !maySubmit {
		t.Fatalf("consume permit: maySubmit=%t err=%v", maySubmit, err)
	}
	newOwner, err := store.ClaimFundingCarryRuntimeGeneration(ctx, []string{scope})
	if err != nil {
		t.Fatal(err)
	}
	episode, _, err = store.TakeOverRecoveryEpisode(ctx, newOwner, accounting.RecoveryOwnerTakeoverAuthorization{EpisodeID: episode.ID, OperationID: "found-takeover", Actor: "admin", AuditRef: "incident-found", ExpectedOwnerGeneration: oldOwner.ID(), ExpectedEpisodeRevision: episode.Revision, Confirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	venue := &fundingCarryRecoveryLookupExchange{mockFCExchange: &mockFCExchange{name: "binance", marketType: "futures"}}
	carry := NewFundingCarryStrategy("funding_carry", nil, config.SymbolConfig{Symbol: intent.Symbol}, venue, nil, nil, nil)
	provider := &durableRecoveryProviderFixture{repository: store, generation: newOwner, bot: episode.Bot, scopes: []string{scope}}
	carry.SetRuntimeStateStore(&durableRecoveryRuntimeBindingFixture{RuntimeStateStore: &durableRecoveryRuntimeFixture{}, provider: provider})
	request := accounting.RecoverySubmissionAdjudication{EpisodeID: episode.ID, OperationID: "found-operation", PermitID: permit.ID, ExpectedAttemptRevision: 1, ExpectedEpisodeRevision: episode.Revision, Decision: accounting.RecoverySubmissionFound, Actor: "admin", AuditRef: "incident-found", Confirmed: true}
	if _, _, err = carry.AdjudicateRecoverySubmissionFound(ctx, request); err == nil {
		t.Fatal("nil venue result must remain UNKNOWN")
	}
	invalidVenueOrders := []struct {
		name  string
		order *exchange.Order
	}{
		{name: "client order ID", order: &exchange.Order{OrderID: 2, ClientOrderID: "wrong-cid", Symbol: intent.Symbol, Side: exchange.SideBuy, Type: exchange.OrderTypeLimit, Price: 100, Quantity: 0.1}},
		{name: "quantity", order: &exchange.Order{OrderID: 2, ClientOrderID: intent.ClientOrderID, Symbol: intent.Symbol, Side: exchange.SideBuy, Type: exchange.OrderTypeLimit, Price: 100, Quantity: 0.2}},
		{name: "price", order: &exchange.Order{OrderID: 2, ClientOrderID: intent.ClientOrderID, Symbol: intent.Symbol, Side: exchange.SideBuy, Type: exchange.OrderTypeLimit, Price: 101, Quantity: 0.1}},
		{name: "order type", order: &exchange.Order{OrderID: 2, ClientOrderID: intent.ClientOrderID, Symbol: intent.Symbol, Side: exchange.SideBuy, Type: exchange.OrderTypeMarket, Price: 100, Quantity: 0.1}},
	}
	for _, testCase := range invalidVenueOrders {
		venue.order = testCase.order
		if _, _, err = carry.AdjudicateRecoverySubmissionFound(ctx, request); err == nil {
			t.Errorf("mismatched %s must remain UNKNOWN", testCase.name)
		}
	}
	attempt, found, err := store.GetRecoverySubmissionAttempt(ctx, episode.ID, permit.ID)
	if err != nil || !found || attempt.State != accounting.RecoverySubmissionUnknown {
		t.Fatalf("incomplete venue evidence changed attempt: %+v found=%t err=%v", attempt, found, err)
	}
	venue.order = &exchange.Order{OrderID: 991, ClientOrderID: intent.ClientOrderID, Symbol: intent.Symbol, Side: exchange.SideBuy, Type: exchange.OrderTypeLimit, Price: 100, Quantity: 0.25, Status: exchange.OrderStatusPartiallyFilled, ExecutedQty: 0.1, AvgPrice: 100, UpdateTime: 12345}
	request.OperationID = "found-operation"
	result, replayed, err := carry.AdjudicateRecoverySubmissionFound(ctx, request)
	evidenceBytes, evidenceErr := hex.DecodeString(result.EvidenceHash)
	if err != nil || evidenceErr != nil || len(evidenceBytes) != sha256.Size || replayed || result.State != accounting.RecoverySubmissionFound || result.VenueOrderID != "991" {
		t.Fatalf("venue-positive adjudication: result=%+v replayed=%t err=%v", result, replayed, err)
	}
	queriesAfterCommit := venue.calls
	venue.order = nil
	result, replayed, err = carry.AdjudicateRecoverySubmissionFound(ctx, request)
	if err != nil || !replayed || result.VenueOrderID != "991" || venue.calls != queriesAfterCommit {
		t.Fatalf("idempotent retry: result=%+v replayed=%t err=%v", result, replayed, err)
	}
	hold, held, err := store.GetRecoveryHold(ctx, episode.ID, episode.RecoveryGeneration)
	if err != nil || !held || hold.State != "held" {
		t.Fatalf("found adjudication must preserve hold: %+v held=%t err=%v", hold, held, err)
	}
}

func TestFundingCarryAcknowledgedFoundRequiresOriginalVenueOrderID(t *testing.T) {
	ctx := context.Background()
	scope := fmt.Sprintf("%064x", 778)
	store, err := storage.NewSQLStorage(t.TempDir() + "/ack-found-adjudication.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err = store.MigrateFundingRecovery(ctx); err != nil {
		t.Fatal(err)
	}
	oldOwner, err := store.ClaimFundingCarryRuntimeGeneration(ctx, []string{scope})
	if err != nil {
		t.Fatal(err)
	}
	intent := accounting.RecoveryOrderIntent{SchemaVersion: 1, Exchange: "binance", Market: "futures", Symbol: "BTCUSDT", Side: "BUY", OrderType: "LIMIT", TimeInForce: "GTC", Price: "100", Quantity: "0.25", ClientOrderID: "ack-found-original-cid"}
	hash, err := intent.CanonicalHash()
	if err != nil {
		t.Fatal(err)
	}
	action := accounting.RecoveryAction{ID: "ack-found-action", Ordinal: 1, Kind: accounting.RecoveryActionOrder, Role: "open_futures", AccountScope: scope, Market: intent.Market, Symbol: intent.Symbol, OwnerGeneration: oldOwner.ID(), OrderIntentHash: hash, Status: "authorized"}
	episode := accounting.RecoveryEpisode{ID: "ack-found-episode", Bot: "ack-found-bot", Strategy: "funding_carry", AccountScopes: []string{scope}, OwnerGeneration: oldOwner.ID(), Revision: 1, RecoveryGeneration: 1, NextActionOrdinal: 1, State: accounting.RecoveryPrepared, ActionPlan: []accounting.RecoveryAction{action}}
	if _, err = store.CreateRecoveryEpisode(ctx, oldOwner, episode); err != nil {
		t.Fatal(err)
	}
	permit := accounting.OneShotPermit{ID: "ack-found-permit", EpisodeID: episode.ID, Generation: 1, ActionID: action.ID, ActionOrdinal: 1, Kind: accounting.RecoveryActionOrder, AccountScope: scope, OwnerGeneration: oldOwner.ID(), LeaseID: 1, OrderIntentHash: hash, OrderIntent: &intent, ExpectedRevision: 1, State: accounting.PermitAuthorized}
	if _, err = store.CreateRecoveryPermit(ctx, oldOwner, permit); err != nil {
		t.Fatal(err)
	}
	if _, maySubmit, err := store.ConsumeRecoveryPermit(ctx, oldOwner, permit); err != nil || !maySubmit {
		t.Fatalf("consume recovery permit: maySubmit=%t err=%v", maySubmit, err)
	}
	if err = store.AcknowledgeRecoverySubmission(ctx, oldOwner, permit, "991"); err != nil {
		t.Fatal(err)
	}
	newOwner, err := store.ClaimFundingCarryRuntimeGeneration(ctx, []string{scope})
	if err != nil {
		t.Fatal(err)
	}
	episode, _, err = store.TakeOverRecoveryEpisode(ctx, newOwner, accounting.RecoveryOwnerTakeoverAuthorization{EpisodeID: episode.ID, OperationID: "ack-found-takeover", Actor: "admin", AuditRef: "ack-found-incident", ExpectedOwnerGeneration: oldOwner.ID(), ExpectedEpisodeRevision: episode.Revision, Confirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	venue := &fundingCarryRecoveryLookupExchange{mockFCExchange: &mockFCExchange{name: "binance", marketType: "futures"}}
	carry := NewFundingCarryStrategy("funding_carry", nil, config.SymbolConfig{Symbol: intent.Symbol}, venue, nil, nil, nil)
	provider := &durableRecoveryProviderFixture{repository: store, generation: newOwner, bot: episode.Bot, scopes: []string{scope}}
	carry.SetRuntimeStateStore(&durableRecoveryRuntimeBindingFixture{RuntimeStateStore: &durableRecoveryRuntimeFixture{}, provider: provider})
	request := accounting.RecoverySubmissionAdjudication{EpisodeID: episode.ID, OperationID: "ack-found-operation", PermitID: permit.ID, ExpectedAttemptRevision: 2, ExpectedEpisodeRevision: episode.Revision, Decision: accounting.RecoverySubmissionFound, Actor: "admin", AuditRef: "ack-found-incident", Confirmed: true}
	venue.order = &exchange.Order{OrderID: 992, ClientOrderID: intent.ClientOrderID, Symbol: intent.Symbol, Side: exchange.SideBuy, Type: exchange.OrderTypeLimit, Price: 100, Quantity: 0.25}
	if _, _, err = carry.AdjudicateRecoverySubmissionFound(ctx, request); err == nil {
		t.Fatal("different venue order ID must not adjudicate the acknowledged attempt")
	}
	attempt, found, err := store.GetRecoverySubmissionAttempt(ctx, episode.ID, permit.ID)
	if err != nil || !found || attempt.State != accounting.RecoverySubmissionAcknowledged || attempt.VenueOrderID != "991" {
		t.Fatalf("mismatched venue read changed acknowledged attempt: %+v found=%t err=%v", attempt, found, err)
	}
	venue.order.OrderID = 991
	attempt, replayed, err := carry.AdjudicateRecoverySubmissionFound(ctx, request)
	if err != nil || replayed || venue.calls != 2 || attempt.State != accounting.RecoverySubmissionFound || attempt.VenueOrderID != "991" {
		t.Fatalf("exact original venue order adjudication: attempt=%+v replayed=%t venueQueries=%d err=%v", attempt, replayed, venue.calls, err)
	}
	hold, held, err := store.GetRecoveryHold(ctx, episode.ID, episode.RecoveryGeneration)
	if err != nil || !held || hold.State != "held" {
		t.Fatalf("ACK found adjudication released hold: hold=%+v held=%t err=%v", hold, held, err)
	}
}

func TestRecoveryVenueDecimalMatchesRequiresExactRepresentableValue(t *testing.T) {
	for _, tc := range []struct {
		name     string
		actual   float64
		expected string
		want     bool
	}{
		{name: "equal", actual: 0.1, expected: "0.1", want: true},
		{name: "normalized trailing zero", actual: 0.1, expected: "0.100", want: true},
		{name: "tiny mismatch below former tolerance", actual: 0, expected: "0.0000000000001", want: false},
		{name: "ordinary mismatch", actual: 0.10000000000000002, expected: "0.1", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := recoveryVenueDecimalMatches(tc.actual, tc.expected); got != tc.want {
				t.Fatalf("recoveryVenueDecimalMatches(%g, %q) = %t, want %t", tc.actual, tc.expected, got, tc.want)
			}
		})
	}
}

func TestFundingCarryRecoveryOrderRequiresPersistedExactEpisodeAndHold(t *testing.T) {
	ctx := context.Background()
	scope := fmt.Sprintf("%064x", 41)
	store, err := storage.NewSQLStorage(t.TempDir() + "/recovery-order.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.MigrateFundingRecovery(ctx); err != nil {
		t.Fatal(err)
	}
	generation, err := store.ClaimFundingCarryRuntimeGeneration(ctx, []string{scope})
	if err != nil {
		t.Fatal(err)
	}
	futures := &mockFCExchange{name: "binance", marketType: "futures"}
	spot := &mockFCExchange{name: "binance", marketType: "spot"}
	strategy := NewFundingCarryStrategy("funding_carry", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, futures, spot, nil, nil)
	provider := &durableRecoveryProviderFixture{repository: store, generation: generation, bot: "bot-fc", scopes: []string{scope}}
	runtime := &durableRecoveryRuntimeFixture{}
	strategy.SetRuntimeStateStore(&durableRecoveryRuntimeBindingFixture{RuntimeStateStore: runtime, provider: provider})
	executor := &durableRecoveryOrderExecutorFixture{order: &exchange.Order{ClientOrderID: "recovery-cid", Symbol: "BTCUSDT", Side: exchange.SideBuy}}
	strategy.SetOrderExecutors(executor, &durableRecoveryOrderExecutorFixture{}, nil)

	request := &exchange.OrderRequest{Symbol: "BTCUSDT", Side: exchange.SideBuy, Type: exchange.OrderTypeLimit, TimeInForce: exchange.TimeInForceGTC, Price: 100, Quantity: 0.25, ClientOrderID: "recovery-cid"}
	intent := accounting.RecoveryOrderIntent{
		SchemaVersion: 1, Exchange: futures.GetName(), Market: "futures", Symbol: request.Symbol, Side: string(request.Side), OrderType: string(request.Type),
		TimeInForce: string(request.TimeInForce), Price: "100", Quantity: "0.25", ClientOrderID: request.ClientOrderID,
	}
	intentHash, err := intent.CanonicalHash()
	if err != nil {
		t.Fatal(err)
	}
	action := accounting.RecoveryAction{
		ID: "action-order", Ordinal: 1, Kind: accounting.RecoveryActionOrder, Role: "open_futures",
		AccountScope: scope, Market: "futures", Symbol: "BTCUSDT", OwnerGeneration: generation.ID(),
		OrderIntentHash: intentHash, Status: "authorized",
	}
	episode := accounting.RecoveryEpisode{
		ID: "episode-order", Bot: "bot-fc", Strategy: "funding_carry", AccountScopes: []string{scope},
		OwnerGeneration: generation.ID(), Revision: 1, RecoveryGeneration: 1,
		State: accounting.RecoveryPrepared, NextActionOrdinal: 1, ActionPlan: []accounting.RecoveryAction{action},
	}
	if _, err := store.CreateRecoveryEpisode(ctx, generation, episode); err != nil {
		t.Fatal(err)
	}
	permit := accounting.OneShotPermit{
		ID: "permit-order", EpisodeID: episode.ID, Generation: 1, ActionID: action.ID, ActionOrdinal: 1,
		Kind: accounting.RecoveryActionOrder, AccountScope: scope, OwnerGeneration: generation.ID(),
		LeaseID: 1, OrderIntentHash: intentHash, OrderIntent: &intent, ExpectedRevision: 1, State: accounting.PermitAuthorized,
	}
	if _, err := store.CreateRecoveryPermit(ctx, generation, permit); err != nil {
		t.Fatal(err)
	}
	if _, err := strategy.SubmitRecoveryOrderAction(ctx, "future", request, permit); err != nil {
		t.Fatalf("valid persisted recovery action rejected: %v", err)
	}
	if executor.calls != 1 {
		t.Fatalf("recovery executor calls=%d, want 1", executor.calls)
	}

	drifted := *request
	drifted.Quantity = 0.5
	if _, err := strategy.SubmitRecoveryOrderAction(ctx, "futures", &drifted, permit); !errors.Is(err, accounting.ErrRecoveryConflict) {
		t.Fatalf("request drift error=%v, want recovery conflict", err)
	}
	if executor.calls != 1 {
		t.Fatalf("drifted action reached executor: calls=%d", executor.calls)
	}
}

func TestFundingCarryDurableRecoveryRejectsOwnerAndProjectionMismatches(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*durableRecoveryFixture)
	}{
		{"bot mismatch", func(f *durableRecoveryFixture) { f.repository.episode.Bot = "another-bot" }},
		{"scope mismatch", func(f *durableRecoveryFixture) {
			f.repository.episode.AccountScopes = []string{fmt.Sprintf("%064x", 32)}
		}},
		{"generation mismatch", func(f *durableRecoveryFixture) { f.repository.episode.OwnerGeneration = "another-generation" }},
		{"projection hash mismatch", func(f *durableRecoveryFixture) { f.repository.outbox.ProjectionHash = "bad-hash" }},
		{"event owner mismatch", func(f *durableRecoveryFixture) {
			var projection fundingCarryRecoveryProjectionPayload
			_ = json.Unmarshal(f.repository.outbox.ProjectionPayload, &projection)
			projection.Events[0].AccountScope = fmt.Sprintf("%064x", 32)
			f.setProjection(projection)
		}},
		{"projection parent bot mismatch", func(f *durableRecoveryFixture) {
			var projection fundingCarryRecoveryProjectionPayload
			_ = json.Unmarshal(f.repository.outbox.ProjectionPayload, &projection)
			projection.Episode.Bot = "another-bot"
			f.setProjection(projection)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newDurableRecoveryFixture(t)
			test.mutate(&fixture)
			if err := fixture.strategy.reconcileDurableFundingCarryRecovery(fixture.ctx); err == nil {
				t.Fatal("mismatched durable recovery evidence was accepted")
			}
			if fixture.repository.marks != 0 {
				t.Fatal("mismatched projection was marked complete")
			}
		})
	}
}

func TestFundingCarryDurableRecoveryIgnoresDifferentBotWithDisjointScopes(t *testing.T) {
	fixture := newDurableRecoveryFixture(t)
	initialRuntimePayload := fixture.runtime.payload
	initialOutbox := fixture.repository.outbox

	fixture.repository.episode.Bot = "unrelated-bot"
	fixture.repository.episode.AccountScopes = []string{fmt.Sprintf("%064x", 99)}

	if err := fixture.strategy.reconcileDurableFundingCarryRecovery(fixture.ctx); err != nil {
		t.Fatalf("reconcile unrelated owner's pending outbox: %v", err)
	}
	if fixture.runtime.compareCalls != 0 || fixture.runtime.payload != initialRuntimePayload {
		t.Fatalf("unrelated owner's outbox changed runtime state: CAS calls=%d", fixture.runtime.compareCalls)
	}
	if fixture.repository.marks != 0 {
		t.Fatalf("unrelated owner's outbox was marked projected: marks=%d", fixture.repository.marks)
	}
	if fixture.repository.outbox.State != initialOutbox.State ||
		fixture.repository.outbox.AppliedOperationID != initialOutbox.AppliedOperationID ||
		fixture.repository.outbox.AppliedRevision != initialOutbox.AppliedRevision ||
		fixture.repository.outbox.AppliedHash != initialOutbox.AppliedHash ||
		!bytes.Equal(fixture.repository.outbox.ProjectionPayload, initialOutbox.ProjectionPayload) {
		t.Fatalf("unrelated owner's outbox was modified: before=%+v after=%+v", initialOutbox, fixture.repository.outbox)
	}
}

func (f *durableRecoveryFixture) setProjection(projection fundingCarryRecoveryProjectionPayload) {
	projection.ExpectedRuntimeRevision = f.repository.outbox.ExpectedRuntimeRevision
	payload, _ := json.Marshal(projection)
	hash := sha256.Sum256(payload)
	f.repository.outbox.ProjectionPayload = payload
	f.repository.outbox.ProjectionHash = hex.EncodeToString(hash[:])
}

func makeSharedSnapshotProjection(fixture durableRecoveryFixture, episodeID, operationID string, targetRevision uint64, expectedRuntimeRevision string) (accounting.RecoveryEpisode, accounting.RecoveryOutbox) {
	base := fixture.repository.episode
	base.ID = episodeID
	base.Revision = targetRevision
	base.State = accounting.RecoveryLedgerCommitted
	event := accounting.RecoveryEvent{
		ID: "event-" + operationID, Source: "venue", Exchange: "binance", Market: "spot", AccountScope: base.AccountScopes[0],
		Kind: accounting.RecoveryFeeEvent, ExternalID: "commission-" + operationID, EpisodeID: episodeID,
		Asset: "USDT", Amount: "0", SourceRevision: "r-" + operationID, SourceHash: fmt.Sprintf("%064x", targetRevision+11),
		ObservedAt: time.Date(2026, 10, 12, 2, 0, 0, 0, time.UTC),
	}
	payload, _ := json.Marshal(fundingCarryRecoveryProjectionPayload{Episode: base, Events: []accounting.RecoveryEvent{event},
		ExpectedRuntimeRevision: expectedRuntimeRevision})
	hash := sha256.Sum256(payload)
	return base, accounting.RecoveryOutbox{
		EpisodeID: episodeID, OperationID: operationID, ProjectionPayload: payload,
		ExpectedRuntimeRevision: expectedRuntimeRevision, TargetEpisodeRevision: targetRevision,
		ProjectionHash: hex.EncodeToString(hash[:]), State: accounting.RecoveryProjectionPending,
	}
}

func sharedSnapshotBinding(fixture durableRecoveryFixture, repository storage.FundingRecoveryStore) fundingCarryRecoveryBinding {
	_, generation, botID, scopes, err := fixture.strategy.CurrentFundingCarryRecoveryBinding()
	if err != nil {
		panic(err)
	}
	return fundingCarryRecoveryBinding{
		store: repository, generation: generation, botID: botID,
		scopeKeys: scopes, stateStore: fixture.strategy.runtimeStateStore.(RuntimeStateStore),
	}
}

func TestFundingCarryDurableRecoveryRejectsOldSnapshotAndRuntimeCASConflict(t *testing.T) {
	t.Run("old projection schema", func(t *testing.T) {
		fixture := newDurableRecoveryFixture(t)
		var state fundingCarryRuntimeState
		_ = json.Unmarshal([]byte(fixture.runtime.payload), &state)
		state.RecoveryProjectionSchema = 0
		payload, _ := json.Marshal(state)
		fixture.runtime.payload = string(payload)
		fixture.repository.outbox.ExpectedRuntimeRevision = fundingCarryRuntimeRevision(fundingCarryRuntimeStateVersion, string(payload))
		if err := fixture.strategy.reconcileDurableFundingCarryRecovery(fixture.ctx); err == nil {
			t.Fatal("legacy runtime snapshot accepted durable projection")
		}
		if fixture.repository.marks != 0 {
			t.Fatal("legacy runtime snapshot projection was marked complete")
		}
	})
	t.Run("CAS conflict", func(t *testing.T) {
		fixture := newDurableRecoveryFixture(t)
		fixture.runtime.conflict = true
		if err := fixture.strategy.reconcileDurableFundingCarryRecovery(fixture.ctx); err == nil {
			t.Fatal("runtime CAS conflict was accepted")
		}
		if fixture.repository.marks != 0 {
			t.Fatal("CAS-conflicted projection was marked complete")
		}
	})
}

func TestFundingCarryDurableRecoveryStorageAndReadbackErrorsRemainHeld(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*durableRecoveryFixture)
	}{
		{"list outbox", func(f *durableRecoveryFixture) { f.repository.listErr = errors.New("outbox unavailable") }},
		{"read episode", func(f *durableRecoveryFixture) { f.repository.episodeErr = errors.New("episode unavailable") }},
		{"runtime read", func(f *durableRecoveryFixture) { f.runtime.readErr = errors.New("runtime unavailable") }},
		{"runtime readback", func(f *durableRecoveryFixture) { f.runtime.readbackErr = errors.New("readback unavailable") }},
		{"mark projected", func(f *durableRecoveryFixture) { f.repository.markErr = errors.New("mark unavailable") }},
		{"receipt readback", func(f *durableRecoveryFixture) { f.repository.readbackErr = errors.New("receipt unavailable") }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newDurableRecoveryFixture(t)
			test.prepare(&fixture)
			if err := fixture.strategy.reconcileDurableFundingCarryRecovery(fixture.ctx); err == nil {
				t.Fatal("storage/readback failure was accepted")
			}
			fixture.strategy.mu.RLock()
			held := fixture.strategy.unownedExposure
			fixture.strategy.mu.RUnlock()
			if !held {
				t.Fatal("storage/readback failure did not latch the strategy recovery hold")
			}
		})
	}
}

func TestFundingCarryDurableProjectionRetryIsIdempotent(t *testing.T) {
	fixture := newDurableRecoveryFixture(t)
	for range 2 {
		if err := fixture.strategy.reconcileDurableFundingCarryRecovery(fixture.ctx); err != nil {
			t.Fatalf("sequential projection retry: %v", err)
		}
	}
	var state fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(fixture.runtime.payload), &state); err != nil {
		t.Fatal(err)
	}
	if len(state.RecoveryProjectionMarkers) != 1 || len(state.RecoveryFacts) != 1 || fixture.repository.marks != 1 {
		t.Fatalf("retry duplicated projection: markers=%d events=%d marks=%d", len(state.RecoveryProjectionMarkers), len(state.RecoveryFacts), fixture.repository.marks)
	}
}

func TestFundingCarryRuntimeCASResponseLostRetryReadsBackMarkerIdempotently(t *testing.T) {
	fixture := newDurableRecoveryFixture(t)
	fixture.runtime.casCommitThenError = errors.New("runtime CAS response lost after commit")
	if err := fixture.strategy.reconcileDurableFundingCarryRecovery(fixture.ctx); err == nil {
		t.Fatal("CAS response loss should remain fail-closed until marker is read back")
	}
	var afterLostResponse fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(fixture.runtime.payload), &afterLostResponse); err != nil {
		t.Fatal(err)
	}
	if len(afterLostResponse.RecoveryProjectionMarkers) != 1 || len(afterLostResponse.RecoveryFacts) != 1 || fixture.repository.marks != 0 {
		t.Fatalf("CAS response-loss state is not durable and pending: markers=%d events=%d marks=%d",
			len(afterLostResponse.RecoveryProjectionMarkers), len(afterLostResponse.RecoveryFacts), fixture.repository.marks)
	}
	fixture.runtime.casCommitThenError = nil
	if err := fixture.strategy.reconcileDurableFundingCarryRecovery(fixture.ctx); err != nil {
		t.Fatalf("retry after CAS response loss: %v", err)
	}
	var recovered fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(fixture.runtime.payload), &recovered); err != nil {
		t.Fatal(err)
	}
	if len(recovered.RecoveryProjectionMarkers) != 1 || len(recovered.RecoveryFacts) != 1 || fixture.repository.marks != 1 {
		t.Fatalf("marker readback retry duplicated projection: markers=%d events=%d marks=%d",
			len(recovered.RecoveryProjectionMarkers), len(recovered.RecoveryFacts), fixture.repository.marks)
	}
}

func TestFundingCarryConcurrentDurableProjectionRetriesRemainIdempotent(t *testing.T) {
	fixture := newDurableRecoveryFixture(t)
	fixture.runtime.casEntered = make(chan struct{}, 2)
	fixture.runtime.casRelease = make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() { results <- fixture.strategy.reconcileDurableFundingCarryRecovery(fixture.ctx) }()
	}
	<-fixture.runtime.casEntered
	<-fixture.runtime.casEntered
	close(fixture.runtime.casRelease)
	for range 2 {
		if err := <-results; err != nil {
			t.Errorf("concurrent projection retry: %v", err)
		}
	}
	var state fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(fixture.runtime.payload), &state); err != nil {
		t.Fatal(err)
	}
	if len(state.RecoveryProjectionMarkers) != 1 || len(state.RecoveryFacts) != 1 || fixture.repository.marks != 1 {
		t.Fatalf("concurrent retry duplicated projection: markers=%d events=%d marks=%d", len(state.RecoveryProjectionMarkers), len(state.RecoveryFacts), fixture.repository.marks)
	}
}
