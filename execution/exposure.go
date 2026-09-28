package execution

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"sync"
	"time"
)

var (
	ErrExposureLimit      = errors.New("opening exposure limit exceeded")
	ErrExposureUnverified = errors.New("opening exposure requires reconciliation")
)

// Limits apply to gross positions PLUS all unconfirmed opening remainder.
// Notional is an admission ceiling at max(current mark, pending limit price),
// not a promise that subsequent market moves cannot increase existing exposure.
type ExposureLimits struct {
	Quantity float64 `json:"quantity"`
	Notional float64 `json:"notional"`
	Layers   int     `json:"layers"`
}

type ExposurePosition struct {
	Key, Group, Leg string
	Quantity        float64
}

type ExposureRequest struct {
	ID, Group, Lot, Leg string
	Opening             bool
	BotWideClose        bool // explicit owner-scoped manual close, never an opening
	Quantity, Price     float64
}

type ExposureUpdate struct {
	CumulativeQty, OrderQty, Price float64
	Status                         string
}

type ExposureSnapshot struct {
	Ready             bool           `json:"ready"`
	Reason            string         `json:"reason,omitempty"`
	ReasonCode        string         `json:"reason_code"`
	OpeningAvailable  bool           `json:"opening_available"`
	NewLotAvailable   bool           `json:"new_lot_available"`
	PositionQuantity  float64        `json:"position_quantity"`
	PendingQuantity   float64        `json:"pending_quantity"`
	ProjectedQuantity float64        `json:"projected_quantity"`
	ProjectedNotional float64        `json:"projected_notional"`
	Layers            int            `json:"layers"`
	Limits            ExposureLimits `json:"limits"`
	Mark              float64        `json:"mark"`
	MarkAt            time.Time      `json:"mark_at"`
}

type exposureLot struct {
	group, leg string
	quantity   *big.Rat
}

type exposureIntent struct {
	request       ExposureRequest
	filled        *big.Rat
	venueQuantity float64 // independently reported original quantity, never inferred from fills
	terminal      bool
	unknown       bool
	closeLots     map[string]*big.Rat // remaining inventory reserved for this close
}

// ExposureBook is a per-owner admission ledger. All strategies of that owner
// must share ONE book. Wiring/restoration must complete before opening begins.
// No method infers flatness from process restart or from a cancel ACK.
type ExposureBook struct {
	mu             sync.Mutex
	limits         ExposureLimits
	ready          bool
	initialized    bool
	reason         string
	mark           float64
	markAt         time.Time
	markValid      bool
	markEvidenceAt time.Time
	maxMarkAge     time.Duration
	lots           map[string]*exposureLot
	lotOrder       []string
	intents        map[string]*exposureIntent
}

func NewExposureBook(limits ExposureLimits, maxMarkAge time.Duration) (*ExposureBook, error) {
	if err := validateExposureLimits(limits); err != nil {
		return nil, err
	}
	if maxMarkAge <= 0 {
		return nil, fmt.Errorf("positive exposure mark age required")
	}
	return &ExposureBook{limits: limits, maxMarkAge: maxMarkAge, lots: make(map[string]*exposureLot), intents: make(map[string]*exposureIntent)}, nil
}

func exposureNumber(value float64) (*big.Rat, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return nil, fmt.Errorf("invalid exposure number")
	}
	n, ok := new(big.Rat).SetString(strconv.FormatFloat(value, 'f', -1, 64))
	if !ok {
		return nil, fmt.Errorf("invalid exposure decimal")
	}
	return n, nil
}

func validateExposureLimits(limits ExposureLimits) error {
	if _, err := exposureNumber(limits.Quantity); err != nil {
		return err
	}
	if _, err := exposureNumber(limits.Notional); err != nil {
		return err
	}
	if limits.Layers < 0 {
		return fmt.Errorf("negative exposure layer limit")
	}
	return nil
}

func (b *ExposureBook) SetLimits(limits ExposureLimits) error {
	if err := validateExposureLimits(limits); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.limits = limits // lowering limits never discards live positions/reservations
	return nil
}

// OverLimits includes filled inventory and every unconfirmed opening remainder.
func (b *ExposureBook) OverLimits() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return errors.Is(b.checkLimitsLocked(nil), ErrExposureLimit)
}

func (b *ExposureBook) SetMark(price float64, at time.Time) error {
	if _, err := exposureNumber(price); err != nil || price == 0 || at.IsZero() {
		return fmt.Errorf("invalid exposure mark")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if at.Before(b.markAt) {
		return fmt.Errorf("exposure mark regressed")
	}
	b.mark, b.markAt = price, at
	b.markValid = true
	b.markEvidenceAt = at
	return nil
}

// Seed is a one-time initialization from a reconciled inventory. It is not a
// reset API: existing reservations/history must never be erased to free quota.
// Restoring open/UNKNOWN orders requires the future durable-intent import path;
// callers may not declare Ready until that reconciliation is complete.
func (b *ExposureBook) Seed(positions []ExposurePosition) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.initialized || len(b.intents) != 0 {
		return fmt.Errorf("exposure book already in use")
	}
	lots := make(map[string]*exposureLot, len(positions))
	var order []string
	for _, position := range positions {
		qty, err := exposureNumber(position.Quantity)
		if err != nil || position.Quantity <= 0 || position.Key == "" || position.Group == "" || !exposureLeg(position.Leg) {
			return fmt.Errorf("invalid restored exposure position")
		}
		if _, exists := lots[position.Key]; exists {
			return fmt.Errorf("duplicate restored exposure lot")
		}
		lots[position.Key] = &exposureLot{group: position.Group, leg: position.Leg, quantity: qty}
		order = append(order, position.Key)
	}
	b.lots, b.lotOrder, b.ready = lots, order, true
	b.initialized = true
	return nil
}

func exposureLeg(leg string) bool { return leg == "LONG" || leg == "SHORT" }

func (b *ExposureBook) failLocked(reason string) error {
	if b.reason == "" {
		b.reason = reason
	}
	return fmt.Errorf("%s: %w", reason, ErrExposureUnverified)
}

func (b *ExposureBook) ReadyError(now time.Time) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.readyErrorLocked(now)
}

func (b *ExposureBook) readyErrorLocked(now time.Time) error {
	if !b.ready || b.reason != "" {
		return fmt.Errorf("inventory or order state unverified: %w", ErrExposureUnverified)
	}
	if !b.markValid || b.mark <= 0 || b.markAt.IsZero() || b.markAt.After(now) || now.Sub(b.markAt) > b.maxMarkAge {
		return fmt.Errorf("exposure mark unavailable or stale: %w", ErrExposureUnverified)
	}
	return nil
}

func (b *ExposureBook) Reserve(req ExposureRequest, now time.Time) error {
	if req.BotWideClose && (req.Opening || req.Lot != "") {
		return fmt.Errorf("bot-wide close cannot open or specify a strategy lot")
	}
	qty, err := exposureNumber(req.Quantity)
	if err != nil || qty.Sign() <= 0 || req.ID == "" || req.Group == "" || !exposureLeg(req.Leg) {
		return fmt.Errorf("invalid exposure request")
	}
	if _, err := exposureNumber(req.Price); err != nil || (req.Opening && req.Price <= 0) {
		return fmt.Errorf("opening exposure requires bounded price")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, exists := b.intents[req.ID]; exists {
		return fmt.Errorf("exposure intent already exists: %w", ErrIntentPending)
	}
	if req.Opening {
		if err := b.readyErrorLocked(now); err != nil {
			return err
		}
		if req.Lot == "" {
			req.Lot = req.ID
		}
		if lot := b.lots[req.Lot]; lot != nil && (lot.group != req.Group || lot.leg != req.Leg) {
			return fmt.Errorf("exposure lot ownership conflict")
		}
		if err := b.checkLimitsLocked(&req); err != nil {
			return err
		}
		if b.lots[req.Lot] == nil {
			b.lots[req.Lot] = &exposureLot{group: req.Group, leg: req.Leg, quantity: new(big.Rat)}
			b.lotOrder = append(b.lotOrder, req.Lot)
		}
	}
	intent := &exposureIntent{request: req, filled: new(big.Rat)}
	if !req.Opening {
		allocation, err := b.reserveCloseLocked(req, qty)
		if err != nil {
			return err
		}
		intent.closeLots = allocation
	}
	b.intents[req.ID] = intent
	return nil
}

func (b *ExposureBook) Reject(id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	i := b.intents[id]
	if i == nil {
		return nil
	}
	if i.unknown || i.filled.Sign() > 0 || i.terminal {
		return b.failLocked("refusal conflicts with observed exposure")
	}
	delete(b.intents, id)
	return nil
}

func (b *ExposureBook) MarkUnknown(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if i := b.intents[id]; i != nil {
		i.unknown = true
	}
	_ = b.failLocked("order outcome unknown")
}

// Reprice is called before every physical attempt. A refused SELL PostOnly
// order cannot increase its limit price beyond the reserved notional ceiling.
func (b *ExposureBook) Reprice(id string, price float64, now time.Time) error {
	if _, err := exposureNumber(price); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	i := b.intents[id]
	if i == nil {
		return fmt.Errorf("exposure intent missing")
	}
	if i.unknown || i.terminal || i.filled.Sign() > 0 {
		return b.failLocked("observed intent cannot be submitted again")
	}
	if !i.request.Opening {
		return nil
	}
	if err := b.readyErrorLocked(now); err != nil {
		return err
	}
	if price <= 0 {
		return fmt.Errorf("opening price is unbounded")
	}
	old := i.request.Price
	i.request.Price = math.Max(old, price)
	if err := b.checkLimitsLocked(nil); err != nil {
		i.request.Price = old
		return err
	}
	return nil
}
