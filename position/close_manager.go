package position

import (
	"context"
	"errors"
	"fmt"
	"quantmesh/config"
	"quantmesh/execution"
	"quantmesh/utils"
	"sync"
	"time"
)

type CloseMethod string

const (
	CloseMethodMarket   CloseMethod = "market"
	CloseMethodLimit    CloseMethod = "limit"
	CloseStatusPending              = "PENDING"
	CloseStatusFilled               = "FILLED"
	CloseStatusTimeout              = "TIMEOUT"
	CloseStatusFailed               = "FAILED"
	CloseStatusCanceled             = "CANCELED"
	CloseStatusUnknown              = "UNKNOWN"
	closePollInterval               = 250 * time.Millisecond
	closeRequestTimeout             = 10 * time.Second
)

// Exported records are immutable snapshots, never shared with polling workers.
type ClosePositionRecord struct {
	RecordID      string      `json:"record_id"`
	BotID         string      `json:"bot_id"`
	Symbol        string      `json:"symbol"`
	Side          string      `json:"side"`
	TargetQty     float64     `json:"target_qty"`
	FilledQty     float64     `json:"filled_qty"`
	Method        CloseMethod `json:"method"`
	Price         float64     `json:"price"`
	OrderID       int64       `json:"order_id"`
	ClientOrderID string      `json:"client_order_id"`
	Status        string      `json:"status"`
	CreatedAt     time.Time   `json:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at"`
	TimeoutAt     time.Time   `json:"timeout_at"`
	RetryCount    int         `json:"retry_count"`
	ErrorMessage  string      `json:"error_message"`
}
type closeOperation struct {
	mu                   sync.Mutex
	record               ClosePositionRecord
	request              ExchangeOrderRequest
	baseFilled, progress float64
	terminal             bool
	done                 chan struct{}
}
type ClosePositionManager struct {
	exchange      ExchangeWrapper
	botID, symbol string
	priceDecimals int
	mu            sync.Mutex
	records       map[string]*closeOperation
	stopped       bool
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	stopOnce      sync.Once
	done          chan struct{}
	pollInterval  time.Duration
}
type ExchangeWrapper interface {
	GetName() string
	PlaceOrder(context.Context, *ExchangeOrderRequest) (*ExchangeOrder, error)
	GetOrder(context.Context, string, int64) (*ExchangeOrder, error)
	CancelOrder(context.Context, string, int64) error
	GetLatestPrice(context.Context, string) (float64, error)
}
type ExchangeOrderRequest struct {
	Symbol, Side, Type, ClientOrderID string
	Quantity, Price                   float64
	ReduceOnly, PostOnly              bool
	TimeInForce                       string
	PriceDecimals                     int
}
type ExchangeOrder struct {
	OrderID                             int64
	ClientOrderID, Symbol, Side, Status string
	Quantity, ExecutedQty, AvgPrice     float64
}
type priceDecimalsProvider interface{ GetPriceDecimals() int }

func NewClosePositionManager(ex ExchangeWrapper, botID, symbol string) *ClosePositionManager {
	ctx, cancel := context.WithCancel(context.Background())
	decimals := -1
	if p, ok := ex.(priceDecimalsProvider); ok {
		decimals = p.GetPriceDecimals()
	}
	return &ClosePositionManager{exchange: ex, botID: botID, symbol: symbol, priceDecimals: decimals,
		records: make(map[string]*closeOperation), ctx: ctx, cancel: cancel, done: make(chan struct{}), pollInterval: closePollInterval}
}
func (cpm *ClosePositionManager) SetPriceDecimals(n int) {
	cpm.mu.Lock()
	defer cpm.mu.Unlock()
	cpm.priceDecimals = n
}
func (op *closeOperation) snapshot() *ClosePositionRecord {
	op.mu.Lock()
	defer op.mu.Unlock()
	copy := op.record
	return &copy
}
func (op *closeOperation) fail(status string, err error) {
	op.mu.Lock()
	defer op.mu.Unlock()
	op.record.Status, op.record.ErrorMessage, op.record.UpdatedAt = status, err.Error(), time.Now()
}
func (cpm *ClosePositionManager) ClosePositions(ctx context.Context, side string, qty float64, cfg config.ClosePositionConfig) (*ClosePositionRecord, error) {
	if err := validateCloseRequest(side, qty, cfg); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cpm.mu.Lock()
	if cpm.stopped {
		cpm.mu.Unlock()
		return nil, fmt.Errorf("close manager stopped")
	}
	for _, prior := range cpm.records {
		s := prior.snapshot().Status
		if s == CloseStatusPending || s == CloseStatusUnknown {
			cpm.mu.Unlock()
			return nil, execution.ErrIntentPending
		}
	}
	decimals := cpm.priceDecimals
	if decimals < 0 {
		decimals = fallbackClosePriceDecimals
	}
	now := time.Now()
	op := &closeOperation{record: ClosePositionRecord{RecordID: "close_" + utils.NewCompactOrderID(), BotID: cpm.botID,
		Symbol: cpm.symbol, Side: side, TargetQty: qty, Method: CloseMethod(cfg.Method), Status: CloseStatusPending, CreatedAt: now, UpdatedAt: now}, done: make(chan struct{})}
	if cfg.TimeoutSec > 0 {
		op.record.TimeoutAt = now.Add(time.Duration(cfg.TimeoutSec) * time.Second)
	}
	op.request = ExchangeOrderRequest{Symbol: cpm.symbol, Side: side, Quantity: qty, ReduceOnly: true,
		ClientOrderID: utils.NewCompactOrderID(), PriceDecimals: decimals, Type: "MARKET"}
	op.record.ClientOrderID = op.request.ClientOrderID
	cpm.records[op.record.RecordID] = op
	cpm.wg.Add(1)
	cpm.mu.Unlock()
	callCtx, cancel := context.WithTimeout(ctx, closeRequestTimeout)
	stopCancel := context.AfterFunc(cpm.ctx, cancel)
	defer func() { stopCancel(); cancel() }()
	if cfg.Method == string(CloseMethodLimit) {
		if err := cpm.prepareCloseLimit(callCtx, op, cfg); err != nil {
			op.fail(CloseStatusFailed, err)
			close(op.done)
			cpm.wg.Done()
			return op.snapshot(), err
		}
	}
	if err := cpm.submitClose(callCtx, op); err != nil {
		close(op.done)
		cpm.wg.Done()
		return op.snapshot(), err
	}
	// Accepted operations belong to the runtime rather than an HTTP connection.
	// Preserve coordinator context values; Stop still cancels all future calls.
	workerCtx, workerCancel := context.WithCancel(context.WithoutCancel(ctx))
	stopWorker := context.AfterFunc(cpm.ctx, workerCancel)
	go func() {
		defer cpm.wg.Done()
		defer close(op.done)
		defer workerCancel()
		defer stopWorker()
		cpm.watchClose(workerCtx, op, cfg)
	}()
	return op.snapshot(), nil
}
func (cpm *ClosePositionManager) prepareCloseLimit(ctx context.Context, op *closeOperation, cfg config.ClosePositionConfig) error {
	price, err := cpm.exchange.GetLatestPrice(ctx, cpm.symbol)
	if err != nil {
		return err
	}
	if !positiveFinite(price) {
		return fmt.Errorf("invalid close reference price")
	}
	op.mu.Lock()
	defer op.mu.Unlock()
	price = utils.RoundToDecimals(calculateLimitPrice(price, op.record.Side, cfg.PriceOffset), op.request.PriceDecimals)
	if !positiveFinite(price) {
		return fmt.Errorf("invalid rounded close price")
	}
	op.request.Type, op.request.Price, op.request.TimeInForce, op.record.Price = "LIMIT", price, "GTC", price
	return nil
}
func (cpm *ClosePositionManager) GetRecord(id string) (*ClosePositionRecord, bool) {
	cpm.mu.Lock()
	op, ok := cpm.records[id]
	cpm.mu.Unlock()
	if !ok {
		return nil, false
	}
	return op.snapshot(), true
}
func (cpm *ClosePositionManager) ListRecords() []*ClosePositionRecord {
	cpm.mu.Lock()
	defer cpm.mu.Unlock()
	out := make([]*ClosePositionRecord, 0, len(cpm.records))
	for _, op := range cpm.records {
		out = append(out, op.snapshot())
	}
	return out
}
func (cpm *ClosePositionManager) WaitRecord(ctx context.Context, id string) (*ClosePositionRecord, error) {
	cpm.mu.Lock()
	op := cpm.records[id]
	cpm.mu.Unlock()
	if op == nil {
		return nil, fmt.Errorf("unknown close record")
	}
	select {
	case <-ctx.Done():
		return op.snapshot(), ctx.Err()
	case <-op.done:
	}
	snapshot := op.snapshot()
	if snapshot.Status != CloseStatusFilled {
		return snapshot, fmt.Errorf("close not filled: %s: %s", snapshot.Status, snapshot.ErrorMessage)
	}
	return snapshot, nil
}
func (cpm *ClosePositionManager) StopContext(ctx context.Context) error {
	cpm.stopOnce.Do(func() {
		cpm.mu.Lock()
		cpm.stopped = true
		cpm.cancel()
		cpm.mu.Unlock()
		go func() { cpm.wg.Wait(); close(cpm.done) }()
	})
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-cpm.done:
		return nil
	}
}
func (cpm *ClosePositionManager) Stop() { _ = cpm.StopContext(context.Background()) }
func (cpm *ClosePositionManager) submitClose(ctx context.Context, op *closeOperation) error {
	op.mu.Lock()
	req := op.request
	op.mu.Unlock()
	ord, err := cpm.exchange.PlaceOrder(ctx, &req)
	if err != nil {
		status := CloseStatusFailed
		if ord != nil || errors.Is(err, execution.ErrOrderUnknown) {
			status = CloseStatusUnknown
		}
		if ord != nil {
			_ = cpm.observeClose(op, ord)
		}
		op.fail(status, err)
		return err
	}
	if err := cpm.observeClose(op, ord); err != nil {
		op.fail(CloseStatusUnknown, err)
		return err
	}
	return nil
}
