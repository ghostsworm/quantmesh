package order

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"quantmesh/exchange"
)

func validateOrderExecution(req *OrderRequest) (exchange.OrderType, exchange.TimeInForce, error) {
	kind := exchange.OrderType(strings.ToUpper(strings.TrimSpace(req.Type)))
	if kind == "" {
		kind = exchange.OrderTypeLimit
	}
	tif := exchange.TimeInForce(strings.ToUpper(strings.TrimSpace(req.TimeInForce)))
	invalid := func(reason string) (exchange.OrderType, exchange.TimeInForce, error) {
		return "", "", fmt.Errorf("invalid order execution: %s", reason)
	}
	if math.IsNaN(req.Quantity) || math.IsInf(req.Quantity, 0) || req.Quantity <= 0 {
		return invalid("quantity must be positive and finite")
	}
	if math.IsNaN(req.Price) || math.IsInf(req.Price, 0) || req.Price < 0 {
		return invalid("price must be finite and non-negative")
	}
	switch kind {
	case exchange.OrderTypeMarket:
		if req.PostOnly || tif != "" {
			return invalid("MARKET cannot be PostOnly or carry a limit time-in-force")
		}
	case exchange.OrderTypeLimit:
		if req.Price <= 0 {
			return invalid("LIMIT needs a positive price")
		}
		if tif == "" {
			tif = exchange.TimeInForceGTC
		}
		switch tif {
		case exchange.TimeInForceGTC, exchange.TimeInForceIOC, exchange.TimeInForceFOK, exchange.TimeInForceGTX:
		default:
			return invalid("unsupported time-in-force")
		}
		if req.PostOnly && tif != exchange.TimeInForceGTC && tif != exchange.TimeInForceGTX {
			return invalid("PostOnly conflicts with immediate execution")
		}
	default:
		return invalid("unsupported order type")
	}
	return kind, tif, nil
}

func waitForOrderRetry(ctx context.Context, delay time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
