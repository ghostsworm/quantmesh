package storage

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"quantmesh/utils"
)

// ========== FIX 协议会话状态 / 订单映射 存儲 ==========

// UpsertFixSessionState 保存或更新 FIX 会话状态
func (s *SQLStorage) UpsertFixSessionState(state *FixSessionState) error {
	if state == nil {
		return fmt.Errorf("fix session state is nil")
	}
	if strings.TrimSpace(state.SessionID) == "" {
		return fmt.Errorf("fix session_id is required")
	}
	updatedAt := utils.ToUTC(state.UpdatedAt)
	if updatedAt.IsZero() {
		updatedAt = utils.NowUTC()
	}
	lastLogonAt := toNullableTime(state.LastLogonAt)
	lastHeartbeatAt := toNullableTime(state.LastHeartbeatAt)
	isLoggedOn := 0
	if state.IsLoggedOn {
		isLoggedOn = 1
	}

	_, err := s.db.Exec(`
		INSERT INTO fix_session_states
		(session_id, bot_id, role, begin_string, sender_comp_id, target_comp_id, next_sender_seq, next_target_seq, is_logged_on, last_logon_at, last_heartbeat_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET
			bot_id = COALESCE(NULLIF(excluded.bot_id, ''), fix_session_states.bot_id),
			role = excluded.role,
			begin_string = excluded.begin_string,
			sender_comp_id = excluded.sender_comp_id,
			target_comp_id = excluded.target_comp_id,
			next_sender_seq = excluded.next_sender_seq,
			next_target_seq = excluded.next_target_seq,
			is_logged_on = excluded.is_logged_on,
			last_logon_at = COALESCE(excluded.last_logon_at, fix_session_states.last_logon_at),
			last_heartbeat_at = COALESCE(excluded.last_heartbeat_at, fix_session_states.last_heartbeat_at),
			updated_at = excluded.updated_at
	`, state.SessionID, state.BotID, state.Role, state.BeginString, state.SenderCompID, state.TargetCompID, state.NextSenderSeq, state.NextTargetSeq, isLoggedOn, lastLogonAt, lastHeartbeatAt, updatedAt)
	if err != nil {
		return fmt.Errorf("upsert fix_session_states failed: %w", err)
	}
	return nil
}

// GetFixSessionState 根据 session_id 获取 FIX 会话状态
func (s *SQLStorage) GetFixSessionState(sessionID string) (*FixSessionState, error) {
	var item FixSessionState
	var isLoggedOn int
	var lastLogonAt sql.NullTime
	var lastHeartbeatAt sql.NullTime

	err := s.db.QueryRow(`
		SELECT session_id, bot_id, role, begin_string, sender_comp_id, target_comp_id, next_sender_seq, next_target_seq, is_logged_on, last_logon_at, last_heartbeat_at, updated_at
		FROM fix_session_states
		WHERE session_id = ?
	`, sessionID).Scan(
		&item.SessionID, &item.BotID, &item.Role, &item.BeginString, &item.SenderCompID, &item.TargetCompID, &item.NextSenderSeq, &item.NextTargetSeq, &isLoggedOn, &lastLogonAt, &lastHeartbeatAt, &item.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query fix_session_states failed: %w", err)
	}
	item.IsLoggedOn = isLoggedOn == 1
	item.LastLogonAt = fromNullableTime(lastLogonAt)
	item.LastHeartbeatAt = fromNullableTime(lastHeartbeatAt)
	return &item, nil
}

// ListFixSessionStates 列出 FIX 会话状态
func (s *SQLStorage) ListFixSessionStates(limit, offset int) ([]*FixSessionState, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.Query(`
		SELECT session_id, bot_id, role, begin_string, sender_comp_id, target_comp_id, next_sender_seq, next_target_seq, is_logged_on, last_logon_at, last_heartbeat_at, updated_at
		FROM fix_session_states
		ORDER BY updated_at DESC
		LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list fix_session_states failed: %w", err)
	}
	defer rows.Close()

	var result []*FixSessionState
	for rows.Next() {
		item := &FixSessionState{}
		var isLoggedOn int
		var lastLogonAt sql.NullTime
		var lastHeartbeatAt sql.NullTime
		if err := rows.Scan(
			&item.SessionID, &item.BotID, &item.Role, &item.BeginString, &item.SenderCompID, &item.TargetCompID, &item.NextSenderSeq, &item.NextTargetSeq, &isLoggedOn, &lastLogonAt, &lastHeartbeatAt, &item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan fix_session_states failed: %w", err)
		}
		item.IsLoggedOn = isLoggedOn == 1
		item.LastLogonAt = fromNullableTime(lastLogonAt)
		item.LastHeartbeatAt = fromNullableTime(lastHeartbeatAt)
		result = append(result, item)
	}
	return result, rows.Err()
}

// UpsertFixOrderLink 保存或更新 FIX 主订单映射
func (s *SQLStorage) UpsertFixOrderLink(link *FixOrderLink) error {
	if link == nil {
		return fmt.Errorf("fix order link is nil")
	}
	if strings.TrimSpace(link.SessionID) == "" || strings.TrimSpace(link.ClOrdID) == "" {
		return fmt.Errorf("session_id and cl_ord_id are required")
	}

	createdAt := utils.ToUTC(link.CreatedAt)
	if createdAt.IsZero() {
		createdAt = utils.NowUTC()
	}
	updatedAt := utils.ToUTC(link.UpdatedAt)
	if updatedAt.IsZero() {
		updatedAt = utils.NowUTC()
	}

	_, err := s.db.Exec(`
		INSERT INTO fix_order_links
		(session_id, cl_ord_id, orig_cl_ord_id, bot_id, exchange, symbol, side, internal_order_id, last_exec_id, ord_status, cum_qty, leaves_qty, avg_px, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id, cl_ord_id) DO UPDATE SET
			orig_cl_ord_id = COALESCE(NULLIF(excluded.orig_cl_ord_id, ''), fix_order_links.orig_cl_ord_id),
			bot_id = COALESCE(NULLIF(excluded.bot_id, ''), fix_order_links.bot_id),
			exchange = COALESCE(NULLIF(excluded.exchange, ''), fix_order_links.exchange),
			symbol = COALESCE(NULLIF(excluded.symbol, ''), fix_order_links.symbol),
			side = COALESCE(NULLIF(excluded.side, ''), fix_order_links.side),
			internal_order_id = CASE WHEN excluded.internal_order_id > 0 THEN excluded.internal_order_id ELSE fix_order_links.internal_order_id END,
			last_exec_id = COALESCE(NULLIF(excluded.last_exec_id, ''), fix_order_links.last_exec_id),
			ord_status = COALESCE(NULLIF(excluded.ord_status, ''), fix_order_links.ord_status),
			cum_qty = CASE WHEN excluded.cum_qty > fix_order_links.cum_qty THEN excluded.cum_qty ELSE fix_order_links.cum_qty END,
			leaves_qty = CASE WHEN excluded.leaves_qty >= 0 THEN excluded.leaves_qty ELSE fix_order_links.leaves_qty END,
			avg_px = CASE WHEN excluded.avg_px > 0 THEN excluded.avg_px ELSE fix_order_links.avg_px END,
			updated_at = excluded.updated_at
	`, link.SessionID, link.ClOrdID, link.OrigClOrdID, link.BotID, link.Exchange, link.Symbol, link.Side, link.InternalOrderID, link.LastExecID, link.OrdStatus, link.CumQty, link.LeavesQty, link.AvgPx, createdAt, updatedAt)
	if err != nil {
		return fmt.Errorf("upsert fix_order_links failed: %w", err)
	}
	return nil
}

// GetFixOrderLinkByClOrdID 通过 cl_ord_id 查询 FIX 主订单映射
func (s *SQLStorage) GetFixOrderLinkByClOrdID(sessionID, clOrdID string) (*FixOrderLink, error) {
	row := s.db.QueryRow(`
		SELECT id, session_id, cl_ord_id, orig_cl_ord_id, bot_id, exchange, symbol, side, internal_order_id, last_exec_id, ord_status, cum_qty, leaves_qty, avg_px, created_at, updated_at
		FROM fix_order_links
		WHERE session_id = ? AND cl_ord_id = ?
	`, sessionID, clOrdID)
	return scanFixOrderLinkRow(row)
}

// GetFixOrderLinkByInternalOrderID 通过内部订单号查询 FIX 主订单映射
func (s *SQLStorage) GetFixOrderLinkByInternalOrderID(sessionID string, internalOrderID int64) (*FixOrderLink, error) {
	row := s.db.QueryRow(`
		SELECT id, session_id, cl_ord_id, orig_cl_ord_id, bot_id, exchange, symbol, side, internal_order_id, last_exec_id, ord_status, cum_qty, leaves_qty, avg_px, created_at, updated_at
		FROM fix_order_links
		WHERE session_id = ? AND internal_order_id = ?
		ORDER BY updated_at DESC
		LIMIT 1
	`, sessionID, internalOrderID)
	return scanFixOrderLinkRow(row)
}

// ListFixOrderLinks 列出 FIX 主订单映射
func (s *SQLStorage) ListFixOrderLinks(sessionID, ordStatus string, limit, offset int) ([]*FixOrderLink, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 2000 {
		limit = 2000
	}
	if offset < 0 {
		offset = 0
	}
	query := `
		SELECT id, session_id, cl_ord_id, orig_cl_ord_id, bot_id, exchange, symbol, side, internal_order_id, last_exec_id, ord_status, cum_qty, leaves_qty, avg_px, created_at, updated_at
		FROM fix_order_links
		WHERE 1=1
	`
	args := make([]interface{}, 0, 4)
	if sessionID != "" {
		query += " AND session_id = ?"
		args = append(args, sessionID)
	}
	if ordStatus != "" {
		query += " AND ord_status = ?"
		args = append(args, ordStatus)
	}
	query += " ORDER BY updated_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list fix_order_links failed: %w", err)
	}
	defer rows.Close()

	result := make([]*FixOrderLink, 0)
	for rows.Next() {
		item := &FixOrderLink{}
		if err := rows.Scan(
			&item.ID, &item.SessionID, &item.ClOrdID, &item.OrigClOrdID, &item.BotID, &item.Exchange, &item.Symbol, &item.Side, &item.InternalOrderID, &item.LastExecID, &item.OrdStatus, &item.CumQty, &item.LeavesQty, &item.AvgPx, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan fix_order_links failed: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func scanFixOrderLinkRow(row *sql.Row) (*FixOrderLink, error) {
	item := &FixOrderLink{}
	if err := row.Scan(
		&item.ID, &item.SessionID, &item.ClOrdID, &item.OrigClOrdID, &item.BotID, &item.Exchange, &item.Symbol, &item.Side, &item.InternalOrderID, &item.LastExecID, &item.OrdStatus, &item.CumQty, &item.LeavesQty, &item.AvgPx, &item.CreatedAt, &item.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("query fix_order_links failed: %w", err)
	}
	return item, nil
}

func toNullableTime(t *time.Time) interface{} {
	if t == nil || t.IsZero() {
		return nil
	}
	return utils.ToUTC(*t)
}

func fromNullableTime(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	value := utils.ToUTC(t.Time)
	return &value
}

// migrateFixTables 遷移 FIX 会话状态与订单映射表
func migrateFixTables(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS fix_session_states (
			session_id TEXT PRIMARY KEY,
			bot_id TEXT NOT NULL DEFAULT '',
			role TEXT NOT NULL DEFAULT '',
			begin_string TEXT NOT NULL DEFAULT '',
			sender_comp_id TEXT NOT NULL DEFAULT '',
			target_comp_id TEXT NOT NULL DEFAULT '',
			next_sender_seq INTEGER NOT NULL DEFAULT 1,
			next_target_seq INTEGER NOT NULL DEFAULT 1,
			is_logged_on INTEGER NOT NULL DEFAULT 0,
			last_logon_at TIMESTAMP,
			last_heartbeat_at TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_fix_session_updated_at ON fix_session_states(updated_at);
		CREATE INDEX IF NOT EXISTS idx_fix_session_bot_id ON fix_session_states(bot_id);

		CREATE TABLE IF NOT EXISTS fix_order_links (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			cl_ord_id TEXT NOT NULL,
			orig_cl_ord_id TEXT DEFAULT '',
			bot_id TEXT DEFAULT '',
			exchange TEXT DEFAULT '',
			symbol TEXT DEFAULT '',
			side TEXT DEFAULT '',
			internal_order_id BIGINT NOT NULL DEFAULT 0,
			last_exec_id TEXT DEFAULT '',
			ord_status TEXT DEFAULT '',
			cum_qty REAL NOT NULL DEFAULT 0,
			leaves_qty REAL NOT NULL DEFAULT 0,
			avg_px REAL NOT NULL DEFAULT 0,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(session_id, cl_ord_id)
		);
		CREATE INDEX IF NOT EXISTS idx_fix_order_session_updated ON fix_order_links(session_id, updated_at);
		CREATE INDEX IF NOT EXISTS idx_fix_order_session_status ON fix_order_links(session_id, ord_status);
		CREATE INDEX IF NOT EXISTS idx_fix_order_internal ON fix_order_links(session_id, internal_order_id);
	`)
	if err != nil {
		return err
	}

	// 兼容已存在的早期表结构
	var count int
	if e := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('fix_session_states') WHERE name = 'bot_id'").Scan(&count); e == nil && count == 0 {
		_, _ = db.Exec("ALTER TABLE fix_session_states ADD COLUMN bot_id TEXT NOT NULL DEFAULT ''")
		_, _ = db.Exec("CREATE INDEX IF NOT EXISTS idx_fix_session_bot_id ON fix_session_states(bot_id)")
	}
	return nil
}
