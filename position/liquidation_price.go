package position

import (
	"context"
	"time"

	"quantmesh/logger"
)

const (
	// liquidationFallbackSlippage 全平倉無盤口數據時相對現價的讓價比例，同時作為盤口價的最大讓價邊界
	liquidationFallbackSlippage = 0.01
	// liquidationOrderBookDepth 全平倉查詢盤口的檔位數
	liquidationOrderBookDepth = 20
	// liquidationOrderBookTimeout 全平倉查詢盤口超時
	liquidationOrderBookTimeout = 3 * time.Second
)

// liquidationBookPrices 全平倉用的盤口可成交價（0 表示無可用數據）
type liquidationBookPrices struct {
	sell float64 // SELL 平多：吃買盤，覆蓋 sellQty 所需的最差買價（數量不超過買一時即買一價）
	buy  float64 // BUY 平空：吃賣盤，覆蓋 buyQty 所需的最差賣價（數量不超過賣一時即賣一價）
	// bestBid/bestAsk 買一/賣一（0 表示無數據），用於保證穿價
	bestBid float64
	bestAsk float64
}

// firstLevelPrice 盤口第一個有效檔位價格；無數據返回 0
func firstLevelPrice(levels []OrderBookLevel) float64 {
	for _, lv := range levels {
		if lv.Price > 0 {
			return lv.Price
		}
	}
	return 0
}

// marketableLiquidationPrice 保證全平倉限價穿價：SELL 不高於買一、BUY 不低於賣一。
// 現價±1% 的讓價上限在盤口大幅偏離時可能不穿價，此時以買一/賣一為準；無盤口數據時原樣返回。
func marketableLiquidationPrice(side string, px float64, book liquidationBookPrices) float64 {
	if side == "BUY" {
		if book.bestAsk > 0 && (px <= 0 || px < book.bestAsk) {
			return book.bestAsk
		}
		return px
	}
	if book.bestBid > 0 && (px <= 0 || px > book.bestBid) {
		return book.bestBid
	}
	return px
}

// sweepPrice 沿盤口累計數量，返回覆蓋 qty 所需的最深一檔價格；深度不足或無數據返回 0
func sweepPrice(levels []OrderBookLevel, qty float64) float64 {
	if qty <= 0 || len(levels) == 0 {
		return 0
	}
	cum := 0.0
	for _, lv := range levels {
		if lv.Price <= 0 {
			continue
		}
		cum += lv.Quantity
		if cum >= qty-allocationFillEpsilon {
			return lv.Price
		}
	}
	return 0
}

// fetchLiquidationBookPrices 查詢盤口計算全平倉價格。調用方不得持有槽位鎖（含網絡請求）。
func (spm *SuperPositionManager) fetchLiquidationBookPrices(sellQty, buyQty float64) liquidationBookPrices {
	return spm.fetchLiquidationBookPricesContext(context.Background(), sellQty, buyQty)
}

func (spm *SuperPositionManager) fetchLiquidationBookPricesContext(parent context.Context, sellQty, buyQty float64) liquidationBookPrices {
	var out liquidationBookPrices
	if spm.exchange == nil || (sellQty <= 0 && buyQty <= 0) {
		return out
	}
	ctx, cancel := context.WithTimeout(parent, liquidationOrderBookTimeout)
	defer cancel()
	book, err := spm.exchange.GetOrderBook(ctx, spm.config.Trading.Symbol, liquidationOrderBookDepth)
	if err != nil {
		logger.Warn("⚠️ [%s] [全平倉] 獲取盤口失敗，回退為現價±%.0f%%: %v", spm.logPrefix(), liquidationFallbackSlippage*100, err)
		return out
	}
	if book == nil {
		logger.Info("ℹ️ [%s] [全平倉] 交易所未提供盤口數據，使用現價±%.0f%%", spm.logPrefix(), liquidationFallbackSlippage*100)
		return out
	}
	out.sell = sweepPrice(book.Bids, sellQty)
	out.buy = sweepPrice(book.Asks, buyQty)
	out.bestBid = firstLevelPrice(book.Bids)
	out.bestAsk = firstLevelPrice(book.Asks)
	return out
}

// liquidationLimitPrice 全平倉限價：優先盤口可成交價，但讓價不超過現價±liquidationFallbackSlippage；
// 無盤口數據時使用 現價±liquidationFallbackSlippage。
func liquidationLimitPrice(side string, lastPrice float64, book liquidationBookPrices) float64 {
	if side == "BUY" {
		bound := lastPrice * (1 + liquidationFallbackSlippage)
		if book.buy > 0 && (lastPrice <= 0 || book.buy < bound) {
			return book.buy
		}
		return bound
	}
	bound := lastPrice * (1 - liquidationFallbackSlippage)
	if book.sell > 0 && (lastPrice <= 0 || book.sell > bound) {
		return book.sell
	}
	return bound
}
