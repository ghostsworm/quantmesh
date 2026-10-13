package order

import (
	"context"
	"errors"
	"testing"

	"quantmesh/accounting"
	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/lock"
)

type recoveryPermitConsumerFixture struct {
	permit    accounting.OneShotPermit
	maySubmit bool
	calls     int
	ackCalls  int
	ackErr    error
	venueID   string
}

func (f *recoveryPermitConsumerFixture) AcknowledgeRecoverySubmission(_ context.Context, permit accounting.OneShotPermit, venueOrderID string) error {
	f.ackCalls++
	if permit.ID != f.permit.ID {
		return accounting.ErrRecoveryConflict
	}
	f.venueID = venueOrderID
	return f.ackErr
}

func (f *recoveryPermitConsumerFixture) ConsumeRecoveryPermit(_ context.Context, permit accounting.OneShotPermit) (accounting.OneShotPermit, bool, error) {
	f.calls++
	if permit.ID != f.permit.ID {
		return accounting.OneShotPermit{}, false, accounting.ErrRecoveryConflict
	}
	consumed := f.permit
	consumed.State = accounting.PermitConsumed
	return consumed, f.maySubmit, nil
}

func TestPlaceRecoveryOrderConsumesExactPermitForOnePhysicalAttempt(t *testing.T) {
	ex := newRecoveryAckOrderExchange("futures")
	oe := NewExchangeOrderExecutor(ex, "BTCUSDT", 0, 0, lock.NewNopLock(), "bot")
	var gate execution.OpeningGate
	oe.SetOpeningGate(&gate, "LONG")
	const episodeID = "episode-order-submit"
	const exception = "execution_intent_recovery"
	gate.Block(exception)
	gate.Block(execution.RecoveryEpisodeGateSource(episodeID, 1))
	req := &OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Type: "LIMIT", TimeInForce: "GTC", Price: 100, Quantity: 0.25, ClientOrderID: "durable-cid", StrategyName: "funding_carry"}
	orderType, tif, err := validateOrderExecution(req)
	if err != nil {
		t.Fatal(err)
	}
	intentHash, err := CanonicalRecoveryOrderIntentHash(oe, req, orderType, tif)
	if err != nil {
		t.Fatal(err)
	}
	intent := accounting.RecoveryOrderIntent{SchemaVersion: 1, Exchange: ex.GetName(), Market: "futures", Symbol: req.Symbol, Side: req.Side, OrderType: string(orderType), TimeInForce: string(tif), Price: "100", Quantity: "0.25", ClientOrderID: req.ClientOrderID}
	permit := accounting.OneShotPermit{
		ID: "permit-order-submit", EpisodeID: episodeID, Generation: 1, ActionID: "action-1", ActionOrdinal: 1,
		Kind: accounting.RecoveryActionOrder, AccountScope: "scope", OwnerGeneration: "owner", LeaseID: 1,
		GateSourceExceptions: []string{exception}, OrderIntentHash: intentHash, OrderIntent: &intent, ExpectedRevision: 1, State: accounting.PermitAuthorized,
	}
	consumer := &recoveryPermitConsumerFixture{permit: permit, maySubmit: true}
	placed, err := oe.PlaceRecoveryOrderContext(context.Background(), req, permit, consumer)
	if err != nil || placed == nil || len(ex.placed) != 1 || consumer.calls != 1 || consumer.ackCalls != 1 || consumer.venueID != "1" {
		t.Fatalf("authorized recovery order failed: placed=%+v consume=%d ack=%d venueID=%q err=%v", placed, consumer.calls, consumer.ackCalls, consumer.venueID, err)
	}
	if ex.placed[0].ClientOrderID != req.ClientOrderID {
		t.Fatalf("venue CID=%q, want durable CID %q", ex.placed[0].ClientOrderID, req.ClientOrderID)
	}
	if !gate.HasBlock(exception) || !gate.HasBlock(execution.RecoveryEpisodeGateSource(episodeID, 1)) {
		t.Fatal("successful physical attempt cleared one of the recovery holds")
	}
	if _, err := oe.PlaceRecoveryOrderContext(context.Background(), req, permit, consumer); err == nil || len(ex.placed) != 1 {
		t.Fatalf("one-shot permit was reusable: calls=%d err=%v", len(ex.placed), err)
	}
}

func TestPlaceRecoveryOrderKeepsUnknownWhenAcknowledgementPersistenceFails(t *testing.T) {
	ex := newRecoveryAckOrderExchange("futures")
	oe := NewExchangeOrderExecutor(ex, "BTCUSDT", 0, 0, lock.NewNopLock(), "bot")
	var gate execution.OpeningGate
	oe.SetOpeningGate(&gate, "LONG")
	permit := recoveryOrderPermitForTest(t, oe)
	gate.Block("execution_intent_recovery")
	gate.Block(execution.RecoveryEpisodeGateSource(permit.EpisodeID, permit.Generation))
	consumer := &recoveryPermitConsumerFixture{permit: permit, maySubmit: true, ackErr: errors.New("database unavailable")}
	req := &OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Type: "LIMIT", TimeInForce: "GTC", Price: 100, Quantity: 0.25, ClientOrderID: "durable-cid"}
	if _, err := oe.PlaceRecoveryOrderContext(context.Background(), req, permit, consumer); !errors.Is(err, execution.ErrOrderUnknown) {
		t.Fatalf("ack persistence failure = %v; want UNKNOWN", err)
	}
	if len(ex.placed) != 1 || consumer.ackCalls != 1 || !gate.HasBlock("execution_intent_recovery") || !gate.HasBlock(execution.RecoveryEpisodeGateSource(permit.EpisodeID, permit.Generation)) {
		t.Fatalf("failed acknowledgement persistence changed safety state: venue=%d ack=%d", len(ex.placed), consumer.ackCalls)
	}
}

func TestPlaceRecoveryOrderRejectsVenueAcknowledgementWithoutIdentityEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*exchange.Order)
	}{
		{name: "missing client order ID", mutate: func(order *exchange.Order) { order.ClientOrderID = "" }},
		{name: "foreign client order ID", mutate: func(order *exchange.Order) { order.ClientOrderID = "foreign-cid" }},
		{name: "missing symbol", mutate: func(order *exchange.Order) { order.Symbol = "" }},
		{name: "foreign symbol", mutate: func(order *exchange.Order) { order.Symbol = "ETHUSDT" }},
		{name: "missing side", mutate: func(order *exchange.Order) { order.Side = "" }},
		{name: "foreign side", mutate: func(order *exchange.Order) { order.Side = exchange.SideSell }},
		{name: "nonpositive venue order ID", mutate: func(order *exchange.Order) { order.OrderID = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ex := newRecoveryAckOrderExchange("futures")
			permit := recoveryOrderPermitForTest(t, NewExchangeOrderExecutor(ex, "BTCUSDT", 0, 0, lock.NewNopLock(), "bot"))
			ex.ackMutate = tc.mutate
			oe := NewExchangeOrderExecutor(ex, "BTCUSDT", 0, 0, lock.NewNopLock(), "bot")
			var gate execution.OpeningGate
			oe.SetOpeningGate(&gate, "LONG")
			gate.Block("execution_intent_recovery")
			gate.Block(execution.RecoveryEpisodeGateSource(permit.EpisodeID, permit.Generation))
			consumer := &recoveryPermitConsumerFixture{permit: permit, maySubmit: true}
			req := &OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Type: "LIMIT", TimeInForce: "GTC", Price: 100, Quantity: 0.25, ClientOrderID: "durable-cid"}
			if _, err := oe.PlaceRecoveryOrderContext(context.Background(), req, permit, consumer); !errors.Is(err, execution.ErrOrderUnknown) {
				t.Fatalf("incomplete venue identity error = %v; want UNKNOWN", err)
			}
			if consumer.ackCalls != 0 || !gate.HasBlock("execution_intent_recovery") {
				t.Fatalf("incomplete venue identity was acknowledged or unblocked: ack=%d", consumer.ackCalls)
			}
		})
	}
}

func TestPlaceRecoveryOrderRejectsRequestDriftAndExtraGateSourceBeforeCAS(t *testing.T) {
	for _, mutate := range []struct {
		name          string
		gateSource    string
		changeRequest bool
	}{
		{name: "request drift", changeRequest: true},
		{name: "manual hold", gateSource: "manual"},
		{name: "risk hold", gateSource: "risk"},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			ex := &gatedOrderExchange{market: "futures"}
			oe := NewExchangeOrderExecutor(ex, "BTCUSDT", 0, 0, lock.NewNopLock(), "bot")
			var gate execution.OpeningGate
			oe.SetOpeningGate(&gate, "LONG")
			permit := recoveryOrderPermitForTest(t, oe)
			req := &OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Type: "LIMIT", TimeInForce: "GTC", Price: 100, Quantity: 0.25, ClientOrderID: "durable-cid"}
			if mutate.changeRequest {
				req.Quantity = 0.5
			}
			gate.Block("execution_intent_recovery")
			gate.Block(execution.RecoveryEpisodeGateSource(permit.EpisodeID, permit.Generation))
			if mutate.gateSource != "" {
				gate.Block(mutate.gateSource)
			}
			consumer := &recoveryPermitConsumerFixture{permit: permit, maySubmit: true}
			if _, err := oe.PlaceRecoveryOrderContext(context.Background(), req, permit, consumer); err == nil || consumer.calls != 0 || len(ex.placed) != 0 {
				t.Fatalf("invalid recovery submission crossed durable CAS: consumer=%d venue=%d err=%v", consumer.calls, len(ex.placed), err)
			}
			if mutate.gateSource != "" && !gate.HasBlock(mutate.gateSource) {
				t.Fatalf("independent %s source was cleared", mutate.gateSource)
			}
		})
	}
}

func TestPlaceRecoveryOrderDoesNotRetryDefinitiveVenueRefusal(t *testing.T) {
	ex := &recoveryRefusalOrderExchange{gatedOrderExchange: gatedOrderExchange{market: "futures"}}
	oe := NewExchangeOrderExecutor(ex, "BTCUSDT", 0, 0, lock.NewNopLock(), "bot")
	var gate execution.OpeningGate
	oe.SetOpeningGate(&gate, "LONG")
	permit := recoveryOrderPermitForTest(t, oe)
	gate.Block("execution_intent_recovery")
	gate.Block(execution.RecoveryEpisodeGateSource(permit.EpisodeID, permit.Generation))
	consumer := &recoveryPermitConsumerFixture{permit: permit, maySubmit: true}
	req := &OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Type: "LIMIT", TimeInForce: "GTC", Price: 100, Quantity: 0.25, ClientOrderID: "durable-cid"}
	if _, err := oe.PlaceRecoveryOrderContext(context.Background(), req, permit, consumer); !errors.Is(err, execution.ErrOrderUnknown) {
		t.Fatalf("venue refusal error = %v; want recovery UNKNOWN classification", err)
	}
	if len(ex.placed) != 1 || consumer.calls != 1 {
		t.Fatalf("recovery action retried after one-shot consumption: venue=%d consume=%d", len(ex.placed), consumer.calls)
	}
}

type recoveryRefusalOrderExchange struct{ gatedOrderExchange }

func (e *recoveryRefusalOrderExchange) PlaceOrder(_ context.Context, req *exchange.OrderRequest) (*exchange.Order, error) {
	e.placed = append(e.placed, req)
	return nil, errors.New("-1003 rate limit")
}

type recoveryAckOrderExchange struct {
	gatedOrderExchange
	ackMutate func(*exchange.Order)
}

func newRecoveryAckOrderExchange(market string) *recoveryAckOrderExchange {
	return &recoveryAckOrderExchange{gatedOrderExchange: gatedOrderExchange{market: market}}
}

func (e *recoveryAckOrderExchange) PlaceOrder(_ context.Context, req *exchange.OrderRequest) (*exchange.Order, error) {
	e.placed = append(e.placed, req)
	ack := &exchange.Order{OrderID: int64(len(e.placed)), ClientOrderID: req.ClientOrderID, Symbol: req.Symbol, Side: exchange.Side(req.Side), Status: exchange.OrderStatusNew}
	if e.ackMutate != nil {
		e.ackMutate(ack)
	}
	return ack, nil
}

func recoveryOrderPermitForTest(t *testing.T, oe *ExchangeOrderExecutor) accounting.OneShotPermit {
	t.Helper()
	req := &OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Type: "LIMIT", TimeInForce: "GTC", Price: 100, Quantity: 0.25, ClientOrderID: "durable-cid"}
	orderType, tif, err := validateOrderExecution(req)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := CanonicalRecoveryOrderIntentHash(oe, req, orderType, tif)
	if err != nil {
		t.Fatal(err)
	}
	intent := accounting.RecoveryOrderIntent{SchemaVersion: 1, Exchange: oe.exchange.GetName(), Market: "futures", Symbol: req.Symbol, Side: req.Side, OrderType: string(orderType), TimeInForce: string(tif), Price: "100", Quantity: "0.25", ClientOrderID: req.ClientOrderID}
	return accounting.OneShotPermit{
		ID: "permit-order-submit", EpisodeID: "episode-order-submit", Generation: 1, ActionID: "action-1", ActionOrdinal: 1,
		Kind: accounting.RecoveryActionOrder, AccountScope: "scope", OwnerGeneration: "owner", LeaseID: 1,
		GateSourceExceptions: []string{"execution_intent_recovery"}, OrderIntentHash: hash, OrderIntent: &intent, ExpectedRevision: 1, State: accounting.PermitAuthorized,
	}
}

type leaseRetryOrderExchange struct{ fakeOrderExchange }

func (e *leaseRetryOrderExchange) PlaceOrder(_ context.Context, req *exchange.OrderRequest) (*exchange.Order, error) {
	e.placed = append(e.placed, req)
	if len(e.placed) == 1 {
		return nil, errors.New("-1003 rate limit")
	}
	return &exchange.Order{OrderID: int64(len(e.placed)), ClientOrderID: req.ClientOrderID, Status: exchange.OrderStatusFilled, ExecutedQty: req.Quantity}, nil
}

type gatedOrderExchange struct {
	fakeOrderExchange
	market string
}

func (e *gatedOrderExchange) GetMarketType() string { return e.market }

func TestOpeningGateAtPhysicalExecutor(t *testing.T) {
	for _, tc := range []struct {
		name, market, direction, leg, side string
		reduce, wantAllowed                bool
	}{
		{"grid long open", "futures", "LONG", "", "BUY", false, false},
		{"short open", "futures", "SHORT", "SHORT", "SELL", false, false},
		{"unclassified both", "futures", "BOTH", "", "SELL", false, false},
		{"long close", "futures", "LONG", "LONG", "SELL", true, true},
		{"short close", "futures", "SHORT", "SHORT", "BUY", true, true},
		{"unsafe futures non-reduce", "futures", "LONG", "LONG", "SELL", false, false},
		{"spot open", "spot", "LONG", "LONG", "BUY", false, false},
		{"spot inventory sale", "spot", "LONG", "LONG", "SELL", false, true},
		{"spot default direction close", "spot", "", "", "SELL", false, true},
		{"spot reduce flag cannot authorize buy", "spot", "LONG", "LONG", "BUY", true, false},
		{"spot short inventory rebuy", "spot", "LONG", "SHORT", "BUY", false, true},
		{"unknown spot leg", "spot", "BOTH", "", "SELL", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ex := &gatedOrderExchange{market: tc.market}
			oe := NewExchangeOrderExecutor(ex, "BTCUSDT", 0, 0, lock.NewNopLock(), "")
			var gate execution.OpeningGate
			oe.SetOpeningGate(&gate, tc.direction)
			gate.Block("test")
			req := &OrderRequest{Symbol: "BTCUSDT", Side: tc.side, Price: 100, Quantity: 1, PositionSide: tc.leg, ReduceOnly: tc.reduce, ClientOrderID: "test"}
			ord, err := oe.PlaceOrder(req)
			if tc.wantAllowed {
				if err != nil || ord == nil || len(ex.placed) != 1 {
					t.Fatalf("close blocked: order=%v err=%v calls=%d", ord, err, len(ex.placed))
				}
			} else if !errors.Is(err, execution.ErrOpeningPaused) || ord != nil || len(ex.placed) != 0 {
				t.Fatalf("opening reached exchange: order=%v err=%v calls=%d", ord, err, len(ex.placed))
			}
		})
	}
}

func TestOpeningGateBatchAndResume(t *testing.T) {
	ex := &gatedOrderExchange{market: "futures"}
	oe := NewExchangeOrderExecutor(ex, "BTCUSDT", 0, 0, lock.NewNopLock(), "")
	var gate execution.OpeningGate
	oe.SetOpeningGate(&gate, "LONG")
	gate.Block("manual")
	reqs := []*OrderRequest{
		{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "open"},
		{Symbol: "BTCUSDT", Side: "SELL", Price: 101, Quantity: 1, ReduceOnly: true, ClientOrderID: "close"},
	}
	result := oe.BatchPlaceOrdersWithDetails(reqs)
	if len(result.PlacedOrders) != 1 || result.PlacedOrders[0].Side != "SELL" || len(ex.placed) != 1 {
		t.Fatalf("batch bypassed gate or lost close: %+v", result)
	}
	gate.Unblock("manual")
	if _, err := oe.PlaceOrder(reqs[0]); err != nil || len(ex.placed) != 2 {
		t.Fatalf("resume did not restore admission: %v", err)
	}
}

func TestOpeningAdmissionGuardRunsOnlyForOpeningsAndFailsClosed(t *testing.T) {
	ex := &gatedOrderExchange{market: "futures"}
	oe := NewExchangeOrderExecutor(ex, "BTCUSDT", 0, 0, lock.NewNopLock(), "")
	var gate execution.OpeningGate
	oe.SetOpeningGate(&gate, "LONG")
	guardCalls := 0
	oe.SetOpeningAdmissionGuard(func(context.Context) error {
		guardCalls++
		return errors.New("wallet evidence unavailable")
	})
	opening := &OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1}
	if _, err := oe.PlaceOrder(opening); err == nil || guardCalls != 1 || len(ex.placed) != 0 {
		t.Fatalf("opening guard did not fail closed: calls=%d placed=%d err=%v", guardCalls, len(ex.placed), err)
	}
	closing := &OrderRequest{Symbol: "BTCUSDT", Side: "SELL", Price: 100, Quantity: 1, ReduceOnly: true}
	if _, err := oe.PlaceOrder(closing); err != nil || guardCalls != 1 || len(ex.placed) != 1 {
		t.Fatalf("opening guard blocked protective close: calls=%d placed=%d err=%v", guardCalls, len(ex.placed), err)
	}
}

func TestOpeningAdmissionGuardRechecksBeforeEveryPhysicalRetry(t *testing.T) {
	ex := &leaseRetryOrderExchange{}
	oe := NewExchangeOrderExecutor(ex, "BTCUSDT", 0, 0, lock.NewNopLock(), "")
	var gate execution.OpeningGate
	oe.SetOpeningGate(&gate, "LONG")
	guardCalls := 0
	leaseLost := errors.New("runtime ownership lease expired")
	oe.SetOpeningAdmissionGuard(func(context.Context) error {
		guardCalls++
		if guardCalls == 3 {
			return leaseLost
		}
		return nil
	})
	_, err := oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "lease-retry"})
	if !errors.Is(err, leaseLost) {
		t.Fatalf("retry after ownership loss error = %v, want %v", err, leaseLost)
	}
	if guardCalls != 3 || len(ex.placed) != 1 {
		t.Fatalf("ownership admission was not rechecked before each physical RPC: guard=%d submissions=%d", guardCalls, len(ex.placed))
	}
}

func TestPhysicalSubmissionGuardBlocksStaleOwnerOpensAndCloses(t *testing.T) {
	for _, tc := range []struct {
		name      string
		side      string
		reduce    bool
		loseAt    int
		wantCalls int
	}{
		{name: "opening loses ownership before first RPC", side: "BUY", loseAt: 2},
		{name: "protective close loses ownership before first RPC", side: "SELL", reduce: true, loseAt: 2},
		{name: "opening loses ownership between retries", side: "BUY", loseAt: 3, wantCalls: 1},
		{name: "protective close loses ownership between retries", side: "SELL", reduce: true, loseAt: 3, wantCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ex := &leaseRetryOrderExchange{}
			oe := NewExchangeOrderExecutor(ex, "BTCUSDT", 0, 0, lock.NewNopLock(), "")
			var gate execution.OpeningGate
			oe.SetOpeningGate(&gate, "LONG")
			guardCalls := 0
			leaseLost := errors.New("runtime owner lease lost")
			oe.SetPhysicalSubmissionGuard(func(context.Context) error {
				guardCalls++
				if guardCalls == tc.loseAt {
					return leaseLost
				}
				return nil
			})
			_, err := oe.PlaceOrder(&OrderRequest{
				Symbol: "BTCUSDT", Side: tc.side, Price: 100, Quantity: 1,
				ReduceOnly: tc.reduce, ClientOrderID: "owner-guard-" + tc.name,
			})
			if !errors.Is(err, leaseLost) {
				t.Fatalf("submission error = %v, want owner lease loss", err)
			}
			if len(ex.placed) != tc.wantCalls {
				t.Fatalf("venue submissions = %d, want %d", len(ex.placed), tc.wantCalls)
			}
		})
	}
}
