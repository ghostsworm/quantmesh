package order

import (
	"context"
	"errors"
	"fmt"

	"quantmesh/execution"
)

const RuntimeShutdownBlock = "runtime_shutdown"

var ErrRuntimeStopping = errors.New("runtime is stopping")

type shutdownCloseKey struct{}

type shutdownCloseOwners map[*ExchangeOrderExecutor]struct{}

func (oe *ExchangeOrderExecutor) IsShutdownCloseContext(ctx context.Context) bool {
	owners, ok := ctx.Value(shutdownCloseKey{}).(shutdownCloseOwners)
	_, authorized := owners[oe]
	return ok && authorized && oe.submissionGate.HasBlock(RuntimeShutdownBlock)
}

// BeginShutdown is irreversible for this executor. Block every runtime before
// waiting for any one of them; otherwise later Bots could keep adding risk.
func (oe *ExchangeOrderExecutor) BeginShutdown() {
	oe.openingGate.Block(RuntimeShutdownBlock)
	oe.submissionGate.Block(RuntimeShutdownBlock)
}

func (oe *ExchangeOrderExecutor) DrainShutdown(ctx context.Context) error {
	if !oe.submissionGate.HasBlock(RuntimeShutdownBlock) {
		return fmt.Errorf("shutdown has not begun")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return oe.submissionGate.Drain(ctx)
}

// ShutdownCloseContext grants only this executor's shutdown coordinator a
// close-only path after ordinary submissions drain. It does not bypass intent
// persistence, UNKNOWN checks, exposure limits or the opening gate.
func (oe *ExchangeOrderExecutor) ShutdownCloseContext(ctx context.Context) (context.Context, error) {
	if err := oe.DrainShutdown(ctx); err != nil {
		return nil, err
	}
	owners := make(shutdownCloseOwners)
	if existing, ok := ctx.Value(shutdownCloseKey{}).(shutdownCloseOwners); ok {
		for owner := range existing {
			owners[owner] = struct{}{}
		}
	}
	owners[oe] = struct{}{}
	return context.WithValue(ctx, shutdownCloseKey{}, owners), nil
}

func (oe *ExchangeOrderExecutor) admitSubmission(ctx context.Context, req *OrderRequest) (func(), error) {
	if owners, ok := ctx.Value(shutdownCloseKey{}).(shutdownCloseOwners); ok {
		_, authorized := owners[oe]
		if authorized &&
			oe.submissionGate.HasBlock(RuntimeShutdownBlock) && !oe.isOpeningOrder(req) {
			return func() {}, nil
		}
	}
	release, err := oe.submissionGate.Begin()
	if errors.Is(err, execution.ErrOpeningPaused) {
		return nil, ErrRuntimeStopping
	}
	return release, err
}
