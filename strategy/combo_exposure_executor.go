package strategy

import (
	"context"
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

// BatchPlaceOrders signals either a margin error or a local Combo admission rejection.
func (e *comboExposureAdmissionExecutor) BatchPlaceOrders(orders []*position.OrderRequest) ([]*position.Order, bool) {
	result := e.BatchPlaceOrdersWithDetails(orders)
	return result.PlacedOrders, result.HasMarginError || result.HasAdmissionError
}

func (e *comboExposureAdmissionExecutor) BatchPlaceOrdersWithDetails(orders []*position.OrderRequest) *position.BatchPlaceOrdersResult {
	return e.BatchPlaceOrdersWithDetailsContext(context.Background(), orders)
}

func (e *comboExposureAdmissionExecutor) BatchPlaceOrdersWithDetailsContext(ctx context.Context, orders []*position.OrderRequest) *position.BatchPlaceOrdersResult {
	if ctx == nil {
		ctx = context.Background()
	}
	result := &position.BatchPlaceOrdersResult{
		PlacedOrders:     make([]*position.Order, 0),
		AdmissionErrors:  make(map[string]string),
		ReduceOnlyErrors: make(map[string]bool),
		UnknownOrders:    make(map[string]bool),
	}
	classifier, classifierAvailable := e.next.(comboOrderRiskAdapter)
	clientIDCounts := make(map[string]int, len(orders))
	for _, req := range orders {
		if req != nil && req.ClientOrderID != "" {
			clientIDCounts[req.ClientOrderID]++
		}
	}
	included := make([]bool, len(orders))
	openingIndices := make([]int, 0, len(orders))
	openingNotional := 0.0
	for i, req := range orders {
		if req == nil {
			continue
		}
		included[i] = true
		key := comboBatchOrderKey(req, i)
		if req.ClientOrderID != "" && clientIDCounts[req.ClientOrderID] > 1 {
			included[i] = false
			result.AdmissionErrors[key] = "combo batch contains duplicate client order IDs"
			continue
		}
		if !classifierAvailable {
			if req.ReduceOnly {
				continue
			}
			included[i] = false
			result.AdmissionErrors[key] = "combo order risk classifier unavailable"
			continue
		}
		opening, err := classifier.classifyComboOrder(req)
		if err != nil {
			included[i] = false
			result.AdmissionErrors[key] = "combo order admission: " + err.Error()
			continue
		}
		if !opening {
			continue
		}
		if req.ClientOrderID == "" {
			included[i] = false
			result.AdmissionErrors[key] = "combo opening order admission requires a client order ID"
			continue
		}
		notional, err := classifier.estimateComboOrderNotional(req)
		if err != nil || !finiteNumber(notional) || notional <= 0 {
			included[i] = false
			if err != nil {
				result.AdmissionErrors[key] = "combo opening order notional estimate: " + err.Error()
			} else {
				result.AdmissionErrors[key] = "combo opening order notional estimate is invalid"
			}
			continue
		}
		openingIndices = append(openingIndices, i)
		openingNotional += notional
	}

	if len(openingIndices) > 0 {
		e.mu.Lock()
		ready := e.active && e.combo != nil && e.combo.strategyCfg != nil
		capital, maxExposure := 0.0, 0.0
		if ready {
			capital, maxExposure = e.combo.strategyCfg.TotalCapital, e.combo.strategyCfg.MaxExposure
		}
		limit := capital * maxExposure
		projected := e.baseNotional + e.reserved + openingNotional
		reason := ""
		if !ready {
			reason = "combo opening order admission snapshot unavailable"
		} else if !finiteNumber(capital) || capital <= 0 || !finiteNumber(maxExposure) || maxExposure <= 0 ||
			!finiteNumber(limit) || !finiteNumber(projected) || projected >= limit {
			reason = fmt.Sprintf("combo max exposure admission rejected: projected notional %.8g reaches limit %.8g", projected, limit)
		} else {
			e.reserved += openingNotional
		}
		if reason != "" {
			for _, i := range openingIndices {
				included[i] = false
				result.AdmissionErrors[comboBatchOrderKey(orders[i], i)] = reason
			}
		}
		e.mu.Unlock()
	}

	toSubmit := make([]*position.OrderRequest, 0, len(orders))
	for i, req := range orders {
		if included[i] {
			toSubmit = append(toSubmit, req)
		}
	}
	if len(toSubmit) > 0 {
		var submitted *position.BatchPlaceOrdersResult
		if contextual, ok := e.next.(interface {
			BatchPlaceOrdersWithDetailsContext(context.Context, []*position.OrderRequest) *position.BatchPlaceOrdersResult
		}); ok {
			submitted = contextual.BatchPlaceOrdersWithDetailsContext(ctx, toSubmit)
		} else {
			submitted = e.next.BatchPlaceOrdersWithDetails(toSubmit)
		}
		if submitted != nil {
			result.PlacedOrders = submitted.PlacedOrders
			result.HasMarginError = submitted.HasMarginError
			result.ReduceOnlyErrors = submitted.ReduceOnlyErrors
			result.UnknownOrders = submitted.UnknownOrders
			for key, reason := range submitted.AdmissionErrors {
				result.AdmissionErrors[key] = reason
			}
		} else {
			// A missing response does not prove that the venue rejected the batch.
			// Preserve only the requests actually sent, using original batch keys.
			for i, req := range orders {
				if included[i] {
					result.UnknownOrders[comboBatchOrderKey(req, i)] = true
				}
			}
		}
	}
	result.HasAdmissionError = len(result.AdmissionErrors) > 0
	return result
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

func comboBatchOrderKey(req *position.OrderRequest, index int) string {
	if req != nil && req.ClientOrderID != "" {
		return req.ClientOrderID
	}
	return fmt.Sprintf("batch-index:%d", index)
}
