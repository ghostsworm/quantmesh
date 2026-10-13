# RC1187 團隊計畫

## 目標與背景

RC1186 提交 `d39ffbcb4f8b4fd4d2edae64e22f5758b529027f` 已推送至 `main`。同 SHA GitHub CI run `38347532032` 的 `go test ./...` 在 `TestMySQLFundingRecoveryAnomalyDifferentAdjustmentsHaveSingleWinner` 失敗，訊息為 `project source cursor outbox: recovery outbox payload hash mismatch`。MySQL migration 將 `funding_recovery_outbox.payload_json` 定義成原生 `JSON`；生產校驗又對內嵌 `json.RawMessage` 原始 bytes 做 SHA256。這表示 MySQL JSON 序列化正規化可能改變巢狀 payload bytes，使正常記錄不能投影。

影響範圍是 durable outbox 投影/游標恢復路徑；失敗必須繼續 fail-closed，Gate/hold 不得釋放。不得因此降低哈希完整性或令舊 pending 記錄永久無法恢復。不得訪問真實帳戶、下單或宣稱實盤/盈利驗收。

## 已確認的計畫與假設

- 使用 bugfix 版號 `3.111.0-rc1187`，前後端版本一致，只遞增 rc。
- 優先在應用層兼容 MySQL JSON 正規化，不新增 schema migration；舊 payload hash 保留唯讀驗證能力。RC1186 舊格式缺少 `ExpectedRuntimeRevision` 的投影哈希綁定，已由使用者確認採 fail-closed：舊 pending 禁止自動投影/重放並轉人工核帳；新格式在哈希鏈中綁定 revision。
- RC1186 同 SHA CI 失敗必須以新的同 SHA CI 實際通過才算修復完成；RC1185/本地 SQLite/跳過的 MySQL 均不能替代。
- 先維持現有失敗封鎖行為；只修復能證明完整性有效的 durable payload 讀回。
- 任何 JSON 正規化必須涵蓋完整投影（包含 `source_cursor` 和新格式 `ExpectedRuntimeRevision`），未知字段不得靜默丟棄；檢查 `projection_hash` 與 receipt 的錨定關係並記錄普通 SHA-256 的威脅模型邊界。
- 保留工作區既有 `.codex/goal-teams/INDEX.md` 改動及所有未跟蹤舊 RC 目錄、`--help/`、`doc-capsules.jsonl`，不納入本次提交。

## 團隊規劃表

### 2026-10-13 擴圍修訂（已由使用者確認）

| 成員 | Skill/Subagent | 任務 / 鎖定範圍 | 完成標準 | 測試與獨立驗證 |
|---|---|---|---|---|
| 後端-完整哈希調用鏈修復 | `goal_backend` | T01；只改 `storage/funding_recovery.go`、`strategy/funding_carry_durable_recovery.go`、`strategy/funding_carry_income_reconciliation.go`、`web/api_funding_recovery.go` | Storage 導出共用 semantic/legacy verifier 並接入投影/接管/重試/API replay；durable `ExpectedRuntimeRevision` 精確比較；不改 schema/API 外部合約，不放寬 owner/CAS/Gate | QA 新測試；reviewer read-only final review |
| QA-恢复投影边界验收 | `goal_qa` | T02；只新增 `storage/funding_recovery_outbox_integrity_test.go`、`strategy/funding_carry_recovery_hash_integration_test.go` | 覆蓋 receipt-only/runtime-revision mismatch no-side-effect；新/舊 outbox 經實際 production recovery call path；維持 MySQL 8 強制 DSN | QA 執行、Lead 重跑；reviewer 確認測試有效性 |
| 評審-恢復哈希邊界終審 | `goal_reviewer` | T03；唯讀 | 關閉既有 P1/P2/P3；完整 hash call chain、舊 pending compatibility、fail-closed/hold invariants 均成立 | Lead 最終驗收 |

此修訂取代下方初始窄範圍分工。使用者確認「按建議啟動」後才開始新增分工與實作；不含 schema migration、策略交易邏輯擴展、真實帳戶或下單。

| 成員 | Skill/Subagent | Goal Slice | 任務 | 鎖定範圍 | 交付與完成標準 | 文檔/任務清單 | 測試負責 | 獨立驗證 |
|---|---|---|---|---|---|---|---|---|
| 後端-MySQL哈希兼容修復 | `goal_backend` | 保持 durable outbox hash 完整性並支援 MySQL JSON normalization | T01 | 僅 `storage/funding_recovery.go`；先分析舊 pending payload 相容性；不改 schema/API | MySQL JSON 正常讀回可投影；舊格式 pending outbox 可恢復；payload 有語義改動時仍拒絕；不放寬 hash/owner/CAS/Gate 條件 | 回報設計決策/Doc Capsule；Lead 更新文檔 | QA 成員對最終 MySQL 整合路徑驗證 | T03 評審只讀 review |
| QA-MySQL Outbox投影驗收 | `goal_qa` | 真實 MySQL 8 持久化與投影邊界 | T02 | 僅 `storage/funding_recovery_anomaly_mysql_test.go` 及新增獨立測試檔（先協調）；不得改生產程式 | 強制 DSN 下從 seed/讀回/投影/cursor 推進通過；anomaly 調整競爭僅一勝者；負例拒絕 tamper 且 hold 不解除；覆蓋舊 pending envelope | 更新測試計畫與結果 capsule | QA 自己執行 MySQL 8 專項與關聯測試 | Lead 驗證測試；T03 檢查覆蓋可信度 |
| 評審-恢復哈希邊界 | `goal_reviewer` | 只讀獨立安全/相容性複核 | T03 | 只讀，禁止改檔 | 確認正規化處理仍涵蓋完整 payload 語義、舊 outbox 相容、未知/篡改內容 fail-closed、失敗不釋放 hold | 提交 findings 與 capsule | 不擁有測試實作 | Lead 對生產接線與 CI 證據做最終驗收 |

### Reviewer follow-up：完整 response-loss replay 路徑

只讀終審發現成交、anomaly adjustment、fee adjustment 三個 durable response-loss retry 解碼 outbox payload 前未調 verifier；並指出 legacy nil-children 格式遇 JSON key reorder 時仍以原 payload bytes 比較舊 hash。此為已批准的完整生產調用鏈範圍，列入現有 T01/T02，不縮小驗收：

| 成員 | 增補範圍 | 完成標準 |
|---|---|---|
| 後端-完整哈希調用鏈修復 | `strategy/funding_carry_fill_reconciliation.go`、`strategy/funding_carry_anomaly_adjustment.go`、`strategy/funding_carry_fee_adjustment.go`；`storage/funding_recovery.go` 的 legacy nil-children 校驗 | 每個 durable response-loss replay 在 decode/重建 request 前驗 payload hash；nil children legacy hash 以精確重建的舊 serializer bytes 驗證，canonical equality 證明沒有 unknown/變更字段 |
| QA-恢復投影邊界驗收 | MySQL nil-children legacy 測試；策略 response-loss replay tamper tests | MySQL key reorder 後舊 pending 可驗；fill/anomaly/fee replay tamper fail-closed 且 receipt/ledger/cursor/hold 不變；receipt mismatch 有明確斷言 |
| 評審-恢復哈希邊界終審 | 只讀 | 確認所有 durable callers（非僅 raw SHA callsites）都有 verifier，舊格式重排相容和測試閉合 |

API verifier 的錯誤訊息 detail 丟棄只列為診斷建議，不變更外部 API 合約。

### Reviewer follow-up：receipt 錨點前置與嚴格投影 schema

第二輪只讀複核指出，`MarkRecoveryProjected` 雖會拒絕 receipt/outbox 不一致，但 FundingCarry 可能已先 CAS runtime；已完成操作的 replay 也可能不再走 Mark。另 semantic hash 快路徑尚未明確證明 typed projection 沒有忽略 unknown JSON keys。這仍屬已批准的完整生產哈希調用鏈範圍，先更新分工再實作：

| 成員 | 增補範圍 | 完成標準 |
|---|---|---|
| 後端-完整哈希調用鏈修復 | 投影 runtime CAS 前共用驗證 receipt/outbox/episode 身份、狀態、revision、hash；所有 response-loss/completed replay、takeover record receipt（含 target+2 replay）及 completed takeover 候選舊 outbox 均驗證；semantic 與 legacy payload 必須精確匹配已知完整 projection schema | receipt-only mismatch 在任何 runtime CAS/成功 replay 前 fail-closed；一般 projected receipt 僅接受 target+1；target+2 僅在 episode 完整前態、takeover receipt 與 takeover 專用證據全部驗證後接受；takeover receipt request hash 依授權/owner/scope 重算相等，審計時間非零 |
| QA-恢復投影邊界驗收 | 新增 receipt-only mismatch 的 pending 投影、completed retry/takeover 無副作用測試；新增 unknown-field + matching semantic hash 負例；校驗 target+2 無 takeover 證據、receipt request hash/time/審計欄位錯配拒絕 | runtime、ledger、cursor、receipt/outbox、hold 不變；正常 replay 與被審計 takeover replay 通過 |
| 評審-恢復哈希邊界終審 | 唯讀檢查新檢查順序及 schema/hash compatibility | 確認無 runtime-before-receipt-validation、未證明的 target+2、takeover/candidate receipt 漏檢、未知字段忽略；舊 nil-children outbox 相容仍成立 |

## SPEC 與驗收

- Requirement card / PRD：小型儲存相容性 bugfix，需求已由失敗 CI 和 schema/調用鏈證據界定；不另造 PRD。
- Architecture Design：只記錄兼容邊界與 hash 算法選擇，不擴展其他 recovery lane。
- Test Plan / Acceptance：必須覆蓋 MySQL 8 原生 JSON 的確切 round-trip，舊 pending payload、tamper 負例及 CAS/Gate 不變量。
- 完成前不得打 tag 或發佈。推送後要求同 SHA GitHub CI 的 MySQL 測試、全套 Go tests、vet、race、前端測試與構建全綠；失敗則修復並重跑新的提交。

## 停止條件與風險

| 風險 | 負責人 | 停止條件 |
|---|---|---|
| Hash 兼容可能只覆蓋序列化格式、卻漏掉語義改寫/`source_cursor` | 後端 + 評審 | 無法證明完整 payload 綁定或未知字段被拒絕時停止，不可放寬比較 |
| 已持久化的 pending outbox 可能被修復版拒絕 | 後端 + QA | 不能提供舊記錄 fixture/重放證據時停止發布 |
| MySQL 服務端/driver 版本行為不一致 | QA | CI fixture 未使用受支持 MySQL 8 或 DSN 未強制時不驗收 |
| 失敗 outbox 被誤投影/釋放 Gate | 全體 | 任一 hold/CAS/owner 安全斷言回歸立即停止 |
