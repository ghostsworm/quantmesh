package storage

import (
	"database/sql"
	"fmt"
	"math"
	"strings"
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
			// 漏掉帳戶會讓該帳戶的提盈規則被靜默跳過不執行
			return nil, fmt.Errorf("解析提盈帳戶 ID 失败: %w", err)
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
		SELECT id, account_id, account_scope, exchange_id, strategy_id, enabled, trigger_amount, withdraw_ratio,
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
			&r.AccountScope,
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

	if _, err := tx.Exec(`DELETE FROM profit_withdraw_rules WHERE account_id = ? AND COALESCE(claim_id, '') = ''`, accountID); err != nil {
		return fmt.Errorf("清空舊规则失败: %w", err)
	}
	var activeClaims int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM profit_withdraw_rules WHERE account_id = ? AND COALESCE(claim_id, '') <> ''`, accountID).Scan(&activeClaims); err != nil {
		return fmt.Errorf("检查规则执行状态失败: %w", err)
	}
	if activeClaims > 0 {
		return fmt.Errorf("账户存在正在执行或待核实的提取规则，拒绝替换")
	}

	now := utils.NowUTC()
	stmt, err := tx.Prepare(`
		INSERT INTO profit_withdraw_rules
		(id, account_id, account_scope, exchange_id, strategy_id, enabled, trigger_amount, withdraw_ratio,
		 frequency, destination, wallet_address, min_withdraw_amount, max_withdraw_amount,
		 created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
		if r.AccountScope == "" {
			return fmt.Errorf("account_scope 不能為空")
		}
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
			r.AccountScope,
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
	if rule.AccountScope == "" {
		return fmt.Errorf("account_scope 不能為空")
	}

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

	result, err := s.db.Exec(`
		INSERT INTO profit_withdraw_rules
		(id, account_id, account_scope, exchange_id, strategy_id, enabled, trigger_amount, withdraw_ratio,
		 frequency, destination, wallet_address, min_withdraw_amount, max_withdraw_amount,
		 created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
		  account_scope=excluded.account_scope,
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
		WHERE COALESCE(profit_withdraw_rules.claim_id, '') = ''
	`, rule.ID, rule.AccountID, rule.AccountScope, rule.ExchangeID, rule.StrategyID, enabledInt,
		rule.TriggerAmount, rule.WithdrawRatio, rule.Frequency, rule.Destination, wallet,
		rule.MinWithdrawAmount, maxAmt, utils.ToUTC(rule.CreatedAt), utils.ToUTC(rule.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("upsert profit_withdraw_rules 失败: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil {
		return fmt.Errorf("读取提取规则写入结果失败: %w", rowsErr)
	} else if rows == 0 {
		return fmt.Errorf("规则正在执行或等待划转核账，拒绝修改")
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
	result, err := s.db.Exec(`DELETE FROM profit_withdraw_rules WHERE account_id = ? AND id = ? AND COALESCE(claim_id, '') = ''`, accountID, ruleID)
	if err != nil {
		return fmt.Errorf("刪除 profit_withdraw_rules 失败: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil {
		return fmt.Errorf("读取提取规则删除结果失败: %w", rowsErr)
	} else if rows == 0 {
		return fmt.Errorf("规则正在执行或等待划转核账，拒绝删除")
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

// ClaimProfitWithdrawRule atomically grants a durable single-runner claim.
func (s *SQLStorage) ClaimProfitWithdrawRule(ruleID, claimID string) (bool, error) {
	if ruleID == "" || claimID == "" {
		return false, fmt.Errorf("ruleID 和 claimID 不能為空")
	}
	result, err := s.db.Exec(`UPDATE profit_withdraw_rules SET claim_id = ? WHERE id = ? AND COALESCE(claim_id, '') = ''`, claimID, ruleID)
	if err != nil {
		return false, fmt.Errorf("认领利润提取规则失败 rule=%s: %w", ruleID, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("读取规则认领结果失败 rule=%s: %w", ruleID, err)
	}
	return rows == 1, nil
}

// ReleaseProfitWithdrawRuleClaim releases only the caller's own claim.
func (s *SQLStorage) ReleaseProfitWithdrawRuleClaim(ruleID, claimID string) error {
	result, err := s.db.Exec(`UPDATE profit_withdraw_rules SET claim_id = '' WHERE id = ? AND claim_id = ?`, ruleID, claimID)
	if err != nil {
		return fmt.Errorf("释放利润提取规则认领失败 rule=%s: %w", ruleID, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("读取规则认领释放结果失败 rule=%s: %w", ruleID, err)
	}
	if rows != 1 {
		return fmt.Errorf("规则认领已变化，拒绝释放 rule=%s", ruleID)
	}
	return nil
}

// SaveWithdrawRecord 保存提取記錄
func (s *SQLStorage) SaveWithdrawRecord(record *ProfitWithdrawRecord) error {
	_, err := s.db.Exec(`
		INSERT INTO profit_withdraw_records (id, rule_id, account_id, account_scope, claim_id, exchange_id, strategy_id, amount, fee, net_amount, currency, type, status, destination, transfer_id, created_at, completed_at, failed_reason, note)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.RuleID, record.AccountID, record.AccountScope, record.ClaimID, record.ExchangeID, record.StrategyID,
		record.Amount, record.Fee, record.NetAmount, record.Currency, record.Type, record.Status, record.Destination,
		record.TransferID, record.CreatedAt, nil, record.FailedReason, record.Note)
	if err != nil {
		return fmt.Errorf("保存 profit_withdraw_records 失败: %w", err)
	}
	return nil
}

func (s *SQLStorage) GetWithdrawRecord(accountID, recordID string) (*ProfitWithdrawRecord, error) {
	if accountID == "" {
		accountID = "default"
	}
	if recordID == "" {
		return nil, fmt.Errorf("recordID 不能為空")
	}
	row := s.db.QueryRow(`SELECT id, rule_id, account_id, account_scope, claim_id, exchange_id, strategy_id, amount, fee, net_amount, currency, type, status, destination, transfer_id, created_at, completed_at, failed_reason, note FROM profit_withdraw_records WHERE account_id = ? AND id = ?`, accountID, recordID)
	return scanProfitWithdrawRecord(row)
}

// ResolvePendingWithdrawRecord records an operator's ledger-based decision and
// atomically releases only the claim carried by that pending transfer record.
func (s *SQLStorage) ResolvePendingWithdrawRecord(accountID, recordID, outcome, reference, evidence string) error {
	if accountID == "" {
		accountID = "default"
	}
	if outcome != "completed" && outcome != "failed" {
		return fmt.Errorf("核账结果必须是 completed 或 failed")
	}
	if strings.TrimSpace(reference) == "" || len(strings.TrimSpace(evidence)) < 24 {
		return fmt.Errorf("必须提供交易所流水参考和至少 24 字的核账依据")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("开始提取核账事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var ruleID, claimID, note string
	staleProcessingBefore := time.Now().Add(-10 * time.Minute)
	if err := tx.QueryRow(`SELECT rule_id, claim_id, COALESCE(note, '') FROM profit_withdraw_records WHERE account_id = ? AND id = ? AND (status = 'pending' OR (status = 'processing' AND created_at <= ?))`, accountID, recordID, staleProcessingBefore).Scan(&ruleID, &claimID, &note); err != nil {
		return fmt.Errorf("仅可核账本账户 pending 或超过十分钟的 processing 提取记录: %w", err)
	}
	resolvedNote := strings.TrimSpace(note + "\n核账结果=" + outcome + "; 流水参考=" + strings.TrimSpace(reference) + "; 核账依据=" + strings.TrimSpace(evidence))
	failedReason := ""
	if outcome == "failed" {
		failedReason = "人工核账确认交易所流水未发生划转：" + strings.TrimSpace(evidence)
	}
	result, err := tx.Exec(`UPDATE profit_withdraw_records SET status = ?, transfer_id = ?, failed_reason = ?, note = ?, completed_at = ? WHERE account_id = ? AND id = ? AND (status = 'pending' OR (status = 'processing' AND created_at <= ?))`, outcome, reference, failedReason, resolvedNote, time.Now(), accountID, recordID, staleProcessingBefore)
	if err != nil {
		return fmt.Errorf("更新提取核账状态失败: %w", err)
	}
	if rows, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("读取提取核账结果失败: %w", err)
	} else if rows != 1 {
		return fmt.Errorf("提取记录已被其他操作处理")
	}
	if claimID != "" {
		result, err := tx.Exec(`UPDATE profit_withdraw_rules SET claim_id = '' WHERE id = ? AND claim_id = ?`, ruleID, claimID)
		if err != nil {
			return fmt.Errorf("释放对应自动提取规则 claim 失败: %w", err)
		}
		if rows, err := result.RowsAffected(); err != nil {
			return fmt.Errorf("读取自动提取规则 claim 结果失败: %w", err)
		} else if rows != 1 {
			return fmt.Errorf("自动提取规则 claim 与核账记录不一致，事务已回滚")
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交提取核账事务失败: %w", err)
	}
	return nil
}

type withdrawRecordScanner interface {
	Scan(dest ...interface{}) error
}

func scanProfitWithdrawRecord(row withdrawRecordScanner) (*ProfitWithdrawRecord, error) {
	record := &ProfitWithdrawRecord{}
	var transferID, failedReason, note sql.NullString
	var completedAt sql.NullTime
	err := row.Scan(&record.ID, &record.RuleID, &record.AccountID, &record.AccountScope, &record.ClaimID,
		&record.ExchangeID, &record.StrategyID, &record.Amount, &record.Fee, &record.NetAmount,
		&record.Currency, &record.Type, &record.Status, &record.Destination, &transferID,
		&record.CreatedAt, &completedAt, &failedReason, &note)
	if err != nil {
		return nil, fmt.Errorf("读取提取记录失败: %w", err)
	}
	if transferID.Valid {
		record.TransferID = transferID.String
	}
	if completedAt.Valid {
		t := completedAt.Time
		record.CompletedAt = &t
	}
	if failedReason.Valid {
		record.FailedReason = failedReason.String
	}
	if note.Valid {
		record.Note = note.String
	}
	return record, nil
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
		SELECT id, rule_id, account_id, account_scope, claim_id, exchange_id, strategy_id, amount, fee, net_amount, currency, type, status, destination, transfer_id, created_at, completed_at, failed_reason, note
		FROM profit_withdraw_records WHERE account_id = ? ORDER BY created_at DESC LIMIT ?`, accountID, limit)
	if err != nil {
		return nil, fmt.Errorf("查詢 profit_withdraw_records 失败: %w", err)
	}
	defer rows.Close()

	var out []*ProfitWithdrawRecord
	for rows.Next() {
		r, err := scanProfitWithdrawRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SumReservedWithdrawAmount aggregates every non-released transfer reservation
// for one rule and time window; unlike a paginated history read it cannot omit
// older pending transfers when the account has many records.
func (s *SQLStorage) SumReservedWithdrawAmount(accountID, ruleID string, since time.Time) (float64, error) {
	if accountID == "" {
		accountID = "default"
	}
	if ruleID == "" {
		return 0, fmt.Errorf("ruleID 不能為空")
	}
	var total float64
	err := s.db.QueryRow(`
		SELECT COALESCE(SUM(amount), 0)
		FROM profit_withdraw_records
		WHERE account_id = ? AND rule_id = ? AND created_at > ?
		  AND status NOT IN ('failed', 'cancelled')
	`, accountID, ruleID, since).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("汇总提取预留金额 rule=%s account=%s: %w", ruleID, accountID, err)
	}
	if math.IsNaN(total) || math.IsInf(total, 0) || total < 0 {
		return 0, fmt.Errorf("提取预留金额无效 rule=%s account=%s", ruleID, accountID)
	}
	return total, nil
}
