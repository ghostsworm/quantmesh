package order

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"quantmesh/accounting"
	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/execution"
)

// SetOpeningAdmissionGuard installs a fail-closed check evaluated before an
// opening order acquires its gate lease. Protective closes do not invoke it.
func (oe *ExchangeOrderExecutor) SetOpeningAdmissionGuard(guard func(context.Context) error) {
	oe.openingAdmissionGuard = guard
}

// SetPhysicalSubmissionGuard installs an owner check before any physical
// venue RPC, including protective closes and every retry. It cannot revoke an
// RPC that was already sent.
func (oe *ExchangeOrderExecutor) SetPhysicalSubmissionGuard(guard func(context.Context) error) {
	oe.physicalSubmissionGuard = guard
}

func (oe *ExchangeOrderExecutor) verifyPhysicalSubmission(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if oe.physicalSubmissionGuard == nil {
		return nil
	}
	if err := oe.physicalSubmissionGuard(ctx); err != nil {
		return fmt.Errorf("physical submission ownership check failed: %w", err)
	}
	return nil
}

// SetOpeningGate wires the bot's shared gate before any strategy starts.
// PositionSide on a request takes precedence over the configured grid direction.
func (oe *ExchangeOrderExecutor) SetOpeningGate(gate *execution.OpeningGate, direction string) {
	if gate == nil {
		gate = &execution.OpeningGate{}
	}
	oe.openingGate = gate
	oe.positionDirection = config.NormalizeDirection(direction)
}

func (oe *ExchangeOrderExecutor) IsOpeningPaused() bool {
	return oe.openingGate != nil && oe.openingGate.Blocked()
}

func (oe *ExchangeOrderExecutor) admitOrder(req *OrderRequest) (func(), execution.OpeningLease, error) {
	if req == nil {
		return nil, execution.OpeningLease{}, fmt.Errorf("order request is nil")
	}
	side := strings.ToUpper(strings.TrimSpace(req.Side))
	if side != "BUY" && side != "SELL" {
		return nil, execution.OpeningLease{}, fmt.Errorf("invalid order side %q", req.Side)
	}
	if oe.openingGate == nil || !oe.isOpeningOrder(req) {
		return func() {}, execution.OpeningLease{}, nil
	}
	lease, err := oe.openingGate.BeginLease()
	if err != nil {
		return nil, execution.OpeningLease{}, err
	}
	return func() { oe.openingGate.ReleaseLease(lease.ID) }, lease, nil
}

func validateRecoveryOrderSubmission(oe *ExchangeOrderExecutor, req *OrderRequest, orderType exchange.OrderType, tif exchange.TimeInForce, permit accounting.OneShotPermit) error {
	if oe.openingGate == nil || !oe.isOpeningOrder(req) {
		return execution.ErrOpeningPaused
	}
	if permit.Kind != accounting.RecoveryActionOrder || permit.State != accounting.PermitAuthorized {
		return errors.New("recovery order requires an authorized order permit")
	}
	if req.ClientOrderID == "" {
		return errors.New("recovery order requires its pre-persisted client order ID")
	}
	hash, err := CanonicalRecoveryOrderIntentHash(oe, req, orderType, tif)
	if err != nil {
		return err
	}
	if hash != permit.OrderIntentHash {
		return accounting.ErrRecoveryConflict
	}
	return permit.Validate()
}

func (oe *ExchangeOrderExecutor) beginRecoveryOrder(submission *recoveryOrderSubmission) (execution.OpeningLease, func(), error) {
	lease, err := oe.openingGate.BeginWithEpisodeActionPermit(submission.permit)
	if err != nil {
		return execution.OpeningLease{}, nil, err
	}
	release := func() { oe.openingGate.ReleaseLease(lease.ID) }
	return lease, release, nil
}

func sameConsumedRecoveryPermit(authorized, consumed accounting.OneShotPermit) bool {
	if consumed.State != accounting.PermitConsumed || authorized.ID != consumed.ID || authorized.EpisodeID != consumed.EpisodeID ||
		authorized.Generation != consumed.Generation || authorized.ActionID != consumed.ActionID || authorized.ActionOrdinal != consumed.ActionOrdinal ||
		authorized.Kind != consumed.Kind || authorized.AccountScope != consumed.AccountScope || authorized.OwnerGeneration != consumed.OwnerGeneration ||
		authorized.LeaseID != consumed.LeaseID || authorized.OrderIntentHash != consumed.OrderIntentHash ||
		authorized.ExpectedRevision != consumed.ExpectedRevision || len(authorized.GateSourceExceptions) != len(consumed.GateSourceExceptions) {
		return false
	}
	for i := range authorized.GateSourceExceptions {
		if authorized.GateSourceExceptions[i] != consumed.GateSourceExceptions[i] {
			return false
		}
	}
	return true
}

// CanonicalRecoveryOrderIntentHash binds the durable permit to the exact
// client ID, market/leg and order parameters that will be passed to the venue.
func CanonicalRecoveryOrderIntentHash(oe *ExchangeOrderExecutor, req *OrderRequest, orderType exchange.OrderType, tif exchange.TimeInForce) (string, error) {
	if oe == nil || oe.exchange == nil || req == nil {
		return "", errors.New("recovery order intent requires an executor and request")
	}
	return (accounting.RecoveryOrderIntent{
		SchemaVersion: 1, Exchange: oe.exchange.GetName(), Market: oe.exchange.GetMarketType(), Symbol: req.Symbol, Side: req.Side,
		OrderType: string(orderType), TimeInForce: string(tif), Price: strconv.FormatFloat(req.Price, 'f', -1, 64),
		Quantity: strconv.FormatFloat(req.Quantity, 'f', -1, 64), PriceDecimals: req.PriceDecimals,
		ReduceOnly: req.ReduceOnly, PostOnly: req.PostOnly, PositionSide: req.PositionSide,
		ClientOrderID: req.ClientOrderID,
	}).CanonicalHash()
}

func (oe *ExchangeOrderExecutor) isOpeningOrder(req *OrderRequest) bool {
	side := strings.ToUpper(strings.TrimSpace(req.Side))
	// Futures without ReduceOnly must remain gated even when their direction
	// resembles a close: an oversized non-reduce order can reverse the position.
	// Spot closes cannot use ReduceOnly; their explicit inventory leg is needed.
	marketType := strings.ToLower(strings.TrimSpace(oe.exchange.GetMarketType()))
	if marketType == "spot" || marketType == "spot_margin" {
		// Spot venues do not enforce ReduceOnly. An incorrectly tagged BUY
		// must not bypass the gate merely because that flag is set.
		leg := strings.ToUpper(strings.TrimSpace(req.PositionSide))
		if leg == "" {
			leg = oe.positionDirection
		}
		if (leg == "LONG" && side == "SELL") || (leg == "SHORT" && side == "BUY") {
			return false
		}
	} else if req.ReduceOnly {
		return false
	}
	return true
}
