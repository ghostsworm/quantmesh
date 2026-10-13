package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"quantmesh/accounting"
)

func (s *SQLStorage) RecordRecoverySourceAnomaly(ctx context.Context, generation FundingCarryRuntimeGeneration, anomaly accounting.RecoverySourceAnomaly) (accounting.RecoverySourceAnomaly, bool, error) {
	if s == nil || s.db == nil || ctx == nil || anomaly.State != accounting.RecoverySourceAnomalyOpen || anomaly.Revision != 1 || anomaly.Validate() != nil {
		return accounting.RecoverySourceAnomaly{}, false, errors.New("invalid initial recovery source anomaly")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return accounting.RecoverySourceAnomaly{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	episode, err := s.lockFundingRecoveryGenerationForEpisode(ctx, tx, generation, anomaly.EpisodeID)
	if err != nil {
		return accounting.RecoverySourceAnomaly{}, false, err
	}
	if episode.Revision != anomaly.EpisodeRevision || episode.RecoveryGeneration != anomaly.RecoveryGeneration ||
		(episode.State != accounting.RecoveryBlocked && episode.State != accounting.RecoveryProjected) ||
		!containsString(episode.AccountScopes, anomaly.Identity.AccountScope) || !containsString(generation.ScopeKeys(), anomaly.Identity.AccountScope) {
		return accounting.RecoverySourceAnomaly{}, false, accounting.ErrRecoveryConflict
	}
	var holdRaw []byte
	err = tx.QueryRowContext(ctx, `SELECT payload_json FROM funding_recovery_holds WHERE episode_id=? AND recovery_generation=? AND state='held'`, episode.ID, episode.RecoveryGeneration).Scan(&holdRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return accounting.RecoverySourceAnomaly{}, false, errors.New("source anomaly capture requires the exact durable held recovery episode")
	}
	if err != nil {
		return accounting.RecoverySourceAnomaly{}, false, err
	}
	var hold accounting.RecoveryHold
	if err = json.Unmarshal(holdRaw, &hold); err != nil {
		return accounting.RecoverySourceAnomaly{}, false, err
	}
	if hold.EpisodeID != episode.ID || hold.Generation != episode.RecoveryGeneration || hold.State != "held" || hold.Bot != episode.Bot || hold.Strategy != episode.Strategy {
		return accounting.RecoverySourceAnomaly{}, false, errors.New("source anomaly capture found a mismatched durable hold")
	}
	identityHash, err := recoverySourceIdentityHash(anomaly.Identity)
	if err != nil {
		return accounting.RecoverySourceAnomaly{}, false, err
	}
	var cursorRaw []byte
	err = tx.QueryRowContext(ctx, `SELECT payload_json FROM funding_recovery_source_cursors WHERE cursor_key=?`, recoverySourceCursorKey(anomaly.Identity)).Scan(&cursorRaw)
	if err != nil {
		return accounting.RecoverySourceAnomaly{}, false, errors.Join(errors.New("source anomaly cursor read failed"), err)
	}
	var cursor accounting.RecoverySourceCursor
	if err = json.Unmarshal(cursorRaw, &cursor); err != nil {
		return accounting.RecoverySourceAnomaly{}, false, err
	}
	if cursor.EpisodeID != episode.ID || cursor.Revision != anomaly.CursorRevision || !cursor.InitialFrom.Equal(anomaly.InitialFrom) || !cursor.Through.Equal(anomaly.Through) ||
		cursor.HistoricalEvidenceHash != anomaly.ExpectedHistoryHash || anomaly.ObservedHistoryHash == anomaly.ExpectedHistoryHash {
		return accounting.RecoverySourceAnomaly{}, false, accounting.ErrRecoveryConflict
	}
	if existing, found, getErr := getRecoverySourceAnomalyTx(ctx, tx, anomaly.ID); getErr != nil {
		return accounting.RecoverySourceAnomaly{}, false, getErr
	} else if found {
		if !sameRecoverySourceAnomalyFacts(existing, anomaly) {
			return accounting.RecoverySourceAnomaly{}, false, accounting.ErrRecoveryOperationConflict
		}
		if err = tx.Commit(); err != nil {
			return accounting.RecoverySourceAnomaly{}, false, err
		}
		return existing, true, nil
	}
	payload, err := json.Marshal(anomaly)
	if err != nil {
		return accounting.RecoverySourceAnomaly{}, false, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO funding_recovery_source_anomalies(anomaly_id,episode_id,source_identity_hash,cursor_revision,observed_history_hash,state,revision,payload_json) VALUES(?,?,?,?,?,?,?,?)`,
		anomaly.ID, anomaly.EpisodeID, identityHash, anomaly.CursorRevision, anomaly.ObservedHistoryHash, string(anomaly.State), anomaly.Revision, payload)
	if err != nil {
		if errors.Is(recoveryConflict(err), accounting.ErrRecoveryConflict) {
			return accounting.RecoverySourceAnomaly{}, false, accounting.ErrRecoveryOperationConflict
		}
		return accounting.RecoverySourceAnomaly{}, false, fmt.Errorf("insert durable recovery source anomaly: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return accounting.RecoverySourceAnomaly{}, false, err
	}
	return anomaly, false, nil
}

func (s *SQLStorage) GetRecoverySourceAnomaly(ctx context.Context, episodeID, anomalyID string) (accounting.RecoverySourceAnomaly, bool, error) {
	if s == nil || s.db == nil || ctx == nil || episodeID == "" || anomalyID == "" {
		return accounting.RecoverySourceAnomaly{}, false, ErrFundingRecoveryUnavailable
	}
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT payload_json FROM funding_recovery_source_anomalies WHERE episode_id=? AND anomaly_id=?`, episodeID, anomalyID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return accounting.RecoverySourceAnomaly{}, false, nil
	}
	if err != nil {
		return accounting.RecoverySourceAnomaly{}, false, err
	}
	var anomaly accounting.RecoverySourceAnomaly
	if err = json.Unmarshal(raw, &anomaly); err != nil {
		return anomaly, false, err
	}
	if err = anomaly.Validate(); err != nil {
		return anomaly, false, fmt.Errorf("validate durable source anomaly: %w", err)
	}
	return anomaly, true, nil
}

func (s *SQLStorage) ListRecoverySourceAnomalies(ctx context.Context, episodeID string) ([]accounting.RecoverySourceAnomaly, error) {
	if s == nil || s.db == nil || ctx == nil || episodeID == "" {
		return nil, ErrFundingRecoveryUnavailable
	}
	rows, err := s.db.QueryContext(ctx, `SELECT payload_json FROM funding_recovery_source_anomalies WHERE episode_id=? ORDER BY created_at,anomaly_id`, episodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	anomalies := make([]accounting.RecoverySourceAnomaly, 0)
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var anomaly accounting.RecoverySourceAnomaly
		if err = json.Unmarshal(raw, &anomaly); err != nil {
			return nil, err
		}
		if err = anomaly.Validate(); err != nil {
			return nil, fmt.Errorf("validate durable source anomaly: %w", err)
		}
		anomalies = append(anomalies, anomaly)
	}
	return anomalies, rows.Err()
}

func getRecoverySourceAnomalyTx(ctx context.Context, tx *sql.Tx, anomalyID string) (accounting.RecoverySourceAnomaly, bool, error) {
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT payload_json FROM funding_recovery_source_anomalies WHERE anomaly_id=?`, anomalyID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return accounting.RecoverySourceAnomaly{}, false, nil
	}
	if err != nil {
		return accounting.RecoverySourceAnomaly{}, false, err
	}
	var anomaly accounting.RecoverySourceAnomaly
	if err = json.Unmarshal(raw, &anomaly); err != nil {
		return anomaly, false, err
	}
	if err = anomaly.Validate(); err != nil {
		return anomaly, false, err
	}
	return anomaly, true, nil
}

func sameRecoverySourceAnomalyFacts(left, right accounting.RecoverySourceAnomaly) bool {
	return left.ID == right.ID && left.EpisodeID == right.EpisodeID && left.RecoveryGeneration == right.RecoveryGeneration &&
		left.Identity == right.Identity && left.CursorRevision == right.CursorRevision && left.InitialFrom.Equal(right.InitialFrom) && left.Through.Equal(right.Through) &&
		left.ExpectedHistoryHash == right.ExpectedHistoryHash && left.ObservedHistoryHash == right.ObservedHistoryHash && reflect.DeepEqual(left.Facts, right.Facts)
}

func recoverySourceIdentityHash(identity accounting.RecoverySourceIdentity) (string, error) {
	if err := identity.Validate(); err != nil {
		return "", err
	}
	data, err := json.Marshal(identity)
	if err != nil {
		return "", err
	}
	return canonicalHash(data), nil
}

func isRecoverySourceCursorEvent(event accounting.RecoveryEvent) bool {
	if event.Kind == accounting.RecoveryFundingEvent && event.Source == accounting.RecoveryManualFundingAdjustmentSource {
		return false
	}
	return event.Kind == accounting.RecoveryBorrowEvent || event.Kind == accounting.RecoveryRepayEvent ||
		event.Kind == accounting.RecoveryInterestEvent || event.Kind == accounting.RecoveryFundingEvent
}

func updateRecoverySourceAnomaliesTx(ctx context.Context, tx *sql.Tx, op accounting.RecoveryOperation, episode accounting.RecoveryEpisode) error {
	if op.SourceAnomalyID == "" && op.ExpectedAnomalyRevision != 0 {
		return errors.New("anomaly revision requires an anomaly identity")
	}
	for _, event := range op.Events {
		if event.FundingAdjustment != nil && op.SourceAnomalyID == "" {
			return errors.New("manual funding adjustment must resolve a durable source anomaly")
		}
	}
	if op.SourceAnomalyID != "" {
		if op.ExpectedAnomalyRevision == 0 || len(op.VerifiedAnomalyIDs) != 0 {
			return errors.New("source anomaly adjustment CAS fields are incomplete or mixed with verification")
		}
		anomaly, found, err := getRecoverySourceAnomalyTx(ctx, tx, op.SourceAnomalyID)
		if err != nil || !found {
			return errors.Join(ErrFundingRecoveryNotFound, err)
		}
		if anomaly.EpisodeID != episode.ID || anomaly.Revision != op.ExpectedAnomalyRevision || anomaly.RecoveryGeneration != episode.RecoveryGeneration ||
			anomaly.State != accounting.RecoverySourceAnomalyOpen || episode.Revision != op.ExpectedEpisodeRevision+1 ||
			len(op.Events) != len(anomaly.Facts) || op.Actor == "" || op.AuditRef == "" || !validRecoveryAnomalyDigest(op.EvidenceHash) {
			return accounting.ErrRecoveryConflict
		}
		if err := validateSourceAnomalyAdjustmentEvents(anomaly, op); err != nil {
			return err
		}
		anomaly.Revision++
		anomaly.State = accounting.RecoverySourceAnomalyAdjustmentApplied
		anomaly.AdjustmentOperationID, anomaly.AdjustmentActor = op.OperationID, op.Actor
		anomaly.AdjustmentAuditRef, anomaly.AdjustmentEvidenceHash = op.AuditRef, op.EvidenceHash
		return persistRecoverySourceAnomalyTx(ctx, tx, anomaly, op.ExpectedAnomalyRevision)
	}
	if len(op.VerifiedAnomalyIDs) > 0 {
		if op.SourceWindow == nil {
			return errors.New("source anomalies can only be verified by a complete cursor-advancing source window")
		}
		seen := make(map[string]struct{}, len(op.VerifiedAnomalyIDs))
		for _, anomalyID := range op.VerifiedAnomalyIDs {
			if _, duplicate := seen[anomalyID]; duplicate {
				return errors.New("source anomaly verification list contains duplicates")
			}
			seen[anomalyID] = struct{}{}
			anomaly, found, err := getRecoverySourceAnomalyTx(ctx, tx, anomalyID)
			if err != nil || !found {
				return errors.Join(ErrFundingRecoveryNotFound, err)
			}
			if anomaly.EpisodeID != episode.ID || anomaly.RecoveryGeneration != episode.RecoveryGeneration || anomaly.State != accounting.RecoverySourceAnomalyAdjustmentApplied ||
				anomaly.Identity != op.SourceWindow.Identity() || anomaly.CursorRevision != op.SourceWindow.ExpectedRevision ||
				op.SourceWindow.HistoricalEvidenceHash == "" || episode.Revision != op.ExpectedEpisodeRevision+1 {
				return accounting.ErrRecoveryConflict
			}
			anomaly.Revision++
			anomaly.State = accounting.RecoverySourceAnomalyVerified
			anomaly.VerifiedHistoryHash, anomaly.VerifiedAt = op.SourceWindow.HistoricalEvidenceHash, time.Now().UTC()
			if err = persistRecoverySourceAnomalyTx(ctx, tx, anomaly, anomaly.Revision-1); err != nil {
				return err
			}
		}
	}
	return nil
}

func validRecoveryAnomalyDigest(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	for _, char := range value {
		if !(char >= '0' && char <= '9') && !(char >= 'a' && char <= 'f') {
			return false
		}
	}
	return true
}

func validateSourceAnomalyAdjustmentEvents(anomaly accounting.RecoverySourceAnomaly, op accounting.RecoveryOperation) error {
	byExternalID := make(map[string]accounting.RecoveryEvent, len(op.Events))
	for _, event := range op.Events {
		if event.Kind != accounting.RecoveryFundingEvent || event.Source != accounting.RecoveryManualFundingAdjustmentSource || event.FundingAdjustment == nil ||
			event.EpisodeID != anomaly.EpisodeID || event.Exchange != anomaly.Identity.Exchange || event.Market != anomaly.Identity.Market ||
			event.AccountScope != anomaly.Identity.AccountScope || event.Symbol != anomaly.Identity.Symbol || event.Asset != anomaly.Identity.Asset ||
			event.FundingAdjustment.AnomalyID != anomaly.ID || event.FundingAdjustment.Actor != op.Actor || event.FundingAdjustment.AuditRef != op.AuditRef ||
			event.FundingAdjustment.EvidenceHash != op.EvidenceHash || event.ExternalID == "" {
			return errors.New("funding adjustment event is not bound to the durable anomaly and operation audit")
		}
		if _, exists := byExternalID[event.RelatedExternalID]; exists {
			return errors.New("funding adjustment operation repeats a source transaction identity")
		}
		byExternalID[event.RelatedExternalID] = event
	}
	if len(byExternalID) != len(anomaly.Facts) {
		return errors.New("funding adjustment operation does not cover every anomaly fact")
	}
	for _, fact := range anomaly.Facts {
		event, found := byExternalID[fact.ExternalID]
		if !found {
			return errors.New("funding adjustment operation omitted an anomaly fact")
		}
		evidence := event.FundingAdjustment
		originalAmount, originalHash, observedAmount, observedHash := "", "", "", ""
		if fact.OriginalEvent != nil {
			originalAmount, originalHash = fact.OriginalEvent.Amount, fact.OriginalEvent.SourceHash
		}
		if fact.ObservedEvent != nil {
			observedAmount, observedHash = fact.ObservedEvent.Amount, fact.ObservedEvent.SourceHash
		}
		if evidence.OriginalExternalID != fact.ExternalID || evidence.OriginalAmountRaw != originalAmount || evidence.OriginalSourceHash != originalHash ||
			evidence.ObservedAmountRaw != observedAmount || evidence.ObservedSourceHash != observedHash || evidence.DeltaAmountRaw != fact.DeltaAmount || event.Amount != fact.DeltaAmount {
			return accounting.ErrRecoveryOperationConflict
		}
	}
	return nil
}

func persistRecoverySourceAnomalyTx(ctx context.Context, tx *sql.Tx, anomaly accounting.RecoverySourceAnomaly, expectedRevision uint64) error {
	if err := anomaly.Validate(); err != nil {
		return err
	}
	payload, err := json.Marshal(anomaly)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE funding_recovery_source_anomalies SET state=?,revision=?,payload_json=?,updated_at=CURRENT_TIMESTAMP WHERE anomaly_id=? AND episode_id=? AND revision=?`,
		string(anomaly.State), anomaly.Revision, payload, anomaly.ID, anomaly.EpisodeID, expectedRevision)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		return errors.Join(accounting.ErrRecoveryConflict, err)
	}
	return nil
}

func requireRecoverySourceAnomaliesVerified(ctx context.Context, tx *sql.Tx, episodeID string) error {
	var unresolved int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM funding_recovery_source_anomalies WHERE episode_id=? AND state<>?`,
		episodeID, string(accounting.RecoverySourceAnomalyVerified)).Scan(&unresolved); err != nil {
		return fmt.Errorf("check unresolved funding source anomalies before hold release: %w", err)
	}
	if unresolved != 0 {
		return accounting.ErrRecoveryConflict
	}
	return nil
}
