# 3.111.0-rc1187

目標：修正 MySQL 原生 JSON 正規化造成 durable recovery outbox payload hash 不匹配的問題；新 outbox 綁定 runtime revision，舊格式缺少此證明時 fail-closed 並轉人工核帳。

| 文件 | 負責人 | 狀態 |
|---|---|---|
| [團隊計畫](plan.md) | Goal Lead | 已確認 |
| [目標包](goal-packet.md) | Goal Lead | 已建立 |
| [任務清單](tasklist.md) | Goal Lead | 待分派 |
| [驗收標準](spec/acceptance.md) | Goal Lead | 已建立 |
| [測試計畫](spec/test-plan.md) | QA-MySQL Outbox投影驗收 | 已建立 |
| [進度](progress.md) | Goal Lead | 進行中 |
| [決策](decisions.md) | Goal Lead | 已記錄使用者決策 |
| [後端成員包](member-packets/T01-mysql-outbox-hash.md) | 後端-MySQL哈希兼容修復 | 待啟動 |
| [QA 成員包](member-packets/T02-mysql-outbox-projection.md) | QA-MySQL Outbox投影驗收 | 待啟動 |
| [評審成員包](member-packets/T03-outbox-hash-review.md) | 評審-恢復哈希邊界 | 待啟動 |
