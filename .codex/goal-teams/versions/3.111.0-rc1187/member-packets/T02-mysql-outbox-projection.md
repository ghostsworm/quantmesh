# 成員包：QA-MySQL Outbox投影驗收

- member_id：T02
- role / skill：Goal QA / `goal_qa`
- 目標：覆蓋 MySQL 原生 JSON 往返、舊 pending outbox 投影和 RC1186 anomaly adjustment 路徑。
- 鎖定範圍：只新增/修改 storage 測試文件；不得改生產程式或 migration；與 Lead 先協調共享測試檔以避免競寫。
- 必須測試：seed source cursor operation 的 `GetRecoveryOutbox` + `MarkRecoveryProjected`；MySQL-normalized JSON 老記錄；payload 任一語義字段篡改拒絕；anomaly 不同 operation race、兩側 retry 與 cursor/hold/receipt/outbox/ledger readback。
- 測試環境：GitHub Actions 強制 MySQL 8.0.36 DSN + destructive schema opt-in；本地缺 DSN 時僅標記 skip，不能標 pass。
- 交付：測試文件、具體命令與結果、DB/Go 行為觀察、Doc Capsule、未覆蓋邊界。
