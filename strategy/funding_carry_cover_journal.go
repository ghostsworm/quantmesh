package strategy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"quantmesh/exchange"
	"quantmesh/utils"
)

// Historical evidence, not a spendable inventory balance. Repayment consumption
// and leftover-asset attribution must still be reconciled separately.
type fundingCarryCoverOrder struct {
	OrderID         int64                 `json:"order_id"`
	ClientOrderID   string                `json:"client_order_id,omitempty"`
	RequestPrice    float64               `json:"request_price,omitempty"`
	PreparedAt      time.Time             `json:"prepared_at,omitempty"`
	Asset           string                `json:"asset"`
	AccountScope    string                `json:"account_scope"`
	Requested       float64               `json:"requested"`
	DebtToCover     float64               `json:"debt_to_cover"`
	Gross           float64               `json:"gross"`
	Net             float64               `json:"net"`
	Verified        bool                  `json:"verified"`
	RepayTransferID int64                 `json:"repay_transfer_id,omitempty"`
	Consumed        float64               `json:"consumed,omitempty"`
	Fills           []*exchange.OrderFill `json:"fills,omitempty"`
}

func cloneFundingCarryCoverOrders(orders []fundingCarryCoverOrder) []fundingCarryCoverOrder {
	cloned := append([]fundingCarryCoverOrder(nil), orders...)
	for i := range cloned {
		cloned[i].Fills = make([]*exchange.OrderFill, len(orders[i].Fills))
		for j, fill := range orders[i].Fills {
			if fill != nil {
				copy := *fill
				cloned[i].Fills[j] = &copy
			}
		}
	}
	return cloned
}

func (s *FundingCarryStrategy) checkpointMarginCoverOrder(ctx context.Context, order *exchange.Order, requested, debt float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.intentInFlight || order == nil || order.OrderID <= 0 || !validRuntimeAmount(requested) || requested <= 0 || !validRuntimeAmount(debt) || debt <= 0 {
		return fmt.Errorf("margin cover ACK requires valid operation and quantities")
	}
	for _, existing := range s.marginCoverOrders {
		if existing.OrderID == order.OrderID {
			return fmt.Errorf("margin cover order identity already recorded")
		}
	}
	pending := s.marginCoverIntent
	cid := order.ClientOrderID
	if pending != nil {
		if !fundingCarryFinancialAmountsMatch(pending.Quantity, requested) || !fundingCarryFinancialAmountsMatch(pending.DebtToCover, debt) || pending.AccountScope != s.marginAccountScope || (cid != "" && cid != pending.ClientOrderID && cid != utils.AddBrokerPrefix(strings.ToLower(s.marginEx.GetName()), pending.ClientOrderID)) {
			return fmt.Errorf("margin cover ACK does not match saved request")
		}
		cid = pending.ClientOrderID
	}
	s.marginCoverOrders = append(s.marginCoverOrders, fundingCarryCoverOrder{OrderID: order.OrderID, ClientOrderID: cid, Asset: s.spot.GetBaseAsset(), AccountScope: s.marginAccountScope, Requested: requested, DebtToCover: debt})
	if pending != nil {
		last := len(s.marginCoverOrders) - 1
		s.marginCoverOrders[last].RequestPrice, s.marginCoverOrders[last].PreparedAt = pending.Price, pending.PreparedAt
	}
	operationErr := s.verifyDebtCommitLocked(ctx)
	if operationErr != nil {
		s.unownedExposure = true
		if verifyStrategyWalletRuntimeOwner(s.openingGate) != nil {
			return operationErr
		}
	}
	s.marginCoverIntent = nil
	if err := s.persistRuntimeStateLocked(); err != nil {
		s.marginCoverIntent = pending
		s.unownedExposure, s.runtimeStateErr = true, err
		return errors.Join(operationErr, err) // retain local ACK if storage failed
	}
	return operationErr
}

func (s *FundingCarryStrategy) checkpointMarginCoverFills(ctx context.Context, id int64, gross, net float64, fills []*exchange.OrderFill) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.verifyDebtCommitLocked(ctx); err != nil {
		return err
	}
	for i := range s.marginCoverOrders {
		if s.marginCoverOrders[i].OrderID != id {
			continue
		}
		previous := s.marginCoverOrders[i]
		if previous.AccountScope != s.marginAccountScope || !strings.EqualFold(previous.Asset, s.spot.GetBaseAsset()) {
			return fmt.Errorf("margin cover evidence scope changed")
		}
		record := previous
		record.Gross, record.Net, record.Verified = gross, net, true
		record.Fills = cloneFundingCarryCoverOrders([]fundingCarryCoverOrder{{Fills: fills}})[0].Fills
		s.marginCoverOrders[i] = record
		if err := s.persistRuntimeStateLocked(); err != nil {
			s.marginCoverOrders[i] = previous
			s.unownedExposure, s.runtimeStateErr = true, err
			return err
		}
		return nil
	}
	return fmt.Errorf("margin cover fill evidence has no saved ACK")
}

func validateFundingCarryCoverOrders(state fundingCarryRuntimeState, allowPending bool) error {
	seen := make(map[int64]bool)
	usedRepayments := make(map[int64]bool)
	for _, record := range state.MarginCoverOrders {
		if record.OrderID <= 0 || seen[record.OrderID] || record.AccountScope != state.MarginAccountScope || strings.TrimSpace(record.Asset) == "" || !validRuntimeAmount(record.Requested) || record.Requested <= 0 || !validRuntimeAmount(record.DebtToCover) || record.DebtToCover <= 0 {
			return fmt.Errorf("margin cover journal identity is invalid")
		}
		seen[record.OrderID] = true
		if !validRuntimeAmount(record.RequestPrice) || (record.RequestPrice == 0) != record.PreparedAt.IsZero() {
			return fmt.Errorf("margin cover original request metadata is invalid")
		}
		if record.RepayTransferID < 0 || !validRuntimeAmount(record.Consumed) || (record.RepayTransferID == 0 && record.Consumed != 0) {
			return fmt.Errorf("margin cover consumption identity is invalid")
		}
		if record.RepayTransferID > 0 {
			if usedRepayments[record.RepayTransferID] || !record.Verified || record.Consumed <= 0 || record.Consumed > record.Net && !fundingCarryFinancialAmountsMatch(record.Consumed, record.Net) {
				return fmt.Errorf("margin cover consumption exceeds evidence or repeats repayment")
			}
			usedRepayments[record.RepayTransferID] = true
			matched := false
			for _, event := range state.MarginDebtEvents {
				if event.Action == "repay" && event.TransferID == record.RepayTransferID && event.AccountScope == record.AccountScope && strings.EqualFold(event.Asset, record.Asset) && fundingCarryFinancialAmountsMatch(event.Amount, record.Consumed) {
					matched = true
				}
			}
			if !matched {
				return fmt.Errorf("margin cover consumption has no matching repayment evidence")
			}
		}
		if !record.Verified {
			if !allowPending || !state.IntentInFlight || record.Gross != 0 || record.Net != 0 || len(record.Fills) != 0 {
				return fmt.Errorf("margin cover journal remains unresolved")
			}
			continue
		}
		net, err := fundingCarryNetCoverFromFills(state.Symbol, record.Asset, record.OrderID, record.Gross, record.Requested, record.DebtToCover, record.Fills)
		if err != nil || !fundingCarryFinancialAmountsMatch(net, record.Net) {
			return fmt.Errorf("margin cover journal fill evidence is invalid")
		}
	}
	if state.MarginRepayIntent != nil && state.MarginRepayIntent.CoverOrderID != 0 {
		return validateFundingCarryCoverSource(state.MarginCoverOrders, state.MarginRepayIntent)
	}
	return nil
}
