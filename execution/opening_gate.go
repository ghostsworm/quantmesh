// Package execution contains execution controls shared by grid and signal strategies.
package execution

import (
	"context"
	"errors"
	"sort"
	"sync"
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
	idle           chan struct{}
	admissionCheck func() bool
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
}

func (g *OpeningGate) Unblock(source string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.sources, source)
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
	defer g.mu.Unlock()
	if len(g.sources) != 0 || (g.admissionCheck != nil && !g.admissionCheck()) {
		return nil, ErrOpeningPaused
	}
	if g.active == 0 {
		g.idle = make(chan struct{})
	}
	g.active++
	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			defer g.mu.Unlock()
			g.active--
			if g.active == 0 {
				close(g.idle)
			}
		})
	}, nil
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
