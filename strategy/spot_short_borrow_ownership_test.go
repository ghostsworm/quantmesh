package strategy

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"quantmesh/execution"
	"quantmesh/position"
)

type spotShortOwnershipOrderExecutor struct{ signalTestExecutor }

func (e *spotShortOwnershipOrderExecutor) PlaceOrderContext(ctx context.Context, req *position.OrderRequest) (*position.Order, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return e.signalTestExecutor.PlaceOrder(req)
}

type spotShortPriceOwnershipExchange struct {
	signalTestExchange
	gate *execution.OpeningGate
}

func (e *spotShortPriceOwnershipExchange) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	price, err := e.signalTestExchange.GetLatestPrice(ctx, symbol)
	e.gate.Block("runtime_ownership_unverified")
	return price, err
}

type spotShortBorrowOwnershipStore struct {
	*memoryRuntimeStateStore
	gate *execution.OpeningGate
}

func (s *spotShortBorrowOwnershipStore) SaveRuntimeState(name string, version int, payload string) error {
	if err := s.memoryRuntimeStateStore.SaveRuntimeState(name, version, payload); err != nil {
		return err
	}
	var state spotShortRuntimeState
	if err := json.Unmarshal([]byte(payload), &state); err != nil {
		return err
	}
	if len(state.PendingBorrow) > 0 {
		s.gate.Block("runtime_ownership_unverified")
	}
	return nil
}

func TestSpotShortBorrowStagesRejectKnownOwnershipLoss(t *testing.T) {
	for _, stage := range []string{"price", "persist", "borrow"} {
		t.Run(stage, func(t *testing.T) {
			gate := &execution.OpeningGate{}
			margin := &mockMarginExchange{}
			executor := &spotShortOwnershipOrderExecutor{}
			s := newSpotShortForTest(executor, &signalTestExchange{}, margin)
			store := &memoryRuntimeStateStore{}
			s.SetRuntimeStateStore(store)
			s.SetOpeningGate(gate)
			coordinator := &walletCoordinationTestLock{}
			if err := s.SetAccountWalletCoordinationLock(coordinator, "funding_carry_wallet:"+t.Name()); err != nil {
				t.Fatal(err)
			}
			switch stage {
			case "price":
				s.ex = &spotShortPriceOwnershipExchange{gate: gate}
			case "persist":
				s.SetRuntimeStateStore(&spotShortBorrowOwnershipStore{memoryRuntimeStateStore: store, gate: gate})
			case "borrow":
				margin.beforeBorrow = func() { gate.Block("runtime_ownership_unverified") }
			}
			err := s.increaseShort(context.Background(), 0.5)
			if err == nil || !strings.Contains(err.Error(), "runtime ownership is unverified") || len(executor.orders) != 0 || coordinator.active != 0 {
				t.Fatalf("lost owner entered next stage: err=%v borrowed=%v orders=%d", err, margin.borrowed, len(executor.orders))
			}
			if stage == "price" && (len(s.pendingBorrow) != 0 || len(margin.borrowed) != 0) {
				t.Fatal("price-time loss created borrow intent or debt")
			}
			if stage != "price" {
				var saved spotShortRuntimeState
				if err := json.Unmarshal([]byte(store.payload), &saved); err != nil {
					t.Fatal(err)
				}
				if len(saved.PendingBorrow) != 1 || len(s.pendingBorrow) != 1 {
					t.Fatal("lost owner discarded borrow intent")
				}
				for cid, pending := range s.pendingBorrow {
					if saved.PendingBorrow[cid] != pending {
						t.Fatal("borrow evidence was not durable")
					}
					if stage == "persist" && (pending.Phase != "unsubmitted" || len(margin.borrowed) != 0) {
						t.Fatal("persistence-time loss borrowed")
					}
					if stage == "borrow" && (pending.Phase != "borrowed" || pending.BorrowTransferID != 1 || len(margin.borrowed) != 1) {
						t.Fatal("lost owner failed to preserve confirmed debt")
					}
				}
			}
		})
	}
}
