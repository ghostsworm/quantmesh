# RC1187 任務清單

## 2026-10-14 RC1190 SQLite 迁移连接并发修复

- T01 后端：已在 SQLite `db.Conn(ctx)` 获取阶段，仅对 SQLite BUSY/LOCKED 做 30 秒上限、尊重调用方 context 的重试；MySQL 获取路径和事务/schema 路径未扩展。定向/全包测试通过。
- T02 QA：已对连接获取注入 BUSY、非锁错误、MySQL 单次失败做确定性断言；12 个独立连接当前/旧 schema 并发迁移重复 100 轮通过，包含取消测试。
- T03 Reviewer：两轮独立只读复核，前轮发现的错误链测试过度断言与产品概览旧版本均已修正；终审未发现新缺陷。
- T04 Lead：RC1190 版本及 changelog/product docs 已更新；全仓 Go、race、vet、build、WebUI verify 全绿；已按既有授权本地提交，待推送。PR #9 同 SHA MySQL/完整 CI 全绿前不打 tag、不发布。

## 2026-10-13 RC1189 MySQL 哈希测试夹具回归

- T02：已补 `ExpectedRuntimeRevision` 到历史哈希 helper，并新增无需 MySQL 的单测断言版本值变化会改变哈希；定向单测通过。MySQL 集成仍因本机无 MySQL 8.0 端点而跳过，完整本地/远端验收待完成。
- T04：RC1189 版本与变更文档已更新；待最终门禁后按既有授权提交并推送，PR #9 新 SHA 的同提交 MySQL/完整 CI 全绿前不打 tag、不发布。

## 2026-10-13 擴圍狀態

| ID | 成員 | 狀態 | 鎖定範圍 | 完成標準 | 驗證責任 |
|---|---|---|---|---|---|
| T01 | 後端-完整哈希調用鏈修復 | 實作完成，待終審 | `storage/funding_recovery.go`、FundingCarry durable projection/replay/takeover callers | 共用 semantic/legacy verifier 覆蓋投影與 durable replay；receipt/outbox CAS 前驗證；target+2 takeover 證據完整；新 outbox runtime revision 位於 hashed projection 並與外層值相等 | QA 定向/整合測試通過；T03 最終唯讀複核 |
| T02 | QA-恢復投影邊界驗收 | 定向測試完成；MySQL CI 暴露 fixture CAST 錯誤，RC1188 修復後待重驗 | storage/strategy integrity tests | receipt-only/runtime-revision mismatch 無副作用；target+2 takeover/refusal；unknown-field matching-hash；MySQL legacy fixture；pending/completed replay tamper fail-closed | 定向 storage/strategy 測試通過；RC1187 同 SHA MySQL 8.0.36 實際失敗，須由 RC1188 新 SHA 全套 CI 重驗 |
| T03 | 評審-恢復哈希邊界終審 | 第二輪複核中 | 唯讀 | 關閉 P1/P2，另審 ExpectedRuntimeRevision hash binding 與 legacy payload 相容邊界 | Lead 最終接受/退回 |
| T04 | Goal Lead | 進行中 | 版本、變更記錄、整合、delivery | 本地門禁通過後提交並推送；同 SHA 遠端 MySQL/完整 CI 綠後才 tag/release；保留既有未跟蹤項目 | 本地全套 + 遠端 CI evidence |

目前狀態：完整調用鏈與新增 CAS revision 綁定已實作；使用者已選擇舊格式缺欄位時拒絕自動投影/重放並轉人工核帳。PR #9 的 RC1187 SHA `997ea94fcb460fd0e9190656d48057feb47219b7` 已實際啟動 MySQL 8.0.36；前端測試通過，但 Go 測試在本文件所列兩個 fixture 的 `CAST(? AS JSON)` 哈希參數失敗，靜態分析/race/build 隨後跳過。RC1188 僅把 hash JSON_SET value 改為普通字串參數，保留 projection JSON CAST 和所有生產 fail-closed 規則；需以新 SHA 執行本地全套（本地 MySQL 如不可用須記為 skipped）及 PR MySQL/完整 CI。未驗收前不打 tag、不發布。

下方舊表為首次確認時的窄範圍歷史快照，已被本節擴圍狀態及 `plan.md` 的 reviewer follow-up 修訂取代，不作為目前任務邊界。

| ID | 成員 | 狀態 | 鎖定範圍 | 完成標準 | 驗證責任 | 文檔更新 |
|---|---|---|---|---|---|---|
| T01 | 後端-MySQL哈希兼容修復 | 實作完成，待終審 | `storage/funding_recovery.go` | 完成格式正規化相容與舊 pending 恢復，保留完整 tamper detection；不改 schema | T02 整合測試、T03 獨立 review | 已完成首輪實作 |
| T02 | QA-MySQL Outbox投影驗收 | 已領取/測試設計中 | storage `_test.go` | MySQL 8 強制 DSN seed/project/old pending/competing anomaly/tamper 路徑覆蓋 | Lead 重跑本地和 CI；T03 review 測試有效性 | 更新 test-plan/progress |
| T03 | 評審-恢復哈希邊界 | 已領取/獨立分析中 | 唯讀 | 審查新舊格式 hash binding、owner/CAS/hold、不變量與 Go/MySQL 行為 | Lead 最終接受/退回 | 提供 findings capsule |
| T04 | Goal Lead | 進行中 | 版本/Changelog/產品概覽/最終整合 | 只在確認同 SHA MySQL/完整 CI 成功後打 tag/release | 全量 Go/vet/frontend/local + GitHub CI | 更新 RC1187 文檔狀態 |
