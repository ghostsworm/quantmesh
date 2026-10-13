// Package execution contains execution controls shared by grid and signal strategies.
package execution

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"quantmesh/accounting"
)

var ErrOpeningPaused = errors.New("opening orders are paused")

// UnverifiedCancellationBlock survives removal of the original pause source
// until the owning executor verifies that residual opening orders terminated.
const UnverifiedCancellationBlock = "unverified_opening_cancellation"

// OpeningGate is a per-bot admission barrier, independent of strategy state.
// Block prevents new admissions immediately. Previously admitted operations must
// be drained before residual opening orders can be reliably cancelled.
// Each source can only remove its own block; closing orders never need a lease.
// The zero value is ready for use.
type OpeningGate struct {
	mu             sync.Mutex
	sources        map[string]struct{}
	active         int
	nextLeaseID    uint64
	leases         map[uint64]openingLeaseState
	issuedPermits  map[string]struct{}
	sourceLeaseIDs map[string]uint64
	idle           chan struct{}
	admissionCheck func() bool
}

type openingLeaseState struct {
	source               string
	permitID             string
	consumed             bool
	submitted            bool
	revoked              bool
	inFlight             bool
	physical             bool
	parentID             uint64
	gateSourceExceptions []string
}

// OpeningLease is a local admission capability. Its ID is monotonic for the
// lifetime of this gate and is never a substitute for the durable permit.
type OpeningLease struct {
	ID       uint64
	Source   string
	PermitID string
}

const RecoveryEpisodeGateSourcePrefix = "recovery_episode:"
const PairedExecutionLedgerGateSource = "execution_ledger_unverified"

func RecoveryEpisodeGateSource(episodeID string, generation uint64) string {
	if episodeID == "" || generation == 0 {
		return ""
	}
	return fmt.Sprintf("%s%s:%d", RecoveryEpisodeGateSourcePrefix, episodeID, generation)
}

// SetAdmissionCheck installs a read-only, nonblocking risk predicate. It must
// never call back into this gate or perform persistence/network operations.
func (g *OpeningGate) SetAdmissionCheck(check func() bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.admissionCheck = check
}

type openingAdmissionContextKey struct{}

// WithOpeningAdmissionCheck carries the shared live predicate into runtime
// constructors before any strategy producer starts.
func WithOpeningAdmissionCheck(ctx context.Context, check func() bool) context.Context {
	return context.WithValue(ctx, openingAdmissionContextKey{}, check)
}

func (g *OpeningGate) ApplyAdmissionContext(ctx context.Context) {
	if check, ok := ctx.Value(openingAdmissionContextKey{}).(func() bool); ok {
		g.SetAdmissionCheck(check)
	}
}

func (g *OpeningGate) Block(source string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.sources == nil {
		g.sources = make(map[string]struct{})
	}
	g.sources[source] = struct{}{}
	delete(g.sourceLeaseIDs, source)
	g.revokeUnsubmittedLeases(0)
}

// BlockForOperation binds a newly published source to the already-admitted
// logical operation that created it. Only physical child leases of that exact
// operation may pass this source; all other opening attempts remain blocked.
func (g *OpeningGate) BlockForOperation(source string, operation OpeningLease) error {
	if source != PairedExecutionLedgerGateSource || operation.ID == 0 || operation.Source != "" || operation.PermitID != "" {
		return ErrOpeningPaused
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, exists := g.leases[operation.ID]; !exists {
		return ErrOpeningPaused
	}
	if g.sources == nil {
		g.sources = make(map[string]struct{})
	}
	if _, exists := g.sources[source]; exists && g.sourceLeaseIDs[source] != operation.ID {
		return ErrOpeningPaused
	}
	g.sources[source] = struct{}{}
	if g.sourceLeaseIDs == nil {
		g.sourceLeaseIDs = make(map[string]uint64)
	}
	g.sourceLeaseIDs[source] = operation.ID
	g.revokeUnsubmittedLeases(0)
	return nil
}

// BeginOperationLease records an admitted multi-step operation. It is drainable
// but is not itself a physical submission capability.
func (g *OpeningGate) BeginOperationLease() (OpeningLease, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.sources) != 0 || (g.admissionCheck != nil && !g.admissionCheck()) {
		return OpeningLease{}, ErrOpeningPaused
	}
	return g.beginLeaseLocked(false), nil
}

// BeginChildOpeningLease grants a physical attempt to an already admitted
// multi-step operation, allowing only sources explicitly bound to that parent.
func (g *OpeningGate) BeginChildOpeningLease(operation OpeningLease) (OpeningLease, error) {
	if operation.ID == 0 || operation.Source != "" || operation.PermitID != "" {
		return OpeningLease{}, ErrOpeningPaused
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if state, exists := g.leases[operation.ID]; !exists || state.physical {
		return OpeningLease{}, ErrOpeningPaused
	}
	for source := range g.sources {
		if g.sourceLeaseIDs[source] != operation.ID {
			return OpeningLease{}, ErrOpeningPaused
		}
	}
	lease := g.beginLeaseLocked(true)
	state := g.leases[lease.ID]
	state.parentID = operation.ID
	g.leases[lease.ID] = state
	return lease, nil
}

// BeginRecoveryActionWithHold atomically publishes the source and creates the
// exact scoped lease for the durable, authorized action. All queued ordinary
// leases are revoked under the same mutex before this method returns.
func (g *OpeningGate) BeginRecoveryActionWithHold(permit accounting.OneShotPermit) (OpeningLease, error) {
	if err := permit.Validate(); err != nil {
		return OpeningLease{}, err
	}
	source := RecoveryEpisodeGateSource(permit.EpisodeID, permit.Generation)
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.sources == nil {
		g.sources = make(map[string]struct{})
	}
	if !matchesRecoverySources(g.sources, source, permit.GateSourceExceptions, false) {
		return OpeningLease{}, ErrOpeningPaused
	}
	g.sources[source] = struct{}{}
	if !matchesRecoverySources(g.sources, source, permit.GateSourceExceptions, true) {
		return OpeningLease{}, ErrOpeningPaused
	}
	lease, err := g.beginRecoveryLeaseLocked(permit, source)
	if err != nil {
		return OpeningLease{}, ErrOpeningPaused
	}
	g.revokeUnsubmittedLeases(lease.ID)
	return lease, nil
}

func (g *OpeningGate) beginRecoveryLeaseLocked(permit accounting.OneShotPermit, source string) (OpeningLease, error) {
	if permit.LeaseID == 0 || permit.LeaseID <= g.nextLeaseID {
		return OpeningLease{}, ErrOpeningPaused
	}
	if _, exists := g.leases[permit.LeaseID]; exists {
		return OpeningLease{}, ErrOpeningPaused
	}
	if _, exists := g.issuedPermits[permit.ID]; exists {
		return OpeningLease{}, ErrOpeningPaused
	}
	if g.active == 0 {
		g.idle = make(chan struct{})
	}
	g.active++
	g.nextLeaseID = permit.LeaseID
	if g.leases == nil {
		g.leases = make(map[uint64]openingLeaseState)
	}
	if g.issuedPermits == nil {
		g.issuedPermits = make(map[string]struct{})
	}
	g.issuedPermits[permit.ID] = struct{}{}
	lease := OpeningLease{ID: permit.LeaseID, Source: source, PermitID: permit.ID}
	g.leases[lease.ID] = openingLeaseState{source: source, permitID: permit.ID, physical: true, gateSourceExceptions: append([]string(nil), permit.GateSourceExceptions...)}
	return lease, nil
}

func matchesRecoverySources(current map[string]struct{}, episodeSource string, exceptions []string, exact bool) bool {
	expected := make(map[string]struct{}, len(exceptions)+1)
	expected[episodeSource] = struct{}{}
	for _, source := range exceptions {
		expected[source] = struct{}{}
	}
	if exact && len(current) != len(expected) {
		return false
	}
	for source := range current {
		if _, ok := expected[source]; !ok {
			return false
		}
	}
	if exact {
		for source := range expected {
			if _, ok := current[source]; !ok {
				return false
			}
		}
	}
	return true
}

func sameGateSourceExceptions(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func (g *OpeningGate) revokeUnsubmittedLeases(except uint64) {
	for id, state := range g.leases {
		if id == except || state.inFlight || !state.physical {
			continue
		}
		state.revoked = true
		g.leases[id] = state
	}
}

func (g *OpeningGate) Unblock(source string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.sources, source)
	delete(g.sourceLeaseIDs, source)
}

// HoldIfBlocked atomically extends an existing pause. A separate Blocked/Block
// pair could race a resume and cancel orders from a newly resumed session.
func (g *OpeningGate) HoldIfBlocked(source string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.sources) == 0 && (g.admissionCheck == nil || g.admissionCheck()) {
		return false
	}
	if g.sources == nil {
		g.sources = make(map[string]struct{})
	}
	g.sources[source] = struct{}{}
	delete(g.sourceLeaseIDs, source)
	g.revokeUnsubmittedLeases(0)
	return true
}

func (g *OpeningGate) Blocked() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.sources) != 0 || (g.admissionCheck != nil && !g.admissionCheck())
}

func (g *OpeningGate) HasBlock(source string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	_, exists := g.sources[source]
	return exists
}

// Sources returns a stable snapshot of the current owners for diagnostics.
func (g *OpeningGate) Sources() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	sources := make([]string, 0, len(g.sources))
	for source := range g.sources {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	return sources
}

// Begin admits one logical opening order, including its existing-ID retries.
// The caller must release the lease even when submission fails or panics.
func (g *OpeningGate) Begin() (release func(), err error) {
	g.mu.Lock()
	if len(g.sources) != 0 || (g.admissionCheck != nil && !g.admissionCheck()) {
		g.mu.Unlock()
		return nil, ErrOpeningPaused
	}
	lease := g.beginLeaseLocked(false)
	g.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { g.ReleaseLease(lease.ID) }) }, nil
}

// BeginLease acquires an ordinary opening lease. Callers must release it.
func (g *OpeningGate) BeginLease() (OpeningLease, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.sources) != 0 || (g.admissionCheck != nil && !g.admissionCheck()) {
		return OpeningLease{}, ErrOpeningPaused
	}
	return g.beginLeaseLocked(true), nil
}

func (g *OpeningGate) beginLeaseLocked(physical bool) OpeningLease {
	if g.active == 0 {
		g.idle = make(chan struct{})
	}
	g.active++
	g.nextLeaseID++
	if g.leases == nil {
		g.leases = make(map[uint64]openingLeaseState)
	}
	g.leases[g.nextLeaseID] = openingLeaseState{physical: physical}
	return OpeningLease{ID: g.nextLeaseID}
}

// BeginWithEpisodeActionPermit permits exactly one lease through the exact
// persisted source set. Independent or unapproved sources still deny it.
// Durable CAS/readback must be completed by the caller before this method.
func (g *OpeningGate) BeginWithEpisodeActionPermit(permit accounting.OneShotPermit) (OpeningLease, error) {
	if err := permit.Validate(); err != nil {
		return OpeningLease{}, err
	}
	source := RecoveryEpisodeGateSource(permit.EpisodeID, permit.Generation)
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.admissionCheck != nil && !g.admissionCheck() {
		return OpeningLease{}, ErrOpeningPaused
	}
	if !matchesRecoverySources(g.sources, source, permit.GateSourceExceptions, true) {
		return OpeningLease{}, ErrOpeningPaused
	}
	return g.beginRecoveryLeaseLocked(permit, source)
}

// ConsumeEpisodeActionLease performs the final in-memory source/lease check
// after durable permit consumption and immediately before the single RPC.
func (g *OpeningGate) ConsumeEpisodeActionLease(lease OpeningLease, permit accounting.OneShotPermit) error {
	if lease.ID == 0 || lease.ID != permit.LeaseID || lease.PermitID == "" || lease.PermitID != permit.ID || lease.Source != RecoveryEpisodeGateSource(permit.EpisodeID, permit.Generation) || permit.State != accounting.PermitConsumed {
		return ErrOpeningPaused
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	state, ok := g.leases[lease.ID]
	if !ok || state.source != lease.Source || state.permitID != lease.PermitID || state.consumed {
		return ErrOpeningPaused
	}
	if !sameGateSourceExceptions(state.gateSourceExceptions, permit.GateSourceExceptions) || !matchesRecoverySources(g.sources, lease.Source, permit.GateSourceExceptions, true) {
		return ErrOpeningPaused
	}
	if g.admissionCheck != nil && !g.admissionCheck() {
		return ErrOpeningPaused
	}
	state.consumed = true
	g.leases[lease.ID] = state
	return nil
}

// AuthorizeOpeningSubmission is the linearization point immediately before a
// physical opening-order RPC. A concurrent Block either wins first and denies
// this attempt, or observes it as in-flight and Drain waits for its lease.
func (g *OpeningGate) AuthorizeOpeningSubmission(lease OpeningLease) (func(), error) {
	if lease.ID == 0 {
		return nil, ErrOpeningPaused
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	state, ok := g.leases[lease.ID]
	if !ok || !state.physical || state.revoked || state.inFlight {
		return nil, ErrOpeningPaused
	}
	if state.parentID != 0 {
		parent, parentExists := g.leases[state.parentID]
		if !parentExists || parent.physical {
			return nil, ErrOpeningPaused
		}
	}
	if lease.Source == "" {
		if state.source != "" || state.consumed {
			return nil, ErrOpeningPaused
		}
		for source := range g.sources {
			if state.parentID == 0 || g.sourceLeaseIDs[source] != state.parentID {
				return nil, ErrOpeningPaused
			}
		}
	} else if state.source != lease.Source || state.permitID != lease.PermitID || !state.consumed || state.submitted || !matchesRecoverySources(g.sources, lease.Source, state.gateSourceExceptions, true) {
		return nil, ErrOpeningPaused
	}
	if g.admissionCheck != nil && !g.admissionCheck() {
		return nil, ErrOpeningPaused
	}
	state.inFlight = true
	if lease.Source != "" {
		state.submitted = true
	}
	g.leases[lease.ID] = state
	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			defer g.mu.Unlock()
			current, exists := g.leases[lease.ID]
			if !exists {
				return
			}
			current.inFlight = false
			g.leases[lease.ID] = current
		})
	}, nil
}

func (g *OpeningGate) ReleaseLease(id uint64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	_, ok := g.leases[id]
	if !ok {
		return
	}
	delete(g.leases, id)
	for source, operationID := range g.sourceLeaseIDs {
		if operationID == id {
			delete(g.sourceLeaseIDs, source)
		}
	}
	// Keep issued permit IDs as tombstones for this gate lifetime. Releasing an
	// in-memory lease must not make the same authorization reusable.
	g.active--
	if g.active == 0 && g.idle != nil {
		close(g.idle)
	}
}

// Drain waits for already admitted operations. Call after Block, and keep the
// block held while cancelling residual orders. A timeout is not proof of safety.
func (g *OpeningGate) Drain(ctx context.Context) error {
	g.mu.Lock()
	if g.active == 0 {
		g.mu.Unlock()
		return nil
	}
	idle := g.idle
	g.mu.Unlock()
	select {
	case <-idle:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
