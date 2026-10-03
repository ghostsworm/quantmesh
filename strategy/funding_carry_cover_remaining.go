package strategy

import (
	"fmt"
	"math/big"
	"strings"
)

// Historical net fills less confirmed repayments, not live spendable inventory.
// Never discard a positive remainder using exchange order-size tolerances.
func fundingCarryCoverRemaining(state fundingCarryRuntimeState, asset string) (*big.Rat, error) {
	if state.MarginCoverIntent != nil {
		return nil, fmt.Errorf("margin cover remaining assets have an unresolved submission")
	}
	if err := validateFundingCarryDebtAsset(state, asset); err != nil {
		return nil, err
	}
	seen := make(map[fundingCarryDebtEventIdentity]bool)
	for _, event := range state.MarginDebtEvents {
		identity := fundingCarryDebtEventKey(event)
		if event.TransferID <= 0 || (event.Action != "borrow" && event.Action != "repay") || seen[identity] {
			return nil, fmt.Errorf("margin cover remaining assets have invalid financial identity")
		}
		if err := validateFundingCarryDebtEventIntegrity(event, state.MarginAccountScope); err != nil {
			return nil, err
		}
		seen[identity] = true
	}
	if err := validateFundingCarryCoverOrders(state, false); err != nil {
		return nil, fmt.Errorf("margin cover remaining assets have invalid evidence: %w", err)
	}
	remaining := new(big.Rat)
	for _, record := range state.MarginCoverOrders {
		if strings.TrimSpace(state.MarginAccountScope) == "" || !strings.EqualFold(record.Asset, asset) || !validRuntimeAmount(record.Net) || record.Consumed > record.Net {
			return nil, fmt.Errorf("margin cover remaining assets have invalid scope, asset or consumption")
		}
		net := fundingCarryDecimalPrincipal(record.Net)
		consumed := fundingCarryDecimalPrincipal(record.Consumed)
		remaining.Add(remaining, new(big.Rat).Sub(net, consumed))
	}
	return remaining, nil
}

func requireNoFundingCarryCoverRemaining(state fundingCarryRuntimeState, asset string) error {
	remaining, err := fundingCarryCoverRemaining(state, asset)
	if err != nil {
		return err
	}
	if remaining.Sign() != 0 {
		return fmt.Errorf("margin cover remaining assets require reconciliation: %s %s", fundingCarryCoverRemainingString(remaining), asset)
	}
	return nil
}

func fundingCarryCoverRemainingString(amount *big.Rat) string {
	// Shortest decimal float64 quantities have at most 324 fractional digits.
	return strings.TrimRight(strings.TrimRight(amount.FloatString(324), "0"), ".")
}

// Caller holds s.mu. Use the journal directly without copying unrelated state.
func (s *FundingCarryStrategy) coverRemainingStateLocked() fundingCarryRuntimeState {
	return fundingCarryRuntimeState{Symbol: s.symbol, MarginAccountScope: s.marginAccountScope,
		MarginDebtEvents: s.marginDebtEvents, MarginCoverOrders: s.marginCoverOrders,
		MarginCoverIntent: s.marginCoverIntent, MarginRepayIntent: s.marginRepayIntent}
}

func (s *FundingCarryStrategy) coverRemainingStatusLocked() (interface{}, bool) {
	if !s.strategySpotKnown {
		return nil, false
	}
	remaining, err := fundingCarryCoverRemaining(s.coverRemainingStateLocked(), s.spot.GetBaseAsset())
	if err != nil {
		return nil, false
	}
	return fundingCarryCoverRemainingString(remaining), true
}
