package storage

import (
	"database/sql"
	"fmt"
	"time"

	"quantmesh/logger"
	"quantmesh/utils"
)

// ========== Funding Rate / Funding Payment 存儲 ==========

// SaveFundingRate 保存资金费率（僅在变动時存儲）
func (s *SQLStorage) SaveFundingRate(symbol, exchange string, rate float64, timestamp time.Time) error {
	// 獲取該交易對的最新资金费率
	latestRate, err := s.GetLatestFundingRate(symbol, exchange)
	if err == nil {
		// 比较新舊费率（考虑浮点精度误差）
		const epsilon = 0.0000001
		if abs(latestRate-rate) < epsilon {
			// 费率未变化，不存儲
			return nil
		}
	}

	// 费率有变化，插入新記錄
	_, err = s.db.Exec(`
		INSERT INTO funding_rates (symbol, exchange, rate, timestamp, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, symbol, exchange, rate, timestamp, time.Now())
	return err
}

// GetLatestFundingRate 獲取最新的资金费率
func (s *SQLStorage) GetLatestFundingRate(symbol, exchange string) (float64, error) {
	var rate float64
	err := s.db.QueryRow(`
		SELECT rate FROM funding_rates
		WHERE symbol = ? AND exchange = ?
		ORDER BY timestamp DESC
		LIMIT 1
	`, symbol, exchange).Scan(&rate)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("未找到资金费率記錄")
	}
	return rate, err
}

// GetFundingRateHistory 獲取资金费率历史
func (s *SQLStorage) GetFundingRateHistory(symbol, exchange string, limit int) ([]*FundingRate, error) {
	// 限制最大返回數量，防止記憶體占用過大
	maxLimit := 10000 // 最多返回1万条资金费率記錄
	if limit <= 0 {
		limit = 100 // 預設 100条
	}
	if limit > maxLimit {
		limit = maxLimit
		logger.Warn("⚠️ 资金费率历史查詢 limit 超過限制 (%d)，已限制為 %d", limit, maxLimit)
	}

	query := `
		SELECT id, symbol, exchange, rate, timestamp, created_at
		FROM funding_rates
		WHERE 1=1
	`
	args := []interface{}{}

	if symbol != "" {
		query += " AND symbol = ?"
		args = append(args, symbol)
	}
	if exchange != "" {
		query += " AND exchange = ?"
		args = append(args, exchange)
	}

	query += " ORDER BY timestamp DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rates []*FundingRate
	for rows.Next() {
		var fr FundingRate
		err := rows.Scan(&fr.ID, &fr.Symbol, &fr.Exchange, &fr.Rate, &fr.Timestamp, &fr.CreatedAt)
		if err != nil {
			return nil, err
		}
		rates = append(rates, &fr)
	}

	return rates, rows.Err()
}

// SaveFundingPayment 保存資金費用記錄
func (s *SQLStorage) SaveFundingPayment(payment *FundingPayment) error {
	tradeTime := utils.ToUTC(payment.TradeTime)
	_, err := s.db.Exec(`
		INSERT INTO funding_payments (exchange, symbol, account, income_type, income, asset, info, transaction_id, trade_time, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, payment.Exchange, payment.Symbol, payment.Account, payment.IncomeType, payment.Income, payment.Asset, payment.Info, payment.TransactionID, tradeTime, time.Now().UTC())
	return err
}

// GetFundingPayments 獲取資金費用記錄（按時間區間）
func (s *SQLStorage) GetFundingPayments(account, exchange string, startTime, endTime time.Time) ([]*FundingPayment, error) {
	startUTC := utils.ToUTC(startTime)
	endUTC := utils.ToUTC(endTime)
	query := `
		SELECT id, exchange, symbol, account, income_type, income, asset, info, transaction_id, trade_time, created_at
		FROM funding_payments
		WHERE trade_time >= ? AND trade_time <= ?
	`
	args := []interface{}{startUTC, endUTC}
	if exchange != "" {
		query += " AND exchange = ?"
		args = append(args, exchange)
	}
	if account != "" {
		query += " AND (account = ? OR account IS NULL OR account = '')"
		args = append(args, account)
	}
	query += " ORDER BY trade_time DESC LIMIT 10000"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []*FundingPayment
	for rows.Next() {
		var p FundingPayment
		var tradeTime, createdAt time.Time
		err := rows.Scan(&p.ID, &p.Exchange, &p.Symbol, &p.Account, &p.IncomeType, &p.Income, &p.Asset, &p.Info, &p.TransactionID, &tradeTime, &createdAt)
		if err != nil {
			return nil, err
		}
		p.TradeTime = tradeTime
		p.CreatedAt = createdAt
		list = append(list, &p)
	}
	return list, rows.Err()
}

// GetFundingPaymentsSum 獲取資金費用淨額（收入 - 支出，正數表示淨收入）
func (s *SQLStorage) GetFundingPaymentsSum(account, exchange string, startTime, endTime time.Time) (float64, error) {
	startUTC := utils.ToUTC(startTime)
	endUTC := utils.ToUTC(endTime)
	query := `
		SELECT COALESCE(SUM(income), 0) FROM funding_payments
		WHERE trade_time >= ? AND trade_time <= ?
	`
	args := []interface{}{startUTC, endUTC}
	if exchange != "" {
		query += " AND exchange = ?"
		args = append(args, exchange)
	}
	if account != "" {
		query += " AND (account = ? OR account IS NULL OR account = '')"
		args = append(args, account)
	}

	var sum sql.NullFloat64
	err := s.db.QueryRow(query, args...).Scan(&sum)
	if err != nil {
		return 0, err
	}
	if sum.Valid {
		return sum.Float64, nil
	}
	return 0, nil
}

// GetDailyFundingPayments 獲取每日資金費用（按日期分組）
func (s *SQLStorage) GetDailyFundingPayments(account, exchange string, startTime, endTime time.Time) (map[string]float64, error) {
	startUTC := utils.ToUTC(startTime)
	endUTC := utils.ToUTC(endTime)

	// 獲取配置時區的偏移秒數
	tzOffsetSeconds := utils.GetTimezoneOffsetSeconds()
	tzModifier := fmt.Sprintf("%+d seconds", tzOffsetSeconds)

	query := fmt.Sprintf(`
		SELECT date(datetime(trade_time, '%s')) as date, COALESCE(SUM(income), 0) as daily_funding
		FROM funding_payments
		WHERE trade_time >= ? AND trade_time <= ?
	`, tzModifier)
	args := []interface{}{startUTC, endUTC}
	if exchange != "" {
		query += " AND exchange = ?"
		args = append(args, exchange)
	}
	if account != "" {
		query += " AND (account = ? OR account IS NULL OR account = '')"
		args = append(args, account)
	}
	query += fmt.Sprintf(" GROUP BY date(datetime(trade_time, '%s'))", tzModifier)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]float64)
	for rows.Next() {
		var dateStr string
		var dailyFunding float64
		if err := rows.Scan(&dateStr, &dailyFunding); err != nil {
			continue
		}
		result[dateStr] = dailyFunding
	}
	return result, rows.Err()
}

// abs 计算绝對值（用於浮点數比较）
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
