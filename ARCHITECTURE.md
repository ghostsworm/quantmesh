# QuantMesh 当前架构约束

版本：`3.111.0-rc1196`（开发中，未发布）。

QuantMesh 是既有 Go + React 应用。`cmd/quantmesh` 是启动入口，`internal/app` 负责 runtime 装配与生命周期；`strategy` 承载策略和开仓 Gate；`accounting` 定义恢复事实与授权；`storage` 以 SQLite/MySQL 保存恢复 episode、hold、操作凭据和投影 outbox；`web` 提供受会话权限保护的 HTTP API。前端由 `webui` 构建并嵌入 Go 服务。旧版整体架构材料见 [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)，其中历史版本与配置描述不作为当前恢复协议的依据。

## FundingCarry 管理员解锁续办

恢复状态以数据库为准，runtime generation 的 capability 是账户范围内的写入 fence；generation ID 仅是非秘密审计标识，不能代替 capability。Bot 操作锁负责单实例生命周期串行化，数据库 generation 锁与 CAS 负责跨连接、跨实例竞争。

正常释放分为三个步骤：准备原释放操作并提交 `release_pending`；事务释放 durable hold 并写入 `released`；清理当前 runtime 的精确 recovery Gate。准备提交后中断时，新 runtime 只保留核账能力；普通 takeover 不处理 `release_pending/released`。专用管理员续办在事务中校验原 operation/receipt、scope、证据、owner 和 revisions，追加独立 owner 续办审计，并原子完成原释放操作。之后只移除 `episode_id + recovery_generation` 对应的 Gate。它不启动策略，也不调用交易执行。

续办审计扩展现有 JSON payload，无新增表或 DDL 迁移。原释放授权身份和证据不被改写；新管理员操作使用独立 operation 与 receipt。幂等重试仍验证当前 generation 及提交后证据，证据变化时停止清理。API actor 由管理员 session 注入，客户端不能提交身份或账务事实替代证明。

详细接口、状态转换、故障边界与验证记录见 [FundingCarry 解锁续办协议](docs/funding-recovery-release-resume.md)。本变更未部署，不表示真实交易或盈利验收。
