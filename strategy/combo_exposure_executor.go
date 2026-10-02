package strategy

import (
	"fmt"
	"sync"

	"quantmesh/position"
)

type comboOrderRiskAdapter interface {
	classifyComboOrder(*position.OrderRequest) (bool, error)
	estimateComboOrderNotional(*position.OrderRequest) (float64, error)
}

// comboExposureAdmissionExecutor reserves opening notional for the duration of
// one child callback. Child strategies may hold their own mutex while submitting,
// so admission uses the pre-callback risk snapshot instead of calling GetOrders.
type comboExposureAdmissionExecutor struct {
	combo *ComboStrategy
	next  position.OrderExecutorInterface

	mu           sync.Mutex
	active       bool
	baseNotional float64
	reserved     float64
}

func (e *comboExposureAdmissionExecutor) beginChildAdmission() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.combo.mu.RLock()
	e.baseNotional = e.combo.riskExposureNotional
	ready := e.combo.riskExposureReady
	e.combo.mu.RUnlock()
	e.reserved = 0
	e.active = ready
}

func (e *comboExposureAdmissionExecutor) endChildAdmission() {
	e.mu.Lock()
	e.active = false
	e.mu.Unlock()
}

func (e *comboExposureAdmissionExecutor) PlaceOrder(req *position.OrderRequest) (*position.Order, error) {
	if req == nil {
		return nil, fmt.Errorf("combo order request is nil")
	}
	classifier, ok := e.next.(comboOrderRiskAdapter)
	if !ok {
		return nil, fmt.Errorf("combo order risk classifier unavailable")
	}
	opening, err := classifier.classifyComboOrder(req)
	if err != nil {
		return nil, fmt.Errorf("combo order admission: %w", err)
	}
	if !opening {
		return e.next.PlaceOrder(req)
	}
	if req.ClientOrderID == "" {
		return nil, fmt.Errorf("combo opening order admission requires a client order ID")
	}
	estimator, ok := e.next.(comboOrderRiskAdapter)
	if !ok {
		return nil, fmt.Errorf("combo order notional estimator unavailable")
	}
	notional, err := estimator.estimateComboOrderNotional(req)
	if err != nil || !finiteNumber(notional) || notional <= 0 {
		if err != nil {
			return nil, fmt.Errorf("combo opening order notional estimate: %w", err)
		}
		return nil, fmt.Errorf("combo opening order notional estimate is invalid")
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.active || e.combo == nil || e.combo.strategyCfg == nil {
		return nil, fmt.Errorf("combo opening order admission snapshot unavailable")
	}
	capital, maxExposure := e.combo.strategyCfg.TotalCapital, e.combo.strategyCfg.MaxExposure
	limit := capital * maxExposure
	projected := e.baseNotional + e.reserved + notional
	if !finiteNumber(capital) || capital <= 0 || !finiteNumber(maxExposure) || maxExposure <= 0 ||
		!finiteNumber(limit) || !finiteNumber(projected) || projected >= limit {
		return nil, fmt.Errorf("combo max exposure admission rejected: projected notional %.8g reaches limit %.8g", projected, limit)
	}
	// Retain the reservation until this child callback completes, including when
	// the exchange submission outcome is unknown.
	e.reserved += notional
	return e.next.PlaceOrder(req)
}

func (e *comboExposureAdmissionExecutor) BatchPlaceOrders(orders []*position.OrderRequest) ([]*position.Order, bool) {
	return e.next.BatchPlaceOrders(orders)
}

func (e *comboExposureAdmissionExecutor) BatchPlaceOrdersWithDetails(orders []*position.OrderRequest) *position.BatchPlaceOrdersResult {
	return e.next.BatchPlaceOrdersWithDetails(orders)
}

func (e *comboExposureAdmissionExecutor) BatchCancelOrders(orderIDs []int64) error {
	return e.next.BatchCancelOrders(orderIDs)
}

func (e *comboExposureAdmissionExecutor) IsOpeningPaused() bool {
	if gate, ok := e.next.(interface{ IsOpeningPaused() bool }); ok {
		return gate.IsOpeningPaused()
	}
	return false
}

func (e *comboExposureAdmissionExecutor) MarkOrderReconciliationRequired(orderID int64, clientOrderID, reason string) error {
	tracker, ok := e.next.(interface {
		MarkOrderReconciliationRequired(int64, string, string) error
	})
	if !ok {
		return fmt.Errorf("order reconciliation tracker unavailable")
	}
	return tracker.MarkOrderReconciliationRequired(orderID, clientOrderID, reason)
}

var _ position.OrderExecutorInterface = (*comboExposureAdmissionExecutor)(nil)
