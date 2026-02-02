package event

import (
	"fmt"
)

// BuildMessageFromData 根據事件類型和 data 構建顯示消息（用於讀取時補齊 Storage 寫入的舊數據）
func BuildMessageFromData(eventType EventType, data map[string]interface{}) string {
	if data == nil {
		return fmt.Sprintf("事件類型: %s", eventType)
	}
	extract := func(key string) string {
		if v, ok := data[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return ""
	}
	switch eventType {
	case EventTypeOrderPlaced, EventTypeOrderFilled, EventTypeOrderCanceled, EventTypeOrderFailed:
		symbol := extract("symbol")
		side := extract("side")
		price := data["price"]
		quantity := data["quantity"]
		return fmt.Sprintf("%s %s %.8f @ %v", symbol, side, quantity, price)
	case EventTypeWebSocketDisconnected, EventTypeWebSocketReconnected:
		exchange := extract("exchange")
		symbol := extract("symbol")
		reason := extract("reason")
		if reason != "" {
			return fmt.Sprintf("%s %s WebSocket: %s", exchange, symbol, reason)
		}
		return fmt.Sprintf("%s %s WebSocket 連接狀態變化", exchange, symbol)
	case EventTypeAPIRateLimited, EventTypeAPIServerError, EventTypeAPIRequestFailed:
		exchange := extract("exchange")
		endpoint := extract("endpoint")
		errorMsg := extract("error")
		if endpoint != "" {
			return fmt.Sprintf("%s API [%s]: %s", exchange, endpoint, errorMsg)
		}
		return fmt.Sprintf("%s API 錯誤: %s", exchange, errorMsg)
	case EventTypePriceVolatility:
		symbol := extract("symbol")
		oldPrice := data["old_price"]
		newPrice := data["new_price"]
		changePercent := data["change_percent"]
		return fmt.Sprintf("%s 價格波動: %v → %v (%v%%)", symbol, oldPrice, newPrice, changePercent)
	case EventTypeSystemCPUHigh, EventTypeSystemMemoryHigh:
		resourceType := extract("resource_type")
		usage := data["usage"]
		threshold := data["threshold"]
		return fmt.Sprintf("%s 使用率 %v%% (閾值: %v%%)", resourceType, usage, threshold)
	case EventTypePrecisionAdjustment:
		symbol := extract("symbol")
		calculatedQty := data["calculated_qty"]
		minQty := data["min_qty"]
		action := extract("action")
		if action == "pause" {
			return fmt.Sprintf("[%s] 下單數量 %.8f 低於最小精度 %.8f，交易已自動暫停", symbol, calculatedQty, minQty)
		}
		return fmt.Sprintf("[%s] 下單數量精度調整: %.8f -> %.8f", symbol, calculatedQty, minQty)
	case EventTypeRiskTriggered, EventTypeRiskRecovered:
		symbol := extract("symbol")
		reason := extract("reason")
		price := data["price"]
		if eventType == EventTypeRiskRecovered {
			if price != nil {
				return fmt.Sprintf("[%s] 風控解除，價格 %.2f，已恢復自動交易", symbol, price)
			}
			return fmt.Sprintf("[%s] 風控解除，已恢復自動交易", symbol)
		}
		if reason != "" {
			if price != nil {
				return fmt.Sprintf("[%s] 觸發風控 (價格 %.2f)：%s", symbol, price, reason)
			}
			return fmt.Sprintf("[%s] 觸發風控：%s", symbol, reason)
		}
		if price != nil {
			return fmt.Sprintf("[%s] 觸發風控，價格 %.2f", symbol, price)
		}
		return fmt.Sprintf("[%s] 觸發風控", symbol)
	default:
		if msg := extract("message"); msg != "" {
			return msg
		}
		if err := extract("error"); err != "" {
			return err
		}
		if title := extract("title"); title != "" {
			return title
		}
		return fmt.Sprintf("事件類型: %s", eventType)
	}
}
