package strategy

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
)

func recoveryProofPayload(t *testing.T, state interface{}) string {
	t.Helper()
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}

func TestFundingCarryRecoveryConfigProof(t *testing.T) {
	for _, net := range []float64{0.4, 0.4008, math.Nextafter(0.4, 1)} {
		s, _ := remainingCoverFixture(net)
		s.fut.(*mockFCExchange).name = "venue-futures"
		s.spot.(*mockFCExchange).name = "venue-spot"
		state := s.runtimeStateSnapshotLocked()
		binding := FundingCarryRecoveryBinding{state.FuturesExchange, state.SpotExchange, state.Symbol, "BTC", state.MarginAccountScope}
		payload := recoveryProofPayload(t, state)
		err := VerifyFundingCarryRecoveryConfigState(6, payload, binding)
		if net == 0.4 && err != nil {
			t.Fatalf("flat journal rejected: %v", err)
		}
		if net > 0.4 && !errors.Is(err, ErrRecoveryConfigRequired) {
			t.Fatalf("remaining assets accepted: %v", err)
		}
		if payload != recoveryProofPayload(t, state) {
			t.Fatal("proof mutated evidence")
		}
		binding.MarginAccountScope = "other-account"
		if err := VerifyFundingCarryRecoveryConfigState(6, payload, binding); !errors.Is(err, ErrRecoveryConfigUnverified) {
			t.Fatalf("wrong scope accepted: %v", err)
		}
	}
	s, _ := remainingCoverFixture(0.4)
	s.fut.(*mockFCExchange).name = "venue-futures"
	s.spot.(*mockFCExchange).name = "venue-spot"
	state := s.runtimeStateSnapshotLocked()
	binding := FundingCarryRecoveryBinding{state.FuturesExchange, state.SpotExchange, state.Symbol, "BTC", state.MarginAccountScope}
	state.IntentInFlight, state.ExposureUnknown = true, true
	if err := VerifyFundingCarryRecoveryConfigState(6, recoveryProofPayload(t, state), binding); !errors.Is(err, ErrRecoveryConfigRequired) {
		t.Fatalf("pending state accepted: %v", err)
	}
	state.IntentInFlight, state.ExposureUnknown = false, false
	payload := recoveryProofPayload(t, state)
	for _, bad := range []string{"{}", strings.Replace(payload, `"owned_spot":0`, `"owned_spot":null`, 1)} {
		if err := VerifyFundingCarryRecoveryConfigState(6, bad, binding); !errors.Is(err, ErrRecoveryConfigUnverified) {
			t.Fatalf("incomplete carry evidence accepted: %v", err)
		}
	}
	if err := VerifyFundingCarryRecoveryConfigState(5, payload, binding); !errors.Is(err, ErrRecoveryConfigUnverified) {
		t.Fatal("legacy carry schema certified flat")
	}
	binding.BaseAsset = "ETH"
	if err := VerifyFundingCarryRecoveryConfigState(6, recoveryProofPayload(t, state), binding); !errors.Is(err, ErrRecoveryConfigUnverified) {
		t.Fatalf("wrong asset accepted: %v", err)
	}
}

func TestFundingPerpSpreadRecoveryConfigProof(t *testing.T) {
	flat := fundingPerpSpreadRuntimeState{Strategy: "funding_perp_spread", LegAExchange: "venue-a", LegASymbol: "BTCUSDT", LegBExchange: "venue-b", LegBSymbol: "BTCUSDT", OwnershipReady: true}
	binding := FundingPerpSpreadRecoveryBinding{flat.LegAExchange, flat.LegASymbol, flat.LegBExchange, flat.LegBSymbol}
	if err := VerifyFundingPerpSpreadRecoveryConfigState(6, recoveryProofPayload(t, flat), binding); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"position", "tiny_position", "ledger", "emergency", "pending", "execution"} {
		t.Run(kind, func(t *testing.T) {
			state := flat
			switch kind {
			case "position":
				state.OwnedB = -0.4
			case "tiny_position":
				state.OwnedA = math.SmallestNonzeroFloat64
			case "ledger":
				state.ExecutionLedgerUnverified = true
			case "emergency":
				state.EmergencyCloseRequired = true
			case "pending":
				state.IntentInFlight, state.ExposureUnknown = true, true
				state.PendingOrder = &fundingPerpSpreadOrderIntent{ClientOrderID: "pending-1", LegExchange: flat.LegAExchange, Symbol: flat.LegASymbol, Side: "BUY", Quantity: 0.4}
			case "execution":
				state.ExecutionLedgerUnverified = true
				state.PendingExecutions = []fundingPerpSpreadPendingExecutionState{{Exchange: flat.LegAExchange, Symbol: flat.LegASymbol, ClientOrderID: "pending-1", Side: "BUY", Quantity: 0.4}}
			}
			if err := VerifyFundingPerpSpreadRecoveryConfigState(6, recoveryProofPayload(t, state), binding); !errors.Is(err, ErrRecoveryConfigRequired) {
				t.Fatalf("unresolved state accepted: %v", err)
			}
		})
	}
}

func TestRecoveryConfigProofRejectsAmbiguousEvidence(t *testing.T) {
	state := fundingPerpSpreadRuntimeState{Strategy: "funding_perp_spread", LegAExchange: "a", LegASymbol: "BTCUSDT", LegBExchange: "b", LegBSymbol: "BTCUSDT", OwnershipReady: true}
	binding := FundingPerpSpreadRecoveryBinding{"a", "BTCUSDT", "b", "BTCUSDT"}
	valid := recoveryProofPayload(t, state)
	for _, payload := range []string{
		"null", "{}", valid + "{}",
		strings.Replace(valid, `"owned_a":0`, `"owned_a":null`, 1),
		strings.Replace(valid, `"owned_a":0,`, "", 1),
		strings.Replace(valid, `"owned_a":0`, `"owned_a":1,"owned_a":0`, 1),
		strings.Replace(valid, `"owned_a":0`, `"owned_a":1,"OWNED_A":0`, 1),
		strings.Replace(valid, `"owned_a":0`, `"owned_a":0,"future_financial_field":1`, 1),
	} {
		if err := VerifyFundingPerpSpreadRecoveryConfigState(6, payload, binding); !errors.Is(err, ErrRecoveryConfigUnverified) {
			t.Fatalf("ambiguous evidence accepted: %s: %v", payload, err)
		}
	}
	for _, version := range []int{0, 1, 5, 7} {
		if err := VerifyFundingPerpSpreadRecoveryConfigState(version, valid, binding); !errors.Is(err, ErrRecoveryConfigUnverified) {
			t.Fatalf("schema %d accepted", version)
		}
	}
	if err := VerifyFundingPerpSpreadRecoveryConfigState(6, valid, FundingPerpSpreadRecoveryBinding{}); !errors.Is(err, ErrRecoveryConfigUnverified) {
		t.Fatal("missing independent identity accepted")
	}
	if err := verifyRecoveryJSON(`{"outer":{"x":1,"x":0}}`, &map[string]interface{}{}); err == nil {
		t.Fatal("nested duplicate accepted")
	}
}
