package storage

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"time"

	"quantmesh/utils"
)

// SaveMarginInterestPayment stores one exchange-confirmed interest charge
// idempotently without inventing a symbol or Bot attribution for cross margin.
func (s *SQLStorage) SaveMarginInterestPayment(payment *MarginInterestPayment) error {
	if payment == nil {
		return fmt.Errorf("margin interest payment requires exact account identity and finite non-negative values")
	}
	normalized := *payment
	var err error
	if normalized.Principal, err = roundMarginInterestDecimal12(normalized.Principal); err != nil {
		return err
	}
	if normalized.Interest, err = roundMarginInterestDecimal12(normalized.Interest); err != nil {
		return err
	}
	if normalized.Rate, err = roundMarginInterestDecimal16(normalized.Rate); err != nil {
		return err
	}
	if normalized.ValuationRate, err = roundMarginInterestDecimal16(normalized.ValuationRate); err != nil {
		return err
	}
	if normalized.ValuationAmount, err = roundMarginInterestDecimal12(normalized.ValuationAmount); err != nil {
		return err
	}
	if normalized.ValuationStatus == "" {
		normalized.ValuationStatus = "UNVALUED"
	}
	normalized.ValuationStatus = strings.ToUpper(strings.TrimSpace(normalized.ValuationStatus))
	if (payment.Principal > 0 && normalized.Principal == 0) || (payment.Interest > 0 && normalized.Interest == 0) || (payment.Rate > 0 && normalized.Rate == 0) {
		return fmt.Errorf("positive margin interest values fall below database precision")
	}
	payment = &normalized
	if strings.TrimSpace(payment.Exchange) == "" || strings.TrimSpace(payment.AccountScope) == "" ||
		strings.TrimSpace(payment.Asset) == "" || strings.TrimSpace(payment.InterestType) == "" || payment.TransactionID <= 0 ||
		payment.AccruedAt.IsZero() || !finiteNonNegative(payment.Principal) || !finiteNonNegative(payment.Interest) || !finiteNonNegative(payment.Rate) {
		return fmt.Errorf("margin interest payment requires exact account identity and finite non-negative values")
	}
	if (payment.ValuationStatus != "VALUED" && payment.ValuationStatus != "UNVALUED") ||
		!finiteNonNegative(payment.ValuationRate) || !finiteNonNegative(payment.ValuationAmount) ||
		(payment.ValuationStatus == "VALUED" && (strings.TrimSpace(payment.ValuationAsset) == "" || (payment.Interest > 0 && (payment.ValuationRate <= 0 || payment.ValuationSource == "")))) ||
		(payment.ValuationStatus == "UNVALUED" && payment.ValuationAmount != 0) {
		return fmt.Errorf("margin interest valuation must be complete and finite or explicitly unvalued")
	}
	if payment.ValuationStatus == "VALUED" && payment.Interest > 0 {
		expectedAmount, err := roundMarginInterestDecimal12(payment.Interest * payment.ValuationRate)
		if err != nil || expectedAmount == 0 || normalized.ValuationAmount != expectedAmount {
			return fmt.Errorf("valued margin interest amount must match its persisted interest and historical rate")
		}
	}
	identity := marginInterestIdentity(payment)
	args := []interface{}{
		strings.ToLower(strings.TrimSpace(payment.Exchange)), payment.Account, strings.TrimSpace(payment.AccountScope),
		strings.ToUpper(strings.TrimSpace(payment.Asset)), strings.ToUpper(strings.TrimSpace(payment.RawAsset)), payment.Principal, payment.Interest, payment.Rate,
		strings.ToUpper(strings.TrimSpace(payment.InterestType)), strings.ToUpper(strings.TrimSpace(payment.IsolatedSymbol)),
		payment.TransactionID, utils.ToUTC(payment.AccruedAt), identity,
		strings.ToUpper(strings.TrimSpace(payment.ValuationAsset)), payment.ValuationRate, payment.ValuationAmount, payment.ValuationStatus, payment.ValuationMinute, strings.TrimSpace(payment.ValuationSource),
	}
	query := `INSERT INTO margin_interest_payments
		(exchange, account, account_scope, asset, raw_asset, principal, interest, interest_rate, interest_type, isolated_symbol, transaction_id, accrued_at, identity_key, valuation_asset, valuation_rate, valuation_amount, valuation_status, valuation_minute, valuation_source)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(identity_key) DO NOTHING`
	if s.dbType == "mysql" {
		query = `INSERT INTO margin_interest_payments
			(exchange, account, account_scope, asset, raw_asset, principal, interest, interest_rate, interest_type, isolated_symbol, transaction_id, accrued_at, identity_key, valuation_asset, valuation_rate, valuation_amount, valuation_status, valuation_minute, valuation_source)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE identity_key=identity_key`
	}
	_, err = s.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("save margin interest transaction %d: %w", payment.TransactionID, err)
	}
	if _, err := s.db.Exec(`UPDATE margin_interest_payments SET valuation_asset=?, valuation_rate=?, valuation_amount=?,
		valuation_status='VALUED', valuation_minute=?, valuation_source=?
		WHERE identity_key=? AND valuation_status='UNVALUED' AND ?='VALUED'`,
		strings.ToUpper(strings.TrimSpace(payment.ValuationAsset)), payment.ValuationRate, payment.ValuationAmount,
		payment.ValuationMinute, strings.TrimSpace(payment.ValuationSource), identity, payment.ValuationStatus); err != nil {
		return fmt.Errorf("update margin interest valuation for transaction %d: %w", payment.TransactionID, err)
	}
	var existing MarginInterestPayment
	err = s.db.QueryRow(`SELECT exchange, account_scope, asset, raw_asset, principal, interest, interest_rate, interest_type, isolated_symbol, transaction_id, accrued_at,
		valuation_asset, valuation_rate, valuation_amount, valuation_status, valuation_minute, valuation_source
		FROM margin_interest_payments WHERE identity_key = ?`, identity).Scan(
		&existing.Exchange, &existing.AccountScope, &existing.Asset, &existing.RawAsset, &existing.Principal, &existing.Interest,
		&existing.Rate, &existing.InterestType, &existing.IsolatedSymbol, &existing.TransactionID, &existing.AccruedAt,
		&existing.ValuationAsset, &existing.ValuationRate, &existing.ValuationAmount, &existing.ValuationStatus, &existing.ValuationMinute, &existing.ValuationSource)
	if err != nil {
		return fmt.Errorf("verify saved margin interest transaction %d: %w", payment.TransactionID, err)
	}
	if !sameMarginInterestPayment(existing, *payment) {
		return fmt.Errorf("margin interest transaction %d conflicts with its previously saved identity", payment.TransactionID)
	}
	return nil
}

func (s *SQLStorage) SaveMarginInterestAllocation(allocation *MarginInterestAllocation) error {
	if allocation == nil {
		return fmt.Errorf("margin interest allocation requires exact Bot/account identity and positive verified amounts")
	}
	normalized := *allocation
	var err error
	if normalized.BotPrincipal, err = roundMarginInterestDecimal12(normalized.BotPrincipal); err != nil {
		return err
	}
	if normalized.AccountPrincipal, err = roundMarginInterestDecimal12(normalized.AccountPrincipal); err != nil {
		return err
	}
	if normalized.Interest, err = roundMarginInterestDecimal12(normalized.Interest); err != nil {
		return err
	}
	if normalized.ValuationRate, err = roundMarginInterestDecimal16(normalized.ValuationRate); err != nil {
		return err
	}
	if normalized.ValuationAmount, err = roundMarginInterestDecimal12(normalized.ValuationAmount); err != nil {
		return err
	}
	if normalized.ValuationStatus == "" {
		normalized.ValuationStatus = "UNVALUED"
	}
	normalized.ValuationStatus = strings.ToUpper(strings.TrimSpace(normalized.ValuationStatus))
	if strings.TrimSpace(normalized.Exchange) == "" || strings.TrimSpace(normalized.AccountScope) == "" ||
		normalized.TransactionID <= 0 || strings.TrimSpace(normalized.BotID) == "" || strings.TrimSpace(normalized.Asset) == "" ||
		strings.TrimSpace(normalized.RawAsset) == "" || !finiteNonNegative(normalized.BotPrincipal) || normalized.BotPrincipal <= 0 ||
		!finiteNonNegative(normalized.AccountPrincipal) || normalized.AccountPrincipal < normalized.BotPrincipal ||
		!finiteNonNegative(normalized.Interest) || normalized.Interest <= 0 || normalized.AccruedAt.IsZero() {
		return fmt.Errorf("margin interest allocation requires exact Bot/account identity and positive verified amounts")
	}
	allocation = &normalized
	if (allocation.ValuationStatus != "VALUED" && allocation.ValuationStatus != "UNVALUED") ||
		!finiteNonNegative(allocation.ValuationRate) || !finiteNonNegative(allocation.ValuationAmount) ||
		(allocation.ValuationStatus == "VALUED" && (strings.TrimSpace(allocation.ValuationAsset) == "" || allocation.ValuationRate <= 0 || allocation.ValuationMinute <= 0 || allocation.ValuationSource == "")) ||
		(allocation.ValuationStatus == "UNVALUED" && allocation.ValuationAmount != 0) {
		return fmt.Errorf("margin interest allocation valuation must be complete and finite or explicitly unvalued")
	}
	if allocation.ValuationStatus == "VALUED" {
		expectedAmount, err := roundMarginInterestDecimal12(allocation.Interest * allocation.ValuationRate)
		if err != nil || expectedAmount == 0 || math.Abs(normalized.ValuationAmount-expectedAmount) > 1e-12 {
			return fmt.Errorf("margin interest allocation valuation must reconcile to its interest and historical rate")
		}
	}
	identity := marginInterestAllocationIdentity(allocation)
	args := []interface{}{
		strings.ToLower(strings.TrimSpace(allocation.Exchange)), strings.TrimSpace(allocation.AccountScope), allocation.TransactionID,
		strings.TrimSpace(allocation.BotID), strings.ToUpper(strings.TrimSpace(allocation.Asset)), strings.ToUpper(strings.TrimSpace(allocation.RawAsset)),
		allocation.BotPrincipal, allocation.AccountPrincipal, allocation.Interest, utils.ToUTC(allocation.AccruedAt), identity,
		strings.ToUpper(strings.TrimSpace(allocation.ValuationAsset)), allocation.ValuationRate, allocation.ValuationAmount,
		allocation.ValuationStatus, allocation.ValuationMinute, strings.TrimSpace(allocation.ValuationSource),
	}
	query := `INSERT INTO margin_interest_allocations
		(exchange, account_scope, transaction_id, bot_id, asset, raw_asset, bot_principal, account_principal, interest, accrued_at, identity_key, valuation_asset, valuation_rate, valuation_amount, valuation_status, valuation_minute, valuation_source)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(identity_key) DO NOTHING`
	if s.dbType == "mysql" {
		query = `INSERT INTO margin_interest_allocations
			(exchange, account_scope, transaction_id, bot_id, asset, raw_asset, bot_principal, account_principal, interest, accrued_at, identity_key, valuation_asset, valuation_rate, valuation_amount, valuation_status, valuation_minute, valuation_source)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE identity_key=identity_key`
	}
	if _, err := s.db.Exec(query, args...); err != nil {
		return fmt.Errorf("save margin interest allocation tx=%d bot=%s: %w", allocation.TransactionID, allocation.BotID, err)
	}
	if _, err := s.db.Exec(`UPDATE margin_interest_allocations SET valuation_asset=?, valuation_rate=?, valuation_amount=?,
		valuation_status='VALUED', valuation_minute=?, valuation_source=?
		WHERE identity_key=? AND valuation_status='UNVALUED' AND ?='VALUED'`,
		strings.ToUpper(strings.TrimSpace(allocation.ValuationAsset)), allocation.ValuationRate, allocation.ValuationAmount,
		allocation.ValuationMinute, strings.TrimSpace(allocation.ValuationSource), identity, allocation.ValuationStatus); err != nil {
		return fmt.Errorf("update margin interest allocation valuation tx=%d bot=%s: %w", allocation.TransactionID, allocation.BotID, err)
	}
	var existing MarginInterestAllocation
	err = s.db.QueryRow(`SELECT exchange, account_scope, transaction_id, bot_id, asset, raw_asset, bot_principal, account_principal, interest, accrued_at,
		valuation_asset, valuation_rate, valuation_amount, valuation_status, valuation_minute, valuation_source
		FROM margin_interest_allocations WHERE identity_key = ?`, identity).Scan(
		&existing.Exchange, &existing.AccountScope, &existing.TransactionID, &existing.BotID, &existing.Asset, &existing.RawAsset,
		&existing.BotPrincipal, &existing.AccountPrincipal, &existing.Interest, &existing.AccruedAt,
		&existing.ValuationAsset, &existing.ValuationRate, &existing.ValuationAmount, &existing.ValuationStatus, &existing.ValuationMinute, &existing.ValuationSource)
	if err != nil {
		return fmt.Errorf("verify margin interest allocation tx=%d bot=%s: %w", allocation.TransactionID, allocation.BotID, err)
	}
	if !sameMarginInterestAllocation(existing, *allocation) {
		return fmt.Errorf("margin interest allocation tx=%d bot=%s conflicts with its saved identity", allocation.TransactionID, allocation.BotID)
	}
	return nil
}

func roundMarginInterestDecimal12(value float64) (float64, error) {
	return roundMarginInterestDecimal(value, 1e12)
}

func roundMarginInterestDecimal16(value float64) (float64, error) {
	return roundMarginInterestDecimal(value, 1e16)
}

func roundMarginInterestDecimal(value, scale float64) (float64, error) {
	if !finiteNonNegative(value) || value > math.MaxFloat64/scale {
		return 0, fmt.Errorf("margin interest amount is outside supported database decimal precision")
	}
	rounded := math.Round(value*scale) / scale
	if !finiteNonNegative(rounded) {
		return 0, fmt.Errorf("margin interest allocation rounding produced an invalid amount")
	}
	return rounded, nil
}

func (s *SQLStorage) ListMarginInterestAllocations(exchange, accountScope string, transactionID int64) ([]*MarginInterestAllocation, error) {
	if strings.TrimSpace(exchange) == "" || strings.TrimSpace(accountScope) == "" || transactionID <= 0 {
		return nil, fmt.Errorf("margin interest allocation lookup requires exact exchange, account scope, and transaction")
	}
	rows, err := s.db.Query(`SELECT exchange, account_scope, transaction_id, bot_id, asset, raw_asset, bot_principal, account_principal, interest, accrued_at,
		valuation_asset, valuation_rate, valuation_amount, valuation_status, valuation_minute, valuation_source
		FROM margin_interest_allocations WHERE LOWER(TRIM(exchange))=? AND account_scope=? AND transaction_id=? ORDER BY bot_id`,
		strings.ToLower(strings.TrimSpace(exchange)), strings.TrimSpace(accountScope), transactionID)
	if err != nil {
		return nil, fmt.Errorf("list margin interest allocations tx=%d: %w", transactionID, err)
	}
	defer rows.Close()
	allocations := make([]*MarginInterestAllocation, 0)
	for rows.Next() {
		allocation := &MarginInterestAllocation{}
		if err := rows.Scan(&allocation.Exchange, &allocation.AccountScope, &allocation.TransactionID, &allocation.BotID, &allocation.Asset,
			&allocation.RawAsset, &allocation.BotPrincipal, &allocation.AccountPrincipal, &allocation.Interest, &allocation.AccruedAt,
			&allocation.ValuationAsset, &allocation.ValuationRate, &allocation.ValuationAmount, &allocation.ValuationStatus, &allocation.ValuationMinute, &allocation.ValuationSource); err != nil {
			return nil, fmt.Errorf("scan margin interest allocation tx=%d: %w", transactionID, err)
		}
		allocations = append(allocations, allocation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate margin interest allocations tx=%d: %w", transactionID, err)
	}
	return allocations, nil
}

func marginInterestAllocationIdentity(allocation *MarginInterestAllocation) string {
	material := strings.Join([]string{
		strings.ToLower(strings.TrimSpace(allocation.Exchange)), strings.TrimSpace(allocation.AccountScope),
		fmt.Sprint(allocation.TransactionID), strings.TrimSpace(allocation.BotID),
	}, "\x00")
	digest := sha256.Sum256([]byte(material))
	return hex.EncodeToString(digest[:])
}

func sameMarginInterestAllocation(a, b MarginInterestAllocation) bool {
	return strings.EqualFold(strings.TrimSpace(a.Exchange), strings.TrimSpace(b.Exchange)) && a.AccountScope == b.AccountScope &&
		a.TransactionID == b.TransactionID && a.BotID == b.BotID && strings.EqualFold(a.Asset, b.Asset) && strings.EqualFold(a.RawAsset, b.RawAsset) &&
		a.BotPrincipal == b.BotPrincipal && a.AccountPrincipal == b.AccountPrincipal && a.Interest == b.Interest &&
		utils.ToUTC(a.AccruedAt).Equal(utils.ToUTC(b.AccruedAt))
}

func marginInterestIdentity(payment *MarginInterestPayment) string {
	identity := strings.Join([]string{
		strings.ToLower(strings.TrimSpace(payment.Exchange)), strings.TrimSpace(payment.AccountScope),
		strings.ToUpper(strings.TrimSpace(payment.Asset)), fmt.Sprint(payment.TransactionID),
	}, "\x00")
	digest := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(digest[:])
}

func sameMarginInterestPayment(a, b MarginInterestPayment) bool {
	return strings.EqualFold(strings.TrimSpace(a.Exchange), strings.TrimSpace(b.Exchange)) &&
		a.AccountScope == b.AccountScope && strings.EqualFold(strings.TrimSpace(a.Asset), strings.TrimSpace(b.Asset)) &&
		strings.EqualFold(strings.TrimSpace(a.RawAsset), strings.TrimSpace(b.RawAsset)) &&
		a.Principal == b.Principal && a.Interest == b.Interest && a.Rate == b.Rate &&
		strings.EqualFold(strings.TrimSpace(a.InterestType), strings.TrimSpace(b.InterestType)) &&
		strings.EqualFold(strings.TrimSpace(a.IsolatedSymbol), strings.TrimSpace(b.IsolatedSymbol)) &&
		a.TransactionID == b.TransactionID && utils.ToUTC(a.AccruedAt).Equal(utils.ToUTC(b.AccruedAt))
}

// MarkMarginInterestCoverage is called only after every page and row was saved.
// Overlapping intervals merge; disjoint newer snapshots do not hide outages.
func (s *SQLStorage) MarkMarginInterestCoverage(exchange, accountScope, asset string, startTime, endTime time.Time) error {
	if strings.TrimSpace(exchange) == "" || strings.TrimSpace(accountScope) == "" || strings.TrimSpace(asset) == "" ||
		startTime.IsZero() || endTime.IsZero() || !startTime.Before(endTime) {
		return fmt.Errorf("margin interest coverage requires exact account, asset, and valid interval")
	}
	key := marginInterestCoverageKey(exchange, accountScope, asset)
	from, through, now := utils.ToUTC(startTime), utils.ToUTC(endTime), time.Now().UTC()
	query := `INSERT INTO margin_interest_sync_state (scope_key, exchange, account_scope, asset, covered_from, covered_through, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT(scope_key) DO UPDATE SET
		covered_from=CASE WHEN excluded.covered_through < covered_through THEN covered_from WHEN excluded.covered_from <= covered_through THEN MIN(covered_from, excluded.covered_from) ELSE excluded.covered_from END,
		updated_at=CASE WHEN excluded.covered_through >= covered_through THEN excluded.updated_at ELSE updated_at END,
		covered_through=MAX(covered_through, excluded.covered_through)`
	if s.dbType == "mysql" {
		query = `INSERT INTO margin_interest_sync_state (scope_key, exchange, account_scope, asset, covered_from, covered_through, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE
			covered_from=CASE WHEN VALUES(covered_through) < covered_through THEN covered_from WHEN VALUES(covered_from) <= covered_through THEN LEAST(covered_from, VALUES(covered_from)) ELSE VALUES(covered_from) END,
			updated_at=CASE WHEN VALUES(covered_through) >= covered_through THEN VALUES(updated_at) ELSE updated_at END,
			covered_through=GREATEST(covered_through, VALUES(covered_through))`
	}
	if _, err := s.db.Exec(query, key, strings.ToLower(strings.TrimSpace(exchange)), strings.TrimSpace(accountScope), strings.ToUpper(strings.TrimSpace(asset)), from, through, now); err != nil {
		return fmt.Errorf("mark margin interest coverage asset=%s: %w", asset, err)
	}
	return nil
}

func (s *SQLStorage) GetMarginInterestCoverage(exchange, accountScope, asset string) (time.Time, time.Time, error) {
	if strings.TrimSpace(exchange) == "" || strings.TrimSpace(accountScope) == "" || strings.TrimSpace(asset) == "" {
		return time.Time{}, time.Time{}, fmt.Errorf("margin interest coverage requires exact exchange, account, and asset")
	}
	var from, through time.Time
	err := s.db.QueryRow(`SELECT covered_from, covered_through FROM margin_interest_sync_state WHERE scope_key = ?`, marginInterestCoverageKey(exchange, accountScope, asset)).Scan(&from, &through)
	if err == sql.ErrNoRows {
		return time.Time{}, time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("query margin interest coverage asset=%s: %w", asset, err)
	}
	return utils.ToUTC(from), utils.ToUTC(through), nil
}

func (s *SQLStorage) HasMarginInterestCoverage(exchange, accountScope, asset string, startTime, endTime time.Time) (bool, error) {
	if startTime.IsZero() || endTime.IsZero() || !startTime.Before(endTime) {
		return false, fmt.Errorf("margin interest coverage requires a non-empty interval")
	}
	from, through, err := s.GetMarginInterestCoverage(exchange, accountScope, asset)
	if err != nil || from.IsZero() || through.IsZero() {
		return false, err
	}
	return !from.After(utils.ToUTC(startTime)) && !through.Before(utils.ToUTC(endTime)), nil
}

func marginInterestCoverageKey(exchange, accountScope, asset string) string {
	identity := strings.Join([]string{strings.ToLower(strings.TrimSpace(exchange)), strings.TrimSpace(accountScope), strings.ToUpper(strings.TrimSpace(asset))}, "\x00")
	digest := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(digest[:])
}

func (s *SQLStorage) GetMarginInterestTotalByAccountScope(exchange, accountScope, asset string, startTime, endTime time.Time) (float64, error) {
	if strings.TrimSpace(exchange) == "" || strings.TrimSpace(accountScope) == "" || strings.TrimSpace(asset) == "" ||
		startTime.IsZero() || endTime.IsZero() || !startTime.Before(endTime) {
		return 0, fmt.Errorf("margin interest sum requires exact account, asset, and valid interval")
	}
	covered, err := s.HasMarginInterestCoverage(exchange, accountScope, "*", startTime, endTime)
	if err != nil {
		return 0, fmt.Errorf("verify margin interest coverage: %w", err)
	}
	if !covered {
		return 0, fmt.Errorf("margin interest history does not fully cover requested interval for %s", asset)
	}
	var total sql.NullFloat64
	err = s.db.QueryRow(`SELECT SUM(interest) FROM margin_interest_payments WHERE LOWER(TRIM(exchange))=? AND account_scope=? AND UPPER(TRIM(asset))=? AND accrued_at>? AND accrued_at<=?`,
		strings.ToLower(strings.TrimSpace(exchange)), strings.TrimSpace(accountScope), strings.ToUpper(strings.TrimSpace(asset)), utils.ToUTC(startTime), utils.ToUTC(endTime)).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("sum margin interest by account scope asset=%s: %w", asset, err)
	}
	if !total.Valid {
		return 0, nil
	}
	if !finiteNonNegative(total.Float64) {
		return 0, fmt.Errorf("margin interest sum is invalid for asset=%s", asset)
	}
	return total.Float64, nil
}
