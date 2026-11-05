# FundingCarry 管理员解锁续办验收

结果：通过
时间：2026-11-05T10:24:38Z
基线：7de606656a3a93eee9779b92015c777e5cb9e0f2
版本：3.111.0-rc1196 / 3.111.0-rc1196
源码摘要：2df92ba58e99909d0dd305111ccf470f4794108d59403b166895a7e3d229aa0f
测试期间源码稳定：true
MySQL 专项完成：true

Isolated SQLite/MySQL, production router and retained runtime. No production account, deployment, live exchange, or profitability acceptance.

## 用例

| 包 | 用例 | 结果 |
|---|---|---|
| quantmesh/storage | TestMySQLFundingRecoveryReleaseResumeAtomicAndReplay/released=false | pass |
| quantmesh/storage | TestMySQLFundingRecoveryReleaseResumeAtomicAndReplay/released=true | pass |
| quantmesh/storage | TestMySQLFundingRecoveryReleaseResumeAtomicAndReplay | pass |
| quantmesh/storage | TestMySQLFundingRecoveryReleaseResumeConcurrentPools/sameOperation=true | pass |
| quantmesh/storage | TestMySQLFundingRecoveryReleaseResumeConcurrentPools/sameOperation=false | pass |
| quantmesh/storage | TestMySQLFundingRecoveryReleaseResumeConcurrentPools | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeReopenPreservesOwnerHistory | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeAtomicAndReplay/released=false | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeAtomicAndReplay/released=true | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeAtomicAndReplay | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeAuthorizationBoundaries/confirmation | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeAuthorizationBoundaries/actor | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeAuthorizationBoundaries/audit | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeAuthorizationBoundaries/same_operation | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeAuthorizationBoundaries/bad_release_hash | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeAuthorizationBoundaries/wrong_release_hash | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeAuthorizationBoundaries/bad_evidence | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeAuthorizationBoundaries/stale_evidence | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeAuthorizationBoundaries/old_owner | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeAuthorizationBoundaries/unbound_new_owner | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeAuthorizationBoundaries/same_owner | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeAuthorizationBoundaries/episode_CAS | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeAuthorizationBoundaries/hold_CAS | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeAuthorizationBoundaries/recovery_generation | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeAuthorizationBoundaries/episode_overflow | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeAuthorizationBoundaries/hold_overflow | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeAuthorizationBoundaries/empty_scopes | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeAuthorizationBoundaries/other_scope | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeAuthorizationBoundaries/duplicate_scopes | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeAuthorizationBoundaries | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries/afterCommit=false/episode_scalar | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries/afterCommit=false/hold_scalar | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries/afterCommit=false/scope_payload | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries/afterCommit=false/hold_scope | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries/afterCommit=false/release_scalar_hash | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries/afterCommit=false/release_phase | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries/afterCommit=false/release_actor | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries/afterCommit=false/release_evidence | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries/afterCommit=false/release_exposure | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries/afterCommit=false/release_owner | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries/afterCommit=false/release_projection | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries/afterCommit=false/event | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries/afterCommit=false/cursor | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries/afterCommit=false/pending_outbox_scalar | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries/afterCommit=false/outbox_payload | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries/afterCommit=false/unverified_anomaly | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries/afterCommit=false/unknown_child | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries/afterCommit=false/unknown_attempt | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries/afterCommit=false/authorized_permit | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries/afterCommit=false/reopened | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries/afterCommit=true/episode_scalar | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries/afterCommit=true/hold_scalar | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries/afterCommit=true/scope_payload | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries/afterCommit=true/hold_scope | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries/afterCommit=true/release_scalar_hash | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries/afterCommit=true/release_phase | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries/afterCommit=true/release_actor | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries/afterCommit=true/release_evidence | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries/afterCommit=true/release_exposure | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries/afterCommit=true/release_owner | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries/afterCommit=true/release_projection | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries/afterCommit=true/event | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries/afterCommit=true/cursor | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries/afterCommit=true/pending_outbox_scalar | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries/afterCommit=true/outbox_payload | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries/afterCommit=true/unverified_anomaly | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries/afterCommit=true/unknown_child | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries/afterCommit=true/unknown_attempt | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries/afterCommit=true/authorized_permit | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries/afterCommit=true/reopened | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeConcurrentPools/sameOperation=true | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeConcurrentPools/sameOperation=false | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeConcurrentPools | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeLateCASRollsBack | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeAuditOnlyEvidenceChangeRejectsReplay | pass |
| quantmesh/storage | TestSQLiteFundingRecoveryReleaseResumeHistoricalReceiptTampering | pass |
| quantmesh/storage | TestFundingRecoveryReleaseResumePreviewJSONContract | pass |
| quantmesh/strategy | TestFundingRecoveryReleaseResumeKeepsHistoricalFillOwners | pass |
| quantmesh/strategy | TestFundingCarryReleasePendingStartupRetainsOnlyStoppedRuntime/current_owner | pass |
| quantmesh/strategy | TestFundingCarryReleasePendingStartupRetainsOnlyStoppedRuntime/new_runtime_generation | pass |
| quantmesh/strategy | TestFundingCarryReleasePendingStartupRetainsOnlyStoppedRuntime | pass |
| quantmesh/strategy | TestFundingCarryReleasePendingStartupVerifiesPriorTakeoverProjectionReceipt/tampered_takeover_receipt_false | pass |
| quantmesh/strategy | TestFundingCarryReleasePendingStartupVerifiesPriorTakeoverProjectionReceipt/tampered_takeover_receipt_true | pass |
| quantmesh/strategy | TestFundingCarryReleasePendingStartupVerifiesPriorTakeoverProjectionReceipt | pass |
| quantmesh/strategy | TestFundingCarryReleasePendingStartupRejectsIncompleteEvidence/released_episode | pass |
| quantmesh/strategy | TestFundingCarryReleasePendingStartupRejectsIncompleteEvidence/released_hold | pass |
| quantmesh/strategy | TestFundingCarryReleasePendingStartupRejectsIncompleteEvidence/zero_hold_revision | pass |
| quantmesh/strategy | TestFundingCarryReleasePendingStartupRejectsIncompleteEvidence/released_by_operation | pass |
| quantmesh/strategy | TestFundingCarryReleasePendingStartupRejectsIncompleteEvidence/wrong_hold_source | pass |
| quantmesh/strategy | TestFundingCarryReleasePendingStartupRejectsIncompleteEvidence/wrong_hold_generation | pass |
| quantmesh/strategy | TestFundingCarryReleasePendingStartupRejectsIncompleteEvidence/wrong_hold_scope | pass |
| quantmesh/strategy | TestFundingCarryReleasePendingStartupRejectsIncompleteEvidence/missing_projection | pass |
| quantmesh/strategy | TestFundingCarryReleasePendingStartupRejectsIncompleteEvidence/pending_projection_current_owner | pass |
| quantmesh/strategy | TestFundingCarryReleasePendingStartupRejectsIncompleteEvidence/pending_projection_old_owner | pass |
| quantmesh/strategy | TestFundingCarryReleasePendingStartupRejectsIncompleteEvidence/projection_hash_drift | pass |
| quantmesh/strategy | TestFundingCarryReleasePendingStartupRejectsIncompleteEvidence/missing_receipt | pass |
| quantmesh/strategy | TestFundingCarryReleasePendingStartupRejectsIncompleteEvidence/receipt_read_error | pass |
| quantmesh/strategy | TestFundingCarryReleasePendingStartupRejectsIncompleteEvidence/receipt_hash_drift | pass |
| quantmesh/strategy | TestFundingCarryReleasePendingStartupRejectsIncompleteEvidence/receipt_owner_drift | pass |
| quantmesh/strategy | TestFundingCarryReleasePendingStartupRejectsIncompleteEvidence/receipt_revision_drift | pass |
| quantmesh/strategy | TestFundingCarryReleasePendingStartupRejectsIncompleteEvidence/receipt_generation_drift | pass |
| quantmesh/strategy | TestFundingCarryReleasePendingStartupRejectsIncompleteEvidence/receipt_still_pending | pass |
| quantmesh/strategy | TestFundingCarryReleasePendingStartupRejectsIncompleteEvidence | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumeRequiresSessionAdmin/preview/unauthenticated | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumeRequiresSessionAdmin/preview/non_admin | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumeRequiresSessionAdmin/apply/unauthenticated | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumeRequiresSessionAdmin/apply/non_admin | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumeRequiresSessionAdmin | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumeRejectsInvalidJSON/client_actor | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumeRejectsInvalidJSON/unknown_field | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumeRejectsInvalidJSON/trailing_object | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumeRejectsInvalidJSON/trailing_null | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumeRejectsInvalidJSON/trailing_garbage | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumeRejectsInvalidJSON/oversized_body | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumeRejectsInvalidJSON/oversized_trailing_whitespace | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumeRejectsInvalidJSON | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumeRequiresCompleteAuthorization/missing_confirmed | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumeRequiresCompleteAuthorization/missing_operation_id | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumeRequiresCompleteAuthorization/missing_release_operation_id | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumeRequiresCompleteAuthorization/missing_release_request_hash | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumeRequiresCompleteAuthorization/missing_audit_ref | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumeRequiresCompleteAuthorization/missing_evidence_hash | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumeRequiresCompleteAuthorization/missing_expected_owner_generation | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumeRequiresCompleteAuthorization/missing_owner_generation | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumeRequiresCompleteAuthorization/missing_account_scopes | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumeRequiresCompleteAuthorization/missing_expected_episode_revision | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumeRequiresCompleteAuthorization/missing_expected_hold_revision | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumeRequiresCompleteAuthorization/missing_recovery_generation | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumeRequiresCompleteAuthorization/not_confirmed | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumeRequiresCompleteAuthorization/invalid_release_hash | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumeRequiresCompleteAuthorization/invalid_evidence_hash | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumeRequiresCompleteAuthorization/empty_scope | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumeRequiresCompleteAuthorization/duplicate_scope | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumeRequiresCompleteAuthorization | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumePreviewBindsRequestAndFlattensEvidence | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumeBindsAuthorizationAndReturnsBothReceipts/verified_true | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumeBindsAuthorizationAndReturnsBothReceipts/verified_false | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumeBindsAuthorizationAndReturnsBothReceipts | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumePreservesDurableErrorState/preview/hold_already_released | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumePreservesDurableErrorState/preview/gate_cleanup_failed | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumePreservesDurableErrorState/preview/conflict | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumePreservesDurableErrorState/preview/operation_conflict | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumePreservesDurableErrorState/preview/not_found | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumePreservesDurableErrorState/apply/hold_already_released | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumePreservesDurableErrorState/apply/gate_cleanup_failed | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumePreservesDurableErrorState/apply/conflict | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumePreservesDurableErrorState/apply/operation_conflict | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumePreservesDurableErrorState/apply/not_found | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumePreservesDurableErrorState | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumeUnavailableWithoutCallback/no_service/preview | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumeUnavailableWithoutCallback/no_service/apply | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumeUnavailableWithoutCallback/no_callback/preview | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumeUnavailableWithoutCallback/no_callback/apply | pass |
| quantmesh/web | TestFundingRecoveryReleaseResumeUnavailableWithoutCallback | pass |
| quantmesh/internal/app | TestFundingCarryFullConstructorRetainsReleasePendingAfterRestart | pass |
| quantmesh/internal/app | TestProductionFundingRecoveryRouteTakeoverAndFoundUsesRetainedRuntime | pass |
| quantmesh/internal/app | TestProductionFundingRecoveryReleaseInterruptedAcrossInstances/evidence | pass |
| quantmesh/internal/app | TestProductionFundingRecoveryReleaseInterruptedAcrossInstances/release_receipt | pass |
| quantmesh/internal/app | TestProductionFundingRecoveryReleaseInterruptedAcrossInstances/old_owner | pass |
| quantmesh/internal/app | TestProductionFundingRecoveryReleaseInterruptedAcrossInstances/scope | pass |
| quantmesh/internal/app | TestProductionFundingRecoveryReleaseInterruptedAcrossInstances/revision | pass |
| quantmesh/internal/app | TestProductionFundingRecoveryReleaseInterruptedAcrossInstances | pass |

缺失的必选用例：

## 执行诊断

```text

```
