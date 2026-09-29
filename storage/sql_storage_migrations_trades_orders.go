package storage

import (
	"database/sql"
	"fmt"
	"strings"

	"quantmesh/logger"
)

// migrateTradesTable 迁移 trades 表，添加 exchange 和 account 字段
func migrateTradesTable(db *sql.DB) error {
	logger.Info("🔧 开始检查 trades 表結構...")

	// 检查 exchange 列是否存在
	rows, err := db.Query(`PRAGMA table_info(trades)`)
	if err != nil {
		return fmt.Errorf("检查表結構失败: %w", err)
	}
	defer rows.Close()

	hasExchangeColumn := false
	hasAccountColumn := false
	hasFeeColumn := false
	hasFeeAssetColumn := false
	hasBuyPriceDeviationColumn := false
	hasSellPriceDeviationColumn := false
	hasBotIDColumn := false
	hasExecutionKeyColumn := false
	for rows.Next() {
		var cid int
		var name string
		var dataType string
		var notNull int
		var defaultValue interface{}
		var pk int

		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk); err != nil {
			// 漏讀一列會讓遷移誤判欄位不存在，進而重複 ALTER TABLE 或跳過必要遷移
			return fmt.Errorf("讀取 trades 表結構失败: %w", err)
		}
		if name == "exchange" {
			hasExchangeColumn = true
		}
		if name == "account" {
			hasAccountColumn = true
		}
		if name == "fee" {
			hasFeeColumn = true
		}
		if name == "fee_asset" {
			hasFeeAssetColumn = true
		}
		if name == "buy_price_deviation" {
			hasBuyPriceDeviationColumn = true
		}
		if name == "sell_price_deviation" {
			hasSellPriceDeviationColumn = true
		}
		if name == "bot_id" {
			hasBotIDColumn = true
		}
		if name == "execution_key" {
			hasExecutionKeyColumn = true
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("遍歷 trades 表結構失败: %w", err)
	}

	if !hasExchangeColumn {
		logger.Info("🔄 开始迁移 trades 表：添加 exchange 字段")
		_, err := db.Exec(`ALTER TABLE trades ADD COLUMN exchange TEXT`)
		if err != nil {
			return fmt.Errorf("添加 exchange 列失败: %w", err)
		}
		logger.Info("✅ exchange 列添加成功")

		// 更新現有數據
		_, err = db.Exec(`UPDATE trades SET exchange = 'binance' WHERE exchange IS NULL`)
		if err != nil {
			logger.Warn("⚠️ 更新現有 exchange 數據失败: %v", err)
		}
	}

	if !hasAccountColumn {
		logger.Info("🔄 开始迁移 trades 表：添加 account 字段")
		_, err := db.Exec(`ALTER TABLE trades ADD COLUMN account TEXT`)
		if err != nil {
			return fmt.Errorf("添加 account 列失败: %w", err)
		}
		logger.Info("✅ account 列添加成功")

		// 為現有數據創建索引
		_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_trades_account_symbol ON trades(account, symbol)`)
		if err != nil {
			logger.Warn("⚠️ 創建 account 索引失败: %v", err)
		}
	}

	// 检查 fee 列是否存在（手續費支持）
	if !hasFeeColumn {
		logger.Info("🔄 开始迁移 trades 表：添加 fee 字段")
		_, err := db.Exec(`ALTER TABLE trades ADD COLUMN fee DECIMAL(20,8) DEFAULT 0`)
		if err != nil {
			return fmt.Errorf("添加 fee 列失败: %w", err)
		}
		logger.Info("✅ fee 列添加成功")
	}
	if !hasFeeAssetColumn {
		logger.Info("🔄 开始迁移 trades 表：添加 fee_asset 字段")
		_, err := db.Exec(`ALTER TABLE trades ADD COLUMN fee_asset TEXT DEFAULT ''`)
		if err != nil {
			return fmt.Errorf("添加 fee_asset 列失败: %w", err)
		}
		logger.Info("✅ fee_asset 列添加成功")
	}

	// 🔥 检查并添加价格偏差字段
	if !hasBuyPriceDeviationColumn {
		logger.Info("🔄 开始迁移 trades 表：添加 buy_price_deviation 字段")
		_, err := db.Exec(`ALTER TABLE trades ADD COLUMN buy_price_deviation DECIMAL(20,8) DEFAULT 0`)
		if err != nil {
			return fmt.Errorf("添加 buy_price_deviation 列失败: %w", err)
		}
		logger.Info("✅ buy_price_deviation 列添加成功")
	}
	if !hasSellPriceDeviationColumn {
		logger.Info("🔄 开始迁移 trades 表：添加 sell_price_deviation 字段")
		_, err := db.Exec(`ALTER TABLE trades ADD COLUMN sell_price_deviation DECIMAL(20,8) DEFAULT 0`)
		if err != nil {
			return fmt.Errorf("添加 sell_price_deviation 列失败: %w", err)
		}
		logger.Info("✅ sell_price_deviation 列添加成功")
	}

	if !hasBotIDColumn {
		logger.Info("🔄 开始迁移 trades 表：添加 bot_id 字段")
		_, err := db.Exec(`ALTER TABLE trades ADD COLUMN bot_id TEXT DEFAULT ''`)
		if err != nil {
			return fmt.Errorf("添加 bot_id 列失败: %w", err)
		}
		logger.Info("✅ bot_id 列添加成功")
	}
	if err := backfillTradesBotIDFromOrders(db, "trades", "orders"); err != nil {
		return err
	}
	if !hasExecutionKeyColumn {
		if _, err := db.Exec(`ALTER TABLE trades ADD COLUMN execution_key TEXT`); err != nil {
			return fmt.Errorf("添加 trades.execution_key 列失败: %w", err)
		}
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uk_trades_execution_key ON trades(execution_key) WHERE execution_key IS NOT NULL`); err != nil {
		return fmt.Errorf("创建 trades.execution_key 唯一索引失败: %w", err)
	}

	// 無論是否是新增列，都确保索引存在（老库可能已有列但缺索引）
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_trades_account_symbol ON trades(account, symbol)`)
	if err != nil {
		logger.Warn("⚠️ 确保 trades account 索引失败: %v", err)
	}
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_trades_exchange_symbol ON trades(exchange, symbol)`)
	if err != nil {
		logger.Warn("⚠️ 确保 trades exchange 索引失败: %v", err)
	}
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_trades_bot_exchange_symbol ON trades(bot_id, exchange, symbol)`)
	if err != nil {
		logger.Warn("⚠️ 确保 trades bot_id 索引失败: %v", err)
	}

	logger.Info("✅ trades 表迁移检查完成")
	return nil
}

// backfillTradesBotIDFromOrders 用订单表中的 bot_id 回填配对成交。
func backfillTradesBotIDFromOrders(db *sql.DB, tableName, ordersTableName string) error {
	if !isSafeMigrationIdentifier(tableName) || !isSafeMigrationIdentifier(ordersTableName) {
		return fmt.Errorf("backfill table names must be simple SQL identifiers")
	}
	scopeMatch := fmt.Sprintf(`
		LOWER(TRIM(COALESCE(o.exchange, ''))) = LOWER(TRIM(COALESCE(%[1]s.exchange, '')))
		AND TRIM(COALESCE(o.account, '')) = TRIM(COALESCE(%[1]s.account, ''))
		AND LOWER(TRIM(COALESCE(o.market_type, ''))) = LOWER(TRIM(COALESCE(%[1]s.market_type, '')))
		AND UPPER(TRIM(COALESCE(o.symbol, ''))) = UPPER(TRIM(COALESCE(%[1]s.symbol, '')))
		AND TRIM(COALESCE(o.account_scope, '')) = TRIM(COALESCE(%[1]s.account_scope, ''))
	`, tableName)
	orderBot := func(orderColumn string) string {
		return fmt.Sprintf(`(SELECT MIN(NULLIF(TRIM(o.bot_id), '')) FROM %s o WHERE o.order_id = %s.%s AND %s HAVING COUNT(DISTINCT CASE WHEN NULLIF(TRIM(o.bot_id), '') IS NOT NULL THEN HEX(TRIM(o.bot_id)) END) = 1)`, ordersTableName, tableName, orderColumn, scopeMatch)
	}
	orderOwnerCount := func(orderColumn string) string {
		return fmt.Sprintf(`(SELECT COUNT(DISTINCT CASE WHEN NULLIF(TRIM(o.bot_id), '') IS NOT NULL THEN HEX(TRIM(o.bot_id)) END) FROM %s o WHERE o.order_id = %s.%s AND %s)`, ordersTableName, tableName, orderColumn, scopeMatch)
	}
	q := fmt.Sprintf(`
		UPDATE %[1]s SET bot_id = COALESCE(%[2]s, %[3]s, '')
		WHERE (bot_id IS NULL OR bot_id = '')
			AND (%[4]s = 1 OR %[5]s = 1)
			AND %[4]s <= 1 AND %[5]s <= 1
			AND (%[2]s IS NULL OR %[3]s IS NULL OR %[2]s = %[3]s)
	`, tableName, orderBot("sell_order_id"), orderBot("buy_order_id"), orderOwnerCount("sell_order_id"), orderOwnerCount("buy_order_id"))
	if _, err := db.Exec(q); err != nil {
		return fmt.Errorf("回填 %s.bot_id 失败: %w", tableName, err)
	}
	logger.Info("✅ 已嘗試從 orders 回填 %s.bot_id", tableName)
	return nil
}

func isSafeMigrationIdentifier(name string) bool {
	if name == "" || !((name[0] >= 'a' && name[0] <= 'z') || (name[0] >= 'A' && name[0] <= 'Z') || name[0] == '_') {
		return false
	}
	for i := 1; i < len(name); i++ {
		ch := name[i]
		if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_') {
			return false
		}
	}
	return true
}

// migrateOrdersTable 迁移 orders 表，添加 filled_qty / exchange / type / realized_pnl / strategy_name / strategy_type 列
func migrateTradesExchangePnL(db *sql.DB) error {
	// 检查表是否存在新字段，不存在则添加
	columns := []struct {
		name string
		def  string
	}{
		{"exchange_pnl", "ALTER TABLE trades ADD COLUMN exchange_pnl DECIMAL(20,8) DEFAULT 0"},
	}

	for _, col := range columns {
		// 先检查列是否存在
		var count int
		err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('trades') WHERE name = ?", col.name).Scan(&count)
		if err != nil {
			continue // 忽略错误，尝试下一列
		}
		if count == 0 {
			if _, err := db.Exec(col.def); err != nil {
				logger.Warn("⚠️ trades 表添加列 %s 失败: %v", col.name, err)
			} else {
				logger.Info("🔄 trades 表成功添加列: %s", col.name)
			}
		}
	}
	return nil
}

// migrateTradesMarketType adds an explicit market dimension without guessing legacy rows.
// Empty values remain unclassified and must not be silently attributed to spot or futures.
func migrateTradesMarketType(db *sql.DB) error {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('trades') WHERE name = ?`, "market_type").Scan(&count); err != nil {
		return fmt.Errorf("检查 trades.market_type 字段失败: %w", err)
	}
	if count > 0 {
		return nil
	}
	if _, err := db.Exec(`ALTER TABLE trades ADD COLUMN market_type TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("添加 trades.market_type 字段失败: %w", err)
	}
	return nil
}

func migrateTradesAccountScope(db *sql.DB) error {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('trades') WHERE name = ?`, "account_scope").Scan(&count); err != nil {
		return fmt.Errorf("检查 trades.account_scope 字段失败: %w", err)
	}
	if count > 0 {
		return nil
	}
	if _, err := db.Exec(`ALTER TABLE trades ADD COLUMN account_scope TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("添加 trades.account_scope 字段失败: %w", err)
	}
	return nil
}

// migrateTradesPnLAsset adds explicit denomination metadata; legacy rows remain unknown and are never guessed.
func migrateTradesPnLAsset(db *sql.DB) error {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('trades') WHERE name = ?`, "pnl_asset").Scan(&count); err != nil {
		return fmt.Errorf("檢查 trades.pnl_asset 欄位失敗: %w", err)
	}
	if count > 0 {
		return nil
	}
	if _, err := db.Exec(`ALTER TABLE trades ADD COLUMN pnl_asset TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("添加 trades.pnl_asset 欄位失敗: %w", err)
	}
	return nil
}

func migrateOrdersTable(db *sql.DB) error {
	columns := []struct {
		name string
		def  string
	}{
		{"filled_qty", "ALTER TABLE orders ADD COLUMN filled_qty DECIMAL(20,8) DEFAULT 0"},
		{"bot_id", "ALTER TABLE orders ADD COLUMN bot_id TEXT DEFAULT ''"},
		{"account", "ALTER TABLE orders ADD COLUMN account TEXT DEFAULT ''"},
		{"market_type", "ALTER TABLE orders ADD COLUMN market_type TEXT NOT NULL DEFAULT ''"},
		{"account_scope", "ALTER TABLE orders ADD COLUMN account_scope TEXT NOT NULL DEFAULT ''"},
		{"exchange", "ALTER TABLE orders ADD COLUMN exchange TEXT DEFAULT ''"},
		{"type", "ALTER TABLE orders ADD COLUMN type TEXT DEFAULT ''"},
		{"realized_pnl", "ALTER TABLE orders ADD COLUMN realized_pnl DECIMAL(20,8)"},
		{"strategy_name", "ALTER TABLE orders ADD COLUMN strategy_name TEXT DEFAULT ''"},
		{"strategy_type", "ALTER TABLE orders ADD COLUMN strategy_type TEXT DEFAULT ''"},
		{"order_source", "ALTER TABLE orders ADD COLUMN order_source TEXT DEFAULT ''"},
	}

	for _, col := range columns {
		row := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('orders') WHERE name=?`, col.name)
		var count int
		if err := row.Scan(&count); err != nil {
			continue
		}
		if count == 0 {
			if _, err := db.Exec(col.def); err != nil {
				logger.Warn("⚠️ orders 表添加列 %s 失败: %v", col.name, err)
			} else {
				logger.Info("🔄 orders 表成功添加列: %s", col.name)
			}
		}
	}

	if err := ensureOrdersCompositeUniqueConstraint(db); err != nil {
		return err
	}
	return nil
}

func migrateRiskCheckHistoryTable(db *sql.DB) error {
	columns := []struct {
		name string
		def  string
	}{
		{"bot_id", "ALTER TABLE risk_check_history ADD COLUMN bot_id TEXT DEFAULT ''"},
		{"exchange", "ALTER TABLE risk_check_history ADD COLUMN exchange TEXT DEFAULT ''"},
		{"market_type", "ALTER TABLE risk_check_history ADD COLUMN market_type TEXT DEFAULT ''"},
	}
	for _, col := range columns {
		row := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('risk_check_history') WHERE name=?`, col.name)
		var count int
		if err := row.Scan(&count); err != nil {
			continue
		}
		if count == 0 {
			if _, err := db.Exec(col.def); err != nil {
				logger.Warn("⚠️ risk_check_history 表添加列 %s 失败: %v", col.name, err)
			} else {
				logger.Info("🔄 risk_check_history 表成功添加列: %s", col.name)
			}
		}
	}

	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_risk_check_history_bot_id ON risk_check_history(bot_id)`); err != nil {
		logger.Warn("⚠️ 创建 risk_check_history bot_id 索引失败: %v", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_risk_check_history_bot_time ON risk_check_history(bot_id, check_time)`); err != nil {
		logger.Warn("⚠️ 创建 risk_check_history bot_id+check_time 索引失败: %v", err)
	}
	return nil
}

func hasLegacyOrderIDUniqueConstraint(db *sql.DB) (bool, error) {
	rows, err := db.Query(`PRAGMA index_list('orders')`)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	type idxInfo struct {
		name    string
		unique  int
		partial int
	}
	var indexes []idxInfo
	for rows.Next() {
		var seq int
		var name string
		var unique int
		var origin string
		var partial int
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			return false, err
		}
		indexes = append(indexes, idxInfo{name: name, unique: unique, partial: partial})
	}
	if err := rows.Err(); err != nil {
		return false, err
	}

	for _, idx := range indexes {
		if idx.unique == 0 || idx.partial != 0 {
			continue
		}
		infoRows, err := db.Query(fmt.Sprintf(`PRAGMA index_info('%s')`, idx.name))
		if err != nil {
			return false, err
		}
		var cols []string
		for infoRows.Next() {
			var seqno, cid int
			var colName string
			if err := infoRows.Scan(&seqno, &cid, &colName); err != nil {
				infoRows.Close()
				return false, err
			}
			cols = append(cols, colName)
		}
		infoRows.Close()
		if len(cols) == 1 && cols[0] == "order_id" {
			return true, nil
		}
		if len(cols) == 2 && cols[0] == "exchange" && cols[1] == "order_id" {
			return true, nil
		}
	}
	return false, nil
}

// ordersCompositeUniqueIndexName 與 SaveOrder 中 SQLite ON CONFLICT 目標列一致。
const ordersCompositeUniqueIndexName = "idx_orders_exchange_account_symbol_order_id"

// ordersCompositeUniqueIndexMatches 檢查是否存在與 upsert 一致的複合 UNIQUE 索引（列名與順序須完全一致）。
func ordersCompositeUniqueIndexMatches(db *sql.DB) (bool, error) {
	rows, err := db.Query(`PRAGMA index_list('orders')`)
	if err != nil {
		return false, err
	}
	found := false
	for rows.Next() {
		var seq int
		var name string
		var unique int
		var origin string
		var partial int
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			rows.Close()
			return false, err
		}
		if name != ordersCompositeUniqueIndexName {
			continue
		}
		if unique == 0 || partial != 0 {
			rows.Close()
			return false, nil
		}
		found = true
		break
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return false, err
	}
	rows.Close() // SQLite 單連接：必須關閉結果集後再發起 PRAGMA index_info，否則死鎖

	if !found {
		return false, nil
	}
	infoRows, err := db.Query(fmt.Sprintf(`PRAGMA index_info('%s')`, ordersCompositeUniqueIndexName))
	if err != nil {
		return false, err
	}
	defer infoRows.Close()
	want := []string{"exchange", "account", "symbol", "order_id"}
	var cols []string
	for infoRows.Next() {
		var seqno, cid int
		var colName string
		if err := infoRows.Scan(&seqno, &cid, &colName); err != nil {
			return false, err
		}
		cols = append(cols, colName)
	}
	if err := infoRows.Err(); err != nil {
		return false, err
	}
	if len(cols) != len(want) {
		return false, nil
	}
	for i := range want {
		if cols[i] != want[i] {
			return false, nil
		}
	}
	return true, nil
}

// ordersCompositeUniqueColumns 與 SaveOrder 的 ON CONFLICT 目標列（順序一致）。
var ordersCompositeUniqueColumns = []string{"exchange", "account", "symbol", "order_id"}

// createOrdersCompositeUniqueIndexIfColumnsReady 在 orders 表已具備全部目標列時創建複合 UNIQUE 索引（冪等）。
// 供 createTables 在全新數據庫上一次到位；列不齊（舊庫）時靜默跳過，由遷移補齊。
func createOrdersCompositeUniqueIndexIfColumnsReady(db *sql.DB) error {
	for _, col := range ordersCompositeUniqueColumns {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('orders') WHERE name=?`, col).Scan(&count); err != nil {
			return fmt.Errorf("檢查 orders 列 %s 失败: %w", col, err)
		}
		if count == 0 {
			return nil
		}
	}
	// 若存在同名但列不一致的舊索引，IF NOT EXISTS 不會修正它；那種情況由 ensureOrdersCompositeUniqueConstraint 處理。
	_, err := db.Exec(fmt.Sprintf(
		`CREATE UNIQUE INDEX IF NOT EXISTS %s ON orders(exchange, account, symbol, order_id)`,
		ordersCompositeUniqueIndexName,
	))
	return err
}

// isOrdersOnConflictMismatch 判斷 SQLite 是否因缺少與 ON CONFLICT 匹配的唯一約束而報錯。
func isOrdersOnConflictMismatch(err error) bool {
	return err != nil && strings.Contains(err.Error(), "ON CONFLICT clause does not match")
}

// EnsureOrdersSchema 重新校驗並修復 orders 複合唯一索引（僅 SQLite；冪等）。
// 用途：同一 SQLite 文件被其他組件（如 database 包的 GORM AutoMigrate）重建 orders 表後，索引會丟失，需在其後再修一次。
func (s *SQLStorage) EnsureOrdersSchema() error {
	if s == nil || s.db == nil || s.dbType != "sqlite" {
		return nil
	}
	return ensureOrdersCompositeUniqueConstraint(s.db)
}

func rebuildOrdersTableForCompositeUnique(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.Exec(`
		CREATE TABLE IF NOT EXISTS orders_v2 (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			order_id BIGINT,
			bot_id TEXT DEFAULT '',
			account TEXT DEFAULT '',
			market_type TEXT NOT NULL DEFAULT '',
			account_scope TEXT NOT NULL DEFAULT '',
			client_order_id TEXT,
			symbol TEXT,
			side TEXT,
			exchange TEXT DEFAULT '',
			type TEXT DEFAULT '',
			price DECIMAL(20,8),
			quantity DECIMAL(20,8),
			filled_qty DECIMAL(20,8) DEFAULT 0,
			status TEXT,
			realized_pnl DECIMAL(20,8),
			strategy_name TEXT DEFAULT '',
			strategy_type TEXT DEFAULT '',
			order_source TEXT DEFAULT '',
			created_at TIMESTAMP,
			updated_at TIMESTAMP
		);
	`); err != nil {
		return err
	}

	if _, err = tx.Exec(`
		INSERT INTO orders_v2 (
			id, order_id, bot_id, account, market_type, account_scope, client_order_id, symbol, side, exchange, type, price, quantity, filled_qty,
			status, realized_pnl, strategy_name, strategy_type, order_source, created_at, updated_at
		)
		SELECT
			id, order_id, COALESCE(bot_id, ''), COALESCE(account, COALESCE(bot_id, '')), COALESCE(market_type, ''), COALESCE(account_scope, ''), client_order_id, symbol, side, COALESCE(exchange, ''), COALESCE(type, ''),
			price, quantity, COALESCE(filled_qty, 0), status, realized_pnl, COALESCE(strategy_name, ''),
			COALESCE(strategy_type, ''), COALESCE(order_source, ''), created_at, updated_at
		FROM orders;
	`); err != nil {
		return err
	}

	if _, err = tx.Exec(`DROP TABLE orders`); err != nil {
		return err
	}
	if _, err = tx.Exec(`ALTER TABLE orders_v2 RENAME TO orders`); err != nil {
		return err
	}
	if _, err = tx.Exec(`DROP INDEX IF EXISTS idx_orders_exchange_order_id`); err != nil {
		return err
	}
	if _, err = tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_orders_exchange_account_symbol_order_id ON orders(exchange, account, symbol, order_id)`); err != nil {
		return err
	}
	if _, err = tx.Exec(`CREATE INDEX IF NOT EXISTS idx_orders_order_id ON orders(order_id)`); err != nil {
		return err
	}
	if _, err = tx.Exec(`CREATE INDEX IF NOT EXISTS idx_orders_bot_id ON orders(bot_id)`); err != nil {
		return err
	}
	if _, err = tx.Exec(`CREATE INDEX IF NOT EXISTS idx_orders_account ON orders(account)`); err != nil {
		return err
	}
	if _, err = tx.Exec(`CREATE INDEX IF NOT EXISTS idx_orders_created_at ON orders(created_at)`); err != nil {
		return err
	}
	return tx.Commit()
}

func ensureOrdersCompositeUniqueConstraint(db *sql.DB) error {
	legacyUnique, err := hasLegacyOrderIDUniqueConstraint(db)
	if err != nil {
		return err
	}
	if legacyUnique {
		logger.Info("🔄 檢測到舊版 orders 唯一約束，開始遷移為 (exchange, account, symbol, order_id)")
		if err := rebuildOrdersTableForCompositeUnique(db); err != nil {
			return fmt.Errorf("重建 orders 表失败: %w", err)
		}
	}
	if _, err := db.Exec(`DROP INDEX IF EXISTS idx_orders_exchange_order_id`); err != nil {
		return err
	}
	ok, err := ordersCompositeUniqueIndexMatches(db)
	if err != nil {
		return err
	}
	if !ok {
		logger.Info("🔄 orders 複合唯一索引缺失或與 ON CONFLICT 列不一致，正在重建 %s", ordersCompositeUniqueIndexName)
		if _, err := db.Exec(fmt.Sprintf(`DROP INDEX IF EXISTS %s`, ordersCompositeUniqueIndexName)); err != nil {
			return err
		}
		if _, err := db.Exec(fmt.Sprintf(
			`CREATE UNIQUE INDEX %s ON orders(exchange, account, symbol, order_id)`,
			ordersCompositeUniqueIndexName,
		)); err != nil {
			return fmt.Errorf("創建 orders 複合唯一索引失败（若表中存在重複的 exchange/account/symbol/order_id 組合，請先清理重複行）: %w", err)
		}
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_orders_order_id ON orders(order_id)`); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_orders_bot_id ON orders(bot_id)`); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_orders_account ON orders(account)`); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_orders_scope_market_symbol_time ON orders(account_scope, exchange, market_type, symbol, created_at)`); err != nil {
		return err
	}
	return nil
}
