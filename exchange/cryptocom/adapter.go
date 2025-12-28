package cryptocom

import (
	"context"
	"fmt"
	"strings"
	"quantmesh/logger"
)

type Adapter struct {
	client           *CryptoComClient
	instrumentName   string
	priceDecimals    int
	quantityDecimals int
	baseAsset        string
	quoteAsset       string
}

func NewAdapter(config map[string]string, symbol string) (*Adapter, error) {
	apiKey := config["api_key"]
	secretKey := config["secret_key"]
	isTestnet := config["testnet"] == "true"

	if apiKey == "" || secretKey == "" {
		return nil, fmt.Errorf("Crypto.com API key or secret key is empty")
	}

	client := NewCryptoComClient(apiKey, secretKey, isTestnet)
	instrumentName := convertSymbolToCryptoCom(symbol)

	adapter := &Adapter{
		client:           client,
		instrumentName:   instrumentName,
		priceDecimals:    2,
		quantityDecimals: 4,
		baseAsset:        "BTC",
		quoteAsset:       "USDT",
	}

	ctx := context.Background()
	instruments, err := client.GetInstruments(ctx)
	if err != nil {
		logger.Warn("Failed to get Crypto.com instruments: %v", err)
	} else {
		for _, inst := range instruments {
			if inst.InstrumentName == instrumentName {
				adapter.priceDecimals = inst.PriceDecimals
				adapter.quantityDecimals = inst.QuantityDecimals
				adapter.baseAsset = strings.ToUpper(inst.BaseCurrency)
				adapter.quoteAsset = strings.ToUpper(inst.QuoteCurrency)
				break
			}
		}
	}

	return adapter, nil
}

func convertSymbolToCryptoCom(symbol string) string {
	symbol = strings.ToUpper(symbol)
	if strings.HasSuffix(symbol, "USDT") {
		base := strings.TrimSuffix(symbol, "USDT")
		return base + "_USDT"
	}
	return symbol
}

func (a *Adapter) GetName() string {
	return "Crypto.com"
}

func (a *Adapter) PlaceOrder(ctx context.Context, side OrderSide, price, quantity float64, clientOID string) (*OrderLocal, error) {
	var ccSide string
	if side == SideBuy {
		ccSide = "BUY"
	} else {
		ccSide = "SELL"
	}

	req := &OrderRequest{
		InstrumentName: a.instrumentName,
		Side:           ccSide,
		Type:           "LIMIT",
		Quantity:       quantity,
		Price:          price,
		ClientOID:      clientOID,
	}

	order, err := a.client.CreateOrder(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("Crypto.com place order error: %w", err)
	}

	return a.convertOrder(order), nil
}

func (a *Adapter) CancelOrder(ctx context.Context, orderID int64) error {
	return a.client.CancelOrder(ctx, a.instrumentName, orderID)
}

func (a *Adapter) GetOrder(ctx context.Context, orderID int64) (*OrderLocal, error) {
	order, err := a.client.GetOrderDetail(ctx, orderID)
	if err != nil {
		return nil, err
	}
	return a.convertOrder(order), nil
}

func (a *Adapter) GetOpenOrders(ctx context.Context) ([]*OrderLocal, error) {
	orders, err := a.client.GetOpenOrders(ctx, a.instrumentName)
	if err != nil {
		return nil, err
	}

	result := make([]*OrderLocal, 0, len(orders))
	for _, order := range orders {
		result = append(result, a.convertOrder(&order))
	}
	return result, nil
}

func (a *Adapter) GetAccount(ctx context.Context) (*AccountLocal, error) {
	account, err := a.client.GetAccountSummary(ctx)
	if err != nil {
		return nil, err
	}

	return &AccountLocal{
		TotalWalletBalance: account.Balance,
		TotalMarginBalance: account.Balance,
		AvailableBalance:   account.Available,
	}, nil
}

func (a *Adapter) GetPositions(ctx context.Context) ([]*PositionLocal, error) {
	return []*PositionLocal{}, nil
}

func (a *Adapter) GetBalance(ctx context.Context) (float64, error) {
	account, err := a.client.GetAccountSummary(ctx)
	if err != nil {
		return 0, err
	}
	return account.Available, nil
}

func (a *Adapter) GetLatestPrice(ctx context.Context) (float64, error) {
	ticker, err := a.client.GetTicker(ctx, a.instrumentName)
	if err != nil {
		return 0, err
	}
	return ticker.LastPrice, nil
}

func (a *Adapter) GetHistoricalKlines(ctx context.Context, interval string, limit int) ([]*CandleLocal, error) {
	timeframe := string(ConvertInterval(interval))
	klines, err := a.client.GetCandlestick(ctx, a.instrumentName, timeframe)
	if err != nil {
		return nil, err
	}

	result := make([]*CandleLocal, 0, len(klines))
	for _, kline := range klines {
		result = append(result, &CandleLocal{
			Symbol:    a.instrumentName,
			Timestamp: kline.Timestamp,
			Open:      kline.Open,
			High:      kline.High,
			Low:       kline.Low,
			Close:     kline.Close,
			Volume:    kline.Volume,
		})
	}
	return result, nil
}

func (a *Adapter) GetPriceDecimals() int { return a.priceDecimals }
func (a *Adapter) GetQuantityDecimals() int { return a.quantityDecimals }
func (a *Adapter) GetBaseAsset() string { return a.baseAsset }
func (a *Adapter) GetQuoteAsset() string { return a.quoteAsset }
func (a *Adapter) GetFundingRate(ctx context.Context) (float64, error) { return 0, nil }

func (a *Adapter) convertOrder(order *Order) *OrderLocal {
	var side OrderSide
	if order.Side == "BUY" {
		side = SideBuy
	} else {
		side = SideSell
	}

	var status OrderStatus
	switch order.Status {
	case "ACTIVE":
		status = OrderStatusActive
	case "FILLED":
		status = OrderStatusFilled
	case "CANCELED":
		status = OrderStatusCanceled
	case "REJECTED":
		status = OrderStatusRejected
	case "EXPIRED":
		status = OrderStatusExpired
	default:
		status = OrderStatusActive
	}

	return &OrderLocal{
		OrderID:        order.OrderID,
		ClientOID:      order.ClientOID,
		InstrumentName: order.InstrumentName,
		Side:           side,
		Price:          order.Price,
		Quantity:       order.Quantity,
		ExecutedQty:    order.CumQuantity,
		Status:         status,
		UpdateTime:     order.UpdateTime,
	}
}
