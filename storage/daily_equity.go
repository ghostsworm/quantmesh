package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	sqlite3 "github.com/mattn/go-sqlite3"

	"quantmesh/accounting/daily"
)

type dailyEquityKey struct {
	exchange     string
	accountScope string
	localDate    string
}

type dailyEquityCurrentState struct {
	evidenceRevision int64
	evidenceHash     string
	reportRevision   int64
	reportDigest     string
	conflicted       bool
}

type dailyEquityEvidenceVersion struct {
	revision int64
	hash     string
	payload  string
}

type dailyEquityConflict struct {
	source              string
	sourceID            string
	wallet              string
	reason              string
	existingFingerprint string
	incomingFingerprint string
}

var _ daily.Repository = (*SQLStorage)(nil)

func normalizeDailyEquityQuery(query daily.Query) (daily.Query, dailyEquityKey, time.Time, time.Time, error) {
	normalized := daily.Query{
		Exchange:     strings.ToLower(strings.TrimSpace(query.Exchange)),
		AccountScope: strings.TrimSpace(query.AccountScope),
		Date:         strings.TrimSpace(query.Date),
	}
	if normalized.Exchange == "" || normalized.AccountScope == "" || normalized.Date == "" {
		return daily.Query{}, dailyEquityKey{}, time.Time{}, time.Time{}, fmt.Errorf("%w: daily equity query requires exchange, account scope and date", daily.ErrInvalid)
	}
	start, end, err := daily.DayBounds(normalized.Date)
	if err != nil {
		return daily.Query{}, dailyEquityKey{}, time.Time{}, time.Time{}, err
	}
	key := dailyEquityKey{exchange: normalized.Exchange, accountScope: normalized.AccountScope, localDate: normalized.Date}
	return normalized, key, start, end, nil
}

// ImportDailyEquityEvidence persists one immutable evidence revision. It is
// idempotent for an identical payload, CAS-guarded for a changed payload and
// fails closed when a previously imported source identity changes or vanishes.
func (s *SQLStorage) ImportDailyEquityEvidence(ctx context.Context, evidence daily.Evidence, expectedRevision int64) (int64, error) {
	if err := s.dailyEquityValidateBackend(); err != nil {
		return 0, err
	}
	if expectedRevision < 0 {
		return 0, fmt.Errorf("%w: expected evidence revision must not be negative", daily.ErrInvalid)
	}
	if evidence.Timezone == "" {
		evidence.Timezone = daily.Timezone
	}
	if evidence.Timezone != daily.Timezone {
		return 0, fmt.Errorf("%w: unsupported daily equity timezone %q", daily.ErrInvalid, evidence.Timezone)
	}
	if evidence.Method == "" {
		evidence.Method = daily.MethodVersion
	}
	if evidence.Method != daily.MethodVersion {
		return 0, fmt.Errorf("%w: unsupported daily equity method %q", daily.ErrInvalid, evidence.Method)
	}
	prepared, err := daily.PrepareEvidence(evidence)
	if err != nil {
		if errors.Is(err, daily.ErrConflict) {
			if persistErr := s.recordDailyEquityPayloadConflict(ctx, evidence); persistErr != nil {
				return 0, errors.Join(err, persistErr)
			}
		}
		return 0, err
	}
	// The API and service queries normalize the exchange to lower case. Persist
	// the same canonical identity so Service.matchQuery remains an exact check.
	prepared.Scope.Exchange = strings.ToLower(strings.TrimSpace(prepared.Scope.Exchange))
	prepared.Scope.AccountScope = strings.TrimSpace(prepared.Scope.AccountScope)
	prepared.Date = strings.TrimSpace(prepared.Date)
	exchange := strings.ToLower(strings.TrimSpace(prepared.Scope.Exchange))
	accountScope := strings.TrimSpace(prepared.Scope.AccountScope)
	date := strings.TrimSpace(prepared.Date)
	if exchange == "" || accountScope == "" {
		return 0, fmt.Errorf("%w: daily equity evidence requires exchange and account scope", daily.ErrInvalid)
	}
	if prepared.Scope.Revision < 0 {
		return 0, fmt.Errorf("%w: daily equity scope revision must not be negative", daily.ErrInvalid)
	}
	if _, _, err := daily.DayBounds(date); err != nil {
		return 0, err
	}
	hash, err := daily.EvidenceHash(prepared)
	if err != nil {
		return 0, err
	}
	payload, err := json.Marshal(prepared)
	if err != nil {
		return 0, fmt.Errorf("%w: encode daily equity evidence: %v", daily.ErrInvalid, err)
	}
	key := dailyEquityKey{exchange: exchange, accountScope: accountScope, localDate: date}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin daily equity evidence import: %w", err)
	}
	defer tx.Rollback()

	versions, err := s.readDailyEquityEvidenceVersions(ctx, tx, key)
	if err != nil {
		return 0, err
	}
	current, found, err := s.readDailyEquityCurrent(ctx, tx, key, true)
	if err != nil {
		return 0, err
	}
	currentRevision := int64(0)
	currentHash := ""
	if len(versions) > 0 {
		currentRevision = versions[len(versions)-1].revision
		currentHash = versions[len(versions)-1].hash
	}
	if found {
		if current.evidenceRevision > currentRevision {
			return 0, fmt.Errorf("daily equity current pointer exceeds persisted evidence revisions")
		}
		currentRevision = current.evidenceRevision
		currentHash = current.evidenceHash
	}
	if currentHash == hash {
		if err := tx.Commit(); err != nil {
			return 0, fmt.Errorf("commit idempotent daily equity evidence replay: %w", err)
		}
		return currentRevision, nil
	}
	if expectedRevision != currentRevision {
		return 0, fmt.Errorf("%w: expected evidence revision %d does not match current %d", daily.ErrConflict, expectedRevision, currentRevision)
	}
	conflicts, err := dailyEquityEvidenceConflicts(versions, prepared)
	if err != nil {
		return 0, err
	}
	if len(conflicts) > 0 {
		for _, conflict := range conflicts {
			if err := s.recordDailyEquityConflict(ctx, tx, key, conflict); err != nil {
				return 0, err
			}
		}
		if err := s.markDailyEquityConflicted(ctx, tx, key, current); err != nil {
			return 0, err
		}
		if err := tx.Commit(); err != nil {
			return 0, fmt.Errorf("commit daily equity evidence conflict: %w", err)
		}
		return 0, fmt.Errorf("%w: daily equity evidence conflicts with %d previously imported source identities", daily.ErrConflict, len(conflicts))
	}

	newRevision := currentRevision + 1
	if _, err := tx.ExecContext(ctx, `INSERT INTO daily_equity_evidence_versions
		(exchange, account_scope, local_date, revision, scope_revision, timezone, method, evidence_hash, payload_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		exchange, accountScope, date, newRevision, prepared.Scope.Revision, prepared.Timezone, prepared.Method, hash, string(payload)); err != nil {
		if isDailyEquityUniqueViolation(err) {
			return 0, fmt.Errorf("%w: concurrent daily equity evidence revision", daily.ErrConflict)
		}
		return 0, fmt.Errorf("insert daily equity evidence revision: %w", err)
	}
	if err := s.upsertDailyEquityEvidencePointer(ctx, tx, key, newRevision, hash); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit daily equity evidence revision: %w", err)
	}
	return newRevision, nil
}

// LoadDailyEquityInputs projects the current evidence revision plus the real
// fill/funding/interest ledgers and their coverage into the core input snapshot.
func (s *SQLStorage) LoadDailyEquityInputs(ctx context.Context, query daily.Query) (daily.Inputs, error) {
	if err := s.dailyEquityValidateBackend(); err != nil {
		return daily.Inputs{}, err
	}
	return s.loadDailyEquityInputs(ctx, s.db, query)
}

func (s *SQLStorage) loadDailyEquityInputs(ctx context.Context, q dailyEquityQuerier, query daily.Query) (daily.Inputs, error) {
	_, key, _, _, err := normalizeDailyEquityQuery(query)
	if err != nil {
		return daily.Inputs{}, err
	}
	current, found, err := s.readDailyEquityCurrent(ctx, q, key, false)
	if err != nil {
		return daily.Inputs{}, err
	}
	if !found || current.evidenceRevision == 0 {
		return daily.Inputs{}, fmt.Errorf("%w: no daily equity evidence for %s/%s/%s", daily.ErrNotFound, key.exchange, key.accountScope, key.localDate)
	}
	version, err := s.readDailyEquityEvidenceVersion(ctx, q, key, current.evidenceRevision)
	if err != nil {
		return daily.Inputs{}, err
	}
	var evidence daily.Evidence
	if err := json.Unmarshal([]byte(version.payload), &evidence); err != nil {
		return daily.Inputs{}, fmt.Errorf("decode daily equity evidence revision %d: %w", version.revision, err)
	}
	hash, err := daily.EvidenceHash(evidence)
	if err != nil {
		return daily.Inputs{}, fmt.Errorf("verify daily equity evidence revision %d: %w", version.revision, err)
	}
	if hash != version.hash {
		return daily.Inputs{}, fmt.Errorf("daily equity evidence revision %d hash mismatch", version.revision)
	}
	if !strings.EqualFold(strings.TrimSpace(evidence.Scope.Exchange), key.exchange) ||
		strings.TrimSpace(evidence.Scope.AccountScope) != key.accountScope ||
		strings.TrimSpace(evidence.Date) != key.localDate {
		return daily.Inputs{}, fmt.Errorf("%w: daily equity evidence scope does not match the query", daily.ErrInvalid)
	}
	entries, coverage, blockers, err := loadDailyEquityProjection(ctx, q, evidence)
	if err != nil {
		return daily.Inputs{}, err
	}
	if current.conflicted {
		blockers = append(blockers, "daily equity source conflict is persisted; the day cannot be reconciled")
		sort.Strings(blockers)
	}
	inputs := daily.Inputs{
		Evidence:         evidence,
		EvidenceRevision: version.revision,
		Entries:          entries,
		LedgerCoverage:   coverage,
		Blockers:         blockers,
	}
	digest, err := daily.InputDigest(inputs)
	if err != nil {
		return daily.Inputs{}, err
	}
	inputs.Digest = digest
	return inputs, nil
}

// CommitDailyEquityResult re-reads the complete input snapshot inside the
// publication transaction, verifies the caller's digest, and then either
// replays the existing report revision or inserts a new immutable one.
func (s *SQLStorage) CommitDailyEquityResult(ctx context.Context, query daily.Query, inputDigest string, result daily.Result) (daily.Result, error) {
	if err := s.dailyEquityValidateBackend(); err != nil {
		return daily.Result{}, err
	}
	if !dailyEquityValidDigest(inputDigest) {
		return daily.Result{}, fmt.Errorf("%w: daily equity commit requires a sha256 input digest", daily.ErrInvalid)
	}
	normalized, key, start, end, err := normalizeDailyEquityQuery(query)
	if err != nil {
		return daily.Result{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return daily.Result{}, fmt.Errorf("begin daily equity result commit: %w", err)
	}
	defer tx.Rollback()

	current, found, err := s.readDailyEquityCurrent(ctx, tx, key, true)
	if err != nil {
		return daily.Result{}, err
	}
	if !found || current.evidenceRevision == 0 {
		return daily.Result{}, fmt.Errorf("%w: no daily equity evidence for %s/%s/%s", daily.ErrNotFound, key.exchange, key.accountScope, key.localDate)
	}
	if current.conflicted {
		return daily.Result{}, fmt.Errorf("%w: daily equity evidence is conflicted", daily.ErrConflict)
	}
	inputs, err := s.loadDailyEquityInputs(ctx, tx, normalized)
	if err != nil {
		return daily.Result{}, err
	}
	if inputs.Digest != inputDigest {
		return daily.Result{}, fmt.Errorf("%w: daily equity inputs changed after evaluation", daily.ErrConflict)
	}
	if result.InputDigest != "" && result.InputDigest != inputDigest {
		return daily.Result{}, fmt.Errorf("%w: daily equity result digest does not match its inputs", daily.ErrInvalid)
	}
	if result.EquityReconciliationComplete && len(inputs.Blockers) > 0 {
		return daily.Result{}, fmt.Errorf("%w: daily equity cannot publish a reconciled result with blockers", daily.ErrInvalid)
	}
	if result.EquityReconciliationComplete {
		for _, entry := range inputs.Entries {
			if !entry.Known {
				return daily.Result{}, fmt.Errorf("%w: daily equity cannot publish a reconciled result with unknown ledger entries", daily.ErrInvalid)
			}
		}
		if len(result.Reasons) > 0 {
			return daily.Result{}, fmt.Errorf("%w: daily equity cannot publish a reconciled result with reasons", daily.ErrInvalid)
		}
		if result.ComputedAt.IsZero() {
			return daily.Result{}, fmt.Errorf("%w: daily equity reconciled result requires its computation time", daily.ErrInvalid)
		}
		// Re-evaluate the transaction snapshot so a caller cannot publish true
		// with a valid digest but a forged or stale completeness claim.
		evaluated, evalErr := daily.Evaluate(inputs, result.ComputedAt)
		if evalErr != nil {
			return daily.Result{}, fmt.Errorf("%w: re-evaluate daily equity inputs: %v", daily.ErrInvalid, evalErr)
		}
		if !evaluated.EquityReconciliationComplete || evaluated.InputDigest != inputDigest {
			return daily.Result{}, fmt.Errorf("%w: daily equity result claims reconciliation that its inputs do not reproduce", daily.ErrInvalid)
		}
		if evaluated.Amounts != result.Amounts {
			return daily.Result{}, fmt.Errorf("%w: daily equity result amounts do not match the evaluated inputs", daily.ErrInvalid)
		}
	}
	result, err = dailyEquityNormalizeResult(result, normalized, inputs, start, end)
	if err != nil {
		return daily.Result{}, err
	}

	existing, existingRevision, found, err := s.findDailyEquityReportByDigest(ctx, tx, key, inputDigest, result.Method)
	if err != nil {
		return daily.Result{}, err
	}
	if found {
		if err := s.upsertDailyEquityReportPointer(ctx, tx, key, current, existing.EvidenceRevision, existingRevision, inputDigest); err != nil {
			return daily.Result{}, err
		}
		if err := tx.Commit(); err != nil {
			return daily.Result{}, fmt.Errorf("commit daily equity report replay: %w", err)
		}
		return existing, nil
	}

	var maxRevision int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(revision), 0) FROM daily_equity_report_versions
		WHERE exchange = ? AND account_scope = ? AND local_date = ?`, key.exchange, key.accountScope, key.localDate).Scan(&maxRevision); err != nil {
		return daily.Result{}, fmt.Errorf("read daily equity report revision: %w", err)
	}
	reportRevision := maxRevision + 1
	result.EvidenceRevision = inputs.EvidenceRevision
	result.InputDigest = inputDigest
	reportJSON, err := json.Marshal(result)
	if err != nil {
		return daily.Result{}, fmt.Errorf("encode daily equity result: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO daily_equity_report_versions
		(exchange, account_scope, local_date, revision, evidence_revision, input_digest, method, equity_reconciliation_complete, report_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		key.exchange, key.accountScope, key.localDate, reportRevision, inputs.EvidenceRevision, inputDigest, result.Method,
		result.EquityReconciliationComplete, string(reportJSON)); err != nil {
		if isDailyEquityUniqueViolation(err) {
			return daily.Result{}, fmt.Errorf("%w: concurrent daily equity report revision", daily.ErrConflict)
		}
		return daily.Result{}, fmt.Errorf("insert daily equity report revision: %w", err)
	}
	if err := s.upsertDailyEquityReportPointer(ctx, tx, key, current, inputs.EvidenceRevision, reportRevision, inputDigest); err != nil {
		return daily.Result{}, err
	}
	if err := tx.Commit(); err != nil {
		return daily.Result{}, fmt.Errorf("commit daily equity report revision: %w", err)
	}
	return result, nil
}

// LoadDailyEquityResult returns the last published report. Callers that need
// to know whether its inputs are still current must reload LoadDailyEquityInputs
// and compare the digest (Service.Read does exactly that). A persisted source
// conflict can never leave the stored true visible as reconciled.
func (s *SQLStorage) LoadDailyEquityResult(ctx context.Context, query daily.Query) (daily.Result, error) {
	if err := s.dailyEquityValidateBackend(); err != nil {
		return daily.Result{}, err
	}
	_, key, _, _, err := normalizeDailyEquityQuery(query)
	if err != nil {
		return daily.Result{}, err
	}
	current, found, err := s.readDailyEquityCurrent(ctx, s.db, key, false)
	if err != nil {
		return daily.Result{}, err
	}
	if !found || current.reportRevision == 0 {
		return daily.Result{}, fmt.Errorf("%w: no daily equity report for %s/%s/%s", daily.ErrNotFound, key.exchange, key.accountScope, key.localDate)
	}
	report, found, err := s.readDailyEquityReport(ctx, s.db, key, current.reportRevision)
	if err != nil {
		return daily.Result{}, err
	}
	if !found {
		return daily.Result{}, fmt.Errorf("%w: daily equity report revision is missing", daily.ErrNotFound)
	}
	if current.conflicted {
		report.EquityReconciliationComplete = false
		found := false
		for _, reason := range report.Reasons {
			if reason == "source_changed" {
				found = true
				break
			}
		}
		if !found {
			report.Reasons = append(report.Reasons, "source_changed")
		}
	}
	return report, nil
}

func dailyEquityNormalizeResult(result daily.Result, query daily.Query, inputs daily.Inputs, start, end time.Time) (daily.Result, error) {
	evidence := inputs.Evidence
	if result.Query.Exchange != "" {
		result.Query.Exchange = strings.ToLower(strings.TrimSpace(result.Query.Exchange))
	}
	if result.Query.AccountScope != "" {
		result.Query.AccountScope = strings.TrimSpace(result.Query.AccountScope)
	}
	if result.Query.Date != "" {
		result.Query.Date = strings.TrimSpace(result.Query.Date)
	}
	if result.Query != (daily.Query{}) && result.Query != query {
		return daily.Result{}, fmt.Errorf("%w: daily equity result query does not match the requested scope", daily.ErrInvalid)
	}
	result.Query = query
	if result.Scope.Exchange == "" && result.Scope.AccountScope == "" {
		result.Scope = evidence.Scope
	} else if !strings.EqualFold(strings.TrimSpace(result.Scope.Exchange), strings.TrimSpace(evidence.Scope.Exchange)) ||
		strings.TrimSpace(result.Scope.AccountScope) != strings.TrimSpace(evidence.Scope.AccountScope) ||
		result.Scope.Revision != evidence.Scope.Revision {
		return daily.Result{}, fmt.Errorf("%w: daily equity result scope does not match its evidence", daily.ErrInvalid)
	}
	if result.Timezone == "" {
		result.Timezone = daily.Timezone
	}
	if result.Timezone != daily.Timezone {
		return daily.Result{}, fmt.Errorf("%w: daily equity result timezone mismatch", daily.ErrInvalid)
	}
	if result.Method == "" {
		result.Method = daily.MethodVersion
	}
	if result.Method != daily.MethodVersion {
		return daily.Result{}, fmt.Errorf("%w: daily equity result method mismatch", daily.ErrInvalid)
	}
	if result.Asset == "" {
		result.Asset = daily.Asset
	}
	if result.Asset != daily.Asset {
		return daily.Result{}, fmt.Errorf("%w: daily equity result asset mismatch", daily.ErrInvalid)
	}
	if result.PeriodStart.IsZero() {
		result.PeriodStart = start
	}
	if result.PeriodEnd.IsZero() {
		result.PeriodEnd = end
	}
	if !result.PeriodStart.Equal(start) || !result.PeriodEnd.Equal(end) {
		return daily.Result{}, fmt.Errorf("%w: daily equity result period does not match the day", daily.ErrInvalid)
	}
	if result.Revision == 0 {
		result.Revision = evidence.Scope.Revision
	}
	if result.Revision != evidence.Scope.Revision {
		return daily.Result{}, fmt.Errorf("%w: daily equity result revision does not match the scope revision", daily.ErrInvalid)
	}
	if result.EvidenceRevision != 0 && result.EvidenceRevision != inputs.EvidenceRevision {
		return daily.Result{}, fmt.Errorf("%w: daily equity result evidence revision mismatch", daily.ErrInvalid)
	}
	result.EvidenceRevision = inputs.EvidenceRevision
	result.InputDigest = inputs.Digest
	if result.Tolerance == "" {
		result.Tolerance = daily.Tolerance
	}
	return result, nil
}

func (s *SQLStorage) readDailyEquityCurrent(ctx context.Context, q dailyEquityQuerier, key dailyEquityKey, forUpdate bool) (dailyEquityCurrentState, bool, error) {
	query := `SELECT evidence_revision, evidence_hash, report_revision, report_input_digest, conflicted
		FROM daily_equity_current WHERE exchange = ? AND account_scope = ? AND local_date = ?`
	if forUpdate && s.dbType == "mysql" {
		query += ` FOR UPDATE`
	}
	var state dailyEquityCurrentState
	err := q.QueryRowContext(ctx, query, key.exchange, key.accountScope, key.localDate).Scan(
		&state.evidenceRevision, &state.evidenceHash, &state.reportRevision, &state.reportDigest, &state.conflicted)
	if err == sql.ErrNoRows {
		return dailyEquityCurrentState{}, false, nil
	}
	if err != nil {
		return dailyEquityCurrentState{}, false, fmt.Errorf("read daily equity current pointer: %w", err)
	}
	return state, true, nil
}

func (s *SQLStorage) readDailyEquityEvidenceVersions(ctx context.Context, q dailyEquityQuerier, key dailyEquityKey) ([]dailyEquityEvidenceVersion, error) {
	rows, err := q.QueryContext(ctx, `SELECT revision, evidence_hash, payload_json FROM daily_equity_evidence_versions
		WHERE exchange = ? AND account_scope = ? AND local_date = ? ORDER BY revision ASC`,
		key.exchange, key.accountScope, key.localDate)
	if err != nil {
		return nil, fmt.Errorf("read daily equity evidence revisions: %w", err)
	}
	defer rows.Close()
	versions := make([]dailyEquityEvidenceVersion, 0)
	for rows.Next() {
		var version dailyEquityEvidenceVersion
		if err := rows.Scan(&version.revision, &version.hash, &version.payload); err != nil {
			return nil, fmt.Errorf("scan daily equity evidence revision: %w", err)
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate daily equity evidence revisions: %w", err)
	}
	return versions, nil
}

func (s *SQLStorage) readDailyEquityEvidenceVersion(ctx context.Context, q dailyEquityQuerier, key dailyEquityKey, revision int64) (dailyEquityEvidenceVersion, error) {
	var version dailyEquityEvidenceVersion
	err := q.QueryRowContext(ctx, `SELECT revision, evidence_hash, payload_json FROM daily_equity_evidence_versions
		WHERE exchange = ? AND account_scope = ? AND local_date = ? AND revision = ?`,
		key.exchange, key.accountScope, key.localDate, revision).Scan(&version.revision, &version.hash, &version.payload)
	if err == sql.ErrNoRows {
		return dailyEquityEvidenceVersion{}, fmt.Errorf("%w: daily equity evidence revision %d is missing", daily.ErrNotFound, revision)
	}
	if err != nil {
		return dailyEquityEvidenceVersion{}, fmt.Errorf("read daily equity evidence revision: %w", err)
	}
	return version, nil
}

func (s *SQLStorage) upsertDailyEquityEvidencePointer(ctx context.Context, tx *sql.Tx, key dailyEquityKey, revision int64, hash string) error {
	if s.dbType == "mysql" {
		_, err := tx.ExecContext(ctx, `INSERT INTO daily_equity_current
			(exchange, account_scope, local_date, evidence_revision, evidence_hash, report_revision, report_input_digest, conflicted)
			VALUES (?, ?, ?, ?, ?, 0, '', 0)
			ON DUPLICATE KEY UPDATE evidence_revision = VALUES(evidence_revision), evidence_hash = VALUES(evidence_hash), updated_at = CURRENT_TIMESTAMP(3)`,
			key.exchange, key.accountScope, key.localDate, revision, hash)
		if err != nil {
			return fmt.Errorf("update daily equity evidence pointer: %w", err)
		}
		return nil
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO daily_equity_current
		(exchange, account_scope, local_date, evidence_revision, evidence_hash, report_revision, report_input_digest, conflicted)
		VALUES (?, ?, ?, ?, ?, 0, '', 0)
		ON CONFLICT(exchange, account_scope, local_date) DO UPDATE SET
			evidence_revision = excluded.evidence_revision,
			evidence_hash = excluded.evidence_hash,
			updated_at = CURRENT_TIMESTAMP`,
		key.exchange, key.accountScope, key.localDate, revision, hash)
	if err != nil {
		return fmt.Errorf("update daily equity evidence pointer: %w", err)
	}
	return nil
}

func (s *SQLStorage) upsertDailyEquityReportPointer(ctx context.Context, tx *sql.Tx, key dailyEquityKey, current dailyEquityCurrentState, evidenceRevision, reportRevision int64, digest string) error {
	if s.dbType == "mysql" {
		_, err := tx.ExecContext(ctx, `INSERT INTO daily_equity_current
			(exchange, account_scope, local_date, evidence_revision, evidence_hash, report_revision, report_input_digest, conflicted)
			VALUES (?, ?, ?, ?, ?, ?, ?, 0)
			ON DUPLICATE KEY UPDATE evidence_revision = VALUES(evidence_revision), evidence_hash = VALUES(evidence_hash),
				report_revision = VALUES(report_revision), report_input_digest = VALUES(report_input_digest), updated_at = CURRENT_TIMESTAMP(3)`,
			key.exchange, key.accountScope, key.localDate, evidenceRevision, current.evidenceHash, reportRevision, digest)
		if err != nil {
			return fmt.Errorf("update daily equity report pointer: %w", err)
		}
		return nil
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO daily_equity_current
		(exchange, account_scope, local_date, evidence_revision, evidence_hash, report_revision, report_input_digest, conflicted)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0)
		ON CONFLICT(exchange, account_scope, local_date) DO UPDATE SET
			evidence_revision = excluded.evidence_revision,
			evidence_hash = excluded.evidence_hash,
			report_revision = excluded.report_revision,
			report_input_digest = excluded.report_input_digest,
			updated_at = CURRENT_TIMESTAMP`,
		key.exchange, key.accountScope, key.localDate, evidenceRevision, current.evidenceHash, reportRevision, digest)
	if err != nil {
		return fmt.Errorf("update daily equity report pointer: %w", err)
	}
	return nil
}

func (s *SQLStorage) markDailyEquityConflicted(ctx context.Context, tx *sql.Tx, key dailyEquityKey, current dailyEquityCurrentState) error {
	if s.dbType == "mysql" {
		_, err := tx.ExecContext(ctx, `INSERT INTO daily_equity_current
			(exchange, account_scope, local_date, evidence_revision, evidence_hash, report_revision, report_input_digest, conflicted)
			VALUES (?, ?, ?, ?, ?, ?, ?, 1)
			ON DUPLICATE KEY UPDATE conflicted = 1, updated_at = CURRENT_TIMESTAMP(3)`,
			key.exchange, key.accountScope, key.localDate, current.evidenceRevision, current.evidenceHash, current.reportRevision, current.reportDigest)
		if err != nil {
			return fmt.Errorf("mark daily equity conflict: %w", err)
		}
		return nil
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO daily_equity_current
		(exchange, account_scope, local_date, evidence_revision, evidence_hash, report_revision, report_input_digest, conflicted)
		VALUES (?, ?, ?, ?, ?, ?, ?, 1)
		ON CONFLICT(exchange, account_scope, local_date) DO UPDATE SET conflicted = 1, updated_at = CURRENT_TIMESTAMP`,
		key.exchange, key.accountScope, key.localDate, current.evidenceRevision, current.evidenceHash, current.reportRevision, current.reportDigest)
	if err != nil {
		return fmt.Errorf("mark daily equity conflict: %w", err)
	}
	return nil
}

func (s *SQLStorage) recordDailyEquityConflict(ctx context.Context, tx *sql.Tx, key dailyEquityKey, conflict dailyEquityConflict) error {
	if len(conflict.source) > 64 || len(conflict.sourceID) > 191 || len(conflict.wallet) > 32 || len(conflict.reason) > 64 {
		return fmt.Errorf("%w: daily equity conflict identity exceeds schema limits", daily.ErrInvalid)
	}
	if s.dbType == "mysql" {
		_, err := tx.ExecContext(ctx, `INSERT INTO daily_equity_conflicts
			(exchange, account_scope, local_date, source, source_id, wallet, reason, existing_fingerprint, incoming_fingerprint)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE existing_fingerprint = VALUES(existing_fingerprint), incoming_fingerprint = VALUES(incoming_fingerprint), updated_at = CURRENT_TIMESTAMP(3)`,
			key.exchange, key.accountScope, key.localDate, conflict.source, conflict.sourceID, conflict.wallet, conflict.reason,
			conflict.existingFingerprint, conflict.incomingFingerprint)
		if err != nil {
			return fmt.Errorf("persist daily equity conflict: %w", err)
		}
		return nil
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO daily_equity_conflicts
		(exchange, account_scope, local_date, source, source_id, wallet, reason, existing_fingerprint, incoming_fingerprint)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(exchange, account_scope, local_date, source, source_id, wallet, reason) DO UPDATE SET
			existing_fingerprint = excluded.existing_fingerprint,
			incoming_fingerprint = excluded.incoming_fingerprint,
			updated_at = CURRENT_TIMESTAMP`,
		key.exchange, key.accountScope, key.localDate, conflict.source, conflict.sourceID, conflict.wallet, conflict.reason,
		conflict.existingFingerprint, conflict.incomingFingerprint)
	if err != nil {
		return fmt.Errorf("persist daily equity conflict: %w", err)
	}
	return nil
}

// recordDailyEquityPayloadConflict persists the fact that an evidence payload
// itself reused one source identity with different content. PrepareEvidence
// reports the identity only in an error string, so the persisted row is
// deliberately scope-scoped rather than inventing a source identity.
func (s *SQLStorage) recordDailyEquityPayloadConflict(ctx context.Context, evidence daily.Evidence) error {
	if s == nil || s.db == nil || (s.dbType != "sqlite" && s.dbType != "mysql") {
		return nil
	}
	exchange := strings.ToLower(strings.TrimSpace(evidence.Scope.Exchange))
	accountScope := strings.TrimSpace(evidence.Scope.AccountScope)
	date := strings.TrimSpace(evidence.Date)
	if exchange == "" || accountScope == "" || date == "" {
		return nil
	}
	if _, _, err := daily.DayBounds(date); err != nil {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin daily equity payload conflict: %w", err)
	}
	defer tx.Rollback()
	if err := s.recordDailyEquityConflict(ctx, tx, dailyEquityKey{exchange: exchange, accountScope: accountScope, localDate: date}, dailyEquityConflict{
		reason: "evidence_payload_conflict",
	}); err != nil {
		return err
	}
	current, _, err := s.readDailyEquityCurrent(ctx, tx, dailyEquityKey{exchange: exchange, accountScope: accountScope, localDate: date}, true)
	if err != nil {
		return err
	}
	if err := s.markDailyEquityConflicted(ctx, tx, dailyEquityKey{exchange: exchange, accountScope: accountScope, localDate: date}, current); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit daily equity payload conflict: %w", err)
	}
	return nil
}

func (s *SQLStorage) findDailyEquityReportByDigest(ctx context.Context, q dailyEquityQuerier, key dailyEquityKey, digest, method string) (daily.Result, int64, bool, error) {
	var revision int64
	err := q.QueryRowContext(ctx, `SELECT revision FROM daily_equity_report_versions
		WHERE exchange = ? AND account_scope = ? AND local_date = ? AND input_digest = ? AND method = ?
		ORDER BY revision DESC LIMIT 1`, key.exchange, key.accountScope, key.localDate, digest, method).Scan(&revision)
	if err == sql.ErrNoRows {
		return daily.Result{}, 0, false, nil
	}
	if err != nil {
		return daily.Result{}, 0, false, fmt.Errorf("find daily equity report by input digest: %w", err)
	}
	report, found, err := s.readDailyEquityReport(ctx, q, key, revision)
	if err != nil || !found {
		return daily.Result{}, 0, false, err
	}
	return report, revision, true, nil
}

func (s *SQLStorage) readDailyEquityReport(ctx context.Context, q dailyEquityQuerier, key dailyEquityKey, revision int64) (daily.Result, bool, error) {
	var reportJSON string
	var result daily.Result
	err := q.QueryRowContext(ctx, `SELECT report_json FROM daily_equity_report_versions
		WHERE exchange = ? AND account_scope = ? AND local_date = ? AND revision = ?`,
		key.exchange, key.accountScope, key.localDate, revision).Scan(&reportJSON)
	if err == sql.ErrNoRows {
		return daily.Result{}, false, nil
	}
	if err != nil {
		return daily.Result{}, false, fmt.Errorf("read daily equity report revision %d: %w", revision, err)
	}
	if err := json.Unmarshal([]byte(reportJSON), &result); err != nil {
		return daily.Result{}, false, fmt.Errorf("decode daily equity report revision %d: %w", revision, err)
	}
	return result, true, nil
}

func dailyEquityEvidenceConflicts(versions []dailyEquityEvidenceVersion, incoming daily.Evidence) ([]dailyEquityConflict, error) {
	type previousEvent struct {
		event   daily.Event
		present bool
	}
	previous := make(map[string]previousEvent)
	for _, version := range versions {
		var evidence daily.Evidence
		if err := json.Unmarshal([]byte(version.payload), &evidence); err != nil {
			return nil, fmt.Errorf("decode prior daily equity evidence revision %d: %w", version.revision, err)
		}
		for _, event := range evidence.Events {
			previous[dailyEquityEventKey(event)] = previousEvent{event: event, present: true}
		}
	}
	incomingKeys := make(map[string]struct{}, len(incoming.Events))
	conflicts := make([]dailyEquityConflict, 0)
	for _, event := range incoming.Events {
		key := dailyEquityEventKey(event)
		incomingKeys[key] = struct{}{}
		old, exists := previous[key]
		if !exists {
			continue
		}
		oldAmount := dailyEquityEventAmountFingerprint(old.event)
		newAmount := dailyEquityEventAmountFingerprint(event)
		if old.event.Kind == event.Kind {
			if dailyEquityEventPayloadFingerprint(old.event) != dailyEquityEventPayloadFingerprint(event) {
				conflicts = append(conflicts, dailyEquityConflict{
					source: event.Source, sourceID: event.SourceID, wallet: event.Wallet, reason: "event_payload_changed",
					existingFingerprint: dailyEquityEventPayloadFingerprint(old.event), incomingFingerprint: dailyEquityEventPayloadFingerprint(event),
				})
			}
			continue
		}
		if old.event.Kind == daily.Unknown && event.Kind != daily.Unknown {
			if oldAmount != newAmount {
				conflicts = append(conflicts, dailyEquityConflict{
					source: event.Source, sourceID: event.SourceID, wallet: event.Wallet, reason: "event_payload_changed",
					existingFingerprint: oldAmount, incomingFingerprint: newAmount,
				})
			}
			continue
		}
		conflicts = append(conflicts, dailyEquityConflict{
			source: event.Source, sourceID: event.SourceID, wallet: event.Wallet, reason: "event_kind_changed",
			existingFingerprint: dailyEquityEventPayloadFingerprint(old.event), incomingFingerprint: dailyEquityEventPayloadFingerprint(event),
		})
	}
	for key, old := range previous {
		if !old.present {
			continue
		}
		if _, exists := incomingKeys[key]; exists {
			continue
		}
		conflicts = append(conflicts, dailyEquityConflict{
			source: old.event.Source, sourceID: old.event.SourceID, wallet: old.event.Wallet, reason: "event_removed",
			existingFingerprint: dailyEquityEventPayloadFingerprint(old.event),
		})
	}
	sort.Slice(conflicts, func(i, j int) bool {
		left, right := conflicts[i], conflicts[j]
		if left.source != right.source {
			return left.source < right.source
		}
		if left.sourceID != right.sourceID {
			return left.sourceID < right.sourceID
		}
		if left.wallet != right.wallet {
			return left.wallet < right.wallet
		}
		return left.reason < right.reason
	})
	return conflicts, nil
}

func dailyEquityEventKey(event daily.Event) string {
	return strings.Join([]string{event.Source, event.SourceID, event.Wallet}, "\x00")
}

func dailyEquityEventAmountFingerprint(event daily.Event) string {
	material := strings.Join([]string{
		"daily-equity-event-amount-v1",
		event.Source, event.SourceID, event.Wallet, event.Asset,
		dailyEquityTimeText(event.At), event.Amount, event.PrincipalDelta, event.InterestDelta,
	}, "\x00")
	sum := sha256.Sum256([]byte(material))
	return hex.EncodeToString(sum[:])
}

func dailyEquityEventPayloadFingerprint(event daily.Event) string {
	material := strings.Join([]string{
		"daily-equity-event-payload-v1",
		event.Source, event.SourceID, event.Wallet, event.Asset,
		dailyEquityTimeText(event.At), event.Amount, event.PrincipalDelta, event.InterestDelta,
		string(event.Kind), event.EconomicID, event.LedgerRef, event.TransferID, event.CounterpartyWallet,
	}, "\x00")
	sum := sha256.Sum256([]byte(material))
	return hex.EncodeToString(sum[:])
}

func dailyEquityValidDigest(digest string) bool {
	if len(digest) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(digest)
	return err == nil && len(decoded) == 32
}

func isDailyEquityUniqueViolation(err error) bool {
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number == 1062
	}
	var sqliteErr sqlite3.Error
	if errors.As(err, &sqliteErr) {
		return sqliteErr.Code == sqlite3.ErrConstraint
	}
	return strings.Contains(strings.ToLower(err.Error()), "unique constraint")
}
