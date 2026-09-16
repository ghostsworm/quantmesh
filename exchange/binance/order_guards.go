package binance

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"quantmesh/logger"
	"quantmesh/utils"

	"github.com/adshao/go-binance/v2/common"
	"github.com/adshao/go-binance/v2/futures"
)

// 下單前本地防護（R4：X3 最小名義金額、X4 市價單估價、X5 持倉模式自檢、E5 按 ClientOrderID 查單）

const (
	// binanceBrokerName utils.AddBrokerPrefix / RemoveBrokerPrefix 使用的交易所標識
	binanceBrokerName = "binance"
	// binanceErrCodeNoSuchOrder 查詢訂單不存在（-2013）
	binanceErrCodeNoSuchOrder = -2013
	// filterFieldNotional 合約 MIN_NOTIONAL 過濾器中的下限字段名
	filterFieldNotional = "notional"
	// filterFieldType 過濾器類型字段名
	filterFieldType = "filterType"
	// minNotionalRelTolerance 名義金額比較的相對容差，吸收浮點乘法誤差（100.0 算成 99.99999999999999）
	minNotionalRelTolerance = 1e-9

	// positionModeRecheckInterval 持倉模式查詢失敗或檢測到對沖模式後，再次查詢的最小間隔
	positionModeRecheckInterval = 30 * time.Second
	// positionModeQueryTimeout 單次持倉模式查詢超時
	positionModeQueryTimeout = 5 * time.Second
)

var (
	// ErrOrderNotionalTooSmall 訂單名義金額低於交易所 MIN_NOTIONAL。
	// 文案帶 -4164（交易所同類錯誤碼），上層據此判定為不可重試的配置問題。
	ErrOrderNotionalTooSmall = errors.New("order notional below exchange MIN_NOTIONAL (-4164)")

	// ErrHedgePositionMode 賬戶處於對沖（雙向）持倉模式。本適配器下單不帶 positionSide，
	// 對沖模式下每單都會被拒（-4061），因此直接拒絕啟動/下單。
	ErrHedgePositionMode = errors.New("binance futures account is in hedge (dual-side) position mode; switch to one-way mode (-4061)")
)

// parseMinNotional 從合約交易對過濾器中解析 MIN_NOTIONAL.notional
func parseMinNotional(s *futures.Symbol) (float64, bool) {
	if s == nil {
		return 0, false
	}
	for _, f := range s.Filters {
		if ft, _ := f[filterFieldType].(string); ft != string(futures.SymbolFilterTypeMinNotional) {
			continue
		}
		raw, _ := f[filterFieldNotional].(string)
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil || v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, false
		}
		return v, true
	}
	return 0, false
}

// minNotionalFor 返回交易對的最小名義金額；未知時 ok=false
func (b *BinanceAdapter) minNotionalFor(symbol string) (float64, bool) {
	b.minNotionalsMu.RLock()
	defer b.minNotionalsMu.RUnlock()
	v, ok := b.minNotionals[symbol]
	return v, ok
}

// checkMinNotional 校驗訂單名義金額不低於 MIN_NOTIONAL。
// reduceOnly 單交易所豁免；下限未知或價格無法估算（如市價單取不到最新價）時跳過，由交易所兜底校驗。
func (b *BinanceAdapter) checkMinNotional(symbol string, price, quantity float64, reduceOnly bool) error {
	if reduceOnly {
		return nil
	}
	minNotional, ok := b.minNotionalFor(symbol)
	if !ok {
		return nil
	}
	if !isPositiveFinite(price) || !isPositiveFinite(quantity) {
		return nil
	}
	notional := price * quantity
	if notional >= minNotional*(1-minNotionalRelTolerance) {
		return nil
	}
	quote := b.quoteAsset
	if quote == "" {
		_, quote = parseFuturesSymbolBaseQuote(symbol)
	}
	return fmt.Errorf("%w: %s 名義金額 %.4f < 最小 %.4f %s（價格=%.8f，數量=%.8f），請提高每單金額或數量",
		ErrOrderNotionalTooSmall, symbol, notional, minNotional, quote, price, quantity)
}

func isPositiveFinite(v float64) bool {
	return v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0)
}

// estimateMarketOrderPrice 市價單估價（最新價）；失敗返回 0，表示跳過本地名義金額校驗
func (b *BinanceAdapter) estimateMarketOrderPrice(ctx context.Context, symbol string) float64 {
	price, err := b.GetLatestPrice(ctx, symbol)
	if err != nil || !isPositiveFinite(price) {
		logger.Warn("⚠️ [Binance] [%s] 市價單無法獲取最新價（%v），跳過本地最小名義金額校驗，由交易所校驗", symbol, err)
		return 0
	}
	return price
}

// ensureOneWayPositionMode 確認合約賬戶為單向持倉模式（GET /fapi/v1/positionSide/dual）。
// 對沖模式返回 ErrHedgePositionMode；查詢失敗只告警放行（交易所仍會以 -4061 拒單），並按間隔重查。
func (b *BinanceAdapter) ensureOneWayPositionMode(ctx context.Context) error {
	b.positionModeMu.Lock()
	defer b.positionModeMu.Unlock()

	if b.positionModeVerified {
		return nil
	}
	if !b.positionModeLastAttempt.IsZero() && time.Since(b.positionModeLastAttempt) < positionModeRecheckInterval {
		if b.positionModeHedge {
			return b.hedgeModeError()
		}
		return nil
	}
	b.positionModeLastAttempt = time.Now()

	queryCtx, cancel := context.WithTimeout(ctx, positionModeQueryTimeout)
	defer cancel()
	mode, err := b.client.NewGetPositionModeService().Do(queryCtx)
	if err != nil {
		b.positionModeHedge = false
		logger.Warn("⚠️ [Binance] 查詢持倉模式失敗（%v 後重試），暫按單向模式處理: %v", positionModeRecheckInterval, err)
		return nil
	}
	if mode.DualSidePosition {
		b.positionModeHedge = true
		logger.Error("❌ [Binance] %v", b.hedgeModeError())
		return b.hedgeModeError()
	}
	b.positionModeHedge = false
	b.positionModeVerified = true
	return nil
}

func (b *BinanceAdapter) hedgeModeError() error {
	return fmt.Errorf("%w（symbol=%s testnet=%v）：請在幣安合約「偏好設置 → 倉位模式」改為單向持倉後重啟",
		ErrHedgePositionMode, b.symbol, b.useTestnet)
}

// isBinanceNoSuchOrderError 判斷是否為「查詢訂單不存在」錯誤（code -2013）
func isBinanceNoSuchOrderError(err error) bool {
	var apiErr *common.APIError
	return errors.As(err, &apiErr) && apiErr.Code == binanceErrCodeNoSuchOrder
}

// GetOrderByClientOrderID 按自定義訂單 ID 查詢訂單（含已成交/已撤銷的訂單）。
// clientOrderID 可帶或不帶返佣前綴；訂單不存在時返回 nil, nil。
func (b *BinanceAdapter) GetOrderByClientOrderID(ctx context.Context, symbol, clientOrderID string) (*Order, error) {
	if clientOrderID == "" {
		return nil, fmt.Errorf("query binance order on %s: empty clientOrderID", symbol)
	}
	// 與 PlaceOrder 相同的前綴/截斷規則，保證查詢 ID 與提交 ID 一致
	cid := utils.AddBrokerPrefix(binanceBrokerName, utils.RemoveBrokerPrefix(binanceBrokerName, clientOrderID))
	o, err := b.client.NewGetOrderService().Symbol(symbol).OrigClientOrderID(cid).Do(ctx)
	if err != nil {
		if isBinanceNoSuchOrderError(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("query binance order by clientOrderID %s on %s: %w", cid, symbol, err)
	}
	return convertFuturesOrder(o), nil
}

// convertFuturesOrder 將 go-binance 合約訂單轉為本地 Order
func convertFuturesOrder(o *futures.Order) *Order {
	if o == nil {
		return nil
	}
	price, _ := strconv.ParseFloat(o.Price, 64)
	quantity, _ := strconv.ParseFloat(o.OrigQuantity, 64)
	executedQty, _ := strconv.ParseFloat(o.ExecutedQuantity, 64)
	avgPrice, _ := strconv.ParseFloat(o.AvgPrice, 64)
	return &Order{
		OrderID:       o.OrderID,
		ClientOrderID: o.ClientOrderID,
		Symbol:        o.Symbol,
		Side:          Side(o.Side),
		Type:          OrderType(o.Type),
		Price:         price,
		Quantity:      quantity,
		ExecutedQty:   executedQty,
		AvgPrice:      avgPrice,
		Status:        OrderStatus(o.Status),
		UpdateTime:    o.UpdateTime,
	}
}
