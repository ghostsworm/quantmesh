package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"quantmesh/accounting"
	"quantmesh/storage"
)

const fundingRecoveryReleaseResumeMaxBodyBytes = 16 << 10

// FundingRecoveryReleaseResumePreview 保留儲存層證據，並補上目前的 Gate 來源。
type FundingRecoveryReleaseResumePreview struct {
	storage.FundingRecoveryReleaseResumePreview
	GateSources        []string `json:"gate_sources"`
	RecoveryGateSource string   `json:"recovery_gate_source"`
}

// FundingRecoveryReleaseResumeReadback 的 Receipt 是續辦收據；ReleaseReceipt 是原始釋放收據。
type FundingRecoveryReleaseResumeReadback struct {
	FundingRecoveryReleaseReadback
	ReleaseReceipt accounting.RecoveryReceipt `json:"release_receipt"`
	Replayed       bool                       `json:"replayed"`
}

type fundingRecoveryReleaseResumeRequest struct {
	OperationID             string   `json:"operation_id"`
	ReleaseOperationID      string   `json:"release_operation_id"`
	ReleaseRequestHash      string   `json:"release_request_hash"`
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

func getFundingRecoveryReleaseResumePreviewHandler(c *gin.Context) {
	if !requireRetiredEquityAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin_required"})
		return
	}
	s := currentOrderReconciliationService()
	if s == nil || s.RecoveryReleaseResumePreview == nil {
		reconciliationUnavailable(c)
		return
	}
	episodeID, releaseOperationID := c.Param("episode_id"), c.Query("release_operation_id")
	if strings.TrimSpace(episodeID) == "" || strings.TrimSpace(releaseOperationID) == "" {
		writeFundingRecoveryReleaseError(c, errors.New("incomplete recovery release resume preview identity"))
		return
	}
	preview, err := s.RecoveryReleaseResumePreview(c.Request.Context(), episodeID, releaseOperationID)
	if err != nil {
		writeFundingRecoveryReleaseError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"preview": preview, "execution_replayed": false})
}

func applyFundingRecoveryReleaseResumeHandler(c *gin.Context) {
	if !requireRetiredEquityAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin_required"})
		return
	}
	s := currentOrderReconciliationService()
	if s == nil || s.RecoveryReleaseResumeApply == nil {
		reconciliationUnavailable(c)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, fundingRecoveryReleaseResumeMaxBodyBytes)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var input fundingRecoveryReleaseResumeRequest
	if err := decoder.Decode(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_funding_recovery_release_resume"})
		return
	}
	var trailing any
	if !errors.Is(decoder.Decode(&trailing), io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_funding_recovery_release_resume"})
		return
	}
	actor := orderReconciliationActor(c)
	if strings.TrimSpace(actor) == "" {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin_required"})
		return
	}
	// 身分只取自已驗證的管理員 session；客戶端僅提交預覽綁定的授權條件。
	auth := accounting.RecoveryReleaseResumeAuthorization{
		EpisodeID: c.Param("episode_id"), OperationID: input.OperationID,
		ReleaseOperationID: input.ReleaseOperationID, ReleaseRequestHash: input.ReleaseRequestHash,
		Actor: actor, AuditRef: input.AuditRef, EvidenceHash: input.EvidenceHash,
		ExpectedOwnerGeneration: input.ExpectedOwnerGeneration, OwnerGeneration: input.OwnerGeneration,
		AccountScopes: input.AccountScopes, ExpectedEpisodeRevision: input.ExpectedEpisodeRevision,
		ExpectedHoldRevision: input.ExpectedHoldRevision, RecoveryGeneration: input.RecoveryGeneration,
		Confirmed: input.Confirmed,
	}
	if err := auth.Validate(); err != nil {
		writeFundingRecoveryReleaseError(c, err)
		return
	}
	readback, err := s.RecoveryReleaseResumeApply(c.Request.Context(), auth)
	if err != nil {
		writeFundingRecoveryReleaseError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"readback": readback, "hold_released": readback.Verified, "execution_replayed": false})
}
