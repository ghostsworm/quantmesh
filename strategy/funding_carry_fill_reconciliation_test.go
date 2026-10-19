package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"quantmesh/accounting"
	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/storage"
)

type fundingRecoveryBatchExchange struct {
	*mockFCExchange
	batch *exchange.OrderFillBatch
	err   error
	calls int
}

func (e *fundingRecoveryBatchExchange) GetOrderFillBatch(_ context.Context, symbol string, orderID int64) (*exchange.OrderFillBatch, error) {
	e.calls++
	if e.err != nil {
		return nil, e.err
	}
	if e.batch == nil || e.batch.Symbol != symbol || e.batch.OrderID != orderID {
		return nil, errors.New("unexpected fill batch identity")
	}
	copy := cloneOrderFillBatch(e.batch)
	return &copy, nil
}

type fundingRecoveryFillFixture struct {
	strategy *FundingCarryStrategy
	store    *storage.SQLStorage
	runtime  *durableRecoveryRuntimeFixture
	venue    *fundingRecoveryBatchExchange
	gate     *execution.OpeningGate
	episode  accounting.RecoveryEpisode
	child    accounting.RecoveryChild
	request  ApplyFundingRecoveryFillsRequest
	ctx      context.Context
}

func newFundingRecoveryFillFixture(t *testing.T, fills []*exchange.OrderFill, complete, feesComplete, adjudicateFound bool, intentQuantity ...string) fundingRecoveryFillFixture {
	t.Helper()
	ctx := context.Background()
	scope := fmt.Sprintf("%064x", 171)
	store, err := storage.NewSQLStorage(filepath.Join(t.TempDir(), "funding-fill-recovery.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err = store.MigrateFundingRecovery(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	oldGeneration, err := store.ClaimFundingCarryRuntimeGeneration(ctx, []string{scope})
	if err != nil {
		t.Fatal(err)
	}
	intent := accounting.RecoveryOrderIntent{SchemaVersion: 1, Exchange: "binance", Market: "spot", Symbol: "BTCUSDT",
		Side: "BUY", OrderType: "LIMIT", TimeInForce: "GTC", Price: "100", Quantity: "0.1", ClientOrderID: "fill-cid-171"}
	if len(intentQuantity) > 0 {
		intent.Quantity = intentQuantity[0]
	}
	intentHash, err := intent.CanonicalHash()
	if err != nil {
		t.Fatal(err)
	}
	episode := accounting.RecoveryEpisode{ID: "episode-fill-171", Bot: "bot-fill-171", Strategy: "funding_carry", AccountScopes: []string{scope},
		OwnerGeneration: oldGeneration.ID(), Revision: 1, RecoveryGeneration: 1, NextActionOrdinal: 1, State: accounting.RecoveryPrepared,
		ActionPlan: []accounting.RecoveryAction{{ID: "order-action-171", Ordinal: 1, Kind: accounting.RecoveryActionOrder,
			Role: "spot", AccountScope: scope, Market: intent.Market, Symbol: intent.Symbol, OwnerGeneration: oldGeneration.ID(),
			OrderIntentHash: intentHash, Status: "authorized"}}}
	if _, err = store.CreateRecoveryEpisode(ctx, oldGeneration, episode); err != nil {
		t.Fatal(err)
	}
	permit := accounting.OneShotPermit{ID: "permit-fill-171", EpisodeID: episode.ID, Generation: 1, ActionID: "order-action-171", ActionOrdinal: 1,
		Kind: accounting.RecoveryActionOrder, AccountScope: scope, OwnerGeneration: oldGeneration.ID(), LeaseID: 91,
		OrderIntentHash: intentHash, OrderIntent: &intent, ExpectedRevision: 1, State: accounting.PermitAuthorized}
	if _, err = store.CreateRecoveryPermit(ctx, oldGeneration, permit); err != nil {
		t.Fatal(err)
	}
	if _, maySubmit, consumeErr := store.ConsumeRecoveryPermit(ctx, oldGeneration, permit); consumeErr != nil || !maySubmit {
		t.Fatalf("consume permit: maySubmit=%v err=%v", maySubmit, consumeErr)
	}
	if err = store.AcknowledgeRecoverySubmission(ctx, oldGeneration, permit, "4171"); err != nil {
		t.Fatal(err)
	}
	currentEpisode, found, err := store.GetRecoveryEpisode(ctx, episode.ID)
	if err != nil || !found {
		t.Fatalf("read episode after acknowledgement: found=%v err=%v", found, err)
	}
	newGeneration := oldGeneration
	takenOver := currentEpisode
	newGeneration, err = store.ClaimFundingCarryRuntimeGeneration(ctx, []string{scope})
	if err != nil {
		t.Fatal(err)
	}
	takenOver, replayed, err := store.TakeOverRecoveryEpisode(ctx, newGeneration, accounting.RecoveryOwnerTakeoverAuthorization{
		EpisodeID: episode.ID, OperationID: "takeover-fill-171", Actor: "operator", AuditRef: "incident-171",
		ExpectedOwnerGeneration: oldGeneration.ID(), ExpectedEpisodeRevision: currentEpisode.Revision, Confirmed: true})
	if err != nil || replayed {
		t.Fatalf("takeover: replayed=%v err=%v", replayed, err)
	}
	attempt, found, err := store.GetRecoverySubmissionAttempt(ctx, episode.ID, permit.ID)
	if err != nil || !found {
		t.Fatalf("read attempt: found=%v err=%v", found, err)
	}
	if adjudicateFound {
		attempt, _, err = store.AdjudicateRecoverySubmission(ctx, newGeneration, accounting.RecoverySubmissionAdjudication{
			EpisodeID: episode.ID, OperationID: "found-fill-171", PermitID: permit.ID,
			ExpectedAttemptRevision: attempt.Revision, ExpectedEpisodeRevision: takenOver.Revision,
			Decision: accounting.RecoverySubmissionFound, VenueOrderID: "4171", Actor: "operator", AuditRef: "incident-171",
			EvidenceHash: fmt.Sprintf("%064x", 172), Confirmed: true})
		if err != nil {
			t.Fatal(err)
		}
	}
	children, err := store.ListRecoveryChildren(ctx, episode.ID)
	if err != nil || len(children) != 1 {
		t.Fatalf("read bound child: %v %+v", err, children)
	}
	orderStatus := exchange.OrderStatusFilled
	executedRaw := "0.1"
	if len(fills) == 0 {
		orderStatus, executedRaw = exchange.OrderStatusCanceled, "0.000"
	}
	batch := &exchange.OrderFillBatch{OrderID: 4171, ClientOrderID: intent.ClientOrderID, Symbol: intent.Symbol, Side: exchange.SideBuy,
		TerminalStatus: orderStatus, OrderUpdateTime: 1_800_000_000_000, ExecutedQuantityRaw: executedRaw,
		SourceRevision: "sha256:source-171", Fills: fills, FillHistoryComplete: complete, Complete: complete, FeesComplete: feesComplete}
	venue := &fundingRecoveryBatchExchange{mockFCExchange: &mockFCExchange{name: "binance", marketType: "spot", baseAsset: "BTC"}, batch: batch}
	futures := &mockFCExchange{name: "binance", marketType: "futures", baseAsset: "BTC"}
	state := fundingCarryRuntimeState{Strategy: "funding_carry", FuturesExchange: "binance", SpotExchange: "binance", Symbol: "BTCUSDT",
		OwnershipReady: true, RecoveryProjectionSchema: fundingCarryRecoveryProjectionSchemaVersion}
	statePayload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	runtime := &durableRecoveryRuntimeFixture{version: fundingCarryRuntimeStateVersion, payload: string(statePayload), found: true}
	provider := &durableRecoveryProviderFixture{repository: store, generation: newGeneration, bot: episode.Bot, scopes: []string{scope}}
	strategy := NewFundingCarryStrategy("funding_carry", nil, config.SymbolConfig{Symbol: intent.Symbol}, futures, venue, nil, nil)
	strategy.SetRuntimeStateStore(&durableRecoveryRuntimeBindingFixture{RuntimeStateStore: runtime, provider: provider})
	gate := &execution.OpeningGate{}
	gate.Block(execution.RecoveryEpisodeGateSource(episode.ID, episode.RecoveryGeneration))
	strategy.SetOpeningGate(gate)
	child := children[0]
	return fundingRecoveryFillFixture{strategy: strategy, store: store, runtime: runtime, venue: venue, gate: gate,
		episode: takenOver, child: child, ctx: ctx}
}

func validFundingRecoveryFill(tradeID, qty string, commissionKnown bool) *exchange.OrderFill {
	fill := &exchange.OrderFill{OrderID: 4171, TradeID: tradeID, Symbol: "BTCUSDT", Side: exchange.SideBuy,
		PriceRaw: "100.00", QuantityRaw: qty, TradeTime: 1_799_999_999_000, CommissionKnown: commissionKnown}
	if commissionKnown {
		fill.CommissionRaw, fill.CommissionAsset = "0.0001", "BNB"
	}
	return fill
}

func TestFundingCarryFillPreviewIsReadOnlyAndApplyKeepsUnknownFeeHeld(t *testing.T) {
	fixture := newFundingRecoveryFillFixture(t, []*exchange.OrderFill{validFundingRecoveryFill("12", "0.1", false)}, true, false, true)
	preview, err := fixture.strategy.PreviewFundingRecoveryFills(fixture.ctx, fixture.episode.ID, fixture.child.ID)
	if err != nil || len(preview.NewTradeIDs) != 1 || preview.NewTradeIDs[0] != "12" || preview.FeesComplete {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
	if _, found, err := fixture.store.GetRecoveryReceipt(fixture.ctx, fixture.episode.ID, "fill-op-171"); err != nil || found {
		t.Fatalf("preview wrote receipt: found=%v err=%v", found, err)
	}
	result, err := fixture.strategy.ApplyFundingRecoveryFills(fixture.ctx, ApplyFundingRecoveryFillsRequest{
		EpisodeID: fixture.episode.ID, ChildID: fixture.child.ID, OperationID: "fill-op-171",
		ExpectedEpisodeRevision: preview.EpisodeRevision, ExpectedChildRevision: preview.ChildRevision,
		PreviewSourceRevision: preview.Batch.SourceRevision, Actor: "operator", AuditRef: "incident-171", Confirmed: true})
	if err != nil || !result.Projected || !result.HoldRetained || result.FeesComplete || !result.ManualFeeAdjustmentRequired {
		t.Fatalf("unknown-fee apply result=%+v err=%v", result, err)
	}
	hold, found, err := fixture.store.GetRecoveryHold(fixture.ctx, fixture.episode.ID, fixture.episode.RecoveryGeneration)
	if err != nil || !found || hold.State != "held" || !fixture.gate.HasBlock(execution.RecoveryEpisodeGateSource(fixture.episode.ID, fixture.episode.RecoveryGeneration)) {
		t.Fatalf("fill apply changed hold/Gate: hold=%+v found=%v err=%v sources=%v", hold, found, err, fixture.gate.Sources())
	}
	events, err := fixture.store.ListRecoveryEvents(fixture.ctx, fixture.episode.ID)
	fillEvents := make([]accounting.RecoveryEvent, 0, 1)
	for _, event := range events {
		if event.Kind == accounting.RecoveryFillEvent {
			fillEvents = append(fillEvents, event)
		}
	}
	if err != nil || len(fillEvents) != 1 || fillEvents[0].FillEvidence.CommissionKnown || fillEvents[0].FillEvidence.CommissionRaw != "" || fillEvents[0].FillEvidence.CommissionAsset != "" {
		t.Fatalf("unknown fee was converted or written as fee zero: events=%+v err=%v", events, err)
	}
	if calls := fixture.venue.calls; calls != 2 {
		t.Fatalf("preview and apply venue reread calls=%d, want 2", calls)
	}
}

func TestFundingCarryFillApplyRejectsUnfoundAttemptPartialPageAndTradeTime(t *testing.T) {
	testCases := []struct {
		name      string
		found     bool
		complete  bool
		tradeTime int64
		wantCalls int
	}{
		{name: "acknowledged is not found", found: false, complete: true, tradeTime: 1_799_999_999_000, wantCalls: 0},
		{name: "partial source", found: true, complete: false, tradeTime: 1_799_999_999_000, wantCalls: 1},
		{name: "missing trade time", found: true, complete: true, tradeTime: 0, wantCalls: 1},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fill := validFundingRecoveryFill("12", "0.1", true)
			fill.TradeTime = testCase.tradeTime
			fixture := newFundingRecoveryFillFixture(t, []*exchange.OrderFill{fill}, testCase.complete, true, testCase.found)
			if _, err := fixture.strategy.PreviewFundingRecoveryFills(fixture.ctx, fixture.episode.ID, fixture.child.ID); err == nil {
				t.Fatal("unsafe attempt/source evidence accepted")
			}
			if fixture.venue.calls != testCase.wantCalls {
				t.Fatalf("venue calls=%d want=%d", fixture.venue.calls, testCase.wantCalls)
			}
			if receipts, err := fixture.store.ListRecoveryEvents(fixture.ctx, fixture.episode.ID); err != nil || len(receipts) != 0 {
				t.Fatalf("failed preview wrote fill events: events=%+v err=%v", receipts, err)
			}
		})
	}
}

func TestFundingCarryFillRejectsUnattributedRealizedPnL(t *testing.T) {
	testCases := []struct {
		name  string
		known bool
		raw   string
		asset string
	}{
		{name: "known PnL without asset", known: true, raw: "0.01"},
		{name: "known PnL without raw amount", known: true, asset: "USDT"},
		{name: "unknown PnL with raw amount", raw: "0"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fill := validFundingRecoveryFill("13", "0.1", true)
			fill.RealizedPnLKnown, fill.RealizedPnLRaw, fill.RealizedPnLAsset = testCase.known, testCase.raw, testCase.asset
			fixture := newFundingRecoveryFillFixture(t, []*exchange.OrderFill{fill}, true, true, true)
			if _, err := fixture.strategy.PreviewFundingRecoveryFills(fixture.ctx, fixture.episode.ID, fixture.child.ID); err == nil {
				t.Fatal("missing/inconsistent realized-PnL evidence was silently accepted")
			}
			if events, err := fixture.store.ListRecoveryEvents(fixture.ctx, fixture.episode.ID); err != nil || len(events) != 0 {
				t.Fatalf("invalid PnL evidence changed durable ledger: events=%+v err=%v", events, err)
			}
		})
	}
}

func TestFundingRecoveryEpisodeAcceptsOriginalAttemptOwnerAcrossTakeoverChain(t *testing.T) {
	episode := accounting.RecoveryEpisode{
		OwnerGeneration: "generation-c",
		OwnerTakeovers: []accounting.RecoveryOwnerTakeoverRecord{
			{PreviousOwnerGeneration: "generation-a", OwnerGeneration: "generation-b"},
			{PreviousOwnerGeneration: "generation-b", OwnerGeneration: "generation-c"},
		},
	}
	for _, generation := range []string{"generation-a", "generation-b", "generation-c"} {
		if !fundingRecoveryEpisodeOwnsGeneration(episode, generation) {
			t.Errorf("audited generation %q should own historical recovery work", generation)
		}
	}
	if fundingRecoveryEpisodeOwnsGeneration(episode, "unrelated-generation") || fundingRecoveryEpisodeOwnsGeneration(episode, "") {
		t.Fatal("unrelated or empty generation must not claim the episode")
	}
	episode.OwnerTakeovers[1].OwnerGeneration = "disconnected-generation"
	if fundingRecoveryEpisodeOwnsGeneration(episode, "generation-a") {
		t.Fatal("disconnected takeover chain must not authorize the original owner")
	}
}

func TestFundingCarryZeroFillTerminalResponseLossRetryKeepsFeeCompleteAndHeld(t *testing.T) {
	fixture := newFundingRecoveryFillFixture(t, nil, true, true, true)
	preview, err := fixture.strategy.PreviewFundingRecoveryFills(fixture.ctx, fixture.episode.ID, fixture.child.ID)
	if err != nil {
		t.Fatal(err)
	}
	fixture.runtime.casErr = errors.New("simulate projection response loss before runtime commit")
	request := ApplyFundingRecoveryFillsRequest{EpisodeID: fixture.episode.ID, ChildID: fixture.child.ID, OperationID: "zero-op-171",
		ExpectedEpisodeRevision: preview.EpisodeRevision, ExpectedChildRevision: preview.ChildRevision,
		PreviewSourceRevision: preview.Batch.SourceRevision, Actor: "operator", AuditRef: "incident-zero-171", Confirmed: true}
	if result, err := fixture.strategy.ApplyFundingRecoveryFills(fixture.ctx, request); err == nil || result.Projected || !result.HoldRetained {
		t.Fatalf("first zero-terminal apply should commit receipt but leave pending hold: result=%+v err=%v", result, err)
	}
	fixture.runtime.casErr = nil
	result, err := fixture.strategy.ApplyFundingRecoveryFills(fixture.ctx, request)
	if err != nil || !result.Replayed || !result.Projected || !result.FeesComplete || result.ManualFeeAdjustmentRequired || !result.HoldRetained {
		t.Fatalf("zero-terminal response-loss retry result=%+v err=%v", result, err)
	}
	events, err := fixture.store.ListRecoveryEvents(fixture.ctx, fixture.episode.ID)
	if err != nil || len(events) != 1 || events[0].Kind != accounting.RecoveryTerminalEvent || events[0].Amount != "0" {
		t.Fatalf("zero terminal evidence=%+v err=%v", events, err)
	}
	if hold, found, err := fixture.store.GetRecoveryHold(fixture.ctx, fixture.episode.ID, fixture.episode.RecoveryGeneration); err != nil || !found || hold.State != "held" {
		t.Fatalf("zero terminal retry released hold: hold=%+v found=%v err=%v", hold, found, err)
	}
}

func TestFundingCarryFillPreviewFailsClosedForLowerLateTradeID(t *testing.T) {
	fixture := newFundingRecoveryFillFixture(t, []*exchange.OrderFill{validFundingRecoveryFill("20", "0.1", true)}, true, true, true)
	fixture.venue = &fundingRecoveryBatchExchange{mockFCExchange: fixture.venue.mockFCExchange, batch: fixture.venue.batch}
	fixture.strategy.spot = fixture.venue
	preview, err := fixture.strategy.PreviewFundingRecoveryFills(fixture.ctx, fixture.episode.ID, fixture.child.ID)
	if err != nil {
		t.Fatal(err)
	}
	request := ApplyFundingRecoveryFillsRequest{EpisodeID: fixture.episode.ID, ChildID: fixture.child.ID, OperationID: "cursor-op-171",
		ExpectedEpisodeRevision: preview.EpisodeRevision, ExpectedChildRevision: preview.ChildRevision,
		PreviewSourceRevision: preview.Batch.SourceRevision, Actor: "operator", AuditRef: "incident-cursor-171", Confirmed: true}
	if _, err = fixture.strategy.ApplyFundingRecoveryFills(fixture.ctx, request); err != nil {
		t.Fatal(err)
	}
	current, found, err := fixture.store.GetRecoveryEpisode(fixture.ctx, fixture.episode.ID)
	if err != nil || !found {
		t.Fatal("read current episode", err)
	}
	children, err := fixture.store.ListRecoveryChildren(fixture.ctx, fixture.episode.ID)
	if err != nil || len(children) != 1 {
		t.Fatal("read current child", err)
	}
	batchCopy := cloneOrderFillBatch(fixture.venue.batch)
	fixture.venue.batch = &batchCopy
	fixture.venue.batch.Fills = append(fixture.venue.batch.Fills, validFundingRecoveryFill("19", "0.01", true))
	fixture.venue.batch.ExecutedQuantityRaw = "0.11"
	fixture.venue.batch.SourceRevision = "sha256:late-lower-trade"
	fixture.venue.batch.Complete = true
	fixture.venue.batch.FillHistoryComplete = true
	if _, err = fixture.strategy.PreviewFundingRecoveryFills(fixture.ctx, current.ID, children[0].ID); err == nil {
		t.Fatal("newly discovered lower numeric trade ID was accepted")
	}
	if records, err := fixture.store.ListRecoveryEvents(fixture.ctx, fixture.episode.ID); err != nil || len(records) != 2 {
		t.Fatalf("late lower trade ID changed durable cursor/events: events=%+v err=%v", records, err)
	}
}

func TestFundingCarryAdditiveLateHigherTradeGetsNewDurableBatchMarker(t *testing.T) {
	fixture := newFundingRecoveryFillFixture(t, []*exchange.OrderFill{validFundingRecoveryFill("20", "0.1", true)}, true, true, true, "0.2")
	firstPreview, err := fixture.strategy.PreviewFundingRecoveryFills(fixture.ctx, fixture.episode.ID, fixture.child.ID)
	if err != nil {
		t.Fatal(err)
	}
	firstRequest := ApplyFundingRecoveryFillsRequest{EpisodeID: fixture.episode.ID, ChildID: fixture.child.ID, OperationID: "late-higher-first",
		ExpectedEpisodeRevision: firstPreview.EpisodeRevision, ExpectedChildRevision: firstPreview.ChildRevision,
		PreviewSourceRevision: firstPreview.Batch.SourceRevision, Actor: "operator", AuditRef: "late-fill-first", Confirmed: true}
	if _, err := fixture.strategy.ApplyFundingRecoveryFills(fixture.ctx, firstRequest); err != nil {
		t.Fatal("apply initial complete fill batch:", err)
	}
	updatedEpisode, found, err := fixture.store.GetRecoveryEpisode(fixture.ctx, fixture.episode.ID)
	if err != nil || !found {
		t.Fatalf("read episode after first batch: found=%t err=%v", found, err)
	}
	children, err := fixture.store.ListRecoveryChildren(fixture.ctx, fixture.episode.ID)
	if err != nil || len(children) != 1 {
		t.Fatalf("read child after first batch: children=%+v err=%v", children, err)
	}
	batch := cloneOrderFillBatch(fixture.venue.batch)
	batch.Fills = append(batch.Fills, validFundingRecoveryFill("21", "0.1", true))
	batch.ExecutedQuantityRaw = "0.2"
	batch.SourceRevision = "sha256:late-higher-trade"
	fixture.venue.batch = &batch
	secondPreview, err := fixture.strategy.PreviewFundingRecoveryFills(fixture.ctx, updatedEpisode.ID, children[0].ID)
	if err != nil {
		t.Fatal("preview additive later complete batch:", err)
	}
	secondRequest := ApplyFundingRecoveryFillsRequest{EpisodeID: fixture.episode.ID, ChildID: fixture.child.ID, OperationID: "late-higher-second",
		ExpectedEpisodeRevision: secondPreview.EpisodeRevision, ExpectedChildRevision: secondPreview.ChildRevision,
		PreviewSourceRevision: secondPreview.Batch.SourceRevision, Actor: "operator", AuditRef: "late-fill-second", Confirmed: true}
	if _, err := fixture.strategy.ApplyFundingRecoveryFills(fixture.ctx, secondRequest); err != nil {
		t.Fatal("append later trade without replacing historical facts:", err)
	}
	events, err := fixture.store.ListRecoveryEvents(fixture.ctx, fixture.episode.ID)
	if err != nil || len(events) != 4 {
		t.Fatalf("late additive batch events=%+v err=%v; want two fills and two immutable markers", events, err)
	}
	if err := accounting.ValidateRecoveryFillBatchMarkers(events, fixture.child.ID); err != nil {
		t.Fatalf("complete marker chain did not authenticate the append-only fill prefix: %v", err)
	}
}

var _ exchange.OrderFillBatchSource = (*fundingRecoveryBatchExchange)(nil)
