package accounting

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// RecoveryReleaseResumeAuthorization grants only completion/cleanup of an
// already authorized release. It never grants trading or ordinary takeover.
type RecoveryReleaseResumeAuthorization struct {
	EpisodeID               string   `json:"episode_id"`
	OperationID             string   `json:"operation_id"`
	ReleaseOperationID      string   `json:"release_operation_id"`
	ReleaseRequestHash      string   `json:"release_request_hash"`
	Actor                   string   `json:"actor"`
	AuditRef                string   `json:"audit_ref"`
	EvidenceHash            string   `json:"evidence_hash"`
	ExpectedOwnerGeneration string   `json:"expected_owner_generation"`
	OwnerGeneration         string   `json:"owner_generation"`
	AccountScopes           []string `json:"account_scopes"`
	ExpectedEpisodeRevision uint64   `json:"expected_episode_revision"`
	ExpectedHoldRevision    uint64   `json:"expected_hold_revision"`
	RecoveryGeneration      uint64   `json:"recovery_generation"`
	Confirmed               bool     `json:"confirmed"`
}

func (a RecoveryReleaseResumeAuthorization) Validate() error {
	for _, value := range []string{a.EpisodeID, a.OperationID, a.ReleaseOperationID, a.Actor, a.AuditRef, a.ExpectedOwnerGeneration, a.OwnerGeneration} {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\r\n\t") {
			return errors.New("recovery release resume requires canonical identity and audit fields")
		}
	}
	if !a.Confirmed || a.OperationID == a.ReleaseOperationID || a.ExpectedOwnerGeneration == a.OwnerGeneration ||
		!validRecoveryHash(a.ReleaseRequestHash) || !validRecoveryHash(a.EvidenceHash) || len(a.AccountScopes) == 0 ||
		a.ExpectedEpisodeRevision == 0 || a.ExpectedEpisodeRevision >= MaxDurableRecoveryRevision ||
		a.ExpectedHoldRevision == 0 || a.ExpectedHoldRevision > MaxDurableRecoveryRevision ||
		a.RecoveryGeneration == 0 || a.RecoveryGeneration > MaxDurableRecoveryRevision {
		return errors.New("incomplete recovery release resume authorization")
	}
	for i, scope := range a.AccountScopes {
		if !validRecoveryHash(scope) || (i > 0 && a.AccountScopes[i-1] >= scope) {
			return errors.New("recovery release resume requires sorted unique account scopes")
		}
	}
	return nil
}

// RecoveryReleaseResumeRecord is separate from blocked-state owner takeovers.
// ResultEvidenceHash binds the committed business snapshot with this record's
// ResultEvidenceHash blank, allowing replay to verify evidence without writes.
type RecoveryReleaseResumeRecord struct {
	OperationID             string        `json:"operation_id"`
	RequestHash             string        `json:"request_hash"`
	ReleaseOperationID      string        `json:"release_operation_id"`
	ReleaseRequestHash      string        `json:"release_request_hash"`
	Actor                   string        `json:"actor"`
	AuditRef                string        `json:"audit_ref"`
	EvidenceHash            string        `json:"evidence_hash"`
	ResultEvidenceHash      string        `json:"result_evidence_hash"`
	PreviousOwnerGeneration string        `json:"previous_owner_generation"`
	OwnerGeneration         string        `json:"owner_generation"`
	AccountScopes           []string      `json:"account_scopes"`
	ExpectedEpisodeRevision uint64        `json:"expected_episode_revision"`
	ExpectedHoldRevision    uint64        `json:"expected_hold_revision"`
	RecoveryGeneration      uint64        `json:"recovery_generation"`
	PreviousState           RecoveryState `json:"previous_state"`
	EpisodeRevision         uint64        `json:"episode_revision"`
	HoldRevision            uint64        `json:"hold_revision"`
	CreatedAt               time.Time     `json:"created_at"`
}

// ValidateOwnerHistory verifies the chronological chain across both kinds of
// owner transition. Resumptions remain distinct audit records and never imply
// the blocked-state privileges of an ordinary owner takeover.
func (e RecoveryEpisode) ValidateOwnerHistory() error {
	type transition struct {
		operation, previous, owner string
		revision                   uint64
	}
	if e.OwnerGeneration == "" || e.Revision == 0 || e.Revision > MaxDurableRecoveryRevision || e.RecoveryGeneration == 0 || e.RecoveryGeneration > MaxDurableRecoveryRevision {
		return errors.New("invalid recovery owner history episode identity")
	}
	transitions := make([]transition, 0, len(e.OwnerTakeovers)+len(e.ReleaseResumptions))
	for i, record := range e.OwnerTakeovers {
		if record.OperationID == "" || record.Actor == "" || record.AuditRef == "" || !validRecoveryHash(record.EvidenceHash) ||
			record.PreviousOwnerGeneration == "" || record.OwnerGeneration == "" || record.PreviousOwnerGeneration == record.OwnerGeneration ||
			record.EpisodeRevision == 0 || record.EpisodeRevision > e.Revision || record.CreatedAt.IsZero() ||
			(i > 0 && e.OwnerTakeovers[i-1].EpisodeRevision >= record.EpisodeRevision) {
			return fmt.Errorf("invalid owner takeover record %d", i)
		}
		transitions = append(transitions, transition{record.OperationID, record.PreviousOwnerGeneration, record.OwnerGeneration, record.EpisodeRevision})
	}
	scopes := append([]string(nil), e.AccountScopes...)
	sort.Strings(scopes)
	for i, record := range e.ReleaseResumptions {
		auth := RecoveryReleaseResumeAuthorization{EpisodeID: e.ID, OperationID: record.OperationID, ReleaseOperationID: record.ReleaseOperationID,
			ReleaseRequestHash: record.ReleaseRequestHash, Actor: record.Actor, AuditRef: record.AuditRef, EvidenceHash: record.EvidenceHash,
			ExpectedOwnerGeneration: record.PreviousOwnerGeneration, OwnerGeneration: record.OwnerGeneration, AccountScopes: record.AccountScopes,
			ExpectedEpisodeRevision: record.ExpectedEpisodeRevision, ExpectedHoldRevision: record.ExpectedHoldRevision, RecoveryGeneration: record.RecoveryGeneration, Confirmed: true}
		if auth.Validate() != nil || !validRecoveryHash(record.RequestHash) || !validRecoveryHash(record.ResultEvidenceHash) || record.CreatedAt.IsZero() ||
			record.EpisodeRevision != record.ExpectedEpisodeRevision+1 || record.EpisodeRevision > e.Revision || record.RecoveryGeneration > e.RecoveryGeneration ||
			!sameRecoveryStrings(record.AccountScopes, scopes) || (i > 0 && (e.ReleaseResumptions[i-1].EpisodeRevision >= record.EpisodeRevision || e.ReleaseResumptions[i-1].RecoveryGeneration > record.RecoveryGeneration)) {
			return fmt.Errorf("invalid release resumption record %d", i)
		}
		switch record.PreviousState {
		case RecoveryReleasePending:
			if record.ExpectedHoldRevision >= MaxDurableRecoveryRevision || record.HoldRevision != record.ExpectedHoldRevision+1 {
				return fmt.Errorf("invalid pending resumption hold CAS %d", i)
			}
		case RecoveryReleased:
			if record.HoldRevision != record.ExpectedHoldRevision {
				return fmt.Errorf("invalid released resumption hold CAS %d", i)
			}
		default:
			return fmt.Errorf("invalid resumption previous state %d", i)
		}
		transitions = append(transitions, transition{record.OperationID, record.PreviousOwnerGeneration, record.OwnerGeneration, record.EpisodeRevision})
	}
	sort.Slice(transitions, func(i, j int) bool { return transitions[i].revision < transitions[j].revision })
	seen := make(map[string]bool, len(transitions))
	for i, record := range transitions {
		if seen[record.operation] || (i > 0 && (transitions[i-1].revision == record.revision || transitions[i-1].owner != record.previous)) {
			return fmt.Errorf("disconnected recovery owner history at revision %d", record.revision)
		}
		seen[record.operation] = true
	}
	if len(transitions) > 0 && transitions[len(transitions)-1].owner != e.OwnerGeneration {
		return errors.New("recovery owner history does not match current owner")
	}
	return nil
}

// KnowsOwnerGeneration accepts an historical owner only through the complete,
// validated owner audit chain. It does not grant execution authorization.
func (e RecoveryEpisode) KnowsOwnerGeneration(owner string) bool {
	if owner == "" || e.ValidateOwnerHistory() != nil {
		return false
	}
	if e.OwnerGeneration == owner {
		return true
	}
	for _, record := range e.OwnerTakeovers {
		if record.PreviousOwnerGeneration == owner || record.OwnerGeneration == owner {
			return true
		}
	}
	for _, record := range e.ReleaseResumptions {
		if record.PreviousOwnerGeneration == owner || record.OwnerGeneration == owner {
			return true
		}
	}
	return false
}
