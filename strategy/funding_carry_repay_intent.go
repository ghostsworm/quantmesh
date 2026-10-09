package strategy

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"quantmesh/storage"
)

type fundingCarryRepayIntent struct {
	Asset             string  `json:"asset"`
	AccountScope      string  `json:"account_scope"`
	Amount            float64 `json:"amount"`
	ExpectedRemaining float64 `json:"expected_remaining"`
	BorrowTransferID  int64   `json:"borrow_transfer_id"`
	TransferID        int64   `json:"transfer_id"`
	CoverOrderID      int64   `json:"cover_order_id,omitempty"`
}

func cloneFundingCarryRepayIntent(intent *fundingCarryRepayIntent) *fundingCarryRepayIntent {
	if intent == nil {
		return nil
	}
	copy := *intent
	return &copy
}

// Called while the strategy operation and account wallet coordination are held.
// A saved ACK may be queried again, but an uncertain ID-less RPC is never repeated.
func (s *FundingCarryStrategy) repayMarginPrincipal(ctx context.Context, asset string, amount, expectedRemaining float64) error {
	s.mu.RLock()
	coverID := int64(0)
	if s.marginRepayIntent != nil {
		coverID = s.marginRepayIntent.CoverOrderID
	}
	s.mu.RUnlock()
	return s.repayMarginPrincipalWithCover(ctx, asset, amount, expectedRemaining, coverID)
}

func (s *FundingCarryStrategy) repayMarginPrincipalWithCover(ctx context.Context, asset string, amount, expectedRemaining float64, coverID int64) error {
	s.mu.Lock()
	if err := s.verifyDebtCommitLocked(ctx); err != nil {
		s.mu.Unlock()
		return err
	}
	if !s.intentInFlight || strings.TrimSpace(asset) == "" || !validRuntimeAmount(amount) || amount <= 0 || !validRuntimeAmount(expectedRemaining) {
		s.mu.Unlock()
		return fmt.Errorf("margin repayment requires valid durable operation intent")
	}
	pending := cloneFundingCarryRepayIntent(s.marginRepayIntent)
	created := pending == nil
	if created {
		pending = &fundingCarryRepayIntent{Asset: asset, AccountScope: s.marginAccountScope, Amount: amount, ExpectedRemaining: expectedRemaining, BorrowTransferID: s.marginBorrowTransferID, CoverOrderID: coverID}
		if coverID != 0 {
			if err := validateFundingCarryCoverSource(s.marginCoverOrders, pending); err != nil {
				s.mu.Unlock()
				return err
			}
		}
		s.marginRepayIntent = cloneFundingCarryRepayIntent(pending)
		if err := s.persistRuntimeStateLocked(); err != nil {
			s.unownedExposure, s.runtimeStateErr = true, err
			s.mu.Unlock()
			return err
		}
	} else if pending.CoverOrderID != coverID || !strings.EqualFold(pending.Asset, asset) || pending.AccountScope != s.marginAccountScope || pending.BorrowTransferID != s.marginBorrowTransferID || !fundingCarryFinancialAmountsMatch(pending.Amount, amount) || !fundingCarryFinancialAmountsMatch(pending.ExpectedRemaining, expectedRemaining) {
		s.mu.Unlock()
		return fmt.Errorf("pending margin repayment does not match requested operation")
	}
	s.mu.Unlock()
	if created {
		id, rpcErr := s.marginEx.Repay(ctx, asset, amount)
		if id > 0 {
			s.mu.Lock()
			s.marginRepayIntent.TransferID = id
			pending.TransferID = id
			operationErr := s.verifyDebtCommitLocked(ctx)
			if rpcErr != nil || operationErr != nil {
				s.unownedExposure = true
			}
			var saveErr error
			if verifyStrategyWalletRuntimeOwner(s.openingGate) == nil {
				saveErr = s.persistRuntimeStateLocked()
			}
			if saveErr != nil {
				s.unownedExposure, s.runtimeStateErr = true, saveErr
			}
			s.mu.Unlock()
			if err := errors.Join(rpcErr, operationErr, saveErr); err != nil {
				return err
			}
		} else {
			return errors.Join(rpcErr, fmt.Errorf("margin repayment has no accepted identity; recovery required"))
		}
	}
	if pending.TransferID <= 0 {
		return fmt.Errorf("uncertain margin repayment cannot be resubmitted")
	}
	if err := s.returnBorrowedPrincipal(ctx, pending.TransferID, pending.Asset, pending.Amount, pending.ExpectedRemaining); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.verifyDebtCommitLocked(ctx); err != nil {
		return err
	}
	return s.clearMarginRepayIntentLocked(ctx, pending)
}

// Caller holds s.mu and has verified the operation owner. A confirmed
// commit/cancellation keeps the cleared intent in memory to match durable
// state, while cancellation still stops the current financial flow.
func (s *FundingCarryStrategy) clearMarginRepayIntentLocked(ctx context.Context, pending *fundingCarryRepayIntent) error {
	s.marginRepayIntent = nil
	if err := s.persistRecoveryCheckpointLocked(ctx); err != nil {
		if errors.Is(err, storage.ErrFundingCarryRuntimeStateCommitConfirmedCanceled) {
			s.runtimeStateErr = nil
			return err
		}
		s.marginRepayIntent = pending
		s.unownedExposure, s.runtimeStateErr = true, err
		return err
	}
	return nil
}
