package strategy

import (
	"context"
	"fmt"

	"quantmesh/position"
)

// Normal runtime construction binds its one grid wrapper to the same manager
// whose startup/runtime accounting supplies the bot's inventory proof.
func (s *GridStrategy) VerifyCapitalReleaseManagerBinding(manager *position.SuperPositionManager) error {
	if s == nil || manager == nil || s.manager != manager {
		return fmt.Errorf("Grid capital inventory manager binding is mismatched")
	}
	return nil
}

// This capital-proof-only reader returns empty rows only after verifying the
// wrapper's own complete slot/accounting state. Nonempty/unverified state is an
// error, not a display snapshot. It intentionally needs no mark/PnL RPC and does
// not use GetAllSlotsDetailed's display limit. Normal UI getters are unchanged.
func (s *GridStrategy) CapitalReleaseInventorySnapshot(ctx context.Context) ([]*Position, []*Order, error) {
	if ctx == nil || s == nil || s.manager == nil {
		return nil, nil, fmt.Errorf("Grid capital inventory requires context and manager")
	}
	// manager is an immutable constructor binding; Start/Stop only change flags.
	empty, err := s.manager.GridRuntimeStateIsVerifiedEmptyContext(ctx)
	if err != nil {
		return nil, nil, err
	}
	if !empty {
		return nil, nil, fmt.Errorf("Grid capital inventory is nonempty or unverified")
	}
	return []*Position{}, []*Order{}, nil
}
