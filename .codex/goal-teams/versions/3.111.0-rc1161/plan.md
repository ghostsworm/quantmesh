# Goal Teams 计划：核账服务生产装配与安全封锁

## 目标与已确认边界

承接用户已确认的方案 2 与 RC1159 全链路计划。把人工核账持久化案例/审计及可信订单证据读取接入生产启动路径；本切片不伪造任何策略账务适配器或 Gate bridge。只要 `Accounting` 或 `Gate` 尚未有可验证的逐策略实现，核账/解锁必须不可执行并保持 owner 风险封锁。

版本按 bugfix 顺延为 `3.111.0-rc1161`。禁止真实账户/生产数据库访问、下单、撤单、发布或部署。MySQL/SQLite 仅使用隔离测试数据库；盈利不得宣称已验收。

## 基线证据

- RC1160 提交：`210f2ba34daa464bef9cd9858228015277ab0041`，已推送到 `origin/main`。
- 同 SHA 的 GitHub CI #801 成功：MySQL 8.0.36 service 启动，Go 全仓/前端/静态检查成功，强制 MySQL race gate `pass=2373`、缺失 MySQL 验证用例为 0，Linux amd64 构建成功。
- 独立审查确认 `web.SetOrderReconciliationService` 只有测试调用；生产审计 API 因服务未注入会返回 503。
- `main_order_reconciliation_evidence.go` 已有只读可信订单证据读取器；核账服务中的 `Accounting`、`Gate` 生产适配器尚无实现。不得把它们留空说成完整闭环。

## 分工与锁定范围

| 成员 | 子代理 | 任务 | 锁定范围 | 验收/独立验证 |
|---|---|---|---|---|
| 后端-核账服务生产装配 | `goal_backend` | 设计并实现启动装配 helper；仅注入耐久 SQL 案例/审计存储和可信订单证据读取器 | 新增 `main_order_reconciliation_service.go` 及其单测；Goal Lead 单独编辑 `main.go`、版本与文档 | 启动后管理员可读取/创建耐久核账案和审计；Accounting/Gate 缺失时 reconcile/release 明确失败；测试覆盖初始化与重启读回 |
| 策略-账务能力盘点 | `goal_requirements_analyst` | 只读盘点生产策略的 durable cursor、CAS、费用币种/精度、UNKNOWN/部分成交与重启能力 | 仅源码与现有测试阅读，不改文件 | 按策略给出可证明支持/unsupported 及缺失证据，独立 Reviewer 复核 |
| 测试-核账生产装配验收 | `goal_qa` | 对实际启动 composition 与 API 持久化路径设计/执行测试 | 仅测试文件；不得编辑实现 | 验证生产初始化而非仅 handler/helper，并验证服务不可用时安全封锁 |
| 评审-核账装配安全复核 | `goal_reviewer` | 只读审查 owner scope、迁移并发、生命周期、缺失依赖和失败状态 | 不改实现 | 确认无越权/提前开 Gate；列明未覆盖风险 |

## 停止条件

- SQL schema 初始化无法证明幂等/并发安全或不能保持原存储工作流兼容：停止生产装配，保留旧的不可用 fail-closed 状态并报告。
- 不能证明完整订单归属、终态、成交与费用证据：该核账案不得进入策略账务 reconcile。
- 没有经过独立验证的逐策略 `OrderAccounting` 与 owner-specific durable Gate bridge：严禁接通成功 reconcile/release。
- 任一测试使用真实密钥、真实账户或真实交易：立即停止并移除该访问路径。

## 测试与交付

实现后需覆盖：初始化成功/失败、MySQL 与 SQLite 隔离库、重复启动/幂等迁移、案例和审计持久化读回、受限账户授权、缺少 `Accounting`/`Gate` 的封锁、schema/DB 故障、race 与 restart。随后运行 Go 全仓、MySQL 强制门禁、前端检查、构建与独立安全评审；不将本轮本地通过冒充远端 CI。
