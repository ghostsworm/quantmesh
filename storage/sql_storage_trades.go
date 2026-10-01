package storage

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"quantmesh/logger"
	"quantmesh/utils"
)

// SavePosition 保存持倉
func (s *SQLStorage) SavePosition(position *Position) error {
	// 轉换為UTC時间存儲
	openedAt := utils.ToUTC(position.OpenedAt)
	var closedAt interface{}
	if position.ClosedAt != nil {
		closedAtUTC := utils.ToUTC(*position.ClosedAt)
		closedAt = closedAtUTC
	}

	_, err := s.db.Exec(`
		INSERT INTO positions
		(slot_price, symbol, size, entry_price, current_price, pnl, opened_at, closed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, position.SlotPrice, position.Symbol, position.Size,
		position.EntryPrice, position.CurrentPrice, position.PnL,
		openedAt, closedAt)
	return err
}

// SaveTrade 保存交易
func (s *SQLStorage) SaveTrade(trade *Trade) error {
	// 轉换為UTC時间存儲
	createdAt := utils.ToUTC(trade.CreatedAt)
	// 确保 exchange 不為空，默认為 binance（兼容舊數據）
	exchange := trade.Exchange
	if exchange == "" {
		exchange = "binance"
	}
	botID := strings.TrimSpace(trade.BotID)
	_, err := s.db.Exec(fmt.Sprintf(`
		INSERT INTO %s
		(execution_key, buy_order_id, sell_order_id, bot_id, exchange, market_type, pnl_asset, account_scope, account, symbol, buy_price, sell_price, quantity, pnl, exchange_pnl, fee, fee_asset, buy_price_deviation, sell_price_deviation, created_at)
		VALUES (NULLIF(?, ''), ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, s.tradesTbl()), trade.ExecutionKey, trade.BuyOrderID, trade.SellOrderID, botID, exchange, strings.ToLower(strings.TrimSpace(trade.MarketType)), strings.ToUpper(strings.TrimSpace(trade.PnLAsset)), trade.AccountScope, trade.Account, trade.Symbol,
		trade.BuyPrice, trade.SellPrice, trade.Quantity, trade.PnL, trade.ExchangePnL, trade.Fee, trade.FeeAsset,
		trade.BuyPriceDeviation, trade.SellPriceDeviation, createdAt)
	if err != nil {
		return err
	}
	// 合规审计：記錄成交事件
	if globalAuditLogger != nil {
		globalAuditLogger.LogTrade(trade)
	}
	return nil
}

// SaveTradeIdempotent retries safely when the database committed a row but the
// acknowledgement was lost. A reused key with different economics is rejected.
func (s *SQLStorage) SaveTradeIdempotent(trade *Trade) error {
	if trade == nil || strings.TrimSpace(trade.ExecutionKey) == "" {
		return fmt.Errorf("idempotent trade write requires an execution key")
	}
	if err := validateTradeEconomics(trade); err != nil {
		return err
	}
	canonical := *trade
	canonical.ExecutionKey = strings.TrimSpace(canonical.ExecutionKey)
	canonical.BotID = strings.TrimSpace(canonical.BotID)
	canonical.MarketType = strings.ToLower(strings.TrimSpace(canonical.MarketType))
	if strings.TrimSpace(canonical.Exchange) == "" {
		canonical.Exchange = "binance"
	}
	trade = &canonical
	if err := s.SaveTrade(trade); err == nil {
		return nil
	} else {
		var existing Trade
		query := fmt.Sprintf(`SELECT buy_order_id, sell_order_id, bot_id, exchange, market_type, pnl_asset, account_scope, account, symbol, buy_price, sell_price, quantity, pnl, exchange_pnl, fee, fee_asset, buy_price_deviation, sell_price_deviation FROM %s WHERE execution_key = ?`, s.tradesTbl())
		if readErr := s.db.QueryRow(query, trade.ExecutionKey).Scan(
			&existing.BuyOrderID, &existing.SellOrderID, &existing.BotID, &existing.Exchange, &existing.MarketType, &existing.PnLAsset, &existing.AccountScope, &existing.Account, &existing.Symbol,
			&existing.BuyPrice, &existing.SellPrice, &existing.Quantity, &existing.PnL, &existing.ExchangePnL, &existing.Fee,
			&existing.FeeAsset, &existing.BuyPriceDeviation, &existing.SellPriceDeviation,
		); readErr != nil {
			return err
		}
		if !sameTradeEconomics(existing, *trade) {
			return fmt.Errorf("execution key %q already exists with different trade economics: %w", trade.ExecutionKey, err)
		}
		return nil
	}
}

func validateTradeEconomics(trade *Trade) error {
	values := []struct {
		name  string
		value float64
	}{
		{"buy price", trade.BuyPrice}, {"sell price", trade.SellPrice}, {"quantity", trade.Quantity},
		{"PnL", trade.PnL}, {"exchange PnL", trade.ExchangePnL}, {"fee", trade.Fee},
		{"buy price deviation", trade.BuyPriceDeviation}, {"sell price deviation", trade.SellPriceDeviation},
	}
	for _, item := range values {
		if math.IsNaN(item.value) || math.IsInf(item.value, 0) {
			return fmt.Errorf("idempotent trade write has non-finite %s", item.name)
		}
	}
	if trade.BuyPrice < 0 || trade.SellPrice < 0 || trade.Quantity < 0 {
		return fmt.Errorf("idempotent trade write has negative price or quantity")
	}
	return nil
}

func sameTradeEconomics(a, b Trade) bool {
	close := func(x, y float64) bool { return math.Abs(x-y) <= 0.00000001 }
	return a.BuyOrderID == b.BuyOrderID && a.SellOrderID == b.SellOrderID && strings.TrimSpace(a.BotID) == strings.TrimSpace(b.BotID) && a.AccountScope == b.AccountScope && a.Account == b.Account &&
		strings.EqualFold(a.Exchange, b.Exchange) && strings.EqualFold(a.MarketType, b.MarketType) && strings.EqualFold(a.PnLAsset, b.PnLAsset) && a.Symbol == b.Symbol && a.FeeAsset == b.FeeAsset &&
		close(a.BuyPrice, b.BuyPrice) && close(a.SellPrice, b.SellPrice) && close(a.Quantity, b.Quantity) && close(a.PnL, b.PnL) &&
		close(a.ExchangePnL, b.ExchangePnL) && close(a.Fee, b.Fee) && close(a.BuyPriceDeviation, b.BuyPriceDeviation) && close(a.SellPriceDeviation, b.SellPriceDeviation)
}

// SaveTradeWithDeviation 保存交易記錄（包含價格偏差）
func (s *SQLStorage) SaveTradeWithDeviation(buyOrderID, sellOrderID int64, exchange, symbol string, buyPrice, sellPrice, quantity, pnl, fee float64, feeAsset string, buyPriceDeviation, sellPriceDeviation float64, createdAt time.Time, botID string) error {
	trade := &Trade{
		BuyOrderID:         buyOrderID,
		SellOrderID:        sellOrderID,
		BotID:              strings.TrimSpace(botID),
		Exchange:           exchange,
		Symbol:             symbol,
		BuyPrice:           buyPrice,
		SellPrice:          sellPrice,
		Quantity:           quantity,
		PnL:                pnl,
		Fee:                fee,
		FeeAsset:           feeAsset,
		BuyPriceDeviation:  buyPriceDeviation,
		SellPriceDeviation: sellPriceDeviation,
		CreatedAt:          createdAt,
	}
	return s.SaveTrade(trade)
}

// SaveTradeWithExchangePnL 保存交易記錄（包含交易所盈虧和價格偏差）
func (s *SQLStorage) SaveTradeWithExchangePnL(buyOrderID, sellOrderID int64, exchange, symbol string, buyPrice, sellPrice, quantity, pnl, exchangePnL, fee float64, feeAsset string, buyPriceDeviation, sellPriceDeviation float64, createdAt time.Time, botID string) error {
	trade := &Trade{
		BuyOrderID:         buyOrderID,
		SellOrderID:        sellOrderID,
		BotID:              strings.TrimSpace(botID),
		Exchange:           exchange,
		Symbol:             symbol,
		BuyPrice:           buyPrice,
		SellPrice:          sellPrice,
		Quantity:           quantity,
		PnL:                pnl,
		ExchangePnL:        exchangePnL,
		Fee:                fee,
		FeeAsset:           feeAsset,
		BuyPriceDeviation:  buyPriceDeviation,
		SellPriceDeviation: sellPriceDeviation,
		CreatedAt:          createdAt,
	}
	return s.SaveTrade(trade)
}

// SaveTradeWithExchangePnLAndMarketType persists market identity when the execution owner knows it.
func (s *SQLStorage) SaveTradeWithExchangePnLAndMarketType(buyOrderID, sellOrderID int64, exchange, marketType, symbol string, buyPrice, sellPrice, quantity, pnl, exchangePnL, fee float64, feeAsset string, buyPriceDeviation, sellPriceDeviation float64, createdAt time.Time, botID string) error {
	trade := &Trade{
		BuyOrderID: buyOrderID, SellOrderID: sellOrderID, BotID: strings.TrimSpace(botID),
		Exchange: exchange, MarketType: strings.ToLower(strings.TrimSpace(marketType)), Symbol: symbol,
		BuyPrice: buyPrice, SellPrice: sellPrice, Quantity: quantity, PnL: pnl, ExchangePnL: exchangePnL,
		Fee: fee, FeeAsset: feeAsset, BuyPriceDeviation: buyPriceDeviation,
		SellPriceDeviation: sellPriceDeviation, CreatedAt: createdAt,
	}
	return s.SaveTrade(trade)
}

// QueryPositions 查詢持倉历史
func (s *SQLStorage) QueryPositions(limit, offset int) ([]*Position, error) {
	maxLimit := 10000
	if limit <= 0 {
		limit = 100
	}
	if limit > maxLimit {
		limit = maxLimit
		logger.Warn("⚠️ 持倉查詢 limit 超過限制 (%d)，已限制為 %d", limit, maxLimit)
	}

	rows, err := s.db.Query(`
		SELECT slot_price, symbol, size, entry_price, current_price, pnl, opened_at, closed_at
		FROM positions
		ORDER BY opened_at DESC
		LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("查詢持倉失败: %w", err)
	}
	defer rows.Close()

	var positions []*Position
	for rows.Next() {
		p := &Position{}
		var closedAt interface{}
		err := rows.Scan(
			&p.SlotPrice,
			&p.Symbol,
			&p.Size,
			&p.EntryPrice,
			&p.CurrentPrice,
			&p.PnL,
			&p.OpenedAt,
			&closedAt,
		)
		if err != nil {
			continue
		}
		if closedAt != nil {
			if t, ok := closedAt.(time.Time); ok {
				p.ClosedAt = &t
			}
		}
		positions = append(positions, p)
	}

	return positions, rows.Err()
}

// QueryTrades 查詢交易
func (s *SQLStorage) QueryTrades(startTime, endTime time.Time, limit, offset int) ([]*Trade, error) {
	// 限制最大返回數量，防止記憶體占用過大
	maxLimit := 10000 // 最多返回1万条交易
	if limit <= 0 {
		limit = 100 // 預設 100条
	}
	if limit > maxLimit {
		limit = maxLimit
		logger.Warn("⚠️ 交易查詢 limit 超過限制 (%d)，已限制為 %d", limit, maxLimit)
	}

	rows, err := s.db.Query(fmt.Sprintf(`
		SELECT id, buy_order_id, sell_order_id, exchange, account, symbol, buy_price, sell_price, quantity, pnl, COALESCE(fee, 0) as fee, COALESCE(fee_asset, ''), COALESCE(pnl_asset, ''), created_at
		FROM %s
		WHERE created_at >= ? AND created_at <= ?
		ORDER BY created_at DESC, id DESC
		LIMIT ? OFFSET ?
	`, s.tradesTbl()), startTime, endTime, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("查詢交易失败: %w", err)
	}
	defer rows.Close()

	var trades []*Trade
	for rows.Next() {
		trade := &Trade{}
		err := rows.Scan(
			&trade.ID,
			&trade.BuyOrderID,
			&trade.SellOrderID,
			&trade.Exchange,
			&trade.Account,
			&trade.Symbol,
			&trade.BuyPrice,
			&trade.SellPrice,
			&trade.Quantity,
			&trade.PnL,
			&trade.Fee,
			&trade.FeeAsset,
			&trade.PnLAsset,
			&trade.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("解析成交記錄失败: %w", err)
		}
		// 兼容舊數據：如果 exchange 為空，默认為 binance
		if trade.Exchange == "" {
			trade.Exchange = "binance"
		}
		trades = append(trades, trade)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍歷成交記錄失败: %w", err)
	}

	return trades, nil
}

// QueryTradesByAccountScopeAndAsset returns diagnostic execution rows from one
// exact exchange credential scope and PnL denomination. Callers must first
// validate that the scope has complete ownership and fee-asset evidence.
func (s *SQLStorage) QueryTradesByAccountScopeAndAsset(exchange, accountScope, marketType, asset string, startTime, endTime time.Time, limit int) ([]*Trade, error) {
	exchange = strings.ToLower(strings.TrimSpace(exchange))
	accountScope = strings.TrimSpace(accountScope)
	marketType = strings.ToLower(strings.TrimSpace(marketType))
	asset = strings.ToUpper(strings.TrimSpace(asset))
	if exchange == "" || accountScope == "" || marketType == "" || asset == "" || startTime.IsZero() || endTime.IsZero() || endTime.Before(startTime) {
		return nil, fmt.Errorf("exchange, account_scope, market_type, asset and a valid time range are required")
	}
	if limit <= 0 || limit > 100000 {
		limit = 100000
	}
	var unscoped int64
	if err := s.db.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s
		WHERE (LOWER(TRIM(exchange)) = ? AND TRIM(COALESCE(account_scope, '')) = '') OR TRIM(COALESCE(exchange, '')) = ''`, s.tradesTbl()), exchange).Scan(&unscoped); err != nil {
		return nil, fmt.Errorf("validate diagnostic trade ownership: %w", err)
	}
	if unscoped > 0 {
		return nil, fmt.Errorf("diagnostic trade history is incomplete: exchange %s has %d unattributed trades", exchange, unscoped)
	}
	var unclassified int64
	if err := s.db.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s
		WHERE LOWER(TRIM(exchange)) = ? AND account_scope = ? AND
		(TRIM(COALESCE(pnl_asset, '')) = '' OR
		 (COALESCE(fee, 0) <> 0 AND UPPER(TRIM(COALESCE(fee_asset, ''))) <> UPPER(TRIM(COALESCE(pnl_asset, '')))))`, s.tradesTbl()), exchange, accountScope).Scan(&unclassified); err != nil {
		return nil, fmt.Errorf("validate diagnostic trade denomination: %w", err)
	}
	if unclassified > 0 {
		return nil, fmt.Errorf("diagnostic trade history is incomplete: scope %s has %d trades with unknown or mismatched PnL/fee assets", accountScope, unclassified)
	}
	rows, err := s.db.Query(fmt.Sprintf(`
		SELECT id, buy_order_id, sell_order_id, bot_id, LOWER(TRIM(exchange)), COALESCE(NULLIF(LOWER(TRIM(market_type)), ''), 'unknown'),
			COALESCE(pnl_asset, ''), account_scope, account, symbol, buy_price, sell_price, quantity, pnl,
			COALESCE(exchange_pnl, 0), COALESCE(fee, 0), COALESCE(fee_asset, ''), created_at
		FROM %s
		WHERE LOWER(TRIM(exchange)) = ? AND account_scope = ? AND LOWER(TRIM(market_type)) = ? AND UPPER(TRIM(pnl_asset)) = ? AND created_at >= ? AND created_at <= ?
		ORDER BY created_at DESC, id DESC LIMIT ?`, s.tradesTbl()), exchange, accountScope, marketType, asset, startTime, endTime, limit+1)
	if err != nil {
		return nil, fmt.Errorf("query scoped diagnostic trades: %w", err)
	}
	defer rows.Close()
	trades := make([]*Trade, 0)
	for rows.Next() {
		trade := &Trade{}
		if err := rows.Scan(&trade.ID, &trade.BuyOrderID, &trade.SellOrderID, &trade.BotID, &trade.Exchange, &trade.MarketType,
			&trade.PnLAsset, &trade.AccountScope, &trade.Account, &trade.Symbol, &trade.BuyPrice, &trade.SellPrice, &trade.Quantity,
			&trade.PnL, &trade.ExchangePnL, &trade.Fee, &trade.FeeAsset, &trade.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan scoped diagnostic trade: %w", err)
		}
		trades = append(trades, trade)
		if len(trades) > limit {
			return nil, fmt.Errorf("scoped diagnostic result exceeds limit %d", limit)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate scoped diagnostic trades: %w", err)
	}
	return trades, nil
}

// ScanTradesContext streams trades newest-first without materializing or truncating the result set.
// Returning false from visit stops the scan successfully.
func (s *SQLStorage) ScanTradesContext(ctx context.Context, startTime, endTime time.Time, visit func(*Trade) bool) error {
	if visit == nil {
		return fmt.Errorf("scan trades requires a visitor")
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, exchange, account, symbol, pnl, COALESCE(fee, 0) AS fee, COALESCE(fee_asset, ''), COALESCE(pnl_asset, ''), created_at
		FROM %s
		WHERE created_at >= ? AND created_at <= ?
		ORDER BY created_at DESC, id DESC
	`, s.tradesTbl()), startTime, endTime)
	if err != nil {
		return fmt.Errorf("stream trades (%s ~ %s): %w", startTime.Format(time.RFC3339), endTime.Format(time.RFC3339), err)
	}
	defer rows.Close()

	for rows.Next() {
		trade := &Trade{}
		if err := rows.Scan(&trade.ID, &trade.Exchange, &trade.Account, &trade.Symbol, &trade.PnL, &trade.Fee, &trade.FeeAsset, &trade.PnLAsset, &trade.CreatedAt); err != nil {
			return fmt.Errorf("scan trade row: %w", err)
		}
		if trade.Exchange == "" {
			trade.Exchange = "binance"
		}
		if !visit(trade) {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate trades: %w", err)
	}
	return nil
}

// GetTradesBySellOrderIDs 根據賣單 ID 查詢對應的成交盈虧，返回 sell_order_id -> pnl 的映射
func (s *SQLStorage) GetTradesBySellOrderIDs(sellOrderIDs []int64) (map[int64]float64, error) {
	result := make(map[int64]float64)
	if len(sellOrderIDs) == 0 {
		return result, nil
	}
	// 構建 IN 子句的佔位符
	placeholders := ""
	args := make([]interface{}, 0, len(sellOrderIDs))
	for i, id := range sellOrderIDs {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, id)
	}
	query := fmt.Sprintf(`
		SELECT sell_order_id, pnl FROM %s WHERE sell_order_id IN (%s)
	`, s.tradesTbl(), placeholders)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("查詢賣單盈虧失败: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var sellOrderID int64
		var pnl float64
		if err := rows.Scan(&sellOrderID, &pnl); err != nil {
			// 金額聚合不能跳行：少一筆就是盈亏數字直接算錯
			return nil, fmt.Errorf("解析成交盈亏失败: %w", err)
		}
		result[sellOrderID] += pnl
	}
	return result, rows.Err()
}
