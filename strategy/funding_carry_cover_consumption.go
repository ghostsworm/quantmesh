package strategy

import (
	"fmt"
	"strings"
)

func validateFundingCarryCoverSource(orders []fundingCarryCoverOrder, pending *fundingCarryRepayIntent) error {
	if pending == nil || pending.CoverOrderID <= 0 || !validRuntimeAmount(pending.Amount) || pending.Amount <= 0 {
		return fmt.Errorf("repayment cover source requires a positive amount and identity")
	}
	for _, record := range orders {
		if record.OrderID != pending.CoverOrderID {
			continue
		}
		// DebtToCover is the historical buy target, not later accrued interest.
		// The exact repayment ACK binds actual consumption.
		if !record.Verified || !validRuntimeAmount(record.Net) || !validRuntimeAmount(record.Consumed) || (record.RepayTransferID == 0 && record.Consumed != 0) || record.AccountScope != pending.AccountScope || !strings.EqualFold(record.Asset, pending.Asset) || record.Net < pending.Amount && !fundingCarryFinancialAmountsMatch(record.Net, pending.Amount) {
			return fmt.Errorf("repayment does not match verified cover source")
		}
		if record.RepayTransferID != 0 && (record.RepayTransferID != pending.TransferID || !fundingCarryFinancialAmountsMatch(record.Consumed, pending.Amount)) {
			return fmt.Errorf("cover source already consumed by another repayment")
		}
		return nil
	}
	return fmt.Errorf("repayment cover order is missing")
}

// Called under s.mu; caller commits the result atomically with principal/event.
func (s *FundingCarryStrategy) coverOrdersAfterRepaymentLocked(event fundingCarryMarginDebtEvent) ([]fundingCarryCoverOrder, error) {
	pending := s.marginRepayIntent
	if pending == nil || pending.CoverOrderID == 0 {
		return s.marginCoverOrders, nil
	}
	if pending.TransferID != event.TransferID || pending.AccountScope != event.AccountScope || !strings.EqualFold(pending.Asset, event.Asset) || !fundingCarryFinancialAmountsMatch(pending.Amount, event.Amount) {
		return nil, fmt.Errorf("repayment consumption does not match pending acknowledgement")
	}
	if err := validateFundingCarryCoverSource(s.marginCoverOrders, pending); err != nil {
		return nil, err
	}
	orders := cloneFundingCarryCoverOrders(s.marginCoverOrders)
	for i := range orders {
		if orders[i].RepayTransferID == event.TransferID && orders[i].OrderID != pending.CoverOrderID {
			return nil, fmt.Errorf("repayment already attributed to another cover order")
		}
		if orders[i].OrderID == pending.CoverOrderID {
			orders[i].RepayTransferID, orders[i].Consumed = event.TransferID, event.Amount
		}
	}
	return orders, nil
}
