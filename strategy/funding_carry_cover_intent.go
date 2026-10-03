package strategy

import (
	"context"
	"fmt"
	"strings"
	"time"

	"quantmesh/exchange"
	"quantmesh/utils"
)

type fundingCarryCoverIntent struct {
	ClientOrderID string    `json:"client_order_id"`
	Symbol        string    `json:"symbol"`
	Asset         string    `json:"asset"`
	AccountScope  string    `json:"account_scope"`
	Quantity      float64   `json:"quantity"`
	Price         float64   `json:"price"`
	DebtToCover   float64   `json:"debt_to_cover"`
	PreparedAt    time.Time `json:"prepared_at"`
}

func cloneFundingCarryCoverIntent(intent *fundingCarryCoverIntent) *fundingCarryCoverIntent {
	if intent == nil {
		return nil
	}
	copy := *intent
	return &copy
}

func (s *FundingCarryStrategy) prepareMarginCoverIntent(ctx context.Context, req *exchange.OrderRequest, debt float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.verifyDebtCommitLocked(ctx); err != nil {
		return err
	}
	if !s.intentInFlight || s.marginCoverIntent != nil || req == nil || req.Symbol != s.symbol || req.Side != exchange.SideBuy || req.Type != exchange.OrderTypeLimit || !finitePositive(req.Quantity) || !finitePositive(req.Price) || !finitePositive(debt) || strings.TrimSpace(s.spot.GetBaseAsset()) == "" {
		return fmt.Errorf("margin cover request requires valid durable operation")
	}
	req.ClientOrderID = utils.NewCompactOrderID()
	s.marginCoverIntent = &fundingCarryCoverIntent{ClientOrderID: req.ClientOrderID, Symbol: req.Symbol, Asset: s.spot.GetBaseAsset(), AccountScope: s.marginAccountScope, Quantity: req.Quantity, Price: req.Price, DebtToCover: debt, PreparedAt: time.Now().UTC()}
	if err := s.persistRuntimeStateLocked(); err != nil {
		s.unownedExposure, s.runtimeStateErr = true, err
		return err
	}
	return s.verifyDebtCommitLocked(ctx)
}

func validateFundingCarryCoverIntent(state fundingCarryRuntimeState) error {
	i := state.MarginCoverIntent
	if i == nil {
		return nil
	}
	if !state.IntentInFlight || state.Direction != DirectionReverse || strings.TrimSpace(i.ClientOrderID) == "" || strings.TrimSpace(i.Asset) == "" || i.Symbol != state.Symbol || i.AccountScope != state.MarginAccountScope || !finitePositive(i.Quantity) || !finitePositive(i.Price) || !finitePositive(i.DebtToCover) || i.PreparedAt.IsZero() {
		return fmt.Errorf("margin cover submission intent is invalid")
	}
	return nil
}
