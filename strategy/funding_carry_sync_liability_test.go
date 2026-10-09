package strategy

import (
	"context"
	"errors"
	"math"
	"testing"

	"quantmesh/config"
	"quantmesh/exchange"
)

type fundingCarrySyncLiabilityVenue struct {
	*mockFCExchange
	principal, interest float64
	liabilityErr        error
	reads               int
	cancel              context.CancelFunc
}

func (v *fundingCarrySyncLiabilityVenue) GetMarginLiability(ctx context.Context, asset string) (float64, float64, error) {
	v.reads++
	if asset != "BTC" {
		return 0, 0, errors.New("wrong liability asset")
	}
	if v.cancel != nil {
		v.cancel()
	}
	return v.principal, v.interest, v.liabilityErr
}

func TestFundingCarrySyncReadsIndependentMarginLiability(t *testing.T) {
	for _, mode := range []string{"valid", "unowned_principal", "negative", "nan", "overflow", "query_error", "cancelled", "already_unknown"} {
		t.Run(mode, func(t *testing.T) {
			futures := &mockFCExchange{quantityDecimals: 3, positions: []*exchange.Position{{Symbol: "BTCUSDT", Size: 0.4}}}
			spot := &mockFCExchange{baseAsset: "BTC", quantityDecimals: 3, balance: 2}
			venue := &fundingCarrySyncLiabilityVenue{mockFCExchange: &mockFCExchange{positionsErr: errors.New("bought-back base inventory prevents short-position attribution")}, principal: 0.4, interest: 0.0005}
			s := NewFundingCarryStrategy("fc", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, futures, spot, venue, nil)
			s.direction, s.futQty, s.marginDebt, s.strategySpotKnown = DirectionReverse, 0.4, 0.4, true
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch mode {
			case "unowned_principal":
				venue.principal = 0.41
			case "negative":
				venue.interest = -1
			case "nan":
				venue.principal = math.NaN()
			case "overflow":
				venue.principal, venue.interest = math.MaxFloat64, math.MaxFloat64
			case "query_error":
				venue.liabilityErr = errors.New("liability unavailable")
			case "cancelled":
				venue.cancel = cancel
			case "already_unknown":
				s.unownedExposure, s.intentInFlight = true, true
			}
			err := s.syncPositions(ctx)
			if venue.reads != 1 || (err == nil) != (mode == "valid") {
				t.Fatalf("reads=%d err=%v mode=%s", venue.reads, err, mode)
			}
			if s.marginDebt != 0.4 || s.strategySpotQty != 0 || s.spotQty != 0 {
				t.Fatal("liability read adopted balances or rewrote owned debt")
			}
			if mode != "valid" && !s.unownedExposure {
				t.Fatal("failed debt proof did not retain unknown exposure")
			}
		})
	}
}
