# Goal Teams 计划：核账生产组合验收与迁移并发安全

## 目标与已确认边界

完成用户确认的顺序第一阶段：验证生产核账装配至受保护 API、持久化 case/audit 和重启读回的组合调用链；消除 SQLite/MySQL 迁移在多实例同时启动时可能发生的 schema 竞态。本切片不实现逐策略 durable cursor/CAS、经济账适配器或 Gate 解锁桥。

版本按 bugfix 顺延为 `3.111.0-rc1162`。只使用隔离测试数据库和模拟运行态，不连接真实账户，不下单/撤单，不触碰现有未跟踪文件；不提交、推送、打 tag、发布或部署，除非用户另行明确要求。

## 基线与根因线索

- 基线提交：`b8ece4019ec59398322344936659261c9a5c4c0e`（main，RC1161）。
- RC1161 已在 `main_order_reconciliation_service.go` 装配耐久 SQL repository 与只读运行态 evidence provider；`Accounting`、`Gate` 显式留空。
- `main.go` 在 `StorageService` 和 `BotManager` 创建后注册服务，但 RC1161 测试将持久化重启和 API handler 覆盖拆开，未证明真实组合 seam 到受保护路由的一条端到端路径。
- `storage/order_reconciliation.go:migrateOrderReconciliationRevisionColumns` 采用 `columnExists` 后执行 `ALTER TABLE ADD COLUMN`。多实例在两次检查间同时看到列缺失时可能重复 ALTER；SQLite/MySQL DDL 的锁定与事务语义不同，需按真实方言证明安全。
- 本地此前已存在的未跟踪文件必须保持原状，不得纳入任何补丁。

## 分工与锁定范围

| 成员 | 子代理 | 任务 | 锁定范围 | 验收/独立验证 |
|---|---|---|---|---|
| 后端-核账迁移并发安全 | `goal_backend` | 使 SQLite/MySQL order reconciliation schema migration 在并发启动下可安全重入 | `storage/order_reconciliation.go`、`storage/order_reconciliation_test.go` | QA 执行迁移测试；Reviewer 独立审查 DDL/锁策略 |
| 测试-核账生产组合验收 | `goal_qa` | 建立生产服务装配到受保护 API、case/audit 持久化与 restart 的组合测试 | 测试文件；禁止修改 storage migration 实现 | 评审成员独立检查测试确实穿过 production composition seam |
| 评审-核账启动迁移安全复核 | `goal_reviewer` | 只读审查 SQLite/MySQL 并发语义、启动装配、锁/死锁与安全封锁 | 无写入 | Goal Lead 处理发现并记录验收状态 |
| Goal Lead | 当前会话 | 集成、版本文档、全量验证和最终状态核验 | 版本/changelog/产品概览/启动或测试 seam 的必要协调，不覆盖成员锁定文件 | 检查完整 diff、命令输出、版本一致及真实 Git 状态 |

## 验收条件

1. 至少一个单一组合测试从真实 `StorageService` schema 初始化开始，经 production service builder 与真实受保护路由，创建/读取核账案和审计，验证 `Accounting`/`Gate` 缺失时 reconcile/release fail-closed，并在重启后读回。
2. SQLite 多个独立 storage handle 并发重复初始化，不出现迁移错误、死锁或半完成 schema；测试可重复运行。
3. MySQL 8 隔离库并发迁移能证明恰好一个有效 schema 结果，已有旧 schema 补列和重复启动也安全。若本机 DSN 不可用，则 CI 必须有强制 MySQL 场景；纯 SQLite 不可代替。
4. 迁移部分失败或 DB 不可用时，生产装配不可注册半成品服务；不会打开 Gate 或报告解锁成功。
5. race、重复操作、旧版 schema 升级、版本号/CHANGELOG/产品概览一致性及独立安全评审通过。
6. 不宣称策略账务支持、真实实盘或盈利已验收；不宣称本地改动已提交/推送/发布。

## 停止条件

- 无法依据 SQLite/MySQL 官方/驱动语义证明锁或 DDL 方案不会自锁、永久阻塞或掩盖迁移错误时，停止改变迁移实现并报告根因。
- 为测试真实受保护路由必须触碰真实账户、密钥或交易接口时，立即停止并改用隔离存储及 mock 身份/运行态。
- 无法加入可信组合 seam 而需要大规模 main 启动重构时，先报告，不扩展范围。
- 任何实现若令缺 `Accounting`/`Gate` 的 case 可完成补账或释放，立即回滚该实现差异并保留 fail-closed。

## 文档、测试与交付状态

编码完成后执行 storage/main/web 定向测试、迁移并发测试、相关 race、Go 全仓及必要的强制 MySQL gate；按实际环境区分通过与跳过。前后端版本必须同为 `3.111.0-rc1162`，CHANGELOG 与 overview 同步。工作区提交、远端推送、CI 和发布状态逐项分别记录。
