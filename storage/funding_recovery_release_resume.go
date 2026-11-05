package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"quantmesh/accounting"
)

// FundingRecoveryReleaseResumePreview is read under the same scoped generation
// fence as ResumeRecoveryRelease. Its digest binds all durable business evidence
// and the new capability owner; it is not permission to release by itself.
type FundingRecoveryReleaseResumePreview struct {
	Episode         accounting.RecoveryEpisode `json:"episode"`
	Hold            accounting.RecoveryHold    `json:"hold"`
	ReleaseReceipt  accounting.RecoveryReceipt `json:"release_receipt"`
	EvidenceHash    string                     `json:"evidence_hash"`
	OwnerGeneration string                     `json:"owner_generation"`
}

func (s *SQLStorage) PreviewRecoveryReleaseResume(ctx context.Context, generation FundingCarryRuntimeGeneration, episodeID, releaseOperationID string) (FundingRecoveryReleaseResumePreview, error) {
	if ctx == nil || s == nil || s.db == nil || episodeID == "" || releaseOperationID == "" {
		return FundingRecoveryReleaseResumePreview{}, ErrFundingRecoveryUnavailable
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return FundingRecoveryReleaseResumePreview{}, err
	}
	defer tx.Rollback()
	if err = s.lockFundingRecoveryGenerationForScopes(ctx, tx, generation, generation.ScopeKeys()); err != nil {
		return FundingRecoveryReleaseResumePreview{}, err
	}
	evidence, err := readReleaseResumeEvidence(ctx, tx, generation, episodeID, releaseOperationID)
	if err != nil {
		return FundingRecoveryReleaseResumePreview{}, err
	}
	if evidence.Episode.OwnerGeneration == generation.ID() || evidence.Episode.Revision >= accounting.MaxDurableRecoveryRevision ||
		(evidence.Hold.State == "held" && evidence.Hold.Revision >= accounting.MaxDurableRecoveryRevision) {
		return FundingRecoveryReleaseResumePreview{}, accounting.ErrRecoveryConflict
	}
	digest, err := releaseResumeEvidenceHash(evidence, generation.ID(), false)
	if err != nil {
		return FundingRecoveryReleaseResumePreview{}, err
	}
	return FundingRecoveryReleaseResumePreview{Episode: evidence.Episode, Hold: evidence.Hold, ReleaseReceipt: evidence.ReleaseReceipt,
		EvidenceHash: digest, OwnerGeneration: generation.ID()}, nil
}

func releaseResumeRequestHash(auth accounting.RecoveryReleaseResumeAuthorization) (string, error) {
	data, err := json.Marshal(struct {
		Kind          string                                        `json:"kind"`
		Authorization accounting.RecoveryReleaseResumeAuthorization `json:"authorization"`
	}{"recovery_release_resume_v1", auth})
	if err != nil {
		return "", err
	}
	return canonicalHash(data), nil
}

// ResumeRecoveryRelease atomically transfers cleanup responsibility and, only
// for release_pending/held, completes the existing release. Original receipt
// attribution stays immutable. The returned receipt belongs to the new resume.
func (s *SQLStorage) ResumeRecoveryRelease(ctx context.Context, generation FundingCarryRuntimeGeneration, auth accounting.RecoveryReleaseResumeAuthorization) (episode accounting.RecoveryEpisode, hold accounting.RecoveryHold, receipt accounting.RecoveryReceipt, replayed bool, err error) {
	if err = auth.Validate(); err != nil {
		return
	}
	if ctx == nil || s == nil || s.db == nil {
		err = ErrFundingRecoveryUnavailable
		return
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer tx.Rollback()
	// Fence the live capability and its exact account scope BEFORE any receipt
	// lookup. An old owner can never use idempotency as an authorization bypass.
	if err = s.lockFundingRecoveryGenerationForScopes(ctx, tx, generation, auth.AccountScopes); err != nil {
		return
	}
	if auth.OwnerGeneration != generation.ID() {
		err = accounting.ErrRecoveryConflict
		return
	}
	evidence, err := readReleaseResumeEvidence(ctx, tx, generation, auth.EpisodeID, auth.ReleaseOperationID)
	if err != nil {
		return
	}
	if evidence.ReleaseReceipt.RequestHash != auth.ReleaseRequestHash {
		err = accounting.ErrRecoveryOperationConflict
		return
	}
	requestHash, err := releaseResumeRequestHash(auth)
	if err != nil {
		return
	}
	prior, exists, err := readReleaseResumeReceipt(ctx, tx, auth.EpisodeID, auth.OperationID)
	if err != nil {
		return
	}
	if exists {
		if prior.RequestHash != requestHash {
			err = accounting.ErrRecoveryOperationConflict
			return
		}
		if err = validateReleaseResumeReplay(evidence, auth, prior); err != nil {
			return
		}
		return evidence.Episode, evidence.Hold, prior, true, nil
	}
	if evidence.Episode.OwnerGeneration != auth.ExpectedOwnerGeneration || evidence.Episode.Revision != auth.ExpectedEpisodeRevision ||
		evidence.Episode.RecoveryGeneration != auth.RecoveryGeneration || evidence.Hold.Revision != auth.ExpectedHoldRevision ||
		evidence.Episode.Revision >= accounting.MaxDurableRecoveryRevision ||
		(evidence.Hold.State == "held" && evidence.Hold.Revision >= accounting.MaxDurableRecoveryRevision) {
		err = accounting.ErrRecoveryConflict
		return
	}
	digest, err := releaseResumeEvidenceHash(evidence, generation.ID(), false)
	if err != nil {
		return
	}
	if digest != auth.EvidenceHash {
		err = accounting.ErrRecoveryConflict
		return
	}
	before := evidence
	episode, hold = evidence.Episode, evidence.Hold
	now := time.Now().UTC()
	episode.Revision++
	episode.State = accounting.RecoveryReleased
	episode.OwnerGeneration = generation.ID()
	episode.UpdatedAt = now
	if before.Episode.State == accounting.RecoveryReleasePending {
		hold.Revision++
		hold.State = "released"
		hold.ReleasedByOperation = auth.ReleaseOperationID
		hold.UpdatedAt = now
		evidence.ReleaseReceipt.State = accounting.RecoveryReleased
		evidence.ReleaseReceipt.EpisodeRevision = episode.Revision
	}
	record := accounting.RecoveryReleaseResumeRecord{
		OperationID: auth.OperationID, RequestHash: requestHash, ReleaseOperationID: auth.ReleaseOperationID, ReleaseRequestHash: auth.ReleaseRequestHash,
		Actor: auth.Actor, AuditRef: auth.AuditRef, EvidenceHash: auth.EvidenceHash,
		PreviousOwnerGeneration: auth.ExpectedOwnerGeneration, OwnerGeneration: auth.OwnerGeneration, AccountScopes: append([]string(nil), auth.AccountScopes...),
		ExpectedEpisodeRevision: auth.ExpectedEpisodeRevision, ExpectedHoldRevision: auth.ExpectedHoldRevision, RecoveryGeneration: auth.RecoveryGeneration,
		PreviousState: before.Episode.State, EpisodeRevision: episode.Revision, HoldRevision: hold.Revision, CreatedAt: now,
	}
	episode.ReleaseResumptions = append(append([]accounting.RecoveryReleaseResumeRecord(nil), episode.ReleaseResumptions...), record)
	evidence.Episode, evidence.Hold = episode, hold
	// The completed receipt is itself durable evidence: replay reconstructs the
	// same list from all resumption records, so include it before hashing the
	// committed snapshot or every legitimate retry would appear tampered.
	receipt = releaseResumeReceiptForRecord(episode.ID, record)
	evidence.ResumeReceipts = append(evidence.ResumeReceipts, receipt)
	record.ResultEvidenceHash, err = releaseResumeEvidenceHash(evidence, generation.ID(), true)
	if err != nil {
		return
	}
	episode.ReleaseResumptions[len(episode.ReleaseResumptions)-1] = record
	if before.Episode.State == accounting.RecoveryReleasePending {
		if err = updateReleaseResumeHold(ctx, tx, before.Hold, hold); err != nil {
			return
		}
		if err = updateReleaseResumeOriginalReceipt(ctx, tx, before.ReleaseReceipt, evidence.ReleaseReceipt); err != nil {
			return
		}
	}
	if err = updateReleaseResumeEpisode(ctx, tx, before.Episode, episode); err != nil {
		return
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		return
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO funding_recovery_operations(episode_id,operation_id,request_hash,phase,receipt_json) VALUES(?,?,?,?,?)`,
		episode.ID, receipt.OperationID, receipt.RequestHash, string(receipt.State), raw)
	if err = releaseResumeAffectedOne(result, err); err != nil {
		return
	}
	err = tx.Commit()
	return
}

func releaseResumeReceiptForRecord(episodeID string, record accounting.RecoveryReleaseResumeRecord) accounting.RecoveryReceipt {
	return accounting.RecoveryReceipt{
		EpisodeID: episodeID, OperationID: record.OperationID, RequestHash: record.RequestHash, EpisodeRevision: record.EpisodeRevision,
		RecoveryGeneration: record.RecoveryGeneration, State: accounting.RecoveryReleased, ProjectionHash: record.EvidenceHash,
		Actor: record.Actor, AuditRef: record.AuditRef, EvidenceHash: record.EvidenceHash,
		PreviousOwnerGeneration: record.PreviousOwnerGeneration, OwnerGeneration: record.OwnerGeneration,
		ReleaseOperationID: record.ReleaseOperationID, ReleaseRequestHash: record.ReleaseRequestHash, CreatedAt: record.CreatedAt,
	}
}

func validateReleaseResumeReplay(evidence releaseResumeEvidence, auth accounting.RecoveryReleaseResumeAuthorization, receipt accounting.RecoveryReceipt) error {
	episode, hold := evidence.Episode, evidence.Hold
	if episode.State != accounting.RecoveryReleased || hold.State != "released" || episode.OwnerGeneration != auth.OwnerGeneration ||
		episode.RecoveryGeneration != auth.RecoveryGeneration || episode.Revision != auth.ExpectedEpisodeRevision+1 ||
		len(episode.ReleaseResumptions) == 0 {
		return accounting.ErrRecoveryConflict
	}
	record := episode.ReleaseResumptions[len(episode.ReleaseResumptions)-1]
	if record.OperationID != auth.OperationID || record.ExpectedEpisodeRevision != auth.ExpectedEpisodeRevision ||
		record.ExpectedHoldRevision != auth.ExpectedHoldRevision || record.HoldRevision != hold.Revision ||
		record.PreviousOwnerGeneration != auth.ExpectedOwnerGeneration || record.EvidenceHash != auth.EvidenceHash ||
		!reflect.DeepEqual(receipt, releaseResumeReceiptForRecord(episode.ID, record)) {
		return accounting.ErrRecoveryConflict
	}
	digest, err := releaseResumeEvidenceHash(evidence, auth.OwnerGeneration, true)
	if err != nil {
		return err
	}
	if digest != record.ResultEvidenceHash {
		return accounting.ErrRecoveryConflict
	}
	return nil
}

func readReleaseResumeReceipt(ctx context.Context, tx *sql.Tx, episodeID, operationID string) (accounting.RecoveryReceipt, bool, error) {
	var raw []byte
	var hash, phase string
	err := tx.QueryRowContext(ctx, `SELECT request_hash,phase,receipt_json FROM funding_recovery_operations WHERE episode_id=? AND operation_id=?`, episodeID, operationID).Scan(&hash, &phase, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return accounting.RecoveryReceipt{}, false, nil
	}
	if err != nil {
		return accounting.RecoveryReceipt{}, false, err
	}
	var receipt accounting.RecoveryReceipt
	if err = json.Unmarshal(raw, &receipt); err != nil {
		return receipt, false, err
	}
	if receipt.EpisodeID != episodeID || receipt.OperationID != operationID || receipt.RequestHash != hash || string(receipt.State) != phase || !validRecoveryAnomalyDigest(hash) {
		return accounting.RecoveryReceipt{}, false, accounting.ErrRecoveryConflict
	}
	return receipt, true, nil
}

func updateReleaseResumeEpisode(ctx context.Context, tx *sql.Tx, before, after accounting.RecoveryEpisode) error {
	raw, err := json.Marshal(after)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE funding_recovery_episodes SET revision=?,state=?,payload_json=?,updated_at=CURRENT_TIMESTAMP WHERE episode_id=? AND revision=? AND recovery_generation=? AND state=?`,
		after.Revision, string(after.State), raw, before.ID, before.Revision, before.RecoveryGeneration, string(before.State))
	return releaseResumeAffectedOne(result, err)
}

func updateReleaseResumeHold(ctx context.Context, tx *sql.Tx, before, after accounting.RecoveryHold) error {
	raw, err := json.Marshal(after)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE funding_recovery_holds SET hold_revision=?,state=?,payload_json=?,updated_at=CURRENT_TIMESTAMP WHERE hold_id=? AND episode_id=? AND recovery_generation=? AND hold_revision=? AND state=?`,
		after.Revision, after.State, raw, before.ID, before.EpisodeID, before.Generation, before.Revision, before.State)
	return releaseResumeAffectedOne(result, err)
}

func updateReleaseResumeOriginalReceipt(ctx context.Context, tx *sql.Tx, before, after accounting.RecoveryReceipt) error {
	raw, err := json.Marshal(after)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE funding_recovery_operations SET phase=?,receipt_json=? WHERE episode_id=? AND operation_id=? AND request_hash=? AND phase=?`,
		string(after.State), raw, before.EpisodeID, before.OperationID, before.RequestHash, string(before.State))
	return releaseResumeAffectedOne(result, err)
}

func releaseResumeAffectedOne(result sql.Result, err error) error {
	if err != nil {
		return fmt.Errorf("persist recovery release resume: %w", recoveryConflict(err))
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check recovery release resume CAS: %w", err)
	}
	if n != 1 {
		return accounting.ErrRecoveryConflict
	}
	return nil
}
