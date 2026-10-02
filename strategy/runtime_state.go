package strategy

import "context"

// RuntimeStateContextReader bounds safety-critical proof reads by the caller's
// lease/request context, without changing legacy recovery store contracts.
type RuntimeStateContextReader interface {
	LoadRuntimeStateContext(ctx context.Context, strategyName string) (int, string, bool, error)
}

// RuntimeStateStore persists versioned snapshots under a bot/strategy identity.
type RuntimeStateStore interface {
	LoadRuntimeState(strategyName string) (schemaVersion int, payload string, found bool, err error)
	SaveRuntimeState(strategyName string, schemaVersion int, payload string) error
}
