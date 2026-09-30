package main

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"quantmesh/exchange"
	"quantmesh/position"
	"quantmesh/storage"
	"quantmesh/strategy"
	ordersync "quantmesh/sync"
	"quantmesh/utils"
)

const fundingPerpSpreadOrderResolveTimeout = 5 * time.Second

func newFundingPerpSpreadExecutionRecorder(writer interface {
	SaveOrderFill(*storage.OrderFill) error
}, botID string, targets []fundingPerpSpreadIncomeTarget) strategy.FundingPerpSpreadExecutionRecorder {
	accountScopes := make(map[string]string, len(targets))
	for _, target := range targets {
		accountScopes[strings.ToLower(target.Exchange)+"/"+strings.ToUpper(target.Symbol)] = target.AccountScope
	}
	return func(ctx context.Context, client exchange.IExchange, request *exchange.OrderRequest, placedOrder *exchange.Order) (bool, error) {
		if ctx == nil || client == nil || request == nil || writer == nil || request.ClientOrderID == "" || request.Symbol == "" ||
			math.IsNaN(request.Quantity) || math.IsInf(request.Quantity, 0) || request.Quantity <= 0 {
			return false, fmt.Errorf("order submission is missing its exact exchange request or durable fill writer")
		}
		accountScope := accountScopes[strings.ToLower(client.GetName())+"/"+strings.ToUpper(request.Symbol)]
		if accountScope == "" || client.GetMarketType() == "" || botID == "" {
			return false, fmt.Errorf("account-scoped execution ledger identity is unavailable")
		}

		order := placedOrder
		resolveCtx, cancel := context.WithTimeout(ctx, fundingPerpSpreadOrderResolveTimeout)
		defer cancel()
		if order == nil || order.OrderID <= 0 {
			clientIDQuerier, ok := client.(exchange.OrderByClientIDQuerier)
			if !ok {
				return false, fmt.Errorf("exchange %s cannot recover order identity by client order ID", client.GetName())
			}
			resolvedOrder, err := clientIDQuerier.GetOrderByClientOrderID(resolveCtx, request.Symbol, request.ClientOrderID)
			if err != nil {
				return false, fmt.Errorf("query order by client ID %s: %w", request.ClientOrderID, err)
			}
			if resolvedOrder == nil {
				return false, fmt.Errorf("order for client ID %s is not found", request.ClientOrderID)
			}
			order = resolvedOrder
		}
		if order.OrderID <= 0 {
			return false, fmt.Errorf("exchange order has no usable order ID")
		}
		if err := validateFundingPerpSpreadOrderIdentity(client, request, order.OrderID, order); err != nil {
			return false, err
		}
		canonicalCID := utils.RemoveBrokerPrefix(strings.ToLower(client.GetName()), order.ClientOrderID)
		for !terminalOrderUpdate(string(order.Status)) {
			refreshed, err := client.GetOrder(resolveCtx, request.Symbol, order.OrderID)
			if err != nil {
				return false, fmt.Errorf("query terminal status for order %d: %w", order.OrderID, err)
			}
			if refreshed != nil {
				if err := validateFundingPerpSpreadOrderIdentity(client, request, order.OrderID, refreshed); err != nil {
					return false, err
				}
				order = refreshed
			}
			timer := time.NewTimer(150 * time.Millisecond)
			select {
			case <-resolveCtx.Done():
				timer.Stop()
				return false, fmt.Errorf("order %d did not reach a terminal state: %w", order.OrderID, resolveCtx.Err())
			case <-timer.C:
			}
		}
		if order.ExecutedQty < 0 || math.IsNaN(order.ExecutedQty) || math.IsInf(order.ExecutedQty, 0) || order.ExecutedQty > order.Quantity+math.Max(1e-12, order.Quantity*1e-9) {
			return false, fmt.Errorf("order %d reports an invalid executed quantity", order.OrderID)
		}
		if order.ExecutedQty == 0 {
			switch strings.ToUpper(string(order.Status)) {
			case "CANCELED", "CANCELLED", "REJECTED", "EXPIRED":
				return true, nil
			default:
				return false, fmt.Errorf("terminal order %d status %s has no verifiable executed quantity", order.OrderID, order.Status)
			}
		}
		update := position.OrderUpdate{
			OrderID: order.OrderID, ClientOrderID: canonicalCID, Symbol: order.Symbol,
			Side: string(order.Side), Status: string(order.Status),
			ExecutedQty: order.ExecutedQty, AvgPrice: order.AvgPrice,
		}
		if err := ordersync.PersistOwnedOrderFills(resolveCtx, client, writer, update,
			client.GetName(), client.GetMarketType(), accountScope, accountScope, botID); err != nil {
			return true, fmt.Errorf("persist complete fills for order %d: %w", order.OrderID, err)
		}
		return true, nil
	}
}

func validateFundingPerpSpreadOrderIdentity(client exchange.IExchange, request *exchange.OrderRequest, orderID int64, order *exchange.Order) error {
	if order == nil || order.OrderID != orderID || !strings.EqualFold(order.Symbol, request.Symbol) || order.Side != request.Side ||
		math.IsNaN(order.Quantity) || math.IsInf(order.Quantity, 0) ||
		math.Abs(order.Quantity-request.Quantity) > math.Max(1e-12, request.Quantity*1e-9) ||
		utils.RemoveBrokerPrefix(strings.ToLower(client.GetName()), order.ClientOrderID) != request.ClientOrderID {
		return fmt.Errorf("queried order %d identity does not match the submitted funding_perp_spread intent", orderID)
	}
	return nil
}
