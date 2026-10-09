package bitget

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"quantmesh/exchange/accounting"
)

var accountFuturesProductTypes = []string{"USDT-FUTURES", "USDC-FUTURES", "COIN-FUTURES"}

// AccountFuturesPosition is a venue-reported position from an account-wide
// futures snapshot. Total remains a decimal string so callers can distinguish
// tiny nonzero positions from exact zero without float rounding.
type AccountFuturesPosition struct {
	ProductType string
	Symbol      string
	MarginCoin  string
	HoldSide    string
	Total       string
}

type AccountFuturesPositionReader interface {
	GetAccountFuturesPositions(context.Context) ([]AccountFuturesPosition, error)
}

// AccountFuturesFlatnessEvidence is a completed, read-only snapshot for the
// futures products represented by this adapter. It is not an atomic exchange
// snapshot and must not by itself authorize an old-account reset.
type AccountFuturesFlatnessEvidence struct {
	ObservedAt     time.Time
	Positions      []AccountFuturesPosition
	OpenOrderCount int
	Complete       bool
}

func (e AccountFuturesFlatnessEvidence) IsFlat() (bool, error) {
	if !e.Complete || e.ObservedAt.IsZero() || e.OpenOrderCount < 0 || e.Positions == nil {
		return false, fmt.Errorf("Bitget futures flatness evidence is incomplete")
	}
	if e.OpenOrderCount != 0 {
		return false, nil
	}
	for _, position := range e.Positions {
		open, err := position.HasOpenQuantity()
		if err != nil {
			return false, err
		}
		if open {
			return false, nil
		}
	}
	return true, nil
}

// GetAccountFuturesPositions exposes account-wide position evidence through
// the REST-only evidence adapter without requiring a trading runtime.
func (b *BitgetAdapter) GetAccountFuturesPositions(ctx context.Context) ([]AccountFuturesPosition, error) {
	if b == nil || b.client == nil {
		return nil, fmt.Errorf("Bitget futures account evidence client is unavailable")
	}
	return b.client.GetAccountFuturesPositions(ctx)
}

// ReadAccountFuturesFlatnessEvidence combines complete positions and open
// orders from the REST-only evidence adapter. Calls are sequential rather than
// exchange-atomic; lifecycle code must require repeat evidence before reset.
func (b *BitgetAdapter) ReadAccountFuturesFlatnessEvidence(ctx context.Context) (AccountFuturesFlatnessEvidence, error) {
	if ctx == nil || b == nil || b.client == nil {
		return AccountFuturesFlatnessEvidence{}, fmt.Errorf("Bitget futures flatness query requires context and evidence client")
	}
	positions, err := b.GetAccountFuturesPositions(ctx)
	if err != nil {
		return AccountFuturesFlatnessEvidence{}, err
	}
	orders, err := b.GetAccountOpenOrders(ctx)
	if err != nil {
		return AccountFuturesFlatnessEvidence{}, fmt.Errorf("query Bitget account-wide futures open orders: %w", err)
	}
	for _, order := range orders {
		if order == nil {
			return AccountFuturesFlatnessEvidence{}, fmt.Errorf("Bitget futures open-order snapshot contains a null row")
		}
	}
	return AccountFuturesFlatnessEvidence{ObservedAt: time.Now().UTC(), Positions: positions,
		OpenOrderCount: len(orders), Complete: true}, nil
}

// VerifyAccountFuturesPositionsAndOrdersFlat implements the shared read-only
// verifier contract. It proves only this non-atomic Futures snapshot.
func (b *BitgetAdapter) VerifyAccountFuturesPositionsAndOrdersFlat(ctx context.Context) error {
	evidence, err := b.ReadAccountFuturesFlatnessEvidence(ctx)
	if err != nil {
		return err
	}
	flat, err := evidence.IsFlat()
	if err != nil {
		return err
	}
	if !flat {
		return fmt.Errorf("Bitget Futures positions or open orders remain")
	}
	return nil
}

// GetAccountFuturesPositions reads every supported Bitget futures product.
// Empty data is accepted only when every product returns an explicit array;
// missing/null data, malformed rows, and any product error fail closed.
func (c *Client) GetAccountFuturesPositions(ctx context.Context) ([]AccountFuturesPosition, error) {
	all := make([]AccountFuturesPosition, 0)
	for _, productType := range accountFuturesProductTypes {
		resp, err := c.DoRequest(ctx, "GET", "/api/v2/mix/position/all-position?productType="+productType, nil)
		if err != nil {
			return nil, fmt.Errorf("query Bitget %s account-wide futures positions: %w", productType, err)
		}
		if len(resp.Data) == 0 || string(resp.Data) == "null" {
			return nil, fmt.Errorf("Bitget %s account-wide position response has missing or null data", productType)
		}
		var rows []struct {
			Symbol     string `json:"symbol"`
			MarginCoin string `json:"marginCoin"`
			HoldSide   string `json:"holdSide"`
			Total      string `json:"total"`
		}
		if err := json.Unmarshal(resp.Data, &rows); err != nil || rows == nil {
			return nil, fmt.Errorf("decode Bitget %s account-wide positions: invalid or null list", productType)
		}
		for _, row := range rows {
			symbol := strings.TrimSpace(row.Symbol)
			marginCoin := strings.TrimSpace(row.MarginCoin)
			holdSide := strings.ToLower(strings.TrimSpace(row.HoldSide))
			total := strings.TrimSpace(row.Total)
			quantity, parseErr := accounting.Decimal(total)
			if symbol == "" || marginCoin == "" || (holdSide != "long" && holdSide != "short") || parseErr != nil || quantity.Sign() < 0 {
				return nil, fmt.Errorf("Bitget %s account-wide position contains an invalid row for %q", productType, symbol)
			}
			all = append(all, AccountFuturesPosition{ProductType: productType, Symbol: symbol,
				MarginCoin: marginCoin, HoldSide: holdSide, Total: total})
		}
	}
	return all, nil
}

// HasOpenQuantity reports whether a venue-reported quantity is mathematically
// nonzero. It deliberately does not apply a float/tolerance threshold.
func (p AccountFuturesPosition) HasOpenQuantity() (bool, error) {
	quantity, err := accounting.Decimal(p.Total)
	if err != nil || quantity.Sign() < 0 {
		return false, fmt.Errorf("invalid Bitget position quantity")
	}
	return quantity.Sign() != 0, nil
}
