package strategy

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"quantmesh/config"
	"quantmesh/position"
)

const (
	signalActionOpenLong  = "open_long"
	signalActionCloseLong = "close_long"
)

func signalStrategyFloat(cfg map[string]interface{}, keys []string, defaultValue float64) float64 {
	for _, key := range keys {
		v, ok := cfg[key]
		if !ok {
			continue
		}
		switch val := v.(type) {
		case float64:
			if signalFinite(val) {
				return val
			}
		case float32:
			f := float64(val)
			if signalFinite(f) {
				return f
			}
		case int:
			return float64(val)
		case int64:
			return float64(val)
		case string:
			if parsed, err := strconv.ParseFloat(strings.TrimSpace(val), 64); err == nil && signalFinite(parsed) {
				return parsed
			}
		}
	}
	return defaultValue
}

func signalFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func signalStrategyString(cfg map[string]interface{}, key, defaultValue string) string {
	if cfg == nil {
		return defaultValue
	}
	if v, ok := cfg[key].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return defaultValue
}

func signalStrategySymbol(cfg *config.Config, strategyCfg map[string]interface{}) string {
	defaultSymbol := ""
	if cfg != nil {
		defaultSymbol = strings.TrimSpace(cfg.Trading.Symbol)
	}
	if defaultSymbol == "" {
		defaultSymbol = "BTCUSDT"
	}
	return signalStrategyString(strategyCfg, "symbol", defaultSymbol)
}

func signalStrategyOrderAmount(strategyCfg map[string]interface{}) float64 {
	amount := signalStrategyFloat(strategyCfg, []string{
		"order_amount",
		"trade_amount",
		"initial_amount",
		"base_order_amount",
	}, 100)
	if amount <= 0 {
		return 100
	}
	return amount
}

func signalStrategySlippage(strategyCfg map[string]interface{}) float64 {
	slippage := signalStrategyFloat(strategyCfg, []string{"slippage", "price_slippage"}, 0.001)
	if slippage < 0 {
		return 0
	}
	if slippage > 0.05 {
		return 0.05
	}
	return slippage
}

func signalRound(value float64, decimals int) float64 {
	if decimals < 0 {
		decimals = 0
	}
	factor := math.Pow10(decimals)
	return math.Round(value*factor) / factor
}

func signalFloor(value float64, decimals int) float64 {
	if decimals < 0 {
		decimals = 0
	}
	factor := math.Pow10(decimals)
	return math.Floor(value*factor) / factor
}

func signalPriceDecimals(exchange position.IExchange) int {
	if exchange == nil {
		return 2
	}
	decimals := exchange.GetPriceDecimals()
	if decimals <= 0 {
		return 2
	}
	return decimals
}

func signalQuantityDecimals(exchange position.IExchange) int {
	if exchange == nil {
		return 6
	}
	decimals := exchange.GetQuantityDecimals()
	if decimals <= 0 {
		return 6
	}
	return decimals
}

func signalIsFuturesMarket(cfg *config.Config) bool {
	if cfg == nil {
		return true
	}
	marketType := strings.ToLower(strings.TrimSpace(cfg.Trading.MarketType))
	return marketType == "" || marketType == "futures" || marketType == "swap" || marketType == "perpetual"
}

func signalOrderStatusFilled(status string) bool {
	status = strings.ToUpper(strings.TrimSpace(status))
	return status == "FILLED" || status == "FULLY_FILLED" || status == "CLOSED"
}

func signalOrderStatusTerminal(status string) bool {
	status = strings.ToUpper(strings.TrimSpace(status))
	return status == "CANCELED" || status == "CANCELLED" || status == "REJECTED" || status == "EXPIRED" || status == "FAILED"
}

func signalOrderMatches(order *Order, update *position.OrderUpdate) bool {
	if order == nil || update == nil {
		return false
	}
	if update.OrderID > 0 && order.OrderID == update.OrderID {
		return true
	}
	return update.ClientOrderID != "" && order.ClientOrderID == update.ClientOrderID
}

func signalClientOrderID(strategyName, action string) string {
	clean := strings.NewReplacer(" ", "_", "/", "_", ":", "_").Replace(strategyName)
	return fmt.Sprintf("%s_%s_%d", clean, action, time.Now().UnixNano())
}
