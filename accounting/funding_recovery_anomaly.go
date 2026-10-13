package accounting

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
)

const RecoveryManualFundingAdjustmentSource = "manual_funding_adjustment_v1"

type RecoverySourceAnomalyKind string

const (
	RecoverySourceAnomalyLate     RecoverySourceAnomalyKind = "late_insert"
	RecoverySourceAnomalyChanged  RecoverySourceAnomalyKind = "changed"
	RecoverySourceAnomalyDeleted  RecoverySourceAnomalyKind = "deleted"
	RecoverySourceAnomalyMetadata RecoverySourceAnomalyKind = "metadata_changed"
)

type RecoverySourceAnomalyState string

const (
	RecoverySourceAnomalyOpen              RecoverySourceAnomalyState = "open"
	RecoverySourceAnomalyAdjustmentApplied RecoverySourceAnomalyState = "adjustment_applied"
	RecoverySourceAnomalyVerified          RecoverySourceAnomalyState = "verified"
)

// RecoverySourceAnomalyFact is an immutable comparison of one source row
// against the funding fact already recorded in the recovery ledger. Exactly
// one side may be nil for late/deleted rows.
type RecoverySourceAnomalyFact struct {
	Kind          RecoverySourceAnomalyKind `json:"kind"`
	ExternalID    string                    `json:"external_id"`
	OriginalEvent *RecoveryEvent            `json:"original_event,omitempty"`
	ObservedEvent *RecoveryEvent            `json:"observed_event,omitempty"`
	DeltaAmount   string                    `json:"delta_amount_raw"`
}

// RecoverySourceAnomaly remains durable until a separately audited
// adjustment is appended and a later complete source scan verifies it.
type RecoverySourceAnomaly struct {
	ID                     string                      `json:"anomaly_id"`
	EpisodeID              string                      `json:"episode_id"`
	RecoveryGeneration     uint64                      `json:"recovery_generation"`
	EpisodeRevision        uint64                      `json:"episode_revision"`
	Identity               RecoverySourceIdentity      `json:"source_identity"`
	CursorRevision         uint64                      `json:"cursor_revision"`
	InitialFrom            time.Time                   `json:"initial_from"`
	Through                time.Time                   `json:"through"`
	ExpectedHistoryHash    string                      `json:"expected_history_hash"`
	ObservedHistoryHash    string                      `json:"observed_history_hash"`
	Facts                  []RecoverySourceAnomalyFact `json:"facts"`
	UnclassifiedReason     string                      `json:"unclassified_reason,omitempty"`
	Revision               uint64                      `json:"revision"`
	State                  RecoverySourceAnomalyState  `json:"state"`
	DetectedActor          string                      `json:"detected_actor"`
	DetectedAuditRef       string                      `json:"detected_audit_ref"`
	DetectedAt             time.Time                   `json:"detected_at"`
	AdjustmentOperationID  string                      `json:"adjustment_operation_id,omitempty"`
	AdjustmentActor        string                      `json:"adjustment_actor,omitempty"`
	AdjustmentAuditRef     string                      `json:"adjustment_audit_ref,omitempty"`
	AdjustmentEvidenceHash string                      `json:"adjustment_evidence_hash,omitempty"`
	VerifiedHistoryHash    string                      `json:"verified_history_hash,omitempty"`
	VerifiedAt             time.Time                   `json:"verified_at,omitempty"`
}

// RecoveryFundingAdjustmentEvidence binds a signed raw-amount delta to the
// source facts and operator evidence. Original events are never overwritten.
type RecoveryFundingAdjustmentEvidence struct {
	AnomalyID          string    `json:"anomaly_id"`
	OriginalExternalID string    `json:"original_external_id"`
	OriginalSourceHash string    `json:"original_source_hash,omitempty"`
	ObservedSourceHash string    `json:"observed_source_hash,omitempty"`
	OriginalAmountRaw  string    `json:"original_amount_raw,omitempty"`
	ObservedAmountRaw  string    `json:"observed_amount_raw,omitempty"`
	ObservedRowAt      time.Time `json:"observed_row_at,omitempty"`
	DeltaAmountRaw     string    `json:"delta_amount_raw"`
	EvidenceHash       string    `json:"evidence_hash"`
	Actor              string    `json:"actor"`
	AuditRef           string    `json:"audit_ref"`
	ConfirmedAt        time.Time `json:"confirmed_at"`
}

func (f RecoverySourceAnomalyFact) Validate(episodeID string, identity RecoverySourceIdentity) error {
	if strings.TrimSpace(f.ExternalID) == "" || f.ExternalID != strings.TrimSpace(f.ExternalID) {
		return errors.New("source anomaly external ID is required")
	}
	if (f.OriginalEvent == nil) == (f.ObservedEvent == nil) && f.Kind != RecoverySourceAnomalyChanged && f.Kind != RecoverySourceAnomalyMetadata {
		return errors.New("late/deleted anomaly must have exactly one source fact")
	}
	if f.OriginalEvent == nil && f.ObservedEvent == nil {
		return errors.New("source anomaly must preserve at least one source fact")
	}
	for _, event := range []*RecoveryEvent{f.OriginalEvent, f.ObservedEvent} {
		if event == nil {
			continue
		}
		if err := event.Validate(); err != nil {
			return fmt.Errorf("validate source anomaly fact: %w", err)
		}
		if event.EpisodeID != episodeID || event.Source != identity.Source || event.Exchange != identity.Exchange || event.Market != identity.Market ||
			event.AccountScope != identity.AccountScope || event.Symbol != identity.Symbol || event.Kind != identity.Kind || event.Asset != identity.Asset || event.ExternalID != f.ExternalID {
			return errors.New("source anomaly fact identity does not match its source scope")
		}
	}
	switch f.Kind {
	case RecoverySourceAnomalyLate:
		if f.OriginalEvent != nil || f.ObservedEvent == nil {
			return errors.New("late source anomaly requires only the observed fact")
		}
	case RecoverySourceAnomalyDeleted:
		if f.OriginalEvent == nil || f.ObservedEvent != nil {
			return errors.New("deleted source anomaly requires only the original fact")
		}
	case RecoverySourceAnomalyChanged, RecoverySourceAnomalyMetadata:
		if f.OriginalEvent == nil || f.ObservedEvent == nil {
			return errors.New("changed source anomaly requires original and observed facts")
		}
	default:
		return errors.New("unsupported source anomaly kind")
	}
	expectedDelta, err := RecoveryFundingAmountDelta(f.OriginalEvent, f.ObservedEvent)
	if err != nil {
		return err
	}
	if !rawRecoveryDecimalsEqual(f.DeltaAmount, expectedDelta) {
		return errors.New("source anomaly delta does not equal the exact observed-minus-original amount")
	}
	return nil
}

func (a RecoverySourceAnomaly) Validate() error {
	if !validRecoveryHash(a.ID) || strings.TrimSpace(a.EpisodeID) == "" || a.RecoveryGeneration == 0 || a.EpisodeRevision == 0 || a.CursorRevision == 0 ||
		a.Revision == 0 || a.InitialFrom.IsZero() || a.Through.IsZero() || !a.InitialFrom.Before(a.Through) || a.DetectedAt.IsZero() ||
		a.DetectedActor == "" || a.DetectedAuditRef == "" || !validRecoveryHash(a.ExpectedHistoryHash) || !validRecoveryHash(a.ObservedHistoryHash) ||
		(len(a.Facts) == 0 && strings.TrimSpace(a.UnclassifiedReason) == "") || (len(a.Facts) > 0 && a.UnclassifiedReason != "") {
		return errors.New("incomplete durable source anomaly")
	}
	if err := a.Identity.Validate(); err != nil {
		return err
	}
	for _, fact := range a.Facts {
		if err := fact.Validate(a.EpisodeID, a.Identity); err != nil {
			return err
		}
	}
	switch a.State {
	case RecoverySourceAnomalyOpen:
		if a.AdjustmentOperationID != "" || a.VerifiedAt.IsZero() == false || a.VerifiedHistoryHash != "" {
			return errors.New("open source anomaly cannot claim adjustment or verification")
		}
	case RecoverySourceAnomalyAdjustmentApplied:
		if a.AdjustmentOperationID == "" || a.AdjustmentActor == "" || a.AdjustmentAuditRef == "" || !validRecoveryHash(a.AdjustmentEvidenceHash) || !a.VerifiedAt.IsZero() || a.VerifiedHistoryHash != "" {
			return errors.New("adjusted source anomaly lacks its immutable adjustment audit")
		}
	case RecoverySourceAnomalyVerified:
		if a.AdjustmentOperationID == "" || a.AdjustmentActor == "" || a.AdjustmentAuditRef == "" || !validRecoveryHash(a.AdjustmentEvidenceHash) || a.VerifiedAt.IsZero() || !validRecoveryHash(a.VerifiedHistoryHash) {
			return errors.New("verified source anomaly lacks adjusted and rescanned evidence")
		}
	default:
		return errors.New("unsupported source anomaly state")
	}
	return nil
}

func (a RecoverySourceAnomaly) Fingerprint() (string, error) {
	a.ID = ""
	a.Revision = 0
	a.DetectedActor, a.DetectedAuditRef = "", ""
	a.DetectedAt = time.Time{}
	data, err := json.Marshal(a)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func (e RecoveryFundingAdjustmentEvidence) Validate() error {
	if e.AnomalyID == "" || e.OriginalExternalID == "" || e.Actor == "" || e.AuditRef == "" || e.ConfirmedAt.IsZero() || !validRecoveryHash(e.EvidenceHash) {
		return errors.New("funding adjustment requires anomaly, source identity, operator audit, and evidence")
	}
	if err := validateRawRecoveryDecimal(e.DeltaAmountRaw, true); err != nil {
		return fmt.Errorf("funding adjustment delta: %w", err)
	}
	if e.OriginalAmountRaw != "" {
		if err := validateRawRecoveryDecimal(e.OriginalAmountRaw, true); err != nil {
			return fmt.Errorf("original funding amount: %w", err)
		}
	}
	if e.ObservedAmountRaw != "" {
		if err := validateRawRecoveryDecimal(e.ObservedAmountRaw, true); err != nil {
			return fmt.Errorf("observed funding amount: %w", err)
		}
		if e.ObservedRowAt.IsZero() {
			return errors.New("observed funding row requires its source timestamp")
		}
	} else if !e.ObservedRowAt.IsZero() {
		return errors.New("deleted funding fact cannot have an observed source timestamp")
	}
	if !validOptionalRecoveryHash(e.OriginalSourceHash) || !validOptionalRecoveryHash(e.ObservedSourceHash) {
		return errors.New("funding adjustment source hash is invalid")
	}
	expected, err := RecoveryFundingAmountDeltaRaw(e.OriginalAmountRaw, e.ObservedAmountRaw)
	if err != nil {
		return err
	}
	if !rawRecoveryDecimalsEqual(e.DeltaAmountRaw, expected) {
		return errors.New("funding adjustment delta does not match exact source amounts")
	}
	return nil
}

// RecoveryFundingAmountDelta returns observed-original using exact decimal
// arithmetic. Nil facts represent an absent original or observed source row.
func RecoveryFundingAmountDelta(original, observed *RecoveryEvent) (string, error) {
	oldAmount, newAmount := "", ""
	if original != nil {
		oldAmount = original.Amount
	}
	if observed != nil {
		newAmount = observed.Amount
	}
	return RecoveryFundingAmountDeltaRaw(oldAmount, newAmount)
}

func RecoveryFundingAmountDeltaRaw(original, observed string) (string, error) {
	oldValue, newValue := new(big.Rat), new(big.Rat)
	if original != "" {
		if err := validateRawRecoveryDecimal(original, true); err != nil {
			return "", err
		}
		if _, ok := oldValue.SetString(original); !ok {
			return "", errors.New("parse original funding amount")
		}
	}
	if observed != "" {
		if err := validateRawRecoveryDecimal(observed, true); err != nil {
			return "", err
		}
		if _, ok := newValue.SetString(observed); !ok {
			return "", errors.New("parse observed funding amount")
		}
	}
	delta := new(big.Rat).Sub(newValue, oldValue)
	precision := max(rawDecimalPrecision(original), rawDecimalPrecision(observed))
	result := delta.FloatString(precision)
	if strings.Contains(result, ".") {
		result = strings.TrimRight(strings.TrimRight(result, "0"), ".")
	}
	if result == "" || result == "-0" {
		result = "0"
	}
	return result, nil
}

func rawDecimalPrecision(value string) int {
	if point := strings.IndexByte(value, '.'); point >= 0 {
		return len(value) - point - 1
	}
	return 0
}

func validOptionalRecoveryHash(value string) bool {
	return value == "" || validRecoveryHash(value)
}
