package order

import (
	"context"
	"fmt"
	"strings"

	"quantmesh/config"
	"quantmesh/execution"
)

// SetOpeningAdmissionGuard installs a fail-closed check evaluated before an
// opening order acquires its gate lease. Protective closes do not invoke it.
func (oe *ExchangeOrderExecutor) SetOpeningAdmissionGuard(guard func(context.Context) error) {
	oe.openingAdmissionGuard = guard
}

// SetOpeningGate wires the bot's shared gate before any strategy starts.
// PositionSide on a request takes precedence over the configured grid direction.
func (oe *ExchangeOrderExecutor) SetOpeningGate(gate *execution.OpeningGate, direction string) {
	if gate == nil {
		gate = &execution.OpeningGate{}
	}
	oe.openingGate = gate
	oe.positionDirection = config.NormalizeDirection(direction)
}

func (oe *ExchangeOrderExecutor) IsOpeningPaused() bool {
	return oe.openingGate != nil && oe.openingGate.Blocked()
}

func (oe *ExchangeOrderExecutor) admitOrder(req *OrderRequest) (func(), error) {
	if req == nil {
		return nil, fmt.Errorf("order request is nil")
	}
	side := strings.ToUpper(strings.TrimSpace(req.Side))
	if side != "BUY" && side != "SELL" {
		return nil, fmt.Errorf("invalid order side %q", req.Side)
	}
	if oe.openingGate == nil || !oe.isOpeningOrder(req) {
		return func() {}, nil
	}
	return oe.openingGate.Begin()
}

func (oe *ExchangeOrderExecutor) isOpeningOrder(req *OrderRequest) bool {
	side := strings.ToUpper(strings.TrimSpace(req.Side))
	// Futures without ReduceOnly must remain gated even when their direction
	// resembles a close: an oversized non-reduce order can reverse the position.
	// Spot closes cannot use ReduceOnly; their explicit inventory leg is needed.
	marketType := strings.ToLower(strings.TrimSpace(oe.exchange.GetMarketType()))
	if marketType == "spot" || marketType == "spot_margin" {
		// Spot venues do not enforce ReduceOnly. An incorrectly tagged BUY
		// must not bypass the gate merely because that flag is set.
		leg := strings.ToUpper(strings.TrimSpace(req.PositionSide))
		if leg == "" {
			leg = oe.positionDirection
		}
		if (leg == "LONG" && side == "SELL") || (leg == "SHORT" && side == "BUY") {
			return false
		}
	} else if req.ReduceOnly {
		return false
	}
	return true
}
