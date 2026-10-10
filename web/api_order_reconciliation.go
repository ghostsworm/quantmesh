package web

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"quantmesh/accounting"
	"quantmesh/storage"
)

type orderReconciliationRepository interface {
	CreateOrderReconciliationCase(context.Context, storage.NewOrderReconciliationCase) (storage.OrderReconciliationCase, bool, error)
	GetOrderReconciliation(context.Context, string) (storage.OrderReconciliationCase, bool, error)
	ListOrderReconciliations(context.Context, int) ([]storage.OrderReconciliationCase, error)
	StartOrderReconciliation(context.Context, string, int64, string, accounting.ReconcileRequest, string) (storage.OrderReconciliationCase, error)
	TransitionOrderReconciliation(context.Context, string, int64, storage.OrderReconciliationStatus, storage.OrderReconciliationStatus, string, string, string, []byte, *storage.OrderReconciliationReceipt, string) (storage.OrderReconciliationCase, error)
	ListOrderReconciliationAudit(context.Context, string) ([]storage.OrderReconciliationAudit, error)
}

// TrustedOrderEvidenceProvider must read venue fills/fees server-side. Client
// evidence references are labels only and never become economic evidence.
type TrustedOrderEvidenceProvider interface {
	ReadVerifiedOrderEvidence(context.Context, storage.OrderReconciliationCase) (VerifiedOrderEvidence, error)
}

type VerifiedOrderEvidence struct {
	Owner          accounting.OrderOwner
	IntentRevision string
	OwnerMatched   bool
	VenueTerminal  bool
	FillsComplete  bool
	FeesComplete   bool
	Target         accounting.Cursor
	Fills          []accounting.VerifiedFill
	Fees           []accounting.VerifiedFee
	ExpectedResult accounting.EconomicResult
	Summary        accounting.EvidenceSummary
}

// OrderReconciliationGateBridge must release only the quarantine source owned
// by this exact order/case. It must be durable and idempotent.
type OrderReconciliationGateBridge interface {
	ReleaseOrderQuarantine(context.Context, storage.OrderReconciliationCase, string) error
}

// OrderAccountingOperationReader is optional, but preferred for response-loss
// recovery. If unavailable, the identical durable operation ID is resubmitted
// only after strategy state has been read back.
type OrderAccountingOperationReader interface {
	ReadOrderAccountingReceipt(context.Context, string) (accounting.ReconciliationReceipt, bool, error)
}

type OrderReconciliationService struct {
	Repository orderReconciliationRepository
	Evidence   TrustedOrderEvidenceProvider
	Accounting accounting.OrderAccounting
	Gate       OrderReconciliationGateBridge
}

type OrderReconciliationValidation struct {
	OwnerMatched          bool     `json:"owner_matched"`
	IntentRevisionMatched bool     `json:"intent_revision_matched"`
	VenueTerminal         bool     `json:"venue_terminal"`
	FillsComplete         bool     `json:"fills_complete"`
	FeesComplete          bool     `json:"fees_complete"`
	StrategyReadback      bool     `json:"strategy_readback"`
	GateAvailable         bool     `json:"gate_bridge_available"`
	EvidenceMatchesCase   bool     `json:"evidence_matches_case"`
	ReleaseRetryable      bool     `json:"release_retryable"`
	ReleaseReady          bool     `json:"release_ready"`
	Blockers              []string `json:"blockers"`
}

// The DTO deliberately excludes the stored operation request (which contains
// fills/fees). Unavailable evidence is represented as null/false + blockers.
type OrderReconciliationCaseDTO struct {
	ID                       string                            `json:"case_id"`
	IdempotencyKey           string                            `json:"idempotency_key"`
	Owner                    accounting.OrderOwner             `json:"owner"`
	ExpectedIntentRevision   string                            `json:"expected_intent_revision"`
	ExpectedStrategyRevision string                            `json:"expected_strategy_revision"`
	Reason                   string                            `json:"reason"`
	EvidenceRefs             []string                          `json:"evidence_refs"`
	Status                   storage.OrderReconciliationStatus `json:"status"`
	Revision                 int64                             `json:"revision"`
	Receipt                  *orderReconciliationReceiptDTO    `json:"receipt,omitempty"`
	ReleaseEvidenceHash      string                            `json:"release_evidence_hash,omitempty"`
	CreatedBy                string                            `json:"created_by"`
	CreatedAt                time.Time                         `json:"created_at"`
	UpdatedAt                time.Time                         `json:"updated_at"`
	StrategyCursor           *accounting.AccountingSnapshot    `json:"strategy_cursor"`
	Validation               OrderReconciliationValidation     `json:"validation"`
}
type orderReconciliationReceiptDTO struct {
	Outcome          string                    `json:"outcome"`
	OperationID      string                    `json:"operation_id"`
	Owner            accounting.OrderOwner     `json:"owner"`
	ExpectedRevision string                    `json:"expected_strategy_revision"`
	From             accounting.Cursor         `json:"from"`
	Target           accounting.Cursor         `json:"target"`
	Revision         string                    `json:"revision"`
	Result           accounting.EconomicResult `json:"result"`
	CompletedAt      time.Time                 `json:"completed_at"`
}

var orderReconciliationServiceState struct {
	sync.RWMutex
	service *OrderReconciliationService
}

func SetOrderReconciliationService(service *OrderReconciliationService) {
	orderReconciliationServiceState.Lock()
	orderReconciliationServiceState.service = service
	orderReconciliationServiceState.Unlock()
}
func currentOrderReconciliationService() *OrderReconciliationService {
	orderReconciliationServiceState.RLock()
	defer orderReconciliationServiceState.RUnlock()
	return orderReconciliationServiceState.service
}

type createOrderReconciliationRequest struct {
	Owner                    accounting.OrderOwner `json:"owner"`
	ExpectedIntentRevision   string                `json:"expected_intent_revision"`
	ExpectedStrategyRevision string                `json:"expected_strategy_revision"`
	Reason                   string                `json:"reason"`
	EvidenceRefs             []string              `json:"evidence_refs"`
	IdempotencyKey           string                `json:"idempotency_key"`
}
type confirmOrderReconciliationRequest struct {
	CaseRevision int64 `json:"case_revision"`
	Confirm      bool  `json:"confirm"`
}
type releaseOrderReconciliationRequest struct {
	CaseRevision int64  `json:"case_revision"`
	EvidenceHash string `json:"evidence_hash"`
	Confirm      bool   `json:"confirm"`
}

func listOrderReconciliationsHandler(c *gin.Context) {
	if !requireRetiredEquityAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin_required"})
		return
	}
	s := currentOrderReconciliationService()
	if s == nil || s.Repository == nil {
		reconciliationUnavailable(c)
		return
	}
	items, err := s.Repository.ListOrderReconciliations(c.Request.Context(), 100)
	if err != nil {
		reconciliationUnavailable(c)
		return
	}
	dtos := make([]OrderReconciliationCaseDTO, 0, len(items))
	for _, item := range items {
		dtos = append(dtos, buildOrderReconciliationCaseDTO(c.Request.Context(), s, item))
	}
	c.JSON(http.StatusOK, gin.H{"cases": dtos})
}

func createOrderReconciliationHandler(c *gin.Context) {
	if !requireRetiredEquityAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin_required"})
		return
	}
	s := currentOrderReconciliationService()
	if s == nil || s.Repository == nil {
		reconciliationUnavailable(c)
		return
	}
	var req createOrderReconciliationRequest
	if !decodeStrictJSON(c, &req) {
		return
	}
	if err := req.Owner.Validate(); err != nil || strings.TrimSpace(req.ExpectedIntentRevision) == "" || strings.TrimSpace(req.ExpectedStrategyRevision) == "" || strings.TrimSpace(req.Reason) == "" || len(req.Reason) > 2000 || len(req.EvidenceRefs) == 0 || len(req.EvidenceRefs) > 32 || strings.TrimSpace(req.IdempotencyKey) == "" || len(req.IdempotencyKey) > 128 {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "invalid_reconciliation_case"})
		return
	}
	for _, ref := range req.EvidenceRefs {
		if strings.TrimSpace(ref) == "" || len(ref) > 512 {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "invalid_evidence_reference"})
			return
		}
	}
	actor := orderReconciliationActor(c)
	id, err := newOrderReconciliationID()
	if err != nil {
		reconciliationUnavailable(c)
		return
	}
	item, replayed, err := s.Repository.CreateOrderReconciliationCase(c.Request.Context(), storage.NewOrderReconciliationCase{ID: id, IdempotencyKey: req.IdempotencyKey, Owner: req.Owner, ExpectedIntentRevision: req.ExpectedIntentRevision, ExpectedStrategyRevision: req.ExpectedStrategyRevision, Reason: req.Reason, Actor: actor, EvidenceRefs: req.EvidenceRefs})
	if errors.Is(err, storage.ErrOrderReconciliationConflict) {
		c.JSON(http.StatusConflict, gin.H{"error": "idempotency_conflict"})
		return
	}
	if err != nil {
		reconciliationUnavailable(c)
		return
	}
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	c.JSON(status, gin.H{"case": buildOrderReconciliationCaseDTO(c.Request.Context(), s, item), "status": string(item.Status), "evidence_verified": false})
}

func reconcileOrderReconciliationHandler(c *gin.Context) {
	if !requireRetiredEquityAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin_required"})
		return
	}
	s := currentOrderReconciliationService()
	if s == nil || s.Repository == nil {
		reconciliationUnavailable(c)
		return
	}
	var req confirmOrderReconciliationRequest
	if !decodeStrictJSON(c, &req) {
		return
	}
	if !req.Confirm || req.CaseRevision < 1 {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "explicit_confirmation_required"})
		return
	}
	item, found, err := s.Repository.GetOrderReconciliation(c.Request.Context(), c.Param("case_id"))
	if err != nil {
		reconciliationUnavailable(c)
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "reconciliation_case_not_found"})
		return
	}
	if item.Revision != req.CaseRevision || (item.Status != storage.OrderReconciliationPrepared && item.Status != storage.OrderReconciliationRunning) {
		c.JSON(http.StatusConflict, gin.H{"error": "reconciliation_case_revision_or_state_conflict", "case": buildOrderReconciliationCaseDTO(c.Request.Context(), s, item)})
		return
	}
	if s.Accounting == nil {
		reconciliationUnavailable(c)
		return
	}
	if s.Evidence == nil {
		reconciliationUnavailable(c)
		return
	}
	evidence, evidenceErr := s.Evidence.ReadVerifiedOrderEvidence(c.Request.Context(), item)
	if evidenceErr != nil || !verifiedOrderEvidenceComplete(evidence, item) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "trusted_order_evidence_unavailable"})
		return
	}
	evidenceHash, hashErr := verifiedOrderEvidenceHash(item, evidence)
	if hashErr != nil {
		reconciliationUnavailable(c)
		return
	}
	before, readErr := s.Accounting.ReadOrderAccounting(c.Request.Context(), item.Owner)
	if readErr != nil || before.Validate() != nil || before.Owner != item.Owner {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "strategy_reconciliation_readback_unavailable", "case": buildOrderReconciliationCaseDTO(c.Request.Context(), s, item)})
		return
	}
	if item.Status == storage.OrderReconciliationPrepared {
		if before.Revision != item.ExpectedStrategyRevision {
			c.JSON(http.StatusConflict, gin.H{"error": "strategy_accounting_revision_conflict"})
			return
		}
		fills, fees, prefixOK := verifiedHistorySuffix(evidence, item, before.Cursor)
		if !prefixOK {
			c.JSON(http.StatusConflict, gin.H{"error": "strategy_cursor_is_not_verified_history_prefix"})
			return
		}
		if before.Cursor == evidence.Target {
			if !equalEconomicResult(before.Result, evidence.ExpectedResult) {
				c.JSON(http.StatusConflict, gin.H{"error": "already_accounted_economic_result_mismatch"})
				return
			}
			receipt := alreadyAccountedReceipt(item, evidence, before)
			detail := mustJSON(map[string]string{"outcome": "already_accounted", "evidence_sha256": evidenceHash, "strategy_revision": before.Revision})
			ready, transitionErr := s.Repository.TransitionOrderReconciliation(c.Request.Context(), item.ID, item.Revision, storage.OrderReconciliationPrepared, storage.OrderReconciliationReady, orderReconciliationActor(c), "already_accounted_readback", "verified", detail, &receipt, evidenceHash)
			if transitionErr != nil {
				reconciliationUnavailable(c)
				return
			}
			c.JSON(http.StatusOK, gin.H{"case": buildOrderReconciliationCaseDTO(c.Request.Context(), s, ready), "status": "ready_to_release", "release_requires_confirmation": true})
			return
		}
		manual := accounting.ReconcileRequest{OperationID: item.ID, Owner: item.Owner, ExpectedRevision: before.Revision, From: before.Cursor, Target: evidence.Target, Fills: fills, Fees: fees, Evidence: evidence.Summary}
		if err := manual.Validate(); err != nil {
			c.JSON(http.StatusConflict, gin.H{"error": "verified_order_evidence_invalid"})
			return
		}
		item, err = s.Repository.StartOrderReconciliation(c.Request.Context(), item.ID, item.Revision, orderReconciliationActor(c), manual, evidenceHash)
		if err != nil {
			if errors.Is(err, storage.ErrOrderReconciliationConflict) {
				c.JSON(http.StatusConflict, gin.H{"error": "reconciliation_case_revision_or_state_conflict"})
				return
			}
			reconciliationUnavailable(c)
			return
		}
	}
	if item.Status == storage.OrderReconciliationRunning {
		if item.OperationRequest == nil || item.OperationRequest.OperationID != item.ID || item.OperationEvidenceHash == "" || item.OperationEvidenceHash != evidenceHash {
			c.JSON(http.StatusConflict, gin.H{"error": "durable_reconciliation_evidence_changed_or_missing", "case": buildOrderReconciliationCaseDTO(c.Request.Context(), s, item)})
			return
		}
		manual := *item.OperationRequest
		if before.Cursor == evidence.Target && equalEconomicResult(before.Result, evidence.ExpectedResult) {
			receipt := alreadyAccountedReceipt(item, evidence, before)
			detail := mustJSON(map[string]string{"outcome": "already_accounted", "evidence_sha256": evidenceHash, "strategy_revision": before.Revision})
			ready, transitionErr := s.Repository.TransitionOrderReconciliation(c.Request.Context(), item.ID, item.Revision, storage.OrderReconciliationRunning, storage.OrderReconciliationReady, orderReconciliationActor(c), "already_accounted_readback", "verified", detail, &receipt, evidenceHash)
			if transitionErr != nil {
				reconciliationUnavailable(c)
				return
			}
			c.JSON(http.StatusOK, gin.H{"case": buildOrderReconciliationCaseDTO(c.Request.Context(), s, ready), "status": "ready_to_release", "release_requires_confirmation": true})
			return
		}
		if before.Cursor != manual.From || before.Revision != manual.ExpectedRevision {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "strategy_reconciliation_readback_unresolved", "case": buildOrderReconciliationCaseDTO(c.Request.Context(), s, item)})
			return
		}
		var accountingReceipt accounting.ReconciliationReceipt
		foundReceipt := false
		if reader, ok := s.Accounting.(OrderAccountingOperationReader); ok {
			accountingReceipt, foundReceipt, err = reader.ReadOrderAccountingReceipt(c.Request.Context(), manual.OperationID)
			if err != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": "strategy_operation_receipt_unavailable", "case": buildOrderReconciliationCaseDTO(c.Request.Context(), s, item)})
				return
			}
		}
		if !foundReceipt {
			// Retry is bound to the immutable durable request and original operation ID.
			accountingReceipt, err = s.Accounting.ReconcileOrderAccounting(c.Request.Context(), manual)
		}
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "strategy_reconciliation_result_unverified", "case": buildOrderReconciliationCaseDTO(c.Request.Context(), s, item)})
			return
		}
		if err = accountingReceipt.Validate(); err != nil || accountingReceipt.OperationID != item.ID || accountingReceipt.Owner != item.Owner || accountingReceipt.ExpectedRevision != item.ExpectedStrategyRevision || accountingReceipt.Target != evidence.Target {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "strategy_reconciliation_receipt_unverified", "case": buildOrderReconciliationCaseDTO(c.Request.Context(), s, item)})
			return
		}
		after, readErr := s.Accounting.ReadOrderAccounting(c.Request.Context(), item.Owner)
		if readErr != nil || after.Validate() != nil || after.Owner != item.Owner || after.Cursor != manual.Target || after.Revision != accountingReceipt.Revision || !equalEconomicResult(after.Result, evidence.ExpectedResult) || !equalEconomicResult(after.Result, accountingReceipt.Result) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "strategy_accounting_readback_unverified", "case": buildOrderReconciliationCaseDTO(c.Request.Context(), s, item)})
			return
		}
		receipt := orderReconciliationReceiptFromAccounting(accountingReceipt)
		detail := mustJSON(map[string]string{"outcome": "reconciled", "evidence_sha256": evidenceHash, "strategy_revision": receipt.Revision})
		ready, transitionErr := s.Repository.TransitionOrderReconciliation(c.Request.Context(), item.ID, item.Revision, storage.OrderReconciliationRunning, storage.OrderReconciliationReady, orderReconciliationActor(c), "reconcile_verified", "verified", detail, &receipt, evidenceHash)
		if transitionErr != nil {
			reconciliationUnavailable(c)
			return
		}
		c.JSON(http.StatusOK, gin.H{"case": buildOrderReconciliationCaseDTO(c.Request.Context(), s, ready), "status": "ready_to_release", "release_requires_confirmation": true})
		return
	}
	c.JSON(http.StatusConflict, gin.H{"error": "reconciliation_case_state_conflict"})
}

func releaseOrderReconciliationHandler(c *gin.Context) {
	if !requireRetiredEquityAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin_required"})
		return
	}
	s := currentOrderReconciliationService()
	if s == nil || s.Repository == nil {
		reconciliationUnavailable(c)
		return
	}
	var req releaseOrderReconciliationRequest
	if !decodeStrictJSON(c, &req) {
		return
	}
	if !req.Confirm || req.CaseRevision < 1 || len(req.EvidenceHash) != 64 {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "release_confirmation_and_evidence_required"})
		return
	}
	item, found, err := s.Repository.GetOrderReconciliation(c.Request.Context(), c.Param("case_id"))
	if err != nil {
		reconciliationUnavailable(c)
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "reconciliation_case_not_found"})
		return
	}
	if s.Gate == nil || s.Evidence == nil || s.Accounting == nil {
		reconciliationUnavailable(c)
		return
	}
	if (item.Status != storage.OrderReconciliationReady && item.Status != storage.OrderReconciliationReleasePending) || item.Revision != req.CaseRevision || item.Receipt == nil || item.ReleaseEvidenceHash != req.EvidenceHash {
		c.JSON(http.StatusConflict, gin.H{"error": "release_evidence_or_revision_conflict"})
		return
	}
	currentEvidence, evidenceErr := s.Evidence.ReadVerifiedOrderEvidence(c.Request.Context(), item)
	if evidenceErr != nil || !verifiedOrderEvidenceComplete(currentEvidence, item) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "trusted_order_evidence_unavailable", "case": buildOrderReconciliationCaseDTO(c.Request.Context(), s, item)})
		return
	}
	currentHash, hashErr := verifiedOrderEvidenceHash(item, currentEvidence)
	if hashErr != nil || currentHash != item.ReleaseEvidenceHash || currentEvidence.Target != item.Receipt.Target {
		c.JSON(http.StatusConflict, gin.H{"error": "release_evidence_changed", "case": buildOrderReconciliationCaseDTO(c.Request.Context(), s, item)})
		return
	}
	currentAccounting, accountingErr := s.Accounting.ReadOrderAccounting(c.Request.Context(), item.Owner)
	if accountingErr != nil || currentAccounting.Validate() != nil || currentAccounting.Owner != item.Owner || currentAccounting.Cursor != item.Receipt.Target || currentAccounting.Revision != item.Receipt.Revision || !equalEconomicResult(currentAccounting.Result, item.Receipt.Result) {
		c.JSON(http.StatusConflict, gin.H{"error": "strategy_accounting_changed_since_reconcile", "case": buildOrderReconciliationCaseDTO(c.Request.Context(), s, item)})
		return
	}
	// Persist explicit authorization before invoking the owner-specific bridge.
	requested := item
	if item.Status == storage.OrderReconciliationReady {
		requested, err = s.Repository.TransitionOrderReconciliation(c.Request.Context(), item.ID, item.Revision, storage.OrderReconciliationReady, storage.OrderReconciliationReleasePending, orderReconciliationActor(c), "release_confirmed", "pending", mustJSON(map[string]string{"evidence_hash": req.EvidenceHash}), nil, "")
		if err != nil {
			reconciliationUnavailable(c)
			return
		}
	}
	if err = s.Gate.ReleaseOrderQuarantine(c.Request.Context(), requested, req.EvidenceHash); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "owner_scoped_gate_release_unverified", "case": buildOrderReconciliationCaseDTO(c.Request.Context(), s, requested)})
		return
	}
	released, err := s.Repository.TransitionOrderReconciliation(c.Request.Context(), requested.ID, requested.Revision, storage.OrderReconciliationReleasePending, storage.OrderReconciliationReleased, orderReconciliationActor(c), "gate_release_verified", "released", mustJSON(map[string]string{"evidence_hash": req.EvidenceHash}), nil, "")
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "release_audit_readback_unavailable", "case": buildOrderReconciliationCaseDTO(c.Request.Context(), s, requested)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"case": buildOrderReconciliationCaseDTO(c.Request.Context(), s, released), "status": "released"})
}

func decodeStrictJSON(c *gin.Context, dst any) bool {
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "invalid_request"})
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "invalid_request"})
		return false
	}
	return true
}
func orderReconciliationActor(c *gin.Context) string {
	v, _ := c.Get("session")
	session, ok := v.(*Session)
	if !ok {
		return ""
	}
	return session.Username
}
func newOrderReconciliationID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
func reconciliationUnavailable(c *gin.Context) {
	c.JSON(http.StatusServiceUnavailable, gin.H{"error": "order_reconciliation_unavailable"})
}
func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte(`{}`)
	}
	return b
}
func verifiedOrderEvidenceComplete(e VerifiedOrderEvidence, item storage.OrderReconciliationCase) bool {
	if item.ExpectedIntentRevision == "" || item.ExpectedStrategyRevision == "" || e.Owner != item.Owner || !e.OwnerMatched || e.IntentRevision != item.ExpectedIntentRevision || !e.VenueTerminal || !e.FillsComplete || !e.FeesComplete {
		return false
	}
	full := accounting.ReconcileRequest{OperationID: item.ID, Owner: item.Owner, ExpectedRevision: item.ExpectedStrategyRevision, From: accounting.Cursor{}, Target: e.Target, Fills: e.Fills, Fees: e.Fees, Evidence: e.Summary}
	if full.Validate() != nil {
		return false
	}
	return (accounting.AccountingSnapshot{Owner: item.Owner, Revision: item.ExpectedStrategyRevision, Cursor: e.Target, Result: e.ExpectedResult, ReadAt: e.Summary.VerifiedAt}).Validate() == nil
}

func verifiedHistorySuffix(e VerifiedOrderEvidence, item storage.OrderReconciliationCase, before accounting.Cursor) ([]accounting.VerifiedFill, []accounting.VerifiedFee, bool) {
	if !verifiedOrderEvidenceComplete(e, item) || before.Validate() != nil || before.Sequence > e.Target.Sequence || before.Sequence > uint64(len(e.Fills)) {
		return nil, nil, false
	}
	if before.Sequence > 0 && e.Fills[before.Sequence-1].TradeID != before.TradeID {
		return nil, nil, false
	}
	if before.Sequence == e.Target.Sequence && before.TradeID != e.Target.TradeID {
		return nil, nil, false
	}
	start := int(before.Sequence)
	fills := append([]accounting.VerifiedFill(nil), e.Fills[start:]...)
	tradeIDs := make(map[string]struct{}, len(fills))
	for _, fill := range fills {
		tradeIDs[fill.TradeID] = struct{}{}
	}
	fees := make([]accounting.VerifiedFee, 0, len(e.Fees))
	for _, fee := range e.Fees {
		if _, isSuffix := tradeIDs[fee.TradeID]; isSuffix {
			fees = append(fees, fee)
		}
	}
	return fills, fees, true
}

func alreadyAccountedReceipt(item storage.OrderReconciliationCase, evidence VerifiedOrderEvidence, snapshot accounting.AccountingSnapshot) storage.OrderReconciliationReceipt {
	return storage.OrderReconciliationReceipt{Outcome: "already_accounted", OperationID: item.ID, Owner: item.Owner, ExpectedRevision: item.ExpectedStrategyRevision, From: evidence.Target, Target: evidence.Target, Fills: evidence.Fills, Fees: evidence.Fees, Evidence: evidence.Summary, Revision: snapshot.Revision, Result: snapshot.Result, CompletedAt: time.Now().UTC()}
}

func orderReconciliationReceiptFromAccounting(receipt accounting.ReconciliationReceipt) storage.OrderReconciliationReceipt {
	return storage.OrderReconciliationReceipt{Outcome: "reconciled", OperationID: receipt.OperationID, Owner: receipt.Owner, ExpectedRevision: receipt.ExpectedRevision, From: receipt.From, Target: receipt.Target, Fills: receipt.Fills, Fees: receipt.Fees, Evidence: receipt.Evidence, Revision: receipt.Revision, Result: receipt.Result, CompletedAt: receipt.CompletedAt}
}

func equalEconomicResult(a, b accounting.EconomicResult) bool {
	if a.NetQuantity != b.NetQuantity || len(a.RealizedPnL) != len(b.RealizedPnL) {
		return false
	}
	amounts := make(map[string]string, len(a.RealizedPnL))
	for _, item := range a.RealizedPnL {
		if _, exists := amounts[item.Asset]; exists {
			return false
		}
		amounts[item.Asset] = item.Amount
	}
	for _, item := range b.RealizedPnL {
		if amount, exists := amounts[item.Asset]; !exists || amount != item.Amount {
			return false
		}
		delete(amounts, item.Asset)
	}
	return len(amounts) == 0
}

func verifiedOrderEvidenceHash(item storage.OrderReconciliationCase, e VerifiedOrderEvidence) (string, error) {
	fills := append([]accounting.VerifiedFill(nil), e.Fills...)
	sort.Slice(fills, func(i, j int) bool { return fills[i].CursorSequence < fills[j].CursorSequence })
	fees := append([]accounting.VerifiedFee(nil), e.Fees...)
	sort.Slice(fees, func(i, j int) bool { return fees[i].FeeID < fees[j].FeeID })
	payload := struct {
		Owner                       accounting.OrderOwner     `json:"owner"`
		IntentRevision              string                    `json:"intent_revision"`
		ExpectedStrategyRevision    string                    `json:"expected_strategy_revision"`
		EvidenceRefs                []string                  `json:"evidence_refs"`
		Target                      accounting.Cursor         `json:"target"`
		Fills                       []accounting.VerifiedFill `json:"fills"`
		Fees                        []accounting.VerifiedFee  `json:"fees"`
		ExpectedResult              accounting.EconomicResult `json:"expected_result"`
		Source, Summary, VerifiedBy string
	}{item.Owner, e.IntentRevision, item.ExpectedStrategyRevision, item.EvidenceRefs, e.Target, fills, fees, e.ExpectedResult, e.Summary.Source, e.Summary.Summary, e.Summary.VerifiedBy}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(b)
	return hex.EncodeToString(digest[:]), nil
}

func digestOrderEvidence(item storage.OrderReconciliationCase, e VerifiedOrderEvidence) string {
	hash, err := verifiedOrderEvidenceHash(item, e)
	if err != nil {
		return ""
	}
	return hash
}

func buildOrderReconciliationCaseDTO(ctx context.Context, s *OrderReconciliationService, item storage.OrderReconciliationCase) OrderReconciliationCaseDTO {
	dto := OrderReconciliationCaseDTO{ID: item.ID, IdempotencyKey: item.IdempotencyKey, Owner: item.Owner, ExpectedIntentRevision: item.ExpectedIntentRevision, ExpectedStrategyRevision: item.ExpectedStrategyRevision, Reason: item.Reason, EvidenceRefs: item.EvidenceRefs, Status: item.Status, Revision: item.Revision, ReleaseEvidenceHash: item.ReleaseEvidenceHash, CreatedBy: item.CreatedBy, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
	if item.Receipt != nil {
		dto.Receipt = &orderReconciliationReceiptDTO{Outcome: item.Receipt.Outcome, OperationID: item.Receipt.OperationID, Owner: item.Receipt.Owner, ExpectedRevision: item.Receipt.ExpectedRevision, From: item.Receipt.From, Target: item.Receipt.Target, Revision: item.Receipt.Revision, Result: item.Receipt.Result, CompletedAt: item.Receipt.CompletedAt}
	}
	validation := OrderReconciliationValidation{Blockers: make([]string, 0)}
	validation.GateAvailable = s != nil && s.Gate != nil
	if !validation.GateAvailable {
		validation.Blockers = append(validation.Blockers, "owner_scoped_gate_bridge_unavailable")
	}
	var currentEvidence *VerifiedOrderEvidence
	if s == nil || s.Evidence == nil {
		validation.Blockers = append(validation.Blockers, "trusted_order_evidence_unavailable")
	} else if evidence, err := s.Evidence.ReadVerifiedOrderEvidence(ctx, item); err != nil {
		validation.Blockers = append(validation.Blockers, "trusted_order_evidence_unavailable")
	} else {
		currentEvidence = &evidence
		validation.OwnerMatched = evidence.Owner == item.Owner && evidence.OwnerMatched
		validation.IntentRevisionMatched = item.ExpectedIntentRevision != "" && evidence.IntentRevision == item.ExpectedIntentRevision
		validation.VenueTerminal = evidence.VenueTerminal
		validation.FillsComplete = evidence.FillsComplete
		validation.FeesComplete = evidence.FeesComplete
		if !validation.OwnerMatched {
			validation.Blockers = append(validation.Blockers, "owner_match_unverified")
		}
		if !validation.IntentRevisionMatched {
			validation.Blockers = append(validation.Blockers, "intent_revision_unverified")
		}
		if !validation.VenueTerminal {
			validation.Blockers = append(validation.Blockers, "venue_order_not_terminal")
		}
		if !validation.FillsComplete {
			validation.Blockers = append(validation.Blockers, "fills_incomplete")
		}
		if !validation.FeesComplete {
			validation.Blockers = append(validation.Blockers, "fees_incomplete")
		}
		if item.Receipt != nil && item.ReleaseEvidenceHash != "" {
			if digest, err := verifiedOrderEvidenceHash(item, evidence); err == nil && digest == item.ReleaseEvidenceHash {
				validation.EvidenceMatchesCase = true
			} else {
				validation.Blockers = append(validation.Blockers, "verified_evidence_changed_since_reconcile")
			}
		} else if item.Receipt == nil {
			validation.EvidenceMatchesCase = verifiedOrderEvidenceComplete(evidence, item)
			if validation.EvidenceMatchesCase && item.Status == storage.OrderReconciliationRunning {
				validation.EvidenceMatchesCase = item.OperationEvidenceHash != "" && item.OperationEvidenceHash == digestOrderEvidence(item, evidence)
			}
		}
	}
	if s == nil || s.Accounting == nil {
		validation.Blockers = append(validation.Blockers, "strategy_accounting_unavailable")
	} else if snapshot, err := s.Accounting.ReadOrderAccounting(ctx, item.Owner); err != nil || snapshot.Validate() != nil || snapshot.Owner != item.Owner {
		validation.Blockers = append(validation.Blockers, "strategy_accounting_readback_unavailable")
	} else {
		dto.StrategyCursor = &snapshot
		validation.StrategyReadback = true
		if item.Receipt != nil {
			validation.StrategyReadback = snapshot.Cursor == item.Receipt.Target && snapshot.Revision == item.Receipt.Revision && equalEconomicResult(snapshot.Result, item.Receipt.Result)
		} else {
			validation.StrategyReadback = item.ExpectedStrategyRevision != "" && snapshot.Revision == item.ExpectedStrategyRevision
			if currentEvidence != nil {
				if item.Status == storage.OrderReconciliationRunning && snapshot.Cursor == currentEvidence.Target {
					validation.StrategyReadback = item.OperationEvidenceHash != "" && item.OperationEvidenceHash == digestOrderEvidence(item, *currentEvidence) && equalEconomicResult(snapshot.Result, currentEvidence.ExpectedResult)
				} else {
					_, _, prefixOK := verifiedHistorySuffix(*currentEvidence, item, snapshot.Cursor)
					validation.StrategyReadback = validation.StrategyReadback && prefixOK
				}
			}
		}
		if !validation.StrategyReadback {
			validation.Blockers = append(validation.Blockers, "strategy_accounting_cursor_or_revision_mismatch")
		}
	}
	validation.ReleaseRetryable = item.Status == storage.OrderReconciliationReleasePending && item.Receipt != nil && validation.GateAvailable && validation.StrategyReadback && validation.OwnerMatched && validation.IntentRevisionMatched && validation.EvidenceMatchesCase
	validation.ReleaseReady = item.Status == storage.OrderReconciliationReady && item.Receipt != nil && validation.GateAvailable && dto.StrategyCursor != nil && validation.StrategyReadback && validation.OwnerMatched && validation.IntentRevisionMatched && validation.VenueTerminal && validation.FillsComplete && validation.FeesComplete && validation.EvidenceMatchesCase
	if item.Status != storage.OrderReconciliationReady && item.Status != storage.OrderReconciliationReleasePending && item.Status != storage.OrderReconciliationReleased {
		validation.Blockers = append(validation.Blockers, "reconciliation_not_ready")
	}
	dto.Validation = validation
	return dto
}
