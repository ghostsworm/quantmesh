package replay

import (
	"context"
	"fmt"
	"sort"

	"quantmesh/position"
)

// This adapter is used only by the engine's after-tick protective queue. It
// delivers already-produced fills (including fees) before REST-style queries;
// no wall-clock goroutine races a later market tick or reads future prices.
type replayLiquidationVenue struct{ engine *Engine }

func (v *replayLiquidationVenue) GetOrderState(ctx context.Context, symbol string, id int64) (position.LiquidationOrderState, error) {
	if err := ctx.Err(); err != nil {
		return position.LiquidationOrderState{}, err
	}
	v.engine.deliver()
	ex := v.engine.ex
	ex.mu.Lock()
	defer ex.mu.Unlock()
	u, ok := ex.orderStates[id]
	if !ok || symbol != ex.symbol {
		return position.LiquidationOrderState{}, fmt.Errorf("replay order %d not found", id)
	}
	return position.LiquidationOrderState{Status: u.Status, ExecutedQty: u.ExecutedQty, AvgPrice: u.AvgPrice, Price: u.Price}, nil
}

func (v *replayLiquidationVenue) CancelOrder(ctx context.Context, symbol string, id int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if symbol != v.engine.ex.symbol {
		return fmt.Errorf("replay symbol mismatch")
	}
	v.engine.ex.cancel([]int64{id})
	v.engine.deliver()
	return nil
}

func (v *replayLiquidationVenue) GetPositionSizes(ctx context.Context, symbol string) ([]float64, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if symbol != v.engine.ex.symbol {
		return nil, fmt.Errorf("replay symbol mismatch")
	}
	return []float64{v.engine.ex.snapshot().netQty}, nil
}

func (v *replayLiquidationVenue) PlaceMarketOrder(context.Context, string, string, float64, bool) (int64, error) {
	return 0, fmt.Errorf("replay liquidation must submit through its owned executor")
}

func (v *replayLiquidationVenue) GetOpenOrderIDs(ctx context.Context, symbol string) ([]int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ex := v.engine.ex
	ex.mu.Lock()
	defer ex.mu.Unlock()
	if symbol != ex.symbol {
		return nil, fmt.Errorf("replay symbol mismatch")
	}
	ids := make([]int64, 0, len(ex.orders))
	for id := range ex.orders {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, nil
}

func (v *replayLiquidationVenue) CancelAllOrders(context.Context, string) error {
	return fmt.Errorf("replay liquidation cannot sweep unowned orders")
}
