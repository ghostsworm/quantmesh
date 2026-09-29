package coinsph

import (
	"context"
	"encoding/json"
	"fmt"
)

// GetOrderByClientOrderID requires one exact match; the venue may return a list when IDs have been reused.
func (c *CoinsphClient) GetOrderByClientOrderID(ctx context.Context, symbol, clientOrderID string) (*OrderInfo, error) {
	if symbol == "" || clientOrderID == "" {
		return nil, fmt.Errorf("Coins.ph 查詢訂單需要交易對和客戶端訂單 ID")
	}
	data, err := c.request(ctx, "GET", "/openapi/v1/order", map[string]string{
		"symbol": symbol, "origClientOrderId": clientOrderID,
	}, nil, true)
	if err != nil {
		return nil, err
	}
	var single OrderInfo
	if err := json.Unmarshal(data, &single); err == nil {
		if single.ClientOrderID != clientOrderID {
			return nil, fmt.Errorf("Coins.ph 訂單響應的客戶端 ID 不匹配")
		}
		return &single, nil
	}
	var orders []OrderInfo
	if err := json.Unmarshal(data, &orders); err != nil {
		return nil, fmt.Errorf("解析 Coins.ph 訂單信息失敗: %w", err)
	}
	var matched *OrderInfo
	for i := range orders {
		if orders[i].ClientOrderID != clientOrderID {
			continue
		}
		if matched != nil {
			return nil, fmt.Errorf("Coins.ph 訂單客戶端 ID 不唯一(clientOrderId=%s)", clientOrderID)
		}
		matched = &orders[i]
	}
	if matched == nil {
		return nil, fmt.Errorf("Coins.ph 訂單不存在(clientOrderId=%s)", clientOrderID)
	}
	return matched, nil
}
