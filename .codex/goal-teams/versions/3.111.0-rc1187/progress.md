# RC1187 進度

## 起始證據

- `main` 與 `origin/main` 均為 `d39ffbcb4f8b4fd4d2edae64e22f5758b529027f`；RC1186 GitHub CI `38347532032` 已完成且失敗。
- CI 日誌：`TestMySQLFundingRecoveryAnomalyDifferentAdjustmentsHaveSingleWinner` 在 seed cursor projection 收到 `recovery outbox payload hash mismatch`。
- MySQL migration 的 `funding_recovery_outbox.payload_json` 為原生 `JSON`；生產 `MarkRecoveryProjected` 對讀回的 `json.RawMessage` bytes 計算 SHA256。原生 JSON 可能重排/正規化嵌套 JSON bytes，根因仍需由後端分析和 MySQL fixture 證明。
- RC1186 已推送但未打 tag/發布。不可將失敗 CI 描述成通過。

## 分工狀態

| 成員 | 任務 | 狀態 | 證據 | 下一步 |
|---|---|---|---|---|
| 後端-MySQL哈希兼容修復 | T01 | 已領取/分析中 | 尚未改生產程式 | 提交兼容算法分析與舊 pending rows 證據 |
| QA-MySQL Outbox投影验收 | T02 | 已領取/測試設計中 | CI 失敗已定位至 seed projection | 增加 MySQL JSON round-trip、舊記錄和篡改斷言 |
| 評審-恢復哈希邊界 | T03 | 已領取/獨立分析中 | 只讀檢查 baseline | 待最終差異 review |
| Goal Lead | T04 | 文檔/狀態核驗 | main/遠端/CI 已重新 readback | 整合、全門禁、相同 SHA 驗收後才 release |

## 擴圍修復進展

- 使用者再次明確批准修復所有 FundingCarry production hash callers，以及 `ExpectedRuntimeRevision`/receipt-only 完整性測試；已按不重疊 locked scopes 啟動後端、QA 和只讀 reviewer。
- 後端已完成 shared `storage.VerifyFundingRecoveryProjectionHash` 接線：FundingCarry durable recovery 4 處、income replay 1 處、Web API readback 1 處皆使用兼容 verifier；`MarkRecoveryProjected` 比較 durable/incoming runtime revision。生產源碼 rg 不再發現 raw `ProjectionPayload` SHA 校驗。
- QA 新增 Storage 篡改無副作用測試與 FundingCarry 真實 reconcile/retry 組合測試。定向測試、storage 全包、storage/strategy/web vet 通過；strategy 全包在 QA 先行執行時失敗但未保留具體失敗用例，Lead 正用最後整合版本重跑 `go test ./storage ./strategy ./web`。
- Lead `go vet ./...`、gofmt、`git diff --check` 通過。此前完整 Go 套件在 loopback 限制重跑時主包曾報失敗但輸出被大量日誌截斷，整套結果未驗收；須定位後重跑。MySQL DSN 缺失，MySQL CI 與同 SHA GitHub checks 尚未跑。沒有提交、推送或發布。

## Reviewer follow-up round

- T03 終審另外發現三條 response-loss durable replay（fill/anomaly/fee adjustment）未先驗 hash，及 nil-children legacy payload 的 MySQL key reorder 相容分支缺口。依「完整生產哈希調用鏈」已確認範圍追加到 T01/T02：只允許新增這三個策略文件接線、storage legacy validator 修復及對應測試。
- 經整合修正後，`go test -p=2 ./storage ./strategy ./web -count=1 -timeout=20m` 全部通過（storage 9.8s、strategy 184.6s、web 22.5s）；`go vet ./...`、gofmt、diff check 通過。先前全量主包失敗屬於另一輪 loopback/runtime 環境下輸出無法定位；仍需在 final combined diff 重跑全套，不能將三個關聯包等同全倉通過。
- T01/T02 follow-up 尚在進行；後續驗收仍需再跑新增回放測試和 MySQL 8。MySQL DSN 仍未配置，未提交、未推送、未發布。

## 本輪工作啟動

- 使用者確認按 RC1187 計畫繼續；在 `d39ffbcb` 上建立分支 `codex/funding-recovery-outbox-json-hash-rc1187`。
- 後端完成 `storage/funding_recovery.go`：對完整 projection（含 source cursor）採精確語義 JSON hash，驗證舊 pending 兼容時拒絕未知欄位被丟棄，並在投影前交叉核對 receipt/outbox 錨點。沒有 schema/API 變更。
- QA 新增 MySQL outbox round-trip / 舊 payload / tamper / CAS regression 測試，以及通用 JSON canonicalizer 精確數字、陣列、重複鍵和尾隨值測試。`go test ./storage -count=1 -timeout=10m`、定向 canonicalizer 測試、`go vet ./storage`、gofmt 與 diff check 通過。三個 MySQL 用例因本機無 `QUANTMESH_MYSQL_TEST_DSN` 而跳過，不計通過。
- Reviewer 已回報首輪邊界建議並收到終審請求，最終 findings 尚待回覆。Lead 尚需完成全量 Go/vet/前端/race 驗證，再依使用者授權提交並推送；只有同 SHA MySQL 與完整 CI 全綠後才可打 tag/發布。

## CAS revision 綁定修復

- 獨立終審發現 P1：外層 `RecoveryOutbox.ExpectedRuntimeRevision` 不在 `ProjectionHash` 覆蓋範圍內；只改寫持久化 outbox metadata 可能讓 runtime CAS 使用未被原 projection hash 綁定的 revision。
- 已修復新寫入格式：將 `ExpectedRuntimeRevision` 加入 hashed projection，receipt/outbox 配對及 takeover candidate/pending 驗證交叉核對外層值；`MarkRecoveryProjected` 同時拒絕 durable metadata 與 hashed projection 不一致。未新增 schema migration。
- 新增 storage 測試：持久化層只改寫 outbox metadata 時，投影標記拒絕且 episode/receipt/outbox/cursor/ledger/hold 不變；新增真實 FundingCarry reconcile 路徑在 runtime CAS 前拒絕同一錯配的測試。
- 定向 storage/strategy 用例通過；`go vet ./...`、`git diff --check` 通過。全倉 Go 首次執行受沙箱 loopback 監聽限制而有無關 httptest 失敗；提權重跑及前端 yarn verify 曾在完成前被中止（前端 318 項測試已通過，build 尚未完成）。需對最終 combined diff 重跑。
- 使用者已確認舊格式採 fail-closed：`VerifyFundingRecoveryProjectionHash` 保留舊 payload hash 唯讀稽核相容，但 receipt/outbox pair、投影標記及 takeover pending/candidate 無新綁定字段即拒絕，自動投影/重放必須轉人工核帳。新增單測斷言舊 hash 可讀、pair 以人工核帳原因拒絕。
- 未提交、未推送、未發布、未打 tag；MySQL DSN 未配置，MySQL 專項和同 SHA 遠端 CI 未驗收。

## RC1188 MySQL CI fixture 修正

- PR #9 上 RC1187 SHA `997ea94fcb460fd0e9190656d48057feb47219b7` 的 GitHub Actions run `38365627479` 使用 MySQL 8.0.36；前端測試通過，但 `go test` 在 `TestMySQLFundingRecoveryOutboxJSONRoundTripAndLegacyHashBoundProjection` 與 `TestMySQLFundingRecoveryOutboxReorderedLegacyProjectionWithOmittedNilChildrenFailsClosed` 失敗。兩處錯誤均為 `Error 3141`，原因是 fixture 把未加引號的 64 位 `legacyHash` 當 JSON 文檔 `CAST(? AS JSON)`；並非 production projection payload JSON 無效。
- QA-MySQL夾具故障核驗獨立確認最小修正：`projection_payload` 繼續 `CAST(? AS JSON)`，`projection_hash` 改用普通字串 SQL 參數，讓 `JSON_SET` 將其保存為 JSON string。生產程式、schema、哈希/receipt/CAS/Gate 校驗不變。
- RC1188 已同步到 `main.go`、`webui/package.json`、兩份產品概覽及 `CHANGELOG.md`。本地 `go test -p=2 ./... -count=1 -timeout=30m`（允許 httptest 回環綁定）、`go vet ./...`、WebUI `yarn verify` 與 `git diff --check` 通過；RC1188 兩個 MySQL 用例因本機無 `QUANTMESH_MYSQL_TEST_DSN` 明確 skip，不計通過。本機沒有啟動 MySQL 服務。
- 評審-MySQL夾具修復安全複核未發現程式缺陷：SQL 將 projection hash 作 JSON string 寫入、projection payload 繼續作 JSON object 寫入；無生產程式/schema/hash/CAS/Gate 變更。RC1187 原失敗不代表 RC1188 通過；尚需新 SHA 遠端 MySQL 8.0.36 與完整 CI 全綠後才可結案/考慮後續發布。
