# Goal Teams 计划：策略账务失败后的人工核账与显式解锁

## 目标与决策

版本候选：`3.111.0-rc1157`（bugfix 顺延）。用户确认采用方案 2：策略账务执行返回错误时保持 fail-closed，不自动重放；提供持久化、可审计的人工核账/解锁流程。用户已确认本团队计划：管理员操作、必填理由与证据、持久化审计、明确二次确认；只有核账条件满足后才可解除目标订单自己的封锁。

本切片承接 rc1156 未提交工作区，但不得覆盖、重置或清理既有用户改动。仅更改本任务所需文件。无真实账户/生产数据库访问、无下单、无提交/推送/发布授权。

## 当前环境与既有状态

| 项目 | 证据/处理 |
|---|---|
| 指引 | 根目录 `AGENTS.md` 存在；简体中文输出；代码变更版本同步前后端、Changelog 与产品概览；繁体中文 commit message（本任务不提交） |
| 分支 | `main`；工作区含 rc1156 既有未提交和未跟踪内容，全部视为用户资料并保留 |
| 基线代码 | 当前版本字符串为 `3.111.0-rc1156`；新增 bugfix 后顺延为 `3.111.0-rc1157` |
| 既有计划 | `.codex/goal-teams/versions/3.111.0-rc1156/` 中 GT-005/006 已有工作；D-006 现已由用户选择方案 2 解决 |
| API/审计线索 | `web/server.go` 有认证业务路由和 `/api/capital` 管理端点；`web/api_retired_equity_accounts.go` 有管理员校验范例；不得仅依赖忽略写入错误的非关键日志来证明账务操作已审计 |
| 国际化 | `webui` 使用 React + i18next；新增界面文案必须进入 locale 资源，不得硬编码 |
| 发布边界 | 本计划不包含 commit、push、tag、发布或部署；每项需后续明确授权 |

## 安全契约

1. 策略账务返回错误时不自动重放，不推断“未写入”，不解除对应 order quarantine/opening hold。
2. 管理流程需精确绑定 account scope、Bot、symbol/market、strategy owner、client order ID 与 venue order ID；无法唯一归属即拒绝。
3. 操作人必须是非 local-dev 模式下已认证管理员；理由与证据引用必填；二次确认使用一次性/幂等 operation identity，重复请求不得重复入账或清理其他 hold。
4. 用户已批准扩大范围：为所有生产可达策略提供订单级耐久账务游标读回及受控人工 reconciliation。人工输入只记录声明/外部证据，不能伪造策略账状态。人工二次确认后，服务端必须调用策略专属、幂等且耐久的 reconcile 接口；不得重放原始 `OnOrderUpdate` 回调。先读回：游标已到目标时不再写；游标落后时只补记精确缺口并再读回验证；游标超前、无法排序、结果 UNKNOWN、证据冲突或 storage 错误都继续 fail-closed。解锁前再验证 venue 完整 fills/fees 与策略 durable cursor/经济结果一致。
5. 解锁仅清除此流程拥有的 per-order hold/quarantine，不清 `manual`、`runtimeExposureBootstrapBlock` 或其他风险来源，不自动运行交易、不触发下单。解锁后如需完整 ExposureBook bootstrap，必须走现有完整核账流程。
6. 操作记录必须耐久保存操作者、目标 owner identity（不得包含凭据）、状态转换、时间、理由、证据引用/摘要、幂等键和失败结果；策略账务 cursor/reconcile 回执也必须耐久并可在重启后读回。日志/数据库缺失则拒绝改变 gate。
7. 保护性减仓路径是否不受影响必须由调用链测试证明；不作未经测试的推断。

## 团队分工

| 成员 | 角色/子代理 | 任务 | 锁定范围 | 交付与完成条件 | 独立验证 |
|---|---|---|---|---|---|
| 需求分析-人工核账契约 | `goal_requirements_analyst` | 精确状态机、授权与账本证据契约 | 只读产品/代码；仅写本版本 `spec/requirement-spec-card.md` | 区分 quarantined、核账中、待确认、已解除；无推测性状态转换 | Reviewer 校验调用链与拒绝条件 |
| 策略-账务游标/人工 reconciliation | `goal_backend` | 为所有生产可达策略实现订单/cumulative cursor durable readback 与仅人工触发、幂等、无原始回调重放的精确缺口 reconcile | 仅 `strategy/` 与 `position/` 策略状态/测试；不得改 API/storage/web/wiring | 覆盖所有实际注册策略；精确 order identity/cursor/经济结果；重启可读回；游标 ahead/冲突拒绝 | 策略状态机测试、Reviewer 独立复核 |
| 后端-核账审计与解锁 API | `goal_backend` | durable operation journal、管理员 API、runtime per-order reconcile hook | 仅核账相关 `order/`、`execution/`、`storage/`、`web/`、启动 wiring 文件；不改策略实现/其他风险源 | 通过共用 accounting contract；先审计，再显式调用策略 reconcile，再读回核验，最后精确释放 gate；重启可恢复；失败持续封锁 | QA 黑盒/接口测试，Reviewer 代码审查 |
| 前端-人工核账操作台 | `goal_frontend` | 管理员工作台、证据录入、核账执行、独立确认和历史状态 | 仅相关 `webui/src` 页面、service、测试与 locale；不改后端文件 | 完整 i18n；展示服务端游标/证据缺口；分离“执行人工核账”和“确认解锁”；不得调用原始重放接口 | QA UI 测试，Reviewer 检查危险 affordance |
| 测试-核账解锁端到端验收 | `goal_qa` | 独立接口/存储/策略/接线测试 | 新测试文件及本任务测试计划；不得改生产实现 | 覆盖权限、精确归属、重复/并发、数据库失败、进程重启、各策略 cursor/readback/reconcile、不同 gate source 与保护性减仓路径 | Goal Lead 复跑；Reviewer 独立检查断言 |
| 评审-核账安全最终复核 | `goal_reviewer` | 对同一最终 diff、测试和经济不变量独立复核 | 只读；仅追加本版本 review 文档 | 无未处理 P0/P1；明确列出任何缺失的账户/实盘证据 | Goal Lead 对照验收表 |
| Goal Lead | 当前会话 | 规格整合、版本/changelog、任务协调、集成和最终门禁 | `main.go`、`webui/package.json`、产品概览、版本化文档及整合所必需接线 | 单一版本一致；只报告实际运行结果；不宣称实盘/盈利验收 | Reviewer 核查版本与状态报告 |

## 测试、风险与停止条件

| 风险 | 处理 | 停止条件 |
|---|---|---|
| 策略写入后返回错误，结果模糊 | 永不自动重放原始回调；仅显式双阶段人工操作可按耐久游标精确补齐缺口 | 任一生产策略不能读回游标、证明缺口边界或做到 reconcile 幂等时，该策略继续封锁，不得声称全策略完成 |
| 手工解锁绕过另一个安全来源 | 使用独立 gate owner/source，按 order ID 精确释放 | 当前 OpeningGate 无法区分来源时，不做 broad unblock |
| 多账户/Bot 共享交易所订单 | 使用 durable owner mapping 全字段核对 | 归属不唯一立即拒绝 |
| 操作记录与解锁不原子 | 持久化操作状态后才执行可恢复状态转换，使用幂等 CAS | 无法构造 crash-recovery/idempotency 保证时，不声明可用 |
| UI 演示与生产接线不符 | 测试通过真实 API/runtime 注册接线验证 | 仅 helper/unit tests 不算完成 |
| 测试依赖真实账户/交易所 | 使用 fake adapters、隔离 SQLite/MySQL | 不得连接真实账户或下单 |

## 完成标准

- 管理员能发现并查看待核账记录，查看其 owner、venue、fills/fees、策略 durable cursor/经济结果和意图状态，不泄露凭据。
- 证据/理由必填，操作具幂等键和耐久审计；管理员权限、scope 错误、重复请求、并发竞争与中途崩溃均有测试。
- 不能由普通人机确认凭空标记策略账成功；服务端必须先展示权威耐久读回，仅在人工二次确认后按策略专属幂等 reconcile 补记精确缺口，再读回确认；无法覆盖的策略保持封锁。
- 解除动作只影响准确订单的 hold，且不会触发策略账重放或新订单；必要的完整账户 bootstrap 仍须完成。
- 前端页面/文案完成 i18n；Go 全相关包与前端测试通过；独立 QA 与 Reviewer 均交付结果。
- `main.go`、`webui/package.json` 版本一致为 `3.111.0-rc1157`，同步 `CHANGELOG.md`、`PRODUCT_OVERVIEW.md`、`product-overview.md`。
- 记录同一源码差异的测试结果；区分本地未提交、已提交、已推送、已发布。不得将局部测试、编译成功或演示流程称为实盘/盈利验收。
