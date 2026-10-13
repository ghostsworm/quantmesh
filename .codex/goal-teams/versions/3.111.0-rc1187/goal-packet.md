# RC1187 目標包

## 目標

修復 MySQL 原生 JSON 對持久 outbox payload 的正規化與哈希驗證不相容，讓有效新舊記錄可安全讀回/投影，同時保留對內容改寫的拒絕。

## 成功標準

1. RC1186 失敗測試在 MySQL 8.0.36 強制 DSN 下先通過 seed outbox cursor projection。
2. MySQL JSON 正規化前後的有效舊 pending outbox 能經生產 `MarkRecoveryProjected` 完成投影。
3. 修改任一實際語義字段後仍因哈希不匹配而拒絕；不可用“只比較 projection hash 欄位”等方式繞過內容驗證。
4. MySQL anomaly 不同 operation 競爭只產生一筆 adjustment；輸家沒有 ledger/receipt/outbox/cursor 副作用，Gate/hold 持續封鎖。
5. 本地全量 Go/vet/frontend 及推送後同 SHA CI 全綠；未打標前不得說已發布。

## 證據來源

- `storage/funding_recovery.go:1011` 目前以 SHA256 校驗 `outbox.ProjectionPayload` 原始 bytes。
- `storage/migrations/2026101201_funding_recovery_mysql.up.sql` 將 outbox `payload_json` 設為 MySQL `JSON`。
- CI run `38347532032` / SHA `d39ffbcb4f8b4fd4d2edae64e22f5758b529027f` 在新 MySQL 測試 seed projection 失敗。

## 禁止範圍

本輪經使用者再次確認擴展到完整 FundingCarry production hash callsites。semantic/legacy verifier 必須從 Storage 被策略與 Web API 共用；Storage 必須驗證傳入 `ExpectedRuntimeRevision` 等於 durable row。需補 receipt-only mismatch、runtime revision mismatch 無副作用與真實調用鏈新/舊 payload 測試。

reviewer 終審追加：fill、funding anomaly adjustment、fee adjustment 三個 durable response-loss retries 也必須在 decode 前驗 hash；MySQL key reorder 後 nil children 被省略的 legacy pending envelope 必須以精確重建的舊 serializer bytes 驗證。

- 不改 FundingCarry 策略交易邏輯，不做其他策略切片。
- 不降低 hash、CAS、runtime generation、owner 或 hold/Gate 約束。
- 不訪問真實交易所/賬戶，不下單，不做盈利聲明。
- 不提交/推送 `--help/`、舊 RC 目錄、`doc-capsules.jsonl` 或根 INDEX 的既有用戶改動。
