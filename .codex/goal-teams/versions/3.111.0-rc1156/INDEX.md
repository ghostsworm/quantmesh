# 3.111.0-rc1156 團隊文檔索引

| 文檔 | Owner | 狀態 | 說明 |
|---|---|---|---|
| [計劃](plan.md) | Goal Lead | 已由使用者確認 | 工作範圍、分工、風險及停止條件 |
| [需求規格卡](spec/requirement-spec-card.md) | 需求分析-啟動訂單恢復契約 | 完成（含獨立審查補充） | 包含 durable settled intent 記憶體生命週期 P1 |
| [獨立安全評審](spec/independent-review.md) | 評審-恢復安全審查 | 第一輪完成；最終 diff 待複審 | 證實正成交 bootstrap retry 被 settled map 防重綁檢查阻斷，及零成交缺口 |
| [架構設計](spec/architecture-design.md) | Goal Lead（需求分析/評審輸入） | 实施基线；机制待测试证明 | 同绑定 journal 安全再入、fills/fees-first、CAS/并发恢复均须生产接线证据 |
| [任務清單](tasklist.md) | Goal Lead | 已建立 | 追蹤契約、實作、獨立測試與審查 |
| [測試計畫](spec/test-plan.md) | 測試-訂單恢復生產接線 | 完成（Wegener；实现测试进行中） | 驗證生產訂單事件接線及故障封鎖 |
| [驗收標準](spec/acceptance.md) | Goal Lead | 待需求契約穩定 | 記錄完成證據與明確不涵蓋項 |
| [進度](progress.md) | Goal Lead | 執行中 | 成員派發、證據、阻塞和驗證進度 |
| [決策](decisions.md) | Goal Lead | 已記錄初始決策 | 記錄使用者確認與需求/實作決策 |
| [團隊目標包](goal-packet.md) | Goal Lead | 已建立 | 團隊共同限制及交付契約 |
