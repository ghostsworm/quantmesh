package strategy

import (
	"context"
	"math"
	"testing"

	"quantmesh/config"
	"quantmesh/exchange"
)

// Mask the optional reader to exercise the strict legacy position path too.
type fundingCarryLegacyDebtVenue struct{ exchange.ISpotMarginExchange }

func TestFundingCarrySyncDebtDoesNotUseOrderRounding(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		for _, mode := range []string{"excess_principal", "missing_principal", "tiny_interest_flat", "tiny_principal_forward", "valid_interest", "binary_roundoff"} {
			t.Run(mode+map[bool]string{false: "/reader", true: "/legacy"}[legacy], func(t *testing.T) {
				principal, interest := 0.4, 0.00001
				direction, ownedDebt, ownedFutures, ownedSpot := DirectionReverse, 0.4, 0.4, 0.0
				switch mode {
				case "excess_principal":
					principal += 0.00001
				case "missing_principal":
					principal -= 0.00001
				case "tiny_interest_flat":
					principal, direction, ownedDebt, ownedFutures = 0, DirectionNone, 0, 0
				case "tiny_principal_forward":
					principal, interest, direction, ownedDebt, ownedSpot = 0.00001, 0, DirectionForward, 0, 0.4
				case "binary_roundoff":
					principal = math.Nextafter(principal, math.Inf(1))
				}
				futures := &mockFCExchange{quantityDecimals: 3, positions: []*exchange.Position{}}
				if ownedFutures > 0 {
					size := ownedFutures
					if direction == DirectionForward {
						size = -size
					}
					futures.positions = []*exchange.Position{{Symbol: "BTCUSDT", Size: size}}
				}
				spot := &mockFCExchange{baseAsset: "BTC", quantityDecimals: 3, balance: ownedSpot}
				margin := &mockFCExchange{positions: []*exchange.Position{{Symbol: "BTCUSDT", Size: -(principal + interest), MarginBorrowed: principal, MarginInterest: interest, MarginDebtKnown: true}}}
				var venue exchange.ISpotMarginExchange = &fundingCarrySyncLiabilityVenue{mockFCExchange: margin, principal: principal, interest: interest}
				if legacy {
					venue = &fundingCarryLegacyDebtVenue{margin}
				}
				s := NewFundingCarryStrategy("fc", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, futures, spot, venue, nil)
				s.direction, s.marginDebt, s.futQty, s.strategySpotQty, s.strategySpotKnown = direction, ownedDebt, ownedFutures, ownedSpot, true
				err := s.syncPositions(t.Context())
				valid := mode == "valid_interest" || mode == "binary_roundoff"
				if (err == nil) != valid {
					t.Fatalf("sub-order-precision debt proof: mode=%s legacy=%v err=%v", mode, legacy, err)
				}
				if !valid && !s.unownedExposure {
					t.Fatal("unmatched liability did not retain unknown exposure")
				}
				if s.marginDebt != ownedDebt || margin.repayCalls != 0 || len(margin.placedOrders) != 0 {
					t.Fatal("debt proof adopted principal or made financial requests")
				}
			})
		}
	}
}

func TestFundingCarryStartupRejectsTinyInterestWithoutOwnedDebt(t *testing.T) {
	futures := &mockFCExchange{quantityDecimals: 3, positions: []*exchange.Position{}}
	spot := &mockFCExchange{baseAsset: "BTC", quantityDecimals: 3}
	margin := &fundingCarrySyncLiabilityVenue{mockFCExchange: &mockFCExchange{}, interest: 0.00001}
	s := NewFundingCarryStrategy("fc", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, futures, spot, margin, nil)
	store := &memoryRuntimeStateStore{}
	s.SetRuntimeStateStore(&borrowReceiptContextStore{store})
	s.strategySpotKnown = true
	if err := s.persistRuntimeStateLocked(); err != nil {
		t.Fatal(err)
	}
	err := s.Start(t.Context())
	if err == nil {
		_ = s.QuiesceContext(context.Background())
		t.Fatal("startup admitted an interest-only liability below order precision")
	}
	if s.started || !s.unownedExposure || margin.repayCalls != 0 || len(margin.placedOrders) != 0 {
		t.Fatal("unowned interest started trading or lost its block")
	}
}
