package replay

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"

	"quantmesh/backtest"
	"quantmesh/exchange"
	"quantmesh/position"
)

// 模擬交易所拒單錯誤：文案帶交易所錯誤碼，與 order.ExchangeOrderExecutor 的錯誤分類規則一致
var (
	errPostOnlyWouldCross = errors.New("code=-5022 Post Only order will be rejected (would immediately match)")
	errReduceOnlyRejected = errors.New("code=-2022 ReduceOnly Order is rejected")
	errMarginInsufficient = errors.New("code=-2019 Margin is insufficient")
	errInvalidOrder       = errors.New("invalid order")
)

const (
	// simExchangeName 模擬交易所名（無經紀商前綴，ClientOrderID 原樣返回）
	simExchangeName = "replay"
	// syntheticBookQty 合成盤口每檔數量（僅供 LiquidateAll 取價）
	syntheticBookQty = 1e12
	// statusNew 等 WebSocket 訂單狀態
	statusNew             = "NEW"
	statusPartiallyFilled = "PARTIALLY_FILLED"
	statusFilled          = "FILLED"
	statusCanceled        = "CANCELED"
	sideBuy               = "BUY"
	sideSell              = "SELL"
)

// restingOrder 掛單簿中的訂單
type restingOrder struct {
	id             int64
	clientOID      string
	side           string
	price          float64
	qty            float64
	filled         float64
	filledNotional float64
	reduceOnly     bool
	// queueAhead 觸價成交前需先消耗的排隊量（排隊模型）
	queueAhead float64
}

func (o *restingOrder) remaining() float64 { return o.qty - o.filled }

// counters 撮合統計
type counters struct {
	fills, makerFills, takerFills, partialFills      int
	makerVolume, totalVolume                         float64
	ordersPlaced, ordersCanceled                     int
	postOnlyRejects, postOnlyRepriced, postOnlyFinal int
	reduceOnlyRejects, marginRejects, closedGrids    int
}

// simExchange 模擬合約交易所：單一交易對、單向持倉（淨持倉）、USDT 計價。
// 同時實現 position.IExchange；所有方法並發安全（倉位管理器會在後台協程刷新帳戶）。
type simExchange struct {
	mu sync.Mutex

	symbol           string
	priceDecimals    int
	quantityDecimals int
	matching         MatchingConfig
	leverage         int
	enforceMargin    bool

	nextOrderID int64
	orders      map[int64]*restingOrder
	orderStates map[int64]position.OrderUpdate
	lastPrice   float64
	now         int64

	initialCapital float64
	realized       float64
	feesMaker      float64
	feesTaker      float64
	fundingPaid    float64
	netQty         float64
	avgEntry       float64

	pending []position.OrderUpdate
	trades  []backtest.Trade
	stats   counters
}

func newSimExchange(cfg Config) *simExchange {
	return &simExchange{
		symbol:           cfg.Bot.Trading.Symbol,
		priceDecimals:    cfg.PriceDecimals,
		quantityDecimals: cfg.QuantityDecimals,
		matching:         cfg.Matching,
		leverage:         cfg.Leverage,
		enforceMargin:    cfg.EnforceMargin,
		orders:           make(map[int64]*restingOrder),
		orderStates:      make(map[int64]position.OrderUpdate),
		initialCapital:   cfg.InitialCapital,
	}
}

func (ex *simExchange) tick() float64 { return math.Pow10(-ex.priceDecimals) }

func (ex *simExchange) priceEps() float64 { return ex.tick() / 2 }

// ===== 撮合 =====

// setMarket 更新最新價與模擬時間（不撮合）
func (ex *simExchange) setMarket(ts int64, price float64) {
	ex.mu.Lock()
	ex.now = ts
	if price > 0 {
		ex.lastPrice = price
	}
	ex.mu.Unlock()
}

// placeLimit 提交一筆限價單（價格已由執行器決定）。
// PostOnly 單若在提交時會與最新成交價交叉（BUY ≥ 最新價 / SELL ≤ 最新價）則拒單；
// 非 PostOnly 交叉單按最新價以 taker 立即全部成交；其餘進入掛單簿。
func (ex *simExchange) placeLimit(req *position.OrderRequest, price float64) (*position.Order, error) {
	if req == nil || req.Quantity <= 0 || price <= 0 {
		return nil, fmt.Errorf("place %v: %w", req, errInvalidOrder)
	}
	side := strings.ToUpper(req.Side)
	if side != sideBuy && side != sideSell {
		return nil, fmt.Errorf("place side=%q: %w", req.Side, errInvalidOrder)
	}
	ex.mu.Lock()
	defer ex.mu.Unlock()

	eps := ex.priceEps()
	crosses := ex.lastPrice > 0 &&
		((side == sideBuy && price >= ex.lastPrice-eps) || (side == sideSell && price <= ex.lastPrice+eps))
	if req.PostOnly && crosses {
		ex.stats.postOnlyRejects++
		return nil, fmt.Errorf("%s %s %.*f last=%.*f: %w", ex.symbol, side, ex.priceDecimals, price, ex.priceDecimals, ex.lastPrice, errPostOnlyWouldCross)
	}
	if req.ReduceOnly && !ex.reducesPositionLocked(side, req.Quantity) {
		ex.stats.reduceOnlyRejects++
		return nil, fmt.Errorf("%s %s qty=%.8f net=%.8f: %w", ex.symbol, side, req.Quantity, ex.netQty, errReduceOnlyRejected)
	}
	if ex.enforceMargin && !req.ReduceOnly && !ex.hasMarginLocked(req.Quantity, price) {
		ex.stats.marginRejects++
		return nil, fmt.Errorf("%s %s qty=%.8f price=%.8f: %w", ex.symbol, side, req.Quantity, price, errMarginInsufficient)
	}

	ex.nextOrderID++
	o := &restingOrder{
		id:         ex.nextOrderID,
		clientOID:  req.ClientOrderID,
		side:       side,
		price:      price,
		qty:        req.Quantity,
		reduceOnly: req.ReduceOnly,
		queueAhead: req.Quantity * ex.matching.QueueFactor,
	}
	ex.stats.ordersPlaced++
	ex.pushUpdateLocked(o, statusNew, 0)
	if crosses {
		// 非 PostOnly 交叉單：以最新價吃單（假設盤口深度足夠）
		ex.fillLocked(o, o.qty, ex.lastPrice, false)
	} else {
		ex.orders[o.id] = o
	}
	return &position.Order{
		OrderID:       o.id,
		ClientOrderID: o.clientOID,
		Symbol:        ex.symbol,
		Side:          side,
		Price:         price,
		Quantity:      o.qty,
		Status:        statusNew,
	}, nil
}

// reducesPositionLocked reduce-only 校驗：方向必須與淨持倉相反且數量不超過淨持倉
func (ex *simExchange) reducesPositionLocked(side string, qty float64) bool {
	if side == sideSell {
		return ex.netQty > quantityEpsilon && qty <= ex.netQty+quantityEpsilon
	}
	return ex.netQty < -quantityEpsilon && qty <= -ex.netQty+quantityEpsilon
}

// hasMarginLocked 開倉保證金校驗：可用 = 權益 − 持倉保證金 − 掛單保證金
func (ex *simExchange) hasMarginLocked(qty, price float64) bool {
	return ex.availableBalanceLocked() >= qty*price/float64(ex.leverage)
}

func (ex *simExchange) availableBalanceLocked() float64 {
	lev := float64(ex.leverage)
	used := math.Abs(ex.netQty) * ex.lastPrice / lev
	for _, o := range ex.orders {
		if !o.reduceOnly {
			used += o.remaining() * o.price / lev
		}
	}
	return ex.equityLocked() - used
}

// cancel 撤單：從掛單簿移除並排隊 CANCELED 回報（已不存在的訂單視為成功，與實盤 -2011 處理一致）
func (ex *simExchange) cancel(ids []int64) {
	ex.mu.Lock()
	defer ex.mu.Unlock()
	for _, id := range ids {
		o, ok := ex.orders[id]
		if !ok {
			continue
		}
		delete(ex.orders, id)
		ex.stats.ordersCanceled++
		ex.pushUpdateLocked(o, statusCanceled, 0)
	}
}

// matchTrade 用一筆成交撮合掛單簿：
//   - 穿價（BUY: 成交價 < 掛單價；SELL: 成交價 > 掛單價）才成交；
//   - 觸價（成交價 == 掛單價）默認不成交；FillOnTouch 時成交；QueueFactor>0 時先消耗排隊量再成交；
//   - 可成交量 = 成交量 × 參與率，按價格優先（BUY 高價先、SELL 低價先）依次分配，支持部分成交；
//   - 成交價為掛單價，按 maker 費率收費。
func (ex *simExchange) matchTrade(t Tick) {
	ex.mu.Lock()
	defer ex.mu.Unlock()
	ex.now = t.Timestamp
	ex.lastPrice = t.Price
	if len(ex.orders) == 0 {
		return
	}
	eps := ex.priceEps()
	var buys, sells []*restingOrder
	for _, o := range ex.orders {
		if o.side == sideBuy && t.Price <= o.price+eps {
			buys = append(buys, o)
		} else if o.side == sideSell && t.Price >= o.price-eps {
			sells = append(sells, o)
		}
	}
	sort.Slice(buys, func(i, j int) bool {
		if buys[i].price != buys[j].price {
			return buys[i].price > buys[j].price
		}
		return buys[i].id < buys[j].id
	})
	sort.Slice(sells, func(i, j int) bool {
		if sells[i].price != sells[j].price {
			return sells[i].price < sells[j].price
		}
		return sells[i].id < sells[j].id
	})
	unlimited := t.Quantity <= 0
	budget := t.Quantity * ex.matching.ParticipationRate
	for _, group := range [][]*restingOrder{buys, sells} {
		for _, o := range group {
			if !unlimited && budget <= quantityEpsilon {
				break
			}
			touch := math.Abs(t.Price-o.price) <= eps
			avail := math.Inf(1)
			if !unlimited {
				avail = budget
			}
			if touch && !ex.matching.FillOnTouch {
				if ex.matching.QueueFactor <= 0 {
					continue
				}
				if unlimited {
					o.queueAhead = 0
				} else {
					consumed := math.Min(o.queueAhead, t.Quantity)
					o.queueAhead -= consumed
					leftover := (t.Quantity - consumed) * ex.matching.ParticipationRate
					avail = math.Min(avail, leftover)
				}
				if o.queueAhead > quantityEpsilon {
					continue
				}
			}
			qty := ex.floorQty(math.Min(o.remaining(), avail))
			if qty <= quantityEpsilon {
				continue
			}
			if !unlimited {
				budget -= qty
			}
			ex.fillLocked(o, qty, o.price, true)
			if o.remaining() <= quantityEpsilon {
				delete(ex.orders, o.id)
			}
		}
	}
}

// floorQty 按數量精度向下取整（剩餘量本身不足一個步長時原樣返回，避免殘單永遠無法成交）
func (ex *simExchange) floorQty(q float64) float64 {
	step := math.Pow10(-ex.quantityDecimals)
	floored := math.Floor(q/step+1e-9) * step
	if floored <= quantityEpsilon && q > quantityEpsilon && q < step {
		return q
	}
	return floored
}

// fillLocked 成交記賬並排隊回報
func (ex *simExchange) fillLocked(o *restingOrder, qty, price float64, maker bool) {
	rate := ex.matching.TakerFeeRate
	if maker {
		rate = ex.matching.MakerFeeRate
	}
	notional := qty * price
	fee := notional * rate
	o.filled += qty
	o.filledNotional += qty * price
	if o.filled > o.qty {
		o.filled = o.qty
	}

	ex.stats.fills++
	ex.stats.totalVolume += qty
	if maker {
		ex.stats.makerFills++
		ex.stats.makerVolume += qty
		ex.feesMaker += fee
	} else {
		ex.stats.takerFills++
		ex.feesTaker += fee
	}

	realizedDelta := ex.applyPositionLocked(o.side, qty, price)
	pnl := 0.0
	if realizedDelta != 0 {
		pnl = realizedDelta - fee
	}
	tradeType := "buy"
	if o.side == sideSell {
		tradeType = "sell"
	}
	ex.trades = append(ex.trades, backtest.Trade{
		Timestamp: ex.now,
		Type:      tradeType,
		Price:     price,
		Quantity:  qty,
		Fee:       fee,
		PnL:       pnl,
	})

	status := statusFilled
	if o.remaining() > quantityEpsilon {
		status = statusPartiallyFilled
		ex.stats.partialFills++
	}
	ex.pushFillUpdateLocked(o, status, price, fee, realizedDelta)
}

// applyPositionLocked 更新淨持倉與均價，返回本次已實現盈虧（毛）
func (ex *simExchange) applyPositionLocked(side string, qty, price float64) float64 {
	signed := qty
	if side == sideSell {
		signed = -qty
	}
	if ex.netQty == 0 || (ex.netQty > 0) == (signed > 0) {
		total := math.Abs(ex.netQty) + qty
		ex.avgEntry = (ex.avgEntry*math.Abs(ex.netQty) + price*qty) / total
		ex.netQty += signed
		return 0
	}
	closeQty := math.Min(qty, math.Abs(ex.netQty))
	direction := 1.0
	if ex.netQty < 0 {
		direction = -1
	}
	realized := closeQty * (price - ex.avgEntry) * direction
	ex.realized += realized
	ex.stats.closedGrids++
	ex.netQty += signed
	switch {
	case math.Abs(ex.netQty) <= quantityEpsilon:
		ex.netQty = 0
		ex.avgEntry = 0
	case (ex.netQty > 0) == (signed > 0):
		// 反手：剩餘部分按成交價開新倉
		ex.avgEntry = price
	}
	if realized == 0 {
		// 平價平倉：返回極小非零值會污染統計，這裡保持 0，但仍計入 closedGrids
		return 0
	}
	return realized
}

// settleFunding 跨越 8h 結算點時按淨持倉名義價值結算資金費（多頭在正費率時支付）
func (ex *simExchange) settleFunding(fromTs, toTs int64, rateAt func(ts int64) float64) {
	if toTs <= fromTs {
		return
	}
	ex.mu.Lock()
	defer ex.mu.Unlock()
	first := (fromTs/FundingIntervalMs + 1) * FundingIntervalMs
	for settle := first; settle <= toTs; settle += FundingIntervalMs {
		if ex.netQty == 0 || ex.lastPrice <= 0 {
			continue
		}
		ex.fundingPaid += ex.netQty * ex.lastPrice * rateAt(settle)
	}
}

// settleFundingPoints 按實際歷史結算時間逐筆結算，支援非固定 8h 的資金費週期。
func (ex *simExchange) settleFundingPoints(fromTs, toTs int64, points []FundingPoint) {
	if toTs <= fromTs {
		return
	}
	ex.mu.Lock()
	defer ex.mu.Unlock()
	for _, point := range points {
		if point.Timestamp <= fromTs {
			continue
		}
		if point.Timestamp > toTs {
			break
		}
		markPrice := ex.lastPrice
		if point.MarkPrice != nil {
			markPrice = *point.MarkPrice
		}
		if ex.netQty == 0 || markPrice <= 0 {
			continue
		}
		ex.fundingPaid += ex.netQty * markPrice * point.Rate
	}
}

func (ex *simExchange) pushUpdateLocked(o *restingOrder, status string, commission float64) {
	average := 0.0
	if o.filled > 0 {
		average = o.filledNotional / o.filled
	}
	ex.pending = append(ex.pending, position.OrderUpdate{
		OrderID:         o.id,
		ClientOrderID:   o.clientOID,
		Symbol:          ex.symbol,
		Status:          status,
		ExecutedQty:     o.filled,
		AvgPrice:        average,
		Price:           o.price,
		Side:            o.side,
		Type:            "LIMIT",
		UpdateTime:      ex.now,
		Commission:      commission,
		CommissionAsset: "USDT",
	})
	ex.orderStates[o.id] = ex.pending[len(ex.pending)-1]
}

func (ex *simExchange) pushFillUpdateLocked(o *restingOrder, status string, fillPrice, commission, realized float64) {
	ex.pending = append(ex.pending, position.OrderUpdate{
		OrderID:         o.id,
		ClientOrderID:   o.clientOID,
		Symbol:          ex.symbol,
		Status:          status,
		ExecutedQty:     o.filled,
		Price:           o.price,
		AvgPrice:        o.filledNotional / o.filled,
		Side:            o.side,
		Type:            "LIMIT",
		UpdateTime:      ex.now,
		Commission:      commission,
		CommissionAsset: "USDT",
		RealizedPnL:     realized,
	})
	ex.orderStates[o.id] = ex.pending[len(ex.pending)-1]
}

// drainUpdates 取出待投遞的訂單回報（模擬 WebSocket 異步推送）
func (ex *simExchange) drainUpdates() []position.OrderUpdate {
	ex.mu.Lock()
	defer ex.mu.Unlock()
	out := ex.pending
	ex.pending = nil
	return out
}

// ===== 帳戶 =====

func (ex *simExchange) equityLocked() float64 {
	unrealized := 0.0
	if ex.netQty != 0 && ex.lastPrice > 0 {
		unrealized = ex.netQty * (ex.lastPrice - ex.avgEntry)
	}
	return ex.initialCapital + ex.realized - ex.feesMaker - ex.feesTaker - ex.fundingPaid + unrealized
}

// snapshot 權益與敞口快照
type snapshot struct {
	equity, netQty, lastPrice float64
}

func (ex *simExchange) snapshot() snapshot {
	ex.mu.Lock()
	defer ex.mu.Unlock()
	return snapshot{equity: ex.equityLocked(), netQty: ex.netQty, lastPrice: ex.lastPrice}
}

func (ex *simExchange) openOrderCount() int {
	ex.mu.Lock()
	defer ex.mu.Unlock()
	return len(ex.orders)
}

// ===== position.IExchange =====

func (ex *simExchange) GetName() string { return simExchangeName }

// GetPositions 回放從明確的空倉快照開始；nil 代表查詢結果未知，不能用作空倉證據
func (ex *simExchange) GetPositions(ctx context.Context, symbol string) (interface{}, error) {
	return []*position.PositionInfo{}, nil
}

func (ex *simExchange) GetOpenOrders(ctx context.Context, symbol string) (interface{}, error) {
	return nil, nil
}

func (ex *simExchange) GetOrder(ctx context.Context, symbol string, orderID int64) (interface{}, error) {
	return nil, nil
}

func (ex *simExchange) GetBaseAsset() string {
	return strings.TrimSuffix(strings.ToUpper(ex.symbol), ex.GetQuoteAsset())
}

func (ex *simExchange) CancelAllOrders(ctx context.Context, symbol string) error {
	ex.mu.Lock()
	ids := make([]int64, 0, len(ex.orders))
	for id := range ex.orders {
		ids = append(ids, id)
	}
	ex.mu.Unlock()
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	ex.cancel(ids)
	return nil
}

func (ex *simExchange) GetAccount(ctx context.Context) (interface{}, error) {
	ex.mu.Lock()
	defer ex.mu.Unlock()
	equity := ex.equityLocked()
	return &exchange.Account{
		TotalWalletBalance: ex.initialCapital + ex.realized - ex.feesMaker - ex.feesTaker - ex.fundingPaid,
		TotalMarginBalance: equity,
		AvailableBalance:   ex.availableBalanceLocked(),
		AccountLeverage:    ex.leverage,
	}, nil
}

func (ex *simExchange) GetPriceDecimals() int    { return ex.priceDecimals }
func (ex *simExchange) GetQuantityDecimals() int { return ex.quantityDecimals }

// GetOrderBook 合成盤口：最新價 ± 1 tick，數量充足（回放無真實深度）
func (ex *simExchange) GetOrderBook(ctx context.Context, symbol string, limit int) (*position.OrderBook, error) {
	ex.mu.Lock()
	defer ex.mu.Unlock()
	if ex.lastPrice <= 0 {
		return nil, nil
	}
	t := ex.tick()
	return &position.OrderBook{
		Symbol:    ex.symbol,
		Bids:      []position.OrderBookLevel{{Price: ex.lastPrice - t, Quantity: syntheticBookQty}},
		Asks:      []position.OrderBookLevel{{Price: ex.lastPrice + t, Quantity: syntheticBookQty}},
		Timestamp: ex.now,
	}, nil
}

// GetOrderFills 回報中已帶手續費，不支持補查
func (ex *simExchange) GetOrderFills(ctx context.Context, symbol string, orderID int64) (interface{}, error) {
	return nil, nil
}

func (ex *simExchange) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	ex.mu.Lock()
	defer ex.mu.Unlock()
	return ex.lastPrice, nil
}

func (ex *simExchange) GetQuoteAsset() string {
	upper := strings.ToUpper(ex.symbol)
	for _, q := range []string{"USDT", "USDC", "BUSD"} {
		if strings.HasSuffix(upper, q) {
			return q
		}
	}
	return "USDT"
}

func (ex *simExchange) GetBalance(ctx context.Context, asset string) (float64, error) {
	ex.mu.Lock()
	defer ex.mu.Unlock()
	return ex.availableBalanceLocked(), nil
}
