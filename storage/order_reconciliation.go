package storage

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"quantmesh/accounting"
)

//go:embed migrations/2026101001_order_reconciliation_*.sql
var orderReconciliationMigrations embed.FS

type OrderReconciliationStatus string

const (
	OrderReconciliationPrepared       OrderReconciliationStatus = "prepared"
	OrderReconciliationConfirmed      OrderReconciliationStatus = "confirmed"
	OrderReconciliationRunning        OrderReconciliationStatus = "reconciling"
	OrderReconciliationReady          OrderReconciliationStatus = "ready_to_release"
	OrderReconciliationReleasePending OrderReconciliationStatus = "release_pending"
	OrderReconciliationReleased       OrderReconciliationStatus = "released"
	OrderReconciliationUnverified     OrderReconciliationStatus = "unverified"
)

type OrderReconciliationCase struct {
	ID                       string                       `json:"case_id"`
	IdempotencyKey           string                       `json:"idempotency_key"`
	Owner                    accounting.OrderOwner        `json:"owner"`
	ExpectedIntentRevision   string                       `json:"expected_intent_revision"`
	ExpectedStrategyRevision string                       `json:"expected_strategy_revision"`
	Reason                   string                       `json:"reason"`
	EvidenceRefs             []string                     `json:"evidence_refs"`
	Status                   OrderReconciliationStatus    `json:"status"`
	Revision                 int64                        `json:"revision"`
	OperationRequest         *accounting.ReconcileRequest `json:"-"`
	Receipt                  *OrderReconciliationReceipt  `json:"receipt,omitempty"`
	OperationEvidenceHash    string                       `json:"-"`
	ReleaseEvidenceHash      string                       `json:"release_evidence_hash,omitempty"`
	CreatedBy                string                       `json:"created_by"`
	CreatedAt                time.Time                    `json:"created_at"`
	UpdatedAt                time.Time                    `json:"updated_at"`
}

// OrderReconciliationReceipt distinguishes an applied operation from a
// readback-only confirmation that the exact economic result was already durable.
type OrderReconciliationReceipt struct {
	Outcome          string                     `json:"outcome"`
	OperationID      string                     `json:"operation_id"`
	Owner            accounting.OrderOwner      `json:"owner"`
	ExpectedRevision string                     `json:"expected_strategy_revision"`
	From             accounting.Cursor          `json:"from"`
	Target           accounting.Cursor          `json:"target"`
	Fills            []accounting.VerifiedFill  `json:"-"`
	Fees             []accounting.VerifiedFee   `json:"-"`
	Evidence         accounting.EvidenceSummary `json:"-"`
	Revision         string                     `json:"revision"`
	Result           accounting.EconomicResult  `json:"result"`
	CompletedAt      time.Time                  `json:"completed_at"`
}

func (r OrderReconciliationReceipt) Validate() error {
	if r.Outcome == "reconciled" {
		return (accounting.ReconciliationReceipt{OperationID: r.OperationID, Owner: r.Owner, ExpectedRevision: r.ExpectedRevision, From: r.From, Target: r.Target, Fills: r.Fills, Fees: r.Fees, Evidence: r.Evidence, Revision: r.Revision, Result: r.Result, CompletedAt: r.CompletedAt}).Validate()
	}
	if r.Outcome != "already_accounted" || r.OperationID == "" || r.ExpectedRevision == "" || r.Revision == "" || r.From != r.Target || r.Target.Validate() != nil || r.Target.Sequence == 0 || r.CompletedAt.IsZero() {
		return fmt.Errorf("invalid reconciliation receipt outcome")
	}
	if err := r.Owner.Validate(); err != nil {
		return err
	}
	if err := (accounting.AccountingSnapshot{Owner: r.Owner, Revision: r.Revision, Cursor: r.Target, Result: r.Result, ReadAt: r.CompletedAt}).Validate(); err != nil {
		return err
	}
	fullHistory := accounting.ReconcileRequest{OperationID: r.OperationID, Owner: r.Owner, ExpectedRevision: r.ExpectedRevision, From: accounting.Cursor{}, Target: r.Target, Fills: r.Fills, Fees: r.Fees, Evidence: r.Evidence}
	return fullHistory.Validate()
}

type OrderReconciliationAudit struct {
	ID        string          `json:"audit_id"`
	CaseID    string          `json:"case_id"`
	Revision  int64           `json:"case_revision"`
	Action    string          `json:"action"`
	Actor     string          `json:"actor"`
	Outcome   string          `json:"outcome"`
	Detail    json.RawMessage `json:"detail"`
	CreatedAt time.Time       `json:"created_at"`
}

type NewOrderReconciliationCase struct {
	ID, IdempotencyKey       string
	Owner                    accounting.OrderOwner
	ExpectedIntentRevision   string
	ExpectedStrategyRevision string
	Reason, Actor            string
	EvidenceRefs             []string
}

// MigrateOrderReconciliations installs the additive SQLite/MySQL schema.
func (s *SQLStorage) MigrateOrderReconciliations(ctx context.Context) error {
	if s == nil || s.db == nil || (s.dbType != "sqlite" && s.dbType != "mysql") {
		return fmt.Errorf("unsupported order reconciliation database")
	}
	body, err := orderReconciliationMigrations.ReadFile("migrations/2026101001_order_reconciliation_" + s.dbType + ".up.sql")
	if err != nil {
		return err
	}
	for _, statement := range strings.Split(string(body), ";") {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrate order reconciliations: %w", err)
		}
	}
	return s.migrateOrderReconciliationRevisionColumns(ctx)
}

func (s *SQLStorage) CreateOrderReconciliationCase(ctx context.Context, in NewOrderReconciliationCase) (OrderReconciliationCase, bool, error) {
	if err := in.Owner.Validate(); err != nil {
		return OrderReconciliationCase{}, false, err
	}
	if strings.TrimSpace(in.ID) == "" || strings.TrimSpace(in.IdempotencyKey) == "" || len(in.IdempotencyKey) > 128 || strings.TrimSpace(in.ExpectedIntentRevision) == "" || strings.TrimSpace(in.ExpectedStrategyRevision) == "" || strings.TrimSpace(in.Reason) == "" || strings.TrimSpace(in.Actor) == "" || len(in.EvidenceRefs) == 0 {
		return OrderReconciliationCase{}, false, fmt.Errorf("invalid reconciliation case")
	}
	owner, _ := json.Marshal(in.Owner)
	refs, _ := json.Marshal(in.EvidenceRefs)
	if existing, found, readErr := s.GetOrderReconciliationByIdempotencyKey(ctx, in.IdempotencyKey); readErr != nil {
		return OrderReconciliationCase{}, false, readErr
	} else if found {
		if sameOrderCase(existing, in, owner, refs) {
			return existing, true, nil
		}
		return OrderReconciliationCase{}, false, ErrOrderReconciliationConflict
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return OrderReconciliationCase{}, false, err
	}
	defer tx.Rollback()
	query := `INSERT INTO order_reconciliation_cases (case_id,idempotency_key,owner_json,expected_revision,expected_intent_revision,expected_strategy_revision,reason,evidence_refs_json,status,revision,created_by) VALUES (?,?,?,?,?,?,?,?,?,?,?)`
	if _, err = tx.ExecContext(ctx, query, in.ID, in.IdempotencyKey, string(owner), "", in.ExpectedIntentRevision, in.ExpectedStrategyRevision, in.Reason, string(refs), string(OrderReconciliationPrepared), 1, in.Actor); err != nil {
		_ = tx.Rollback()
		if existing, found, readErr := s.GetOrderReconciliationByIdempotencyKey(ctx, in.IdempotencyKey); readErr == nil && found {
			if sameOrderCase(existing, in, owner, refs) {
				return existing, true, nil
			}
			return OrderReconciliationCase{}, false, ErrOrderReconciliationConflict
		}
		return OrderReconciliationCase{}, false, err
	}
	if err = insertOrderReconciliationAudit(ctx, tx, in.ID, 1, "case_created", in.Actor, "prepared", []byte(`{"evidence_verified":false}`)); err != nil {
		return OrderReconciliationCase{}, false, err
	}
	if err = tx.Commit(); err != nil {
		if existing, found, readErr := s.GetOrderReconciliationByIdempotencyKey(ctx, in.IdempotencyKey); readErr == nil && found {
			return existing, true, nil
		}
		return OrderReconciliationCase{}, false, err
	}
	created, _, err := s.GetOrderReconciliation(ctx, in.ID)
	return created, false, err
}

var ErrOrderReconciliationConflict = errors.New("order reconciliation state conflict")

func sameOrderCase(old OrderReconciliationCase, in NewOrderReconciliationCase, owner, refs []byte) bool {
	oldOwner, _ := json.Marshal(old.Owner)
	oldRefs, _ := json.Marshal(old.EvidenceRefs)
	return string(oldOwner) == string(owner) && old.ExpectedIntentRevision == in.ExpectedIntentRevision && old.ExpectedStrategyRevision == in.ExpectedStrategyRevision && old.Reason == in.Reason && string(oldRefs) == string(refs) && old.CreatedBy == in.Actor
}

func (s *SQLStorage) GetOrderReconciliation(ctx context.Context, id string) (OrderReconciliationCase, bool, error) {
	return s.scanOrderReconciliation(s.db.QueryRowContext(ctx, orderReconciliationSelect+` WHERE case_id=?`, id))
}
func (s *SQLStorage) GetOrderReconciliationByIdempotencyKey(ctx context.Context, key string) (OrderReconciliationCase, bool, error) {
	return s.scanOrderReconciliation(s.db.QueryRowContext(ctx, orderReconciliationSelect+` WHERE idempotency_key=?`, key))
}
func (s *SQLStorage) ListOrderReconciliations(ctx context.Context, limit int) ([]OrderReconciliationCase, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, orderReconciliationSelect+` ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]OrderReconciliationCase, 0)
	for rows.Next() {
		item, err := scanOrderReconciliationRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

type reconciliationScanner interface{ Scan(...any) error }

const orderReconciliationSelect = `SELECT case_id,idempotency_key,owner_json,expected_intent_revision,expected_strategy_revision,reason,evidence_refs_json,status,revision,operation_request_json,receipt_json,release_evidence_hash,operation_evidence_hash,created_by,created_at,updated_at FROM order_reconciliation_cases`

func (s *SQLStorage) scanOrderReconciliation(row *sql.Row) (OrderReconciliationCase, bool, error) {
	item, err := scanOrderReconciliationRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return OrderReconciliationCase{}, false, nil
	}
	return item, err == nil, err
}
func scanOrderReconciliationRow(row reconciliationScanner) (OrderReconciliationCase, error) {
	var item OrderReconciliationCase
	var ownerJSON, refsJSON, status string
	var operationRequest, receipt sql.NullString
	var released string
	err := row.Scan(&item.ID, &item.IdempotencyKey, &ownerJSON, &item.ExpectedIntentRevision, &item.ExpectedStrategyRevision, &item.Reason, &refsJSON, &status, &item.Revision, &operationRequest, &receipt, &released, &item.OperationEvidenceHash, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return item, err
	}
	if err = json.Unmarshal([]byte(ownerJSON), &item.Owner); err != nil {
		return item, err
	}
	if err = json.Unmarshal([]byte(refsJSON), &item.EvidenceRefs); err != nil {
		return item, err
	}
	item.Status = OrderReconciliationStatus(status)
	item.ReleaseEvidenceHash = released
	if operationRequest.Valid && strings.TrimSpace(operationRequest.String) != "" {
		var request accounting.ReconcileRequest
		if err = json.Unmarshal([]byte(operationRequest.String), &request); err != nil {
			return item, err
		}
		item.OperationRequest = &request
	}
	if receipt.Valid && strings.TrimSpace(receipt.String) != "" {
		var value OrderReconciliationReceipt
		if err = json.Unmarshal([]byte(receipt.String), &value); err != nil {
			return item, err
		}
		item.Receipt = &value
	}
	return item, nil
}

// StartOrderReconciliation durably binds the operator confirmation to the
// exact idempotent strategy operation before any strategy call can occur.
func (s *SQLStorage) StartOrderReconciliation(ctx context.Context, id string, expected int64, actor string, request accounting.ReconcileRequest, evidenceHash string) (OrderReconciliationCase, error) {
	if request.OperationID != id || actor == "" || len(evidenceHash) != 64 {
		return OrderReconciliationCase{}, fmt.Errorf("invalid reconciliation operation binding")
	}
	if err := request.Validate(); err != nil {
		return OrderReconciliationCase{}, err
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return OrderReconciliationCase{}, err
	}
	ownerJSON, err := json.Marshal(request.Owner)
	if err != nil {
		return OrderReconciliationCase{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return OrderReconciliationCase{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE order_reconciliation_cases SET status=?,revision=revision+1,operation_request_json=?,operation_evidence_hash=?,updated_at=CURRENT_TIMESTAMP WHERE case_id=? AND revision=? AND status=? AND owner_json=? AND expected_strategy_revision=? AND expected_intent_revision<>''`, string(OrderReconciliationRunning), string(payload), evidenceHash, id, expected, string(OrderReconciliationPrepared), string(ownerJSON), request.ExpectedRevision)
	if err != nil {
		return OrderReconciliationCase{}, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return OrderReconciliationCase{}, err
	}
	if n != 1 {
		return OrderReconciliationCase{}, ErrOrderReconciliationConflict
	}
	detail, _ := json.Marshal(map[string]string{"operation_id": request.OperationID, "expected_strategy_revision": request.ExpectedRevision})
	if err = insertOrderReconciliationAudit(ctx, tx, id, expected+1, "reconcile_confirmed", actor, "started", detail); err != nil {
		return OrderReconciliationCase{}, err
	}
	if err = tx.Commit(); err != nil {
		return OrderReconciliationCase{}, err
	}
	item, _, err := s.GetOrderReconciliation(ctx, id)
	return item, err
}

func (s *SQLStorage) TransitionOrderReconciliation(ctx context.Context, id string, expected int64, from, to OrderReconciliationStatus, actor, action, outcome string, detail []byte, receipt *OrderReconciliationReceipt, evidenceHash string) (OrderReconciliationCase, error) {
	if expected < 1 || actor == "" || action == "" || outcome == "" || !json.Valid(detail) || !validOrderReconciliationTransition(from, to) {
		return OrderReconciliationCase{}, fmt.Errorf("invalid reconciliation transition")
	}
	validReadySource := from == OrderReconciliationRunning && receipt != nil && receipt.Outcome == "reconciled" || from == OrderReconciliationPrepared && receipt != nil && receipt.Outcome == "already_accounted" || from == OrderReconciliationRunning && receipt != nil && receipt.Outcome == "already_accounted"
	if to == OrderReconciliationReady && (!validReadySource || receipt.OperationID != id || receipt.Validate() != nil || len(evidenceHash) != 64) {
		return OrderReconciliationCase{}, fmt.Errorf("verified receipt and evidence hash required")
	}
	if to == OrderReconciliationReleased && evidenceHash != "" {
		return OrderReconciliationCase{}, fmt.Errorf("release evidence is already bound to the case")
	}
	var receiptJSON any
	if receipt != nil {
		b, err := json.Marshal(receipt)
		if err != nil {
			return OrderReconciliationCase{}, err
		}
		receiptJSON = string(b)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return OrderReconciliationCase{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE order_reconciliation_cases SET status=?, revision=revision+1, receipt_json=COALESCE(?,receipt_json), release_evidence_hash=CASE WHEN ?='' THEN release_evidence_hash ELSE ? END, updated_at=CURRENT_TIMESTAMP WHERE case_id=? AND revision=? AND status=?`, string(to), receiptJSON, evidenceHash, evidenceHash, id, expected, string(from))
	if err != nil {
		return OrderReconciliationCase{}, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return OrderReconciliationCase{}, err
	}
	if n != 1 {
		return OrderReconciliationCase{}, ErrOrderReconciliationConflict
	}
	if err = insertOrderReconciliationAudit(ctx, tx, id, expected+1, action, actor, outcome, detail); err != nil {
		return OrderReconciliationCase{}, err
	}
	if err = tx.Commit(); err != nil {
		return OrderReconciliationCase{}, err
	}
	item, _, err := s.GetOrderReconciliation(ctx, id)
	return item, err
}

func validOrderReconciliationTransition(from, to OrderReconciliationStatus) bool {
	switch from {
	case OrderReconciliationRunning:
		return to == OrderReconciliationReady
	case OrderReconciliationPrepared:
		return to == OrderReconciliationReady
	case OrderReconciliationReady:
		return to == OrderReconciliationReleasePending
	case OrderReconciliationReleasePending:
		return to == OrderReconciliationReleased
	default:
		return false
	}
}

func (s *SQLStorage) migrateOrderReconciliationRevisionColumns(ctx context.Context) error {
	columns := []struct{ name, sqlite, mysql string }{
		{"expected_intent_revision", `ALTER TABLE order_reconciliation_cases ADD COLUMN expected_intent_revision TEXT NOT NULL DEFAULT ''`, `ALTER TABLE order_reconciliation_cases ADD COLUMN expected_intent_revision VARCHAR(191) NOT NULL DEFAULT ''`},
		{"expected_strategy_revision", `ALTER TABLE order_reconciliation_cases ADD COLUMN expected_strategy_revision TEXT NOT NULL DEFAULT ''`, `ALTER TABLE order_reconciliation_cases ADD COLUMN expected_strategy_revision VARCHAR(191) NOT NULL DEFAULT ''`},
		{"operation_evidence_hash", `ALTER TABLE order_reconciliation_cases ADD COLUMN operation_evidence_hash TEXT NOT NULL DEFAULT ''`, `ALTER TABLE order_reconciliation_cases ADD COLUMN operation_evidence_hash VARCHAR(128) NOT NULL DEFAULT ''`},
	}
	for _, column := range columns {
		present, err := s.orderReconciliationColumnExists(ctx, column.name)
		if err != nil {
			return err
		}
		if present {
			continue
		}
		statement := column.sqlite
		if s.dbType == "mysql" {
			statement = column.mysql
		}
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("add order reconciliation column %s: %w", column.name, err)
		}
	}
	return nil
}

func (s *SQLStorage) orderReconciliationColumnExists(ctx context.Context, name string) (bool, error) {
	if s.dbType == "sqlite" {
		rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(order_reconciliation_cases)`)
		if err != nil {
			return false, err
		}
		defer rows.Close()
		for rows.Next() {
			var index, notNull, primaryKey int
			var column, dataType string
			var defaultValue any
			if err := rows.Scan(&index, &column, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
				return false, err
			}
			if column == name {
				return true, nil
			}
		}
		return false, rows.Err()
	}
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='order_reconciliation_cases' AND COLUMN_NAME=?`, name).Scan(&count)
	return count > 0, err
}

func insertOrderReconciliationAudit(ctx context.Context, tx *sql.Tx, caseID string, revision int64, action, actor, outcome string, detail []byte) error {
	if !json.Valid(detail) {
		return fmt.Errorf("invalid audit detail")
	}
	id := fmt.Sprintf("%d-%s-%s", time.Now().UTC().UnixNano(), caseID, action)
	_, err := tx.ExecContext(ctx, `INSERT INTO order_reconciliation_audit(audit_id,case_id,case_revision,action,actor,outcome,detail_json) VALUES(?,?,?,?,?,?,?)`, id, caseID, revision, action, actor, outcome, string(detail))
	return err
}
func (s *SQLStorage) ListOrderReconciliationAudit(ctx context.Context, id string) ([]OrderReconciliationAudit, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT audit_id,case_id,case_revision,action,actor,outcome,detail_json,created_at FROM order_reconciliation_audit WHERE case_id=? ORDER BY created_at,audit_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]OrderReconciliationAudit, 0)
	for rows.Next() {
		var item OrderReconciliationAudit
		var detail string
		if err := rows.Scan(&item.ID, &item.CaseID, &item.Revision, &item.Action, &item.Actor, &item.Outcome, &detail, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Detail = json.RawMessage(detail)
		out = append(out, item)
	}
	return out, rows.Err()
}
