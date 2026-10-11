# Goal Teams 计划：人工核账全链路与盈利证据闭环

## 目标

用户要求将此前列出的缺口整理成文档并全部解决。此切片承接已确认的方案 2：策略账务不确定时保持封锁，通过管理员显式核账恢复；不自动重放。重点将现有能力接入所有可安全支持的生产策略，修复 MySQL Bot 状态时间持久化回归，并建立可复算的盈利证据框架。

## 当前基线与边界

- 起始分支为 `main`，HEAD `cc40271a` / `v3.111.0-rc1157`，与 `origin/main` 对齐；工作树已有 RC1158 时间修复与文档版本改动及团队计划材料，均须保留。
- 本地候选按 `3.111.0-rc1159` 继续顺延；最终版本、Changelog、两个产品概览必须一致。
- 不连接真实账户、生产数据库或交易所；不下单、不部署。模拟/本地隔离 MySQL、SQLite、fake exchange 测试属于允许范围。
- 本轮不默认提交、推送、打 tag 或发布；以用户对 Git 交付动作的明确授权为准。
- 真实净盈利不能由代码本身证明。可完成指标定义、来源契约、复算/报告能力和仿真验证；实际盈利验收必须等待用户提供/授权真实成交与账户账单数据。

## 执行顺序

1. 固定需求与证据边界；盘点所有生产注册策略、现有核账 API/审计/owner intent/runtime gate。
2. 先修 RC1158 Bot 状态 MySQL 时间参数问题，新增强制 MySQL round-trip 验证；本地 MySQL 案例不得静默跳过。
3. 定义共享精确 accounting contract：venue trade ID、owner 身份、订单终态、累计 cursor、币种化费用、operation ID、expected revision 与 durable receipt。
4. 逐个为有充分证据的生产策略实现 durable readback 与人工幂等 reconcile；无法证明完整经济含义的策略保持 `unsupported` 和封锁，不得伪称全策略可解锁。
5. 持久化 quarantine/case/audit 状态并接入管理员 API 和 runtime gate 的精确 owner/source 恢复；不允许 gate 先开后记审计。
6. 接通前端管理员操作台和所有 locale；核账执行与二次解锁分别操作，读取后端权威证据。
7. 独立 QA 覆盖权限、归属、费用、重启、并发、超时 UNKNOWN、DB 故障、其他 gate、保护性减仓路径；Reviewer 复核最终同一 diff。
8. 建立利润证据报告契约：版本/配置/数据区间、成交与手续费/利息/资金费、权益对账、策略归因、滑点、净 P&L、回撤及复算校验。用合成/历史导入 fixture 验证计算，不声称真实盈利。
9. 对最终源码指纹执行 Go、MySQL、race/vet、前端检查，并报告同一提交 CI/CD、发布状态；仅当授权且有远端证据时标为已推送/发布。

## 成员职责与范围

| 成员 | 任务切片 | 锁定范围 | 交付/完成条件 | 独立验证 |
|---|---|---|---|---|
| Goal Lead | RC1158 时间 bug、版本/文档、跨切片整合 | `main.go`、`webui/package.json`、Changelog、概览、必要整合文件 | 所有代码变更同步 `rc1159`；差异可追溯、未覆盖既有脏内容 | QA 与 Reviewer |
| 策略-账务核验 | 生产策略 registry 清单及订单级 durable cursor/reconcile | `strategy/`、`position/` 与专属测试 | 每个策略可独立读回、按缺口 CAS、费用完整；不满足者明确 unsupported/fail-closed | 跨层 QA、Reviewer |
| 后端-审计解锁接线 | durable quarantine/case、admin API、owner-scoped gate bridge | 核账相关 `order/`、`execution/`、`storage/`、`web/` 与启动接线 | 审计先于状态转移；幂等恢复；只释放准确 owner/order source | 独立 API/存储测试 |
| 前端-核账操作台 | 管理界面与双阶段操作 | 相关 `webui/src`、测试、locale | 文案全 i18n；无强制绕过；展示服务端证据 | 独立 UI/locale 测试 |
| 验证-跨层验收 | 针对实际生产调用链做测试 | 新测试与 test-plan | 不以 helper-only 或跳过 MySQL 通过；调用链覆盖到启动注册 | Goal Lead 复跑 |
| 评审-安全与盈利证据 | 只读安全审查、财务证据定义 | Review 文档、profit-evidence 规格 | 明示无法安全支持的能力和真实证据缺口 | Goal Lead 对照源代码 |

## 停止条件

- 策略缺 trade identity、费用币种/精度、完整成交历史或 CAS/readback 时，不实现该策略的成功解锁路径；保留封锁并列明具体缺口。
- 跨存储无法证明 crash-safe 状态机、审计不可用或 owner 不唯一时，禁止开 gate。
- MySQL 实际服务不可用时，报告为未验证；不可将 SQLite/解析单测冒充 MySQL 集成通过。
- 真实资金、盈利、实盘交易或已发布的结论必须有相应外部数据/环境证据；不得用模拟数据代替。
