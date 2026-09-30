package storage

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
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
	if payment == nil {
		return fmt.Errorf("funding payment is nil")
	}
	if payment.TransactionID <= 0 || strings.TrimSpace(payment.Exchange) == "" || strings.TrimSpace(payment.Symbol) == "" ||
		strings.TrimSpace(payment.MarketType) == "" || strings.TrimSpace(payment.IncomeType) == "" ||
		strings.TrimSpace(payment.AccountScope) == "" || strings.TrimSpace(payment.Asset) == "" || payment.TradeTime.IsZero() ||
		math.IsNaN(payment.Income) || math.IsInf(payment.Income, 0) {
		return fmt.Errorf("funding payment lacks stable identity, denomination, finite amount, or trade time")
	}
	identityKey := fundingPaymentIdentityKey(payment)
	tradeTime := utils.ToUTC(payment.TradeTime)
	_, err := s.db.Exec(`
		INSERT INTO funding_payments (exchange, symbol, account, market_type, account_scope, income_type, income, asset, info, transaction_id, trade_time, created_at, identity_key)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, payment.Exchange, payment.Symbol, payment.Account, payment.MarketType, payment.AccountScope, payment.IncomeType, payment.Income, payment.Asset, payment.Info, payment.TransactionID, tradeTime, time.Now().UTC(), identityKey)
	if err == nil {
		return nil
	}
	// A concurrent replay may win the unique-key race. Accept it only if its
	// accounting payload is identical; identity reuse with different economics
	// must remain visible to the caller.
	var existing FundingPayment
	var existingTradeTime time.Time
	lookupErr := s.db.QueryRow(`
		SELECT exchange, symbol, account, market_type, account_scope, income_type, income, asset, transaction_id, trade_time
		FROM funding_payments WHERE identity_key = ?
	`, identityKey).Scan(&existing.Exchange, &existing.Symbol, &existing.Account, &existing.MarketType, &existing.AccountScope,
		&existing.IncomeType, &existing.Income, &existing.Asset, &existing.TransactionID, &existingTradeTime)
	if lookupErr == nil {
		existing.TradeTime = existingTradeTime
		if sameFundingPayment(existing, *payment) {
			return nil
		}
		return fmt.Errorf("funding payment identity reused with different accounting data exchange=%s type=%s transaction_id=%d", payment.Exchange, payment.IncomeType, payment.TransactionID)
	}
	return fmt.Errorf("save funding payment exchange=%s type=%s transaction_id=%d: %w (identity lookup: %v)", payment.Exchange, payment.IncomeType, payment.TransactionID, err, lookupErr)
}

func fundingPaymentIdentityKey(payment *FundingPayment) string {
	identity := strings.Join([]string{
		strings.ToLower(strings.TrimSpace(payment.Exchange)),
		strings.TrimSpace(payment.AccountScope),
		strings.TrimSpace(payment.Account),
		strings.ToLower(strings.TrimSpace(payment.MarketType)),
		strings.ToUpper(strings.TrimSpace(payment.IncomeType)),
		strings.ToUpper(strings.TrimSpace(payment.Symbol)),
		strings.ToUpper(strings.TrimSpace(payment.Asset)),
		fmt.Sprint(payment.TransactionID),
	}, "\x00")
	digest := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(digest[:])
}

func sameFundingPayment(a, b FundingPayment) bool {
	return strings.EqualFold(strings.TrimSpace(a.Exchange), strings.TrimSpace(b.Exchange)) &&
		strings.EqualFold(strings.TrimSpace(a.Symbol), strings.TrimSpace(b.Symbol)) && a.Account == b.Account &&
		strings.EqualFold(strings.TrimSpace(a.MarketType), strings.TrimSpace(b.MarketType)) &&
		a.AccountScope == b.AccountScope && strings.EqualFold(strings.TrimSpace(a.IncomeType), strings.TrimSpace(b.IncomeType)) &&
		a.TransactionID == b.TransactionID && strings.EqualFold(strings.TrimSpace(a.Asset), strings.TrimSpace(b.Asset)) &&
		math.Abs(a.Income-b.Income) <= 1e-12 && utils.ToUTC(a.TradeTime).Equal(utils.ToUTC(b.TradeTime))
}

// MarkFundingIncomeCoverage records a successfully fetched and fully persisted
// exchange interval. Overlaps merge; a newer disjoint interval replaces the
// prior snapshot without bridging an outage; an older completion cannot rewind it.
func (s *SQLStorage) MarkFundingIncomeCoverage(exchange, symbol, marketType, accountScope string, startTime, endTime time.Time) error {
	if strings.TrimSpace(exchange) == "" || strings.TrimSpace(symbol) == "" || strings.TrimSpace(marketType) == "" ||
		strings.TrimSpace(accountScope) == "" || startTime.IsZero() || endTime.IsZero() || !startTime.Before(endTime) {
		return fmt.Errorf("funding income coverage requires exact account, market, symbol, and valid interval")
	}
	key := fundingIncomeCoverageKey(exchange, symbol, marketType, accountScope)
	from, through := utils.ToUTC(startTime), utils.ToUTC(endTime)
	if s.dbType == "mysql" {
		_, err := s.db.Exec(`
			INSERT INTO funding_income_sync_state (scope_key, exchange, symbol, market_type, account_scope, covered_from, covered_through, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE
			covered_from=CASE WHEN VALUES(covered_through) < covered_through THEN covered_from
				WHEN VALUES(covered_from) <= covered_through THEN LEAST(covered_from, VALUES(covered_from))
				ELSE VALUES(covered_from) END,
			updated_at=CASE WHEN VALUES(covered_through) >= covered_through THEN VALUES(updated_at) ELSE updated_at END,
			covered_through=GREATEST(covered_through, VALUES(covered_through))
		`, key, exchange, symbol, marketType, accountScope, from, through, time.Now().UTC())
		if err != nil {
			return fmt.Errorf("mark funding income coverage exchange=%s symbol=%s: %w", exchange, symbol, err)
		}
		return nil
	}
	_, err := s.db.Exec(`
		INSERT INTO funding_income_sync_state (scope_key, exchange, symbol, market_type, account_scope, covered_from, covered_through, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(scope_key) DO UPDATE SET
			covered_from=CASE WHEN excluded.covered_through < covered_through THEN covered_from
				WHEN excluded.covered_from <= covered_through THEN MIN(covered_from, excluded.covered_from)
				ELSE excluded.covered_from END,
			updated_at=CASE WHEN excluded.covered_through >= covered_through THEN excluded.updated_at ELSE updated_at END,
			covered_through=MAX(covered_through, excluded.covered_through)
	`, key, exchange, symbol, marketType, accountScope, from, through, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("mark funding income coverage exchange=%s symbol=%s: %w", exchange, symbol, err)
	}
	return nil
}

// HasFundingIncomeCoverage is deliberately exact: an empty rule symbol cannot
// borrow one symbol's watermark to authorize an account-wide transfer.
func (s *SQLStorage) HasFundingIncomeCoverage(exchange, symbol, marketType, accountScope string, startTime, endTime time.Time) (bool, error) {
	if startTime.IsZero() || endTime.IsZero() || !startTime.Before(endTime) {
		return false, fmt.Errorf("funding income coverage requires a non-empty interval")
	}
	coveredFrom, coveredThrough, err := s.GetFundingIncomeCoverage(exchange, symbol, marketType, accountScope)
	if err != nil {
		return false, err
	}
	if coveredFrom.IsZero() || coveredThrough.IsZero() {
		return false, nil
	}
	return !coveredFrom.After(utils.ToUTC(startTime)) && !coveredThrough.Before(utils.ToUTC(endTime)), nil
}

// GetFundingIncomeCoverage returns the most recent fully persisted history
// interval for exactly one account, market, and symbol.
func (s *SQLStorage) GetFundingIncomeCoverage(exchange, symbol, marketType, accountScope string) (time.Time, time.Time, error) {
	if strings.TrimSpace(exchange) == "" || strings.TrimSpace(symbol) == "" || strings.TrimSpace(marketType) == "" ||
		strings.TrimSpace(accountScope) == "" {
		return time.Time{}, time.Time{}, fmt.Errorf("funding income coverage requires exact account, market, and symbol")
	}
	var coveredFrom, coveredThrough time.Time
	err := s.db.QueryRow(`
		SELECT covered_from, covered_through FROM funding_income_sync_state WHERE scope_key = ?
	`, fundingIncomeCoverageKey(exchange, symbol, marketType, accountScope)).Scan(&coveredFrom, &coveredThrough)
	if err == sql.ErrNoRows {
		return time.Time{}, time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("query funding income coverage exchange=%s symbol=%s: %w", exchange, symbol, err)
	}
	return utils.ToUTC(coveredFrom), utils.ToUTC(coveredThrough), nil
}

func fundingIncomeCoverageKey(exchange, symbol, marketType, accountScope string) string {
	identity := strings.Join([]string{
		strings.ToLower(strings.TrimSpace(exchange)), strings.ToUpper(strings.TrimSpace(symbol)),
		strings.ToLower(strings.TrimSpace(marketType)), strings.TrimSpace(accountScope),
	}, "\x00")
	digest := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(digest[:])
}

// GetDailyFundingPaymentsByScope returns one day's funding totals grouped by
// denomination. Legacy rows without an immutable account scope are excluded.
func (s *SQLStorage) GetDailyFundingPaymentsByScope(account, exchange, marketType, symbol, accountScope string, startTime, endTime time.Time) (map[string]float64, error) {
	if accountScope == "" || marketType == "" || exchange == "" || symbol == "" || !startTime.Before(endTime) {
		return nil, fmt.Errorf("daily funding query requires complete account and market scope")
	}
	query := `SELECT UPPER(COALESCE(asset, '')), COALESCE(SUM(income), 0) FROM funding_payments WHERE exchange = ? AND market_type = ? AND symbol = ? AND account_scope = ? AND trade_time >= ? AND trade_time < ?`
	args := []interface{}{exchange, marketType, symbol, accountScope, utils.ToUTC(startTime), utils.ToUTC(endTime)}
	if account != "" {
		query += ` AND account = ?`
		args = append(args, account)
	}
	query += ` GROUP BY UPPER(COALESCE(asset, ''))`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query scoped daily funding payments: %w", err)
	}
	defer rows.Close()
	result := make(map[string]float64)
	for rows.Next() {
		var asset string
		var total float64
		if err := rows.Scan(&asset, &total); err != nil {
			return nil, fmt.Errorf("scan scoped daily funding payments: %w", err)
		}
		if asset == "" {
			return nil, fmt.Errorf("daily funding payment has no denomination")
		}
		result[asset] = total
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate scoped daily funding payments: %w", err)
	}
	return result, nil
}

// GetFundingPaymentsSumByScope returns funding income for one immutable
// account/market/symbol scope. Rows without that exact scope are never inferred.
func (s *SQLStorage) GetFundingPaymentsSumByScope(exchange, marketType, symbol, asset, accountScope string, startTime, endTime time.Time) (float64, error) {
	exchange = strings.ToLower(strings.TrimSpace(exchange))
	marketType = strings.ToLower(strings.TrimSpace(marketType))
	asset = strings.ToUpper(strings.TrimSpace(asset))
	if exchange == "" || marketType == "" || strings.TrimSpace(symbol) == "" || asset == "" || strings.TrimSpace(accountScope) == "" {
		return 0, fmt.Errorf("funding sum requires exchange, market_type, symbol, asset and account_scope")
	}
	var total sql.NullFloat64
	err := s.db.QueryRow(`SELECT SUM(income) FROM funding_payments
		WHERE LOWER(TRIM(exchange)) = ? AND LOWER(TRIM(market_type)) = ? AND UPPER(TRIM(symbol)) = ?
		AND UPPER(TRIM(asset)) = ? AND account_scope = ? AND trade_time >= ? AND trade_time <= ?`,
		exchange, marketType, strings.ToUpper(strings.TrimSpace(symbol)), asset, accountScope,
		utils.ToUTC(startTime), utils.ToUTC(endTime)).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("query scoped funding sum exchange=%s symbol=%s: %w", exchange, symbol, err)
	}
	if !total.Valid {
		return 0, nil
	}
	return total.Float64, nil
}

// GetFundingPaymentsSumByAccountScopeAndAsset aggregates one exchange/account
// scope and denomination while allowing legacy account display labels.
func (s *SQLStorage) GetFundingPaymentsSumByAccountScopeAndAsset(exchange, asset, accountScope string, startTime, endTime time.Time) (float64, error) {
	exchange = strings.ToLower(strings.TrimSpace(exchange))
	asset = strings.ToUpper(strings.TrimSpace(asset))
	accountScope = strings.TrimSpace(accountScope)
	if exchange == "" || asset == "" || accountScope == "" || endTime.Before(startTime) {
		return 0, fmt.Errorf("funding sum requires exchange, asset, account_scope and a valid time range")
	}
	var incomplete int64
	err := s.db.QueryRow(`SELECT COUNT(*) FROM funding_payments
		WHERE trade_time >= ? AND trade_time <= ? AND (
			(TRIM(COALESCE(exchange, '')) = '') OR
			(LOWER(TRIM(exchange)) = ? AND (TRIM(COALESCE(account_scope, '')) = '' OR
				(account_scope = ? AND TRIM(COALESCE(asset, '')) = ''))))`,
		utils.ToUTC(startTime), utils.ToUTC(endTime), exchange, accountScope, accountScope).Scan(&incomplete)
	if err != nil {
		return 0, fmt.Errorf("validate funding ownership and denomination evidence: %w", err)
	}
	if incomplete > 0 {
		return 0, fmt.Errorf("funding total is incomplete: %d payments have unknown exchange, account scope or asset", incomplete)
	}
	var total sql.NullFloat64
	err = s.db.QueryRow(`SELECT SUM(income) FROM funding_payments WHERE LOWER(TRIM(exchange)) = ?
		AND UPPER(TRIM(asset)) = ? AND account_scope = ? AND trade_time >= ? AND trade_time <= ?`,
		exchange, asset, accountScope, utils.ToUTC(startTime), utils.ToUTC(endTime)).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("query funding sum by account scope exchange=%s asset=%s: %w", exchange, asset, err)
	}
	if !total.Valid {
		return 0, nil
	}
	if math.IsNaN(total.Float64) || math.IsInf(total.Float64, 0) {
		return 0, fmt.Errorf("funding sum is not finite for exchange=%s asset=%s", exchange, asset)
	}
	return total.Float64, nil
}

// GetDailyFundingPaymentsByAccountScope returns date totals only for one
// exact account, exchange and market scope; unattributed legacy rows are excluded.
func (s *SQLStorage) GetDailyFundingPaymentsByAccountScopeAndAsset(exchange, marketType, asset, accountScope string, startTime, endTime time.Time) (map[string]float64, error) {
	exchange = strings.ToLower(strings.TrimSpace(exchange))
	marketType = strings.ToLower(strings.TrimSpace(marketType))
	asset = strings.ToUpper(strings.TrimSpace(asset))
	if exchange == "" || marketType == "" || asset == "" || strings.TrimSpace(accountScope) == "" || !startTime.Before(endTime) {
		return nil, fmt.Errorf("daily funding query requires exchange, market_type, asset and account_scope")
	}
	dateExpr := s.dateExprInConfiguredTimezone("trade_time")
	query := fmt.Sprintf(`SELECT %s, COALESCE(SUM(income), 0) FROM funding_payments
		WHERE LOWER(TRIM(exchange)) = ? AND LOWER(TRIM(market_type)) = ? AND UPPER(TRIM(asset)) = ? AND account_scope = ?
		AND trade_time >= ? AND trade_time <= ? GROUP BY %s`, dateExpr, dateExpr)
	rows, err := s.db.Query(query, exchange, marketType, asset, accountScope, utils.ToUTC(startTime), utils.ToUTC(endTime))
	if err != nil {
		return nil, fmt.Errorf("query daily funding by account scope: %w", err)
	}
	defer rows.Close()
	result := make(map[string]float64)
	for rows.Next() {
		var day string
		var total float64
		if err := rows.Scan(&day, &total); err != nil {
			return nil, fmt.Errorf("scan daily funding by account scope: %w", err)
		}
		result[day] = total
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate daily funding by account scope: %w", err)
	}
	return result, nil
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
		query += " AND account = ?"
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

// GetFundingPaymentsByAccountScope lists funding rows for an exact immutable
// credential scope. Account labels are presentation metadata and are not used
// for authorization or ownership filtering.
func (s *SQLStorage) GetFundingPaymentsByAccountScope(accountScope, exchange string, startTime, endTime time.Time) ([]*FundingPayment, error) {
	accountScope = strings.TrimSpace(accountScope)
	exchange = strings.ToLower(strings.TrimSpace(exchange))
	if accountScope == "" || exchange == "" || endTime.Before(startTime) {
		return nil, fmt.Errorf("funding history requires account_scope, exchange and a valid time range")
	}
	rows, err := s.db.Query(`SELECT id, exchange, symbol, account, market_type, account_scope, income_type,
		income, asset, info, transaction_id, trade_time, created_at FROM funding_payments
		WHERE account_scope = ? AND LOWER(TRIM(exchange)) = ? AND trade_time >= ? AND trade_time <= ?
		ORDER BY trade_time DESC LIMIT 10000`, accountScope, exchange, utils.ToUTC(startTime), utils.ToUTC(endTime))
	if err != nil {
		return nil, fmt.Errorf("query funding history by account scope exchange=%s: %w", exchange, err)
	}
	defer rows.Close()
	var payments []*FundingPayment
	for rows.Next() {
		var payment FundingPayment
		if err := rows.Scan(&payment.ID, &payment.Exchange, &payment.Symbol, &payment.Account, &payment.MarketType,
			&payment.AccountScope, &payment.IncomeType, &payment.Income, &payment.Asset, &payment.Info,
			&payment.TransactionID, &payment.TradeTime, &payment.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan funding history by account scope: %w", err)
		}
		payments = append(payments, &payment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate funding history by account scope: %w", err)
	}
	return payments, nil
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
		query += " AND account = ?"
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

	dateExpr := s.dateExprInConfiguredTimezone("trade_time")

	query := fmt.Sprintf(`
		SELECT %s as date, COALESCE(SUM(income), 0) as daily_funding
		FROM funding_payments
		WHERE trade_time >= ? AND trade_time <= ?
	`, dateExpr)
	args := []interface{}{startUTC, endUTC}
	if exchange != "" {
		query += " AND exchange = ?"
		args = append(args, exchange)
	}
	if account != "" {
		query += " AND account = ?"
		args = append(args, account)
	}
	query += " GROUP BY " + dateExpr

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
			// 資金費率聚合不能跳行：少一天就是持倉成本算少了
			return nil, fmt.Errorf("解析每日资金费失败: %w", err)
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
