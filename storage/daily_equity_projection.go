package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"quantmesh/accounting/daily"
)

// dailyEquityQuerier is satisfied by *sql.DB and *sql.Tx. Every read that must
// be consistent with the publication CAS is executed on the same transaction.
type dailyEquityQuerier interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type dailyEquityFillRow struct {
	id                   int64
	exchange             string
	marketType           string
	accountScope         string
	account              string
	botID                string
	symbol               string
	tradeID              string
	orderID              int64
	side                 string
	price                string
	quantity             string
	quoteQuantity        string
	commission           string
	commissionAsset      string
	commissionQuote      string
	commissionQuoteKnown int64
	realizedPnL          sql.NullString
	realizedPnLAsset     string
	tradeTime            time.Time
}

type dailyEquityFundingRow struct {
	id           int64
	exchange     string
	symbol       string
	account      string
	marketType   string
	accountScope string
	incomeType   string
	income       string
	asset        string
	transaction  sql.NullInt64
	identityKey  sql.NullString
	tradeTime    time.Time
}

type dailyEquityInterestRow struct {
	id              int64
	exchange        string
	accountScope    string
	asset           string
	rawAsset        string
	principal       string
	interest        string
	rate            string
	interestType    string
	isolatedSymbol  string
	transactionID   int64
	accruedAt       time.Time
	identityKey     string
	valuationAsset  string
	valuationRate   string
	valuationAmount string
	valuationStatus string
	valuationMinute int64
	valuationSource string
}

// loadDailyEquityProjection reads the existing ledgers as a projection. It
// never writes fills, funding or interest and never sums through float64.
func loadDailyEquityProjection(ctx context.Context, q dailyEquityQuerier, evidence daily.Evidence) ([]daily.LedgerEntry, []daily.Coverage, []string, error) {
	start, end, err := daily.DayBounds(evidence.Date)
	if err != nil {
		return nil, nil, nil, err
	}
	exchange := strings.ToLower(strings.TrimSpace(evidence.Scope.Exchange))
	accountScope := strings.TrimSpace(evidence.Scope.AccountScope)
	if exchange == "" || accountScope == "" {
		return nil, nil, nil, fmt.Errorf("%w: daily equity projection requires exchange and account scope", daily.ErrInvalid)
	}

	fills, err := queryDailyEquityFills(ctx, q, exchange, accountScope, start, end)
	if err != nil {
		return nil, nil, nil, err
	}
	funding, err := queryDailyEquityFunding(ctx, q, exchange, accountScope, start, end)
	if err != nil {
		return nil, nil, nil, err
	}
	interest, err := queryDailyEquityInterest(ctx, q, exchange, accountScope, start, end)
	if err != nil {
		return nil, nil, nil, err
	}

	entries := make([]daily.LedgerEntry, 0, len(fills)*2+len(funding)+len(interest))
	blockers := make([]string, 0)
	entries, blockers = projectDailyEquityFills(entries, blockers, fills, start)
	entries, blockers = projectDailyEquityFunding(entries, blockers, funding, start)
	entries, blockers = projectDailyEquityInterest(entries, blockers, interest, start)
	correctionBlockers, err := loadDailyEquityTradeFeeCorrectionBlockers(ctx, q, exchange, accountScope)
	if err != nil {
		return nil, nil, nil, err
	}
	blockers = append(blockers, correctionBlockers...)

	symbols := dailyEquityDiscoveredFuturesSymbols(evidence, entries)
	coverage, blockers, err := loadDailyEquityLedgerCoverage(ctx, q, exchange, accountScope, start, end, evidence, symbols, fills, funding, interest, blockers)
	if err != nil {
		return nil, nil, nil, err
	}
	sort.Strings(blockers)
	return entries, coverage, blockers, nil
}

func queryDailyEquityFills(ctx context.Context, q dailyEquityQuerier, exchange, accountScope string, start, end time.Time) ([]dailyEquityFillRow, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, exchange, market_type, account_scope, account, bot_id, symbol, trade_id, order_id, side,
		price, quantity, quote_quantity, commission, commission_asset, commission_quote, commission_quote_known,
		realized_pnl, realized_pnl_asset, trade_time
		FROM order_fills
		WHERE account_scope = ? AND LOWER(TRIM(exchange)) = ? AND trade_time >= ? AND trade_time < ?
		ORDER BY trade_time, id`, accountScope, exchange, start, end)
	if err != nil {
		return nil, fmt.Errorf("query daily equity order fills: %w", err)
	}
	defer rows.Close()
	result := make([]dailyEquityFillRow, 0)
	for rows.Next() {
		var row dailyEquityFillRow
		if err := rows.Scan(&row.id, &row.exchange, &row.marketType, &row.accountScope, &row.account, &row.botID,
			&row.symbol, &row.tradeID, &row.orderID, &row.side, &row.price, &row.quantity, &row.quoteQuantity,
			&row.commission, &row.commissionAsset, &row.commissionQuote, &row.commissionQuoteKnown,
			&row.realizedPnL, &row.realizedPnLAsset, &row.tradeTime); err != nil {
			return nil, fmt.Errorf("scan daily equity order fill: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate daily equity order fills: %w", err)
	}
	return result, nil
}

func queryDailyEquityFunding(ctx context.Context, q dailyEquityQuerier, exchange, accountScope string, start, end time.Time) ([]dailyEquityFundingRow, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, exchange, symbol, COALESCE(account, ''), market_type, account_scope,
		income_type, income, COALESCE(asset, ''), transaction_id, identity_key, trade_time
		FROM funding_payments
		WHERE account_scope = ? AND LOWER(TRIM(exchange)) = ? AND trade_time >= ? AND trade_time < ?
		ORDER BY trade_time, id`, accountScope, exchange, start, end)
	if err != nil {
		return nil, fmt.Errorf("query daily equity funding payments: %w", err)
	}
	defer rows.Close()
	result := make([]dailyEquityFundingRow, 0)
	for rows.Next() {
		var row dailyEquityFundingRow
		if err := rows.Scan(&row.id, &row.exchange, &row.symbol, &row.account, &row.marketType, &row.accountScope,
			&row.incomeType, &row.income, &row.asset, &row.transaction, &row.identityKey, &row.tradeTime); err != nil {
			return nil, fmt.Errorf("scan daily equity funding payment: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate daily equity funding payments: %w", err)
	}
	return result, nil
}

func queryDailyEquityInterest(ctx context.Context, q dailyEquityQuerier, exchange, accountScope string, start, end time.Time) ([]dailyEquityInterestRow, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, exchange, account_scope, asset, raw_asset, principal, interest, interest_rate,
		interest_type, isolated_symbol, transaction_id, accrued_at, identity_key, valuation_asset, valuation_rate,
		valuation_amount, valuation_status, valuation_minute, valuation_source
		FROM margin_interest_payments
		WHERE account_scope = ? AND LOWER(TRIM(exchange)) = ? AND accrued_at >= ? AND accrued_at < ?
		ORDER BY accrued_at, id`, accountScope, exchange, start, end)
	if err != nil {
		return nil, fmt.Errorf("query daily equity margin interest payments: %w", err)
	}
	defer rows.Close()
	result := make([]dailyEquityInterestRow, 0)
	for rows.Next() {
		var row dailyEquityInterestRow
		if err := rows.Scan(&row.id, &row.exchange, &row.accountScope, &row.asset, &row.rawAsset, &row.principal,
			&row.interest, &row.rate, &row.interestType, &row.isolatedSymbol, &row.transactionID, &row.accruedAt,
			&row.identityKey, &row.valuationAsset, &row.valuationRate, &row.valuationAmount, &row.valuationStatus,
			&row.valuationMinute, &row.valuationSource); err != nil {
			return nil, fmt.Errorf("scan daily equity margin interest payment: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate daily equity margin interest payments: %w", err)
	}
	return result, nil
}

func projectDailyEquityFills(entries []daily.LedgerEntry, blockers []string, fills []dailyEquityFillRow, start time.Time) ([]daily.LedgerEntry, []string) {
	for _, row := range fills {
		market := strings.ToLower(strings.TrimSpace(row.marketType))
		symbol := strings.TrimSpace(row.symbol)
		tradeID := strings.TrimSpace(row.tradeID)
		at := row.tradeTime.UTC()
		futures := market == "futures"
		identityComplete := market != "" && symbol != "" && tradeID != ""
		baseID := fmt.Sprintf("fill/%s/%s/%s", market, symbol, tradeID)
		if !identityComplete {
			baseID = fmt.Sprintf("fill/row/%d", row.id)
			blockers = append(blockers, fmt.Sprintf("fill source identity incomplete row=%d market=%q symbol=%q trade_id=%q", row.id, market, symbol, tradeID))
		}
		if at.IsZero() {
			at = start
			blockers = append(blockers, fmt.Sprintf("fill trade time missing id=%s", baseID))
		}
		pnlKnown := futures && identityComplete
		feeKnown := futures && identityComplete
		if !futures {
			blockers = append(blockers, fmt.Sprintf("fill market unsupported id=%s market=%q", baseID, market))
		}
		entrySymbol := symbol
		if !futures {
			// Non-futures rows must not pollute futures-symbol discovery.
			entrySymbol = ""
		}

		pnlAsset := strings.ToUpper(strings.TrimSpace(row.realizedPnLAsset))
		pnlAmount := "0"
		if !row.realizedPnL.Valid {
			pnlKnown = false
			blockers = append(blockers, fmt.Sprintf("fill realized pnl missing id=%s", baseID))
		} else if canonical, err := dailyLedgerCanonicalDecimal(row.realizedPnL.String); err != nil {
			pnlKnown = false
			blockers = append(blockers, fmt.Sprintf("fill realized pnl invalid id=%s error=%s", baseID, err.Error()))
		} else {
			pnlAmount = canonical
			if pnlAsset != daily.Asset {
				if !(pnlAsset == "" && canonical == "0") {
					pnlKnown = false
					blockers = append(blockers, fmt.Sprintf("fill realized pnl asset unsupported id=%s asset=%q", baseID, pnlAsset))
				} else {
					pnlAsset = daily.Asset
				}
			}
		}
		quantity := ""
		if canonical, err := dailyLedgerCanonicalDecimal(row.quantity); err != nil {
			pnlKnown = false
			blockers = append(blockers, fmt.Sprintf("fill quantity invalid id=%s error=%s", baseID, err.Error()))
		} else {
			quantity = canonical
		}
		if !pnlKnown {
			pnlAmount = "0"
			pnlAsset = ""
		}
		entries = append(entries, daily.LedgerEntry{
			ID:       baseID + "/pnl",
			Kind:     daily.RealizedPnL,
			Market:   market,
			Symbol:   entrySymbol,
			Asset:    pnlAsset,
			At:       at,
			Amount:   pnlAmount,
			Known:    pnlKnown,
			Quantity: quantity,
			Side:     strings.ToUpper(strings.TrimSpace(row.side)),
		})

		feeAsset := strings.ToUpper(strings.TrimSpace(row.commissionAsset))
		feeAmount := "0"
		canonicalCommission, commissionErr := dailyLedgerCanonicalDecimal(row.commission)
		if commissionErr != nil {
			feeKnown = false
			blockers = append(blockers, fmt.Sprintf("fill fee invalid id=%s error=%s", baseID, commissionErr.Error()))
		} else if negated, err := dailyLedgerNegateDecimal(canonicalCommission); err != nil {
			feeKnown = false
			blockers = append(blockers, fmt.Sprintf("fill fee invalid id=%s error=%s", baseID, err.Error()))
		} else {
			feeAmount = negated
			if canonicalCommission == "0" {
				feeAsset = daily.Asset
			} else if feeAsset != daily.Asset {
				feeKnown = false
				blockers = append(blockers, fmt.Sprintf("fill fee asset unsupported id=%s asset=%q", baseID, feeAsset))
			}
		}
		if !feeKnown {
			feeAmount = "0"
			feeAsset = ""
		}
		entries = append(entries, daily.LedgerEntry{
			ID:     baseID + "/fee",
			Kind:   daily.TradingFee,
			Market: market,
			Symbol: entrySymbol,
			Asset:  feeAsset,
			At:     at,
			Amount: feeAmount,
			Known:  feeKnown,
		})
	}
	return entries, blockers
}

type dailyEquityFundingProjection struct {
	entry    daily.LedgerEntry
	groupKey string
	account  string
}

func projectDailyEquityFunding(entries []daily.LedgerEntry, blockers []string, rows []dailyEquityFundingRow, start time.Time) ([]daily.LedgerEntry, []string) {
	projected := make([]dailyEquityFundingProjection, 0, len(rows))
	groups := make(map[string][]int)
	for _, row := range rows {
		market := strings.ToLower(strings.TrimSpace(row.marketType))
		symbol := strings.TrimSpace(row.symbol)
		incomeType := strings.ToUpper(strings.TrimSpace(row.incomeType))
		asset := strings.ToUpper(strings.TrimSpace(row.asset))
		at := row.tradeTime.UTC()
		identity := ""
		if row.identityKey.Valid {
			identity = strings.TrimSpace(row.identityKey.String)
		}
		id := "funding/" + identity
		if identity == "" {
			id = fmt.Sprintf("funding/row/%d", row.id)
		}
		if at.IsZero() {
			at = start
			blockers = append(blockers, fmt.Sprintf("funding trade time missing id=%s", id))
		}
		known := market == "futures" && incomeType == "FUNDING_FEE" && asset == daily.Asset && symbol != "" && identity != ""
		details := make([]string, 0, 4)
		if market != "futures" {
			details = append(details, fmt.Sprintf("market=%q", market))
		}
		if incomeType != "FUNDING_FEE" {
			details = append(details, fmt.Sprintf("income_type=%q", incomeType))
		}
		if asset != daily.Asset {
			details = append(details, fmt.Sprintf("asset=%q", asset))
		}
		if symbol == "" {
			details = append(details, "symbol empty")
		}
		if identity == "" {
			details = append(details, "identity_key empty")
		}
		if len(details) > 0 {
			blockers = append(blockers, fmt.Sprintf("funding record unsupported id=%s %s", id, strings.Join(details, " ")))
		}
		amount := "0"
		if canonical, err := dailyLedgerCanonicalDecimal(row.income); err != nil {
			known = false
			blockers = append(blockers, fmt.Sprintf("funding amount invalid id=%s error=%s", id, err.Error()))
		} else {
			amount = canonical
		}
		entrySymbol := ""
		if market == "futures" {
			entrySymbol = symbol
		}
		entry := daily.LedgerEntry{
			ID:     id,
			Kind:   daily.Funding,
			Market: market,
			Symbol: entrySymbol,
			Asset:  asset,
			At:     at,
			Amount: amount,
			Known:  known,
		}
		if !known {
			entry.Asset = ""
		}
		groupKey := ""
		if row.transaction.Valid && row.transaction.Int64 > 0 {
			groupKey = strings.Join([]string{market, symbol, incomeType, asset, fmt.Sprint(row.transaction.Int64)}, "\x00")
		}
		projected = append(projected, dailyEquityFundingProjection{entry: entry, groupKey: groupKey, account: row.account})
		if groupKey != "" {
			groups[groupKey] = append(groups[groupKey], len(projected)-1)
		}
	}
	skip := make(map[int]struct{})
	for key, indexes := range groups {
		if len(indexes) < 2 {
			continue
		}
		accounts := make([]string, 0, len(indexes))
		seen := make(map[string]struct{}, len(indexes))
		for _, index := range indexes {
			account := projected[index].account
			if account == "" {
				account = "<empty>"
			}
			if _, exists := seen[account]; exists {
				continue
			}
			seen[account] = struct{}{}
			accounts = append(accounts, account)
		}
		sort.Strings(accounts)
		blockers = append(blockers, fmt.Sprintf("funding duplicate transaction identity %s accounts=%s", strings.ReplaceAll(key, "\x00", "/"), strings.Join(accounts, "|")))
		for position, index := range indexes {
			if position == 0 {
				projected[index].entry.Known = false
				projected[index].entry.Asset = ""
				continue
			}
			skip[index] = struct{}{}
		}
	}
	for index, item := range projected {
		if _, skipped := skip[index]; skipped {
			continue
		}
		entries = append(entries, item.entry)
	}
	return entries, blockers
}

func projectDailyEquityInterest(entries []daily.LedgerEntry, blockers []string, rows []dailyEquityInterestRow, start time.Time) ([]daily.LedgerEntry, []string) {
	for _, row := range rows {
		asset := strings.ToUpper(strings.TrimSpace(row.asset))
		rawAsset := strings.ToUpper(strings.TrimSpace(row.rawAsset))
		valuationAsset := strings.ToUpper(strings.TrimSpace(row.valuationAsset))
		valuationStatus := strings.ToUpper(strings.TrimSpace(row.valuationStatus))
		isolated := strings.TrimSpace(row.isolatedSymbol)
		identity := strings.TrimSpace(row.identityKey)
		id := "interest/" + identity
		if identity == "" {
			id = fmt.Sprintf("interest/row/%d", row.id)
		}
		at := row.accruedAt.UTC()
		if at.IsZero() {
			at = start
			blockers = append(blockers, fmt.Sprintf("interest accrued time missing id=%s", id))
		}
		known := asset == daily.Asset && (rawAsset == "" || rawAsset == daily.Asset) &&
			valuationAsset == daily.Asset && valuationStatus == "VALUED" && isolated == "" && identity != ""
		details := make([]string, 0, 5)
		if asset != daily.Asset {
			details = append(details, fmt.Sprintf("asset=%q", asset))
		}
		if rawAsset != "" && rawAsset != daily.Asset {
			details = append(details, fmt.Sprintf("raw_asset=%q", rawAsset))
		}
		if valuationAsset != daily.Asset {
			details = append(details, fmt.Sprintf("valuation_asset=%q", valuationAsset))
		}
		if valuationStatus != "VALUED" {
			details = append(details, fmt.Sprintf("valuation_status=%q", valuationStatus))
		}
		if isolated != "" {
			details = append(details, fmt.Sprintf("isolated_symbol=%q", isolated))
		}
		if identity == "" {
			details = append(details, "identity_key empty")
		}
		if len(details) > 0 {
			blockers = append(blockers, fmt.Sprintf("interest record unsupported id=%s %s", id, strings.Join(details, " ")))
		}
		amount := "0"
		if canonical, err := dailyLedgerCanonicalDecimal(row.valuationAmount); err != nil {
			known = false
			blockers = append(blockers, fmt.Sprintf("interest valuation amount invalid id=%s error=%s", id, err.Error()))
		} else if negated, err := dailyLedgerNegateDecimal(canonical); err != nil {
			known = false
			blockers = append(blockers, fmt.Sprintf("interest valuation amount invalid id=%s error=%s", id, err.Error()))
		} else {
			amount = negated
		}
		entryAsset := asset
		if !known {
			entryAsset = ""
		}
		entries = append(entries, daily.LedgerEntry{
			ID:     id,
			Kind:   daily.InterestAccrual,
			Market: daily.WalletMargin,
			Asset:  entryAsset,
			At:     at,
			Amount: amount,
			Known:  known,
		})
	}
	return entries, blockers
}

// loadDailyEquityTradeFeeCorrectionBlockers blocks a scope/day while any fee
// correction for that account scope is still pending. A correction can revise
// an existing fill without changing its row identity or MAX(id).
func loadDailyEquityTradeFeeCorrectionBlockers(ctx context.Context, q dailyEquityQuerier, exchange, accountScope string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT correction_id, market_type, symbol, status
		FROM trade_fee_corrections
		WHERE LOWER(TRIM(exchange)) = ? AND account_scope = ? AND LOWER(TRIM(status)) = 'pending'
		ORDER BY correction_id`, exchange, accountScope)
	if err != nil {
		return nil, fmt.Errorf("query daily equity trade fee corrections: %w", err)
	}
	defer rows.Close()
	blockers := make([]string, 0)
	for rows.Next() {
		var correctionID, marketType, symbol, status string
		if err := rows.Scan(&correctionID, &marketType, &symbol, &status); err != nil {
			return nil, fmt.Errorf("scan daily equity trade fee correction: %w", err)
		}
		blockers = append(blockers, fmt.Sprintf("trade fee correction pending id=%s market=%s symbol=%s",
			strings.TrimSpace(correctionID), strings.ToLower(strings.TrimSpace(marketType)), strings.TrimSpace(symbol)))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate daily equity trade fee corrections: %w", err)
	}
	return blockers, nil
}

func dailyEquityDiscoveredFuturesSymbols(evidence daily.Evidence, entries []daily.LedgerEntry) []string {
	seen := make(map[string]struct{})
	for _, symbol := range evidence.FuturesSymbols {
		symbol = strings.TrimSpace(symbol)
		if symbol != "" {
			seen[symbol] = struct{}{}
		}
	}
	for _, entry := range entries {
		if strings.TrimSpace(entry.Symbol) == "" {
			continue
		}
		if entry.Kind == daily.RealizedPnL || entry.Kind == daily.Funding {
			seen[entry.Symbol] = struct{}{}
		}
	}
	result := make([]string, 0, len(seen))
	for symbol := range seen {
		result = append(result, symbol)
	}
	sort.Strings(result)
	return result
}

func loadDailyEquityLedgerCoverage(ctx context.Context, q dailyEquityQuerier, exchange, accountScope string, start, end time.Time,
	evidence daily.Evidence, symbols []string, fills []dailyEquityFillRow, funding []dailyEquityFundingRow,
	interest []dailyEquityInterestRow, blockers []string) ([]daily.Coverage, []string, error) {
	coverage := make([]daily.Coverage, 0, len(symbols)*2+1)
	fillCounts := make(map[string]int)
	for _, row := range fills {
		if strings.EqualFold(strings.TrimSpace(row.marketType), "futures") {
			fillCounts[strings.TrimSpace(row.symbol)]++
		}
	}
	fundingCounts := make(map[string]int)
	for _, row := range funding {
		if strings.EqualFold(strings.TrimSpace(row.marketType), "futures") {
			fundingCounts[strings.TrimSpace(row.symbol)]++
		}
	}
	for _, symbol := range symbols {
		from, through, observed, present, err := queryDailyEquityExecutionCoverage(ctx, q, exchange, accountScope, symbol)
		if err != nil {
			return nil, nil, err
		}
		complete, reason := dailyEquityCoverageComplete(start, end, from, through, observed, present)
		if reason != "" {
			blockers = append(blockers, fmt.Sprintf("execution coverage %s symbol=%s window=%s..%s", reason, symbol, dailyEquityTimeText(from), dailyEquityTimeText(through)))
		}
		if complete {
			count := fillCounts[symbol]
			coverage = append(coverage, dailyEquityCoverageReceipt(daily.SourceExecution, exchange, accountScope, daily.WalletFutures, symbol, from, through, observed, count))
		}
		from, through, observed, present, err = queryDailyEquityFundingCoverage(ctx, q, exchange, accountScope, symbol)
		if err != nil {
			return nil, nil, err
		}
		complete, reason = dailyEquityCoverageComplete(start, end, from, through, observed, present)
		if reason != "" {
			blockers = append(blockers, fmt.Sprintf("funding coverage %s symbol=%s window=%s..%s", reason, symbol, dailyEquityTimeText(from), dailyEquityTimeText(through)))
		}
		if complete {
			count := fundingCounts[symbol]
			coverage = append(coverage, dailyEquityCoverageReceipt(daily.SourceFunding, exchange, accountScope, daily.WalletFutures, symbol, from, through, observed, count))
		}
	}
	from, through, observed, present, err := queryDailyEquityInterestCoverage(ctx, q, exchange, accountScope)
	if err != nil {
		return nil, nil, err
	}
	complete, reason := dailyEquityCoverageComplete(start, end, from, through, observed, present)
	if reason != "" {
		blockers = append(blockers, fmt.Sprintf("interest coverage %s asset=* window=%s..%s", reason, dailyEquityTimeText(from), dailyEquityTimeText(through)))
	}
	if complete {
		coverage = append(coverage, dailyEquityCoverageReceipt(daily.SourceInterest, exchange, accountScope, daily.WalletMargin, "*", from, through, observed, len(interest)))
	}
	return coverage, blockers, nil
}

func queryDailyEquityExecutionCoverage(ctx context.Context, q dailyEquityQuerier, exchange, accountScope, symbol string) (time.Time, time.Time, time.Time, bool, error) {
	var from, through, observed time.Time
	err := q.QueryRowContext(ctx, `SELECT covered_from, covered_through, updated_at
		FROM order_fill_sync_coverage
		WHERE scope_hash = ? AND LOWER(TRIM(exchange_name)) = ? AND LOWER(TRIM(market_type)) = ? AND UPPER(TRIM(symbol)) = ?
		ORDER BY updated_at DESC LIMIT 1`, orderFillScopeHash(accountScope), exchange, "futures", strings.ToUpper(strings.TrimSpace(symbol))).Scan(&from, &through, &observed)
	if err == sql.ErrNoRows {
		return time.Time{}, time.Time{}, time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, time.Time{}, time.Time{}, false, fmt.Errorf("query daily equity execution coverage symbol=%s: %w", symbol, err)
	}
	return from.UTC(), through.UTC(), observed.UTC(), true, nil
}

func queryDailyEquityFundingCoverage(ctx context.Context, q dailyEquityQuerier, exchange, accountScope, symbol string) (time.Time, time.Time, time.Time, bool, error) {
	var storedExchange, storedSymbol, storedMarket, storedScope string
	var from, through, observed time.Time
	err := q.QueryRowContext(ctx, `SELECT exchange, symbol, market_type, account_scope, covered_from, covered_through, updated_at
		FROM funding_income_sync_state
		WHERE scope_key = ?`, fundingIncomeCoverageKey(exchange, symbol, "futures", accountScope)).Scan(
		&storedExchange, &storedSymbol, &storedMarket, &storedScope, &from, &through, &observed)
	if err == sql.ErrNoRows {
		return time.Time{}, time.Time{}, time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, time.Time{}, time.Time{}, false, fmt.Errorf("query daily equity funding coverage symbol=%s: %w", symbol, err)
	}
	if !strings.EqualFold(strings.TrimSpace(storedExchange), exchange) || !strings.EqualFold(strings.TrimSpace(storedSymbol), symbol) ||
		!strings.EqualFold(strings.TrimSpace(storedMarket), "futures") || strings.TrimSpace(storedScope) != accountScope {
		return time.Time{}, time.Time{}, time.Time{}, false, fmt.Errorf("daily equity funding coverage identity mismatch symbol=%s", symbol)
	}
	return from.UTC(), through.UTC(), observed.UTC(), true, nil
}

func queryDailyEquityInterestCoverage(ctx context.Context, q dailyEquityQuerier, exchange, accountScope string) (time.Time, time.Time, time.Time, bool, error) {
	var storedExchange, storedScope, storedAsset string
	var from, through, observed time.Time
	err := q.QueryRowContext(ctx, `SELECT exchange, account_scope, asset, covered_from, covered_through, updated_at
		FROM margin_interest_sync_state
		WHERE scope_key = ?`, marginInterestCoverageKey(exchange, accountScope, "*")).Scan(
		&storedExchange, &storedScope, &storedAsset, &from, &through, &observed)
	if err == sql.ErrNoRows {
		return time.Time{}, time.Time{}, time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, time.Time{}, time.Time{}, false, fmt.Errorf("query daily equity interest coverage: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(storedExchange), exchange) || strings.TrimSpace(storedScope) != accountScope || strings.TrimSpace(storedAsset) != "*" {
		return time.Time{}, time.Time{}, time.Time{}, false, fmt.Errorf("daily equity interest coverage identity mismatch")
	}
	return from.UTC(), through.UTC(), observed.UTC(), true, nil
}

func dailyEquityCoverageComplete(start, end, from, through, observed time.Time, present bool) (bool, string) {
	if !present {
		return false, "missing"
	}
	if from.IsZero() || through.IsZero() {
		return false, "has no interval"
	}
	if from.After(start) || through.Before(end) {
		return false, "does not cover the day"
	}
	if observed.IsZero() || observed.Before(through) {
		return false, "observed before the covered through time"
	}
	return true, ""
}

func dailyEquityCoverageReceipt(source, exchange, accountScope, wallet, symbol string, from, through, observed time.Time, count int) daily.Coverage {
	return daily.Coverage{
		Source:        source,
		Wallet:        wallet,
		Symbol:        symbol,
		From:          from.UTC(),
		Through:       through.UTC(),
		ObservedAt:    observed.UTC(),
		Complete:      true,
		Pages:         1,
		ExpectedCount: count,
		ActualCount:   count,
		Digest:        dailyEquityCoverageDigest(source, exchange, accountScope, wallet, symbol, from, through, observed),
	}
}

func dailyEquityCoverageDigest(source, exchange, accountScope, wallet, symbol string, from, through, observed time.Time) string {
	material := strings.Join([]string{
		"daily-equity-ledger-coverage-v1",
		source,
		strings.ToLower(strings.TrimSpace(exchange)),
		strings.TrimSpace(accountScope),
		wallet,
		symbol,
		dailyEquityTimeText(from),
		dailyEquityTimeText(through),
		dailyEquityTimeText(observed),
	}, "\x00")
	sum := sha256.Sum256([]byte(material))
	return hex.EncodeToString(sum[:])
}

func dailyEquityTimeText(value time.Time) string {
	if value.IsZero() {
		return value.UTC().Format(time.RFC3339Nano)
	}
	return value.UTC().Format(time.RFC3339Nano)
}
