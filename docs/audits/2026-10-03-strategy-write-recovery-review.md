# 策略参数写入口的恢复身份绕过复查

基线：`d3744605` / `3.111.0-rc973`，开发分支 `codex/verified-capital-release-rc876`。本次只读审查及隔离测试，不修改生产交易逻辑或版本，不涉及真实账户。

## 已复现的高风险缺口

`PUT /api/bots/:id/strategy` 的 `putBotStrategy` 直接读取配置快照、覆写整个 `Strategies` 列表，再调用 `UpdateConfigWithBotHistorySource`。它没有使用生命周期内完整金融状态门禁。现有运行中策略类型限制不能保护同类型的恢复身份参数，也不能保护已停止但仍有借贷待办的 Bot。

临时SQLite中建立一个已停止的 `spot_margin` Bot，策略为 `spot_short`，配置身份为 `old-group/BTCUSDT`。schema9记录含同一身份及 `pending_borrow`：amount=0.4、phase=borrowed、borrow_transfer_id=42、created_at_unix_milli=1；其余还款/消费记录为空。独立旧身份核验及rc973生产门禁均返回 `ErrRecoveryConfigRequired`，数据库确认没有钱包预留。

| 独立请求 | HTTP结果 | 主配置身份变更 | 金融payload | 请求后门禁 |
| --- | --- | --- | --- | --- |
| 同类型spot_short，group_id改为new-group | 200 | 是 | 原样保留 | ErrRecoveryConfigUnverified |
| 同类型spot_short，策略symbol改为ETHUSDT | 200 | 是 | 原样保留 | ErrRecoveryConfigUnverified |

两项安全断言均失败，这是漏洞红测，不是成功验收。策略列表替换可能使原借款待办失去与当前配置一致的恢复依据；未证明发生真实负债变更或已执行重启。实际 `spotShort.restoreRuntimeStateLocked` 要求Bot/策略/group/symbol/base_asset身份匹配，因此该变更与现存恢复契约冲突；本轮只读回生产配置门禁，没有冒充完整启动/浏览器验收。

## 可复核证据

- 隔离目录：`/private/tmp/quantmesh-strategy-write-audit.cGrfaz/`，包含测试源、overlay.json和JSON/Markdown结果。
- 命令：`go test -race -overlay=/private/tmp/quantmesh-strategy-write-audit.cGrfaz/overlay.json ./web -run '^TestAuditStrategyWritePendingBorrowIdentity$' -count=1 -v`。
- 终态：退出1，Web测试1.816s，两子案例均记录 `http=200 primary_identity_changed=true journal_unchanged=true before=required after_unverified=true`。
- 复用隔离配置/SQLite夹具、实际 `putBotStrategy` 及实际配置持久化；生命周期provider是fixture，不是默认管理器完整流程。未操作生产数据或金融RPC。

## 修复验收要求

1. 在生命周期串行化内读取最新配置并核验完整金融状态，不能在锁外核验后写入陈旧全量快照，也不能仅检查资金预留。
2. 有待核清金融状态时，恢复身份/策略定义不能被覆盖；数据不可核实不能当作无记录。上述两项必须拒绝且原配置/日志不变。
3. 保留运行中正常风险参数调整能力；明确哪些更新可安全热应用、哪些改变恢复契约。不能以拒绝所有运行中参数编辑替代设计或引入能力退化。
4. 验证正常无记录、合法核清、管理器缺失、锁内晚到记录、并发启动/写入、持久化失败及运行时更新失败的行为，并逐项读回。全量配置写入/热更新也需另行逐入口审查。
5. 生产修复需同步前后端rc版本、CHANGELOG和Product Overview及针对性回归；本审查未实施修复，不改变R01–R15范围，不宣称实盘/盈利验收。

本轮不修改main/tag、不推送或部署；推送目的地批准仍未收到。
