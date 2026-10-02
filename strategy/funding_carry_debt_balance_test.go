package strategy

import (
	"encoding/json"
	"testing"
	"time"

	"quantmesh/config"
)

func fundingCarryBalancedDebtFixture() fundingCarryRuntimeState {
	when := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	return fundingCarryRuntimeState{Strategy: "funding_carry", Symbol: "BTCUSDT", OwnershipReady: true, Direction: DirectionReverse,
		MarginDebt: 0.3, MarginBorrowTransferID: 81, MarginBorrowedAt: when,
		MarginDebtEvents: []fundingCarryMarginDebtEvent{
			{Action: "borrow", TransferID: 81, Asset: "BTC", Amount: 0.5, Principal: 0.5, OccurredAt: when},
			{Action: "repay", TransferID: 82, Asset: "BTC", Amount: 0.21, Principal: 0.2, InterestPaid: 0.01, OccurredAt: when.Add(time.Second)},
		}}
}

func TestFundingCarryRestoreRejectsUnbalancedPrincipal(t *testing.T) {
	for _, name := range []string{"understated", "overstated", "missing_history", "repay_before_borrow", "cross_asset", "foreign_asset", "missing_active_identity", "wrong_active_time", "overflow"} {
		t.Run(name, func(t *testing.T) {
			state := fundingCarryBalancedDebtFixture()
			switch name {
			case "understated":
				state.MarginDebt = 0.1
			case "overstated":
				state.MarginDebt = 0.5
			case "missing_history":
				state.MarginDebtEvents = nil
			case "repay_before_borrow":
				state.MarginDebtEvents[0], state.MarginDebtEvents[1] = state.MarginDebtEvents[1], state.MarginDebtEvents[0]
			case "cross_asset":
				state.MarginDebtEvents[1].Asset = "ETH"
			case "foreign_asset":
				for i := range state.MarginDebtEvents {
					state.MarginDebtEvents[i].Asset = "ETH"
				}
			case "overflow":
				for i := range state.MarginDebtEvents {
					state.MarginDebtEvents[i].Action = "borrow"
					state.MarginDebtEvents[i].Amount = 1e308
					state.MarginDebtEvents[i].Principal = 1e308
					state.MarginDebtEvents[i].InterestPaid = 0
				}
			case "missing_active_identity":
				state.MarginBorrowTransferID = 0
			case "wrong_active_time":
				state.MarginBorrowedAt = state.MarginBorrowedAt.Add(time.Second)
			}
			payload, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodeFundingCarryRuntimeState(fundingCarryRuntimeStateVersion, string(payload), "", "", "BTCUSDT"); err == nil && name != "foreign_asset" {
				t.Fatal("inconsistent principal accepted as resolved")
			}
			venue := &mockFCExchange{baseAsset: "BTC"}
			s := NewFundingCarryStrategy("funding_carry", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, venue, venue, venue, nil)
			store := &memoryRuntimeStateStore{version: fundingCarryRuntimeStateVersion, payload: string(payload), found: true}
			s.SetRuntimeStateStore(store)
			s.marginDebt, s.unownedExposure, s.intentInFlight = 0.9, true, true
			if err := s.restoreRuntimeState(); err == nil {
				t.Fatal("restore accepted inconsistent ledger")
			}
			if s.marginDebt != 0.9 || !s.unownedExposure || !s.intentInFlight || s.strategySpotKnown || store.payload != string(payload) {
				t.Fatal("rejected restore changed memory or durable state")
			}
		})
	}
}

func TestFundingCarryRestoreAcceptsBalancedPrincipal(t *testing.T) {
	for _, name := range []string{"partial_repayment", "completed_previous_cycle", "fully_repaid"} {
		t.Run(name, func(t *testing.T) {
			state := fundingCarryBalancedDebtFixture()
			if name == "completed_previous_cycle" {
				previous := state.MarginDebtEvents[0]
				previous.TransferID, previous.Amount, previous.Principal = 90, 0.7, 0.7
				previous.OccurredAt = previous.OccurredAt.Add(-2 * time.Second)
				repaid := previous
				repaid.TransferID, repaid.Action, repaid.OccurredAt = 91, "repay", previous.OccurredAt.Add(time.Second)
				state.MarginDebtEvents = append([]fundingCarryMarginDebtEvent{previous, repaid}, state.MarginDebtEvents...)
				wrong := state
				wrong.MarginBorrowTransferID, wrong.MarginBorrowedAt = previous.TransferID, previous.OccurredAt
				payload, _ := json.Marshal(wrong)
				if _, err := decodeFundingCarryRuntimeState(fundingCarryRuntimeStateVersion, string(payload), "", "", "BTCUSDT"); err == nil {
					t.Fatal("closed previous borrow claimed current debt")
				}
			}
			if name == "fully_repaid" {
				repaid := state.MarginDebtEvents[1]
				repaid.TransferID, repaid.Amount, repaid.Principal = 83, 0.31, 0.3
				repaid.OccurredAt = repaid.OccurredAt.Add(time.Second)
				state.MarginDebtEvents = append(state.MarginDebtEvents, repaid)
				state.Direction, state.MarginDebt, state.MarginBorrowTransferID, state.MarginBorrowedAt = DirectionNone, 0, 0, time.Time{}
			}
			payload, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			venue := &mockFCExchange{baseAsset: "BTC"}
			s := NewFundingCarryStrategy("funding_carry", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, venue, venue, venue, nil)
			store := &memoryRuntimeStateStore{version: fundingCarryRuntimeStateVersion, payload: string(payload), found: true}
			s.SetRuntimeStateStore(store)
			s.unownedExposure, s.intentInFlight = true, true
			if err := s.restoreRuntimeState(); err != nil {
				t.Fatal(err)
			}
			if s.marginDebt != state.MarginDebt || len(s.marginDebtEvents) != len(state.MarginDebtEvents) || !s.strategySpotKnown || s.unownedExposure || s.intentInFlight || store.payload != string(payload) {
				t.Fatal("valid restore changed financial evidence")
			}
		})
	}
}
