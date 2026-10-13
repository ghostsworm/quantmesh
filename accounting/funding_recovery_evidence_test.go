package accounting

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func mustRecoveryFillIntentHash() string {
	intent := RecoveryOrderIntent{
		SchemaVersion: 1, Exchange: "binance", Market: "spot", Symbol: "BTCUSDT", Side: "BUY",
		OrderType: "LIMIT", TimeInForce: "GTC", Price: "67123.45", Quantity: "0.1",
		PriceDecimals: 2, ClientOrderID: "client-1",
	}
	hash, err := intent.CanonicalHash()
	if err != nil {
		panic(err)
	}
	return hash
}

func validRecoveryFillEvent() RecoveryEvent {
	return RecoveryEvent{
		ID: "fill-event-1", Source: "venue", Exchange: "binance", Market: "spot", AccountScope: "scope",
		Kind: RecoveryFillEvent, ExternalID: "trade-1", EpisodeID: "episode-1", ChildID: "child-1",
		CursorSequence: 1, Asset: "BTC", Amount: "0.1", SourceRevision: "rev-1", SourceHash: "hash-1",
		ObservedAt: time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC),
		FillEvidence: &RecoveryFillEvidence{
			ClientOrderID: "client-1", VenueOrderID: "venue-1", Symbol: "BTCUSDT", OrderIntentHash: mustRecoveryFillIntentHash(),
			Side: "BUY", BaseAsset: "BTC", PriceRaw: "67123.4500", QuantityRaw: "0.10000000",
			QuoteAsset: "USDT", QuoteQuantityRaw: "6712.345000000", CommissionRaw: "0.00000000",
			CommissionAsset: "BNB", CommissionKnown: true,
		},
	}
}

func validBoundRecoveryChild() RecoveryChild {
	return RecoveryChild{
		ID: "child-1", EpisodeID: "episode-1", Role: "spot", Exchange: "binance", Market: "spot",
		AccountScope: "scope", Bot: "bot-1", Symbol: "BTCUSDT", StrategyName: "FundingCarry",
		StrategyType: "funding_carry", ClientOrderID: "client-1", VenueOrderID: "venue-1",
		Side: "BUY", OrderIntentHash: mustRecoveryFillIntentHash(), State: ChildBound,
		FillCursor: Cursor{},
	}
}

func TestRecoveryFillEvidencePreservesRawDecimalsAndExplicitZeroCommission(t *testing.T) {
	event := validRecoveryFillEvent()
	if err := event.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var decoded RecoveryEvent
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	event = decoded
	if event.FillEvidence.PriceRaw != "67123.4500" || event.FillEvidence.QuantityRaw != "0.10000000" ||
		event.FillEvidence.QuoteQuantityRaw != "6712.345000000" || event.FillEvidence.CommissionRaw != "0.00000000" || !event.FillEvidence.CommissionKnown {
		t.Fatalf("raw evidence was changed: %+v", event.FillEvidence)
	}
}

func TestRecoveryEventDecodesLegacyJSONWithoutFillEvidence(t *testing.T) {
	const legacy = `{"event_id":"event-legacy","source":"venue","exchange":"binance","market":"spot","account_scope":"scope","event_type":"fill","external_id":"trade-legacy","episode_id":"episode-legacy","child_id":"child-legacy","cursor_sequence":1,"asset":"BTC","amount":"1","source_revision":"rev-1","source_hash":"hash-1","observed_at":"2026-10-12T10:00:00Z"}`
	var event RecoveryEvent
	if err := json.Unmarshal([]byte(legacy), &event); err != nil {
		t.Fatalf("Unmarshal legacy event: %v", err)
	}
	if event.FillEvidence != nil {
		t.Fatalf("legacy event unexpectedly acquired fill evidence: %+v", event.FillEvidence)
	}
	if err := event.Validate(); err == nil {
		t.Fatal("legacy fill event without evidence passed validation")
	}
}

func TestRecoveryFillEventRequiresRawEvidence(t *testing.T) {
	event := validRecoveryFillEvent()
	event.FillEvidence = nil
	if err := event.Validate(); err == nil {
		t.Fatal("fill event without evidence passed validation")
	}
}

func TestRecoveryFillEventMatchesRawQuantityWithoutFloat(t *testing.T) {
	for _, amount := range []string{"0.1", "0.10", "0.10000000"} {
		t.Run(amount, func(t *testing.T) {
			event := validRecoveryFillEvent()
			event.Amount = amount
			if err := event.Validate(); err != nil {
				t.Fatalf("equivalent decimal quantity rejected: %v", err)
			}
		})
	}
	for _, amount := range []string{"0.10000001", "1", "0"} {
		t.Run("mismatch-"+amount, func(t *testing.T) {
			event := validRecoveryFillEvent()
			event.Amount = amount
			if err := event.Validate(); err == nil {
				t.Fatalf("mismatched amount %q passed validation", amount)
			}
		})
	}
}

func TestRecoveryFillUnknownCommissionRemainsUnknown(t *testing.T) {
	event := validRecoveryFillEvent()
	event.FillEvidence.CommissionKnown = false
	event.FillEvidence.CommissionRaw = ""
	event.FillEvidence.CommissionAsset = ""
	if err := event.Validate(); err != nil {
		t.Fatalf("unknown commission should preserve the fill fact: %v", err)
	}
}

func TestRecoveryFeeAdjustmentRequiresRawAmountAssetTradeAndEvidenceDigest(t *testing.T) {
	fee := RecoveryEvent{
		ID: "fee-adjustment-1", Source: RecoveryManualFeeAdjustmentSource, Exchange: "binance", Market: "spot", AccountScope: "scope",
		Kind: RecoveryFeeEvent, ExternalID: "statement-line-1", RelatedExternalID: "trade-1", EpisodeID: "episode-1", ChildID: "child-1",
		Asset: "BNB", Amount: "-0.0001200", SourceRevision: "source-revision-1", SourceHash: fmt.Sprintf("%064x", 17),
		ObservedAt: time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC),
	}
	if err := fee.Validate(); err != nil {
		t.Fatalf("valid signed raw fee adjustment rejected: %v", err)
	}
	for name, mutate := range map[string]func(*RecoveryEvent){
		"missing child":           func(event *RecoveryEvent) { event.ChildID = "" },
		"missing fill link":       func(event *RecoveryEvent) { event.RelatedExternalID = "" },
		"invalid evidence digest": func(event *RecoveryEvent) { event.SourceHash = "not-a-digest" },
		"exponent amount":         func(event *RecoveryEvent) { event.Amount = "1e-8" },
		"plus sign":               func(event *RecoveryEvent) { event.Amount = "+0.1" },
	} {
		t.Run(name, func(t *testing.T) {
			invalid := fee
			mutate(&invalid)
			if err := invalid.Validate(); err == nil {
				t.Fatal("malformed fee adjustment passed validation")
			}
		})
	}
}

func TestRecoveryVenueFeeEventRemainsBackwardCompatible(t *testing.T) {
	fee := RecoveryEvent{
		ID: "venue-fee-1", Source: "venue", Exchange: "binance", Market: "spot", AccountScope: "scope",
		Kind: RecoveryFeeEvent, ExternalID: "commission-1", EpisodeID: "episode-1", Asset: "USDT", Amount: "0.01",
		SourceRevision: "venue-revision-1", SourceHash: "venue-payload-hash",
		ObservedAt: time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC),
	}
	if err := fee.Validate(); err != nil {
		t.Fatalf("existing venue fee event contract unexpectedly became invalid: %v", err)
	}
}

func TestRecoveryChildBoundRequiresSideAndIntentHash(t *testing.T) {
	child := validBoundRecoveryChild()
	if err := child.Validate(); err != nil {
		t.Fatalf("valid bound child rejected: %v", err)
	}
	for _, mutate := range []func(*RecoveryChild){
		func(c *RecoveryChild) { c.Side = "" },
		func(c *RecoveryChild) { c.Side = "BID" },
		func(c *RecoveryChild) { c.OrderIntentHash = "" },
		func(c *RecoveryChild) { c.OrderIntentHash = "not-a-hash" },
	} {
		invalid := validBoundRecoveryChild()
		mutate(&invalid)
		if err := invalid.Validate(); err == nil {
			t.Errorf("invalid bound child passed validation: %+v", invalid)
		}
	}
}

func TestRecoveryChildAllowsLegacyUnboundWithoutOrderIdentity(t *testing.T) {
	child := validBoundRecoveryChild()
	child.State = ChildProvisional
	child.VenueOrderID = ""
	child.Side = ""
	child.OrderIntentHash = ""
	if err := child.Validate(); err != nil {
		t.Fatalf("legacy unbound child rejected: %v", err)
	}
	child.Side = "BUY"
	if err := child.Validate(); err == nil {
		t.Fatal("new unbound order child with a missing intent hash passed validation")
	}
}

func TestOneShotPermitGateSourceExceptionsAreCanonicalAndAllowlisted(t *testing.T) {
	valid := []string{"execution_intent_recovery", "funding_carry_exposure_unverified"}
	if err := ValidateGateSourceExceptions(valid); err != nil {
		t.Fatalf("approved sorted sources rejected: %v", err)
	}
	for _, sources := range [][]string{
		{"funding_carry_exposure_unverified", "execution_intent_recovery"},
		{"execution_intent_recovery", "execution_intent_recovery"},
		{"manual"},
		{"risk"},
	} {
		if err := ValidateGateSourceExceptions(sources); err == nil {
			t.Errorf("invalid source set %q was accepted", sources)
		}
	}
}

func TestOneShotPermitDecodesLegacyJSONWithoutGateSourceExceptions(t *testing.T) {
	const legacy = `{"permit_id":"p1","episode_id":"e1","recovery_generation":1,"action_id":"a1","action_ordinal":1,"action_kind":"borrow","account_scope":"scope","owner_generation":"owner","lease_id":1,"expected_episode_revision":1,"state":"authorized"}`
	var permit OneShotPermit
	if err := json.Unmarshal([]byte(legacy), &permit); err != nil {
		t.Fatal(err)
	}
	if err := permit.Validate(); err != nil {
		t.Fatalf("legacy permit without optional exceptions rejected: %v", err)
	}
}

func TestRecoveryOrderIntentHashBindsClientIDMarketAndOrderParameters(t *testing.T) {
	intent := RecoveryOrderIntent{
		SchemaVersion: 1, Exchange: "binance", Market: "futures", Symbol: "BTCUSDT", Side: "BUY", OrderType: "LIMIT", TimeInForce: "GTC",
		Price: "100", Quantity: "0.25", PriceDecimals: 2, ClientOrderID: "durable-cid",
	}
	first, err := intent.CanonicalHash()
	if err != nil {
		t.Fatal(err)
	}
	mutations := []func(*RecoveryOrderIntent){
		func(i *RecoveryOrderIntent) { i.Exchange = "bybit" },
		func(i *RecoveryOrderIntent) { i.Market = "spot" },
		func(i *RecoveryOrderIntent) { i.Symbol = "ETHUSDT" },
		func(i *RecoveryOrderIntent) { i.Side = "SELL" },
		func(i *RecoveryOrderIntent) { i.Quantity = "0.5" },
		func(i *RecoveryOrderIntent) { i.Price = "101" },
		func(i *RecoveryOrderIntent) { i.ClientOrderID = "another-cid" },
		func(i *RecoveryOrderIntent) { i.ReduceOnly = true },
	}
	for index, mutate := range mutations {
		changed := intent
		mutate(&changed)
		hash, err := changed.CanonicalHash()
		if err != nil {
			t.Fatalf("mutation %d became invalid: %v", index, err)
		}
		if hash == first {
			t.Errorf("mutation %d did not change authorized request hash", index)
		}
	}
}

func TestRecoveryFillEvidenceRejectsMalformedOrAmbiguousRawValues(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*RecoveryEvent)
	}{
		{"missing client order id", func(e *RecoveryEvent) { e.FillEvidence.ClientOrderID = " " }},
		{"missing venue order id", func(e *RecoveryEvent) { e.FillEvidence.VenueOrderID = "" }},
		{"missing symbol", func(e *RecoveryEvent) { e.FillEvidence.Symbol = "" }},
		{"missing order intent hash", func(e *RecoveryEvent) { e.FillEvidence.OrderIntentHash = "" }},
		{"malformed order intent hash", func(e *RecoveryEvent) { e.FillEvidence.OrderIntentHash = "invalid" }},
		{"exponent notation", func(e *RecoveryEvent) { e.FillEvidence.PriceRaw = "6.712345e4" }},
		{"zero quantity", func(e *RecoveryEvent) { e.FillEvidence.QuantityRaw = "0.0000" }},
		{"mismatched event amount", func(e *RecoveryEvent) { e.Amount = "0.11" }},
		{"missing known commission", func(e *RecoveryEvent) { e.FillEvidence.CommissionRaw = "" }},
		{"known commission missing asset", func(e *RecoveryEvent) { e.FillEvidence.CommissionAsset = " " }},
		{"unknown commission with value", func(e *RecoveryEvent) { e.FillEvidence.CommissionKnown = false }},
		{"unknown commission with asset", func(e *RecoveryEvent) {
			e.FillEvidence.CommissionKnown = false
			e.FillEvidence.CommissionRaw = ""
			e.FillEvidence.CommissionAsset = "BNB"
		}},
		{"quote without asset", func(e *RecoveryEvent) { e.FillEvidence.QuoteAsset = "" }},
		{"unsupported side", func(e *RecoveryEvent) { e.FillEvidence.Side = "BID" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			event := validRecoveryFillEvent()
			test.mutate(&event)
			if err := event.Validate(); err == nil {
				t.Fatal("Validate() accepted ambiguous raw fill evidence")
			}
		})
	}
}

func TestRecoveryOperationHashIncludesRawFillEvidence(t *testing.T) {
	event := validRecoveryFillEvent()
	op := RecoveryOperation{
		EpisodeID: event.EpisodeID, OperationID: "op-1", HashVersion: 1,
		ExpectedEpisodeRevision: 1, ExpectedRuntimeRevision: "runtime-1", Actor: "operator", AuditRef: "audit-1",
		EvidenceHash: fmt.Sprintf("%064x", 1), EvidenceReadAt: time.Now().UTC(), Events: []RecoveryEvent{event},
	}
	first, err := op.CanonicalHash()
	if err != nil {
		t.Fatalf("CanonicalHash() error = %v", err)
	}
	op.Events[0].FillEvidence.QuantityRaw = "0.10000001"
	second, err := op.CanonicalHash()
	if err != nil {
		t.Fatalf("CanonicalHash() after raw evidence change error = %v", err)
	}
	if first == second {
		t.Fatal("canonical operation hash ignored raw fill evidence")
	}
	for _, mutate := range []func(*RecoveryFillEvidence){
		func(e *RecoveryFillEvidence) { e.ClientOrderID = "client-2" },
		func(e *RecoveryFillEvidence) { e.VenueOrderID = "venue-2" },
		func(e *RecoveryFillEvidence) { e.Symbol = "ETHUSDT" },
		func(e *RecoveryFillEvidence) { e.OrderIntentHash = fmt.Sprintf("%064x", 2) },
	} {
		op.Events[0] = event
		mutate(op.Events[0].FillEvidence)
		hash, err := op.CanonicalHash()
		if err != nil {
			t.Fatalf("CanonicalHash() after identity change: %v", err)
		}
		if hash == first {
			t.Fatal("canonical operation hash ignored a fill identity field")
		}
	}
}

func TestRecoveryOperationCanonicalHashDoesNotMutateSharedInput(t *testing.T) {
	zone := time.FixedZone("audit-zone", 8*60*60)
	from := time.Date(2026, 10, 13, 8, 0, 0, 0, zone)
	through := from.Add(time.Hour)
	op := RecoveryOperation{
		EpisodeID: "episode-canonical-copy", OperationID: "operation-canonical-copy", HashVersion: 1,
		ExpectedEpisodeRevision: 1, ExpectedRuntimeRevision: "runtime-1", Actor: "operator", AuditRef: "audit-ref",
		EvidenceHash: fmt.Sprintf("%064x", 22), EvidenceReadAt: time.Now().UTC(),
		Children:     []RecoveryChild{{ID: "z-child"}, {ID: "a-child"}},
		Events:       []RecoveryEvent{{ID: "z-event"}, {ID: "a-event"}},
		SourceWindow: &RecoverySourceWindow{From: from, Through: through},
	}
	if _, err := op.CanonicalHash(); err != nil {
		t.Fatal("canonical hash:", err)
	}
	if op.Children[0].ID != "z-child" || op.Children[1].ID != "a-child" || op.Events[0].ID != "z-event" || op.Events[1].ID != "a-event" {
		t.Fatalf("canonical hashing reordered caller-owned slices: children=%+v events=%+v", op.Children, op.Events)
	}
	if op.SourceWindow.From.Location() != zone || op.SourceWindow.Through.Location() != zone {
		t.Fatalf("canonical hashing normalized the caller-owned source window in place: from=%v through=%v", op.SourceWindow.From, op.SourceWindow.Through)
	}
}
