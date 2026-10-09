package strategy

import (
	"context"
	"fmt"
	"strings"

	"quantmesh/exchange"
)

type positionSnapshotQueryError struct{ cause error }

func (e *positionSnapshotQueryError) Error() string { return e.cause.Error() }
func (e *positionSnapshotQueryError) Unwrap() error { return e.cause }

// readScopedPositionSnapshot accepts only an authoritative snapshot whose
// every row belongs to the requested symbol. A symbol-less row must not be
// allowed to prove a strategy's position is flat.
func readScopedPositionSnapshot(ctx context.Context, ex exchange.IExchange, symbol string) ([]*exchange.Position, error) {
	if ctx == nil || ex == nil || strings.TrimSpace(symbol) == "" {
		return nil, fmt.Errorf("position snapshot requires context, exchange, and symbol")
	}
	if err := ctx.Err(); err != nil {
		return nil, &positionSnapshotQueryError{cause: fmt.Errorf("position snapshot for %s was not queried: %w", symbol, err)}
	}
	positions, err := ex.GetPositions(ctx, symbol)
	if err != nil {
		return nil, &positionSnapshotQueryError{cause: err}
	}
	if err := ctx.Err(); err != nil {
		return nil, &positionSnapshotQueryError{cause: fmt.Errorf("position snapshot for %s completed after its context ended: %w", symbol, err)}
	}
	if positions == nil {
		return nil, fmt.Errorf("position snapshot for %s is nil; exposure is unverified", symbol)
	}
	for index, position := range positions {
		if position == nil {
			return nil, fmt.Errorf("position snapshot for %s contains nil row %d", symbol, index)
		}
		if !strings.EqualFold(strings.TrimSpace(position.Symbol), strings.TrimSpace(symbol)) {
			return nil, fmt.Errorf("position snapshot row %d has symbol %q, expected %q", index, position.Symbol, symbol)
		}
	}
	return positions, nil
}
