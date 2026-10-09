package sync

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"quantmesh/exchange"
	"quantmesh/position"
	"quantmesh/storage"
)

type orderFillProvider interface {
	GetOrderFills(context.Context, string, int64) ([]*exchange.OrderFill, error)
}

type orderFillWriter interface {
	SaveOrderFill(*storage.OrderFill) error
}

type orderFillBatchWriter interface {
	SaveOrderFillsAtomic([]*storage.OrderFill) error
}

// PersistOwnedOrderFills fetches the authoritative fills for an already-owned order,
// verifies their cumulative quantity, and stores each execution idempotently.
func PersistOwnedOrderFills(ctx context.Context, provider orderFillProvider, writer orderFillWriter, update position.OrderUpdate,
	exchangeName, marketType, accountScope, account, botID string) error {
	if ctx == nil || provider == nil || writer == nil {
		return fmt.Errorf("execution capture requires context, fill provider, and persistent writer")
	}
	if update.OrderID <= 0 || update.Symbol == "" || update.ExecutedQty <= 0 || exchangeName == "" || marketType == "" || accountScope == "" {
		return fmt.Errorf("execution capture requires a complete owned order and account scope")
	}
	orderSide := strings.ToUpper(strings.TrimSpace(update.Side))
	if orderSide != "" && orderSide != string(exchange.SideBuy) && orderSide != string(exchange.SideSell) {
		return fmt.Errorf("owned order %d has invalid side %q", update.OrderID, update.Side)
	}
	fills, err := provider.GetOrderFills(ctx, update.Symbol, update.OrderID)
	if err != nil {
		return fmt.Errorf("fetch fills for owned order %d: %w", update.OrderID, err)
	}
	if len(fills) == 0 {
		return fmt.Errorf("exchange returned no executions for order %d with executed quantity %.12g", update.OrderID, update.ExecutedQty)
	}
	seen := make(map[string]struct{}, len(fills))
	rows := make([]*storage.OrderFill, 0, len(fills))
	var totalQty float64
	for _, fill := range fills {
		if fill == nil || fill.TradeID == "" || fill.OrderID != update.OrderID || fill.Symbol != update.Symbol || fill.TradeTime <= 0 || fill.Price <= 0 || fill.Quantity <= 0 || math.IsNaN(fill.Price) || math.IsInf(fill.Price, 0) || math.IsNaN(fill.Quantity) || math.IsInf(fill.Quantity, 0) || math.IsNaN(fill.Commission) || math.IsInf(fill.Commission, 0) {
			return fmt.Errorf("exchange returned an invalid execution for order %d", update.OrderID)
		}
		fillSide := strings.ToUpper(strings.TrimSpace(string(fill.Side)))
		if (fillSide != string(exchange.SideBuy) && fillSide != string(exchange.SideSell)) || (orderSide != "" && fillSide != orderSide) {
			return fmt.Errorf("exchange returned execution %q with invalid or mismatched side for order %d", fill.TradeID, update.OrderID)
		}
		if _, exists := seen[fill.TradeID]; exists {
			return fmt.Errorf("exchange returned duplicate execution ID %q for order %d", fill.TradeID, update.OrderID)
		}
		seen[fill.TradeID] = struct{}{}
		totalQty += fill.Quantity
		row := &storage.OrderFill{
			Exchange: exchangeName, MarketType: marketType, AccountScope: accountScope,
			Account: account, BotID: botID, Symbol: update.Symbol, TradeID: fill.TradeID,
			OrderID: fill.OrderID, Side: fillSide, Price: fill.Price, Quantity: fill.Quantity,
			QuoteQuantity: fill.QuoteQuantity,
			Commission:    fill.Commission, CommissionAsset: fill.CommissionAsset,
			RealizedPnLAsset: fill.RealizedPnLAsset,
			CommissionQuote:  fill.CommissionQuote, CommissionQuoteRate: fill.CommissionQuoteRate, CommissionQuoteKnown: fill.CommissionQuoteKnown,
			TradeTime: time.UnixMilli(fill.TradeTime).UTC(),
		}
		if fill.RealizedPnLKnown {
			pnl := fill.RealizedPnL
			row.RealizedPnL = &pnl
		}
		rows = append(rows, row)
	}
	tolerance := math.Max(1e-12, update.ExecutedQty*1e-9)
	if math.Abs(totalQty-update.ExecutedQty) > tolerance {
		return fmt.Errorf("execution quantity for order %d is incomplete: fills %.12g, order update %.12g", update.OrderID, totalQty, update.ExecutedQty)
	}
	if batchWriter, ok := writer.(orderFillBatchWriter); ok {
		if err := batchWriter.SaveOrderFillsAtomic(rows); err != nil {
			return fmt.Errorf("persist complete execution batch for order %d: %w", update.OrderID, err)
		}
		return nil
	}
	for _, row := range rows {
		if err := writer.SaveOrderFill(row); err != nil {
			return fmt.Errorf("persist execution %s for order %d: %w", row.TradeID, update.OrderID, err)
		}
	}
	return nil
}
