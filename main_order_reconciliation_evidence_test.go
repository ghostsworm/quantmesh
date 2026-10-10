package main

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"sync"
	"testing"

	"quantmesh/accounting"
	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/lock"
	"quantmesh/order"
	"quantmesh/storage"
)

type evidenceTestExchange struct {
	exchange.IExchange
	name     string
	market   string
	order    *exchange.Order
	fills    []*exchange.OrderFill
	getErr   error
	executor *order.ExchangeOrderExecutor
}

func (f *evidenceTestExchange) GetName() string       { return f.name }
func (f *evidenceTestExchange) GetMarketType() string { return f.market }
func (f *evidenceTestExchange) GetOrder(_ context.Context, symbol string, orderID int64) (*exchange.Order, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.order == nil || f.order.Symbol != symbol || f.order.OrderID != orderID {
		return nil, errors.New("order not found")
	}
	copy := *f.order
	return &copy, nil
}
func (f *evidenceTestExchange) GetOrderFills(_ context.Context, symbol string, orderID int64) ([]*exchange.OrderFill, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	result := make([]*exchange.OrderFill, 0, len(f.fills))
	for _, fill := range f.fills {
		if fill == nil || fill.Symbol != symbol || fill.OrderID != orderID {
			continue
		}
		copy := *fill
		result = append(result, &copy)
	}
	return result, nil
}

type evidenceTestJournal struct {
	mu      sync.Mutex
	scope   string
	records []execution.IntentJournalRecord
}

func (j *evidenceTestJournal) SaveExecutionIntent(_ context.Context, scope, cid string, revision int64, payload []byte) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if scope != j.scope {
		return errors.New("wrong scope")
	}
	for i := range j.records {
		if j.records[i].ClientOrderID == cid {
			if j.records[i].Revision != revision {
				return execution.ErrIntentJournalConflict
			}
			j.records[i].Revision++
			j.records[i].Payload = append([]byte(nil), payload...)
			return nil
		}
	}
	if revision != 0 {
		return execution.ErrIntentJournalConflict
	}
	j.records = append(j.records, execution.IntentJournalRecord{ID: 1, ClientOrderID: cid, Revision: 1, Payload: append([]byte(nil), payload...)})
	return nil
}
func (j *evidenceTestJournal) LoadExecutionIntents(_ context.Context, scope string, after int64, limit int) ([]execution.IntentJournalRecord, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if scope != j.scope {
		return nil, errors.New("wrong scope")
	}
	result := make([]execution.IntentJournalRecord, 0, len(j.records))
	for _, record := range j.records {
		if record.ID <= after {
			continue
		}
		copy := record
		copy.Payload = append([]byte(nil), record.Payload...)
		result = append(result, copy)
		if len(result) == limit {
			break
		}
	}
	return result, nil
}

type evidencePersistedIntent struct {
	Version int                   `json:"Version"`
	Scope   execution.IntentScope `json:"Scope"`
	Request order.OrderRequest    `json:"Request"`
	Opening bool                  `json:"Opening"`
	Order   *order.Order          `json:"Order"`
	Settled bool                  `json:"Settled"`
}

func newEvidenceProviderFixture(t *testing.T) (*runtimeTrustedOrderEvidenceProvider, storage.OrderReconciliationCase, *evidenceTestExchange) {
	t.Helper()
	const (
		botID        = "bot-evidence"
		symbol       = "BTCUSDT"
		cid          = "qm-grid-evidence-1"
		accountScope = "account-scope-digest"
	)
	venue := &evidenceTestExchange{
		name: "binance", market: "futures",
		order: &exchange.Order{OrderID: 73, ClientOrderID: cid, Symbol: symbol, Side: exchange.SideBuy, Quantity: 2, ExecutedQty: 1.25, Status: exchange.OrderStatusCanceled},
		fills: []*exchange.OrderFill{{OrderID: 73, TradeID: "trade-73-a", Symbol: symbol, Side: exchange.SideBuy, Price: 100, Quantity: 1.25, Commission: 0.001, CommissionAsset: "BTC", TradeTime: 1_760_000_000_000}},
	}
	scope := execution.IntentScope{Account: accountScope, Exchange: venue.name, Market: venue.market, Symbol: symbol, Bot: botID}
	scopeKey, err := scope.Key()
	if err != nil {
		t.Fatal(err)
	}
	journal := &evidenceTestJournal{scope: scopeKey}
	request := order.OrderRequest{Symbol: symbol, Side: "BUY", Price: 100, Quantity: 2, ClientOrderID: cid, StrategyName: "Grid-BTCUSDT", StrategyType: "grid", PositionSide: "LONG", OrderSource: "normal"}
	intentOrder := &order.Order{OrderID: 73, ClientOrderID: cid, Symbol: symbol, Side: "BUY", Quantity: 2, ExecutedQty: 1.25, Status: "CANCELED"}
	payload, err := json.Marshal(evidencePersistedIntent{Version: 1, Scope: scope, Request: request, Opening: true, Order: intentOrder, Settled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.SaveExecutionIntent(t.Context(), scopeKey, cid, 0, payload); err != nil {
		t.Fatal(err)
	}
	executor := order.NewExchangeOrderExecutor(venue, symbol, 0, 0, lock.NewNopLock(), botID)
	if err := executor.ConfigureIntentJournal(t.Context(), journal, scope); err != nil {
		t.Fatal(err)
	}
	venue.executor = executor
	runtime := &SymbolRuntime{
		Config: config.SymbolConfig{Symbol: symbol}, Exchange: venue, ExchangeExecutor: executor,
		AccountScope: accountScope, AccountMarketType: "futures",
	}
	manager := &BotManager{runtimes: map[string]*BotRuntime{botID: {BotID: botID, Inner: runtime}}}
	item := storage.OrderReconciliationCase{
		Owner:                  accounting.OrderOwner{Exchange: venue.name, Market: venue.market, AccountScope: accountScope, Bot: botID, Symbol: symbol, StrategyName: "Grid-BTCUSDT", StrategyType: "grid", ClientOrderID: cid, VenueOrderID: "73"},
		ExpectedIntentRevision: "1", ExpectedStrategyRevision: "strategy-revision-1",
	}
	return newRuntimeTrustedOrderEvidenceProvider(manager), item, venue
}

func TestRuntimeTrustedOrderEvidenceProviderRejectsOwnerMismatch(t *testing.T) {
	provider, item, _ := newEvidenceProviderFixture(t)
	item.Owner.AccountScope = "other-account-scope"
	if _, err := provider.ReadVerifiedOrderEvidence(t.Context(), item); err == nil {
		t.Fatal("expected account owner mismatch to fail closed")
	}
}

func TestRuntimeTrustedOrderEvidenceProviderRejectsPartialFillHistory(t *testing.T) {
	provider, item, venue := newEvidenceProviderFixture(t)
	venue.fills = venue.fills[:0]
	if _, err := provider.ReadVerifiedOrderEvidence(t.Context(), item); err == nil {
		t.Fatal("expected missing executed fill history to fail closed")
	}
}

func TestRuntimeTrustedOrderEvidenceProviderRejectsUnknownFees(t *testing.T) {
	provider, item, venue := newEvidenceProviderFixture(t)
	venue.fills[0].CommissionAsset = " "
	if _, err := provider.ReadVerifiedOrderEvidence(t.Context(), item); err == nil {
		t.Fatal("expected unidentifiable fee asset to fail closed")
	}
}

func TestRuntimeTrustedOrderEvidenceProviderRejectsUnmarkedZeroFee(t *testing.T) {
	provider, item, venue := newEvidenceProviderFixture(t)
	venue.fills[0].Commission = 0
	if _, err := provider.ReadVerifiedOrderEvidence(t.Context(), item); err == nil {
		t.Fatal("expected an unmarked zero fee to remain unverified")
	}
}

func TestRuntimeTrustedOrderEvidenceProviderRejectsOpenOrder(t *testing.T) {
	provider, item, venue := newEvidenceProviderFixture(t)
	venue.order.Status = exchange.OrderStatusPartiallyFilled
	if _, err := provider.ReadVerifiedOrderEvidence(t.Context(), item); err == nil {
		t.Fatal("expected non-terminal venue order to fail closed")
	}
}

func TestRuntimeTrustedOrderEvidenceProviderAllowsKnownUnknownIntentAfterVenueReadback(t *testing.T) {
	provider, item, venue := newEvidenceProviderFixture(t)
	if err := venue.executor.MarkOrderReconciliationRequired(73, item.Owner.ClientOrderID, "strategy ledger write was uncertain"); err != nil {
		t.Fatal(err)
	}
	item.ExpectedIntentRevision = "2"
	got, err := provider.ReadVerifiedOrderEvidence(t.Context(), item)
	if err != nil {
		t.Fatalf("fresh exact terminal venue evidence should allow manual reconciliation of a known UNKNOWN intent: %v", err)
	}
	if !got.OwnerMatched || !got.VenueTerminal || !got.FillsComplete || !got.FeesComplete {
		t.Fatalf("manual evidence is incomplete: %+v", got)
	}
}

func TestRuntimeTrustedOrderEvidenceProviderRejectsDuplicateTradeIDs(t *testing.T) {
	provider, item, venue := newEvidenceProviderFixture(t)
	venue.fills[0].Quantity = 0.625
	duplicate := *venue.fills[0]
	duplicate.TradeTime++
	venue.fills = append(venue.fills, &duplicate)
	if _, err := provider.ReadVerifiedOrderEvidence(t.Context(), item); err == nil {
		t.Fatal("expected duplicate trade IDs to fail closed")
	}
}

func TestRuntimeTrustedOrderEvidenceProviderReturnsVerifiedTerminalFills(t *testing.T) {
	provider, item, venue := newEvidenceProviderFixture(t)
	second := &exchange.OrderFill{OrderID: 73, TradeID: "trade-73-b", Symbol: "BTCUSDT", Side: exchange.SideBuy, Price: 101, Quantity: 0.25, Commission: 0.0002, CommissionAsset: "USDT", TradeTime: 1_760_000_000_000}
	venue.fills[0].Quantity = 1
	venue.fills = append(venue.fills, second)
	got, err := provider.ReadVerifiedOrderEvidence(t.Context(), item)
	if err != nil {
		t.Fatal(err)
	}
	if got.Owner != item.Owner || got.IntentRevision != item.ExpectedIntentRevision || !got.OwnerMatched || !got.VenueTerminal || !got.FillsComplete || !got.FeesComplete {
		t.Fatalf("verified status flags or owner mismatch: %+v", got)
	}
	if got.Target.Sequence != 2 || got.Target.TradeID != second.TradeID || len(got.Fills) != 2 {
		t.Fatalf("unexpected stable fill cursor: target=%+v fills=%+v", got.Target, got.Fills)
	}
	if got.Fills[0].CursorSequence != 1 || got.Fills[0].TradeID != "trade-73-a" || got.Fills[0].PositionSide != "LONG" || got.Fills[0].OrderRole != "entry" {
		t.Fatalf("unexpected first verified fill: %+v", got.Fills[0])
	}
	if got.Fills[1].Price != "101" || got.Fills[1].Quantity != "0.25" || got.Fills[1].CommissionAmount != "0.0002" {
		t.Fatalf("unexpected canonical economic values: %+v", got.Fills[1])
	}
	if got.ExpectedResult.NetQuantity != "1.25" || len(got.ExpectedResult.RealizedPnL) != 0 {
		t.Fatalf("unexpected independently derived order result: %+v", got.ExpectedResult)
	}
}

func TestExactCanonicalFloatRejectsNonFiniteAndPreservesRoundTrip(t *testing.T) {
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := exactCanonicalFloat(value); err == nil {
			t.Fatalf("expected %v to be rejected", value)
		}
	}
	for _, value := range []float64{0, 0.25, 100, 1e-12, -0.5} {
		decimal, err := exactCanonicalFloat(value)
		if err != nil {
			t.Fatalf("exactCanonicalFloat(%v): %v", value, err)
		}
		parsed, err := strconv.ParseFloat(decimal, 64)
		if err != nil || parsed != value {
			t.Fatalf("decimal %q does not round-trip %v", decimal, value)
		}
	}
}
