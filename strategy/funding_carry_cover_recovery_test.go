package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/utils"
)

type fundingCarryCoverQueryVenue struct {
	*fundingCarryRepayIntentExchange
	order      *exchange.Order
	queryErr   error
	queries    int
	cid        string
	afterQuery func()
}

func (v *fundingCarryCoverQueryVenue) GetOrderByClientOrderID(ctx context.Context, symbol, cid string) (*exchange.Order, error) {
	v.queries++
	v.cid = cid
	if _, ok := ctx.Deadline(); !ok {
		return nil, errors.New("startup query has no deadline")
	}
	if v.afterQuery != nil {
		v.afterQuery()
	}
	return v.order, v.queryErr
}

func TestFundingCarryStartupResolvesCoverCIDWithoutResubmission(t *testing.T) {
	for _, mode := range []string{"filled", "new", "partial", "canceled", "broker_prefix", "absent", "query_error", "wrong_cid", "wrong_symbol", "wrong_side", "wrong_price", "wrong_quantity", "invalid_execution", "missing_time", "epoch_time", "unknown_status", "owner_lost", "cancelled", "save_failure", "wrong_scope", "invalid_ledger", "changed_checkpoint"} {
		t.Run(mode, func(t *testing.T) {
			s, margin, store := newFundingCarryRepayIntentFixture()
			s.strategySpotKnown, s.intentInFlight, s.unownedExposure = true, true, true
			s.marginBorrowedAt = time.UnixMilli(1000).UTC()
			s.marginDebtEvents = []fundingCarryMarginDebtEvent{{Action: "borrow", TransferID: 42, Asset: "BTC", Amount: 0.4, Principal: 0.4, AccountScope: "scope-a", OccurredAt: s.marginBorrowedAt}}
			if mode == "invalid_ledger" {
				s.marginDebtEvents = nil
			}
			req := &exchange.OrderRequest{Symbol: "BTCUSDT", Side: exchange.SideBuy, Type: exchange.OrderTypeLimit, Quantity: 0.4, Price: 50000}
			if err := s.prepareMarginCoverIntent(context.Background(), req, 0.4); err != nil {
				t.Fatal(err)
			}
			before := store.payload
			order := &exchange.Order{OrderID: 7, ClientOrderID: req.ClientOrderID, Symbol: req.Symbol, Side: req.Side, Type: req.Type, Price: req.Price, Quantity: req.Quantity, ExecutedQty: req.Quantity, Status: exchange.OrderStatusFilled, CreatedAt: time.Now().UTC()}
			venue := &fundingCarryCoverQueryVenue{fundingCarryRepayIntentExchange: margin, order: order}
			switch mode {
			case "new":
				order.Status, order.ExecutedQty = exchange.OrderStatusNew, 0
			case "partial":
				order.Status, order.ExecutedQty = exchange.OrderStatusPartiallyFilled, 0.2
			case "canceled":
				order.Status, order.ExecutedQty = exchange.OrderStatusCanceled, 0.1
			case "broker_prefix":
				margin.name = "binance"
				order.ClientOrderID = utils.AddBrokerPrefix("binance", req.ClientOrderID)
			case "absent":
				venue.order = nil
			case "query_error":
				venue.queryErr = errors.New("injected query failure")
			case "wrong_cid":
				order.ClientOrderID = "foreign"
			case "wrong_symbol":
				order.Symbol = "ETHUSDT"
			case "wrong_side":
				order.Side = exchange.SideSell
			case "wrong_price":
				order.Price = 40000
			case "wrong_quantity":
				order.Quantity = 0.3
			case "invalid_execution":
				order.ExecutedQty = math.NaN()
			case "missing_time":
				order.CreatedAt = time.Time{}
			case "epoch_time":
				order.CreatedAt = time.UnixMilli(0)
			case "unknown_status":
				order.Status = "UNKNOWN"
			}
			restarted, _, _ := newFundingCarryRepayIntentFixture()
			restarted.marginDebt, restarted.marginBorrowTransferID = 0, 0
			restarted.marginEx = venue
			restarted.SetRuntimeStateStore(&borrowReceiptContextStore{store})
			gate := &execution.OpeningGate{}
			restarted.SetOpeningGate(gate)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "owner_lost" {
				venue.afterQuery = func() { gate.Block(strategyWalletRuntimeOwnershipBlock) }
			}
			if mode == "cancelled" {
				venue.afterQuery = cancel
			}
			if mode == "save_failure" {
				venue.afterQuery = func() { store.err = errors.New("injected ACK save failure") }
			}
			if mode == "changed_checkpoint" {
				venue.afterQuery = func() {
					before = strings.Replace(store.payload, req.ClientOrderID, req.ClientOrderID+"-new", 1)
					if before == store.payload {
						t.Fatal("fixture did not change pending CID")
					}
					store.payload = before
				}
			}
			if mode == "wrong_scope" {
				restarted.marginAccountScope = "scope-b"
			}
			if err := restarted.Start(ctx); err == nil {
				t.Fatal("CID recovery falsely resumed trading")
			}
			if len(margin.placedOrders) != 0 || margin.repayCalls != 0 || restarted.started {
				t.Fatal("recovery submitted order or repayment")
			}
			var saved fundingCarryRuntimeState
			if err := json.Unmarshal([]byte(store.payload), &saved); err != nil {
				t.Fatal(err)
			}
			if mode == "filled" || mode == "new" || mode == "partial" || mode == "canceled" || mode == "broker_prefix" {
				if saved.MarginCoverIntent != nil || len(saved.MarginCoverOrders) != 1 || saved.MarginCoverOrders[0].OrderID != 7 || saved.MarginCoverOrders[0].ClientOrderID != req.ClientOrderID || saved.MarginCoverOrders[0].RequestPrice != req.Price || saved.MarginCoverOrders[0].PreparedAt.IsZero() || saved.MarginCoverOrders[0].Verified || !saved.IntentInFlight || !saved.ExposureUnknown || venue.cid != req.ClientOrderID {
					t.Fatal("exact ACK not saved conservatively")
				}
				if err := restarted.Start(ctx); err == nil || venue.queries != 1 {
					t.Fatal("repeated startup falsely resumed or repeated resolved CID query")
				}
			} else if store.payload != before {
				t.Fatal("failed recovery changed durable request")
			}
			if (mode == "wrong_scope" || mode == "invalid_ledger") && venue.queries != 0 {
				t.Fatal("unowned or invalid request queried venue")
			}
		})
	}
}
