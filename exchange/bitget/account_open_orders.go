package bitget

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
)

const bitgetAccountOpenOrdersPageSize = 100
const bitgetAccountOpenOrdersMaxPages = 1000

type bitgetPendingOrder struct {
	Symbol     string `json:"symbol"`
	Size       string `json:"size"`
	OrderID    string `json:"orderId"`
	ClientOID  string `json:"clientOid"`
	BaseVolume string `json:"baseVolume"`
	Price      string `json:"price"`
	Side       string `json:"side"`
	Status     string `json:"status"`
	PriceAvg   string `json:"priceAvg"`
	UTime      string `json:"uTime"`
	OrderType  string `json:"orderType"`
}

type bitgetSpotOpenOrder struct {
	OrderID    string `json:"orderId"`
	ClientOID  string `json:"clientOid"`
	Symbol     string `json:"symbol"`
	Side       string `json:"side"`
	OrderType  string `json:"orderType"`
	Price      string `json:"price"`
	Size       string `json:"size"`
	FilledSize string `json:"baseVolume"`
	AvgPrice   string `json:"basePrice"`
	Status     string `json:"status"`
	UpdateTime string `json:"uTime"`
}

func (c *Client) GetAccountFuturesOpenOrders(ctx context.Context) ([]bitgetPendingOrder, error) {
	var all []bitgetPendingOrder
	seenIDs := make(map[string]struct{})
	for _, productType := range []string{"USDT-FUTURES", "USDC-FUTURES", "COIN-FUTURES"} {
		for _, status := range []string{"live", "partially_filled"} {
			cursor := ""
			seenCursors := make(map[string]struct{})
			for pageNumber := 0; pageNumber < bitgetAccountOpenOrdersMaxPages; pageNumber++ {
				query := url.Values{}
				query.Set("productType", productType)
				query.Set("status", status)
				query.Set("limit", strconv.Itoa(bitgetAccountOpenOrdersPageSize))
				if cursor != "" {
					query.Set("idLessThan", cursor)
				}
				resp, err := c.DoRequest(ctx, "GET", "/api/v2/mix/order/orders-pending?"+query.Encode(), nil)
				if err != nil {
					return nil, fmt.Errorf("query Bitget %s %s account-wide futures open orders: %w", productType, status, err)
				}
				var page struct {
					EntrustedList json.RawMessage `json:"entrustedList"`
					EndID         string          `json:"endId"`
				}
				if err := json.Unmarshal(resp.Data, &page); err != nil {
					return nil, fmt.Errorf("decode Bitget %s futures open-order page: %w", productType, err)
				}
				if len(page.EntrustedList) == 0 || string(page.EntrustedList) == "null" {
					return nil, fmt.Errorf("Bitget %s futures open-order page has missing or null entrustedList", productType)
				}
				var orders []bitgetPendingOrder
				if err := json.Unmarshal(page.EntrustedList, &orders); err != nil {
					return nil, fmt.Errorf("decode Bitget %s futures entrustedList: %w", productType, err)
				}
				if orders == nil {
					return nil, fmt.Errorf("Bitget %s futures entrustedList is null", productType)
				}
				for _, order := range orders {
					id := strings.TrimSpace(order.OrderID)
					if id == "" || strings.TrimSpace(order.Symbol) == "" {
						return nil, fmt.Errorf("Bitget %s futures snapshot contains order without identity", productType)
					}
					key := productType + ":" + id
					if _, exists := seenIDs[key]; exists {
						return nil, fmt.Errorf("Bitget futures snapshot repeated order %s", key)
					}
					seenIDs[key] = struct{}{}
				}
				all = append(all, orders...)
				if len(orders) < bitgetAccountOpenOrdersPageSize {
					break
				}
				cursor = strings.TrimSpace(page.EndID)
				if cursor == "" {
					return nil, fmt.Errorf("Bitget %s futures full page omitted endId", productType)
				}
				if _, exists := seenCursors[cursor]; exists {
					return nil, fmt.Errorf("Bitget %s futures pagination repeated endId %s", productType, cursor)
				}
				seenCursors[cursor] = struct{}{}
				if pageNumber == bitgetAccountOpenOrdersMaxPages-1 {
					return nil, fmt.Errorf("Bitget %s futures pagination exceeded %d pages", productType, bitgetAccountOpenOrdersMaxPages)
				}
			}
		}
	}
	return all, nil
}

func (c *Client) GetAccountSpotOpenOrders(ctx context.Context) ([]bitgetSpotOpenOrder, error) {
	var all []bitgetSpotOpenOrder
	seenIDs := make(map[string]struct{})
	for _, orderType := range []string{"normal", "tpsl"} {
		cursor := ""
		seenCursors := make(map[string]struct{})
		for pageNumber := 0; pageNumber < bitgetAccountOpenOrdersMaxPages; pageNumber++ {
			query := url.Values{}
			query.Set("limit", strconv.Itoa(bitgetAccountOpenOrdersPageSize))
			query.Set("tpslType", orderType)
			if cursor != "" {
				query.Set("idLessThan", cursor)
			}
			resp, err := c.DoRequest(ctx, "GET", "/api/v2/spot/trade/unfilled-orders?"+query.Encode(), nil)
			if err != nil {
				return nil, fmt.Errorf("query Bitget spot %s open orders: %w", orderType, err)
			}
			if len(resp.Data) == 0 || string(resp.Data) == "null" {
				return nil, fmt.Errorf("Bitget spot %s open-order response has missing or null list", orderType)
			}
			var orders []bitgetSpotOpenOrder
			if err := json.Unmarshal(resp.Data, &orders); err != nil {
				return nil, fmt.Errorf("decode Bitget spot %s open orders: %w", orderType, err)
			}
			if orders == nil {
				return nil, fmt.Errorf("Bitget spot %s open-order list is null", orderType)
			}
			for _, order := range orders {
				id := strings.TrimSpace(order.OrderID)
				if id == "" || strings.TrimSpace(order.Symbol) == "" {
					return nil, fmt.Errorf("Bitget spot %s snapshot contains order without identity", orderType)
				}
				if _, exists := seenIDs[id]; exists {
					return nil, fmt.Errorf("Bitget spot snapshot repeated order %s", id)
				}
				seenIDs[id] = struct{}{}
			}
			all = append(all, orders...)
			if len(orders) < bitgetAccountOpenOrdersPageSize {
				break
			}
			cursor = strings.TrimSpace(orders[len(orders)-1].OrderID)
			if cursor == "" {
				return nil, fmt.Errorf("Bitget spot %s full page omitted last order ID", orderType)
			}
			if _, exists := seenCursors[cursor]; exists {
				return nil, fmt.Errorf("Bitget spot %s pagination repeated cursor %s", orderType, cursor)
			}
			seenCursors[cursor] = struct{}{}
			if pageNumber == bitgetAccountOpenOrdersMaxPages-1 {
				return nil, fmt.Errorf("Bitget spot %s pagination exceeded %d pages", orderType, bitgetAccountOpenOrdersMaxPages)
			}
		}
	}
	return all, nil
}

func parseBitgetOrderNumber(field, value string, optional bool) (float64, error) {
	if strings.TrimSpace(value) == "" && optional {
		return 0, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, fmt.Errorf("invalid Bitget %s value %q", field, value)
	}
	return parsed, nil
}

func bitgetOpenOrderSide(value string) (Side, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "buy":
		return SideBuy, nil
	case "sell":
		return SideSell, nil
	default:
		return "", fmt.Errorf("unknown Bitget open-order side %q", value)
	}
}

func bitgetOpenOrderStatus(value string) (OrderStatus, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "live", "new":
		return "NEW", nil
	case "partially_filled", "partial-fill":
		return "PARTIALLY_FILLED", nil
	default:
		return "", fmt.Errorf("unknown Bitget open-order status %q", value)
	}
}
