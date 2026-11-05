package storage

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"quantmesh/accounting/daily"
	"quantmesh/config"
)

const (
	dailyTestExchange = "binance"
	dailyTestScope    = "scope-daily"
	dailyTestDate     = "2026-03-15"
	dailyTestSymbol   = "BTCUSDT"
)

type dailyTestClock struct {
	start            time.Time
	end              time.Time
	now              time.Time
	fillAt           time.Time
	fundingAt        time.Time
	interestAt       time.Time
	coverageFrom     time.Time
	coverageThrough  time.Time
	coverageObserved time.Time
}

func newDailyTestClock(t *testing.T) dailyTestClock {
	t.Helper()
	start, end, err := daily.DayBounds(dailyTestDate)
	if err != nil {
		t.Fatalf("DayBounds: %v", err)
	}
	return dailyTestClock{
		start:            start,
		end:              end,
		now:              end.Add(time.Hour),
		fillAt:           start.Add(8 * time.Hour),
		fundingAt:        start.Add(9 * time.Hour),
		interestAt:       start.Add(10 * time.Hour),
		coverageFrom:     start.Add(-time.Hour),
		coverageThrough:  end,
		coverageObserved: end.Add(10 * time.Minute),
	}
}

func dailyTestQuery() daily.Query {
	return daily.Query{Exchange: dailyTestExchange, AccountScope: dailyTestScope, Date: dailyTestDate}
}

func openDailyEquitySQLite(t *testing.T, path string) *SQLStorage {
	t.Helper()
	st, err := NewSQLStorage(path)
	if err != nil {
		t.Fatalf("NewSQLStorage: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.MigrateDailyEquity(context.Background()); err != nil {
		t.Fatalf("MigrateDailyEquity: %v", err)
	}
	return st
}

func dailyTestSnapshot(id string, at time.Time, spot, margin, futures, marginInterest string, revision int64, positions []daily.Position) daily.Snapshot {
	return daily.Snapshot{
		SourceID:      id,
		At:            at,
		ObservedAt:    at.Add(10 * time.Minute),
		ScopeRevision: revision,
		Complete:      true,
		Balances: []daily.Balance{
			{Wallet: daily.WalletSpot, Asset: daily.Asset, Quantity: spot, Principal: "0", Interest: "0", Price: "1"},
			{Wallet: daily.WalletMargin, Asset: daily.Asset, Quantity: margin, Principal: "0", Interest: marginInterest, Price: "1"},
			{Wallet: daily.WalletFutures, Asset: daily.Asset, Quantity: futures, Principal: "0", Interest: "0", Price: "1"},
		},
		Positions: positions,
	}
}

func dailyTestCoverageReceipt(clock dailyTestClock, source, wallet, symbol string) daily.Coverage {
	return daily.Coverage{
		Source:        source,
		Wallet:        wallet,
		Symbol:        symbol,
		From:          clock.coverageFrom,
		Through:       clock.coverageThrough,
		ObservedAt:    clock.coverageObserved,
		Complete:      true,
		Pages:         1,
		ExpectedCount: 0,
		ActualCount:   0,
		Digest:        strings.Repeat("a", 64),
	}
}

// dailyTestEvidence builds a complete, internally consistent day: spot +200
// deposit, -50 withdrawal and -2 other fee; futures fill +30 PnL and -2 fee;
// +3 funding; 1 USDT margin interest. Opening equity 1000, closing 1178 and
// explained profit 28 leave a zero residual.
func dailyTestEvidence(clock dailyTestClock, scopeRevision int64) daily.Evidence {
	closingPosition := daily.Position{
		Symbol:      dailyTestSymbol,
		Quantity:    "0.01",
		EntryPrice:  "1000",
		MarkPrice:   "1000",
		Unrealized:  "0",
		PriceSource: "test",
		PriceAt:     clock.end,
	}
	return daily.Evidence{
		Scope: daily.Scope{
			Exchange:         dailyTestExchange,
			AccountScope:     dailyTestScope,
			Revision:         scopeRevision,
			Wallets:          []string{daily.WalletSpot, daily.WalletMargin, daily.WalletFutures},
			HistoryStart:     clock.start.Add(-30 * 24 * time.Hour),
			EffectiveFrom:    clock.start.Add(-30 * 24 * time.Hour),
			EffectiveThrough: clock.end.Add(30 * 24 * time.Hour),
		},
		Date:     dailyTestDate,
		Timezone: daily.Timezone,
		Method:   daily.MethodVersion,
		Opening:  dailyTestSnapshot("opening-snapshot", clock.start, "500", "400", "100", "0", scopeRevision, nil),
		Closing: dailyTestSnapshot("closing-snapshot", clock.end, "648", "400", "131", "1", scopeRevision,
			[]daily.Position{closingPosition}),
		Events: []daily.Event{
			{Source: daily.SourceCashflow, SourceID: "deposit-1", Wallet: daily.WalletSpot, Asset: daily.Asset, At: clock.start.Add(2 * time.Hour), Kind: daily.Deposit, Amount: "200"},
			{Source: daily.SourceCashflow, SourceID: "withdrawal-1", Wallet: daily.WalletSpot, Asset: daily.Asset, At: clock.start.Add(3 * time.Hour), Kind: daily.Withdrawal, Amount: "-50"},
			{Source: daily.SourceCashflow, SourceID: "other-fee-1", Wallet: daily.WalletSpot, Asset: daily.Asset, At: clock.start.Add(4 * time.Hour), Kind: daily.OtherFee, Amount: "-2"},
			{Source: daily.SourceExecution, SourceID: "trade-1-pnl", Wallet: daily.WalletFutures, Asset: daily.Asset, At: clock.fillAt, Kind: daily.RealizedPnL, Amount: "30", LedgerRef: "fill/futures/BTCUSDT/trade-1/pnl"},
			{Source: daily.SourceExecution, SourceID: "trade-1-fee", Wallet: daily.WalletFutures, Asset: daily.Asset, At: clock.fillAt, Kind: daily.TradingFee, Amount: "-2", LedgerRef: "fill/futures/BTCUSDT/trade-1/fee"},
			{Source: daily.SourceFunding, SourceID: "funding-key-1", Wallet: daily.WalletFutures, Asset: daily.Asset, At: clock.fundingAt, Kind: daily.Funding, Amount: "3", LedgerRef: "funding/funding-key-1"},
			{Source: daily.SourceInterest, SourceID: "interest-key-1", Wallet: daily.WalletMargin, Asset: daily.Asset, At: clock.interestAt, Kind: daily.InterestAccrual, Amount: "0", InterestDelta: "1", LedgerRef: "interest/interest-key-1"},
		},
		Coverage: []daily.Coverage{
			dailyTestCoverageReceipt(clock, daily.SourceCashflow, daily.WalletSpot, ""),
			dailyTestCoverageReceipt(clock, daily.SourceCashflow, daily.WalletMargin, ""),
			dailyTestCoverageReceipt(clock, daily.SourceCashflow, daily.WalletFutures, ""),
			dailyTestCoverageReceipt(clock, daily.SourcePositions, daily.WalletFutures, ""),
			dailyTestCoverageReceipt(clock, daily.SourceExecution, daily.WalletSpot, ""),
			dailyTestCoverageReceipt(clock, daily.SourceExecution, daily.WalletMargin, ""),
			dailyTestCoverageReceipt(clock, daily.SourceExecution, daily.WalletFutures, ""),
			dailyTestCoverageReceipt(clock, daily.SourceFunding, daily.WalletFutures, ""),
		},
		FuturesSymbols: []string{dailyTestSymbol},
	}
}

func insertDailyTestLedger(t *testing.T, st *SQLStorage, clock dailyTestClock) {
	t.Helper()
	mustDailyTestExec(t, st, `INSERT INTO order_fills
		(exchange, market_type, account_scope, account, bot_id, symbol, trade_id, order_id, side, price, quantity,
		 quote_quantity, commission, commission_asset, commission_quote, commission_quote_rate, commission_quote_known,
		 realized_pnl, realized_pnl_asset, trade_time)
		VALUES (?, 'futures', ?, 'acct', 'bot', ?, 'trade-1', 1, 'BUY', '1000', '0.01', '10', '2', 'USDT', '0', '0', 0, '30', 'USDT', ?)`,
		dailyTestExchange, dailyTestScope, dailyTestSymbol, clock.fillAt)
	mustDailyTestExec(t, st, `INSERT INTO funding_payments
		(exchange, symbol, account, market_type, account_scope, income_type, income, asset, transaction_id, trade_time, identity_key)
		VALUES (?, ?, 'acct', 'futures', ?, 'FUNDING_FEE', '3', 'USDT', 111, ?, 'funding-key-1')`,
		dailyTestExchange, dailyTestSymbol, dailyTestScope, clock.fundingAt)
	mustDailyTestExec(t, st, `INSERT INTO margin_interest_payments
		(exchange, account, account_scope, asset, raw_asset, principal, interest, interest_rate, interest_type,
		 isolated_symbol, transaction_id, accrued_at, identity_key, valuation_asset, valuation_rate, valuation_amount,
		 valuation_status, valuation_minute, valuation_source)
		VALUES (?, 'acct', ?, 'USDT', 'USDT', '100', '1', '0.01', 'BORROW', '', 222, ?, 'interest-key-1',
		 'USDT', '1', '1', 'VALUED', 0, 'test')`,
		dailyTestExchange, dailyTestScope, clock.interestAt)
	mustDailyTestExec(t, st, `INSERT INTO order_fill_sync_coverage
		(scope_hash, exchange_name, market_type, symbol, account_scope, covered_from, covered_through, updated_at)
		VALUES (?, ?, 'futures', ?, ?, ?, ?, ?)`,
		orderFillScopeHash(dailyTestScope), dailyTestExchange, dailyTestSymbol, dailyTestScope,
		clock.coverageFrom, clock.coverageThrough, clock.coverageObserved)
	mustDailyTestExec(t, st, `INSERT INTO funding_income_sync_state
		(scope_key, exchange, symbol, market_type, account_scope, covered_from, covered_through, updated_at)
		VALUES (?, ?, ?, 'futures', ?, ?, ?, ?)`,
		fundingIncomeCoverageKey(dailyTestExchange, dailyTestSymbol, "futures", dailyTestScope),
		dailyTestExchange, dailyTestSymbol, dailyTestScope, clock.coverageFrom, clock.coverageThrough, clock.coverageObserved)
	mustDailyTestExec(t, st, `INSERT INTO margin_interest_sync_state
		(scope_key, exchange, account_scope, asset, covered_from, covered_through, updated_at)
		VALUES (?, ?, ?, '*', ?, ?, ?)`,
		marginInterestCoverageKey(dailyTestExchange, dailyTestScope, "*"),
		dailyTestExchange, dailyTestScope, clock.coverageFrom, clock.coverageThrough, clock.coverageObserved)
}

func mustDailyTestExec(t *testing.T, st *SQLStorage, query string, args ...any) {
	t.Helper()
	if _, err := st.db.Exec(query, args...); err != nil {
		t.Fatalf("exec fixture (%s): %v", strings.SplitN(strings.TrimSpace(query), "\n", 2)[0], err)
	}
}

func dailyTestEntry(t *testing.T, entries []daily.LedgerEntry, id string) daily.LedgerEntry {
	t.Helper()
	for _, entry := range entries {
		if entry.ID == id {
			return entry
		}
	}
	t.Fatalf("ledger entry %s not found in %+v", id, entries)
	return daily.LedgerEntry{}
}

func dailyTestCoverage(t *testing.T, coverage []daily.Coverage, source, wallet, symbol string) daily.Coverage {
	t.Helper()
	for _, receipt := range coverage {
		if receipt.Source == source && receipt.Wallet == wallet && receipt.Symbol == symbol {
			return receipt
		}
	}
	t.Fatalf("coverage %s/%s/%s not found in %+v", source, wallet, symbol, coverage)
	return daily.Coverage{}
}

func dailyTestReason(reasons []string, want string) bool {
	for _, reason := range reasons {
		if reason == want {
			return true
		}
	}
	return false
}

func dailyTestBlockerContains(blockers []string, want string) bool {
	for _, blocker := range blockers {
		if strings.Contains(blocker, want) {
			return true
		}
	}
	return false
}

func TestDailyEquityMigrationLifecycleSQLite(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "daily.db")
	st, err := NewSQLStorage(path)
	if err != nil {
		t.Fatalf("NewSQLStorage: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	ready, err := st.DailyEquityMigrationStatus(ctx)
	if err != nil {
		t.Fatalf("DailyEquityMigrationStatus before migration: %v", err)
	}
	if ready {
		t.Fatal("daily equity schema reported ready before migration")
	}
	if err := st.MigrateDailyEquity(ctx); err != nil {
		t.Fatalf("first MigrateDailyEquity: %v", err)
	}
	if err := st.MigrateDailyEquity(ctx); err != nil {
		t.Fatalf("idempotent MigrateDailyEquity: %v", err)
	}
	if ready, err = st.DailyEquityMigrationStatus(ctx); err != nil || !ready {
		t.Fatalf("status after migration: ready=%v err=%v", ready, err)
	}
	if err := st.RollbackDailyEquityMigration(ctx); err != nil {
		t.Fatalf("RollbackDailyEquityMigration: %v", err)
	}
	if err := st.RollbackDailyEquityMigration(ctx); err != nil {
		t.Fatalf("idempotent RollbackDailyEquityMigration: %v", err)
	}
	if ready, err = st.DailyEquityMigrationStatus(ctx); err != nil || ready {
		t.Fatalf("status after rollback: ready=%v err=%v", ready, err)
	}
	if err := st.MigrateDailyEquity(ctx); err != nil {
		t.Fatalf("MigrateDailyEquity after rollback: %v", err)
	}
	if ready, err = st.DailyEquityMigrationStatus(ctx); err != nil || !ready {
		t.Fatalf("status after re-migration: ready=%v err=%v", ready, err)
	}
}

func TestDailyEquityMigrationRejectsInvalidSchemaSQLite(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "invalid.db")
	st, err := NewSQLStorage(path)
	if err != nil {
		t.Fatalf("NewSQLStorage: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	mustDailyTestExec(t, st, `CREATE TABLE daily_equity_current (id INTEGER PRIMARY KEY)`)
	ready, err := st.DailyEquityMigrationStatus(ctx)
	if err != nil {
		t.Fatalf("status with incompatible table: %v", err)
	}
	if ready {
		t.Fatal("partial schema was reported ready")
	}
	if err := st.MigrateDailyEquity(ctx); err == nil {
		t.Fatal("migration accepted an incompatible pre-existing daily_equity_current table")
	}
	if _, err := st.LoadDailyEquityInputs(ctx, dailyTestQuery()); err == nil {
		t.Fatal("LoadDailyEquityInputs succeeded against an incompatible schema")
	}
	if err := st.MigrateDailyEquity(ctx); err == nil {
		t.Fatal("second migration still accepted the incompatible schema")
	}

	if _, err := st.db.Exec(`DROP TABLE daily_equity_current`); err != nil {
		t.Fatalf("drop incompatible table: %v", err)
	}
	if err := st.MigrateDailyEquity(ctx); err != nil {
		t.Fatalf("migration after dropping incompatible table: %v", err)
	}
	if ready, err = st.DailyEquityMigrationStatus(ctx); err != nil || !ready {
		t.Fatalf("status after valid migration: ready=%v err=%v", ready, err)
	}
}

func TestDailyEquityMigrationRejectsUnsupportedBackendSQLite(t *testing.T) {
	ctx := context.Background()
	st, err := NewSQLStorage(filepath.Join(t.TempDir(), "backend.db"))
	if err != nil {
		t.Fatalf("NewSQLStorage: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	wrong := &SQLStorage{db: st.db, dbType: "postgres"}
	if _, err := wrong.DailyEquityMigrationStatus(ctx); err == nil {
		t.Fatal("status accepted an unsupported backend")
	}
	if err := wrong.MigrateDailyEquity(ctx); err == nil {
		t.Fatal("MigrateDailyEquity accepted an unsupported backend")
	}
	if err := wrong.RollbackDailyEquityMigration(ctx); err == nil {
		t.Fatal("RollbackDailyEquityMigration accepted an unsupported backend")
	}
	if _, err := dailyEquityMigrationFile("postgres", "up"); err == nil {
		t.Fatal("migration file whitelist accepted postgres")
	}
	if _, err := dailyEquityMigrationFile("sqlite", "sideways"); err == nil {
		t.Fatal("migration direction whitelist accepted sideways")
	}
}

func TestDailyEquityStorageRevisionAndRestart(t *testing.T) {
	ctx := context.Background()
	clock := newDailyTestClock(t)
	path := filepath.Join(t.TempDir(), "daily.db")
	st := openDailyEquitySQLite(t, path)

	revision, err := st.ImportDailyEquityEvidence(ctx, dailyTestEvidence(clock, 7), 0)
	if err != nil || revision != 1 {
		t.Fatalf("first import: revision=%d err=%v", revision, err)
	}

	// Identical payload replay is idempotent and does not increase the revision,
	// even when the caller supplies a stale expected revision.
	revision, err = st.ImportDailyEquityEvidence(ctx, dailyTestEvidence(clock, 7), 0)
	if err != nil || revision != 1 {
		t.Fatalf("identical replay: revision=%d err=%v", revision, err)
	}

	changed := dailyTestEvidence(clock, 7)
	changed.Events = append(changed.Events, daily.Event{
		Source: daily.SourceCashflow, SourceID: "deposit-2", Wallet: daily.WalletSpot,
		Asset: daily.Asset, At: clock.start.Add(5 * time.Hour), Kind: daily.Deposit, Amount: "1",
	})
	if _, err := st.ImportDailyEquityEvidence(ctx, changed, 0); !errors.Is(err, daily.ErrConflict) {
		t.Fatalf("stale expected revision: err=%v", err)
	}
	revision, err = st.ImportDailyEquityEvidence(ctx, changed, 1)
	if err != nil || revision != 2 {
		t.Fatalf("second import: revision=%d err=%v", revision, err)
	}

	var versionCount int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM daily_equity_evidence_versions
		WHERE exchange = ? AND account_scope = ? AND local_date = ?`,
		dailyTestExchange, dailyTestScope, dailyTestDate).Scan(&versionCount); err != nil {
		t.Fatalf("count evidence versions: %v", err)
	}
	if versionCount != 2 {
		t.Fatalf("evidence version count = %d, want 2", versionCount)
	}
	var firstPayload, secondPayload string
	if err := st.db.QueryRow(`SELECT payload_json FROM daily_equity_evidence_versions
		WHERE exchange = ? AND account_scope = ? AND local_date = ? AND revision = 1`,
		dailyTestExchange, dailyTestScope, dailyTestDate).Scan(&firstPayload); err != nil {
		t.Fatalf("read revision 1: %v", err)
	}
	if err := st.db.QueryRow(`SELECT payload_json FROM daily_equity_evidence_versions
		WHERE exchange = ? AND account_scope = ? AND local_date = ? AND revision = 2`,
		dailyTestExchange, dailyTestScope, dailyTestDate).Scan(&secondPayload); err != nil {
		t.Fatalf("read revision 2: %v", err)
	}
	if firstPayload == secondPayload {
		t.Fatal("evidence revisions were overwritten instead of preserved")
	}

	// A storage restart must reopen the same immutable revisions and the current
	// evidence pointer; migrations run again on the existing schema.
	if err := st.Close(); err != nil {
		t.Fatalf("close storage before restart: %v", err)
	}
	reopened, err := NewSQLStorage(path)
	if err != nil {
		t.Fatalf("reopen storage: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if err := reopened.MigrateDailyEquity(ctx); err != nil {
		t.Fatalf("re-run daily equity migration after restart: %v", err)
	}
	var persistedVersions int
	if err := reopened.db.QueryRow(`SELECT COUNT(*) FROM daily_equity_evidence_versions
		WHERE exchange = ? AND account_scope = ? AND local_date = ?`,
		dailyTestExchange, dailyTestScope, dailyTestDate).Scan(&persistedVersions); err != nil {
		t.Fatalf("count persisted versions after restart: %v", err)
	}
	if persistedVersions != 2 {
		t.Fatalf("persisted evidence versions after restart = %d, want 2", persistedVersions)
	}
	inputs, err := reopened.LoadDailyEquityInputs(ctx, dailyTestQuery())
	if err != nil {
		t.Fatalf("load inputs after restart: %v", err)
	}
	if inputs.EvidenceRevision != 2 {
		t.Fatalf("current evidence revision after restart = %d, want 2", inputs.EvidenceRevision)
	}
	if _, err := reopened.ImportDailyEquityEvidence(ctx, dailyTestEvidence(clock, 7), 0); !errors.Is(err, daily.ErrConflict) {
		// Revision 1 is no longer current; replaying its payload with a stale
		// expected revision must fail closed instead of rewinding the pointer.
		t.Fatalf("stale replayed evidence after restart error = %v, want ErrConflict", err)
	}
}

type dailyConflictCase struct {
	name         string
	first        []daily.Event
	second       []daily.Event
	wantRevision int64
	wantConflict bool
	wantReason   string
}

func TestDailyEquityStorageRejectsConflictingReimport(t *testing.T) {
	clock := newDailyTestClock(t)
	baseEvent := func(source, id string, kind daily.Kind, amount string) daily.Event {
		return daily.Event{
			Source: source, SourceID: id, Wallet: daily.WalletFutures, Asset: daily.Asset,
			At: clock.fillAt, Kind: kind, Amount: amount,
		}
	}
	cases := []dailyConflictCase{
		{
			name:         "changed amount",
			first:        []daily.Event{baseEvent(daily.SourceExecution, "fill-1", daily.RealizedPnL, "30")},
			second:       []daily.Event{baseEvent(daily.SourceExecution, "fill-1", daily.RealizedPnL, "31")},
			wantConflict: true, wantReason: "event_payload_changed",
		},
		{
			name:         "removed event",
			first:        []daily.Event{baseEvent(daily.SourceExecution, "fill-1", daily.RealizedPnL, "30")},
			second:       nil,
			wantConflict: true, wantReason: "event_removed",
		},
		{
			name:         "kind downgrade",
			first:        []daily.Event{baseEvent(daily.SourceExecution, "fill-1", daily.RealizedPnL, "30")},
			second:       []daily.Event{baseEvent(daily.SourceExecution, "fill-1", daily.Funding, "30")},
			wantConflict: true, wantReason: "event_kind_changed",
		},
		{
			name:         "unknown to known same amount",
			first:        []daily.Event{baseEvent(daily.SourceExecution, "fill-1", daily.Unknown, "30")},
			second:       []daily.Event{baseEvent(daily.SourceExecution, "fill-1", daily.RealizedPnL, "30")},
			wantRevision: 2,
		},
		{
			name:         "unknown to known changed amount",
			first:        []daily.Event{baseEvent(daily.SourceExecution, "fill-1", daily.Unknown, "30")},
			second:       []daily.Event{baseEvent(daily.SourceExecution, "fill-1", daily.RealizedPnL, "31")},
			wantConflict: true, wantReason: "event_payload_changed",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			st := openDailyEquitySQLite(t, filepath.Join(t.TempDir(), "daily.db"))
			first := dailyMinimalEvidence(7, testCase.first)
			if revision, err := st.ImportDailyEquityEvidence(ctx, first, 0); err != nil || revision != 1 {
				t.Fatalf("first import: revision=%d err=%v", revision, err)
			}
			second := dailyMinimalEvidence(7, testCase.second)
			revision, err := st.ImportDailyEquityEvidence(ctx, second, 1)
			if testCase.wantConflict {
				if !errors.Is(err, daily.ErrConflict) {
					t.Fatalf("second import error = %v, want ErrConflict", err)
				}
				var conflicted bool
				if err := st.db.QueryRow(`SELECT conflicted FROM daily_equity_current
					WHERE exchange = ? AND account_scope = ? AND local_date = ?`,
					dailyTestExchange, dailyTestScope, dailyTestDate).Scan(&conflicted); err != nil {
					t.Fatalf("read conflicted flag: %v", err)
				}
				if !conflicted {
					t.Fatal("conflict did not mark the day as conflicted")
				}
				var reason string
				if err := st.db.QueryRow(`SELECT reason FROM daily_equity_conflicts
					WHERE exchange = ? AND account_scope = ? AND local_date = ?
					ORDER BY id DESC LIMIT 1`,
					dailyTestExchange, dailyTestScope, dailyTestDate).Scan(&reason); err != nil {
					t.Fatalf("read conflict reason: %v", err)
				}
				if reason != testCase.wantReason {
					t.Fatalf("conflict reason = %q, want %q", reason, testCase.wantReason)
				}
				var versions int
				if err := st.db.QueryRow(`SELECT COUNT(*) FROM daily_equity_evidence_versions
					WHERE exchange = ? AND account_scope = ? AND local_date = ?`,
					dailyTestExchange, dailyTestScope, dailyTestDate).Scan(&versions); err != nil {
					t.Fatalf("count versions after conflict: %v", err)
				}
				if versions != 1 {
					t.Fatalf("conflicting import created a new version: %d", versions)
				}
				if _, err := st.CommitDailyEquityResult(ctx, dailyTestQuery(), strings.Repeat("b", 64), daily.Result{}); !errors.Is(err, daily.ErrConflict) {
					t.Fatalf("commit on conflicted day error = %v, want ErrConflict", err)
				}
				return
			}
			if err != nil || revision != testCase.wantRevision {
				t.Fatalf("reclassification import: revision=%d err=%v, want revision %d", revision, err, testCase.wantRevision)
			}
			var conflicted bool
			if err := st.db.QueryRow(`SELECT conflicted FROM daily_equity_current
				WHERE exchange = ? AND account_scope = ? AND local_date = ?`,
				dailyTestExchange, dailyTestScope, dailyTestDate).Scan(&conflicted); err != nil {
				t.Fatalf("read conflicted flag: %v", err)
			}
			if conflicted {
				t.Fatal("allowed reclassification marked the day conflicted")
			}
		})
	}
}

func TestDailyEquityImportPersistsPayloadConflictSQLite(t *testing.T) {
	ctx := context.Background()
	clock := newDailyTestClock(t)
	st := openDailyEquitySQLite(t, filepath.Join(t.TempDir(), "daily.db"))
	event := func(amount string) daily.Event {
		return daily.Event{
			Source: daily.SourceExecution, SourceID: "fill-1", Wallet: daily.WalletFutures,
			Asset: daily.Asset, At: clock.fillAt, Kind: daily.RealizedPnL, Amount: amount,
		}
	}
	evidence := dailyMinimalEvidence(7, []daily.Event{event("30"), event("31")})
	if _, err := st.ImportDailyEquityEvidence(ctx, evidence, 0); !errors.Is(err, daily.ErrConflict) {
		t.Fatalf("duplicate source identity import error = %v, want ErrConflict", err)
	}
	var reason string
	if err := st.db.QueryRow(`SELECT reason FROM daily_equity_conflicts
		WHERE exchange = ? AND account_scope = ? AND local_date = ?`,
		dailyTestExchange, dailyTestScope, dailyTestDate).Scan(&reason); err != nil {
		t.Fatalf("read payload conflict: %v", err)
	}
	if reason != "evidence_payload_conflict" {
		t.Fatalf("payload conflict reason = %q", reason)
	}
	var conflicted bool
	if err := st.db.QueryRow(`SELECT conflicted FROM daily_equity_current
		WHERE exchange = ? AND account_scope = ? AND local_date = ?`,
		dailyTestExchange, dailyTestScope, dailyTestDate).Scan(&conflicted); err != nil {
		t.Fatalf("read conflicted flag: %v", err)
	}
	if !conflicted {
		t.Fatal("payload conflict did not mark the day conflicted")
	}
	if _, err := st.LoadDailyEquityInputs(ctx, dailyTestQuery()); !errors.Is(err, daily.ErrNotFound) {
		t.Fatalf("LoadDailyEquityInputs after payload conflict error = %v, want ErrNotFound", err)
	}
}

func dailyMinimalEvidence(scopeRevision int64, events []daily.Event) daily.Evidence {
	return daily.Evidence{
		Scope: daily.Scope{
			Exchange:     dailyTestExchange,
			AccountScope: dailyTestScope,
			Revision:     scopeRevision,
			Wallets:      []string{daily.WalletSpot, daily.WalletMargin, daily.WalletFutures},
		},
		Date:     dailyTestDate,
		Timezone: daily.Timezone,
		Method:   daily.MethodVersion,
		Events:   events,
	}
}

func TestDailyEquityProjectionSQLite(t *testing.T) {
	ctx := context.Background()
	clock := newDailyTestClock(t)
	path := filepath.Join(t.TempDir(), "daily.db")
	st := openDailyEquitySQLite(t, path)
	evidence := dailyTestEvidence(clock, 7)
	if revision, err := st.ImportDailyEquityEvidence(ctx, evidence, 0); err != nil || revision != 1 {
		t.Fatalf("import evidence: revision=%d err=%v", revision, err)
	}
	insertDailyTestLedger(t, st, clock)

	inputs, err := st.LoadDailyEquityInputs(ctx, dailyTestQuery())
	if err != nil {
		t.Fatalf("LoadDailyEquityInputs: %v", err)
	}
	if len(inputs.Blockers) != 0 {
		t.Fatalf("unexpected blockers: %v", inputs.Blockers)
	}
	if inputs.EvidenceRevision != 1 {
		t.Fatalf("evidence revision = %d, want 1", inputs.EvidenceRevision)
	}
	if len(inputs.Entries) != 4 {
		t.Fatalf("ledger entries = %d (%+v), want 4", len(inputs.Entries), inputs.Entries)
	}
	pnl := dailyTestEntry(t, inputs.Entries, "fill/futures/BTCUSDT/trade-1/pnl")
	if pnl.Kind != daily.RealizedPnL || pnl.Market != "futures" || pnl.Symbol != dailyTestSymbol ||
		pnl.Asset != daily.Asset || !pnl.Known || pnl.Amount != "30" || pnl.Quantity != "0.01" || pnl.Side != "BUY" {
		t.Fatalf("unexpected pnl projection: %+v", pnl)
	}
	if !pnl.At.Equal(clock.fillAt) {
		t.Fatalf("pnl time = %s, want %s", pnl.At, clock.fillAt)
	}
	fee := dailyTestEntry(t, inputs.Entries, "fill/futures/BTCUSDT/trade-1/fee")
	if fee.Kind != daily.TradingFee || !fee.Known || fee.Amount != "-2" || fee.Asset != daily.Asset || fee.Symbol != dailyTestSymbol {
		t.Fatalf("unexpected fee projection: %+v", fee)
	}
	funding := dailyTestEntry(t, inputs.Entries, "funding/funding-key-1")
	if funding.Kind != daily.Funding || !funding.Known || funding.Amount != "3" || funding.Symbol != dailyTestSymbol {
		t.Fatalf("unexpected funding projection: %+v", funding)
	}
	interest := dailyTestEntry(t, inputs.Entries, "interest/interest-key-1")
	if interest.Kind != daily.InterestAccrual || !interest.Known || interest.Amount != "-1" ||
		interest.Market != daily.WalletMargin || interest.Asset != daily.Asset {
		t.Fatalf("unexpected interest projection: %+v", interest)
	}
	if len(inputs.LedgerCoverage) != 3 {
		t.Fatalf("ledger coverage receipts = %d (%+v), want 3", len(inputs.LedgerCoverage), inputs.LedgerCoverage)
	}
	executionCoverage := dailyTestCoverage(t, inputs.LedgerCoverage, daily.SourceExecution, daily.WalletFutures, dailyTestSymbol)
	if !executionCoverage.Complete || executionCoverage.Pages != 1 ||
		executionCoverage.ExpectedCount != 1 || executionCoverage.ActualCount != 1 ||
		len(executionCoverage.Digest) != 64 || !executionCoverage.ObservedAt.Equal(clock.coverageObserved) {
		t.Fatalf("unexpected execution coverage: %+v", executionCoverage)
	}
	fundingCoverage := dailyTestCoverage(t, inputs.LedgerCoverage, daily.SourceFunding, daily.WalletFutures, dailyTestSymbol)
	if !fundingCoverage.Complete || fundingCoverage.ExpectedCount != 1 || fundingCoverage.ActualCount != 1 {
		t.Fatalf("unexpected funding coverage: %+v", fundingCoverage)
	}
	interestCoverage := dailyTestCoverage(t, inputs.LedgerCoverage, daily.SourceInterest, daily.WalletMargin, "*")
	if !interestCoverage.Complete || interestCoverage.ExpectedCount != 1 || interestCoverage.ActualCount != 1 {
		t.Fatalf("unexpected interest coverage: %+v", interestCoverage)
	}

	result, err := daily.Evaluate(inputs, clock.now)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !result.EquityReconciliationComplete {
		t.Fatalf("golden day is not reconciled: reasons=%v blockers=%v", result.Reasons, inputs.Blockers)
	}
	if result.Amounts.Residual != "0" || result.Amounts.ClosingEquity != "1178" || result.Amounts.NetExternalCapital != "150" {
		t.Fatalf("unexpected amounts: %+v", result.Amounts)
	}
	committed, err := st.CommitDailyEquityResult(ctx, dailyTestQuery(), inputs.Digest, result)
	if err != nil {
		t.Fatalf("CommitDailyEquityResult: %v", err)
	}
	if committed.Revision != 7 || committed.EvidenceRevision != 1 || committed.InputDigest != inputs.Digest || !committed.EquityReconciliationComplete {
		t.Fatalf("unexpected committed result: %+v", committed)
	}

	replayed, err := st.CommitDailyEquityResult(ctx, dailyTestQuery(), inputs.Digest, result)
	if err != nil {
		t.Fatalf("idempotent commit: %v", err)
	}
	if replayed.Revision != committed.Revision {
		t.Fatalf("idempotent commit revision = %d, want %d", replayed.Revision, committed.Revision)
	}
	var reportVersions int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM daily_equity_report_versions
		WHERE exchange = ? AND account_scope = ? AND local_date = ?`,
		dailyTestExchange, dailyTestScope, dailyTestDate).Scan(&reportVersions); err != nil {
		t.Fatalf("count report versions: %v", err)
	}
	if reportVersions != 1 {
		t.Fatalf("idempotent commit created %d report versions, want 1", reportVersions)
	}

	service := daily.NewService(st)
	fresh, err := service.Read(ctx, dailyTestQuery(), clock.now)
	if err != nil {
		t.Fatalf("Service.Read: %v", err)
	}
	if !fresh.EquityReconciliationComplete || fresh.Revision != 7 {
		t.Fatalf("unexpected fresh read: %+v", fresh)
	}

	// A real ledger correction can keep MAX(id) unchanged but must still change
	// the full input digest and invalidate the persisted true.
	if _, err := st.db.Exec(`UPDATE order_fills SET commission = '2.5' WHERE trade_id = 'trade-1'`); err != nil {
		t.Fatalf("mutate fill commission: %v", err)
	}
	inputsAfterCorrection, err := st.LoadDailyEquityInputs(ctx, dailyTestQuery())
	if err != nil {
		t.Fatalf("LoadDailyEquityInputs after correction: %v", err)
	}
	if inputsAfterCorrection.Digest == inputs.Digest {
		t.Fatal("ledger correction did not change the input digest")
	}
	if fee := dailyTestEntry(t, inputsAfterCorrection.Entries, "fill/futures/BTCUSDT/trade-1/fee"); fee.Amount != "-2.5" {
		t.Fatalf("corrected fee amount = %q, want -2.5", fee.Amount)
	}
	if second, err := st.LoadDailyEquityInputs(ctx, dailyTestQuery()); err != nil || second.Digest != inputsAfterCorrection.Digest {
		t.Fatalf("digest is not stable across loads: err=%v", err)
	}
	stale, err := service.Read(ctx, dailyTestQuery(), clock.now)
	if err != nil {
		t.Fatalf("Service.Read after correction: %v", err)
	}
	if stale.EquityReconciliationComplete || stale.Revision != 7 || !dailyTestReason(stale.Reasons, "source_changed") {
		t.Fatalf("stale read = %+v, want revision 7 with source_changed", stale)
	}
	if _, err := st.CommitDailyEquityResult(ctx, dailyTestQuery(), inputs.Digest, result); !errors.Is(err, daily.ErrConflict) {
		t.Fatalf("commit with stale digest error = %v, want ErrConflict", err)
	}

	correctedResult, err := daily.Evaluate(inputsAfterCorrection, clock.now)
	if err != nil {
		t.Fatalf("Evaluate corrected inputs: %v", err)
	}
	if correctedResult.EquityReconciliationComplete {
		t.Fatal("corrected inputs unexpectedly reconciled without matching evidence events")
	}
	correctedCommit, err := st.CommitDailyEquityResult(ctx, dailyTestQuery(), inputsAfterCorrection.Digest, correctedResult)
	if err != nil {
		t.Fatalf("commit corrected result: %v", err)
	}
	if correctedCommit.Revision != 7 || correctedCommit.EvidenceRevision != 1 {
		t.Fatalf("corrected report revision = %d evidence=%d, want 7/1", correctedCommit.Revision, correctedCommit.EvidenceRevision)
	}
	var correctedReportRevision int64
	if err := st.db.QueryRow(`SELECT report_revision FROM daily_equity_current
		WHERE exchange = ? AND account_scope = ? AND local_date = ?`,
		dailyTestExchange, dailyTestScope, dailyTestDate).Scan(&correctedReportRevision); err != nil {
		t.Fatalf("read corrected report revision: %v", err)
	}
	if correctedReportRevision != 2 {
		t.Fatalf("current report revision = %d, want 2", correctedReportRevision)
	}
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM daily_equity_report_versions
		WHERE exchange = ? AND account_scope = ? AND local_date = ?`,
		dailyTestExchange, dailyTestScope, dailyTestDate).Scan(&reportVersions); err != nil {
		t.Fatalf("count report versions after correction: %v", err)
	}
	if reportVersions != 2 {
		t.Fatalf("corrected commit report versions = %d, want 2", reportVersions)
	}

	// Restart: everything must be durable and still digest-verifiable.
	if err := st.Close(); err != nil {
		t.Fatalf("close storage: %v", err)
	}
	reopened := openDailyEquitySQLite(t, path)
	persisted, err := reopened.LoadDailyEquityResult(ctx, dailyTestQuery())
	if err != nil || persisted.Revision != 7 || persisted.EquityReconciliationComplete {
		t.Fatalf("persisted result after restart: result=%+v err=%v", persisted, err)
	}
	reopenedInputs, err := reopened.LoadDailyEquityInputs(ctx, dailyTestQuery())
	if err != nil || reopenedInputs.Digest != inputsAfterCorrection.Digest || reopenedInputs.EvidenceRevision != 1 {
		t.Fatalf("inputs after restart: digest=%s err=%v", reopenedInputs.Digest, err)
	}
	reopenedRead, err := daily.NewService(reopened).Read(ctx, dailyTestQuery(), clock.now)
	if err != nil || reopenedRead.Revision != 7 || reopenedRead.EquityReconciliationComplete {
		t.Fatalf("read after restart: result=%+v err=%v", reopenedRead, err)
	}
}

func TestDailyEquityCommitRejectsForgedCompleteSQLite(t *testing.T) {
	ctx := context.Background()
	clock := newDailyTestClock(t)
	st := openDailyEquitySQLite(t, filepath.Join(t.TempDir(), "daily.db"))
	if revision, err := st.ImportDailyEquityEvidence(ctx, dailyTestEvidence(clock, 7), 0); err != nil || revision != 1 {
		t.Fatalf("import evidence: revision=%d err=%v", revision, err)
	}
	insertDailyTestLedger(t, st, clock)
	inputs, err := st.LoadDailyEquityInputs(ctx, dailyTestQuery())
	if err != nil {
		t.Fatalf("load inputs: %v", err)
	}
	result, err := daily.Evaluate(inputs, clock.now)
	if err != nil || !result.EquityReconciliationComplete {
		t.Fatalf("evaluate: complete=%v err=%v reasons=%v", result.EquityReconciliationComplete, err, result.Reasons)
	}
	forged := result
	forged.Amounts.Residual = "0.5"
	if _, err := st.CommitDailyEquityResult(ctx, dailyTestQuery(), inputs.Digest, forged); !errors.Is(err, daily.ErrInvalid) {
		t.Fatalf("forged reconciled result error = %v, want ErrInvalid", err)
	}
	forged = result
	forged.InputDigest = strings.Repeat("c", 64)
	if _, err := st.CommitDailyEquityResult(ctx, dailyTestQuery(), inputs.Digest, forged); !errors.Is(err, daily.ErrInvalid) {
		t.Fatalf("forged result digest error = %v, want ErrInvalid", err)
	}
	if _, err := st.CommitDailyEquityResult(ctx, dailyTestQuery(), inputs.Digest, result); err != nil {
		t.Fatalf("genuine commit after forged attempts: %v", err)
	}
}

func TestDailyEquityScopeRevisionChangesDigestSQLite(t *testing.T) {
	ctx := context.Background()
	clock := newDailyTestClock(t)
	st := openDailyEquitySQLite(t, filepath.Join(t.TempDir(), "daily.db"))
	if revision, err := st.ImportDailyEquityEvidence(ctx, dailyTestEvidence(clock, 7), 0); err != nil || revision != 1 {
		t.Fatalf("import scope revision 7: revision=%d err=%v", revision, err)
	}
	insertDailyTestLedger(t, st, clock)
	inputs7, err := st.LoadDailyEquityInputs(ctx, dailyTestQuery())
	if err != nil {
		t.Fatalf("load inputs revision 7: %v", err)
	}
	result7, err := daily.Evaluate(inputs7, clock.now)
	if err != nil || !result7.EquityReconciliationComplete {
		t.Fatalf("evaluate revision 7: complete=%v err=%v reasons=%v", result7.EquityReconciliationComplete, err, result7.Reasons)
	}
	if _, err := st.CommitDailyEquityResult(ctx, dailyTestQuery(), inputs7.Digest, result7); err != nil {
		t.Fatalf("commit revision 7: %v", err)
	}

	if revision, err := st.ImportDailyEquityEvidence(ctx, dailyTestEvidence(clock, 8), 1); err != nil || revision != 2 {
		t.Fatalf("import scope revision 8: revision=%d err=%v", revision, err)
	}
	inputs8, err := st.LoadDailyEquityInputs(ctx, dailyTestQuery())
	if err != nil {
		t.Fatalf("load inputs revision 8: %v", err)
	}
	if inputs8.Digest == inputs7.Digest {
		t.Fatal("scope revision change did not change the input digest")
	}
	stale, err := daily.NewService(st).Read(ctx, dailyTestQuery(), clock.now)
	if err != nil || stale.EquityReconciliationComplete || !dailyTestReason(stale.Reasons, "source_changed") {
		t.Fatalf("read after scope revision change: result=%+v err=%v", stale, err)
	}
	result8, err := daily.Evaluate(inputs8, clock.now)
	if err != nil || !result8.EquityReconciliationComplete {
		t.Fatalf("evaluate revision 8: complete=%v err=%v reasons=%v", result8.EquityReconciliationComplete, err, result8.Reasons)
	}
	committed8, err := st.CommitDailyEquityResult(ctx, dailyTestQuery(), inputs8.Digest, result8)
	if err != nil {
		t.Fatalf("commit revision 8: %v", err)
	}
	if committed8.Revision != 8 || committed8.EvidenceRevision != 2 || !committed8.EquityReconciliationComplete {
		t.Fatalf("scope revision 8 report: %+v", committed8)
	}
}

func TestDailyEquityConflictInvalidatesPublishedResultSQLite(t *testing.T) {
	ctx := context.Background()
	clock := newDailyTestClock(t)
	st := openDailyEquitySQLite(t, filepath.Join(t.TempDir(), "daily.db"))
	evidence := dailyTestEvidence(clock, 7)
	if revision, err := st.ImportDailyEquityEvidence(ctx, evidence, 0); err != nil || revision != 1 {
		t.Fatalf("import evidence: revision=%d err=%v", revision, err)
	}
	insertDailyTestLedger(t, st, clock)
	inputs, err := st.LoadDailyEquityInputs(ctx, dailyTestQuery())
	if err != nil {
		t.Fatalf("load inputs: %v", err)
	}
	result, err := daily.Evaluate(inputs, clock.now)
	if err != nil || !result.EquityReconciliationComplete {
		t.Fatalf("evaluate: complete=%v err=%v reasons=%v", result.EquityReconciliationComplete, err, result.Reasons)
	}
	if _, err := st.CommitDailyEquityResult(ctx, dailyTestQuery(), inputs.Digest, result); err != nil {
		t.Fatalf("commit: %v", err)
	}

	conflicting := dailyTestEvidence(clock, 7)
	for index := range conflicting.Events {
		if conflicting.Events[index].LedgerRef == "fill/futures/BTCUSDT/trade-1/pnl" {
			conflicting.Events[index].Amount = "31"
		}
	}
	if _, err := st.ImportDailyEquityEvidence(ctx, conflicting, 1); !errors.Is(err, daily.ErrConflict) {
		t.Fatalf("conflicting import error = %v, want ErrConflict", err)
	}
	persisted, err := st.LoadDailyEquityResult(ctx, dailyTestQuery())
	if err != nil {
		t.Fatalf("LoadDailyEquityResult after conflict: %v", err)
	}
	if persisted.EquityReconciliationComplete || !dailyTestReason(persisted.Reasons, "source_changed") {
		t.Fatalf("conflicted persisted result = %+v, want incomplete source_changed", persisted)
	}
	read, err := daily.NewService(st).Read(ctx, dailyTestQuery(), clock.now)
	if err != nil || read.EquityReconciliationComplete || !dailyTestReason(read.Reasons, "source_changed") {
		t.Fatalf("read after conflict: result=%+v err=%v", read, err)
	}
	conflictedInputs, err := st.LoadDailyEquityInputs(ctx, dailyTestQuery())
	if err != nil {
		t.Fatalf("load conflicted inputs: %v", err)
	}
	if !dailyTestBlockerContains(conflictedInputs.Blockers, "source conflict is persisted") {
		t.Fatalf("conflicted inputs missing blocker: %v", conflictedInputs.Blockers)
	}
	if _, err := st.CommitDailyEquityResult(ctx, dailyTestQuery(), inputs.Digest, result); !errors.Is(err, daily.ErrConflict) {
		t.Fatalf("commit on conflicted day error = %v, want ErrConflict", err)
	}
}

func TestDailyEquityProjectionBlockersSQLite(t *testing.T) {
	ctx := context.Background()
	clock := newDailyTestClock(t)
	st := openDailyEquitySQLite(t, filepath.Join(t.TempDir(), "daily.db"))
	evidence := dailyMinimalEvidence(7, nil)
	evidence.FuturesSymbols = []string{dailyTestSymbol}
	if revision, err := st.ImportDailyEquityEvidence(ctx, evidence, 0); err != nil || revision != 1 {
		t.Fatalf("import evidence: revision=%d err=%v", revision, err)
	}
	mustDailyTestExec(t, st, `INSERT INTO order_fills
		(exchange, market_type, account_scope, symbol, trade_id, order_id, side, price, quantity, commission,
		 commission_asset, realized_pnl, realized_pnl_asset, trade_time)
		VALUES (?, 'spot', ?, 'ETHUSDT', 'spot-1', 1, 'BUY', '100', '1', '0.1', 'USDT', '5', 'USDT', ?)`,
		dailyTestExchange, dailyTestScope, clock.start.Add(time.Hour))
	mustDailyTestExec(t, st, `INSERT INTO order_fills
		(exchange, market_type, account_scope, symbol, trade_id, order_id, side, price, quantity, commission,
		 commission_asset, realized_pnl, realized_pnl_asset, trade_time)
		VALUES (?, 'futures', ?, ?, 'f2', 2, 'BUY', '100', '1', '0.2', 'USDT', NULL, '', ?)`,
		dailyTestExchange, dailyTestScope, dailyTestSymbol, clock.start.Add(2*time.Hour))
	mustDailyTestExec(t, st, `INSERT INTO funding_payments
		(exchange, symbol, account, market_type, account_scope, income_type, income, asset, transaction_id, trade_time, identity_key)
		VALUES (?, ?, 'acct', 'futures', ?, 'FUNDING_FEE', '1', 'BUSD', 42, ?, 'bad-funding')`,
		dailyTestExchange, dailyTestSymbol, dailyTestScope, clock.fundingAt)
	mustDailyTestExec(t, st, `INSERT INTO margin_interest_payments
		(exchange, account, account_scope, asset, raw_asset, principal, interest, interest_rate, interest_type,
		 isolated_symbol, transaction_id, accrued_at, identity_key, valuation_asset, valuation_rate, valuation_amount,
		 valuation_status, valuation_minute, valuation_source)
		VALUES (?, 'acct', ?, 'USDT', 'USDT', '100', '1', '0.01', 'BORROW', '', 333, ?, 'bad-interest',
		 'USDT', '0', '0', 'UNVALUED', 0, 'test')`,
		dailyTestExchange, dailyTestScope, clock.interestAt)
	mustDailyTestExec(t, st, `INSERT INTO trade_fee_corrections
		(correction_id, bot_id, exchange, market_type, symbol, account_scope, account, order_id, client_order_id,
		 leg, side, fee, fee_asset, base_fee_qty, executed_qty, reason, evidence_note, status, created_at)
		VALUES ('corr-1', 'bot', ?, 'futures', ?, ?, 'acct', 1, '', 'entry', 'BUY', '0.1', 'USDT', '0', '0',
		 'fee mismatch', 'test', 'pending', ?)`,
		dailyTestExchange, dailyTestSymbol, dailyTestScope, clock.fillAt)

	inputs, err := st.LoadDailyEquityInputs(ctx, dailyTestQuery())
	if err != nil {
		t.Fatalf("LoadDailyEquityInputs: %v", err)
	}
	if len(inputs.Entries) != 6 {
		t.Fatalf("entries = %d (%+v), want 6", len(inputs.Entries), inputs.Entries)
	}
	spotPnL := dailyTestEntry(t, inputs.Entries, "fill/spot/ETHUSDT/spot-1/pnl")
	if spotPnL.Known || spotPnL.Symbol != "" || spotPnL.Asset != "" || spotPnL.Amount != "0" {
		t.Fatalf("non-futures pnl should be unknown and excluded from futures symbols: %+v", spotPnL)
	}
	spotFee := dailyTestEntry(t, inputs.Entries, "fill/spot/ETHUSDT/spot-1/fee")
	if spotFee.Known || spotFee.Asset != "" {
		t.Fatalf("non-futures fee should be unknown: %+v", spotFee)
	}
	missingPnL := dailyTestEntry(t, inputs.Entries, "fill/futures/BTCUSDT/f2/pnl")
	if missingPnL.Known || missingPnL.Asset != "" || missingPnL.Symbol != dailyTestSymbol {
		t.Fatalf("fill without realized pnl should be unknown: %+v", missingPnL)
	}
	knownFee := dailyTestEntry(t, inputs.Entries, "fill/futures/BTCUSDT/f2/fee")
	if !knownFee.Known || knownFee.Amount != "-0.2" {
		t.Fatalf("fee of a futures fill without pnl should stay known: %+v", knownFee)
	}
	badFunding := dailyTestEntry(t, inputs.Entries, "funding/bad-funding")
	if badFunding.Known || badFunding.Asset != "" {
		t.Fatalf("non-USDT funding should be unknown: %+v", badFunding)
	}
	badInterest := dailyTestEntry(t, inputs.Entries, "interest/bad-interest")
	if badInterest.Known || badInterest.Asset != "" || badInterest.Amount != "0" {
		t.Fatalf("unvalued interest should be unknown: %+v", badInterest)
	}
	for _, want := range []string{
		"fill market unsupported",
		"fill realized pnl missing",
		"funding record unsupported",
		"interest record unsupported",
		"trade fee correction pending",
		"execution coverage missing",
		"funding coverage missing",
		"interest coverage missing",
	} {
		if !dailyTestBlockerContains(inputs.Blockers, want) {
			t.Fatalf("blockers missing %q: %v", want, inputs.Blockers)
		}
	}
	if len(inputs.LedgerCoverage) != 0 {
		t.Fatalf("incomplete coverage emitted receipts: %+v", inputs.LedgerCoverage)
	}
}

func TestDailyEquityDuplicateFundingTransactionSQLite(t *testing.T) {
	ctx := context.Background()
	clock := newDailyTestClock(t)
	st := openDailyEquitySQLite(t, filepath.Join(t.TempDir(), "daily.db"))
	if revision, err := st.ImportDailyEquityEvidence(ctx, dailyMinimalEvidence(7, nil), 0); err != nil || revision != 1 {
		t.Fatalf("import evidence: revision=%d err=%v", revision, err)
	}
	for index, account := range []string{"acct-a", "acct-b"} {
		mustDailyTestExec(t, st, `INSERT INTO funding_payments
			(exchange, symbol, account, market_type, account_scope, income_type, income, asset, transaction_id, trade_time, identity_key)
			VALUES (?, ?, ?, 'futures', ?, 'FUNDING_FEE', '3', 'USDT', 555, ?, ?)`,
			dailyTestExchange, dailyTestSymbol, account, dailyTestScope,
			clock.fundingAt.Add(time.Duration(index)*time.Second), fmt.Sprintf("dup-funding-%d", index))
	}
	inputs, err := st.LoadDailyEquityInputs(ctx, dailyTestQuery())
	if err != nil {
		t.Fatalf("LoadDailyEquityInputs: %v", err)
	}
	fundingEntries := make([]daily.LedgerEntry, 0)
	for _, entry := range inputs.Entries {
		if entry.Kind == daily.Funding {
			fundingEntries = append(fundingEntries, entry)
		}
	}
	if len(fundingEntries) != 1 {
		t.Fatalf("duplicate funding transaction produced %d entries: %+v", len(fundingEntries), fundingEntries)
	}
	if fundingEntries[0].Known || fundingEntries[0].Amount != "3" {
		t.Fatalf("duplicate funding entry should be retained but blocked: %+v", fundingEntries[0])
	}
	if !dailyTestBlockerContains(inputs.Blockers, "funding duplicate transaction identity") {
		t.Fatalf("duplicate funding blocker missing: %v", inputs.Blockers)
	}
	if !dailyTestBlockerContains(inputs.Blockers, "acct-a") || !dailyTestBlockerContains(inputs.Blockers, "acct-b") {
		t.Fatalf("duplicate funding blocker does not name both accounts: %v", inputs.Blockers)
	}
}

func TestDailyLedgerCanonicalDecimal(t *testing.T) {
	valid := []struct {
		raw  string
		want string
	}{
		{raw: "0", want: "0"},
		{raw: "-0", want: "0"},
		{raw: "0.000000000000000001", want: "0.000000000000000001"},
		{raw: "1e-8", want: "0.00000001"},
		{raw: "1.23E+3", want: "1230"},
		{raw: "0.1", want: "0.1"},
		{raw: ".5", want: "0.5"},
		{raw: "+1.50", want: "1.5"},
		{raw: "-2.500000", want: "-2.5"},
		{raw: "123456789012345678.123456789012345678", want: "123456789012345678.123456789012345678"},
	}
	for _, testCase := range valid {
		got, err := dailyLedgerCanonicalDecimal(testCase.raw)
		if err != nil || got != testCase.want {
			t.Errorf("dailyLedgerCanonicalDecimal(%q) = %q, %v; want %q", testCase.raw, got, err, testCase.want)
		}
	}
	invalid := []string{
		"",
		"not-a-number",
		"1/2",
		"0x10",
		"0.0000000000000000001",
		"1e41",
		strings.Repeat("9", 37),
		"1e-9999999999999999999999",
	}
	for _, raw := range invalid {
		if got, err := dailyLedgerCanonicalDecimal(raw); err == nil {
			t.Errorf("dailyLedgerCanonicalDecimal(%q) = %q, want error", raw, got)
		}
	}
	negations := map[string]string{"0": "0", "-2.5": "2.5", "3": "-3", "-0.00000001": "0.00000001"}
	for raw, want := range negations {
		got, err := dailyLedgerNegateDecimal(raw)
		if err != nil || got != want {
			t.Errorf("dailyLedgerNegateDecimal(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
	if _, err := dailyLedgerNegateDecimal(""); err == nil {
		t.Error("dailyLedgerNegateDecimal accepted an empty value")
	}
}

func TestStorageServiceFailsStartupOnInvalidDailyEquitySchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	st, err := NewSQLStorage(path)
	if err != nil {
		t.Fatalf("NewSQLStorage: %v", err)
	}
	mustDailyTestExec(t, st, `CREATE TABLE daily_equity_current (id INTEGER PRIMARY KEY)`)
	if err := st.Close(); err != nil {
		t.Fatalf("close fixture storage: %v", err)
	}

	cfg := &config.Config{}
	cfg.Storage.Enabled = true
	cfg.Storage.Type = "sqlite"
	cfg.Storage.Path = path
	cfg.Storage.BufferSize = 16
	cfg.Storage.BatchSize = 4
	service, err := NewStorageService(cfg, context.Background())
	if err == nil {
		service.Stop()
		t.Fatal("NewStorageService accepted an incompatible daily equity schema")
	}
	if !strings.Contains(err.Error(), "daily equity") {
		t.Fatalf("startup error does not name the daily equity schema: %v", err)
	}
}

func TestStorageServiceMigratesDailyEquityAtStartup(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.Enabled = true
	cfg.Storage.Type = "sqlite"
	cfg.Storage.Path = filepath.Join(t.TempDir(), "runtime.db")
	cfg.Storage.BufferSize = 16
	cfg.Storage.BatchSize = 4

	service, err := NewStorageService(cfg, context.Background())
	if err != nil {
		t.Fatalf("NewStorageService: %v", err)
	}
	t.Cleanup(service.Stop)
	store, ok := service.GetStorage().(interface {
		DailyEquityMigrationStatus(context.Context) (bool, error)
	})
	if !ok {
		t.Fatal("storage does not expose daily equity migration status")
	}
	ready, err := store.DailyEquityMigrationStatus(context.Background())
	if err != nil || !ready {
		t.Fatalf("startup did not migrate daily equity schema: ready=%v err=%v", ready, err)
	}
}
