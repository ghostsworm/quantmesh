package binance

import (
	"context"
	"fmt"
	"strconv"

	binancesdk "github.com/adshao/go-binance/v2"
)

// MarginClient Binance 现货杠杆客户端（借还、margin 下单）
type MarginClient struct {
	client *binancesdk.Client
}

// NewMarginClient 创建 margin 客户端（复用 spot client，调用 sapi margin 接口）
func NewMarginClient(client *binancesdk.Client) *MarginClient {
	return &MarginClient{client: client}
}

// Borrow 借币
func (m *MarginClient) Borrow(ctx context.Context, asset string, amount float64, isIsolated bool, symbol string) (txID int64, err error) {
	amountStr := formatFloat(amount)
	srv := m.client.NewMarginBorrowRepayService().
		Asset(asset).
		Amount(amountStr).
		IsIsolated(isIsolated).
		Type(binancesdk.MarginAccountBorrow)
	if isIsolated && symbol != "" {
		srv = srv.Symbol(symbol)
	}
	res, err := srv.Do(ctx)
	if err != nil {
		return 0, err
	}
	return res.TranID, nil
}

// Repay 还币
func (m *MarginClient) Repay(ctx context.Context, asset string, amount float64, isIsolated bool, symbol string) (txID int64, err error) {
	amountStr := formatFloat(amount)
	srv := m.client.NewMarginBorrowRepayService().
		Asset(asset).
		Amount(amountStr).
		IsIsolated(isIsolated).
		Type(binancesdk.MarginAccountRepay)
	if isIsolated && symbol != "" {
		srv = srv.Symbol(symbol)
	}
	res, err := srv.Do(ctx)
	if err != nil {
		return 0, err
	}
	return res.TranID, nil
}

// GetMaxBorrowable 查询最大可借数量
func (m *MarginClient) GetMaxBorrowable(ctx context.Context, asset string, isIsolated bool, symbol string) (float64, error) {
	srv := m.client.NewGetMaxBorrowableService().Asset(asset)
	if isIsolated && symbol != "" {
		srv = srv.IsolatedSymbol(symbol)
	}
	res, err := srv.Do(ctx)
	if err != nil {
		return 0, err
	}
	return strconv.ParseFloat(res.Amount, 64)
}

// GetTradesByOrder returns margin-account trades for one order, paginating from
// the supplied creation time so old fills are not silently omitted by the API's
// default recent-trade window.
func (m *MarginClient) GetTradesByOrder(ctx context.Context, symbol string, orderID int64, startTime int64, isIsolated bool) ([]*binancesdk.TradeV3, error) {
	if m == nil || m.client == nil {
		return nil, fmt.Errorf("margin client is unavailable")
	}
	if symbol == "" || orderID <= 0 || startTime <= 0 {
		return nil, fmt.Errorf("symbol, positive order ID, and start time are required to query margin trades")
	}
	const pageSize = 1000
	const maxPages = 100
	var fromID int64
	trades := make([]*binancesdk.TradeV3, 0)
	seenTradeIDs := make(map[int64]struct{})
	for page := 0; page < maxPages; page++ {
		service := m.client.NewListMarginTradesService().Symbol(symbol).StartTime(startTime).Limit(pageSize).IsIsolated(isIsolated)
		if fromID > 0 {
			service = service.FromID(fromID)
		}
		rows, err := service.Do(ctx)
		if err != nil {
			return nil, fmt.Errorf("query margin trades page %d for %s/order %d: %w", page+1, symbol, orderID, err)
		}
		if len(rows) == 0 {
			return trades, nil
		}
		lastID := int64(0)
		for _, row := range rows {
			if row == nil || row.ID <= 0 {
				return nil, fmt.Errorf("margin trades returned invalid trade identity for %s/order %d", symbol, orderID)
			}
			if row.ID > lastID {
				lastID = row.ID
			}
			if row.OrderID != orderID {
				continue
			}
			if _, duplicate := seenTradeIDs[row.ID]; duplicate {
				return nil, fmt.Errorf("margin trades returned duplicate trade ID %d for order %d", row.ID, orderID)
			}
			seenTradeIDs[row.ID] = struct{}{}
			trades = append(trades, row)
		}
		if len(rows) < pageSize {
			return trades, nil
		}
		if lastID <= fromID {
			return nil, fmt.Errorf("margin trade pagination did not advance for order %d", orderID)
		}
		fromID = lastID + 1
	}
	return nil, fmt.Errorf("margin trade history exceeded %d pages for %s/order %d", maxPages, symbol, orderID)
}

// PlaceMarginOrder 杠杆账户下单（用于做空：先借后卖 / 买回归还）
func (m *MarginClient) PlaceMarginOrder(ctx context.Context, symbol, side, orderType, quantity, price string, isIsolated bool) (orderID int64, err error) {
	return m.PlaceMarginOrderWithClientOrderID(ctx, symbol, side, orderType, quantity, price, "", isIsolated)
}

// PlaceMarginOrderWithClientOrderID submits a margin order with a caller-owned
// identifier so uncertain acknowledgements can be reconciled on the margin API.
func (m *MarginClient) PlaceMarginOrderWithClientOrderID(ctx context.Context, symbol, side, orderType, quantity, price, clientOrderID string, isIsolated bool) (orderID int64, err error) {
	srv := m.client.NewCreateMarginOrderService().
		Symbol(symbol).
		Side(binancesdk.SideType(side)).
		Type(binancesdk.OrderType(orderType)).
		Quantity(quantity).
		IsIsolated(isIsolated)
	if price != "" && price != "0" {
		srv = srv.Price(price).TimeInForce(binancesdk.TimeInForceTypeGTC)
	}
	if clientOrderID != "" {
		srv = srv.NewClientOrderID(clientOrderID)
	}
	res, err := srv.Do(ctx)
	if err != nil {
		return 0, err
	}
	return res.OrderID, nil
}

// GetMarginOrderByClientOrderID queries cross-margin order history by its
// original client ID; the ordinary spot order endpoint is not equivalent.
func (m *MarginClient) GetMarginOrderByClientOrderID(ctx context.Context, symbol, clientOrderID string, isIsolated bool) (*binancesdk.Order, error) {
	if m == nil || m.client == nil || symbol == "" || clientOrderID == "" {
		return nil, fmt.Errorf("margin client, symbol, and client order ID are required")
	}
	return m.client.NewGetMarginOrderService().Symbol(symbol).OrigClientOrderID(clientOrderID).IsIsolated(isIsolated).Do(ctx)
}

// GetBorrowHistory queries a bounded page of cross-margin BORROW records.
func (m *MarginClient) GetBorrowHistory(ctx context.Context, asset string, startTime, endTime, page, pageSize int64) (*binancesdk.MarginBorrowRepayResponse, error) {
	if m == nil || m.client == nil || asset == "" || startTime <= 0 || endTime < startTime || page < 1 || pageSize < 1 || pageSize > 100 {
		return nil, fmt.Errorf("valid margin borrow history query parameters are required")
	}
	return m.client.NewListMarginBorrowRepayService().
		Asset(asset).
		StartTime(startTime).
		EndTime(endTime).
		Current(page).
		Size(pageSize).
		Type(binancesdk.MarginAccountBorrow).
		Do(ctx)
}

func formatFloat(v float64) string {
	if v <= 0 {
		return "0"
	}
	if v >= 1 {
		return fmt.Sprintf("%.8f", v)
	}
	return fmt.Sprintf("%.8f", v)
}
