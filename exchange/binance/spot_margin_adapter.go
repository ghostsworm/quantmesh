package binance

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"quantmesh/logger"

	binancesdk "github.com/adshao/go-binance/v2"
	binancecommon "github.com/adshao/go-binance/v2/common"
)

// BinanceSpotMarginAdapter 幣安現貨槓桿適配器（借幣做空）
// 使用 margin API：借還、margin 下單
type BinanceSpotMarginAdapter struct {
	*BinanceSpotAdapter
	marginClient *MarginClient
}

type MarginBorrowRecord struct {
	TransferID int64
	Asset      string
	Amount     float64
	Status     string
	Timestamp  int64
}

const maxBinanceMarginOCOResponseSize = 1 << 20

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
	orderID, err := b.marginClient.PlaceMarginOrderWithClientOrderID(ctx, sym, string(req.Side), orderType, quantityStr, priceStr, req.ClientOrderID, false)
	if err != nil {
		return nil, err
	}

	price, _ := strconv.ParseFloat(priceStr, 64)
	qty, _ := strconv.ParseFloat(quantityStr, 64)
	return &Order{
		OrderID:       orderID,
		ClientOrderID: req.ClientOrderID,
		Symbol:        b.symbol,
		Side:          req.Side,
		Type:          OrderType(orderType),
		Price:         price,
		Quantity:      qty,
		ExecutedQty:   0,
		AvgPrice:      0,
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
	if acc == nil {
		return nil, fmt.Errorf("Binance margin account response is nil")
	}
	quoteAsset := b.quoteAsset
	if quoteAsset == "" {
		quoteAsset = "USDT"
	}
	btcQuotePrice, err := b.GetLatestPrice(ctx, "BTC"+quoteAsset)
	if err != nil {
		return nil, fmt.Errorf("value Binance margin account in %s: %w", quoteAsset, err)
	}
	totalWallet, totalMargin, available, err := summarizeMarginAccount(acc, quoteAsset, btcQuotePrice)
	if err != nil {
		return nil, err
	}
	return &Account{
		TotalWalletBalance: totalWallet,
		TotalMarginBalance: totalMargin,
		AvailableBalance:   available,
		BalanceAsset:       quoteAsset,
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
	if acc == nil {
		return nil, fmt.Errorf("Binance margin account response is nil")
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
	var debt float64
	for _, ua := range acc.UserAssets {
		if ua.Asset == base {
			_, borrowed, interest, _, err := parseMarginUserAsset(ua)
			if err != nil {
				return nil, fmt.Errorf("parse Binance margin debt for %s: %w", base, err)
			}
			debt = borrowed + interest
			break
		}
	}
	if debt <= 0 {
		// A successful account response with no principal or interest is an
		// authoritative flat snapshot, not an unavailable snapshot. Runtime
		// callers deliberately reject nil position slices as unverified.
		return []*Position{}, nil
	}
	price, _ := b.GetLatestPrice(ctx, symbol)
	if price <= 0 {
		price = 0
	}
	// 空倉負債包括本金與已累計利息。
	return []*Position{{
		Symbol:         symbol,
		Size:           -debt,
		EntryPrice:     price,
		MarkPrice:      price,
		UnrealizedPNL:  0,
		Leverage:       1,
		MarginType:     "cross",
		IsolatedMargin: 0,
	}}, nil
}

// VerifySpotMarginAccountFlat verifies cross and isolated liabilities and
// orders, not only the configured symbol. It is used for conservative
// capital-claim release after the Bot's own state is flat.
func (b *BinanceSpotMarginAdapter) VerifySpotMarginAccountFlat(ctx context.Context) error {
	if ctx == nil || b == nil || b.BinanceSpotAdapter == nil || b.client == nil {
		return fmt.Errorf("Binance margin flatness verification requires context and an initialized adapter")
	}
	if err := verifyCrossMarginDebts(ctx, b); err != nil {
		return err
	}
	isolatedSymbols, err := verifyIsolatedMarginDebts(ctx, b)
	if err != nil {
		return err
	}
	if err := verifyMarginOpenOrders(ctx, b, "cross", false, ""); err != nil {
		return err
	}
	if err := verifyMarginOpenOCOLists(ctx, b, false, ""); err != nil {
		return err
	}
	for _, symbol := range isolatedSymbols {
		if err := verifyMarginOpenOrders(ctx, b, "isolated", true, symbol); err != nil {
			return err
		}
		if err := verifyMarginOpenOCOLists(ctx, b, true, symbol); err != nil {
			return err
		}
	}
	return nil
}

func verifyCrossMarginDebts(ctx context.Context, adapter *BinanceSpotMarginAdapter) error {
	var account *binancesdk.MarginAccount
	if err := adapter.withRateLimit(ctx, func() error {
		var err error
		account, err = adapter.client.NewGetMarginAccountService().Do(ctx)
		return err
	}); err != nil {
		return fmt.Errorf("read Binance cross-margin account liabilities: %w", err)
	}
	if account == nil {
		return fmt.Errorf("Binance cross-margin account response is nil")
	}
	if err := verifyZeroBTCMarginLiability("cross", account.TotalLiabilityOfBTC); err != nil {
		return err
	}
	for _, asset := range account.UserAssets {
		if strings.TrimSpace(asset.Asset) == "" {
			return fmt.Errorf("Binance cross-margin account contains an asset with no identifier")
		}
		_, borrowed, interest, _, err := parseMarginUserAsset(asset)
		if err != nil {
			return fmt.Errorf("verify Binance cross-margin debt for %s: %w", asset.Asset, err)
		}
		if borrowed != 0 || interest != 0 {
			return fmt.Errorf("Binance cross-margin debt remains for %s: principal=%s interest=%s", asset.Asset, asset.Borrowed, asset.Interest)
		}
	}
	return nil
}

func verifyIsolatedMarginDebts(ctx context.Context, adapter *BinanceSpotMarginAdapter) ([]string, error) {
	var isolatedAccount *binancesdk.IsolatedMarginAccount
	if err := adapter.withRateLimit(ctx, func() error {
		var err error
		isolatedAccount, err = adapter.client.NewGetIsolatedMarginAccountService().Do(ctx)
		return err
	}); err != nil {
		return nil, fmt.Errorf("read Binance isolated-margin account liabilities: %w", err)
	}
	if isolatedAccount == nil || isolatedAccount.Assets == nil {
		return nil, fmt.Errorf("Binance isolated-margin account response or asset list is nil")
	}
	if err := verifyZeroBTCMarginLiability("isolated", isolatedAccount.TotalLiabilityOfBTC); err != nil {
		return nil, err
	}
	isolatedSymbols := make([]string, 0, len(isolatedAccount.Assets))
	for _, pair := range isolatedAccount.Assets {
		if err := verifyIsolatedMarginPairDebts(pair); err != nil {
			return nil, err
		}
		isolatedSymbols = append(isolatedSymbols, pair.Symbol)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("Binance isolated-margin liability verification deadline: %w", err)
	}
	return isolatedSymbols, nil
}

func verifyIsolatedMarginPairDebts(pair binancesdk.IsolatedMarginAsset) error {
	if strings.TrimSpace(pair.Symbol) == "" {
		return fmt.Errorf("Binance isolated-margin account contains a pair with no symbol")
	}
	for _, asset := range []binancesdk.IsolatedUserAsset{pair.BaseAsset, pair.QuoteAsset} {
		if strings.TrimSpace(asset.Asset) == "" {
			return fmt.Errorf("Binance isolated-margin pair %s contains an asset with no identifier", pair.Symbol)
		}
		borrowed, err := parseMarginDebtAmount("borrowed", asset.Borrowed)
		if err != nil {
			return fmt.Errorf("verify Binance isolated-margin debt for %s/%s: %w", pair.Symbol, asset.Asset, err)
		}
		interest, err := parseMarginDebtAmount("interest", asset.Interest)
		if err != nil {
			return fmt.Errorf("verify Binance isolated-margin debt for %s/%s: %w", pair.Symbol, asset.Asset, err)
		}
		if borrowed != 0 || interest != 0 {
			return fmt.Errorf("Binance isolated-margin debt remains for %s/%s: principal=%s interest=%s", pair.Symbol, asset.Asset, asset.Borrowed, asset.Interest)
		}
	}
	return nil
}

func verifyZeroBTCMarginLiability(accountType, raw string) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("Binance %s-margin total liability is missing", accountType)
	}
	liabilityBTC, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(liabilityBTC) || math.IsInf(liabilityBTC, 0) || liabilityBTC < 0 {
		return fmt.Errorf("Binance %s-margin total liability %q is invalid", accountType, raw)
	}
	if liabilityBTC != 0 {
		return fmt.Errorf("Binance %s-margin total liability is not zero: %s BTC", accountType, raw)
	}
	return nil
}

func parseMarginDebtAmount(name, raw string) (float64, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, fmt.Errorf("%s amount is missing", name)
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return 0, fmt.Errorf("%s amount %q is invalid", name, raw)
	}
	return value, nil
}

func verifyMarginOpenOrders(ctx context.Context, adapter *BinanceSpotMarginAdapter, accountType string, isolated bool, symbol string) error {
	var orders []*binancesdk.Order
	if err := adapter.withRateLimit(ctx, func() error {
		var err error
		service := adapter.client.NewListMarginOpenOrdersService().IsIsolated(isolated)
		if symbol != "" {
			service = service.Symbol(symbol)
		}
		orders, err = service.Do(ctx)
		return err
	}); err != nil {
		return fmt.Errorf("read Binance %s margin open orders for %s: %w", accountType, symbol, err)
	}
	if orders == nil {
		return fmt.Errorf("Binance %s margin open-order response for %s is nil", accountType, symbol)
	}
	if len(orders) != 0 {
		return fmt.Errorf("Binance %s margin account for %s has %d active orders", accountType, symbol, len(orders))
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("Binance margin open-order verification deadline: %w", err)
	}
	return nil
}

func verifyMarginOpenOCOLists(ctx context.Context, adapter *BinanceSpotMarginAdapter, isolated bool, symbol string) error {
	lists, err := fetchMarginOpenOCOLists(ctx, adapter, isolated, symbol)
	if err != nil {
		return fmt.Errorf("verify Binance %s margin open OCO lists for %s: %w", map[bool]string{false: "cross", true: "isolated"}[isolated], symbol, err)
	}
	if len(lists) != 0 {
		return fmt.Errorf("Binance %s margin account for %s has %d active OCO lists", map[bool]string{false: "cross", true: "isolated"}[isolated], symbol, len(lists))
	}
	return nil
}

func fetchMarginOpenOCOLists(ctx context.Context, adapter *BinanceSpotMarginAdapter, isolated bool, symbol string) ([]*binancesdk.Oco, error) {
	if isolated && strings.TrimSpace(symbol) == "" {
		return nil, fmt.Errorf("isolated-margin OCO verification requires a symbol")
	}
	var lists []*binancesdk.Oco
	err := adapter.withRateLimit(ctx, func() error {
		request, err := newSignedMarginOpenOCORequest(ctx, adapter.client, isolated, symbol)
		if err != nil {
			return err
		}
		response, err := adapter.client.HTTPClient.Do(request)
		if err != nil {
			return fmt.Errorf("request Binance margin OCO lists: %w", err)
		}
		defer response.Body.Close()
		payload, err := io.ReadAll(io.LimitReader(response.Body, maxBinanceMarginOCOResponseSize+1))
		if err != nil {
			return fmt.Errorf("read Binance margin OCO response: %w", err)
		}
		if len(payload) > maxBinanceMarginOCOResponseSize {
			return fmt.Errorf("Binance margin OCO response exceeds %d bytes", maxBinanceMarginOCOResponseSize)
		}
		if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
			return fmt.Errorf("Binance margin OCO request returned HTTP %d", response.StatusCode)
		}
		if err := json.Unmarshal(payload, &lists); err != nil {
			return fmt.Errorf("decode Binance margin OCO response: %w", err)
		}
		if lists == nil {
			return fmt.Errorf("Binance margin OCO response is nil")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return lists, nil
}

func newSignedMarginOpenOCORequest(ctx context.Context, client *binancesdk.Client, isolated bool, symbol string) (*http.Request, error) {
	params := url.Values{}
	params.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli()-client.TimeOffset, 10))
	if isolated {
		params.Set("isIsolated", "TRUE")
		params.Set("symbol", symbol)
	} else {
		params.Set("isIsolated", "FALSE")
	}
	keyType := client.KeyType
	if keyType == "" {
		keyType = binancecommon.KeyTypeHmac
	}
	sign, err := binancecommon.SignFunc(keyType)
	if err != nil {
		return nil, fmt.Errorf("initialize Binance margin OCO request signer: %w", err)
	}
	signature, err := sign(client.SecretKey, params.Encode())
	if err != nil {
		return nil, fmt.Errorf("sign Binance margin OCO request: %w", err)
	}
	requestURL := client.BaseURL + "/sapi/v1/margin/openOrderList?" + params.Encode() + "&signature=" + url.QueryEscape(*signature)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build Binance margin OCO request: %w", err)
	}
	request.Header.Set("X-MBX-APIKEY", client.APIKey)
	return request, nil
}

func parseMarginUserAsset(asset binancesdk.UserAsset) (free, borrowed, interest, net float64, err error) {
	parse := func(name, raw string, allowNegative bool) (float64, error) {
		value, parseErr := strconv.ParseFloat(raw, 64)
		if parseErr != nil || math.IsNaN(value) || math.IsInf(value, 0) || (!allowNegative && value < 0) {
			if parseErr == nil {
				parseErr = fmt.Errorf("invalid finite non-negative amount %q", raw)
			}
			return 0, fmt.Errorf("invalid %s %q: %w", name, raw, parseErr)
		}
		return value, nil
	}
	if free, err = parse("free", asset.Free, false); err != nil {
		return
	}
	if borrowed, err = parse("borrowed", asset.Borrowed, false); err != nil {
		return
	}
	if interest, err = parse("interest", asset.Interest, false); err != nil {
		return
	}
	if net, err = parse("net asset", asset.NetAsset, true); err != nil {
		return
	}
	return
}

func summarizeMarginAccount(account *binancesdk.MarginAccount, quoteAsset string, btcQuotePrice float64) (totalWallet, totalMargin, available float64, err error) {
	if account == nil {
		return 0, 0, 0, fmt.Errorf("Binance margin account response is nil")
	}
	if quoteAsset == "" {
		return 0, 0, 0, fmt.Errorf("quote asset is required to summarize Binance margin account")
	}
	if math.IsNaN(btcQuotePrice) || math.IsInf(btcQuotePrice, 0) || btcQuotePrice <= 0 {
		return 0, 0, 0, fmt.Errorf("invalid BTC/%s valuation price", quoteAsset)
	}
	assetBTC, parseErr := strconv.ParseFloat(account.TotalAssetOfBTC, 64)
	if parseErr != nil || math.IsNaN(assetBTC) || math.IsInf(assetBTC, 0) || assetBTC < 0 {
		return 0, 0, 0, fmt.Errorf("invalid totalAssetOfBtc %q", account.TotalAssetOfBTC)
	}
	netAssetBTC, parseErr := strconv.ParseFloat(account.TotalNetAssetOfBTC, 64)
	if parseErr != nil || math.IsNaN(netAssetBTC) || math.IsInf(netAssetBTC, 0) {
		return 0, 0, 0, fmt.Errorf("invalid totalNetAssetOfBtc %q", account.TotalNetAssetOfBTC)
	}
	totalWallet = assetBTC * btcQuotePrice
	totalMargin = netAssetBTC * btcQuotePrice
	if math.IsInf(totalWallet, 0) || math.IsInf(totalMargin, 0) {
		return 0, 0, 0, fmt.Errorf("Binance margin valuation overflow")
	}
	foundQuote := false
	for _, asset := range account.UserAssets {
		if !strings.EqualFold(asset.Asset, quoteAsset) {
			continue
		}
		if foundQuote {
			return 0, 0, 0, fmt.Errorf("duplicate %s margin asset entries", quoteAsset)
		}
		foundQuote = true
		free, _, _, _, parseErr := parseMarginUserAsset(asset)
		if parseErr != nil {
			return 0, 0, 0, fmt.Errorf("parse Binance margin balance for %s: %w", asset.Asset, parseErr)
		}
		available = free
	}
	return totalWallet, totalMargin, available, nil
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
		if isBinanceUnknownOrderError(err) {
			logger.Info("ℹ️ [Binance Spot Margin] 訂單 %d 已不存在，跳過取消", orderID)
			return nil
		}
		return fmt.Errorf("cancel margin order %d on %s: %w", orderID, sym, err)
	}
	return nil
}

// BatchCancelOrders 批量撤單（使用 margin API）。
// 必須覆蓋內嵌現貨適配器的實現：那條路徑走現貨撤單接口，對槓桿訂單返回「訂單不存在」並被當作成功，訂單實際未撤。
func (b *BinanceSpotMarginAdapter) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	return cancelOrdersSequentially(ctx, "binance spot margin", symbol, orderIDs, b.CancelOrder)
}

// CancelAllOrders 取消所有訂單（使用 margin API；無挂單時 -2011 視為成功）
func (b *BinanceSpotMarginAdapter) CancelAllOrders(ctx context.Context, symbol string) error {
	sym := symbol
	if sym == "" {
		sym = b.symbol
	}
	_, err := b.client.NewCancelAllMarginOrdersService().Symbol(sym).IsIsolated(false).Do(ctx)
	if err != nil && !isBinanceUnknownOrderError(err) {
		return fmt.Errorf("cancel all margin orders on %s: %w", sym, err)
	}
	return nil
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
	cumulativeQuote, _ := strconv.ParseFloat(o.CummulativeQuoteQuantity, 64)
	avgPrice := cumulativeAveragePrice(cumulativeQuote, execQty)
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
		CreatedAt:     time.UnixMilli(o.Time),
		UpdateTime:    o.UpdateTime,
	}, nil
}

// GetOrderByClientOrderID queries the cross-margin order endpoint, not the
// embedded spot adapter's order history.
func (b *BinanceSpotMarginAdapter) GetOrderByClientOrderID(ctx context.Context, symbol, clientOrderID string) (*Order, error) {
	if b == nil || b.marginClient == nil {
		return nil, fmt.Errorf("Binance spot margin client is unavailable")
	}
	sym := symbol
	if sym == "" {
		sym = b.symbol
	}
	o, err := b.marginClient.GetMarginOrderByClientOrderID(ctx, sym, clientOrderID, false)
	if err != nil {
		return nil, err
	}
	if o == nil {
		return nil, nil
	}
	price, err := strconv.ParseFloat(o.Price, 64)
	if err != nil {
		return nil, fmt.Errorf("parse Binance margin order price: %w", err)
	}
	qty, err := strconv.ParseFloat(o.OrigQuantity, 64)
	if err != nil {
		return nil, fmt.Errorf("parse Binance margin order quantity: %w", err)
	}
	execQty, err := strconv.ParseFloat(o.ExecutedQuantity, 64)
	if err != nil {
		return nil, fmt.Errorf("parse Binance margin executed quantity: %w", err)
	}
	cumulativeQuote, err := strconv.ParseFloat(o.CummulativeQuoteQuantity, 64)
	if err != nil {
		return nil, fmt.Errorf("parse Binance margin cumulative quote quantity: %w", err)
	}
	return &Order{
		OrderID: o.OrderID, ClientOrderID: o.ClientOrderID, Symbol: o.Symbol,
		Side: Side(o.Side), Type: OrderType(o.Type), Price: price, Quantity: qty,
		ExecutedQty: execQty, AvgPrice: cumulativeAveragePrice(cumulativeQuote, execQty),
		Status: OrderStatus(o.Status), CreatedAt: time.UnixMilli(o.Time), UpdateTime: o.UpdateTime,
	}, nil
}

func (b *BinanceSpotMarginAdapter) GetMarginBorrowHistory(ctx context.Context, asset string, startTime, endTime int64, page, pageSize int) ([]MarginBorrowRecord, int64, error) {
	if b == nil || b.marginClient == nil {
		return nil, 0, fmt.Errorf("Binance spot margin client is unavailable")
	}
	response, err := b.marginClient.GetBorrowHistory(ctx, asset, startTime, endTime, int64(page), int64(pageSize))
	if err != nil {
		return nil, 0, err
	}
	if response == nil {
		return nil, 0, fmt.Errorf("Binance margin borrow history returned an empty response")
	}
	records := make([]MarginBorrowRecord, 0, len(response.Rows))
	for _, row := range response.Rows {
		amount, parseErr := strconv.ParseFloat(row.Amount, 64)
		if parseErr != nil || math.IsNaN(amount) || math.IsInf(amount, 0) || amount <= 0 {
			return nil, 0, fmt.Errorf("Binance margin borrow history contains invalid amount for transaction %d", row.TxID)
		}
		records = append(records, MarginBorrowRecord{TransferID: row.TxID, Asset: row.Asset, Amount: amount, Status: row.Status, Timestamp: row.Timestamp})
	}
	return records, response.Total, nil
}

// GetOrderFills queries the margin-account trade ledger for a specific order.
// Base-asset commissions are preserved separately so repayment uses net received quantity.
func (b *BinanceSpotMarginAdapter) GetOrderFills(ctx context.Context, symbol string, orderID int64) ([]*OrderFill, error) {
	if b == nil || b.marginClient == nil || b.BinanceSpotAdapter == nil {
		return nil, fmt.Errorf("Binance spot margin adapter is unavailable")
	}
	sym := symbol
	if sym == "" {
		sym = b.symbol
	}
	order, err := b.GetOrder(ctx, sym, orderID)
	if err != nil {
		return nil, fmt.Errorf("load margin order %d before querying fills: %w", orderID, err)
	}
	if order.CreatedAt.IsZero() {
		return nil, fmt.Errorf("margin order %d has no creation time; refusing incomplete fill lookup", orderID)
	}
	trades, err := b.marginClient.GetTradesByOrder(ctx, sym, orderID, order.CreatedAt.Add(-time.Second).UnixMilli(), false)
	if err != nil {
		return nil, err
	}
	fills := make([]*OrderFill, 0, len(trades))
	for _, trade := range trades {
		price, priceErr := strconv.ParseFloat(trade.Price, 64)
		qty, qtyErr := strconv.ParseFloat(trade.Quantity, 64)
		commission, commissionErr := strconv.ParseFloat(trade.Commission, 64)
		if priceErr != nil || qtyErr != nil || commissionErr != nil || price <= 0 || qty <= 0 || commission < 0 {
			return nil, fmt.Errorf("margin order %d trade %d contains invalid price, quantity, or commission", orderID, trade.ID)
		}
		commissionAsset := trade.CommissionAsset
		baseFeeQty := 0.0
		if strings.EqualFold(commissionAsset, b.baseAsset) {
			baseFeeQty = commission
		}
		side := SideSell
		if trade.IsBuyer {
			side = SideBuy
		}
		fills = append(fills, &OrderFill{
			OrderID: orderID, TradeID: strconv.FormatInt(trade.ID, 10), Symbol: trade.Symbol,
			Side: side, Price: price, Quantity: qty, Commission: commission,
			CommissionAsset: commissionAsset, TradeTime: trade.Time, IsMaker: trade.IsMaker, BaseFeeQty: baseFeeQty,
		})
	}
	return fills, nil
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
		cumulativeQuote, _ := strconv.ParseFloat(o.CummulativeQuoteQuantity, 64)
		orders = append(orders, &Order{
			OrderID:       o.OrderID,
			ClientOrderID: o.ClientOrderID,
			Symbol:        o.Symbol,
			Side:          Side(o.Side),
			Type:          OrderType(o.Type),
			Price:         price,
			Quantity:      qty,
			ExecutedQty:   execQty,
			AvgPrice:      cumulativeAveragePrice(cumulativeQuote, execQty),
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
