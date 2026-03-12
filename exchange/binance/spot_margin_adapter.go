package binance

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"quantmesh/logger"

	binancesdk "github.com/adshao/go-binance/v2"
)

// BinanceSpotMarginAdapter 幣安現貨槓桿適配器（借幣做空）
// 使用 margin API：借還、margin 下單
type BinanceSpotMarginAdapter struct {
	*BinanceSpotAdapter
	marginClient *MarginClient
}

// NewBinanceSpotMarginAdapter 創建現貨槓桿適配器
func NewBinanceSpotMarginAdapter(cfg map[string]string, symbol string) (*BinanceSpotMarginAdapter, error) {
	spot, err := NewBinanceSpotAdapter(cfg, symbol)
	if err != nil {
		return nil, err
	}
	return &BinanceSpotMarginAdapter{
		BinanceSpotAdapter: spot,
		marginClient:       NewMarginClient(spot.client),
	}, nil
}

// GetName 獲取交易所名稱
func (b *BinanceSpotMarginAdapter) GetName() string {
	return "Binance Spot Margin"
}

// GetMarketType 獲取市場類型
func (b *BinanceSpotMarginAdapter) GetMarketType() string {
	return "spot_margin"
}

// PlaceOrder 下單（使用 margin API）
func (b *BinanceSpotMarginAdapter) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	if req.Price <= 0 {
		return nil, fmt.Errorf("無效的下單價格: %.8f", req.Price)
	}

	adjustedPrice := b.roundToTickSize(req.Price, req.Side)
	adjustedQty := b.roundToStepSize(req.Quantity)
	if adjustedQty <= 0 {
		adjustedQty = b.stepSize
		if adjustedQty <= 0 {
			adjustedQty = math.Pow10(-b.quantityDecimals)
		}
	}

	pDec := req.PriceDecimals
	if pDec <= 0 {
		pDec = b.priceDecimals
	}
	priceStr := fmt.Sprintf("%.*f", pDec, adjustedPrice)
	quantityStr := fmt.Sprintf("%.*f", b.quantityDecimals, adjustedQty)

	orderType := "LIMIT"
	if req.PostOnly {
		orderType = "LIMIT_MAKER"
	}

	sym := req.Symbol
	if sym == "" {
		sym = b.symbol
	}
	orderID, err := b.marginClient.PlaceMarginOrder(ctx, sym, string(req.Side), orderType, quantityStr, priceStr, false)
	if err != nil {
		return nil, err
	}

	price, _ := strconv.ParseFloat(priceStr, 64)
	qty, _ := strconv.ParseFloat(quantityStr, 64)
	return &Order{
		OrderID:       orderID,
		ClientOrderID: "",
		Symbol:        b.symbol,
		Side:          req.Side,
		Type:          OrderType(orderType),
		Price:         price,
		Quantity:      qty,
		ExecutedQty:   0,
		AvgPrice:      price,
		Status:        OrderStatusNew,
		CreatedAt:     time.Now(),
		UpdateTime:    0,
	}, nil
}

// GetAccount 獲取槓桿賬戶，含限流
func (b *BinanceSpotMarginAdapter) GetAccount(ctx context.Context) (*Account, error) {
	var acc *binancesdk.MarginAccount
	if err := b.withRateLimit(ctx, func() error {
		var err error
		acc, err = b.client.NewGetMarginAccountService().Do(ctx)
		return err
	}); err != nil {
		return nil, err
	}
	var totalWallet, available float64
	for _, ua := range acc.UserAssets {
		free, _ := strconv.ParseFloat(ua.Free, 64)
		borr, _ := strconv.ParseFloat(ua.Borrowed, 64)
		net, _ := strconv.ParseFloat(ua.NetAsset, 64)
		totalWallet += net + borr
		available += free
	}
	// 計價資產可用餘額
	quoteAsset := b.quoteAsset
	if quoteAsset == "" {
		quoteAsset = "USDT"
	}
	for _, ua := range acc.UserAssets {
		if ua.Asset == quoteAsset || ua.Asset == "USDT" || ua.Asset == "USDC" {
			f, _ := strconv.ParseFloat(ua.Free, 64)
			available += f
		}
	}
	return &Account{
		TotalWalletBalance: totalWallet,
		TotalMarginBalance:  totalWallet,
		AvailableBalance:   available,
		Positions:          nil,
	}, nil
}

// GetPositions 獲取持倉（借入的 base 資產視為空倉，Size 為負），含限流
func (b *BinanceSpotMarginAdapter) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	var acc *binancesdk.MarginAccount
	if err := b.withRateLimit(ctx, func() error {
		var err error
		acc, err = b.client.NewGetMarginAccountService().Do(ctx)
		return err
	}); err != nil {
		return nil, err
	}
	base := b.baseAsset
	if base == "" {
		for _, suffix := range []string{"USDT", "USDC", "BUSD", "U"} {
			trimmed := strings.TrimSuffix(symbol, suffix)
			if trimmed != symbol {
				base = trimmed
				break
			}
		}
		if base == "" {
			base = symbol
		}
	}
	var borrowed float64
	for _, ua := range acc.UserAssets {
		if ua.Asset == base {
			borrowed, _ = strconv.ParseFloat(ua.Borrowed, 64)
			break
		}
	}
	if borrowed <= 0 {
		return nil, nil
	}
	price, _ := b.GetLatestPrice(ctx, symbol)
	if price <= 0 {
		price = 0
	}
	// 空倉：Size 為負
	return []*Position{{
		Symbol:         symbol,
		Size:           -borrowed,
		EntryPrice:     price,
		MarkPrice:      price,
		UnrealizedPNL:  0,
		Leverage:       1,
		MarginType:     "cross",
		IsolatedMargin: 0,
	}}, nil
}

// GetMarginClient 獲取 margin 客戶端（借還、查最大可借）
func (b *BinanceSpotMarginAdapter) GetMarginClient() *MarginClient {
	return b.marginClient
}

// CancelOrder 取消訂單（使用 margin API）
func (b *BinanceSpotMarginAdapter) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	sym := symbol
	if sym == "" {
		sym = b.symbol
	}
	_, err := b.client.NewCancelMarginOrderService().Symbol(sym).OrderID(orderID).IsIsolated(false).Do(ctx)
	if err != nil {
		if strings.Contains(err.Error(), "-2011") || strings.Contains(err.Error(), "Unknown order") {
			logger.Info("ℹ️ [Binance Spot Margin] 訂單 %d 已不存在，跳過取消", orderID)
			return nil
		}
		return err
	}
	return nil
}

// CancelAllOrders 取消所有訂單（使用 margin API）
func (b *BinanceSpotMarginAdapter) CancelAllOrders(ctx context.Context, symbol string) error {
	sym := symbol
	if sym == "" {
		sym = b.symbol
	}
	_, err := b.client.NewCancelAllMarginOrdersService().Symbol(sym).IsIsolated(false).Do(ctx)
	return err
}

// GetOrder 查詢訂單（使用 margin API）
func (b *BinanceSpotMarginAdapter) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	sym := symbol
	if sym == "" {
		sym = b.symbol
	}
	o, err := b.client.NewGetMarginOrderService().Symbol(sym).OrderID(orderID).IsIsolated(false).Do(ctx)
	if err != nil {
		return nil, err
	}
	price, _ := strconv.ParseFloat(o.Price, 64)
	qty, _ := strconv.ParseFloat(o.OrigQuantity, 64)
	execQty, _ := strconv.ParseFloat(o.ExecutedQuantity, 64)
	avgPrice, _ := strconv.ParseFloat(o.Price, 64)
	return &Order{
		OrderID:       o.OrderID,
		ClientOrderID: o.ClientOrderID,
		Symbol:        o.Symbol,
		Side:          Side(o.Side),
		Type:          OrderType(o.Type),
		Price:         price,
		Quantity:      qty,
		ExecutedQty:   execQty,
		AvgPrice:      avgPrice,
		Status:        OrderStatus(o.Status),
		UpdateTime:    o.UpdateTime,
	}, nil
}

// GetOpenOrders 查詢未完成訂單（使用 margin API）
func (b *BinanceSpotMarginAdapter) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	sym := symbol
	if sym == "" {
		sym = b.symbol
	}
	list, err := b.client.NewListMarginOpenOrdersService().Symbol(sym).IsIsolated(false).Do(ctx)
	if err != nil {
		return nil, err
	}
	orders := make([]*Order, 0, len(list))
	for _, o := range list {
		price, _ := strconv.ParseFloat(o.Price, 64)
		qty, _ := strconv.ParseFloat(o.OrigQuantity, 64)
		execQty, _ := strconv.ParseFloat(o.ExecutedQuantity, 64)
		orders = append(orders, &Order{
			OrderID:       o.OrderID,
			ClientOrderID: o.ClientOrderID,
			Symbol:        o.Symbol,
			Side:          Side(o.Side),
			Type:          OrderType(o.Type),
			Price:         price,
			Quantity:      qty,
			ExecutedQty:   execQty,
			AvgPrice:      price,
			Status:        OrderStatus(o.Status),
			UpdateTime:    o.UpdateTime,
		})
	}
	return orders, nil
}

// Borrow 借幣
func (b *BinanceSpotMarginAdapter) Borrow(ctx context.Context, asset string, amount float64) (int64, error) {
	logger.Info("📥 [Binance Spot Margin] 借幣 %s 數量 %.8f", asset, amount)
	return b.marginClient.Borrow(ctx, asset, amount, false, "")
}

// Repay 還幣
func (b *BinanceSpotMarginAdapter) Repay(ctx context.Context, asset string, amount float64) (int64, error) {
	logger.Info("📤 [Binance Spot Margin] 還幣 %s 數量 %.8f", asset, amount)
	return b.marginClient.Repay(ctx, asset, amount, false, "")
}
