package storage

import (
	"database/sql"
	"fmt"
	"time"

	"quantmesh/utils"
)

// ========== 自动提取（Profit Withdraw）规则与记录存儲 ==========

// ListAccountIDsWithProfitRules 返回有提取规则的所有 account_id（用於定時任務）
func (s *SQLStorage) ListAccountIDsWithProfitRules() ([]string, error) {
	rows, err := s.db.Query(`SELECT DISTINCT account_id FROM profit_withdraw_rules WHERE enabled = 1`)
	if err != nil {
		return nil, fmt.Errorf("查詢 account_id 失败: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ListProfitWithdrawRules 查詢指定账戶的自动提取规则（返回全交易所）
func (s *SQLStorage) ListProfitWithdrawRules(accountID string) ([]*ProfitWithdrawRule, error) {
	if accountID == "" {
		accountID = "default"
	}

	rows, err := s.db.Query(`
		SELECT id, account_id, exchange_id, strategy_id, enabled, trigger_amount, withdraw_ratio,
		       frequency, destination, wallet_address, min_withdraw_amount, max_withdraw_amount,
		       created_at, updated_at,
		       last_triggered_at
		FROM profit_withdraw_rules
		WHERE account_id = ?
		ORDER BY updated_at DESC
	`, accountID)
	if err != nil {
		return nil, fmt.Errorf("查詢 profit_withdraw_rules 失败: %w", err)
	}
	defer rows.Close()

	var out []*ProfitWithdrawRule
	for rows.Next() {
		r := &ProfitWithdrawRule{}
		var enabledInt int
		var walletAddr sql.NullString
		var maxAmt sql.NullFloat64
		var createdAt, updatedAt time.Time
		var lastTriggered sql.NullTime
		if err := rows.Scan(
			&r.ID,
			&r.AccountID,
			&r.ExchangeID,
			&r.StrategyID,
			&enabledInt,
			&r.TriggerAmount,
			&r.WithdrawRatio,
			&r.Frequency,
			&r.Destination,
			&walletAddr,
			&r.MinWithdrawAmount,
			&maxAmt,
			&createdAt,
			&updatedAt,
			&lastTriggered,
		); err != nil {
			continue
		}
		r.Enabled = enabledInt != 0
		if walletAddr.Valid {
			r.WalletAddress = walletAddr.String
		}
		if maxAmt.Valid {
			v := maxAmt.Float64
			r.MaxWithdrawAmount = &v
		}
		if lastTriggered.Valid {
			t := lastTriggered.Time
			r.LastTriggeredAt = &t
		}
		r.CreatedAt = createdAt
		r.UpdatedAt = updatedAt
		out = append(out, r)
	}
	return out, rows.Err()
}

// ReplaceProfitWithdrawRules 用一组规则替换指定账戶的全部规则（事務保证原子性）
func (s *SQLStorage) ReplaceProfitWithdrawRules(accountID string, rules []*ProfitWithdrawRule) error {
	if accountID == "" {
		accountID = "default"
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("开啟事務失败: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.Exec(`DELETE FROM profit_withdraw_rules WHERE account_id = ?`, accountID); err != nil {
		return fmt.Errorf("清空舊规则失败: %w", err)
	}

	now := utils.NowUTC()
	stmt, err := tx.Prepare(`
		INSERT INTO profit_withdraw_rules
		(id, account_id, exchange_id, strategy_id, enabled, trigger_amount, withdraw_ratio,
		 frequency, destination, wallet_address, min_withdraw_amount, max_withdraw_amount,
		 created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("准备插入语句失败: %w", err)
	}
	defer stmt.Close()

	for _, r := range rules {
		if r == nil {
			continue
		}
		if r.ID == "" {
			r.ID = fmt.Sprintf("rule_%d", time.Now().UnixNano())
		}
		r.AccountID = accountID
		if r.CreatedAt.IsZero() {
			r.CreatedAt = now
		}
		r.UpdatedAt = now
		if r.ExchangeID == "" {
			return fmt.Errorf("exchange_id 不能為空")
		}

		var wallet interface{}
		if r.WalletAddress != "" {
			wallet = r.WalletAddress
		}
		var maxAmt interface{}
		if r.MaxWithdrawAmount != nil {
			maxAmt = *r.MaxWithdrawAmount
		}

		enabledInt := 0
		if r.Enabled {
			enabledInt = 1
		}

		if _, err := stmt.Exec(
			r.ID,
			r.AccountID,
			r.ExchangeID,
			r.StrategyID,
			enabledInt,
			r.TriggerAmount,
			r.WithdrawRatio,
			r.Frequency,
			r.Destination,
			wallet,
			r.MinWithdrawAmount,
			maxAmt,
			utils.ToUTC(r.CreatedAt),
			utils.ToUTC(r.UpdatedAt),
		); err != nil {
			return fmt.Errorf("插入规则失败: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事務失败: %w", err)
	}
	return nil
}

// UpsertProfitWithdrawRule 創建或更新單条规则
func (s *SQLStorage) UpsertProfitWithdrawRule(accountID string, rule *ProfitWithdrawRule) error {
	if rule == nil {
		return fmt.Errorf("rule 不能為空")
	}
	if accountID == "" {
		accountID = "default"
	}
	if rule.ExchangeID == "" {
		return fmt.Errorf("exchange_id 不能為空")
	}
	if rule.ID == "" {
		rule.ID = fmt.Sprintf("rule_%d", time.Now().UnixNano())
	}

	now := utils.NowUTC()
	if rule.CreatedAt.IsZero() {
		rule.CreatedAt = now
	}
	rule.UpdatedAt = now
	rule.AccountID = accountID

	enabledInt := 0
	if rule.Enabled {
		enabledInt = 1
	}
	var wallet interface{}
	if rule.WalletAddress != "" {
		wallet = rule.WalletAddress
	}
	var maxAmt interface{}
	if rule.MaxWithdrawAmount != nil {
		maxAmt = *rule.MaxWithdrawAmount
	}

	_, err := s.db.Exec(`
		INSERT INTO profit_withdraw_rules
		(id, account_id, exchange_id, strategy_id, enabled, trigger_amount, withdraw_ratio,
		 frequency, destination, wallet_address, min_withdraw_amount, max_withdraw_amount,
		 created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
		  exchange_id=excluded.exchange_id,
		  strategy_id=excluded.strategy_id,
		  enabled=excluded.enabled,
		  trigger_amount=excluded.trigger_amount,
		  withdraw_ratio=excluded.withdraw_ratio,
		  frequency=excluded.frequency,
		  destination=excluded.destination,
		  wallet_address=excluded.wallet_address,
		  min_withdraw_amount=excluded.min_withdraw_amount,
		  max_withdraw_amount=excluded.max_withdraw_amount,
		  updated_at=excluded.updated_at
	`, rule.ID, rule.AccountID, rule.ExchangeID, rule.StrategyID, enabledInt,
		rule.TriggerAmount, rule.WithdrawRatio, rule.Frequency, rule.Destination, wallet,
		rule.MinWithdrawAmount, maxAmt, utils.ToUTC(rule.CreatedAt), utils.ToUTC(rule.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("upsert profit_withdraw_rules 失败: %w", err)
	}
	return nil
}

// DeleteProfitWithdrawRule 刪除單条规则（按账戶隔离）
func (s *SQLStorage) DeleteProfitWithdrawRule(accountID string, ruleID string) error {
	if accountID == "" {
		accountID = "default"
	}
	if ruleID == "" {
		return fmt.Errorf("ruleID 不能為空")
	}
	_, err := s.db.Exec(`DELETE FROM profit_withdraw_rules WHERE account_id = ? AND id = ?`, accountID, ruleID)
	if err != nil {
		return fmt.Errorf("刪除 profit_withdraw_rules 失败: %w", err)
	}
	return nil
}

// UpdateRuleLastTriggeredAt 更新规则最后執行時间
func (s *SQLStorage) UpdateRuleLastTriggeredAt(ruleID string, triggeredAt time.Time) error {
	_, err := s.db.Exec(`UPDATE profit_withdraw_rules SET last_triggered_at = ?, updated_at = ? WHERE id = ?`,
		triggeredAt, time.Now(), ruleID)
	if err != nil {
		return fmt.Errorf("更新规则 last_triggered_at 失败: %w", err)
	}
	return nil
}

// SaveWithdrawRecord 保存提取記錄
func (s *SQLStorage) SaveWithdrawRecord(record *ProfitWithdrawRecord) error {
	_, err := s.db.Exec(`
		INSERT INTO profit_withdraw_records (id, rule_id, account_id, exchange_id, strategy_id, amount, fee, net_amount, currency, type, status, destination, transfer_id, created_at, completed_at, failed_reason, note)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.RuleID, record.AccountID, record.ExchangeID, record.StrategyID,
		record.Amount, record.Fee, record.NetAmount, record.Currency, record.Type, record.Status, record.Destination,
		record.TransferID, record.CreatedAt, nil, record.FailedReason, record.Note)
	if err != nil {
		return fmt.Errorf("保存 profit_withdraw_records 失败: %w", err)
	}
	return nil
}

// UpdateWithdrawRecordStatus 更新提取記錄状態
func (s *SQLStorage) UpdateWithdrawRecordStatus(id, status, transferID, failedReason string) error {
	var completedAt interface{}
	if status == "completed" || status == "failed" {
		completedAt = time.Now()
	} else {
		completedAt = nil
	}
	_, err := s.db.Exec(`
		UPDATE profit_withdraw_records SET status = ?, transfer_id = ?, failed_reason = ?, completed_at = ? WHERE id = ?`,
		status, transferID, failedReason, completedAt, id)
	if err != nil {
		return fmt.Errorf("更新 profit_withdraw_records 状態失败: %w", err)
	}
	return nil
}

// GetWithdrawRecords 查詢提取記錄（按創建時间倒序）
func (s *SQLStorage) GetWithdrawRecords(accountID string, limit int) ([]*ProfitWithdrawRecord, error) {
	if accountID == "" {
		accountID = "default"
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := s.db.Query(`
		SELECT id, rule_id, account_id, exchange_id, strategy_id, amount, fee, net_amount, currency, type, status, destination, transfer_id, created_at, completed_at, failed_reason, note
		FROM profit_withdraw_records WHERE account_id = ? ORDER BY created_at DESC LIMIT ?`, accountID, limit)
	if err != nil {
		return nil, fmt.Errorf("查詢 profit_withdraw_records 失败: %w", err)
	}
	defer rows.Close()

	var out []*ProfitWithdrawRecord
	for rows.Next() {
		r := &ProfitWithdrawRecord{}
		var completedAt sql.NullTime
		var transferID, failedReason, note sql.NullString
		if err := rows.Scan(
			&r.ID, &r.RuleID, &r.AccountID, &r.ExchangeID, &r.StrategyID,
			&r.Amount, &r.Fee, &r.NetAmount, &r.Currency, &r.Type, &r.Status, &r.Destination,
			&transferID, &r.CreatedAt, &completedAt, &failedReason, &note,
		); err != nil {
			continue
		}
		if transferID.Valid {
			r.TransferID = transferID.String
		}
		if completedAt.Valid {
			t := completedAt.Time
			r.CompletedAt = &t
		}
		if failedReason.Valid {
			r.FailedReason = failedReason.String
		}
		if note.Valid {
			r.Note = note.String
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
