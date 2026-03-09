package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"quantmesh/agent/types"
)

// DataProvider 数据提供者接口（由 main.go 注入）
type DataProvider interface {
	GetOrders(ctx context.Context, filter *OrderFilter) ([]*Order, error)
	GetPositions(symbolKey string) ([]PositionSlot, error)
	GetBots() ([]BotInfo, error)
	GetBotByID(botID string) (*BotDetail, error)
}

// OrderFilter 订单过滤器
type OrderFilter struct {
	BotID    string
	Exchange string
	Symbol   string
	Status   string
	Side     string
	Limit    int64
	Offset   int64
}

// Order 订单数据
type Order struct {
	OrderID       int64
	BotID         string
	ClientOrderID string
	Symbol        string
	Side          string
	Exchange      string
	Type          string
	Price         float64
	Quantity      float64
	FilledQty     float64
	Status        string
	RealizedPnL   *float64
	StrategyName  string
	StrategyType  string
	OrderSource   string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// PositionSlot 持仓槽位信息
type PositionSlot struct {
	Price         float64
	PositionQty   float64
	PositionStatus string
	AvgBuyPrice   float64
	StrategyName  string
	StrategyType  string
}

// BotInfo Bot 基本信息
type BotInfo struct {
	BotID        string
	Name         string
	Exchange     string
	Symbol       string
	MarketType   string
	Running      bool
	CurrentPrice float64
	TotalPnL     float64
	TotalTrades  int
}

// BotDetail Bot 详情信息
type BotDetail struct {
	BotInfo
	Config      map[string]interface{}
	Strategies  []StrategyInfo
	Leverage    float64
	TotalAllocatedCapital float64
	PriceInterval         float64
	OrderQuantity         float64
}

// StrategyInfo 策略信息
type StrategyInfo struct {
	Type   string
	Weight float64
	Name   string
}

var dataProvider DataProvider

// SetDataProvider 设置数据提供者
func SetDataProvider(provider DataProvider) {
	dataProvider = provider
}

// GetOrdersTool 获取订单历史工具
type GetOrdersTool struct {
	BaseTool
}

func NewGetOrdersTool() *GetOrdersTool {
	return &GetOrdersTool{
		BaseTool: BaseTool{
			name:        "get_orders",
			description: "获取订单历史数据，支持按 Bot ID、交易所、交易对、状态等筛选",
			category:    CategoryMarket,
			schema: CreateParameterSchema(map[string]SchemaProperty{
				"bot_id": {
					Type:        "string",
					Description: "Bot ID（可选，不提供则查询所有 Bot 的订单）",
					Required:    false,
				},
				"exchange": {
					Type:        "string",
					Description: "交易所名称（如 binance, okx）",
					Required:    false,
				},
				"symbol": {
					Type:        "string",
					Description: "交易对（如 BTCUSDT）",
					Required:    false,
				},
				"status": {
					Type:        "string",
					Description: "订单状态（如 FILLED, CANCELED, NEW）",
					Required:    false,
				},
				"side": {
					Type:        "string",
					Description: "订单方向（BUY 或 SELL）",
					Required:    false,
				},
				"limit": {
					Type:        "number",
					Description: "返回数量限制（默认100，最大500）",
					Required:    false,
				},
				"offset": {
					Type:        "number",
					Description: "偏移量（用于分页）",
					Required:    false,
				},
			}),
		},
	}
}

func (t *GetOrdersTool) Execute(ctx context.Context, params map[string]interface{}) (types.ToolResult, error) {
	if dataProvider == nil {
		return types.ToolResult{
			Error: "数据提供者未初始化",
		}, nil
	}

	botID, _ := params["bot_id"].(string)
	exchange, _ := params["exchange"].(string)
	symbol, _ := params["symbol"].(string)
	status, _ := params["status"].(string)
	side, _ := params["side"].(string)
	limit := int64(100)
	if l, ok := params["limit"].(float64); ok {
		limit = int64(l)
	}
	offset := int64(0)
	if o, ok := params["offset"].(float64); ok {
		offset = int64(o)
	}

	filter := &OrderFilter{
		BotID:    botID,
		Exchange: exchange,
		Symbol:   symbol,
		Status:   status,
		Side:     side,
		Limit:    limit,
		Offset:   offset,
	}

	orders, err := dataProvider.GetOrders(ctx, filter)
	if err != nil {
		return types.ToolResult{
			Error: fmt.Sprintf("获取订单失败: %v", err),
		}, nil
	}

	// 转换为易读的格式
	orderList := make([]map[string]interface{}, 0, len(orders))
	for _, o := range orders {
		orderList = append(orderList, map[string]interface{}{
			"bot_id":          o.BotID,
			"order_id":        o.OrderID,
			"client_order_id": o.ClientOrderID,
			"symbol":          o.Symbol,
			"exchange":        o.Exchange,
			"side":            o.Side,
			"type":            o.Type,
			"price":           o.Price,
			"quantity":        o.Quantity,
			"filled_qty":      o.FilledQty,
			"status":          o.Status,
			"realized_pnl":    o.RealizedPnL,
			"strategy_name":   o.StrategyName,
			"strategy_type":   o.StrategyType,
			"order_source":    o.OrderSource,
			"created_at":      o.CreatedAt.Format(time.RFC3339),
		})
	}

	return types.ToolResult{
		Result: map[string]interface{}{
			"orders": orderList,
			"count":  len(orderList),
			"filter": map[string]interface{}{
				"bot_id":   botID,
				"exchange": exchange,
				"symbol":   symbol,
				"status":   status,
				"side":     side,
				"limit":    limit,
				"offset":   offset,
			},
		},
	}, nil
}

func (t *GetOrdersTool) AssessRisk(_ map[string]interface{}) types.SecurityLevel {
	return types.SecurityLevelLow
}

// GetPositionsTool 获取持仓信息工具
type GetPositionsTool struct {
	BaseTool
}

func NewGetPositionsTool() *GetPositionsTool {
	return &GetPositionsTool{
		BaseTool: BaseTool{
			name:        "get_positions",
			description: "获取当前持仓信息，包括持仓价格、数量、未实现盈亏等",
			category:    CategoryMarket,
			schema: CreateParameterSchema(map[string]SchemaProperty{
				"symbol_key": {
					Type:        "string",
					Description: "交易对唯一标识（格式：exchange:symbol:market_type，如 binance:BTCUSDT:futures）",
					Required:    false,
				},
			}),
		},
	}
}

func (t *GetPositionsTool) Execute(ctx context.Context, params map[string]interface{}) (types.ToolResult, error) {
	if dataProvider == nil {
		return types.ToolResult{
			Error: "数据提供者未初始化",
		}, nil
	}

	symbolKey, _ := params["symbol_key"].(string)

	positions, err := dataProvider.GetPositions(symbolKey)
	if err != nil {
		return types.ToolResult{
			Error: fmt.Sprintf("获取持仓失败: %v", err),
		}, nil
	}

	// 计算汇总信息
	totalQuantity := 0.0
	totalValue := 0.0
	positionCount := 0
	var totalCost float64

	positionList := make([]map[string]interface{}, 0)
	for _, pos := range positions {
		if pos.PositionStatus == "FILLED" && pos.PositionQty > 0.000001 {
			positionCount++
			totalQuantity += pos.PositionQty
			cost := pos.Price * pos.PositionQty
			totalCost += cost

			positionList = append(positionList, map[string]interface{}{
				"price":          pos.Price,
				"quantity":       pos.PositionQty,
				"value":          cost,
				"avg_buy_price":  pos.AvgBuyPrice,
				"strategy_name":  pos.StrategyName,
				"strategy_type":  pos.StrategyType,
			})
		}
	}

	averagePrice := 0.0
	if totalQuantity > 0 {
		averagePrice = totalCost / totalQuantity
	}

	return types.ToolResult{
		Result: map[string]interface{}{
			"summary": map[string]interface{}{
				"total_quantity": totalQuantity,
				"total_value":    totalCost,
				"position_count": positionCount,
				"average_price":  averagePrice,
			},
			"positions": positionList,
			"symbol_key": symbolKey,
		},
	}, nil
}

func (t *GetPositionsTool) AssessRisk(_ map[string]interface{}) types.SecurityLevel {
	return types.SecurityLevelLow
}

// GetMarketTickerTool 获取市场行情工具
type GetMarketTickerTool struct {
	BaseTool
}

func NewGetMarketTickerTool() *GetMarketTickerTool {
	return &GetMarketTickerTool{
		BaseTool: BaseTool{
			name:        "get_market_ticker",
			description: "获取市场实时行情数据，包括最新价、标记价、24h高低价等",
			category:    CategoryMarket,
			schema: CreateParameterSchema(map[string]SchemaProperty{
				"exchange": {
					Type:        "string",
					Description: "交易所名称（如 binance, okx, bybit, bitget）",
					Required:    true,
				},
				"symbol": {
					Type:        "string",
					Description: "交易对（如 BTCUSDT）",
					Required:    true,
				},
				"market_type": {
					Type:        "string",
					Description: "市场类型（spot 或 futures，默认 futures）",
					Required:    false,
					Enum:        []string{"spot", "futures"},
					Default:     "futures",
				},
			}),
		},
	}
}

func (t *GetMarketTickerTool) Execute(ctx context.Context, params map[string]interface{}) (types.ToolResult, error) {
	exchange, _ := params["exchange"].(string)
	symbol, _ := params["symbol"].(string)
	marketType := "futures"
	if mt, ok := params["market_type"].(string); ok {
		marketType = mt
	}

	if exchange == "" || symbol == "" {
		return types.ToolResult{
			Error: "缺少必需参数：exchange 和 symbol",
		}, nil
	}

	ticker, err := fetchMarketTicker(exchange, symbol, marketType)
	if err != nil {
		return types.ToolResult{
			Error: fmt.Sprintf("获取行情失败: %v", err),
		}, nil
	}

	return types.ToolResult{
		Result: map[string]interface{}{
			"exchange":    exchange,
			"symbol":      symbol,
			"market_type": marketType,
			"mark_price":  ticker.MarkPrice,
			"last_price":  ticker.LastPrice,
			"high_24h":    ticker.High24h,
			"low_24h":     ticker.Low24h,
			"price_change_24h": ticker.LastPrice - ticker.Low24h,
			"price_change_percent": ((ticker.LastPrice - ticker.Low24h) / ticker.Low24h) * 100,
		},
	}, nil
}

func (t *GetMarketTickerTool) AssessRisk(_ map[string]interface{}) types.SecurityLevel {
	return types.SecurityLevelLow
}

// MarketTicker 市场行情数据
type MarketTicker struct {
	MarkPrice float64
	LastPrice float64
	High24h   float64
	Low24h    float64
}

// fetchMarketTicker 获取市场行情（从各交易所 API）
func fetchMarketTicker(exchange, symbol, marketType string) (*MarketTicker, error) {
	var url string

	switch exchange {
	case "binance":
		if marketType == "spot" {
			url = fmt.Sprintf("https://api.binance.com/api/v3/ticker/24hr?symbol=%s", symbol)
		} else {
			url = fmt.Sprintf("https://fapi.binance.com/fapi/v1/ticker/24hr?symbol=%s", symbol)
		}
	case "okx":
		// OKX 需要转换 symbol 格式
		instID := symbol
		if marketType == "futures" {
			instID = convertToOKXInstID(symbol)
		} else if len(symbol) > 4 {
			instID = symbol[:len(symbol)-4] + "-" + symbol[len(symbol)-4:]
		}
		url = fmt.Sprintf("https://www.okx.com/api/v5/market/ticker?instId=%s", instID)
	case "bybit":
		category := "linear"
		if marketType == "spot" {
			category = "spot"
		}
		url = fmt.Sprintf("https://api.bybit.com/v5/market/tickers?category=%s&symbol=%s", category, symbol)
	case "bitget":
		if marketType == "spot" {
			url = fmt.Sprintf("https://api.bitget.com/api/v2/spot/market/tickers?symbol=%s", symbol)
		} else {
			url = fmt.Sprintf("https://api.bitget.com/api/v2/mix/market/ticker?productType=USDT-FUTURES&symbol=%s", symbol)
		}
	default:
		return nil, fmt.Errorf("不支持的交易所: %s", exchange)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return parseTickerResponse(exchange, marketType, body)
}

// parseTickerResponse 解析交易所响应
func parseTickerResponse(exchange, marketType string, body []byte) (*MarketTicker, error) {
	ticker := &MarketTicker{}

	switch exchange {
	case "binance":
		var data struct {
			LastPrice string `json:"lastPrice"`
			HighPrice string `json:"highPrice"`
			LowPrice  string `json:"lowPrice"`
			MarkPrice string `json:"markPrice"`
		}
		if err := json.Unmarshal(body, &data); err != nil {
			return nil, err
		}
		ticker.LastPrice, _ = strconv.ParseFloat(data.LastPrice, 64)
		ticker.High24h, _ = strconv.ParseFloat(data.HighPrice, 64)
		ticker.Low24h, _ = strconv.ParseFloat(data.LowPrice, 64)
		if data.MarkPrice != "" {
			ticker.MarkPrice, _ = strconv.ParseFloat(data.MarkPrice, 64)
		} else {
			ticker.MarkPrice = ticker.LastPrice
		}

	case "okx":
		var data struct {
			Data []struct {
				Last   string `json:"last"`
				High24h string `json:"high24h"`
				Low24h  string `json:"low24h"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &data); err != nil || len(data.Data) == 0 {
			return nil, fmt.Errorf("解析 OKX 响应失败")
		}
		d := data.Data[0]
		ticker.LastPrice, _ = strconv.ParseFloat(d.Last, 64)
		ticker.High24h, _ = strconv.ParseFloat(d.High24h, 64)
		ticker.Low24h, _ = strconv.ParseFloat(d.Low24h, 64)
		ticker.MarkPrice = ticker.LastPrice

	case "bybit":
		var data struct {
			Result struct {
				List []struct {
					LastPrice     string `json:"lastPrice"`
					HighPrice24h  string `json:"highPrice24h"`
					LowPrice24h   string `json:"lowPrice24h"`
					MarkPrice     string `json:"markPrice"`
				} `json:"list"`
			} `json:"result"`
		}
		if err := json.Unmarshal(body, &data); err != nil || len(data.Result.List) == 0 {
			return nil, fmt.Errorf("解析 Bybit 响应失败")
		}
		d := data.Result.List[0]
		ticker.LastPrice, _ = strconv.ParseFloat(d.LastPrice, 64)
		ticker.High24h, _ = strconv.ParseFloat(d.HighPrice24h, 64)
		ticker.Low24h, _ = strconv.ParseFloat(d.LowPrice24h, 64)
		if d.MarkPrice != "" {
			ticker.MarkPrice, _ = strconv.ParseFloat(d.MarkPrice, 64)
		} else {
			ticker.MarkPrice = ticker.LastPrice
		}

	case "bitget":
		var data struct {
			Data []struct {
				LastPr    string `json:"lastPr"`
				High24    string `json:"high24h"`
				Low24     string `json:"low24h"`
				MarkPrice string `json:"markPrice"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &data); err != nil || len(data.Data) == 0 {
			return nil, fmt.Errorf("解析 Bitget 响应失败")
		}
		d := data.Data[0]
		ticker.LastPrice, _ = strconv.ParseFloat(d.LastPr, 64)
		ticker.High24h, _ = strconv.ParseFloat(d.High24, 64)
		ticker.Low24h, _ = strconv.ParseFloat(d.Low24, 64)
		if d.MarkPrice != "" {
			ticker.MarkPrice, _ = strconv.ParseFloat(d.MarkPrice, 64)
		} else {
			ticker.MarkPrice = ticker.LastPrice
		}

	default:
		return nil, fmt.Errorf("不支持的交易所: %s", exchange)
	}

	return ticker, nil
}

// convertToOKXInstID 转换为 OKX 的 instId 格式
func convertToOKXInstID(symbol string) string {
	// 将 BTCUSDT 转换为 BTC-USDT-SWAP 或类似格式
	if len(symbol) > 4 {
		base := symbol[:len(symbol)-4]
		quote := symbol[len(symbol)-4:]
		return base + "-" + quote + "-SWAP"
	}
	return symbol
}

// GetBotStatusTool 获取 Bot 状态工具
type GetBotStatusTool struct {
	BaseTool
}

func NewGetBotStatusTool() *GetBotStatusTool {
	return &GetBotStatusTool{
		BaseTool: BaseTool{
			name:        "get_bot_status",
			description: "获取 Bot 运行状态和配置信息",
			category:    CategorySystem,
			schema: CreateParameterSchema(map[string]SchemaProperty{
				"bot_id": {
					Type:        "string",
					Description: "Bot ID（不提供则返回所有 Bot 列表）",
					Required:    false,
				},
			}),
		},
	}
}

func (t *GetBotStatusTool) Execute(ctx context.Context, params map[string]interface{}) (types.ToolResult, error) {
	if dataProvider == nil {
		return types.ToolResult{
			Error: "数据提供者未初始化",
		}, nil
	}

	botID, _ := params["bot_id"].(string)

	if botID != "" {
		// 获取单个 Bot 详情
		bot, err := dataProvider.GetBotByID(botID)
		if err != nil {
			return types.ToolResult{
				Error: fmt.Sprintf("获取 Bot 失败: %v", err),
			}, nil
		}
		if bot == nil {
			return types.ToolResult{
				Error: fmt.Sprintf("Bot 不存在: %s", botID),
			}, nil
		}

		return types.ToolResult{
			Result: map[string]interface{}{
				"bot": map[string]interface{}{
					"bot_id":                 bot.BotID,
					"name":                   bot.Name,
					"exchange":               bot.Exchange,
					"symbol":                 bot.Symbol,
					"market_type":            bot.MarketType,
					"running":                bot.Running,
					"current_price":          bot.CurrentPrice,
					"total_pnl":              bot.TotalPnL,
					"total_trades":           bot.TotalTrades,
					"leverage":               bot.Leverage,
					"total_allocated_capital": bot.TotalAllocatedCapital,
					"price_interval":         bot.PriceInterval,
					"order_quantity":         bot.OrderQuantity,
					"strategies":             bot.Strategies,
				},
			},
		}, nil
	}

	// 获取所有 Bot 列表
	bots, err := dataProvider.GetBots()
	if err != nil {
		return types.ToolResult{
			Error: fmt.Sprintf("获取 Bot 列表失败: %v", err),
		}, nil
	}

	botList := make([]map[string]interface{}, 0, len(bots))
	for _, b := range bots {
		botList = append(botList, map[string]interface{}{
			"bot_id":        b.BotID,
			"name":          b.Name,
			"exchange":      b.Exchange,
			"symbol":        b.Symbol,
			"market_type":   b.MarketType,
			"running":       b.Running,
			"current_price": b.CurrentPrice,
			"total_pnl":     b.TotalPnL,
			"total_trades":  b.TotalTrades,
		})
	}

	return types.ToolResult{
		Result: map[string]interface{}{
			"bots":  botList,
			"count": len(botList),
		},
	}, nil
}

func (t *GetBotStatusTool) AssessRisk(_ map[string]interface{}) types.SecurityLevel {
	return types.SecurityLevelLow
}
