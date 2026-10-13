# T04 成员目标包：恢复并发安全评审

- display_name：评审-恢复并发安全
- role/subagent：独立代码审查 / `goal_reviewer`
- version：3.111.0-rc1186
- claimed_tasks：T04
- goal：独立检查 anomaly adjustment transaction/CAS 与并发 SQLite/MySQL 测试，找出会导致重复入账、历史覆盖、身份串用、cursor 前进或 hold 解除的路径。
- required_doc_load：`plan.md`、`spec/acceptance.md`、`spec/test-plan.md`、生产函数、两位实现/测试成员的 diff。
- allowed_scope：只读审查，不修改任何文件。
- forbidden_scope：不得以存在唯一索引或通过单元测试代替语义审查；不得登录真实交易账户。
- locked_scope：重点审查跨句柄锁、MySQL 行锁/隔离级别、CAS loser、提交后响应丢失、重复请求与安全 fixture。
- output_contract：按文件/行和执行路径给出 P1/P2/P3 发现或说明未发现 P1/P2；列明未覆盖情形和建议验证，不能代作者认定通过。
