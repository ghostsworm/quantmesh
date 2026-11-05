package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"time"

	"quantmesh/accounting"
)

// Rows include indexed columns as well as canonical JSON. Both copies are
// checked before hashing, so native MySQL JSON key order is immaterial and
// changing only an indexed state/revision cannot bypass the evidence fence.
type releaseResumeEvidenceRow struct {
	Scalars []string        `json:"scalars"`
	Payload json.RawMessage `json:"payload"`
}

type releaseResumeEvidence struct {
	Episode            accounting.RecoveryEpisode            `json:"episode"`
	Hold               accounting.RecoveryHold               `json:"hold"`
	ReleaseReceipt     accounting.RecoveryReceipt            `json:"release_receipt"`
	Rows               map[string][]releaseResumeEvidenceRow `json:"rows"`
	ProjectionReceipts []accounting.RecoveryReceipt          `json:"projection_receipts"`
	ResumeReceipts     []accounting.RecoveryReceipt          `json:"resume_receipts"`
}

func releaseResumeEvidenceHash(evidence releaseResumeEvidence, owner string, result bool) (string, error) {
	if result {
		evidence.Episode.ReleaseResumptions = append([]accounting.RecoveryReleaseResumeRecord(nil), evidence.Episode.ReleaseResumptions...)
		if len(evidence.Episode.ReleaseResumptions) == 0 {
			return "", accounting.ErrRecoveryConflict
		}
		evidence.Episode.ReleaseResumptions[len(evidence.Episode.ReleaseResumptions)-1].ResultEvidenceHash = ""
	}
	raw, err := json.Marshal(struct {
		Kind     string                `json:"kind"`
		Owner    string                `json:"owner_generation"`
		Evidence releaseResumeEvidence `json:"evidence"`
	}{"recovery_release_resume_evidence_v1", owner, evidence})
	if err != nil {
		return "", err
	}
	raw, err = canonicalRecoveryJSON(raw)
	if err != nil {
		return "", err
	}
	return canonicalHash(raw), nil
}

func readReleaseResumeEvidence(ctx context.Context, tx *sql.Tx, generation FundingCarryRuntimeGeneration, episodeID, releaseOperationID string) (releaseResumeEvidence, error) {
	var evidence releaseResumeEvidence
	var raw []byte
	var revision, recoveryGeneration uint64
	var state, bot, strategy string
	err := tx.QueryRowContext(ctx, `SELECT revision,recovery_generation,state,bot_id,strategy,payload_json FROM funding_recovery_episodes WHERE episode_id=?`, episodeID).
		Scan(&revision, &recoveryGeneration, &state, &bot, &strategy, &raw)
	if err != nil {
		return evidence, recoveryConflict(err)
	}
	if err = json.Unmarshal(raw, &evidence.Episode); err != nil {
		return evidence, err
	}
	ep := evidence.Episode
	if ep.ID != episodeID || ep.Revision != revision || ep.RecoveryGeneration != recoveryGeneration || string(ep.State) != state || ep.Bot != bot || ep.Strategy != strategy ||
		ep.Bot == "" || ep.Strategy == "" || ep.OwnerGeneration == "" || !validResumeRevision(revision) || !validResumeRevision(recoveryGeneration) {
		return evidence, accounting.ErrRecoveryConflict
	}
	if err = releaseResumeScopesMatch(ep.AccountScopes, generation.ScopeKeys()); err != nil {
		return evidence, err
	}
	var holdID string
	err = tx.QueryRowContext(ctx, `SELECT hold_id,hold_revision,state,payload_json FROM funding_recovery_holds WHERE episode_id=? AND recovery_generation=?`, episodeID, recoveryGeneration).
		Scan(&holdID, &revision, &state, &raw)
	if err != nil {
		return evidence, recoveryConflict(err)
	}
	if err = json.Unmarshal(raw, &evidence.Hold); err != nil {
		return evidence, err
	}
	hold := evidence.Hold
	if hold.ID == "" || hold.ID != holdID || hold.EpisodeID != episodeID || hold.Generation != recoveryGeneration || hold.Revision != revision || hold.State != state ||
		hold.Bot != ep.Bot || hold.Strategy != ep.Strategy || !validResumeRevision(revision) ||
		!((ep.State == accounting.RecoveryReleasePending && hold.State == "held" && hold.ReleasedByOperation == "") ||
			(ep.State == accounting.RecoveryReleased && hold.State == "released" && hold.ReleasedByOperation == releaseOperationID)) {
		return evidence, accounting.ErrRecoveryConflict
	}
	if err = releaseResumeScopesMatch(hold.AccountScopes, generation.ScopeKeys()); err != nil {
		return evidence, err
	}
	var exists bool
	evidence.ReleaseReceipt, exists, err = readReleaseResumeReceipt(ctx, tx, episodeID, releaseOperationID)
	if err != nil {
		return evidence, err
	}
	if !exists {
		return evidence, accounting.ErrRecoveryConflict
	}
	if err = validateReleaseResumeOriginalIdentity(evidence); err != nil {
		return evidence, err
	}
	for _, record := range ep.ReleaseResumptions {
		receipt, found, readErr := readReleaseResumeReceipt(ctx, tx, episodeID, record.OperationID)
		if readErr != nil {
			return evidence, readErr
		}
		if !found || !reflect.DeepEqual(receipt, releaseResumeReceiptForRecord(episodeID, record)) {
			return evidence, fmt.Errorf("validate historical release resume receipt %s: %w", record.OperationID, accounting.ErrRecoveryConflict)
		}
		evidence.ResumeReceipts = append(evidence.ResumeReceipts, receipt)
	}
	evidence.Rows = make(map[string][]releaseResumeEvidenceRow)
	// These queries are static; identifiers are never derived from requests.
	queries := []struct{ name, query string }{
		{"children", `SELECT child_id,revision,payload_json FROM funding_recovery_children WHERE episode_id=? ORDER BY child_id`},
		{"events", `SELECT event_key,event_id,event_revision,payload_json FROM funding_recovery_events WHERE episode_id=? ORDER BY event_key`},
		{"attempts", `SELECT permit_id,recovery_generation,action_id,revision,state,client_order_id,intent_hash,payload_json FROM funding_recovery_submission_attempts WHERE episode_id=? ORDER BY permit_id`},
		{"anomalies", `SELECT anomaly_id,source_identity_hash,cursor_revision,observed_history_hash,state,revision,payload_json FROM funding_recovery_source_anomalies WHERE episode_id=? ORDER BY anomaly_id`},
		{"outboxes", `SELECT operation_id,state,target_episode_revision,projection_hash,payload_json FROM funding_recovery_outbox WHERE episode_id=? ORDER BY target_episode_revision,operation_id`},
		{"permits", `SELECT permit_id,recovery_generation,action_id,lease_id,state,payload_json FROM funding_recovery_permits WHERE episode_id=? ORDER BY permit_id`},
		{"cursors", `SELECT cursor_key,revision,source,exchange_name,market,account_scope,event_type,asset,symbol,complete_rows,total_rows,completion_marker,evidence_hash,window_from,window_through,payload_json FROM funding_recovery_source_cursors WHERE episode_id=? ORDER BY cursor_key`},
		{"actions", `SELECT action_id,ordinal,revision,payload_json FROM funding_recovery_actions WHERE episode_id=? ORDER BY ordinal,action_id`},
		{"adjudications", `SELECT operation_id,request_hash,permit_id,payload_json FROM funding_recovery_adjudications WHERE episode_id=? ORDER BY operation_id`},
		{"origins", `SELECT origin_key,payload_json FROM funding_recovery_origins WHERE episode_id=? ORDER BY origin_key`},
		{"reopens", `SELECT operation_id,request_hash,phase,receipt_json FROM funding_recovery_operations WHERE episode_id=? AND phase='reopened' ORDER BY operation_id`},
	}
	for _, query := range queries {
		rows, readErr := readReleaseResumeRows(ctx, tx, query.query, episodeID)
		if readErr != nil {
			return evidence, fmt.Errorf("read release resume %s: %w", query.name, readErr)
		}
		evidence.Rows[query.name] = rows
	}
	if err = validateReleaseResumeRows(evidence); err != nil {
		return evidence, fmt.Errorf("validate release resume business rows for %s: %w", episodeID, err)
	}
	if err = validateRecoveryReleaseSubmissionEvidence(ctx, tx, episodeID); err != nil {
		return evidence, fmt.Errorf("validate release resume submissions for %s: %w", episodeID, err)
	}
	if err = validateReleaseResumeProjections(ctx, tx, &evidence); err != nil {
		return evidence, fmt.Errorf("validate release resume projections for %s: %w", episodeID, err)
	}
	return evidence, nil
}

func releaseResumeScopesMatch(scopes, expected []string) error {
	normalized, err := normalizeFundingCarryGenerationScopes(scopes)
	if err != nil || !sameStrings(normalized, expected) {
		return ErrFundingRecoveryGenerationScopeMismatch
	}
	return nil
}

func validResumeRevision(value uint64) bool {
	return value > 0 && value <= accounting.MaxDurableRecoveryRevision
}
func resumeUint(value uint64) string { return strconv.FormatUint(value, 10) }

func readReleaseResumeRows(ctx context.Context, tx *sql.Tx, query, episodeID string) ([]releaseResumeEvidenceRow, error) {
	rows, err := tx.QueryContext(ctx, query, episodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	result := make([]releaseResumeEvidenceRow, 0)
	for rows.Next() {
		row := releaseResumeEvidenceRow{Scalars: make([]string, len(columns)-1)}
		dest := make([]any, len(columns))
		for i := range row.Scalars {
			dest[i] = &row.Scalars[i]
		}
		var raw []byte
		dest[len(columns)-1] = &raw
		if err = rows.Scan(dest...); err != nil {
			return nil, err
		}
		if _, err = canonicalRecoveryJSON(raw); err != nil {
			return nil, err
		}
		// Keep SQL's integer spelling for typed decoding. The outer evidence
		// hash canonicalizes numbers (including 1 versus 1e0) only at hashing.
		row.Payload = append(json.RawMessage(nil), raw...)
		result = append(result, row)
	}
	return result, rows.Err()
}

func validateReleaseResumeOriginalIdentity(evidence releaseResumeEvidence) error {
	ep, hold, receipt := evidence.Episode, evidence.Hold, evidence.ReleaseReceipt
	if receipt.State != ep.State || receipt.RecoveryGeneration != ep.RecoveryGeneration || !validResumeRevision(receipt.EpisodeRevision) ||
		receipt.Actor == "" || receipt.AuditRef == "" || receipt.OwnerGeneration == "" || receipt.CreatedAt.IsZero() ||
		!validRecoveryAnomalyDigest(receipt.EvidenceHash) || !validRecoveryAnomalyDigest(receipt.ExposureClearanceHash) || !validRecoveryAnomalyDigest(receipt.ProjectionHash) ||
		receipt.ReleaseOperationID != "" || receipt.ReleaseRequestHash != "" || receipt.PreviousOwnerGeneration != "" {
		return accounting.ErrRecoveryConflict
	}
	if err := ep.ValidateOwnerHistory(); err != nil {
		return fmt.Errorf("validate release resume owner history: %w", err)
	}
	owner, revision := receipt.OwnerGeneration, receipt.EpisodeRevision
	episodeScopes := append([]string(nil), ep.AccountScopes...)
	sort.Strings(episodeScopes)
	currentResumptions := make([]accounting.RecoveryReleaseResumeRecord, 0)
	for _, record := range ep.ReleaseResumptions {
		if record.RecoveryGeneration == ep.RecoveryGeneration {
			currentResumptions = append(currentResumptions, record)
		}
	}
	for i, record := range currentResumptions {
		auth := accounting.RecoveryReleaseResumeAuthorization{
			EpisodeID: ep.ID, OperationID: record.OperationID, ReleaseOperationID: record.ReleaseOperationID, ReleaseRequestHash: record.ReleaseRequestHash,
			Actor: record.Actor, AuditRef: record.AuditRef, EvidenceHash: record.EvidenceHash, ExpectedOwnerGeneration: record.PreviousOwnerGeneration,
			OwnerGeneration: record.OwnerGeneration, AccountScopes: record.AccountScopes, ExpectedEpisodeRevision: record.ExpectedEpisodeRevision,
			ExpectedHoldRevision: record.ExpectedHoldRevision, RecoveryGeneration: record.RecoveryGeneration, Confirmed: true,
		}
		if auth.Validate() != nil || record.ReleaseOperationID != receipt.OperationID || record.ReleaseRequestHash != receipt.RequestHash ||
			record.RecoveryGeneration != ep.RecoveryGeneration || record.PreviousOwnerGeneration != owner || record.EpisodeRevision != record.ExpectedEpisodeRevision+1 ||
			!sameStrings(record.AccountScopes, episodeScopes) || record.CreatedAt.IsZero() || !validRecoveryAnomalyDigest(record.ResultEvidenceHash) ||
			record.HoldRevision != hold.Revision || ep.State != accounting.RecoveryReleased {
			return accounting.ErrRecoveryConflict
		}
		hash, err := releaseResumeRequestHash(auth)
		if err != nil || hash != record.RequestHash {
			return accounting.ErrRecoveryConflict
		}
		if i == 0 && record.PreviousState == accounting.RecoveryReleasePending {
			if record.EpisodeRevision != revision || record.ExpectedHoldRevision >= accounting.MaxDurableRecoveryRevision || record.HoldRevision != record.ExpectedHoldRevision+1 {
				return accounting.ErrRecoveryConflict
			}
		} else if record.PreviousState != accounting.RecoveryReleased || record.ExpectedEpisodeRevision != revision || record.HoldRevision != record.ExpectedHoldRevision {
			return accounting.ErrRecoveryConflict
		}
		owner, revision = record.OwnerGeneration, record.EpisodeRevision
	}
	if owner != ep.OwnerGeneration || revision != ep.Revision {
		return accounting.ErrRecoveryConflict
	}
	return nil
}

func validateReleaseResumeRows(evidence releaseResumeEvidence) error {
	ep := evidence.Episode
	scopes := make(map[string]bool, len(ep.AccountScopes))
	for _, scope := range ep.AccountScopes {
		scopes[scope] = true
	}
	for _, row := range evidence.Rows["children"] {
		var value accounting.RecoveryChild
		if json.Unmarshal(row.Payload, &value) != nil || value.Validate() != nil || value.EpisodeID != ep.ID || !scopes[value.AccountScope] ||
			value.Bot != ep.Bot || (value.State != accounting.ChildBound && value.State != accounting.ChildTerminal) ||
			!sameStrings(row.Scalars, []string{value.ID, resumeUint(value.Revision)}) {
			return accounting.ErrRecoveryConflict
		}
	}
	for _, row := range evidence.Rows["events"] {
		var value accounting.RecoveryEvent
		if json.Unmarshal(row.Payload, &value) != nil || value.Validate() != nil || value.EpisodeID != ep.ID || !scopes[value.AccountScope] ||
			row.Scalars[0] != recoveryExternalEventKey(value) || row.Scalars[1] != value.ID || row.Scalars[2] != "1" {
			return accounting.ErrRecoveryConflict
		}
	}
	for _, row := range evidence.Rows["attempts"] {
		var value accounting.RecoverySubmissionAttempt
		if json.Unmarshal(row.Payload, &value) != nil || value.Validate() != nil || value.EpisodeID != ep.ID || !scopes[value.AccountScope] ||
			value.State != accounting.RecoverySubmissionFound || !validResumeRevision(value.Generation) || value.Generation > ep.RecoveryGeneration ||
			!sameStrings(row.Scalars, []string{value.PermitID, resumeUint(value.Generation), value.ActionID, resumeUint(value.Revision), string(value.State), value.Intent.ClientOrderID, value.IntentHash}) {
			return accounting.ErrRecoveryConflict
		}
	}
	for _, row := range evidence.Rows["anomalies"] {
		var value accounting.RecoverySourceAnomaly
		if json.Unmarshal(row.Payload, &value) != nil || value.Validate() != nil || value.EpisodeID != ep.ID || !scopes[value.Identity.AccountScope] ||
			value.State != accounting.RecoverySourceAnomalyVerified || value.RecoveryGeneration > ep.RecoveryGeneration {
			return accounting.ErrRecoveryConflict
		}
		identityHash, err := recoverySourceIdentityHash(value.Identity)
		if err != nil || !sameStrings(row.Scalars, []string{value.ID, identityHash, resumeUint(value.CursorRevision), value.ObservedHistoryHash, string(value.State), resumeUint(value.Revision)}) {
			return accounting.ErrRecoveryConflict
		}
	}
	attempts := make(map[string]accounting.RecoverySubmissionAttempt)
	for _, row := range evidence.Rows["attempts"] {
		var value accounting.RecoverySubmissionAttempt
		if err := json.Unmarshal(row.Payload, &value); err != nil {
			return err
		}
		attempts[value.PermitID] = value
	}
	for _, row := range evidence.Rows["permits"] {
		var value accounting.OneShotPermit
		if err := json.Unmarshal(row.Payload, &value); err != nil {
			return err
		}
		identity := value.Clone()
		identity.State = accounting.PermitAuthorized // Validate checks issuance, not stored terminal states.
		if identity.Validate() != nil || value.EpisodeID != ep.ID || !scopes[value.AccountScope] ||
			(value.State != accounting.PermitConsumed && value.State != accounting.PermitRevoked) || value.Generation > ep.RecoveryGeneration ||
			!sameStrings(row.Scalars, []string{value.ID, resumeUint(value.Generation), value.ActionID, resumeUint(value.LeaseID), string(value.State)}) {
			return accounting.ErrRecoveryConflict
		}
		if value.State == accounting.PermitConsumed && value.Kind == accounting.RecoveryActionOrder {
			attempt, found := attempts[value.ID]
			if !found || attempt.Generation != value.Generation || attempt.OwnerGeneration != value.OwnerGeneration || attempt.IntentHash != value.OrderIntentHash ||
				attempt.AccountScope != value.AccountScope || attempt.ActionID != value.ActionID || attempt.LeaseID != value.LeaseID {
				return accounting.ErrRecoveryConflict
			}
			delete(attempts, value.ID)
		}
	}
	if len(attempts) != 0 {
		return accounting.ErrRecoveryConflict
	}
	for _, row := range evidence.Rows["cursors"] {
		var value accounting.RecoverySourceCursor
		if json.Unmarshal(row.Payload, &value) != nil || value.RecoverySourceWindow.Validate() != nil || value.EpisodeID != ep.ID || !scopes[value.AccountScope] || !validResumeRevision(value.Revision) ||
			!sameStrings(row.Scalars[:13], []string{recoverySourceCursorKey(value.Identity()), resumeUint(value.Revision), value.Source, value.Exchange, value.Market, value.AccountScope, string(value.Kind), value.Asset, value.Symbol, resumeUint(value.Rows), resumeUint(value.TotalRows), value.CompletionMarker, value.EvidenceHash}) ||
			!releaseResumeCursorTimeMatches(row.Scalars[13], value.From) || !releaseResumeCursorTimeMatches(row.Scalars[14], value.Through) {
			return accounting.ErrRecoveryConflict
		}
	}
	return nil
}

// Reopen writes a late event with its own immutable receipt instead of an
// outbox. Bind that fact to the exact reopen request before it can count as
// already-accounted evidence in a later recovery generation.
func includeReleaseResumeReopenEvents(evidence releaseResumeEvidence, events map[string]accounting.RecoveryEvent) error {
	for _, row := range evidence.Rows["reopens"] {
		var receipt accounting.RecoveryReceipt
		if json.Unmarshal(row.Payload, &receipt) != nil || receipt.EpisodeID != evidence.Episode.ID || receipt.State != accounting.RecoveryReopened ||
			receipt.RecoveryGeneration < 2 || receipt.RecoveryGeneration > evidence.Episode.RecoveryGeneration || receipt.EpisodeRevision < 2 ||
			receipt.EpisodeRevision > evidence.Episode.Revision || !sameStrings(row.Scalars, []string{receipt.OperationID, receipt.RequestHash, string(receipt.State)}) {
			return accounting.ErrRecoveryConflict
		}
		found := false
		for _, eventRow := range evidence.Rows["events"] {
			var event accounting.RecoveryEvent
			if err := json.Unmarshal(eventRow.Payload, &event); err != nil {
				return err
			}
			encoded, err := json.Marshal(event)
			if err != nil {
				return err
			}
			if canonicalHash(encoded) != receipt.ProjectionHash {
				continue
			}
			request, err := json.Marshal(struct {
				EpisodeID string                   `json:"episode_id"`
				Revision  uint64                   `json:"expected_revision"`
				Event     accounting.RecoveryEvent `json:"event"`
			}{event.EpisodeID, receipt.EpisodeRevision - 1, event})
			if err != nil {
				return err
			}
			if canonicalHash(request) != receipt.RequestHash || found {
				return accounting.ErrRecoveryConflict
			}
			events[event.ID], found = event, true
		}
		if !found {
			return accounting.ErrRecoveryConflict
		}
	}
	return nil
}

// MySQL persists window endpoints as DATETIME(3); SQLite keeps the complete
// instant. Accept only exact instants or the database's millisecond precision.
func releaseResumeCursorTimeMatches(raw string, expected time.Time) bool {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999"} {
		actual, err := time.Parse(layout, raw)
		if err == nil && (actual.Equal(expected) || actual.Equal(expected.Truncate(time.Millisecond)) || actual.Equal(expected.Round(time.Millisecond))) {
			return true
		}
	}
	return false
}

func validateReleaseResumeProjections(ctx context.Context, tx *sql.Tx, evidence *releaseResumeEvidence) error {
	var latest accounting.RecoveryOutbox
	for _, row := range evidence.Rows["outboxes"] {
		var outbox accounting.RecoveryOutbox
		if json.Unmarshal(row.Payload, &outbox) != nil || outbox.EpisodeID != evidence.Episode.ID || outbox.State != accounting.RecoveryProjected ||
			!sameStrings(row.Scalars, []string{outbox.OperationID, string(outbox.State), resumeUint(outbox.TargetEpisodeRevision), outbox.ProjectionHash}) {
			return accounting.ErrRecoveryConflict
		}
		receipt, found, err := readReleaseResumeReceipt(ctx, tx, evidence.Episode.ID, outbox.OperationID)
		if err != nil {
			return err
		}
		if !found {
			return accounting.ErrRecoveryConflict
		}
		if err = VerifyFundingRecoveryReceiptOutboxPair(receipt, outbox); err != nil {
			// The existing ordinary takeover may add exactly one projection
			// revision. Its own immutable receipt must independently verify.
			if err = verifyFundingRecoveryReceiptOutboxPair(receipt, outbox, 2); err != nil {
				return err
			}
			verified := false
			for _, takeover := range evidence.Episode.OwnerTakeovers {
				if takeover.EpisodeRevision != outbox.TargetEpisodeRevision+1 {
					continue
				}
				prior, exists, readErr := readReleaseResumeReceipt(ctx, tx, evidence.Episode.ID, takeover.OperationID)
				if readErr != nil {
					return readErr
				}
				auditedEpisode := evidence.Episode
				auditedEpisode.RecoveryGeneration = prior.RecoveryGeneration
				if exists && validResumeRevision(prior.RecoveryGeneration) && prior.RecoveryGeneration <= evidence.Episode.RecoveryGeneration && VerifyFundingRecoveryOwnerTakeoverReceipt(auditedEpisode, takeover, prior) == nil {
					verified = true
					evidence.ProjectionReceipts = append(evidence.ProjectionReceipts, prior)
				}
			}
			if !verified {
				return accounting.ErrRecoveryConflict
			}
		}
		evidence.ProjectionReceipts = append(evidence.ProjectionReceipts, receipt)
		latest = outbox
	}
	if latest.OperationID == "" {
		return accounting.ErrRecoveryConflict
	}
	receipt := evidence.ReleaseReceipt
	// Reconstruct the original authorization, including its original CAS and
	// runtime revision. This detects scalar/JSON tampering of any audit/hash.
	delta := uint64(1)
	holdRevision := evidence.Hold.Revision
	if receipt.State == accounting.RecoveryReleased {
		delta = 2
		if holdRevision <= 1 {
			return accounting.ErrRecoveryConflict
		}
		holdRevision--
	}
	if receipt.EpisodeRevision <= delta {
		return accounting.ErrRecoveryConflict
	}
	auth := accounting.RecoveryReleaseAuthorization{
		EpisodeID: receipt.EpisodeID, OperationID: receipt.OperationID, Actor: receipt.Actor, AuditRef: receipt.AuditRef,
		EvidenceHash: receipt.EvidenceHash, ExposureClearanceHash: receipt.ExposureClearanceHash, ProjectionHash: receipt.ProjectionHash,
		RuntimeRevision: latest.ExpectedRuntimeRevision, OwnerGeneration: receipt.OwnerGeneration,
		ExpectedEpisodeRevision: receipt.EpisodeRevision - delta, RecoveryGeneration: receipt.RecoveryGeneration, ExpectedHoldRevision: holdRevision,
		Confirmed: true, ExposureClearanceConfirmed: true,
	}
	if auth.Validate() != nil || latest.ProjectionHash != auth.ProjectionHash ||
		evidence.ProjectionReceipts[len(evidence.ProjectionReceipts)-1].EpisodeRevision != auth.ExpectedEpisodeRevision {
		return accounting.ErrRecoveryConflict
	}
	raw, err := json.Marshal(auth)
	if err != nil {
		return err
	}
	if canonicalHash(raw) != receipt.RequestHash {
		return accounting.ErrRecoveryOperationConflict
	}
	// Projected ledger rows must still agree with the immutable projections.
	// Accumulate the last child image and every event, as operations append
	// events while later operations may advance child completeness.
	children := make(map[string]accounting.RecoveryChild)
	events := make(map[string]accounting.RecoveryEvent)
	cursors := make(map[string]accounting.RecoverySourceCursor)
	for _, row := range evidence.Rows["outboxes"] {
		var outbox accounting.RecoveryOutbox
		if err = json.Unmarshal(row.Payload, &outbox); err != nil {
			return err
		}
		var projection fundingRecoveryProjection
		if err = json.Unmarshal(outbox.ProjectionPayload, &projection); err != nil {
			return err
		}
		if projection.Episode.ID != evidence.Episode.ID || projection.Episode.Revision != outbox.TargetEpisodeRevision {
			return accounting.ErrRecoveryConflict
		}
		for _, child := range projection.Children {
			children[child.ID] = child
		}
		for _, event := range projection.Events {
			events[event.ID] = event
		}
		if projection.SourceCursor != nil {
			cursors[recoverySourceCursorKey(projection.SourceCursor.Identity())] = *projection.SourceCursor
		}
	}
	if err = includeReleaseResumeReopenEvents(*evidence, events); err != nil {
		return err
	}
	for _, row := range evidence.Rows["children"] {
		var child accounting.RecoveryChild
		if err = json.Unmarshal(row.Payload, &child); err != nil {
			return err
		}
		projected, exists := children[child.ID]
		// ApplyRecoveryOperation assigns these two metadata fields when writing
		// the child, while its immutable projection carries the input image.
		projected.Revision, projected.UpdatedAt = child.Revision, child.UpdatedAt
		if !exists || !reflect.DeepEqual(projected, child) {
			return accounting.ErrRecoveryConflict
		}
		delete(children, child.ID)
	}
	for _, row := range evidence.Rows["events"] {
		var event accounting.RecoveryEvent
		if err = json.Unmarshal(row.Payload, &event); err != nil {
			return err
		}
		if !reflect.DeepEqual(events[event.ID], event) {
			return accounting.ErrRecoveryConflict
		}
		delete(events, event.ID)
	}
	for _, row := range evidence.Rows["cursors"] {
		var cursor accounting.RecoverySourceCursor
		if err = json.Unmarshal(row.Payload, &cursor); err != nil {
			return err
		}
		if !reflect.DeepEqual(cursors[row.Scalars[0]], cursor) {
			return accounting.ErrRecoveryConflict
		}
		delete(cursors, row.Scalars[0])
	}
	if len(children) != 0 || len(events) != 0 || len(cursors) != 0 {
		return accounting.ErrRecoveryConflict
	}
	return nil
}
