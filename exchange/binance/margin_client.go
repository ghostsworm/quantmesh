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

// PlaceMarginOrder 杠杆账户下单（用于做空：先借后卖 / 买回归还）
func (m *MarginClient) PlaceMarginOrder(ctx context.Context, symbol, side, orderType, quantity, price string, isIsolated bool) (orderID int64, err error) {
	srv := m.client.NewCreateMarginOrderService().
		Symbol(symbol).
		Side(binancesdk.SideType(side)).
		Type(binancesdk.OrderType(orderType)).
		Quantity(quantity).
		IsIsolated(isIsolated)
	if price != "" && price != "0" {
		srv = srv.Price(price).TimeInForce(binancesdk.TimeInForceTypeGTC)
	}
	res, err := srv.Do(ctx)
	if err != nil {
		return 0, err
	}
	return res.OrderID, nil
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
