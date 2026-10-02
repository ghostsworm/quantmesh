package strategy

import (
	"fmt"
	"strings"
)

type fundingCarryDebtEventIdentity struct {
	AccountScope string
	Action       string
	TransferID   int64
}

func fundingCarryDebtEventKey(event fundingCarryMarginDebtEvent) fundingCarryDebtEventIdentity {
	return fundingCarryDebtEventIdentity{AccountScope: event.AccountScope, Action: event.Action, TransferID: event.TransferID}
}

// Caller holds s.mu. A retry may not rewrite financial evidence for one identity.
func (s *FundingCarryStrategy) debtEventReplayLocked(event fundingCarryMarginDebtEvent) (bool, error) {
	for _, existing := range s.marginDebtEvents {
		if fundingCarryDebtEventKey(existing) != fundingCarryDebtEventKey(event) {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(existing.Asset), strings.TrimSpace(event.Asset)) &&
			existing.Amount == event.Amount && existing.Principal == event.Principal &&
			existing.InterestPaid == event.InterestPaid && existing.OccurredAt.Equal(event.OccurredAt) {
			return true, nil
		}
		return false, fmt.Errorf("margin %s transaction %d conflicts with its recorded financial evidence", event.Action, event.TransferID)
	}
	return false, nil
}
