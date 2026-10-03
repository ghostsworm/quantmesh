package strategy

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/lock"
	"quantmesh/logger"
	"quantmesh/position"
)

// CarryDirection 套利方向
type CarryDirection int

const (
	DirectionNone                CarryDirection = 0
	DirectionForward             CarryDirection = 1 // 現貨多 + 合約空（正費率收錢）
	DirectionReverse             CarryDirection = 2 // 借幣空現貨 + 合約多（負費率收錢）
	fundingCarrySafetyBufferUSDT                = 50.0
)

func (d CarryDirection) String() string {
	switch d {
	case DirectionForward:
		return "forward"
	case DirectionReverse:
		return "reverse"
	default:
		return "none"
	}
}

// FundingCarryStrategy 資金費率期現套利
type FundingCarryStrategy struct {
	name   string
	cfg    *config.Config
	symCfg config.SymbolConfig
	fut    exchange.IExchange
	spot   exchange.IExchange
	symbol string

	// 正向參數
	minFundingRate  float64
	exitFundingRate float64
	maxBasisPct     float64
	tickInterval    time.Duration

	// 反向套利參數
	marginEx           exchange.ISpotMarginExchange // nullable
	reverseEnabled     bool
	reverseMinRate     float64 // 負費率絕對值觸發閾值（正值，如 0.0004）
	reverseExitRate    float64 // 負費率退出閾值（正值）
	marginInterestMax  float64 // 日利率上限
	maxFeeRecoveryDays float64 // 往返手續費最長回收天數

	// 結算時間感知
	nextSettlement   time.Time
	lastSettlement   time.Time
	settlementBuffer time.Duration
	settledThisTick  bool

	// 資金自動劃轉
	autoTransferEnabled           bool
	transferReserveSpot           float64
	profitHarvestEnabled          bool
	profitHarvestMin              float64
	accountFuturesCapitalReserve  float64
	externalFuturesCapitalReserve float64
	externalSpotCapitalReserve    float64
	accountWalletLock             lock.DistributedLock
	accountWalletLockKey          string

	mu                sync.RWMutex
	ctx               context.Context
	cancel            context.CancelFunc
	runDone           chan struct{}
	started           bool
	startupMu         sync.Mutex
	stopMu            sync.Mutex
	stopAttempted     bool
	stopCompleted     bool
	stopErr           error
	eventBus          EventBus
	runtimeStateStore RuntimeStateStore
	runtimeStateErr   error
	intentInFlight    bool
	futuresExecutor   FundingCarryExecutor
	spotExecutor      FundingCarryExecutor
	marginExecutor    FundingCarryExecutor
	openingBlocker    func(string)
	openingGate       *execution.OpeningGate
	openControl       config.OpenPositionControl
	periodicOpen      bool
	periodicSwitch    time.Time
	lastScheduleRun   map[string]string
	operationGate     chan struct{}

	// 持倉狀態（每次 tick 從交易所同步）
	direction              CarryDirection
	spotQty                float64 // 正向：策略自身的現貨腿數量（= min(記賬值, 現貨餘額)）; 反向：0
	futQty                 float64 // 正向：合約空頭 size; 反向：合約多頭 size
	marginDebt             float64 // 反向：借幣數量
	marginBorrowTransferID int64
	marginBorrowedAt       time.Time
	marginDebtEvents       []fundingCarryMarginDebtEvent
	marginRepayIntent      *fundingCarryRepayIntent
	marginCoverOrders      []fundingCarryCoverOrder
	marginAccountScope     string

	// 策略自身買入的現貨數量（僅內存記賬，不含用戶原有持幣）。
	// 策略目前沒有狀態持久化；重啟後首次同步時保守地以 min(合約空頭, 現貨餘額) 重新推導。
	strategySpotQty   float64
	strategySpotKnown bool
	unownedExposure   bool

	consecutiveErrors int
}

const (
	maxConsecutiveErrors        = 5
	orderWaitTimeout            = 30 * time.Second
	orderPollInterval           = 2 * time.Second
	orderCancelVerifyTimeout    = 10 * time.Second
	maxCarryOpenSlippage        = 0.003 // Maximum adverse futures entry-price deviation included in the carry cost gate.
	maxCarrySpotOpenSlippage    = 0.005 // Maximum adverse spot entry-price deviation included in the carry cost gate.
	defaultCarryFeeRecoveryDays = 30.0
	fundingCarryOrderBookDepth  = 50

	// closeSpotSellPriceFactor 平倉現貨限價賣單相對最新價的折讓（保證盡快成交）
	closeSpotSellPriceFactor = 0.99

	// roundQtyEpsilon 數量向下取整時的浮點容差（以最小精度單位計）
	roundQtyEpsilon = 1e-9
)

// NewFundingCarryStrategy 建立策略
func NewFundingCarryStrategy(
	name string,
	cfg *config.Config,
	symCfg config.SymbolConfig,
	fut exchange.IExchange,
	spot exchange.IExchange,
	marginEx exchange.ISpotMarginExchange,
	stratCfg map[string]interface{},
) *FundingCarryStrategy {
	minR := 0.0004
	exitR := 0.0002
	maxBasis := 0.5
	intervalSec := 45
	settleBuf := 5 * time.Minute

	reverseEnabled := false
	reverseMinRate := 0.0004
	reverseExitRate := 0.0002
	marginInterestMax := 0.001
	maxFeeRecoveryDays := defaultCarryFeeRecoveryDays

	autoTransfer := false
	reserveSpot := 50.0
	harvestEnabled := false
	harvestMin := 5.0

	if stratCfg != nil {
		if v, ok := stratCfg["min_funding_rate"].(float64); ok && v > 0 {
			minR = v
		}
		if v, ok := stratCfg["exit_funding_rate"].(float64); ok && v > 0 {
			exitR = v
		}
		if v, ok := stratCfg["max_basis_pct"].(float64); ok && v > 0 {
			maxBasis = v
		}
		if v, ok := stratCfg["rebalance_interval_sec"].(float64); ok && v >= 10 {
			intervalSec = int(v)
		}
		if v, ok := stratCfg["settlement_buffer_min"].(float64); ok && v >= 1 {
			settleBuf = time.Duration(v) * time.Minute
		}
		if v, ok := stratCfg["reverse_enabled"].(bool); ok {
			reverseEnabled = v
		}
		if v, ok := stratCfg["reverse_min_funding_rate"].(float64); ok && v > 0 {
			reverseMinRate = v
		}
		if v, ok := stratCfg["reverse_exit_funding_rate"].(float64); ok && v > 0 {
			reverseExitRate = v
		}
		if v, ok := stratCfg["margin_interest_max"].(float64); ok && finitePositive(v) {
			marginInterestMax = v
		}
		if v, ok := stratCfg["max_fee_recovery_days"].(float64); ok && finitePositive(v) && v <= 365 {
			maxFeeRecoveryDays = v
		}
		if v, ok := stratCfg["auto_transfer_enabled"].(bool); ok {
			autoTransfer = v
		}
		if v, ok := stratCfg["transfer_reserve_spot"].(float64); ok && v >= 0 {
			reserveSpot = v
		}
		if v, ok := stratCfg["profit_harvest_enabled"].(bool); ok {
			harvestEnabled = v
		}
		if v, ok := stratCfg["profit_harvest_min"].(float64); ok && v > 0 {
			harvestMin = v
		}
	}

	if marginEx == nil {
		reverseEnabled = false
	}

	operationGate := make(chan struct{}, 1)
	operationGate <- struct{}{}
	return &FundingCarryStrategy{
		name:                 name,
		cfg:                  cfg,
		symCfg:               symCfg,
		fut:                  fut,
		spot:                 spot,
		symbol:               symCfg.Symbol,
		minFundingRate:       minR,
		exitFundingRate:      exitR,
		maxBasisPct:          maxBasis,
		tickInterval:         time.Duration(intervalSec) * time.Second,
		marginEx:             marginEx,
		reverseEnabled:       reverseEnabled,
		reverseMinRate:       reverseMinRate,
		reverseExitRate:      reverseExitRate,
		marginInterestMax:    marginInterestMax,
		maxFeeRecoveryDays:   maxFeeRecoveryDays,
		settlementBuffer:     settleBuf,
		autoTransferEnabled:  autoTransfer,
		transferReserveSpot:  reserveSpot,
		profitHarvestEnabled: harvestEnabled,
		profitHarvestMin:     harvestMin,
		openControl:          config.CloneOpenPositionControl(symCfg.OpenPositionControl),
		periodicOpen:         true,
		lastScheduleRun:      make(map[string]string),
		operationGate:        operationGate,
	}
}

func (s *FundingCarryStrategy) Name() string { return s.name }

func (s *FundingCarryStrategy) Initialize(*config.Config, position.OrderExecutorInterface, position.IExchange) error {
	return nil
}

func (s *FundingCarryStrategy) SetEventBus(bus EventBus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.eventBus = bus
}

func (s *FundingCarryStrategy) SetRuntimeStateStore(store RuntimeStateStore) {
	s.mu.Lock()
	s.runtimeStateStore = store
	s.mu.Unlock()
}

func (s *FundingCarryStrategy) SetMarginAccountScope(accountScope string) error {
	accountScope = strings.TrimSpace(accountScope)
	if accountScope == "" {
		return fmt.Errorf("funding_carry margin account scope is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return fmt.Errorf("cannot change funding_carry margin account scope after start")
	}
	s.marginAccountScope = accountScope
	return nil
}

// SetAccountCapitalReserves wires the startup-verified cross-Bot wallet budgets.
// The external reserves are protected during transfers; the total futures
// commitment is also protected from profit harvesting.
func (s *FundingCarryStrategy) SetAccountCapitalReserves(futuresTotal, futuresExternal, spotTotal, spotExternal float64) error {
	if !finiteNonNegative(futuresTotal) || !finiteNonNegative(futuresExternal) ||
		!finiteNonNegative(spotTotal) || !finiteNonNegative(spotExternal) ||
		futuresExternal > futuresTotal || spotExternal > spotTotal {
		return fmt.Errorf("account capital reserves must be finite and non-negative")
	}
	s.mu.Lock()
	s.accountFuturesCapitalReserve = futuresTotal
	s.externalFuturesCapitalReserve = futuresExternal
	s.externalSpotCapitalReserve = spotExternal
	s.mu.Unlock()
	return nil
}

// SetAccountWalletCoordinationLock serializes funding_carry wallet mutations
// across Bots sharing the same account. The lock covers both hedge legs and
// harvest transfers; it does not coordinate unrelated external account users.
func (s *FundingCarryStrategy) SetAccountWalletCoordinationLock(coordinator lock.DistributedLock, key string) error {
	if coordinator == nil || strings.TrimSpace(key) == "" {
		return fmt.Errorf("account wallet coordinator and key are required")
	}
	s.mu.Lock()
	s.accountWalletLock = coordinator
	s.accountWalletLockKey = strings.TrimSpace(key)
	s.mu.Unlock()
	return nil
}

type FundingCarryExecutor interface {
	PlaceOrderContext(context.Context, *exchange.OrderRequest) (*exchange.Order, error)
	CancelOrderContext(context.Context, int64) error
	SettleIntent(context.Context, string) error
}

func (s *FundingCarryStrategy) SetOrderExecutors(futures, spot, margin FundingCarryExecutor) {
	s.mu.Lock()
	s.futuresExecutor, s.spotExecutor, s.marginExecutor = futures, spot, margin
	s.mu.Unlock()
}

func (s *FundingCarryStrategy) SetOpeningBlocker(blocker func(string)) {
	s.mu.Lock()
	s.openingBlocker = blocker
	s.mu.Unlock()
}

func (s *FundingCarryStrategy) SetOpeningGate(gate *execution.OpeningGate) {
	s.mu.Lock()
	s.openingGate = gate
	s.mu.Unlock()
}

func (s *FundingCarryStrategy) UpdateOpenPositionControl(control config.OpenPositionControl) {
	s.mu.Lock()
	gate := s.openingGate
	scheduleChanged := !reflect.DeepEqual(control.ScheduleRules, s.openControl.ScheduleRules)
	periodicChanged := !reflect.DeepEqual(control.PeriodicRule, s.openControl.PeriodicRule)
	s.openControl = config.CloneOpenPositionControl(control)
	if periodicChanged {
		s.periodicOpen = true
		s.periodicSwitch = time.Time{}
	}
	if scheduleChanged {
		s.lastScheduleRun = make(map[string]string)
	}
	s.mu.Unlock()
	if gate != nil {
		if scheduleChanged {
			gate.Unblock("schedule")
		}
		if periodicChanged {
			gate.Unblock("periodic")
		}
	}
}

func (s *FundingCarryStrategy) OpenPositionControl() config.OpenPositionControl {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return config.CloneOpenPositionControl(s.openControl)
}

func (s *FundingCarryStrategy) MarkExecutionUnknown(reason error) {
	if reason == nil {
		reason = errors.New("execution result is unknown")
	}
	s.blockOnUnownedExposure(fmt.Errorf("shared order executor reported an unresolved execution: %w", reason))
}

func (s *FundingCarryStrategy) placeOrder(ctx context.Context, ex exchange.IExchange, executor FundingCarryExecutor, request *exchange.OrderRequest) (*exchange.Order, error) {
	if executor == nil {
		return ex.PlaceOrder(ctx, request)
	}
	return executor.PlaceOrderContext(ctx, request)
}

func cancelCarryOrder(ctx context.Context, ex exchange.IExchange, executor FundingCarryExecutor, symbol string, orderID int64) error {
	if executor != nil {
		return executor.CancelOrderContext(ctx, orderID)
	}
	return ex.CancelOrder(ctx, symbol, orderID)
}

func (s *FundingCarryStrategy) executorFor(ex exchange.IExchange) FundingCarryExecutor {
	switch strings.ToLower(strings.TrimSpace(ex.GetMarketType())) {
	case "futures", "future", "usdm", "swap":
		return s.futuresExecutor
	case "spot_margin":
		return s.marginExecutor
	default:
		return s.spotExecutor
	}
}

func settleCarryOrder(ctx context.Context, executor FundingCarryExecutor, order *exchange.Order) error {
	if executor == nil {
		return nil
	}
	if order == nil || order.ClientOrderID == "" {
		return errors.New("executor order acknowledgement has no client order id")
	}
	return executor.SettleIntent(ctx, order.ClientOrderID)
}

func (s *FundingCarryStrategy) Start(ctx context.Context) error {
	s.startupMu.Lock()
	defer s.startupMu.Unlock()
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return errors.New("funding_carry strategy already started")
	}
	s.mu.Unlock()
	if err := s.reconcileSavedMarginRepayment(ctx); err != nil {
		return fmt.Errorf("funding_carry pending repayment recovery: %w", err)
	}
	if err := s.restoreRuntimeState(); err != nil {
		return fmt.Errorf("funding_carry runtime state recovery failed: %w", err)
	}
	checkCtx, checkCancel := context.WithTimeout(ctx, 15*time.Second)
	defer checkCancel()
	s.mu.RLock()
	stateKnown := s.strategySpotKnown
	s.mu.RUnlock()
	if !stateKnown {
		if err := s.requireCleanStart(checkCtx); err != nil {
			return fmt.Errorf("funding_carry refuses startup until existing futures/margin exposure is reconciled: %w", err)
		}
		s.mu.Lock()
		s.strategySpotKnown = true
		s.strategySpotQty = 0
		s.direction = DirectionNone
		s.futQty = 0
		s.marginDebt = 0
		if err := s.persistRuntimeStateLocked(); err != nil {
			s.mu.Unlock()
			return fmt.Errorf("persist initial funding_carry ownership state: %w", err)
		}
		s.mu.Unlock()
	} else if err := s.syncPositions(checkCtx); err != nil {
		return fmt.Errorf("funding_carry restored state does not match exchange: %w", err)
	} else if err := s.requireNoOpenOrders(checkCtx); err != nil {
		return fmt.Errorf("funding_carry restored state has unresolved orders: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return errors.New("funding_carry strategy already started")
	}
	s.started = true
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.runDone = make(chan struct{})
	go s.runLoop()
	logger.Info("✅ [%s] 資金費套利策略已啟動 (min=%.5f exit=%.5f reverse=%v)", s.symbol, s.minFundingRate, s.exitFundingRate, s.reverseEnabled)
	return nil
}

func (s *FundingCarryStrategy) requireCleanStart(ctx context.Context) error {
	positions, err := readScopedPositionSnapshot(ctx, s.fut, s.symbol)
	if err != nil {
		return fmt.Errorf("read futures positions: %w", err)
	}
	if positions == nil {
		return errors.New("futures position snapshot is nil, not an authoritative empty snapshot")
	}
	for _, p := range positions {
		if p == nil || math.IsNaN(p.Size) || math.IsInf(p.Size, 0) {
			return errors.New("futures position snapshot contains invalid data")
		}
		if p.Size != 0 {
			return fmt.Errorf("futures position exists (size=%.8f)", p.Size)
		}
	}
	for name, ex := range map[string]exchange.IExchange{"futures": s.fut, "spot": s.spot} {
		if err := requireAccountHasNoOpenOrders(ctx, ex, name); err != nil {
			return err
		}
	}
	if s.marginEx != nil {
		marginPositions, err := readScopedPositionSnapshot(ctx, s.marginEx, s.symbol)
		if err != nil {
			return fmt.Errorf("read spot-margin positions: %w", err)
		}
		if marginPositions == nil {
			return errors.New("spot-margin position snapshot is nil, not an authoritative empty snapshot")
		}
		for _, p := range marginPositions {
			if p == nil || math.IsNaN(p.Size) || math.IsInf(p.Size, 0) {
				return errors.New("spot-margin position snapshot contains invalid data")
			}
			if p.Size != 0 {
				return fmt.Errorf("spot-margin position/debt exists (size=%.8f)", p.Size)
			}
		}
		if err := requireAccountHasNoOpenOrders(ctx, s.marginEx, "spot-margin"); err != nil {
			return err
		}
	}
	return nil
}

func (s *FundingCarryStrategy) requireNoOpenOrders(ctx context.Context) error {
	for name, ex := range map[string]exchange.IExchange{"futures": s.fut, "spot": s.spot} {
		orders, err := ex.GetOpenOrders(ctx, s.symbol)
		if err != nil {
			return fmt.Errorf("read %s open orders for %s: %w", name, s.symbol, err)
		}
		if orders == nil {
			return fmt.Errorf("read %s open orders for %s: nil response is not an authoritative empty snapshot", name, s.symbol)
		}
		if len(orders) != 0 {
			return fmt.Errorf("%s has %d open orders for %s", name, len(orders), s.symbol)
		}
	}
	if s.marginEx != nil {
		orders, err := s.marginEx.GetOpenOrders(ctx, s.symbol)
		if err != nil {
			return fmt.Errorf("read spot-margin open orders for %s: %w", s.symbol, err)
		}
		if orders == nil {
			return errors.New("read spot-margin open orders: nil response is not an authoritative empty snapshot")
		}
		if len(orders) != 0 {
			return fmt.Errorf("spot-margin has %d open orders for %s", len(orders), s.symbol)
		}
	}
	return nil
}

func requireAccountHasNoOpenOrders(ctx context.Context, ex exchange.IExchange, market string) error {
	if reader, ok := ex.(exchange.AccountOpenOrdersReader); ok {
		orders, err := reader.GetAccountOpenOrders(ctx)
		if err != nil {
			return fmt.Errorf("read account-wide %s open orders: %w", market, err)
		}
		if orders == nil {
			return fmt.Errorf("account-wide %s open-order snapshot is nil, not an authoritative empty snapshot", market)
		}
		for _, order := range orders {
			if order == nil || strings.TrimSpace(order.Symbol) == "" {
				return fmt.Errorf("account-wide %s open-order snapshot contains an invalid row", market)
			}
		}
		if len(orders) != 0 {
			return fmt.Errorf("account-wide %s snapshot has %d open orders", market, len(orders))
		}
		return nil
	}
	if verifier, ok := ex.(exchange.AccountOpenOrdersVerifier); ok {
		if err := verifier.VerifyAccountHasNoOpenOrders(ctx); err != nil {
			return fmt.Errorf("verify account-wide %s open orders are empty: %w", market, err)
		}
		return nil
	}
	return fmt.Errorf("%s exchange does not expose authoritative account-wide open-order evidence", market)
}

// VerifyFlat independently verifies live legs, owned spot inventory, pending
// orders, and the durable ownership snapshot before the runtime releases capital.
func (s *FundingCarryStrategy) VerifyFlat(ctx context.Context) error {
	if ctx == nil {
		return errors.New("funding_carry flat verification requires context")
	}
	return s.withAccountWalletCoordination(ctx, func(verifyCtx context.Context) error {
		if err := s.acquireOperation(verifyCtx); err != nil {
			return fmt.Errorf("wait for funding_carry operation before flat verification: %w", err)
		}
		defer s.releaseOperation()

		s.mu.RLock()
		known, inFlight, unknown := s.strategySpotKnown, s.intentInFlight, s.unownedExposure
		stateErr, stateStore := s.runtimeStateErr, s.runtimeStateStore
		s.mu.RUnlock()
		if !known || inFlight || unknown || stateErr != nil || stateStore == nil {
			return errors.New("funding_carry ownership state is missing, unresolved, or not durable")
		}
		if err := s.syncPositions(verifyCtx); err != nil {
			return fmt.Errorf("verify funding_carry live position ownership: %w", err)
		}
		if err := s.requireNoOpenOrders(verifyCtx); err != nil {
			return fmt.Errorf("verify funding_carry open orders: %w", err)
		}

		version, payload, found, err := stateStore.LoadRuntimeState("funding_carry")
		if err != nil {
			return fmt.Errorf("reload durable funding_carry ownership state: %w", err)
		}
		if !found {
			return errors.New("durable funding_carry ownership state is missing")
		}
		state, err := decodeFundingCarryRuntimeState(version, payload, s.fut.GetName(), s.spot.GetName(), s.symbol)
		if err != nil {
			return fmt.Errorf("durable funding_carry ownership state is unresolved: %w", err)
		}
		if err := validateFundingCarryDebtAsset(state, s.spot.GetBaseAsset()); err != nil {
			return err
		}
		if state.Direction != DirectionNone || state.OwnedSpot > s.roundingTolerance(s.spot.GetQuantityDecimals()) ||
			state.OwnedFutures > s.roundingTolerance(s.fut.GetQuantityDecimals()) ||
			state.MarginDebt > s.roundingTolerance(s.marginQuantityDecimals()) {
			return errors.New("durable funding_carry state still records strategy-owned exposure")
		}

		s.mu.RLock()
		defer s.mu.RUnlock()
		if s.unownedExposure || s.intentInFlight || s.runtimeStateErr != nil || s.direction != DirectionNone ||
			s.strategySpotQty > s.roundingTolerance(s.spot.GetQuantityDecimals()) ||
			s.futQty > s.roundingTolerance(s.fut.GetQuantityDecimals()) ||
			s.marginDebt > s.roundingTolerance(s.marginQuantityDecimals()) {
			return errors.New("in-memory funding_carry ownership state still records unresolved exposure")
		}
		return nil
	})
}

func (s *FundingCarryStrategy) marginQuantityDecimals() int {
	if s.marginEx == nil {
		return s.fut.GetQuantityDecimals()
	}
	return s.marginEx.GetQuantityDecimals()
}

func (s *FundingCarryStrategy) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return s.StopContext(ctx)
}

// StopContext halts the rebalance loop and closes only strategy-owned legs.
// It is used by the runtime shutdown coordinator so the close obeys its deadline.
func (s *FundingCarryStrategy) StopContext(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.RLock()
	cancel, runDone := s.cancel, s.runDone
	s.mu.RUnlock()
	if cancel == nil {
		return nil
	}
	cancel()
	if runDone != nil {
		select {
		case <-runDone:
		case <-ctx.Done():
			return errors.New("funding_carry run loop did not stop; positions left unchanged for safety")
		}
	}
	if err := s.acquireOperation(ctx); err != nil {
		return fmt.Errorf("wait for strategy-owned manual close before stop: %w", err)
	}
	defer s.releaseOperation()
	s.stopMu.Lock()
	defer s.stopMu.Unlock()
	if s.stopCompleted || s.stopAttempted {
		return s.stopErr
	}
	s.stopAttempted = true
	s.mu.RLock()
	dir, ownSpot, unknown := s.direction, s.strategySpotQty, s.unownedExposure
	s.mu.RUnlock()
	if unknown {
		s.stopErr = errors.New("funding_carry exposure is unverified; automatic close is withheld to avoid changing unowned positions")
		return s.stopErr
	}
	if err := ctx.Err(); err != nil {
		s.stopErr = fmt.Errorf("funding_carry stop deadline expired before close: %w", err)
		return s.stopErr
	}
	if dir == DirectionNone {
		if ownSpot > 0 {
			s.stopErr = s.closeStrategySpotWithAccountWalletCoordination(ctx)
		}
	} else {
		logger.Info("⏹️ [%s] 停止前嘗試平倉 (direction=%s)…", s.symbol, dir)
		if dir == DirectionForward {
			s.stopErr = s.closeAllWithAccountWalletCoordination(ctx, "bot_stopped")
		} else {
			s.stopErr = s.closeReverseWithAccountWalletCoordination(ctx, "bot_stopped")
		}
	}
	if s.stopErr != nil {
		logger.Warn("⚠️ [%s] 停止時平倉失敗: %v", s.symbol, s.stopErr)
		s.publishEvent(event.EventTypeRiskTriggered, map[string]interface{}{
			"action": "stop_close_failed", "error": s.stopErr.Error(),
			"message": "Bot 停止時平倉失敗，請手動檢查交易所持倉",
		})
		return s.stopErr
	}
	s.stopCompleted = true
	logger.Info("⏹️ [%s] 資金費套利策略已停止", s.symbol)
	return s.stopErr
}

func (s *FundingCarryStrategy) OnPriceChange(float64) error               { return nil }
func (s *FundingCarryStrategy) OnOrderUpdate(*position.OrderUpdate) error { return nil }
func (s *FundingCarryStrategy) GetPositions() []*Position                 { return nil }
func (s *FundingCarryStrategy) GetOrders() []*Order                       { return nil }
func (s *FundingCarryStrategy) GetStatistics() *StrategyStatistics        { return &StrategyStatistics{} }

func (s *FundingCarryStrategy) GetVisualizationData() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]interface{}{
		"type":            "funding_carry",
		"direction":       s.direction.String(),
		"position_open":   s.direction != DirectionNone,
		"spot_qty":        s.spotQty,
		"futures_qty":     s.futQty,
		"margin_debt":     s.marginDebt,
		"next_settlement": s.nextSettlement.Format(time.RFC3339),
		"reverse_enabled": s.reverseEnabled,
	}
}

// GetFundingStatus 返回結算時間與持倉摘要（供 API 層使用）
func (s *FundingCarryStrategy) GetFundingStatus() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	secsUntil := 0.0
	if !s.nextSettlement.IsZero() {
		secsUntil = time.Until(s.nextSettlement).Seconds()
		if secsUntil < 0 {
			secsUntil = 0
		}
	}
	return map[string]interface{}{
		"symbol":                   s.symbol,
		"direction":                s.direction.String(),
		"spot_qty":                 s.spotQty,
		"fut_qty":                  s.futQty,
		"margin_debt":              s.marginDebt,
		"next_settlement":          s.nextSettlement.Format(time.RFC3339),
		"seconds_until_settlement": int(secsUntil),
		"reverse_enabled":          s.reverseEnabled,
	}
}

// ---------------------------------------------------------------------------
// Main loop
// ---------------------------------------------------------------------------

func (s *FundingCarryStrategy) runLoop() {
	defer close(s.runDone)
	t := time.NewTicker(s.tickInterval)
	defer t.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
			if err := s.tick(); err != nil {
				s.mu.Lock()
				s.consecutiveErrors++
				errCount := s.consecutiveErrors
				s.mu.Unlock()

				logger.Warn("⚠️ [%s] funding_carry tick error (%d): %v", s.symbol, errCount, err)

				if errCount >= maxConsecutiveErrors {
					s.publishEvent(event.EventTypeRiskTriggered, map[string]interface{}{
						"consecutive_errors": errCount,
						"last_error":         err.Error(),
						"message":            fmt.Sprintf("資金費套利連續 %d 次 tick 異常，請檢查 API 狀態", errCount),
					})
					s.mu.Lock()
					s.consecutiveErrors = 0
					s.mu.Unlock()
				}
			} else {
				s.mu.Lock()
				s.consecutiveErrors = 0
				s.mu.Unlock()
			}
		}
	}
}

func (s *FundingCarryStrategy) tick() error {
	s.mu.RLock()
	baseCtx := s.ctx
	s.mu.RUnlock()
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	ctx, cancel := context.WithTimeout(baseCtx, 60*time.Second)
	defer cancel()
	if err := s.acquireOperation(ctx); err != nil {
		return err
	}
	defer s.releaseOperation()

	if err := s.requireNoOpenOrders(ctx); err != nil {
		return s.blockOnUnownedExposure(fmt.Errorf("active orders prevent funding_carry reconciliation: %w", err))
	}
	if err := s.syncPositions(ctx); err != nil {
		return fmt.Errorf("syncPositions: %w", err)
	}
	s.applyOpeningSchedule(time.Now().UTC())

	// 嘗試獲取結算時間（非致命，失敗時用 UTC 8h 估算）
	s.updateSettlementTime(ctx)

	// 結算後首個 tick：收割利潤
	s.mu.RLock()
	settled := s.settledThisTick
	s.mu.RUnlock()
	if settled && s.profitHarvestEnabled {
		s.harvestProfit(ctx)
	}

	fundingInfo, err := s.fut.GetFundingInfo(ctx, s.symbol)
	if err != nil {
		return fmt.Errorf("GetFundingInfo for funding_carry monitoring: %w", err)
	}
	rate, err := fundingCarryNormalizedOpeningRate(fundingInfo, s.symbol)
	if err != nil {
		return fmt.Errorf("normalize funding_carry monitoring rate: %w", err)
	}

	s.mu.RLock()
	dir := s.direction
	spotQ := s.spotQty
	futQ := s.futQty
	debt := s.marginDebt
	nearSettlement := s.isNearSettlement()
	s.mu.RUnlock()

	hasPosition := dir != DirectionNone

	// 腿不平衡檢測
	if hasPosition {
		imbalanced := false
		if dir == DirectionForward && ((spotQ > 0) != (futQ > 0)) {
			imbalanced = true
		}
		if dir == DirectionReverse && ((debt > 0) != (futQ > 0)) {
			imbalanced = true
		}
		if imbalanced {
			logger.Warn("⚠️ [%s] 腿不平衡! dir=%s spot=%.8f fut=%.8f debt=%.8f", s.symbol, dir, spotQ, futQ, debt)
			s.publishEvent(event.EventTypeRiskTriggered, map[string]interface{}{
				"spot_qty":    spotQ,
				"fut_qty":     futQ,
				"margin_debt": debt,
				"direction":   dir.String(),
				"message":     "期現對沖腿不平衡，建議手動檢查",
			})
			if dir == DirectionForward {
				return s.closeAllWithAccountWalletCoordination(ctx, "imbalanced_legs")
			}
			return s.closeReverseWithAccountWalletCoordination(ctx, "imbalanced_legs")
		}
	}

	// 持倉中：檢查退出條件
	if hasPosition {
		if dir == DirectionForward && fundingCarryShouldExit(dir, rate, s.exitFundingRate, s.reverseExitRate) {
			logger.Info("📉 [%s] 資金費 %.5f < 退出閾值 %.5f，正向平倉", s.symbol, rate, s.exitFundingRate)
			return s.closeAllWithAccountWalletCoordination(ctx, "exit_funding_rate")
		}
		if dir == DirectionReverse && fundingCarryShouldExit(dir, rate, s.exitFundingRate, s.reverseExitRate) {
			logger.Info("📈 [%s] 資金費 %.5f > 反向退出閾值 -%.5f，反向平倉", s.symbol, rate, s.reverseExitRate)
			return s.closeReverseWithAccountWalletCoordination(ctx, "exit_reverse_rate")
		}
		return nil
	}
	for market, ex := range map[string]exchange.IExchange{"futures": s.fut, "spot": s.spot} {
		if err := requireAccountHasNoOpenOrders(ctx, ex, market); err != nil {
			return s.blockOnUnownedExposure(fmt.Errorf("account-wide open orders prevent funding_carry entry: %w", err))
		}
	}
	if s.marginEx != nil {
		if err := requireAccountHasNoOpenOrders(ctx, s.marginEx, "spot-margin"); err != nil {
			return s.blockOnUnownedExposure(fmt.Errorf("account-wide open orders prevent funding_carry entry: %w", err))
		}
	}

	// 無倉位：結算臨近時不開新倉
	if nearSettlement {
		logger.Info("⏳ [%s] 距結算 < %v，暫停開倉", s.symbol, s.settlementBuffer)
		return nil
	}
	if s.openingIsBlocked() {
		return nil
	}
	openingRate := rate

	// 評估費率方向
	if openingRate >= s.minFundingRate {
		futPx, err := s.fut.GetLatestPrice(ctx, s.symbol)
		if err != nil {
			return err
		}
		spotPx, err := s.spot.GetLatestPrice(ctx, s.symbol)
		if err != nil {
			return err
		}
		basisPct := math.Abs(futPx-spotPx) / spotPx * 100
		if basisPct > s.maxBasisPct {
			logger.Warn("⚠️ [%s] 期現價差 %.4f%% 超過上限 %.4f%%，暫不開倉", s.symbol, basisPct, s.maxBasisPct)
			return nil
		}
		return s.openHedge(ctx, futPx, spotPx, openingRate)
	}

	if s.reverseEnabled && openingRate <= -s.reverseMinRate {
		futPx, err := s.fut.GetLatestPrice(ctx, s.symbol)
		if err != nil {
			return err
		}
		spotPx, err := s.spot.GetLatestPrice(ctx, s.symbol)
		if err != nil {
			return err
		}
		basisPct := math.Abs(futPx-spotPx) / spotPx * 100
		if basisPct > s.maxBasisPct {
			logger.Warn("⚠️ [%s] 期現價差 %.4f%% 超上限 %.4f%%，暫不反向開倉", s.symbol, basisPct, s.maxBasisPct)
			return nil
		}
		return s.openReverseHedge(ctx, futPx, spotPx, openingRate)
	}

	return nil
}

func fundingCarryNormalizedOpeningRate(info *exchange.FundingInfo, symbol string) (float64, error) {
	rate, err := normalizeFundingRateToEightHours(info, symbol)
	if err != nil {
		return 0, err
	}
	if math.IsNaN(rate) || math.IsInf(rate, 0) {
		return 0, fmt.Errorf("normalized funding rate is non-finite for %s", symbol)
	}
	return rate, nil
}

func fundingCarryShouldExit(direction CarryDirection, rate, forwardExitRate, reverseExitRate float64) bool {
	switch direction {
	case DirectionForward:
		return rate < forwardExitRate
	case DirectionReverse:
		return rate > -reverseExitRate
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// Settlement time awareness
// ---------------------------------------------------------------------------

func (s *FundingCarryStrategy) updateSettlementTime(ctx context.Context) {
	info, err := s.fut.GetFundingInfo(ctx, s.symbol)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.settledThisTick = false

	if err != nil {
		// API 不支持或臨時失敗：用 UTC 8h 估算
		if s.nextSettlement.IsZero() {
			s.nextSettlement = estimateNextSettlement(time.Now().UTC())
		} else if time.Now().After(s.nextSettlement) {
			s.lastSettlement = s.nextSettlement
			s.nextSettlement = estimateNextSettlement(time.Now().UTC())
			s.settledThisTick = true
		}
		return
	}

	// API 返回了精確的下次結算時間
	if !s.nextSettlement.IsZero() && !info.NextFundingTime.Equal(s.nextSettlement) && time.Now().After(s.nextSettlement) {
		s.lastSettlement = s.nextSettlement
		s.settledThisTick = true
	}
	s.nextSettlement = info.NextFundingTime
}

func (s *FundingCarryStrategy) isNearSettlement() bool {
	if s.nextSettlement.IsZero() {
		return false
	}
	return time.Until(s.nextSettlement) < s.settlementBuffer
}

func estimateNextSettlement(now time.Time) time.Time {
	hour := now.Hour()
	base := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	switch {
	case hour < 8:
		return base.Add(8 * time.Hour)
	case hour < 16:
		return base.Add(16 * time.Hour)
	default:
		return base.Add(24 * time.Hour)
	}
}

// ---------------------------------------------------------------------------
// Auto transfer
// ---------------------------------------------------------------------------

func (s *FundingCarryStrategy) ensureFuturesMargin(ctx context.Context, requiredUSDT, spotOrderReserveUSDT float64) (resultErr error) {
	if ctx == nil {
		return errors.New("funding_carry collateral transfer requires context")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("funding_carry collateral check canceled before balance reads: %w", err)
	}
	s.mu.RLock()
	unverified := s.unownedExposure || s.runtimeStateErr != nil
	s.mu.RUnlock()
	if unverified {
		return errors.New("funding_carry wallet or exposure state is unverified; automatic transfer is blocked")
	}
	if !finitePositive(requiredUSDT) || !finiteNonNegative(spotOrderReserveUSDT) ||
		!finiteNonNegative(s.transferReserveSpot) {
		return fmt.Errorf("invalid funding_carry collateral or spot reserve amount")
	}
	futBal, err := fundingCarryFuturesUSDTBalance(ctx, s.fut)
	if err != nil {
		return fmt.Errorf("query futures USDT balance before transfer: %w", err)
	}
	spotBal, err := s.spot.GetBalance(ctx, "USDT")
	if err != nil {
		return fmt.Errorf("query spot USDT balance before transfer: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("spot USDT balance read outlived its context: %w", err)
	}
	if !finiteNonNegative(futBal) || !finiteNonNegative(spotBal) {
		return fmt.Errorf("invalid futures/spot USDT balance: futures %.12g, spot %.12g", futBal, spotBal)
	}
	s.mu.RLock()
	accountFuturesReserve := s.accountFuturesCapitalReserve
	externalFuturesReserve := s.externalFuturesCapitalReserve
	externalSpotReserve := s.externalSpotCapitalReserve
	s.mu.RUnlock()
	ownFuturesReserve := math.Max(0, accountFuturesReserve-externalFuturesReserve)
	futuresReserve := math.Max(requiredUSDT, ownFuturesReserve) + externalFuturesReserve
	if !finitePositive(futuresReserve) {
		return fmt.Errorf("aggregate futures reserve is invalid")
	}
	spotReserve := spotOrderReserveUSDT + externalSpotReserve
	if s.autoTransferEnabled {
		spotReserve += s.transferReserveSpot
	}
	if !finiteNonNegative(spotReserve) || spotBal < spotReserve {
		return fmt.Errorf("insufficient spot USDT for pending hedge leg and reserve: balance %.2f, required %.2f", spotBal, spotReserve)
	}
	if futBal >= futuresReserve {
		return nil
	}
	if !s.autoTransferEnabled {
		return fmt.Errorf("insufficient futures USDT after preserving other Bot budgets: available %.2f, required %.2f", futBal, futuresReserve)
	}
	need := futuresReserve - futBal
	transferable := spotBal - spotReserve
	if need > transferable {
		return fmt.Errorf("insufficient transferable spot USDT after reserving hedge leg: available %.2f, transfer %.2f", transferable, need)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("funding_carry collateral transfer canceled before submission: %w", err)
	}
	if err := s.beginRuntimeIntent(ctx); err != nil {
		return s.blockOnUnownedExposure(fmt.Errorf("persist collateral transfer intent before submission: %w", err))
	}
	transferVerified := false
	defer func() {
		if err := s.finishRuntimeIntent(ctx, transferVerified); err != nil {
			logger.Error("[%s] persist funding_carry collateral transfer result: %v", s.symbol, err)
			resultErr = errors.Join(resultErr, err)
		}
	}()
	txID, err := s.spot.InternalTransfer(ctx, "SPOT", "UMFUTURE", "USDT", need)
	if err != nil {
		return s.blockOnUnownedExposure(fmt.Errorf("SPOT→UMFUTURE transfer %.2f USDT has uncertain outcome; retry blocked pending reconciliation: %w", need, err))
	}
	futBal, err = fundingCarryFuturesUSDTBalance(ctx, s.fut)
	if err != nil {
		return s.blockOnUnownedExposure(fmt.Errorf("transfer %s accepted but futures balance could not be verified: %w", txID, err))
	}
	if !finiteNonNegative(futBal) || futBal < futuresReserve {
		return s.blockOnUnownedExposure(fmt.Errorf("transfer %s completed but futures USDT remains insufficient after preserving other Bot budgets: %.2f < %.2f", txID, futBal, futuresReserve))
	}
	spotBal, err = s.spot.GetBalance(ctx, "USDT")
	if err != nil {
		return s.blockOnUnownedExposure(fmt.Errorf("transfer %s accepted but remaining spot USDT could not be verified: %w", txID, err))
	}
	if err := ctx.Err(); err != nil {
		return s.blockOnUnownedExposure(fmt.Errorf("transfer %s completed but spot balance read outlived its context: %w", txID, err))
	}
	if !finiteNonNegative(spotBal) || spotBal < spotReserve {
		return s.blockOnUnownedExposure(fmt.Errorf("transfer %s completed but spot USDT reserve is insufficient: %.2f < %.2f", txID, spotBal, spotReserve))
	}
	transferVerified = true
	logger.Info("💸 [%s] 自動劃轉 SPOT→UMFUTURE %.2f USDT (txID=%s)", s.symbol, need, txID)
	s.publishEvent(event.EventTypePositionOpened, map[string]interface{}{
		"action":  "auto_transfer_in",
		"amount":  need,
		"tx_id":   txID,
		"message": fmt.Sprintf("自動劃轉 %.2f USDT 到合約帳戶", need),
	})
	return nil
}

// fundingCarryFuturesUSDTBalance bypasses exchange balance caches when a fresh
// account snapshot is available, which is essential immediately after a wallet transfer.
func fundingCarryFuturesUSDTBalance(ctx context.Context, futures exchange.IExchange) (float64, error) {
	if ctx == nil || futures == nil {
		return 0, errors.New("futures balance query requires context and exchange")
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if fresh, ok := futures.(interface {
		GetAccountFresh(context.Context) (*exchange.Account, error)
	}); ok {
		account, err := fresh.GetAccountFresh(ctx)
		if err != nil {
			return 0, err
		}
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if account == nil || !strings.EqualFold(strings.TrimSpace(account.BalanceAsset), "USDT") ||
			!finiteNonNegative(account.AvailableBalance) {
			return 0, errors.New("fresh futures account did not provide a finite USDT available balance")
		}
		return account.AvailableBalance, nil
	}
	balance, err := futures.GetBalance(ctx, "USDT")
	if err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if !finiteNonNegative(balance) {
		return 0, errors.New("futures USDT balance is invalid")
	}
	return balance, nil
}

func validateFundingCarryReverseNetRate(info *exchange.FundingInfo, symbol string, hourlyBorrowRate float64) error {
	if math.IsNaN(hourlyBorrowRate) || math.IsInf(hourlyBorrowRate, 0) || hourlyBorrowRate < 0 {
		return fmt.Errorf("invalid hourly margin borrow rate %.12g", hourlyBorrowRate)
	}
	fundingRate, err := normalizeFundingRateToEightHours(info, symbol)
	if err != nil {
		return err
	}
	borrowCost := hourlyBorrowRate * 8
	if math.IsNaN(borrowCost) || math.IsInf(borrowCost, 0) {
		return fmt.Errorf("non-finite 8-hour margin borrow cost")
	}
	if fundingRate >= 0 || math.Abs(fundingRate) <= borrowCost {
		return fmt.Errorf("8-hour negative funding yield %.8f does not exceed estimated borrow interest %.8f", math.Abs(fundingRate), borrowCost)
	}
	return nil
}

func fundingCarryConfiguredFeeRate(cfg *config.Config, exchangeName string) (float64, error) {
	if cfg == nil {
		return 0, fmt.Errorf("exchange fee rate is not configured; refusing funding carry opening")
	}
	exchangeCfg, ok := cfg.Exchanges[exchangeName]
	if !ok || math.IsNaN(exchangeCfg.FeeRate) || math.IsInf(exchangeCfg.FeeRate, 0) || exchangeCfg.FeeRate < 0 || exchangeCfg.FeeRate > 1 {
		return 0, fmt.Errorf("valid fee_rate is required for exchange %q; refusing funding carry opening", exchangeName)
	}
	return exchangeCfg.FeeRate, nil
}

func validateFundingCarryFeeRecovery(fundingRate, feeRate, maxRecoveryDays, hourlyBorrowRate float64) error {
	return validateFundingCarryExecutionCostRecovery(fundingRate, feeRate, maxRecoveryDays, hourlyBorrowRate, 1, 1, 0)
}

func validateFundingCarryExecutionCostRecovery(fundingRate, feeRate, maxRecoveryDays, hourlyBorrowRate, spotNotional, futuresNotional, roundTripBookCost float64) error {
	if math.IsNaN(fundingRate) || math.IsInf(fundingRate, 0) ||
		math.IsNaN(feeRate) || math.IsInf(feeRate, 0) || feeRate < 0 || feeRate > 1 ||
		!finitePositive(maxRecoveryDays) || maxRecoveryDays > 365 ||
		!finiteNonNegative(hourlyBorrowRate) || !finitePositive(spotNotional) ||
		!finitePositive(futuresNotional) || !finiteNonNegative(roundTripBookCost) {
		return fmt.Errorf("invalid funding carry fee recovery inputs")
	}
	netRate := math.Abs(fundingRate) - hourlyBorrowRate*8
	fundingNotional := futuresNotional
	if !finitePositive(fundingNotional) {
		return fmt.Errorf("invalid funding notional")
	}
	roundTripFees := 2 * feeRate * (spotNotional + futuresNotional)
	roundTripCostRate := (roundTripFees + roundTripBookCost) / fundingNotional
	recoveryRate := roundTripCostRate / (3 * maxRecoveryDays)
	if !finiteNonNegative(recoveryRate) || netRate < recoveryRate {
		return fmt.Errorf("estimated 8-hour net carry %.8f is below %.8f required to recover estimated round-trip fees and book cost within %.2f days", netRate, recoveryRate, maxRecoveryDays)
	}
	return nil
}

func fundingCarryRoundTripOrderBookCost(ctx context.Context, symbol string, spot, futures exchange.IExchange, spotQty, futuresQty float64) (float64, error) {
	if ctx == nil || spot == nil || futures == nil || strings.TrimSpace(symbol) == "" || !finitePositive(spotQty) || !finitePositive(futuresQty) {
		return 0, fmt.Errorf("invalid funding carry order book cost inputs")
	}
	spotBook, err := spot.GetOrderBook(ctx, symbol, fundingCarryOrderBookDepth)
	if err != nil {
		return 0, fmt.Errorf("read spot order book for round-trip cost: %w", err)
	}
	futuresBook, err := futures.GetOrderBook(ctx, symbol, fundingCarryOrderBookDepth)
	if err != nil {
		return 0, fmt.Errorf("read futures order book for round-trip cost: %w", err)
	}
	spotCost, err := fundingCarrySingleMarketRoundTripBookCost(spotBook, symbol, spotQty)
	if err != nil {
		return 0, fmt.Errorf("validate spot round-trip order book: %w", err)
	}
	futuresCost, err := fundingCarrySingleMarketRoundTripBookCost(futuresBook, symbol, futuresQty)
	if err != nil {
		return 0, fmt.Errorf("validate futures round-trip order book: %w", err)
	}
	cost := spotCost + futuresCost
	if !finiteNonNegative(cost) {
		return 0, fmt.Errorf("estimated round-trip order book cost is invalid")
	}
	return cost, nil
}

func fundingCarrySingleMarketRoundTripBookCost(book *exchange.OrderBook, symbol string, quantity float64) (float64, error) {
	if book == nil || !strings.EqualFold(strings.TrimSpace(book.Symbol), strings.TrimSpace(symbol)) ||
		len(book.Bids) == 0 || len(book.Asks) == 0 || !finitePositive(quantity) {
		return 0, fmt.Errorf("order book identity, sides, or quantity is invalid")
	}
	if !finitePositive(book.Bids[0].Price) || !finitePositive(book.Asks[0].Price) || book.Bids[0].Price >= book.Asks[0].Price {
		return 0, fmt.Errorf("order book top of book is invalid or crossed")
	}
	bidVWAP, err := fundingCarryOrderBookVWAP(book.Bids, quantity, exchange.SideSell)
	if err != nil {
		return 0, fmt.Errorf("walk bid side: %w", err)
	}
	askVWAP, err := fundingCarryOrderBookVWAP(book.Asks, quantity, exchange.SideBuy)
	if err != nil {
		return 0, fmt.Errorf("walk ask side: %w", err)
	}
	cost := quantity * (askVWAP - bidVWAP)
	if !finiteNonNegative(cost) {
		return 0, fmt.Errorf("round-trip book cost is negative or non-finite")
	}
	return cost, nil
}

func fundingCarryOrderBookVWAP(levels []exchange.OrderBookLevel, quantity float64, side exchange.Side) (float64, error) {
	remaining := quantity
	quoteValue := 0.0
	for index, level := range levels {
		if !finitePositive(level.Price) || !finitePositive(level.Quantity) {
			return 0, fmt.Errorf("order book contains invalid level")
		}
		if index > 0 && ((side == exchange.SideBuy && level.Price < levels[index-1].Price) ||
			(side == exchange.SideSell && level.Price > levels[index-1].Price)) {
			return 0, fmt.Errorf("order book levels are not ordered best to worst")
		}
		fillQty := math.Min(remaining, level.Quantity)
		quoteValue += fillQty * level.Price
		remaining -= fillQty
		if remaining <= math.Max(1e-12, quantity*1e-12) {
			return quoteValue / quantity, nil
		}
	}
	return 0, fmt.Errorf("order book depth cannot fill requested quantity %.8f", quantity)
}

func fundingCarryWorstCaseEntrySlippageCost(quantity, referencePrice, limitPrice float64, side exchange.Side) (float64, error) {
	if !finitePositive(quantity) || !finitePositive(referencePrice) || !finitePositive(limitPrice) {
		return 0, fmt.Errorf("invalid funding carry entry slippage inputs")
	}
	priceDelta := 0.0
	switch side {
	case exchange.SideBuy:
		priceDelta = math.Max(0, limitPrice-referencePrice)
	case exchange.SideSell:
		priceDelta = math.Max(0, referencePrice-limitPrice)
	default:
		return 0, fmt.Errorf("unsupported funding carry entry side %q", side)
	}
	cost := quantity * priceDelta
	if !finiteNonNegative(cost) {
		return 0, fmt.Errorf("funding carry entry slippage cost is invalid")
	}
	return cost, nil
}

func finitePositive(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func finiteNonNegative(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func fundingCarrySpotBuyReserve(quantity, limitPrice, feeRate float64) (float64, error) {
	if !finitePositive(quantity) || !finitePositive(limitPrice) || math.IsNaN(feeRate) || math.IsInf(feeRate, 0) {
		return 0, fmt.Errorf("invalid spot buy reserve inputs")
	}
	if feeRate < 0 {
		feeRate = 0
	}
	reserve := quantity * limitPrice * (1 + feeRate)
	if !finitePositive(reserve) {
		return 0, fmt.Errorf("spot buy reserve is invalid")
	}
	return reserve, nil
}

func validateFundingCarryMarginBalance(availableUSDT, requiredUSDT float64) error {
	if !finiteNonNegative(availableUSDT) || !finitePositive(requiredUSDT) {
		return fmt.Errorf("spot-margin USDT balance or required collateral is invalid")
	}
	if availableUSDT < requiredUSDT {
		return fmt.Errorf("insufficient spot-margin USDT collateral: available %.2f, required %.2f", availableUSDT, requiredUSDT)
	}
	return nil
}

func (s *FundingCarryStrategy) harvestProfit(ctx context.Context) {
	if !s.profitHarvestEnabled {
		return
	}
	if err := s.withAccountWalletCoordination(ctx, func(operationCtx context.Context) error {
		s.harvestProfitUnderWalletLock(operationCtx)
		return nil
	}); err != nil {
		logger.Warn("⚠️ [%s] 利潤歸集因帳戶錢包協調失敗而跳過: %v", s.symbol, err)
	}
}

func (s *FundingCarryStrategy) harvestProfitUnderWalletLock(ctx context.Context) {
	if !s.profitHarvestEnabled {
		return
	}
	if ctx == nil || ctx.Err() != nil {
		return
	}
	s.mu.RLock()
	unverified := s.unownedExposure || s.runtimeStateErr != nil
	s.mu.RUnlock()
	if unverified {
		return
	}
	futBal, err := fundingCarryFuturesUSDTBalance(ctx, s.fut)
	if err != nil {
		return
	}
	s.mu.RLock()
	dir := s.direction
	futQ := s.futQty
	futuresCapitalReserve := s.accountFuturesCapitalReserve
	s.mu.RUnlock()

	// 策略記錄與持倉方向矛盾時不歸集；也不假設槓桿或跨 Bot 可用資金。
	if (dir == DirectionNone && futQ != 0) || (dir != DirectionNone && !finitePositive(futQ)) {
		return
	}
	var futuresPrice float64
	if dir != DirectionNone {
		px, priceErr := s.fut.GetLatestPrice(ctx, s.symbol)
		if priceErr != nil || !finitePositive(px) {
			return
		}
		futuresPrice = px
	}
	surplus, ok := fundingCarryHarvestableSurplus(futBal, futQ, futuresPrice, s.profitHarvestMin, futuresCapitalReserve)
	if !ok {
		return
	}
	spotBal, err := s.spot.GetBalance(ctx, "USDT")
	if err != nil || ctx.Err() != nil || !finiteNonNegative(spotBal) {
		return
	}
	if err := s.beginRuntimeIntent(ctx); err != nil {
		s.blockOnUnownedExposure(fmt.Errorf("persist profit harvest intent before transfer: %w", err))
		return
	}
	transferVerified := false
	defer func() {
		if err := s.finishRuntimeIntent(ctx, transferVerified); err != nil {
			logger.Error("[%s] persist funding_carry profit harvest result: %v", s.symbol, err)
		}
	}()

	txID, err := s.fut.InternalTransfer(ctx, "UMFUTURE", "SPOT", "USDT", surplus)
	if err != nil {
		s.blockOnUnownedExposure(fmt.Errorf("profit harvest transfer %.2f USDT has uncertain outcome; retry blocked pending reconciliation: %w", surplus, err))
		logger.Warn("⚠️ [%s] 利潤歸集結果不確定 %.2f USDT，已鎖定自動交易: %v", s.symbol, surplus, err)
		return
	}
	postFuturesBalance, err := fundingCarryFuturesUSDTBalance(ctx, s.fut)
	if err != nil {
		s.blockOnUnownedExposure(fmt.Errorf("profit harvest %s accepted but futures balance could not be verified: %w", txID, err))
		return
	}
	postSpotBalance, err := s.spot.GetBalance(ctx, "USDT")
	if err != nil {
		s.blockOnUnownedExposure(fmt.Errorf("profit harvest %s accepted but spot balance could not be verified: %w", txID, err))
		return
	}
	if err := ctx.Err(); err != nil {
		s.blockOnUnownedExposure(fmt.Errorf("profit harvest %s completed but spot balance read outlived its context: %w", txID, err))
		return
	}
	if !finiteNonNegative(postFuturesBalance) || !finiteNonNegative(postSpotBalance) {
		s.blockOnUnownedExposure(fmt.Errorf("profit harvest %s returned invalid post-transfer balances: futures %.12g, spot %.12g", txID, postFuturesBalance, postSpotBalance))
		return
	}
	minimumProtectedBalance := math.Max(futQ*futuresPrice, futuresCapitalReserve) + fundingCarrySafetyBufferUSDT
	const transferVerificationTolerance = 0.01
	if postFuturesBalance+transferVerificationTolerance < minimumProtectedBalance ||
		postSpotBalance+transferVerificationTolerance < spotBal+surplus {
		s.blockOnUnownedExposure(fmt.Errorf("profit harvest %s did not reconcile: futures %.8f (minimum %.8f), spot increase %.8f (expected %.8f)",
			txID, postFuturesBalance, minimumProtectedBalance, postSpotBalance-spotBal, surplus))
		return
	}
	transferVerified = true
	logger.Info("💰 [%s] 結算後利潤歸集 UMFUTURE→SPOT %.2f USDT (txID=%s)", s.symbol, surplus, txID)
	s.publishEvent(event.EventTypePositionClosed, map[string]interface{}{
		"action":  "profit_harvest",
		"amount":  surplus,
		"tx_id":   txID,
		"message": fmt.Sprintf("利潤歸集 %.2f USDT 到現貨帳戶", surplus),
	})
}

func fundingCarryHarvestableSurplus(futuresBalance, futuresQty, futuresPrice, minAmount, accountCapitalReserve float64) (float64, bool) {
	if !finiteNonNegative(futuresBalance) || !finiteNonNegative(futuresQty) ||
		!finiteNonNegative(accountCapitalReserve) || !finitePositive(minAmount) {
		return 0, false
	}
	var positionNotional float64
	if futuresQty > 0 {
		if !finitePositive(futuresPrice) {
			return 0, false
		}
		positionNotional = futuresQty * futuresPrice
		if !finitePositive(positionNotional) {
			return 0, false
		}
	}
	protectedCapital := math.Max(positionNotional, accountCapitalReserve)
	surplus := futuresBalance - protectedCapital - fundingCarrySafetyBufferUSDT
	if !finitePositive(surplus) || surplus < minAmount {
		return 0, false
	}
	return surplus, true
}

// ---------------------------------------------------------------------------
// Position sync
// ---------------------------------------------------------------------------

func (s *FundingCarryStrategy) syncPositions(ctx context.Context) error {
	// 合約持倉
	var futShort, futLong float64
	pos, err := readScopedPositionSnapshot(ctx, s.fut, s.symbol)
	if err != nil {
		return s.blockOnUnownedExposure(fmt.Errorf("fut.GetPositions: %w", err))
	}
	if pos == nil {
		return s.blockOnUnownedExposure(errors.New("futures position snapshot is nil, not an authoritative empty snapshot"))
	}
	for _, p := range pos {
		if p == nil || math.IsNaN(p.Size) || math.IsInf(p.Size, 0) {
			return s.blockOnUnownedExposure(errors.New("futures position snapshot contains invalid data"))
		}
		if p.Size < 0 {
			futShort += math.Abs(p.Size)
		} else if p.Size > 0 {
			futLong += p.Size
		}
	}

	// 現貨餘額
	base := s.spot.GetBaseAsset()
	spotBal, err := s.spot.GetBalance(ctx, base)
	if err != nil {
		return s.blockOnUnownedExposure(fmt.Errorf("spot.GetBalance(%s): %w", base, err))
	}
	if err := ctx.Err(); err != nil {
		return s.blockOnUnownedExposure(fmt.Errorf("spot balance snapshot for %s completed after its context ended: %w", base, err))
	}
	if spotBal < 0 || math.IsNaN(spotBal) || math.IsInf(spotBal, 0) {
		return s.blockOnUnownedExposure(fmt.Errorf("spot balance is invalid: %.8f", spotBal))
	}

	// 保證金借幣負債（反向套利用）
	var debt, debtInterest float64
	if s.marginEx != nil {
		marginPos, e := readScopedPositionSnapshot(ctx, s.marginEx, s.symbol)
		if e != nil {
			return s.blockOnUnownedExposure(fmt.Errorf("marginEx.GetPositions: %w", e))
		}
		if marginPos == nil {
			return s.blockOnUnownedExposure(errors.New("spot-margin position snapshot is nil, not an authoritative empty snapshot"))
		}
		for _, mp := range marginPos {
			if mp == nil || math.IsNaN(mp.Size) || math.IsInf(mp.Size, 0) {
				return s.blockOnUnownedExposure(errors.New("spot-margin position snapshot contains invalid data"))
			}
			if mp.Size < 0 {
				if !mp.MarginDebtKnown || !finiteNonNegative(mp.MarginBorrowed) || !finiteNonNegative(mp.MarginInterest) ||
					math.Abs(math.Abs(mp.Size)-(mp.MarginBorrowed+mp.MarginInterest)) > s.roundingTolerance(s.spot.GetQuantityDecimals()) {
					return s.blockOnUnownedExposure(errors.New("spot-margin liability lacks a consistent principal/interest breakdown"))
				}
				debt += mp.MarginBorrowed
				debtInterest += mp.MarginInterest
			}
		}
	}

	s.mu.Lock()
	if !s.strategySpotKnown {
		s.mu.Unlock()
		if futShort > 0 || futLong > 0 || debt > 0 {
			return s.blockOnUnownedExposure(errors.New("futures/margin exposure exists without strategy-owned inventory state"))
		}
		s.mu.Lock()
		s.strategySpotKnown = true
		s.strategySpotQty = 0
	}
	strategySpot := s.strategySpotQty
	dir, ownedFut, ownedDebt, blocked := s.direction, s.futQty, s.marginDebt, s.unownedExposure
	s.mu.Unlock()
	if blocked {
		return errors.New("funding_carry exposure is unowned/unverified; automatic trading remains blocked")
	}
	tolerance := s.roundingTolerance(s.fut.GetQuantityDecimals())
	if strategySpot > spotBal+tolerance {
		return s.blockOnUnownedExposure(fmt.Errorf("strategy-owned spot inventory %.8f exceeds exchange balance %.8f", strategySpot, spotBal))
	}
	if futShort > 0 && futLong > 0 {
		return s.blockOnUnownedExposure(errors.New("both long and short futures positions exist; ownership is ambiguous"))
	}
	switch dir {
	case DirectionNone:
		if futShort > tolerance || futLong > tolerance || debt+debtInterest > tolerance || strategySpot > tolerance {
			return s.blockOnUnownedExposure(errors.New("exchange exposure exists without an active funding_carry position record"))
		}
	case DirectionForward:
		spotTolerance := math.Max(tolerance, s.roundingTolerance(s.spot.GetQuantityDecimals()))
		if futLong > tolerance || debt+debtInterest > tolerance || math.Abs(futShort-ownedFut) > tolerance || strategySpot > spotBal+spotTolerance || (strategySpot <= tolerance && ownedFut <= tolerance) {
			return s.blockOnUnownedExposure(errors.New("forward carry exposure does not match strategy-owned futures/spot legs"))
		}
	case DirectionReverse:
		if futShort > tolerance || math.Abs(futLong-ownedFut) > tolerance || math.Abs(debt-ownedDebt) > tolerance || (debt+debtInterest <= tolerance && ownedFut <= tolerance) {
			return s.blockOnUnownedExposure(errors.New("reverse carry exposure does not match strategy-owned futures/margin debt"))
		}
	default:
		return s.blockOnUnownedExposure(fmt.Errorf("invalid strategy direction %d", dir))
	}
	s.mu.Lock()
	s.spotQty = strategySpot
	s.mu.Unlock()
	return nil
}

func (s *FundingCarryStrategy) roundingTolerance(decimals int) float64 {
	if decimals < 0 {
		decimals = 0
	}
	return math.Pow10(-decimals) / 2
}

func (s *FundingCarryStrategy) blockOnUnownedExposure(reason error) error {
	s.mu.Lock()
	s.unownedExposure = true
	blocker := s.openingBlocker
	if ownerErr := verifyStrategyWalletRuntimeOwner(s.openingGate); ownerErr == nil {
		if persistErr := s.persistRuntimeStateLocked(); persistErr != nil {
			s.runtimeStateErr = persistErr
		}
	}
	s.mu.Unlock()
	if blocker != nil {
		blocker("funding_carry_exposure_unverified")
	}
	s.publishEvent(event.EventTypeRiskTriggered, map[string]interface{}{
		"action": "funding_carry_exposure_unverified", "error": reason.Error(),
		"message": "期現/保證金實際敞口與策略所有權無法核對，自動交易已鎖定，需人工檢查",
	})
	return reason
}

func (s *FundingCarryStrategy) recordFuturesOpening(order *exchange.Order, side exchange.Side, requested float64) error {
	if order == nil || order.ExecutedQty <= 0 || math.IsNaN(order.ExecutedQty) || math.IsInf(order.ExecutedQty, 0) ||
		order.ExecutedQty > requested+s.roundingTolerance(s.fut.GetQuantityDecimals()) {
		return s.blockOnUnownedExposure(fmt.Errorf("futures opening acknowledgement is not reconcilable (requested=%.8f)", requested))
	}
	s.mu.Lock()
	if side == exchange.SideSell {
		s.direction = DirectionForward
	} else {
		s.direction = DirectionReverse
	}
	s.futQty = order.ExecutedQty
	if err := s.persistRuntimeStateLocked(); err != nil {
		s.runtimeStateErr = err
		s.unownedExposure = true
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	return nil
}

// ---------------------------------------------------------------------------
// Forward: open hedge
// ---------------------------------------------------------------------------

func (s *FundingCarryStrategy) capitalUSDT() float64 {
	c := s.symCfg.TotalAllocatedCapital
	if c <= 0 {
		c = s.symCfg.OrderQuantity
	}
	return c
}

func (s *FundingCarryStrategy) capitalWithinOpeningLimits(futuresPrice, spotPrice float64) (float64, error) {
	s.mu.RLock()
	control := config.CloneOpenPositionControl(s.openControl)
	s.mu.RUnlock()
	capital := s.capitalUSDT()
	maxQty, maxValue, _ := control.PositionLimits()
	if maxValue > 0 && capital > maxValue {
		capital = maxValue
	}
	if maxQty > 0 {
		if futuresPrice <= 0 || math.IsNaN(futuresPrice) || math.IsInf(futuresPrice, 0) ||
			spotPrice <= 0 || math.IsNaN(spotPrice) || math.IsInf(spotPrice, 0) {
			return 0, errors.New("cannot enforce funding_carry quantity limit without valid prices for both legs")
		}
		quantityPerCapital := 0.5/futuresPrice + 0.5/spotPrice
		if quantityCapital := maxQty / quantityPerCapital; capital > quantityCapital {
			capital = quantityCapital
		}
	}
	return capital, nil
}

func (s *FundingCarryStrategy) applyOpeningSchedule(now time.Time) {
	s.mu.Lock()
	control := config.CloneOpenPositionControl(s.openControl)
	gate := s.openingGate
	for i, rule := range control.ScheduleRules {
		if !rule.Enabled || (rule.Action != "pause" && rule.Action != "resume") {
			continue
		}
		ruleTime, err := time.Parse("15:04", strings.TrimSpace(rule.Time))
		if err != nil || now.Hour() != ruleTime.Hour() || now.Minute() != ruleTime.Minute() {
			continue
		}
		if len(rule.Weekdays) > 0 {
			matched := false
			for _, weekday := range rule.Weekdays {
				if weekday == int(now.Weekday()) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		key := fmt.Sprintf("%d:%s", i, now.Format("2006-01-02 15:04"))
		if s.lastScheduleRun[fmt.Sprint(i)] == key {
			break
		}
		s.lastScheduleRun[fmt.Sprint(i)] = key
		if gate != nil {
			if rule.Action == "pause" {
				gate.Block("schedule")
			} else {
				gate.Unblock("schedule")
			}
		}
		break
	}
	periodic := control.PeriodicRule
	if gate != nil && periodic != nil && periodic.Enabled && periodic.OpenDurationMin > 0 && periodic.CloseDurationMin > 0 {
		if s.periodicSwitch.IsZero() {
			s.periodicOpen = true
			s.periodicSwitch = now.Add(time.Duration(periodic.OpenDurationMin) * time.Minute)
		}
		if !now.Before(s.periodicSwitch) {
			if s.periodicOpen {
				s.periodicOpen = false
				s.periodicSwitch = now.Add(time.Duration(periodic.CloseDurationMin) * time.Minute)
			} else {
				s.periodicOpen = true
				s.periodicSwitch = now.Add(time.Duration(periodic.OpenDurationMin) * time.Minute)
			}
		}
		if s.periodicOpen {
			gate.Unblock("periodic")
		} else {
			gate.Block("periodic")
		}
	} else if gate != nil {
		gate.Unblock("periodic")
	}
	s.mu.Unlock()
}

func (s *FundingCarryStrategy) openingIsBlocked() bool {
	s.mu.RLock()
	gate := s.openingGate
	s.mu.RUnlock()
	return gate != nil && gate.Blocked()
}

func (s *FundingCarryStrategy) acquireOperation(ctx context.Context) error {
	if s.operationGate == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.operationGate:
		return nil
	}
}

func (s *FundingCarryStrategy) releaseOperation() {
	if s.operationGate != nil {
		s.operationGate <- struct{}{}
	}
}

// CloseOwned serializes a manual full-close against rebalancing, then delegates
// leg sizing and debt repayment to the strategy's ownership-aware close paths.
func (s *FundingCarryStrategy) CloseOwned(ctx context.Context) (float64, error) {
	s.mu.RLock()
	gate := s.openingGate
	s.mu.RUnlock()
	if gate != nil {
		gate.Block("manual_close")
		defer gate.Unblock("manual_close")
		if err := gate.Drain(ctx); err != nil {
			return 0, fmt.Errorf("wait for opening submissions before manual close: %w", err)
		}
	}
	if err := s.acquireOperation(ctx); err != nil {
		return 0, err
	}
	defer s.releaseOperation()
	if err := s.syncPositions(ctx); err != nil {
		return 0, fmt.Errorf("manual close ownership preflight: %w", err)
	}
	s.mu.RLock()
	direction := s.direction
	quantity := s.futQty + s.strategySpotQty + s.marginDebt
	s.mu.RUnlock()
	var err error
	switch direction {
	case DirectionForward:
		err = s.closeAllWithAccountWalletCoordination(ctx, "manual")
	case DirectionReverse:
		err = s.closeReverseWithAccountWalletCoordination(ctx, "manual")
	case DirectionNone:
		return 0, errors.New("funding_carry has no strategy-owned position to close")
	default:
		return 0, s.blockOnUnownedExposure(fmt.Errorf("invalid manual close direction %d", direction))
	}
	if err != nil {
		return quantity, err
	}
	if err := s.syncPositions(ctx); err != nil {
		return quantity, fmt.Errorf("manual close postflight: %w", err)
	}
	return quantity, nil
}

func (s *FundingCarryStrategy) openHedge(ctx context.Context, futPx, spotPx, rate float64) error {
	return s.withAccountWalletCoordination(ctx, func(operationCtx context.Context) error {
		return s.openHedgeUnderWalletLock(operationCtx, futPx, spotPx, rate)
	})
}

func (s *FundingCarryStrategy) openHedgeUnderWalletLock(ctx context.Context, futPx, spotPx, rate float64) (resultErr error) {
	if rate < s.minFundingRate {
		return fmt.Errorf("8-hour funding rate %.8f is below configured opening minimum %.8f", rate, s.minFundingRate)
	}
	fundingInfo, err := s.fut.GetFundingInfo(ctx, s.symbol)
	if err != nil {
		return fmt.Errorf("refresh funding info before forward opening: %w", err)
	}
	currentRate, err := fundingCarryNormalizedOpeningRate(fundingInfo, s.symbol)
	if err != nil {
		return fmt.Errorf("normalize refreshed funding rate before forward opening: %w", err)
	}
	if currentRate < s.minFundingRate {
		return fmt.Errorf("refreshed 8-hour funding rate %.8f is below configured opening minimum %.8f", currentRate, s.minFundingRate)
	}
	feeRate, err := fundingCarryConfiguredFeeRate(s.cfg, s.symCfg.Exchange)
	if err != nil {
		return err
	}
	if err := validateFundingCarryFeeRecovery(currentRate, feeRate, s.maxFeeRecoveryDays, 0); err != nil {
		return fmt.Errorf("validate forward funding carry fee recovery: %w", err)
	}
	cap, err := s.capitalWithinOpeningLimits(futPx, spotPx)
	if err != nil {
		return err
	}
	if cap < 200 {
		return fmt.Errorf("分配資金 %.2f USDT 過小，建議 ≥200 USDT", cap)
	}
	legNotional := cap / 2
	if legNotional < 100 {
		return fmt.Errorf("單腿名義 %.2f USDT 低於合約最小要求", legNotional)
	}
	qty := legNotional / spotPx
	qty = s.roundQty(qty, s.spot.GetQuantityDecimals())
	if qty <= 0 {
		return fmt.Errorf("現貨買入數量精度截斷為 0")
	}
	plannedFuturesQty := s.roundQty(qty, s.fut.GetQuantityDecimals())
	if plannedFuturesQty <= 0 {
		return fmt.Errorf("合約開倉數量精度截斷為 0")
	}
	buyPrice := s.roundPrice(spotPx*(1+maxCarrySpotOpenSlippage), s.spot.GetPriceDecimals())
	futuresSellLimit := s.roundPrice(futPx*(1-maxCarryOpenSlippage), s.fut.GetPriceDecimals())
	roundTripBookCost, err := fundingCarryRoundTripOrderBookCost(ctx, s.symbol, s.spot, s.fut, qty, plannedFuturesQty)
	if err != nil {
		return err
	}
	spotEntrySlippage, err := fundingCarryWorstCaseEntrySlippageCost(qty, spotPx, buyPrice, exchange.SideBuy)
	if err != nil {
		return err
	}
	futuresEntrySlippage, err := fundingCarryWorstCaseEntrySlippageCost(plannedFuturesQty, futPx, futuresSellLimit, exchange.SideSell)
	if err != nil {
		return err
	}
	roundTripBookCost += spotEntrySlippage + futuresEntrySlippage
	spotNotional := qty * spotPx
	futuresNotional := plannedFuturesQty * futPx
	if err := validateFundingCarryExecutionCostRecovery(currentRate, feeRate, s.maxFeeRecoveryDays, 0, spotNotional, futuresNotional, roundTripBookCost); err != nil {
		return fmt.Errorf("validate forward funding carry execution-cost recovery: %w", err)
	}
	spotBuyReserve, err := fundingCarrySpotBuyReserve(qty, buyPrice, feeRate)
	if err != nil {
		return err
	}
	if err := s.ensureFuturesMargin(ctx, legNotional, spotBuyReserve); err != nil {
		return fmt.Errorf("ensure futures opening margin: %w", err)
	}
	if err := s.beginRuntimeIntent(ctx); err != nil {
		return fmt.Errorf("persist spot/futures opening intent: %w", err)
	}
	opened := false
	defer func() {
		if err := s.finishRuntimeIntent(ctx, opened); err != nil {
			logger.Error("[%s] persist funding_carry opening result: %v", s.symbol, err)
			resultErr = errors.Join(resultErr, err)
		}
	}()

	spotOrder, err := s.placeOrder(ctx, s.spot, s.spotExecutor, &exchange.OrderRequest{
		Symbol:        s.symbol,
		Side:          exchange.SideBuy,
		Type:          exchange.OrderTypeLimit,
		Quantity:      qty,
		Price:         buyPrice,
		PriceDecimals: s.spot.GetPriceDecimals(),
		StrategyType:  "funding_carry",
	})
	if err != nil {
		s.publishEvent(event.EventTypeOrderFailed, map[string]interface{}{
			"side": "spot_buy", "error": err.Error(), "message": "資金費套利現貨買入失敗",
		})
		return fmt.Errorf("現貨買入: %w", err)
	}

	filledQty, spotFillErr := s.waitOrderFill(ctx, s.spot, spotOrder.OrderID, orderWaitTimeout)
	if spotFillErr != nil || filledQty <= 0 {
		if spotFillErr != nil {
			return s.blockOnUnownedExposure(fmt.Errorf("spot order %d could not be brought to a verified terminal state: %w", spotOrder.OrderID, spotFillErr))
		}
		if err := settleCarryOrder(ctx, s.spotExecutor, spotOrder); err != nil {
			return s.blockOnUnownedExposure(fmt.Errorf("zero-fill spot order remains unresolved: %w", err))
		}
		opened = true
		s.publishEvent(event.EventTypeOrderFailed, map[string]interface{}{
			"side": "spot_buy", "order_id": spotOrder.OrderID, "message": "現貨買單已終結且無成交",
		})
		return fmt.Errorf("現貨買入訂單已終結且未成交")
	}

	// 現貨已成交即記入策略自身持倉（即使後續合約腿失敗，這部分幣也屬於策略，平倉時需賣出）
	s.recordStrategySpot(filledQty)
	s.mu.RLock()
	stateErr := s.runtimeStateErr
	s.mu.RUnlock()
	if stateErr != nil {
		return fmt.Errorf("spot filled but strategy-owned inventory could not be persisted: %w", stateErr)
	}
	if err := settleCarryOrder(ctx, s.spotExecutor, spotOrder); err != nil {
		return fmt.Errorf("persist spot fill ownership before futures leg: %w", err)
	}

	futQty := s.roundQty(filledQty, s.fut.GetQuantityDecimals())
	if futQty <= 0 {
		s.mu.Lock()
		s.direction, s.futQty = DirectionForward, 0
		stateErr := s.persistRuntimeStateLocked()
		s.mu.Unlock()
		if stateErr != nil {
			return s.blockOnUnownedExposure(fmt.Errorf("persist spot-only leg before unwind: %w", stateErr))
		}
		if err := s.closeAll(ctx, "futures_quantity_rounded_to_zero"); err != nil {
			return fmt.Errorf("futures quantity rounded to zero; spot unwind failed: %w", err)
		}
		opened = true
		return fmt.Errorf("futures quantity rounded to zero; spot leg was unwound")
	}

	futOrder, err := s.placeOrder(ctx, s.fut, s.futuresExecutor, &exchange.OrderRequest{
		Symbol:        s.symbol,
		Side:          exchange.SideSell,
		Type:          exchange.OrderTypeLimit,
		TimeInForce:   exchange.TimeInForceIOC,
		Quantity:      futQty,
		Price:         futuresSellLimit,
		PriceDecimals: s.fut.GetPriceDecimals(),
		StrategyType:  "funding_carry",
	})
	if err != nil {
		s.blockOnUnownedExposure(fmt.Errorf("futures open response is uncertain after spot purchase: %w", err))
		s.publishEvent(event.EventTypeRiskTriggered, map[string]interface{}{
			"side": "futures_sell", "error": err.Error(), "spot_qty": filledQty,
			"message": "合約開空失敗！現貨已買入，請立即手動處理",
		})
		return fmt.Errorf("合約開空失敗（現貨已買入 %.8f，存在裸多風險）: %w", filledQty, err)
	}
	if futOrder == nil || futOrder.ExecutedQty <= 0 {
		if err := settleCarryOrder(ctx, s.futuresExecutor, futOrder); err != nil {
			return s.blockOnUnownedExposure(fmt.Errorf("futures IOC has no fill and cannot be reconciled: %w", err))
		}
		s.mu.Lock()
		s.direction, s.futQty = DirectionForward, 0
		stateErr := s.persistRuntimeStateLocked()
		s.mu.Unlock()
		if stateErr != nil {
			return s.blockOnUnownedExposure(fmt.Errorf("persist spot-only residual before unwind: %w", stateErr))
		}
		if err := s.closeAll(ctx, "futures_ioc_unfilled"); err != nil {
			return err
		}
		opened = true
		return fmt.Errorf("futures IOC was not filled; strategy spot leg was unwound")
	}
	if err := s.recordFuturesOpening(futOrder, exchange.SideSell, futQty); err != nil {
		return fmt.Errorf("現貨已買入但合約成交歸屬未核實: %w", err)
	}
	if err := settleCarryOrder(ctx, s.futuresExecutor, futOrder); err != nil {
		return fmt.Errorf("persist futures fill ownership: %w", err)
	}
	if futOrder.ExecutedQty+s.roundingTolerance(s.fut.GetQuantityDecimals()) < futQty {
		if err := s.closeAll(ctx, "partial_futures_ioc"); err != nil {
			return fmt.Errorf("partial futures IOC filled %.8f of %.8f and compensating close failed: %w", futOrder.ExecutedQty, futQty, err)
		}
		opened = true
		return fmt.Errorf("partial futures IOC was compensated; carry leg not opened")
	}
	opened = true

	logger.Info("✅ [%s] 正向對沖已建立 spot=%.8f fut_short=%.8f rate=%.5f", s.symbol, filledQty, futQty, rate)
	s.publishEvent(event.EventTypePositionOpened, map[string]interface{}{
		"direction": "forward", "spot_qty": filledQty, "fut_qty": futQty,
		"funding_rate": rate, "message": fmt.Sprintf("正向開倉: %s spot=%.6f short=%.6f rate=%.5f", s.symbol, filledQty, futQty, rate),
	})
	return nil
}

// ---------------------------------------------------------------------------
// Reverse: open reverse hedge (borrow + sell spot + long futures)
// ---------------------------------------------------------------------------

func (s *FundingCarryStrategy) openReverseHedge(ctx context.Context, futPx, spotPx, rate float64) error {
	return s.withAccountWalletCoordination(ctx, func(operationCtx context.Context) error {
		return s.openReverseHedgeUnderWalletLock(operationCtx, futPx, spotPx, rate)
	})
}

func (s *FundingCarryStrategy) closeAllWithAccountWalletCoordination(ctx context.Context, reason string) error {
	return s.withAccountWalletCoordination(ctx, func(operationCtx context.Context) error {
		return s.closeAll(operationCtx, reason)
	})
}

func (s *FundingCarryStrategy) closeReverseWithAccountWalletCoordination(ctx context.Context, reason string) error {
	return s.withAccountWalletCoordination(ctx, func(operationCtx context.Context) error {
		return s.closeReverse(operationCtx, reason)
	})
}

func (s *FundingCarryStrategy) closeStrategySpotWithAccountWalletCoordination(ctx context.Context) error {
	return s.withAccountWalletCoordination(ctx, func(operationCtx context.Context) error {
		return s.closeStrategySpot(operationCtx)
	})
}

func (s *FundingCarryStrategy) openReverseHedgeUnderWalletLock(ctx context.Context, futPx, spotPx, rate float64) (resultErr error) {
	if s.marginEx == nil {
		return fmt.Errorf("反向套利需要保證金帳戶，但 marginEx 為 nil")
	}
	if rate > -s.reverseMinRate {
		return fmt.Errorf("8-hour negative funding rate %.8f is above configured reverse minimum -%.8f", rate, s.reverseMinRate)
	}

	cap, err := s.capitalWithinOpeningLimits(futPx, spotPx)
	if err != nil {
		return err
	}
	if cap < 200 {
		return fmt.Errorf("分配資金 %.2f USDT 過小", cap)
	}
	legNotional := cap / 2
	marginBalance, err := s.marginEx.GetBalance(ctx, "USDT")
	if err != nil {
		return fmt.Errorf("query spot-margin USDT balance before reverse opening: %w", err)
	}
	if err := validateFundingCarryMarginBalance(marginBalance, legNotional); err != nil {
		return err
	}

	base := s.spot.GetBaseAsset()
	rateProvider, ok := s.marginEx.(exchange.MarginBorrowRateProvider)
	if !ok {
		return fmt.Errorf("margin borrow rate provider is unavailable; refusing reverse opening")
	}
	hourlyRate, err := rateProvider.GetNextHourlyBorrowRate(ctx, base)
	if err != nil {
		return fmt.Errorf("query %s margin borrow rate before reverse opening: %w", base, err)
	}
	dailyRateEstimate := hourlyRate * 24
	if math.IsNaN(dailyRateEstimate) || math.IsInf(dailyRateEstimate, 0) || hourlyRate < 0 || dailyRateEstimate > s.marginInterestMax {
		return fmt.Errorf("%s estimated daily margin interest %.8f exceeds configured maximum %.8f", base, dailyRateEstimate, s.marginInterestMax)
	}
	fundingInfo, err := s.fut.GetFundingInfo(ctx, s.symbol)
	if err != nil {
		return fmt.Errorf("query funding interval before reverse opening: %w", err)
	}
	currentRate, err := fundingCarryNormalizedOpeningRate(fundingInfo, s.symbol)
	if err != nil {
		return fmt.Errorf("normalize refreshed funding rate before reverse opening: %w", err)
	}
	if currentRate > -s.reverseMinRate {
		return fmt.Errorf("refreshed 8-hour negative funding rate %.8f is above configured reverse minimum -%.8f", currentRate, s.reverseMinRate)
	}
	if err := validateFundingCarryReverseNetRate(fundingInfo, s.symbol, hourlyRate); err != nil {
		return fmt.Errorf("validate reverse funding economics: %w", err)
	}
	feeRate, err := fundingCarryConfiguredFeeRate(s.cfg, s.symCfg.Exchange)
	if err != nil {
		return err
	}
	borrowQty := legNotional / spotPx
	borrowQty = s.roundQty(borrowQty, s.spot.GetQuantityDecimals())
	if borrowQty <= 0 {
		return fmt.Errorf("借幣數量太小")
	}
	plannedFuturesQty := s.roundQty(borrowQty, s.fut.GetQuantityDecimals())
	if plannedFuturesQty <= 0 {
		return fmt.Errorf("合約開倉數量精度截斷為 0")
	}
	spotSellLimit := s.roundPrice(spotPx*(1-maxCarrySpotOpenSlippage), s.spot.GetPriceDecimals())
	futuresBuyLimit := s.roundPrice(futPx*(1+maxCarryOpenSlippage), s.fut.GetPriceDecimals())
	roundTripBookCost, err := fundingCarryRoundTripOrderBookCost(ctx, s.symbol, s.marginEx, s.fut, borrowQty, plannedFuturesQty)
	if err != nil {
		return err
	}
	spotEntrySlippage, err := fundingCarryWorstCaseEntrySlippageCost(borrowQty, spotPx, spotSellLimit, exchange.SideSell)
	if err != nil {
		return err
	}
	futuresEntrySlippage, err := fundingCarryWorstCaseEntrySlippageCost(plannedFuturesQty, futPx, futuresBuyLimit, exchange.SideBuy)
	if err != nil {
		return err
	}
	roundTripBookCost += spotEntrySlippage + futuresEntrySlippage
	spotNotional := borrowQty * spotPx
	futuresNotional := plannedFuturesQty * futPx
	if err := validateFundingCarryExecutionCostRecovery(currentRate, feeRate, s.maxFeeRecoveryDays, hourlyRate, spotNotional, futuresNotional, roundTripBookCost); err != nil {
		return fmt.Errorf("validate reverse funding carry fee recovery: %w", err)
	}
	if err := s.ensureFuturesMargin(ctx, legNotional, 0); err != nil {
		return fmt.Errorf("ensure futures opening margin: %w", err)
	}
	// Step 1: 借幣
	if err := s.beginRuntimeIntent(ctx); err != nil {
		return fmt.Errorf("persist margin/futures opening intent: %w", err)
	}
	opened := false
	borrowUnresolved := true
	defer func() {
		if borrowUnresolved {
			// An accepted or uncertain borrow is still a recovery operation.
			// Its intent must not be cleared by an ordinary error return.
			return
		}
		if err := s.finishRuntimeIntent(ctx, opened); err != nil {
			logger.Error("[%s] persist funding_carry reverse opening result: %v", s.symbol, err)
			resultErr = errors.Join(resultErr, err)
		}
	}()

	borrowTransferID, err := s.marginEx.Borrow(ctx, base, borrowQty)
	if borrowTransferID > 0 {
		ackErr := s.recordMarginBorrowAcknowledgement(ctx, borrowTransferID)
		if ackErr != nil {
			return s.blockOnUnownedExposure(errors.Join(err, ackErr))
		}
	}
	if err != nil {
		s.blockOnUnownedExposure(fmt.Errorf("margin borrow result is uncertain: %w", err))
		s.publishEvent(event.EventTypeOrderFailed, map[string]interface{}{
			"side": "margin_borrow", "error": err.Error(), "message": "借幣失敗",
		})
		return fmt.Errorf("借幣 %s: %w", base, err)
	}
	if borrowTransferID <= 0 {
		return s.blockOnUnownedExposure(fmt.Errorf("margin borrow acknowledgement has no positive identity"))
	}
	borrowEvent, verifyErr := s.confirmMarginDebtTransaction(ctx, "borrow", borrowTransferID, base, borrowQty)
	if verifyErr != nil {
		return s.blockOnUnownedExposure(fmt.Errorf("verify margin borrow transaction: %w", verifyErr))
	}
	s.mu.Lock()
	if err := s.verifyDebtCommitLocked(ctx); err != nil {
		s.mu.Unlock()
		return s.blockOnUnownedExposure(err)
	}
	if replay, identityErr := s.debtEventReplayLocked(borrowEvent); replay || identityErr != nil {
		s.mu.Unlock()
		return s.blockOnUnownedExposure(fmt.Errorf("new margin borrow returned a reused or conflicting transaction identity %d", borrowTransferID))
	}
	s.direction = DirectionReverse
	s.marginDebt = borrowEvent.Principal
	s.marginBorrowTransferID = borrowTransferID
	s.marginBorrowedAt = borrowEvent.OccurredAt
	s.marginDebtEvents = append(s.marginDebtEvents, borrowEvent)
	persistErr := s.persistRuntimeStateLocked()
	s.mu.Unlock()
	if persistErr != nil {
		return s.blockOnUnownedExposure(fmt.Errorf("persist borrowed margin exposure: %w", persistErr))
	}
	borrowUnresolved = false

	// Step 2: 保證金帳戶賣出（用 marginEx，它 embed 了 IExchange）
	sellOrder, err := s.placeOrder(ctx, s.marginEx, s.marginExecutor, &exchange.OrderRequest{
		Symbol:        s.symbol,
		Side:          exchange.SideSell,
		Type:          exchange.OrderTypeLimit,
		Quantity:      borrowQty,
		Price:         spotSellLimit,
		PriceDecimals: s.spot.GetPriceDecimals(),
		StrategyType:  "funding_carry_reverse",
	})
	if err != nil {
		if errors.Is(err, execution.ErrOrderUnknown) {
			return s.blockOnUnownedExposure(fmt.Errorf("margin sell submission outcome is unknown; borrowed amount retained: %w", err))
		}
		repayErr := s.repayMarginPrincipal(ctx, base, borrowQty, 0)
		if repayErr != nil {
			return s.blockOnUnownedExposure(fmt.Errorf("margin sell was definitively rejected but borrowed amount could not be returned: %w", repayErr))
		}
		s.mu.Lock()
		s.direction, s.marginDebt = DirectionNone, 0
		s.marginBorrowTransferID, s.marginBorrowedAt = 0, time.Time{}
		stateErr := s.persistRuntimeStateLocked()
		s.mu.Unlock()
		if stateErr != nil {
			return s.blockOnUnownedExposure(fmt.Errorf("persist state after rejected margin sell: %w", stateErr))
		}
		opened = true
		s.publishEvent(event.EventTypeOrderFailed, map[string]interface{}{
			"side": "margin_sell", "error": err.Error(), "message": "借幣後賣出被拒，已歸還借幣",
		})
		return fmt.Errorf("保證金賣出: %w", err)
	}

	filledQty, fillErr := s.waitOrderFill(ctx, s.marginEx, sellOrder.OrderID, orderWaitTimeout)
	if fillErr != nil || filledQty <= 0 {
		if fillErr == nil && filledQty <= 0 {
			if err := settleCarryOrder(ctx, s.marginExecutor, sellOrder); err != nil {
				return s.blockOnUnownedExposure(fmt.Errorf("zero-fill margin order remains unresolved: %w", err))
			}
			if err := s.repayMarginPrincipal(ctx, base, borrowQty, 0); err != nil {
				return s.blockOnUnownedExposure(fmt.Errorf("margin sell had no fill; borrowed amount repayment failed: %w", err))
			}
			s.mu.Lock()
			s.direction, s.marginDebt = DirectionNone, 0
			s.marginBorrowTransferID, s.marginBorrowedAt = 0, time.Time{}
			stateErr := s.persistRuntimeStateLocked()
			s.mu.Unlock()
			if stateErr != nil {
				return s.blockOnUnownedExposure(fmt.Errorf("persist zero-exposure reverse state: %w", stateErr))
			}
			opened = true
		} else {
			cancelErr := cancelCarryOrder(ctx, s.marginEx, s.marginExecutor, s.symbol, sellOrder.OrderID)
			if cancelErr != nil {
				return s.blockOnUnownedExposure(fmt.Errorf("margin sell outcome unresolved and cancel failed: %w", cancelErr))
			}
		}
		s.publishEvent(event.EventTypeOrderFailed, map[string]interface{}{
			"side": "margin_sell", "message": "保證金賣出未完成，停止反向開倉",
		})
		return fmt.Errorf("保證金賣出未能核實足額成交: %w", fillErr)
	}
	if filledQty+s.roundingTolerance(s.spot.GetQuantityDecimals()) < borrowQty {
		unusedBorrow := s.roundQty(borrowQty-filledQty, s.spot.GetQuantityDecimals())
		if unusedBorrow > 0 {
			if err := s.repayMarginPrincipal(ctx, base, unusedBorrow, filledQty); err != nil {
				return s.blockOnUnownedExposure(fmt.Errorf("margin sell partially filled %.8f; return unused borrowed amount %.8f: %w", filledQty, unusedBorrow, err))
			}
		}
	}
	if err := settleCarryOrder(ctx, s.marginExecutor, sellOrder); err != nil {
		return fmt.Errorf("persist margin sell fill ownership: %w", err)
	}

	// Step 3: 合約開多
	futQty := s.roundQty(filledQty, s.fut.GetQuantityDecimals())
	if futQty <= 0 {
		if err := s.closeReverse(ctx, "futures_quantity_rounded_to_zero"); err != nil {
			return fmt.Errorf("futures quantity rounded to zero; reverse leg unwind failed: %w", err)
		}
		opened = true
		return fmt.Errorf("futures quantity rounded to zero; borrowed short leg was unwound")
	}

	futOrder, err := s.placeOrder(ctx, s.fut, s.futuresExecutor, &exchange.OrderRequest{
		Symbol:        s.symbol,
		Side:          exchange.SideBuy,
		Type:          exchange.OrderTypeLimit,
		TimeInForce:   exchange.TimeInForceIOC,
		Quantity:      futQty,
		Price:         futuresBuyLimit,
		PriceDecimals: s.fut.GetPriceDecimals(),
		StrategyType:  "funding_carry_reverse",
	})
	if err != nil {
		s.blockOnUnownedExposure(fmt.Errorf("futures open response is uncertain after margin short sale: %w", err))
		s.publishEvent(event.EventTypeRiskTriggered, map[string]interface{}{
			"side": "futures_buy", "error": err.Error(), "sold_qty": filledQty,
			"message": "合約開多失敗！借幣已賣出，存在裸空風險",
		})
		return fmt.Errorf("合約開多失敗（借幣已賣出 %.8f，裸空風險）: %w", filledQty, err)
	}
	if futOrder == nil || futOrder.ExecutedQty <= 0 {
		if err := settleCarryOrder(ctx, s.futuresExecutor, futOrder); err != nil {
			return s.blockOnUnownedExposure(fmt.Errorf("reverse futures IOC has no fill and cannot be reconciled: %w", err))
		}
		s.mu.Lock()
		s.futQty = 0
		stateErr := s.persistRuntimeStateLocked()
		s.mu.Unlock()
		if stateErr != nil {
			return s.blockOnUnownedExposure(fmt.Errorf("persist margin-only residual before unwind: %w", stateErr))
		}
		if err := s.closeReverse(ctx, "reverse_futures_ioc_unfilled"); err != nil {
			return err
		}
		opened = true
		return fmt.Errorf("reverse futures IOC was not filled; borrowed short leg was unwound")
	}
	if err := s.recordFuturesOpening(futOrder, exchange.SideBuy, futQty); err != nil {
		return fmt.Errorf("借幣賣出後合約成交歸屬未核實: %w", err)
	}
	if err := settleCarryOrder(ctx, s.futuresExecutor, futOrder); err != nil {
		return fmt.Errorf("persist reverse futures fill ownership: %w", err)
	}
	if futOrder.ExecutedQty+s.roundingTolerance(s.fut.GetQuantityDecimals()) < futQty {
		if err := s.closeReverse(ctx, "partial_reverse_futures_ioc"); err != nil {
			return fmt.Errorf("partial reverse futures IOC filled %.8f of %.8f and compensating close failed: %w", futOrder.ExecutedQty, futQty, err)
		}
		opened = true
		return fmt.Errorf("partial reverse futures IOC was compensated; carry leg not opened")
	}
	opened = true

	logger.Info("✅ [%s] 反向對沖已建立 borrow=%.8f fut_long=%.8f rate=%.5f", s.symbol, filledQty, futQty, rate)
	s.publishEvent(event.EventTypePositionOpened, map[string]interface{}{
		"direction": "reverse", "borrow_qty": filledQty, "fut_qty": futQty,
		"funding_rate": rate, "message": fmt.Sprintf("反向開倉: %s borrow=%.6f long=%.6f rate=%.5f", s.symbol, filledQty, futQty, rate),
	})
	return nil
}

// ---------------------------------------------------------------------------
// Close positions
// ---------------------------------------------------------------------------

func (s *FundingCarryStrategy) closeAll(ctx context.Context, reason string) (resultErr error) {
	pos, err := readScopedPositionSnapshot(ctx, s.fut, s.symbol)
	if err != nil {
		return s.blockOnUnownedExposure(fmt.Errorf("fut.GetPositions before close: %w", err))
	}
	var futShort, futLong float64
	for _, p := range pos {
		if p == nil || math.IsNaN(p.Size) || math.IsInf(p.Size, 0) {
			return s.blockOnUnownedExposure(errors.New("futures close snapshot contains invalid data"))
		}
		if p.Size < 0 {
			futShort += math.Abs(p.Size)
		} else if p.Size > 0 {
			futLong += p.Size
		}
	}
	s.mu.RLock()
	ownedFut, dir := s.futQty, s.direction
	s.mu.RUnlock()
	tolerance := s.roundingTolerance(s.fut.GetQuantityDecimals())
	if futLong > tolerance || math.Abs(futShort-ownedFut) > tolerance || (ownedFut > tolerance && dir != DirectionForward) {
		return s.blockOnUnownedExposure(fmt.Errorf("refusing futures close: exchange short=%.8f long=%.8f, strategy owns %.8f (%s)", futShort, futLong, ownedFut, dir))
	}
	if err := s.beginRuntimeIntent(ctx); err != nil {
		return fmt.Errorf("persist funding_carry close intent: %w", err)
	}
	closed := false
	defer func() {
		if err := s.finishRuntimeIntent(ctx, closed); err != nil {
			logger.Error("[%s] persist funding_carry close result: %v", s.symbol, err)
			resultErr = errors.Join(resultErr, err)
		}
	}()
	if ownedFut > tolerance {
		order, closeErr := s.placeOrder(ctx, s.fut, s.futuresExecutor, &exchange.OrderRequest{
			Symbol: s.symbol, Side: exchange.SideBuy, Type: exchange.OrderTypeMarket,
			Quantity: ownedFut, ReduceOnly: true, PriceDecimals: s.fut.GetPriceDecimals(),
			StrategyType: "funding_carry",
		})
		if closeErr != nil || order == nil || order.ExecutedQty+tolerance < ownedFut {
			return s.blockOnUnownedExposure(fmt.Errorf("futures close acknowledgement is incomplete (requested=%.8f, order=%+v, err=%v)", ownedFut, order, closeErr))
		}
		remaining, err := readScopedPositionSnapshot(ctx, s.fut, s.symbol)
		if err != nil {
			return s.blockOnUnownedExposure(fmt.Errorf("verify futures close: %w", err))
		}
		for _, p := range remaining {
			if p == nil || math.IsNaN(p.Size) || math.IsInf(p.Size, 0) || math.Abs(p.Size) > tolerance {
				return s.blockOnUnownedExposure(fmt.Errorf("futures close left an unverified position: %+v", p))
			}
		}
		s.mu.Lock()
		s.futQty = 0
		s.mu.Unlock()
		if err := settleCarryOrder(ctx, s.futuresExecutor, order); err != nil {
			return fmt.Errorf("persist futures close reconciliation: %w", err)
		}
	}
	if err := s.closeStrategySpot(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	if s.strategySpotQty <= tolerance && s.futQty <= tolerance {
		s.direction = DirectionNone
	}
	s.mu.Unlock()
	closed = true
	logger.Info("✅ [%s] 正向平倉完成 reason=%s", s.symbol, reason)
	s.publishEvent(event.EventTypePositionClosed, map[string]interface{}{
		"direction": "forward", "reason": reason,
		"message": fmt.Sprintf("正向平倉: %s (%s)", s.symbol, reason),
	})
	return nil
}

// recordStrategySpot 記錄策略自身買入的現貨數量
func (s *FundingCarryStrategy) recordStrategySpot(qty float64) {
	if qty <= 0 {
		return
	}
	s.mu.Lock()
	s.strategySpotQty += qty
	s.strategySpotKnown = true
	s.spotQty = s.strategySpotQty
	if err := s.persistRuntimeStateLocked(); err != nil {
		s.runtimeStateErr = err
		s.unownedExposure = true
	}
	s.mu.Unlock()
}

// releaseStrategySpot 賣出成交後扣減策略自身現貨記賬
func (s *FundingCarryStrategy) releaseStrategySpot(qty float64) {
	if qty <= 0 {
		return
	}
	s.mu.Lock()
	s.strategySpotQty = math.Max(0, s.strategySpotQty-qty)
	s.spotQty = s.strategySpotQty
	if err := s.persistRuntimeStateLocked(); err != nil {
		s.runtimeStateErr = err
		s.unownedExposure = true
	}
	s.mu.Unlock()
}

// closeStrategySpot 只賣出策略自身記賬的現貨數量（不超過當前餘額），不動用戶原有持幣
func (s *FundingCarryStrategy) closeStrategySpot(ctx context.Context) error {
	s.mu.RLock()
	recorded := s.strategySpotQty
	s.mu.RUnlock()
	if recorded <= 0 {
		return nil
	}

	base := s.spot.GetBaseAsset()
	bal, err := s.spot.GetBalance(ctx, base)
	if err != nil {
		return s.blockOnUnownedExposure(fmt.Errorf("spot.GetBalance(%s) before close: %w", base, err))
	}
	if err := ctx.Err(); err != nil {
		return s.blockOnUnownedExposure(fmt.Errorf("spot balance for %s completed after close context ended: %w", base, err))
	}
	tolerance := s.roundingTolerance(s.spot.GetQuantityDecimals())
	if bal < 0 || math.IsNaN(bal) || math.IsInf(bal, 0) || bal+tolerance < recorded {
		return s.blockOnUnownedExposure(fmt.Errorf("strategy-owned spot inventory %.8f does not reconcile with exchange balance %.8f", recorded, bal))
	}
	qty := s.roundQty(recorded, s.spot.GetQuantityDecimals())
	if qty <= 0 {
		return nil
	}

	px, err := s.spot.GetLatestPrice(ctx, s.symbol)
	if err != nil {
		return fmt.Errorf("現貨賣出取價 %s: %w", s.symbol, err)
	}
	sellPrice := s.roundPrice(px*closeSpotSellPriceFactor, s.spot.GetPriceDecimals())
	if sellPrice <= 0 {
		return fmt.Errorf("現貨賣出價格無效 %s: price=%.8f", s.symbol, sellPrice)
	}

	order, err := s.placeOrder(ctx, s.spot, s.spotExecutor, &exchange.OrderRequest{
		Symbol: s.symbol, Side: exchange.SideSell, Type: exchange.OrderTypeLimit,
		Quantity: qty, Price: sellPrice, PriceDecimals: s.spot.GetPriceDecimals(),
		StrategyType: "funding_carry",
	})
	if err != nil {
		s.publishEvent(event.EventTypeOrderFailed, map[string]interface{}{
			"side": "spot_sell", "error": err.Error(), "message": "現貨賣出失敗",
		})
		return s.blockOnUnownedExposure(fmt.Errorf("現貨賣出 qty=%.8f may have an unresolved execution: %w", qty, err))
	}

	filledQty, fillErr := s.waitOrderFill(ctx, s.spot, order.OrderID, orderWaitTimeout)
	if filledQty > 0 {
		s.releaseStrategySpot(filledQty)
	}
	if fillErr != nil || filledQty+tolerance < qty {
		if fillErr != nil {
			if cancelErr := cancelCarryOrder(ctx, s.spot, s.spotExecutor, s.symbol, order.OrderID); cancelErr != nil {
				logger.Warn("⚠️ [%s] 現貨賣單撤單失敗 orderID=%d: %v", s.symbol, order.OrderID, cancelErr)
				return s.blockOnUnownedExposure(fmt.Errorf("spot close order %d may still be live after cancel failed: %w", order.OrderID, cancelErr))
			}
			return s.blockOnUnownedExposure(fmt.Errorf("等待現貨賣出成交 orderID=%d: %w", order.OrderID, fillErr))
		}
		if settleErr := settleCarryOrder(ctx, s.spotExecutor, order); settleErr != nil {
			return s.blockOnUnownedExposure(fmt.Errorf("partial spot close is accounted but execution intent remains unresolved: %w", settleErr))
		}
		return s.blockOnUnownedExposure(fmt.Errorf("現貨賣出未完全成交 orderID=%d filled=%.8f requested=%.8f; residual ownership retained", order.OrderID, filledQty, qty))
	}
	if err := settleCarryOrder(ctx, s.spotExecutor, order); err != nil {
		return s.blockOnUnownedExposure(fmt.Errorf("spot close fill is confirmed but execution intent remains unresolved: %w", err))
	}
	return nil
}

func (s *FundingCarryStrategy) closeReverse(ctx context.Context, reason string) (resultErr error) {
	if s.marginEx == nil {
		return fmt.Errorf("marginEx is nil, cannot close reverse")
	}
	s.mu.RLock()
	ownedFutures, debt, direction := s.futQty, s.marginDebt, s.direction
	s.mu.RUnlock()
	if direction != DirectionReverse || (ownedFutures <= 0 && debt <= 0) {
		return s.blockOnUnownedExposure(errors.New("reverse close requested without complete strategy ownership record"))
	}
	tolerance := s.roundingTolerance(s.fut.GetQuantityDecimals())
	positions, err := readScopedPositionSnapshot(ctx, s.fut, s.symbol)
	if err != nil {
		return s.blockOnUnownedExposure(fmt.Errorf("read futures positions before reverse close: %w", err))
	}
	var liveLong, liveShort float64
	for _, p := range positions {
		if p == nil || math.IsNaN(p.Size) || math.IsInf(p.Size, 0) {
			return s.blockOnUnownedExposure(errors.New("invalid futures position snapshot before reverse close"))
		}
		if p.Size > 0 {
			liveLong += p.Size
		} else {
			liveShort += math.Abs(p.Size)
		}
	}
	if liveShort > tolerance || math.Abs(liveLong-ownedFutures) > tolerance {
		return s.blockOnUnownedExposure(fmt.Errorf("reverse futures position mismatch: live long %.8f, short %.8f, owned %.8f", liveLong, liveShort, ownedFutures))
	}
	marginPositions, err := readScopedPositionSnapshot(ctx, s.marginEx, s.symbol)
	if err != nil {
		return s.blockOnUnownedExposure(fmt.Errorf("read margin debt before reverse close: %w", err))
	}
	var liveDebt, livePrincipal, liveInterest float64
	for _, p := range marginPositions {
		if p == nil || math.IsNaN(p.Size) || math.IsInf(p.Size, 0) {
			return s.blockOnUnownedExposure(errors.New("invalid margin position snapshot before reverse close"))
		}
		if p.Size < 0 {
			liveDebt += math.Abs(p.Size)
			if !p.MarginDebtKnown || !finiteNonNegative(p.MarginBorrowed) || !finiteNonNegative(p.MarginInterest) {
				return s.blockOnUnownedExposure(errors.New("reverse margin liability has no authoritative principal/interest breakdown"))
			}
			if !fundingCarryFinancialAmountsMatch(math.Abs(p.Size), p.MarginBorrowed+p.MarginInterest) {
				return s.blockOnUnownedExposure(errors.New("reverse margin liability disagrees with principal/interest breakdown"))
			}
			livePrincipal += p.MarginBorrowed
			liveInterest += p.MarginInterest
		}
	}
	if !fundingCarryFinancialAmountsMatch(livePrincipal, debt) || !fundingCarryFinancialAmountsMatch(liveDebt, livePrincipal+liveInterest) {
		return s.blockOnUnownedExposure(fmt.Errorf("reverse margin principal mismatch: live principal %.8f, accrued interest %.8f, total %.8f, owned principal %.8f", livePrincipal, liveInterest, liveDebt, debt))
	}
	debtToRepay := livePrincipal + liveInterest
	if err := s.beginRuntimeIntent(ctx); err != nil {
		return fmt.Errorf("persist reverse close intent: %w", err)
	}
	closed := false
	defer func() {
		if err := s.finishRuntimeIntent(ctx, closed); err != nil {
			logger.Error("[%s] persist reverse close result: %v", s.symbol, err)
			resultErr = errors.Join(resultErr, err)
		}
	}()
	if ownedFutures > tolerance {
		order, err := s.placeOrder(ctx, s.fut, s.futuresExecutor, &exchange.OrderRequest{
			Symbol: s.symbol, Side: exchange.SideSell, Type: exchange.OrderTypeMarket,
			Quantity: ownedFutures, ReduceOnly: true, PriceDecimals: s.fut.GetPriceDecimals(),
			StrategyType: "funding_carry_reverse",
		})
		if err != nil || order == nil || order.ExecutedQty+tolerance < ownedFutures {
			return s.blockOnUnownedExposure(fmt.Errorf("reverse futures close incomplete: order=%+v err=%v", order, err))
		}
		remaining, err := readScopedPositionSnapshot(ctx, s.fut, s.symbol)
		if err != nil {
			return s.blockOnUnownedExposure(fmt.Errorf("verify reverse futures close: %w", err))
		}
		for _, p := range remaining {
			if p == nil || math.IsNaN(p.Size) || math.IsInf(p.Size, 0) || p.Size != 0 {
				return s.blockOnUnownedExposure(fmt.Errorf("reverse futures close left position: %+v", p))
			}
		}
		if err := s.checkpointReverseFuturesClosed(ctx, ownedFutures); err != nil {
			return s.blockOnUnownedExposure(err)
		}
		if err := settleCarryOrder(ctx, s.futuresExecutor, order); err != nil {
			return fmt.Errorf("persist reverse futures close reconciliation: %w", err)
		}
	}
	if debtToRepay > 0 {
		base := s.spot.GetBaseAsset()
		buyQty := s.roundQty(debtToRepay*1.002, s.spot.GetQuantityDecimals())
		if !validRuntimeAmount(buyQty) || buyQty <= 0 || (buyQty < debtToRepay && !fundingCarryFinancialAmountsMatch(buyQty, debtToRepay)) {
			return s.blockOnUnownedExposure(fmt.Errorf("margin debt %.12g cannot be covered at exchange quantity precision", debtToRepay))
		}
		price, err := s.spot.GetLatestPrice(ctx, s.symbol)
		if err != nil || price <= 0 || math.IsNaN(price) || math.IsInf(price, 0) {
			return fmt.Errorf("get valid price to cover margin debt: price=%.8f err=%v", price, err)
		}
		buyOrder, err := s.placeOrder(ctx, s.marginEx, s.marginExecutor, &exchange.OrderRequest{
			Symbol: s.symbol, Side: exchange.SideBuy, Type: exchange.OrderTypeLimit,
			Quantity: buyQty, Price: s.roundPrice(price*1.005, s.spot.GetPriceDecimals()),
			PriceDecimals: s.spot.GetPriceDecimals(), StrategyType: "funding_carry_reverse",
		})
		if buyOrder != nil && buyOrder.OrderID > 0 {
			if saveErr := s.checkpointMarginCoverOrder(ctx, buyOrder, buyQty, debtToRepay); saveErr != nil {
				return s.blockOnUnownedExposure(errors.Join(err, saveErr))
			}
		}
		if err != nil || buyOrder == nil {
			return s.blockOnUnownedExposure(fmt.Errorf("submit margin debt cover: order=%+v err=%v", buyOrder, err))
		}
		filled, fillErr := s.waitOrderFill(ctx, s.marginEx, buyOrder.OrderID, orderWaitTimeout)
		coverErr := validateFundingCarryDebtCover(filled, buyQty, debtToRepay)
		if fillErr != nil || coverErr != nil {
			if cancelErr := cancelCarryOrder(ctx, s.marginEx, s.marginExecutor, s.symbol, buyOrder.OrderID); cancelErr != nil {
				return s.blockOnUnownedExposure(fmt.Errorf("margin debt buyback incomplete (filled %.8f of %.8f); cancel failed: %w", filled, debt, cancelErr))
			}
			return s.blockOnUnownedExposure(fmt.Errorf("margin debt buyback incomplete: %w", errors.Join(fillErr, coverErr)))
		}
		if err := s.verifyMarginNetDebtCover(ctx, buyOrder.OrderID, filled, buyQty, debtToRepay); err != nil {
			return s.blockOnUnownedExposure(err)
		}
		if err := settleCarryOrder(ctx, s.marginExecutor, buyOrder); err != nil {
			return fmt.Errorf("persist margin buyback execution: %w", err)
		}
		s.mu.Lock()
		operationErr := s.verifyDebtCommitLocked(ctx)
		s.mu.Unlock()
		if operationErr != nil {
			return s.blockOnUnownedExposure(operationErr)
		}
		if err := s.repayMarginPrincipalWithCover(ctx, base, debtToRepay, 0, buyOrder.OrderID); err != nil {
			return s.blockOnUnownedExposure(fmt.Errorf("repay margin principal and interest %.8f %s: %w", debtToRepay, base, err))
		}
		marginPositions, err = readScopedPositionSnapshot(ctx, s.marginEx, s.symbol)
		if err != nil {
			return s.blockOnUnownedExposure(fmt.Errorf("verify margin repayment: %w", err))
		}
		for _, p := range marginPositions {
			if p == nil || math.IsNaN(p.Size) || math.IsInf(p.Size, 0) || p.Size < 0 || !p.MarginDebtKnown || !validRuntimeAmount(p.MarginBorrowed) || !validRuntimeAmount(p.MarginInterest) || p.MarginBorrowed > 0 || p.MarginInterest > 0 {
				return s.blockOnUnownedExposure(fmt.Errorf("margin repayment left unverified debt: %+v", p))
			}
		}
	}
	s.mu.Lock()
	if err := s.verifyDebtCommitLocked(ctx); err != nil {
		s.mu.Unlock()
		return s.blockOnUnownedExposure(err)
	}
	s.direction, s.futQty, s.marginDebt = DirectionNone, 0, 0
	s.marginBorrowTransferID, s.marginBorrowedAt = 0, time.Time{}
	if err := s.persistRuntimeStateLocked(); err != nil {
		s.unownedExposure = true
		s.runtimeStateErr = err
		s.mu.Unlock()
		return fmt.Errorf("persist flat reverse carry state: %w", err)
	}
	s.mu.Unlock()
	closed = true
	logger.Info("✅ [%s] 反向平倉完成 reason=%s", s.symbol, reason)
	s.publishEvent(event.EventTypePositionClosed, map[string]interface{}{
		"direction": "reverse", "reason": reason,
		"message": fmt.Sprintf("反向平倉: %s (%s)", s.symbol, reason),
	})
	return nil
}

// ---------------------------------------------------------------------------
// Order helpers
// ---------------------------------------------------------------------------

func (s *FundingCarryStrategy) waitOrderFill(ctx context.Context, ex exchange.IExchange, orderID int64, timeout time.Duration) (float64, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(orderPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-deadline.C:
			cancelErr := cancelCarryOrder(ctx, ex, s.executorFor(ex), s.symbol, orderID)
			verifyTimer := time.NewTimer(orderCancelVerifyTimeout)
			defer verifyTimer.Stop()
			for {
				o, err := ex.GetOrder(ctx, s.symbol, orderID)
				if err == nil && o != nil && carryTerminalOrder(o.Status) {
					return o.ExecutedQty, nil
				}
				select {
				case <-ctx.Done():
					return 0, ctx.Err()
				case <-verifyTimer.C:
					return 0, fmt.Errorf("order %d did not reach a verified terminal state after cancel (cancel error: %v)", orderID, cancelErr)
				case <-time.After(orderPollInterval):
				}
			}
		case <-ticker.C:
			o, err := ex.GetOrder(ctx, s.symbol, orderID)
			if err != nil {
				continue
			}
			if o == nil {
				continue
			}
			switch o.Status {
			case exchange.OrderStatusFilled:
				return o.ExecutedQty, nil
			case exchange.OrderStatusPartiallyFilled:
				continue
			case exchange.OrderStatusCanceled, exchange.OrderStatusRejected, exchange.OrderStatusExpired:
				if o.ExecutedQty > 0 {
					return o.ExecutedQty, nil
				}
				return 0, fmt.Errorf("訂單狀態 %s", o.Status)
			}
		}
	}
}

func carryTerminalOrder(status exchange.OrderStatus) bool {
	switch status {
	case exchange.OrderStatusFilled, exchange.OrderStatusCanceled, exchange.OrderStatusRejected, exchange.OrderStatusExpired:
		return true
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// Event & math helpers
// ---------------------------------------------------------------------------

func (s *FundingCarryStrategy) publishEvent(eventType event.EventType, data map[string]interface{}) {
	s.mu.RLock()
	bus := s.eventBus
	s.mu.RUnlock()
	if bus == nil {
		return
	}
	if data == nil {
		data = make(map[string]interface{})
	}
	data["strategy"] = "funding_carry"
	if _, ok := data["symbol"]; !ok {
		data["symbol"] = s.symbol
	}
	bus.Publish(&event.Event{
		Type:      eventType,
		Timestamp: time.Now(),
		Data:      data,
	})
}

func (s *FundingCarryStrategy) roundQty(q float64, decimals int) float64 {
	factor := math.Pow10(decimals)
	if factor <= 0 {
		return q
	}
	// 先乘後除並加微小容差：避免 0.5 被 Floor(q/1e-5)*1e-5 截成 0.49999000000000005
	return math.Floor(q*factor+roundQtyEpsilon) / factor
}

func (s *FundingCarryStrategy) roundPrice(p float64, decimals int) float64 {
	factor := math.Pow10(decimals)
	return math.Round(p*factor) / factor
}

func combineErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	msg := ""
	for i, e := range errs {
		if i > 0 {
			msg += "; "
		}
		msg += e.Error()
	}
	return fmt.Errorf("%s", msg)
}
