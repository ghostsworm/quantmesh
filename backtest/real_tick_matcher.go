package backtest

import (
	"fmt"
	"sort"
	"time"

	"quantmesh/logger"
)

// RealTickMatcher 真實 tick 數據撮合引擎
// 使用 Binance aggTrades 真實成交數據進行訂單撮合
type RealTickMatcher struct {
	// 配置參數
	SlippageBps float64 // 滑點（基點，100 = 1%）
	LatencyMs   int64   // 模擬延遲（毫秒）

	// 運行時狀態
	tickIndex    int
	aggTrades    []AggTradeRow
	tradeIDCounter int
	onTradeCallback func(*TickTrade)
}

// NewRealTickMatcher 創建真實 tick 撮合引擎
func NewRealTickMatcher(slippageBps float64, latencyMs int64) *RealTickMatcher {
	if slippageBps < 0 {
		slippageBps = 1 // 預設 1 基點 (0.01%)
	}
	if latencyMs < 0 {
		latencyMs = 10 // 預設 10 毫秒延遲
	}

	return &RealTickMatcher{
		SlippageBps: slippageBps,
		LatencyMs:   latencyMs,
	}
}

// LoadAggTrades 加載聚合交易數據
func (m *RealTickMatcher) LoadAggTrades(trades []AggTradeRow) {
	m.aggTrades = trades
	m.tickIndex = 0

	logger.Info("Loaded %d aggTrades for real tick matching", len(trades))
	if len(trades) > 0 {
		startTime := time.UnixMilli(trades[0].Timestamp).UTC()
		endTime := time.UnixMilli(trades[len(trades)-1].Timestamp).UTC()
		logger.Info("Trade time range: %s ~ %s", startTime.Format(time.RFC3339), endTime.Format(time.RFC3339))
	}
}

// SetTradeCallback 設置成交回調函數
func (m *RealTickMatcher) SetTradeCallback(callback func(*TickTrade)) {
	m.onTradeCallback = callback
}

// ProcessOrders 處理掛單列表，返回成交列表
// 使用真實 tick 數據進行撮合，模擬真實市場環境
func (m *RealTickMatcher) ProcessOrders(orders []TickOrder) []TickTrade {
	if len(m.aggTrades) == 0 {
		logger.Warn("No aggTrades loaded for matching")
		return nil
	}

	var trades []TickTrade
	filledOrderIDs := make(map[string]bool)

	// 按價格和時間排序訂單
	sort.Slice(orders, func(i, j int) bool {
		if orders[i].Price != orders[j].Price {
			if orders[i].Side == "buy" {
				return orders[i].Price > orders[j].Price // 買單：高價優先
			}
			return orders[i].Price < orders[j].Price // 賣單：低價優先
		}
		return orders[i].OrderID < orders[j].OrderID
	})

	// 遍歷真實成交 tick
	for ; m.tickIndex < len(m.aggTrades); m.tickIndex++ {
		aggTrade := m.aggTrades[m.tickIndex]
		allOrdersFilled := true

		// 對每個訂單進行撮合
		for _, order := range orders {
			if filledOrderIDs[order.OrderID] {
				continue
			}

			allOrdersFilled = false

			// 檢查是否觸發條件
			if m.shouldMatchOrder(order, aggTrade) {
				// 計算成交價格（含滑點）
				fillPrice := m.calculateFillPrice(order, aggTrade)
				slippage := 0.0
				if order.Side == "buy" {
					slippage = fillPrice - order.Price
				} else {
					slippage = order.Price - fillPrice
				}

				// 創建成交記錄
				m.tradeIDCounter++
				trade := &TickTrade{
					TradeID:    fmt.Sprintf("RT%d", m.tradeIDCounter),
					OrderID:    order.OrderID,
					Side:       order.Side,
					Price:      fillPrice,
					Size:       order.Size,
					Strategy:   order.Strategy,
					StrategyID: order.StrategyID,
					AccountID:  order.AccountID,
					Timestamp:  aggTrade.Timestamp,
					GridLevel:  order.GridLevel,
					Slippage:   slippage * order.Size,
				}

				trades = append(trades, *trade)
				filledOrderIDs[order.OrderID] = true

				// 調用回調
				if m.onTradeCallback != nil {
					m.onTradeCallback(trade)
				}
			}
		}

		// 如果所有訂單都已成交，停止處理
		if allOrdersFilled && len(orders) > 0 {
			break
		}
	}

	return trades
}

// ShouldMatchOrder 判斷訂單是否應該成交（導出供 TickBacktester 使用）
func ShouldMatchOrder(order TickOrder, aggTrade AggTradeRow) bool {
	// 買單：市場價 <= 訂單價時成交
	if order.Side == "buy" {
		return aggTrade.Price <= order.Price
	}

	// 賣單：市場價 >= 訂單價時成交
	return aggTrade.Price >= order.Price
}

// CalculateFillPrice 計算成交價格（含滑點）（導出供 TickBacktester 使用）
func CalculateFillPrice(order TickOrder, aggTrade AggTradeRow, slippageBps float64) float64 {
	// 使用真實成交價格作為基準
	fillPrice := aggTrade.Price

	// 添加滑點
	slippageMultiplier := 1.0 + slippageBps/10000.0
	if order.Side == "buy" {
		// 買單滑點向上（買得更貴）
		fillPrice = fillPrice * slippageMultiplier
	} else {
		// 賣單滑點向下（賣得更便宜）
		fillPrice = fillPrice * (2.0 - slippageMultiplier)
	}

	return fillPrice
}

// shouldMatchOrder 判斷訂單是否應該成交
func (m *RealTickMatcher) shouldMatchOrder(order TickOrder, aggTrade AggTradeRow) bool {
	return ShouldMatchOrder(order, aggTrade)
}

// calculateFillPrice 計算成交價格（含滑點）
func (m *RealTickMatcher) calculateFillPrice(order TickOrder, aggTrade AggTradeRow) float64 {
	return CalculateFillPrice(order, aggTrade, m.SlippageBps)
}

// ProcessOrdersWithWindow 在指定時間窗口內處理訂單
func (m *RealTickMatcher) ProcessOrdersWithWindow(orders []TickOrder, startMs, endMs int64) []TickTrade {
	// 找到起始位置
	startIdx := sort.Search(len(m.aggTrades), func(i int) bool {
		return m.aggTrades[i].Timestamp >= startMs
	})

	if startIdx >= len(m.aggTrades) {
		return nil
	}

	// 找到結束位置
	endIdx := sort.Search(len(m.aggTrades), func(i int) bool {
		return m.aggTrades[i].Timestamp > endMs
	})

	// 只處理窗口內的 tick
	windowTrades := m.aggTrades[startIdx:endIdx]
	if len(windowTrades) == 0 {
		return nil
	}

	var trades []TickTrade
	filledOrderIDs := make(map[string]bool)

	for _, aggTrade := range windowTrades {
		allOrdersFilled := true

		for _, order := range orders {
			if filledOrderIDs[order.OrderID] {
				continue
			}

			allOrdersFilled = false

			if m.shouldMatchOrder(order, aggTrade) {
				fillPrice := m.calculateFillPrice(order, aggTrade)
				slippage := 0.0
				if order.Side == "buy" {
					slippage = fillPrice - order.Price
				} else {
					slippage = order.Price - fillPrice
				}

				m.tradeIDCounter++
				trade := &TickTrade{
					TradeID:    fmt.Sprintf("RT%d", m.tradeIDCounter),
					OrderID:    order.OrderID,
					Side:       order.Side,
					Price:      fillPrice,
					Size:       order.Size,
					Strategy:   order.Strategy,
					StrategyID: order.StrategyID,
					AccountID:  order.AccountID,
					Timestamp:  aggTrade.Timestamp,
					GridLevel:  order.GridLevel,
					Slippage:   slippage * order.Size,
				}

				trades = append(trades, *trade)
				filledOrderIDs[order.OrderID] = true

				if m.onTradeCallback != nil {
					m.onTradeCallback(trade)
				}
			}
		}

		if allOrdersFilled && len(orders) > 0 {
			break
		}
	}

	return trades
}

// GetCurrentTick 獲取當前 tick 位置
func (m *RealTickMatcher) GetCurrentTick() *AggTradeRow {
	if m.tickIndex < len(m.aggTrades) {
		return &m.aggTrades[m.tickIndex]
	}
	return nil
}

// GetProgress 獲取當前處理進度
func (m *RealTickMatcher) GetProgress() (current, total int) {
	return m.tickIndex, len(m.aggTrades)
}

// GetProgressPercent 獲取當前處理進度百分比
func (m *RealTickMatcher) GetProgressPercent() float64 {
	if len(m.aggTrades) == 0 {
		return 100.0
	}
	return float64(m.tickIndex) / float64(len(m.aggTrades)) * 100.0
}

// Reset 重置撮合引擎狀態
func (m *RealTickMatcher) Reset() {
	m.tickIndex = 0
	m.tradeIDCounter = 0
}

// Clone 克隆撮合引擎
func (m *RealTickMatcher) Clone() *RealTickMatcher {
	return &RealTickMatcher{
		SlippageBps:     m.SlippageBps,
		LatencyMs:       m.LatencyMs,
		tickIndex:       0,
		aggTrades:       m.aggTrades,
		tradeIDCounter:  0,
	}
}

// GetMarketSnapshot 獲取市場快照（最近 N 個 tick）
type MarketSnapshot struct {
	LastPrice      float64
	BestBidPrice   float64
	BestAskPrice   float64
	Volume24h      float64
	TradeCount     int
	PriceChange24h float64
}

// GetMarketSnapshot 獲取當前市場快照
func (m *RealTickMatcher) GetMarketSnapshot(lastNTicks int) *MarketSnapshot {
	if len(m.aggTrades) == 0 {
		return nil
	}

	// 計算最近 N 個 tick 的統計信息
	startIdx := m.tickIndex - lastNTicks
	if startIdx < 0 {
		startIdx = 0
	}

	recentTrades := m.aggTrades[startIdx:m.tickIndex]
	if len(recentTrades) == 0 {
		return nil
	}

	snapshot := &MarketSnapshot{
		LastPrice:  recentTrades[len(recentTrades)-1].Price,
		TradeCount: len(recentTrades),
	}

	// 計算最優買賣價
	for _, trade := range recentTrades {
		if trade.IsBuyerMaker {
			// 主動賣，這個價格是賣價
			if snapshot.BestAskPrice == 0 || trade.Price < snapshot.BestAskPrice {
				snapshot.BestAskPrice = trade.Price
			}
		} else {
			// 主動買，這個價格是買價
			if snapshot.BestBidPrice == 0 || trade.Price > snapshot.BestBidPrice {
				snapshot.BestBidPrice = trade.Price
			}
		}

		snapshot.Volume24h += trade.Quantity
	}

	return snapshot
}

// EstimateSlippage 估算滑點損失
func (m *RealTickMatcher) EstimateSlippage(trades []TickTrade) float64 {
	totalSlippage := 0.0
	for _, trade := range trades {
		totalSlippage += trade.Slippage
	}
	return totalSlippage
}

// GetStatistics 獲取撮合統計信息
type RealMatcherStatistics struct {
	TotalTicks      int
	ProcessedTicks  int
	TotalTrades     int
	TotalSlippage   float64
	AverageSlippage float64
	ProgressPercent float64
}

// GetStats 獲取當前統計信息
func (m *RealTickMatcher) GetStats() *RealMatcherStatistics {
	stats := &RealMatcherStatistics{
		TotalTicks:     len(m.aggTrades),
		ProcessedTicks: m.tickIndex,
		ProgressPercent: m.GetProgressPercent(),
	}

	if stats.TotalTicks > 0 {
		// 可以在這裡添加更多統計信息
	}

	return stats
}
