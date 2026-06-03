package tools

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeDataProvider struct {
	orders       []*Order
	positions    []PositionSlot
	bots         []BotInfo
	bot          *BotDetail
	ordersErr    error
	positionsErr error
	botsErr      error
	botErr       error
	lastFilter   *OrderFilter
}

func (p *fakeDataProvider) GetOrders(_ context.Context, filter *OrderFilter) ([]*Order, error) {
	p.lastFilter = filter
	if p.ordersErr != nil {
		return nil, p.ordersErr
	}
	return p.orders, nil
}

func (p *fakeDataProvider) GetPositions(_ string) ([]PositionSlot, error) {
	if p.positionsErr != nil {
		return nil, p.positionsErr
	}
	return p.positions, nil
}

func (p *fakeDataProvider) GetBots() ([]BotInfo, error) {
	if p.botsErr != nil {
		return nil, p.botsErr
	}
	return p.bots, nil
}

func (p *fakeDataProvider) GetBotByID(_ string) (*BotDetail, error) {
	if p.botErr != nil {
		return nil, p.botErr
	}
	return p.bot, nil
}

func TestDataToolsExecuteWithProvider(t *testing.T) {
	defer SetDataProvider(nil)
	pnl := 12.5
	provider := &fakeDataProvider{
		orders: []*Order{{
			OrderID: 101, BotID: "bot-1", ClientOrderID: "client-1",
			Symbol: "BTCUSDT", Exchange: "binance", Side: "BUY", Type: "LIMIT",
			Price: 100, Quantity: 0.2, FilledQty: 0.1, Status: "FILLED",
			RealizedPnL: &pnl, StrategyName: "grid", StrategyType: "grid",
			OrderSource: "normal", CreatedAt: time.Date(2026, 6, 3, 1, 2, 3, 0, time.UTC),
		}},
		positions: []PositionSlot{
			{Price: 100, PositionQty: 0.5, PositionStatus: "FILLED", AvgBuyPrice: 98, StrategyName: "grid", StrategyType: "grid"},
			{Price: 90, PositionQty: 0, PositionStatus: "FILLED"},
			{Price: 80, PositionQty: 1, PositionStatus: "PENDING"},
		},
		bots: []BotInfo{{
			BotID: "bot-1", Name: "Grid BTC", Exchange: "binance", Symbol: "BTCUSDT",
			MarketType: "futures", Running: true, CurrentPrice: 100, TotalPnL: 8, TotalTrades: 3,
		}},
		bot: &BotDetail{
			BotInfo: BotInfo{
				BotID: "bot-1", Name: "Grid BTC", Exchange: "binance", Symbol: "BTCUSDT",
				MarketType: "futures", Running: true, CurrentPrice: 100, TotalPnL: 8, TotalTrades: 3,
			},
			Leverage: 5, TotalAllocatedCapital: 1000, PriceInterval: 10, OrderQuantity: 0.01,
			Strategies: []StrategyInfo{{Type: "grid", Weight: 1, Name: "Grid"}},
		},
	}
	SetDataProvider(provider)

	ordersResult, err := NewGetOrdersTool().Execute(context.Background(), map[string]interface{}{
		"bot_id": "bot-1", "exchange": "binance", "symbol": "BTCUSDT",
		"status": "FILLED", "side": "BUY", "limit": float64(25), "offset": float64(5),
	})
	if err != nil || ordersResult.Error != "" {
		t.Fatalf("orders result error = %q, err = %v", ordersResult.Error, err)
	}
	if provider.lastFilter == nil || provider.lastFilter.Limit != 25 || provider.lastFilter.Offset != 5 {
		t.Fatalf("filter = %#v", provider.lastFilter)
	}
	ordersPayload := ordersResult.Result.(map[string]interface{})
	if ordersPayload["count"] != 1 {
		t.Fatalf("orders count = %v", ordersPayload["count"])
	}

	positionsResult, err := NewGetPositionsTool().Execute(context.Background(), map[string]interface{}{"symbol_key": "binance:BTCUSDT:futures"})
	if err != nil || positionsResult.Error != "" {
		t.Fatalf("positions result error = %q, err = %v", positionsResult.Error, err)
	}
	positionsPayload := positionsResult.Result.(map[string]interface{})
	summary := positionsPayload["summary"].(map[string]interface{})
	if summary["position_count"] != 1 || summary["total_quantity"] != 0.5 || summary["total_value"] != 50.0 {
		t.Fatalf("summary = %#v", summary)
	}

	botsResult, err := NewGetBotStatusTool().Execute(context.Background(), map[string]interface{}{})
	if err != nil || botsResult.Error != "" {
		t.Fatalf("bots result error = %q, err = %v", botsResult.Error, err)
	}
	if botsResult.Result.(map[string]interface{})["count"] != 1 {
		t.Fatalf("bots payload = %#v", botsResult.Result)
	}

	botResult, err := NewGetBotStatusTool().Execute(context.Background(), map[string]interface{}{"bot_id": "bot-1"})
	if err != nil || botResult.Error != "" {
		t.Fatalf("bot result error = %q, err = %v", botResult.Error, err)
	}
	botPayload := botResult.Result.(map[string]interface{})["bot"].(map[string]interface{})
	if botPayload["leverage"] != float64(5) || botPayload["price_interval"] != float64(10) {
		t.Fatalf("bot payload = %#v", botPayload)
	}
}

func TestDataToolsErrorPaths(t *testing.T) {
	defer SetDataProvider(nil)

	SetDataProvider(nil)
	for name, call := range map[string]func() string{
		"orders": func() string {
			res, _ := NewGetOrdersTool().Execute(context.Background(), nil)
			return res.Error
		},
		"positions": func() string {
			res, _ := NewGetPositionsTool().Execute(context.Background(), nil)
			return res.Error
		},
		"bot_status": func() string {
			res, _ := NewGetBotStatusTool().Execute(context.Background(), nil)
			return res.Error
		},
	} {
		if got := call(); got == "" {
			t.Fatalf("%s expected nil provider error", name)
		}
	}

	provider := &fakeDataProvider{ordersErr: errors.New("orders down"), positionsErr: errors.New("positions down"), botsErr: errors.New("bots down")}
	SetDataProvider(provider)
	if res, _ := NewGetOrdersTool().Execute(context.Background(), nil); res.Error == "" {
		t.Fatal("expected orders provider error")
	}
	if res, _ := NewGetPositionsTool().Execute(context.Background(), nil); res.Error == "" {
		t.Fatal("expected positions provider error")
	}
	if res, _ := NewGetBotStatusTool().Execute(context.Background(), nil); res.Error == "" {
		t.Fatal("expected bots provider error")
	}

	provider.botsErr = nil
	provider.botErr = errors.New("bot down")
	if res, _ := NewGetBotStatusTool().Execute(context.Background(), map[string]interface{}{"bot_id": "missing"}); res.Error == "" {
		t.Fatal("expected bot provider error")
	}
	provider.botErr = nil
	provider.bot = nil
	if res, _ := NewGetBotStatusTool().Execute(context.Background(), map[string]interface{}{"bot_id": "missing"}); res.Error == "" {
		t.Fatal("expected missing bot error")
	}

	if res, _ := NewGetMarketTickerTool().Execute(context.Background(), map[string]interface{}{}); res.Error == "" {
		t.Fatal("expected missing ticker parameter error")
	}
	if res, _ := NewGetMarketTickerTool().Execute(context.Background(), map[string]interface{}{"exchange": "unknown", "symbol": "BTCUSDT"}); res.Error == "" {
		t.Fatal("expected unsupported exchange error")
	}
}

func TestParseTickerResponseForSupportedExchanges(t *testing.T) {
	cases := []struct {
		name      string
		exchange  string
		market    string
		body      string
		wantMark  float64
		wantLast  float64
		wantHigh  float64
		wantLow   float64
		wantError bool
	}{
		{
			name: "binance futures mark price", exchange: "binance", market: "futures",
			body:     `{"lastPrice":"101","highPrice":"120","lowPrice":"90","markPrice":"100.5"}`,
			wantMark: 100.5, wantLast: 101, wantHigh: 120, wantLow: 90,
		},
		{
			name: "binance spot fallback mark", exchange: "binance", market: "spot",
			body:     `{"lastPrice":"101","highPrice":"120","lowPrice":"90"}`,
			wantMark: 101, wantLast: 101, wantHigh: 120, wantLow: 90,
		},
		{
			name: "okx", exchange: "okx", market: "futures",
			body:     `{"data":[{"last":"201","high24h":"240","low24h":"180"}]}`,
			wantMark: 201, wantLast: 201, wantHigh: 240, wantLow: 180,
		},
		{
			name: "bybit with mark", exchange: "bybit", market: "futures",
			body:     `{"result":{"list":[{"lastPrice":"301","highPrice24h":"330","lowPrice24h":"290","markPrice":"300.5"}]}}`,
			wantMark: 300.5, wantLast: 301, wantHigh: 330, wantLow: 290,
		},
		{
			name: "bybit fallback mark", exchange: "bybit", market: "spot",
			body:     `{"result":{"list":[{"lastPrice":"301","highPrice24h":"330","lowPrice24h":"290"}]}}`,
			wantMark: 301, wantLast: 301, wantHigh: 330, wantLow: 290,
		},
		{
			name: "bitget with mark", exchange: "bitget", market: "futures",
			body:     `{"data":[{"lastPr":"401","high24h":"440","low24h":"380","markPrice":"400.5"}]}`,
			wantMark: 400.5, wantLast: 401, wantHigh: 440, wantLow: 380,
		},
		{
			name: "bitget fallback mark", exchange: "bitget", market: "spot",
			body:     `{"data":[{"lastPr":"401","high24h":"440","low24h":"380"}]}`,
			wantMark: 401, wantLast: 401, wantHigh: 440, wantLow: 380,
		},
		{name: "okx empty", exchange: "okx", body: `{"data":[]}`, wantError: true},
		{name: "bybit empty", exchange: "bybit", body: `{"result":{"list":[]}}`, wantError: true},
		{name: "bitget invalid", exchange: "bitget", body: `{`, wantError: true},
		{name: "unsupported", exchange: "coinbase", body: `{}`, wantError: true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ticker, err := parseTickerResponse(tc.exchange, tc.market, []byte(tc.body))
			if tc.wantError {
				if err == nil {
					t.Fatal("expected parse error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseTickerResponse returned error: %v", err)
			}
			if ticker.MarkPrice != tc.wantMark || ticker.LastPrice != tc.wantLast || ticker.High24h != tc.wantHigh || ticker.Low24h != tc.wantLow {
				t.Fatalf("ticker = %#v", ticker)
			}
		})
	}
}

func TestConvertToOKXInstID(t *testing.T) {
	if got := convertToOKXInstID("BTCUSDT"); got != "BTC-USDT-SWAP" {
		t.Fatalf("convertToOKXInstID BTCUSDT = %q", got)
	}
	if got := convertToOKXInstID("ABC"); got != "ABC" {
		t.Fatalf("convertToOKXInstID short symbol = %q", got)
	}
}
