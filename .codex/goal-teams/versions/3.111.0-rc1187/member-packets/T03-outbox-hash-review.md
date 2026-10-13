# 成員包：評審-恢復哈希邊界

- member_id：T03
- role / skill：獨立 Reviewer / `goal_reviewer`
- 目標：只讀評審 RC1187 最終生產實現與 MySQL 測試。
- 鎖定範圍：只讀，不修改文件。
- 核心問題：MySQL JSON normalization 的兼容方式是否同時綁定完整 projection 語義；既有 pending outbox 是否能恢復；未知字段/空字段/時間、數字表示有無 hash 繞過；CAS、generation、owner、hold 不變量是否保留；MySQL test 是否真實兩 connection 且足夠確定。
- 交付：按嚴重度列出 findings，對應檔案/行號；說明已驗及未驗、應停止發布條件、Doc Capsule。
