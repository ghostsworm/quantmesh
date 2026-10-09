package binance

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"quantmesh/exchange/accounting"
)

type AccountFuturesPositionEvidence struct {
	Symbol         string
	PositionSide   string
	Amount         string
	IsolatedWallet string
}

type AccountFuturesFlatnessEvidence struct {
	ObservedAt     time.Time
	Positions      []AccountFuturesPositionEvidence
	OpenOrderCount int
	Complete       bool
}

func (e AccountFuturesFlatnessEvidence) IsFlat() (bool, error) {
	if !e.Complete || e.ObservedAt.IsZero() || e.Positions == nil || e.OpenOrderCount < 0 {
		return false, fmt.Errorf("Binance futures flatness evidence is incomplete")
	}
	if e.OpenOrderCount != 0 {
		return false, nil
	}
	for _, position := range e.Positions {
		amount, err := accounting.Decimal(position.Amount)
		if err != nil {
			return false, fmt.Errorf("invalid Binance position amount")
		}
		isolatedWallet, err := accounting.Decimal(position.IsolatedWallet)
		if err != nil {
			return false, fmt.Errorf("invalid Binance isolated wallet")
		}
		if amount.Sign() != 0 || isolatedWallet.Sign() != 0 {
			return false, nil
		}
	}
	return true, nil
}

// ReadAccountFuturesFlatnessEvidence reads the all-symbol Futures position
// risk and account-wide open-order endpoints using signed REST only. The pair
// of endpoint reads is not atomic and does not cover spot/margin liabilities.
func (b *BinanceAdapter) ReadAccountFuturesFlatnessEvidence(ctx context.Context) (AccountFuturesFlatnessEvidence, error) {
	if ctx == nil || b == nil || b.client == nil {
		return AccountFuturesFlatnessEvidence{}, fmt.Errorf("Binance futures flatness query requires context and evidence client")
	}
	serverTime, err := b.equityServerTime(ctx)
	if err != nil {
		return AccountFuturesFlatnessEvidence{}, fmt.Errorf("read Binance server time for flatness evidence: %w", err)
	}
	var positionRows []struct {
		Symbol         string `json:"symbol"`
		PositionSide   string `json:"positionSide"`
		PositionAmount string `json:"positionAmt"`
		IsolatedWallet string `json:"isolatedWallet"`
	}
	if err := b.equityGET(ctx, "/fapi/v2/positionRisk", nil, serverTime.UnixMilli(), &positionRows); err != nil {
		return AccountFuturesFlatnessEvidence{}, fmt.Errorf("read Binance all-symbol Futures positions: %w", err)
	}
	if positionRows == nil {
		return AccountFuturesFlatnessEvidence{}, fmt.Errorf("Binance all-symbol Futures position response is null")
	}
	positions := make([]AccountFuturesPositionEvidence, 0, len(positionRows))
	for _, row := range positionRows {
		symbol := strings.TrimSpace(row.Symbol)
		side := strings.ToUpper(strings.TrimSpace(row.PositionSide))
		amount := strings.TrimSpace(row.PositionAmount)
		isolatedWallet := strings.TrimSpace(row.IsolatedWallet)
		if symbol == "" || (side != "BOTH" && side != "LONG" && side != "SHORT") {
			return AccountFuturesFlatnessEvidence{}, fmt.Errorf("Binance all-symbol Futures position has invalid identity")
		}
		if _, err := accounting.Decimal(amount); err != nil {
			return AccountFuturesFlatnessEvidence{}, fmt.Errorf("Binance all-symbol Futures position amount is invalid")
		}
		if _, err := accounting.Decimal(isolatedWallet); err != nil {
			return AccountFuturesFlatnessEvidence{}, fmt.Errorf("Binance all-symbol Futures isolated wallet is invalid")
		}
		positions = append(positions, AccountFuturesPositionEvidence{Symbol: symbol, PositionSide: side,
			Amount: amount, IsolatedWallet: isolatedWallet})
	}

	var orderRows []struct {
		OrderID json.Number `json:"orderId"`
		Symbol  string      `json:"symbol"`
	}
	if err := b.equityGET(ctx, "/fapi/v1/openOrders", url.Values{}, serverTime.UnixMilli(), &orderRows); err != nil {
		return AccountFuturesFlatnessEvidence{}, fmt.Errorf("read Binance account-wide Futures open orders: %w", err)
	}
	if orderRows == nil {
		return AccountFuturesFlatnessEvidence{}, fmt.Errorf("Binance account-wide Futures open-order response is null")
	}
	for _, order := range orderRows {
		orderID, err := order.OrderID.Int64()
		if err != nil || orderID <= 0 || strings.TrimSpace(order.Symbol) == "" {
			return AccountFuturesFlatnessEvidence{}, fmt.Errorf("Binance account-wide Futures open-order row is invalid")
		}
	}
	return AccountFuturesFlatnessEvidence{ObservedAt: time.Now().UTC(), Positions: positions,
		OpenOrderCount: len(orderRows), Complete: true}, nil
}

// VerifyAccountFuturesPositionsAndOrdersFlat implements the shared read-only
// verifier contract. A nil error proves only this non-atomic Futures snapshot.
func (b *BinanceAdapter) VerifyAccountFuturesPositionsAndOrdersFlat(ctx context.Context) error {
	evidence, err := b.ReadAccountFuturesFlatnessEvidence(ctx)
	if err != nil {
		return err
	}
	flat, err := evidence.IsFlat()
	if err != nil {
		return err
	}
	if !flat {
		return fmt.Errorf("Binance Futures positions or open orders remain")
	}
	return nil
}
