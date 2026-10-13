# RC1187 任務清單

## 2026-10-13 擴圍狀態

| ID | 成員 | 狀態 | 鎖定範圍 | 完成標準 | 驗證責任 |
|---|---|---|---|---|---|
| T01 | 後端-完整哈希調用鏈修復 | 實作完成，待終審 | `storage/funding_recovery.go`、FundingCarry durable projection/replay/takeover callers | 共用 semantic/legacy verifier 覆蓋投影與 durable replay；receipt/outbox CAS 前驗證；target+2 takeover 證據完整；新 outbox runtime revision 位於 hashed projection 並與外層值相等 | QA 定向/整合測試通過；T03 最終唯讀複核 |
| T02 | QA-恢復投影邊界驗收 | 測試完成，待全套門禁 | storage/strategy integrity tests | receipt-only/runtime-revision mismatch 無副作用；target+2 takeover/refusal；unknown-field matching-hash；MySQL legacy fixture；pending/completed replay tamper fail-closed | 定向 storage/strategy 測試通過；需最終全套與 MySQL CI |
| T03 | 評審-恢復哈希邊界終審 | 第二輪複核中 | 唯讀 | 關閉 P1/P2，另審 ExpectedRuntimeRevision hash binding 與 legacy payload 相容邊界 | Lead 最終接受/退回 |
| T04 | Goal Lead | 進行中 | 版本、變更記錄、整合、delivery | 本地門禁通過後提交並推送；同 SHA 遠端 MySQL/完整 CI 綠後才 tag/release；保留既有未跟蹤項目 | 本地全套 + 遠端 CI evidence |

目前狀態：完整調用鏈與新增 CAS revision 綁定已實作；使用者已選擇舊格式缺欄位時拒絕自動投影/重放並轉人工核帳。storage/strategy 定向套件和相關 producer-route 測試通過，`go vet ./...`、`git diff --check` 通過。全倉 Go 與前端 build 必須在最終差異上完整重跑；之前的全倉測試一次因沙箱 loopback 權限失敗、一次被中斷，前端測試 318 項通過但 build 被中斷，均不計完整門禁通過。MySQL 等待同 SHA CI；T03 需複核 fail-closed legacy 分支。未提交、未推送、未發布、未打 tag。

下方舊表為首次確認時的窄範圍歷史快照，已被本節擴圍狀態及 `plan.md` 的 reviewer follow-up 修訂取代，不作為目前任務邊界。

| ID | 成員 | 狀態 | 鎖定範圍 | 完成標準 | 驗證責任 | 文檔更新 |
|---|---|---|---|---|---|---|
| T01 | 後端-MySQL哈希兼容修復 | 實作完成，待終審 | `storage/funding_recovery.go` | 完成格式正規化相容與舊 pending 恢復，保留完整 tamper detection；不改 schema | T02 整合測試、T03 獨立 review | 已完成首輪實作 |
| T02 | QA-MySQL Outbox投影驗收 | 已領取/測試設計中 | storage `_test.go` | MySQL 8 強制 DSN seed/project/old pending/competing anomaly/tamper 路徑覆蓋 | Lead 重跑本地和 CI；T03 review 測試有效性 | 更新 test-plan/progress |
| T03 | 評審-恢復哈希邊界 | 已領取/獨立分析中 | 唯讀 | 審查新舊格式 hash binding、owner/CAS/hold、不變量與 Go/MySQL 行為 | Lead 最終接受/退回 | 提供 findings capsule |
| T04 | Goal Lead | 進行中 | 版本/Changelog/產品概覽/最終整合 | 只在確認同 SHA MySQL/完整 CI 成功後打 tag/release | 全量 Go/vet/frontend/local + GitHub CI | 更新 RC1187 文檔狀態 |
