package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/storage"
)

type fundingCarryCoverJournalExchange struct {
	*fundingCarryRepayIntentExchange
	afterPlace   func()
	afterFills   func()
	orderQueries int
}

func (e *fundingCarryCoverJournalExchange) PlaceOrder(ctx context.Context, req *exchange.OrderRequest) (*exchange.Order, error) {
	order, err := e.mockFCExchange.PlaceOrder(ctx, req)
	if e.afterPlace != nil {
		e.afterPlace()
	}
	return order, err
}
func (e *fundingCarryCoverJournalExchange) GetOrder(ctx context.Context, symbol string, id int64) (*exchange.Order, error) {
	e.orderQueries++
	return e.mockFCExchange.GetOrder(ctx, symbol, id)
}
func (e *fundingCarryCoverJournalExchange) GetOrderFills(ctx context.Context, symbol string, id int64) ([]*exchange.OrderFill, error) {
	fills, err := e.mockFCExchange.GetOrderFills(ctx, symbol, id)
	if e.afterFills != nil {
		e.afterFills()
	}
	return fills, err
}

func TestFundingCarryCoverJournalSurvivesRepaymentLookupFailure(t *testing.T) {
	s, margin, store := newFundingCarryRepayIntentFixture()
	if err := s.closeReverse(context.Background(), "journal"); err == nil {
		t.Fatal("query failure ignored")
	}
	var saved fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(store.payload), &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.MarginCoverOrders) != 1 || !saved.MarginCoverOrders[0].Verified || saved.MarginCoverOrders[0].OrderID != 1 || saved.MarginCoverOrders[0].Net != 0.4 || len(saved.MarginCoverOrders[0].Fills) != 1 || margin.repayCalls != 1 {
		t.Fatal("buyback fill evidence lost after repayment query failure")
	}
	cloned := cloneFundingCarryCoverOrders(s.marginCoverOrders)
	cloned[0].Fills[0].Quantity = 99
	if s.marginCoverOrders[0].Fills[0].Quantity != 0.4 {
		t.Fatal("snapshot aliased live fill evidence")
	}
}

func TestFundingCarryCoverJournalFaultsDoNotRepay(t *testing.T) {
	for _, mode := range []string{"ack_save", "ack_owner", "ack_cancel", "fill_save", "fill_owner"} {
		t.Run(mode, func(t *testing.T) {
			s, margin, store := newFundingCarryRepayIntentFixture()
			venue := &fundingCarryCoverJournalExchange{fundingCarryRepayIntentExchange: margin}
			s.marginEx = venue
			gate := &execution.OpeningGate{}
			s.SetOpeningGate(gate)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			fault := func() {
				if mode == "ack_save" || mode == "fill_save" {
					store.err = errors.New("injected cover save failure")
				}
				if mode == "ack_owner" || mode == "fill_owner" {
					gate.Block(strategyWalletRuntimeOwnershipBlock)
				}
				if mode == "ack_cancel" {
					cancel()
				}
			}
			if mode == "ack_save" || mode == "ack_owner" || mode == "ack_cancel" {
				venue.afterPlace = fault
			} else {
				venue.afterFills = fault
			}
			if err := s.closeReverse(ctx, mode); err == nil {
				t.Fatal("checkpoint fault ignored")
			}
			if margin.repayCalls != 0 || len(s.marginCoverOrders) != 1 || s.marginCoverOrders[0].Verified || !s.unownedExposure {
				t.Fatal("failed checkpoint lost ACK or proceeded to repayment")
			}
			if (mode == "ack_save" || mode == "ack_owner" || mode == "ack_cancel") && venue.orderQueries != 0 {
				t.Fatal("failed ACK checkpoint queried fills")
			}
		})
	}
}

func TestFundingCarryCoverAcknowledgementConfirmedCanceledKeepsCommittedMemory(t *testing.T) {
	s, _, base := newFundingCarryRepayIntentFixture()
	s.strategySpotKnown, s.intentInFlight = true, true
	s.marginBorrowedAt = time.UnixMilli(1000).UTC()
	s.marginDebtEvents = []fundingCarryMarginDebtEvent{{Action: "borrow", TransferID: 42, Asset: "BTC", Amount: 0.4, Principal: 0.4, AccountScope: "scope-a", OccurredAt: s.marginBorrowedAt}}
	req := &exchange.OrderRequest{Symbol: "BTCUSDT", Side: exchange.SideBuy, Type: exchange.OrderTypeLimit, Quantity: 0.401, Price: 50000}
	if err := s.prepareMarginCoverIntent(context.Background(), req, 0.4); err != nil {
		t.Fatal("persist prepared cover intent:", err)
	}
	s.SetRuntimeStateStore(&fundingCarryConfirmedCanceledDebtStore{memoryRuntimeStateStore: base})
	order := &exchange.Order{OrderID: 7, ClientOrderID: req.ClientOrderID, Symbol: req.Symbol, Side: req.Side, Type: req.Type, Price: req.Price, Quantity: req.Quantity}

	err := s.checkpointMarginCoverOrder(context.Background(), order, req.Quantity, 0.4)
	if !errors.Is(err, storage.ErrFundingCarryRuntimeStateCommitConfirmedCanceled) {
		t.Fatalf("cover ACK error = %v, want confirmed canceled commit", err)
	}
	if len(s.marginCoverOrders) != 1 || s.marginCoverOrders[0].OrderID != 7 || s.marginCoverIntent != nil || s.runtimeStateErr != nil {
		t.Fatalf("memory did not preserve durable ACK: orders=%+v intent=%+v state_err=%v", s.marginCoverOrders, s.marginCoverIntent, s.runtimeStateErr)
	}
	var committed fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(base.payload), &committed); err != nil {
		t.Fatal("decode durable cover ACK:", err)
	}
	if len(committed.MarginCoverOrders) != 1 || committed.MarginCoverIntent != nil {
		t.Fatalf("durable cover ACK = %+v", committed)
	}
}

func TestFundingCarryCoverFillConfirmedCanceledKeepsCommittedMemory(t *testing.T) {
	s, _, base := newFundingCarryRepayIntentFixture()
	s.strategySpotKnown, s.intentInFlight = true, true
	s.marginBorrowedAt = time.UnixMilli(1000).UTC()
	s.marginDebtEvents = []fundingCarryMarginDebtEvent{{Action: "borrow", TransferID: 42, Asset: "BTC", Amount: 0.4, Principal: 0.4, AccountScope: "scope-a", OccurredAt: s.marginBorrowedAt}}
	s.marginCoverOrders = []fundingCarryCoverOrder{{OrderID: 7, Asset: "BTC", AccountScope: "scope-a", Requested: 0.401, DebtToCover: 0.4}}
	if err := s.persistRuntimeStateLocked(); err != nil {
		t.Fatal("persist cover ACK fixture:", err)
	}
	s.SetRuntimeStateStore(&fundingCarryConfirmedCanceledDebtStore{memoryRuntimeStateStore: base})
	fills := []*exchange.OrderFill{{OrderID: 7, TradeID: "trade-7", Symbol: "BTCUSDT", Side: exchange.SideBuy, Price: 50000, Quantity: 0.4}}

	err := s.checkpointMarginCoverFills(context.Background(), 7, 0.4, 0.4, fills, exchange.OrderStatusFilled)
	if !errors.Is(err, storage.ErrFundingCarryRuntimeStateCommitConfirmedCanceled) {
		t.Fatalf("cover fill error = %v, want confirmed canceled commit", err)
	}
	if len(s.marginCoverOrders) != 1 || !s.marginCoverOrders[0].Verified || len(s.marginCoverOrders[0].Fills) != 1 || s.runtimeStateErr != nil {
		t.Fatalf("memory did not preserve durable fills: orders=%+v state_err=%v", s.marginCoverOrders, s.runtimeStateErr)
	}
	var committed fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(base.payload), &committed); err != nil {
		t.Fatal("decode durable cover fills:", err)
	}
	if len(committed.MarginCoverOrders) != 1 || !committed.MarginCoverOrders[0].Verified || committed.MarginCoverOrders[0].TerminalStatus != exchange.OrderStatusFilled {
		t.Fatalf("durable cover fills = %+v", committed.MarginCoverOrders)
	}
}

func TestFundingCarryCoverJournalDecoderRejectsTampering(t *testing.T) {
	s, _, store := newFundingCarryRepayIntentFixture()
	_ = s.closeReverse(context.Background(), "journal")
	var state fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(store.payload), &state); err != nil {
		t.Fatal(err)
	}
	if err := validateFundingCarryCoverOrders(state, true); err != nil {
		t.Fatal(err)
	}
	state.MarginCoverOrders[0].Net = 0.5
	if err := validateFundingCarryCoverOrders(state, true); err == nil {
		t.Fatal("forged net inventory accepted")
	}
	state.MarginCoverOrders[0].Net = 0.4
	state.MarginCoverOrders = append(state.MarginCoverOrders, state.MarginCoverOrders[0])
	if err := validateFundingCarryCoverOrders(state, true); err == nil {
		t.Fatal("repeated order identity accepted")
	}
}

func TestFundingCarryCoverJournalCannotAdoptLegacyUnscopedEvidence(t *testing.T) {
	s, _, store := newFundingCarryRepayIntentFixture()
	_ = s.closeReverse(context.Background(), "journal")
	var state fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(store.payload), &state); err != nil {
		t.Fatal(err)
	}
	state.Direction, state.MarginDebt, state.MarginBorrowTransferID = DirectionNone, 0, 0
	state.OwnershipReady, state.IntentInFlight, state.ExposureUnknown = true, false, false
	state.MarginDebtEvents, state.MarginRepayIntent = nil, nil
	state.MarginAccountScope, state.MarginCoverOrders[0].AccountScope = "", ""
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store.payload = string(payload)
	s.SetRuntimeStateStore(&borrowReceiptContextStore{store})
	if err := s.restoreRuntimeState(); err == nil {
		t.Fatal("unscoped historical cover adopted by current account")
	}
}
