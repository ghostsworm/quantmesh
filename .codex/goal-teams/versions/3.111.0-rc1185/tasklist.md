# RC1185 任务清单

- [x] 用户确认异常账务规则：保留原始事实，追加有审计依据的精确差额事件。
- [x] 定义 anomaly/fact 类型与精确 decimal delta 领域模型。
- [x] durable anomaly schema、owner-fenced idempotent capture/read APIs（SQLite 已运行测试，MySQL DDL 尚未真实运行）。
- [x] 人工 adjustment operation/CAS/receipt/outbox 原子接线，原 funding 事件不更新/删除。
- [x] 完整历史回扫按原始 ledger facts + adjustments 复核，cursor 只向前；仅显式 release 独立解除 hold/Gate。
- [x] 管理 API、生产 service/router 装配、SQLite 生产组合流程覆盖异常捕获→管理员调整→复扫验证→游标推进；原响应丢失重启流程保留。
- [ ] 独立多 storage process、并发异常调整竞态、真实 MySQL/DSN migration 和同 SHA CI；未提交/推送前远端结果不可验收。
