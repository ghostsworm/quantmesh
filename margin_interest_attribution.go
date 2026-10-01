package main

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"quantmesh/exchange"
	"quantmesh/storage"
)

type fundingCarryMarginDebtEventSnapshot struct {
	Action       string    `json:"action"`
	TransferID   int64     `json:"transfer_id"`
	Asset        string    `json:"asset"`
	Amount       float64   `json:"amount"`
	Principal    float64   `json:"principal"`
	InterestPaid float64   `json:"interest_paid"`
	OccurredAt   time.Time `json:"occurred_at"`
	AccountScope string    `json:"account_scope"`
}

type fundingCarryMarginDebtStateSnapshot struct {
	Strategy           string                                `json:"strategy"`
	FuturesExchange    string                                `json:"futures_exchange"`
	SpotExchange       string                                `json:"spot_exchange"`
	MarginAccountScope string                                `json:"margin_account_scope"`
	OwnershipReady     bool                                  `json:"ownership_ready"`
	IntentInFlight     bool                                  `json:"intent_in_flight"`
	ExposureUnknown    bool                                  `json:"exposure_unknown"`
	Direction          int                                   `json:"direction"`
	MarginDebt         float64                               `json:"margin_debt"`
	MarginDebtEvents   []fundingCarryMarginDebtEventSnapshot `json:"margin_debt_events"`
}

type fundingCarryInterestShare struct {
	BotPrincipal float64
	Interest     float64
}

const marginInterestStorageScale = 1e12

// allocateCrossMarginInterest assigns a fee only when every persisted Funding
// Carry debt event for the account can reconstruct the exchange-reported debt.
// Converted and portfolio charges are rejected because their principal units
// cannot be safely compared with spot-margin borrow ledgers.
func allocateCrossMarginInterest(states []*storage.StrategyRuntimeState, accountScope string, payment exchange.MarginInterestRecord) (map[string]fundingCarryInterestShare, error) {
	accountScope = strings.TrimSpace(accountScope)
	rawAsset := strings.ToUpper(strings.TrimSpace(payment.RawAsset))
	asset := strings.ToUpper(strings.TrimSpace(payment.Asset))
	typeName := strings.ToUpper(strings.TrimSpace(payment.Type))
	if accountScope == "" || rawAsset == "" || asset == "" || rawAsset != asset || payment.AccruedAt <= 0 ||
		!finiteNonNegativeNumber(payment.Principal) || !finiteNonNegativeNumber(payment.Interest) ||
		strings.Contains(typeName, "CONVERTED") || typeName == "PORTFOLIO" || typeName != "PERIODIC" {
		return nil, fmt.Errorf("margin interest row is not in a directly reconcilable asset scope")
	}
	accruedAt := time.UnixMilli(payment.AccruedAt).UTC()
	debtsByBot := make(map[string]float64)
	seenBots := make(map[string]struct{}, len(states))
	for _, persisted := range states {
		if persisted == nil || strings.TrimSpace(persisted.BotID) == "" || persisted.StrategyName != "funding_carry" {
			return nil, fmt.Errorf("funding_carry debt state list contains an incomplete identity")
		}
		botID := strings.TrimSpace(persisted.BotID)
		if _, duplicate := seenBots[botID]; duplicate {
			return nil, fmt.Errorf("duplicate funding_carry debt state for Bot %s", botID)
		}
		seenBots[botID] = struct{}{}
		if persisted.SchemaVersion != 1 {
			return nil, fmt.Errorf("funding_carry Bot %s uses unsupported debt state schema %d", botID, persisted.SchemaVersion)
		}
		var state fundingCarryMarginDebtStateSnapshot
		if err := json.Unmarshal([]byte(persisted.Payload), &state); err != nil {
			return nil, fmt.Errorf("decode funding_carry debt state for Bot %s: %w", botID, err)
		}
		if state.Strategy != "funding_carry" || !strings.EqualFold(state.FuturesExchange, "binance") || !strings.EqualFold(state.SpotExchange, "binance") {
			return nil, fmt.Errorf("funding_carry debt state identity is invalid for Bot %s", botID)
		}
		if state.MarginAccountScope == "" {
			if state.Direction == 2 || len(state.MarginDebtEvents) > 0 {
				return nil, fmt.Errorf("funding_carry Bot %s has debt history without an account scope", botID)
			}
			continue
		}
		if state.MarginAccountScope != accountScope {
			continue
		}
		if (state.IntentInFlight || state.ExposureUnknown || !state.OwnershipReady) &&
			(state.Direction == 2 || state.MarginDebt > 0 || len(state.MarginDebtEvents) > 0) {
			return nil, fmt.Errorf("funding_carry Bot %s has unresolved debt ownership", botID)
		}
		if state.Direction == 2 && len(state.MarginDebtEvents) == 0 {
			return nil, fmt.Errorf("funding_carry Bot %s has active debt without a transaction ledger", botID)
		}
		botDebt, err := fundingCarryDebtAt(state.MarginDebtEvents, rawAsset, accountScope, accruedAt)
		if err != nil {
			return nil, fmt.Errorf("replay funding_carry debt for Bot %s: %w", botID, err)
		}
		if botDebt > 0 {
			debtsByBot[botID] = botDebt
		}
	}

	botIDs := make([]string, 0, len(debtsByBot))
	totalPrincipal := 0.0
	for botID, debt := range debtsByBot {
		botIDs = append(botIDs, botID)
		totalPrincipal += debt
	}
	sort.Strings(botIDs)
	tolerance := math.Max(1e-8, math.Max(math.Abs(totalPrincipal), math.Abs(payment.Principal))*1e-8)
	if math.Abs(totalPrincipal-payment.Principal) > tolerance {
		return nil, fmt.Errorf("account debt mismatch at %s: Bot-owned %.12g %s, exchange principal %.12g %s", accruedAt.Format(time.RFC3339Nano), totalPrincipal, rawAsset, payment.Principal, rawAsset)
	}
	if payment.Interest == 0 {
		return map[string]fundingCarryInterestShare{}, nil
	}
	if totalPrincipal <= 0 || len(botIDs) == 0 {
		return nil, fmt.Errorf("positive interest has no verified Bot-owned principal")
	}
	interestTotal, err := roundMarginInterestStorageAmount(payment.Interest)
	if err != nil || interestTotal <= 0 {
		return nil, fmt.Errorf("positive margin interest cannot be represented at database precision")
	}
	accountPrincipal, err := roundMarginInterestStorageAmount(payment.Principal)
	if err != nil || accountPrincipal <= 0 {
		return nil, fmt.Errorf("account margin principal cannot be represented at database precision")
	}
	allocations := make(map[string]fundingCarryInterestShare, len(botIDs))
	allocated := 0.0
	for index, botID := range botIDs {
		share := interestTotal - allocated
		if index != len(botIDs)-1 {
			share = interestTotal * debtsByBot[botID] / totalPrincipal
			share, err = roundMarginInterestStorageAmount(share)
			if err != nil {
				return nil, fmt.Errorf("margin interest share for Bot %s cannot be represented at database precision", botID)
			}
			allocated += share
		}
		botPrincipal, roundErr := roundMarginInterestStorageAmount(debtsByBot[botID])
		if roundErr != nil || botPrincipal <= 0 {
			return nil, fmt.Errorf("margin principal for Bot %s cannot be represented at database precision", botID)
		}
		if index == len(botIDs)-1 {
			share, err = roundMarginInterestStorageAmount(interestTotal - allocated)
			if err != nil {
				return nil, fmt.Errorf("residual margin interest share cannot be represented at database precision")
			}
		}
		if share <= 0 {
			return nil, fmt.Errorf("margin interest share for Bot %s is below database precision", botID)
		}
		allocations[botID] = fundingCarryInterestShare{BotPrincipal: botPrincipal, Interest: share}
	}
	return allocations, nil
}

func roundMarginInterestStorageAmount(value float64) (float64, error) {
	if !finiteNonNegativeNumber(value) || value > math.MaxFloat64/marginInterestStorageScale {
		return 0, fmt.Errorf("margin interest value is outside the supported database precision range")
	}
	rounded := math.Round(value*marginInterestStorageScale) / marginInterestStorageScale
	if !finiteNonNegativeNumber(rounded) {
		return 0, fmt.Errorf("margin interest rounding produced a non-finite value")
	}
	return rounded, nil
}

func fundingCarryDebtAt(events []fundingCarryMarginDebtEventSnapshot, rawAsset, accountScope string, at time.Time) (float64, error) {
	ordered := append([]fundingCarryMarginDebtEventSnapshot(nil), events...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].OccurredAt.Equal(ordered[j].OccurredAt) {
			return ordered[i].TransferID < ordered[j].TransferID
		}
		return ordered[i].OccurredAt.Before(ordered[j].OccurredAt)
	})
	debt := 0.0
	var lastAt time.Time
	var lastTransferID int64
	for _, event := range ordered {
		if !strings.EqualFold(strings.TrimSpace(event.Asset), rawAsset) || event.OccurredAt.After(at) {
			continue
		}
		if event.AccountScope != accountScope || event.TransferID <= 0 || event.OccurredAt.IsZero() ||
			!finiteNonNegativeNumber(event.Amount) || event.Amount <= 0 || !finiteNonNegativeNumber(event.Principal) ||
			!finiteNonNegativeNumber(event.InterestPaid) || math.Abs(event.Principal+event.InterestPaid-event.Amount) > math.Max(1e-10, event.Amount*1e-8) {
			return 0, fmt.Errorf("transaction %d has incomplete or inconsistent account/amount evidence", event.TransferID)
		}
		if event.OccurredAt.Equal(lastAt) && event.TransferID != lastTransferID {
			return 0, fmt.Errorf("multiple %s debt transactions share an indistinguishable server timestamp", rawAsset)
		}
		lastAt, lastTransferID = event.OccurredAt, event.TransferID
		switch event.Action {
		case "borrow":
			if math.Abs(event.Principal-event.Amount) > math.Max(1e-10, event.Amount*1e-8) || event.InterestPaid != 0 {
				return 0, fmt.Errorf("borrow transaction %d has inconsistent principal", event.TransferID)
			}
			debt += event.Principal
		case "repay":
			debt -= event.Principal
			if debt < -math.Max(1e-8, event.Principal*1e-8) {
				return 0, fmt.Errorf("repayment transaction %d exceeds reconstructed principal", event.TransferID)
			}
			if debt < 0 {
				debt = 0
			}
		default:
			return 0, fmt.Errorf("transaction %d has unsupported action %q", event.TransferID, event.Action)
		}
	}
	return debt, nil
}
