# 成員包：後端-MySQL哈希兼容修復

- member_id：T01
- role / skill：Goal backend / `goal_backend`
- 目標：修復 MySQL 原生 JSON 正規化後 `RecoveryOutbox.ProjectionPayload` 哈希失配。
- 範圍：只允許修改 `storage/funding_recovery.go`；必須先確認已有 MySQL pending row 能恢復；不新增 migration，不改其他 package。
- 必須保留：完整 projection payload 完整性、hash、runtime generation、owner/CAS、Gate/hold fail-closed。
- 關鍵證據：CI `38347532032` 失敗 `storage/funding_recovery_anomaly_mysql_test.go:123`; SQL migration outbox `payload_json JSON`; `MarkRecoveryProjected` 驗原始 `ProjectionPayload` bytes hash。
- 交付：修復方案/權衡、差異、舊資料相容性理由、本地測試結果、Doc Capsule、未解風險。
- 禁止：弱化 hash、只信存儲的 hash 欄位、清除 outbox、改 schema/API、執行真實帳戶動作。
