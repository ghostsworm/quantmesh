# Goal Teams Tasklist：人工核账与显式解锁

| Task | Owner | 状态 | 锁定范围 | 交付/完成条件 | 验证/文档 |
|---|---|---|---|---|---|
| GT-009 | 需求分析-人工核账契约 | completed | 只读代码；只写需求规格卡 | 已查明现有策略接口无订单/累计游标 durable readback；用户已批准扩展所有生产策略以实现 durable cursor + 显式幂等人工 reconcile | `.codex/goal-teams/versions/3.111.0-rc1157/spec/requirement-spec-card.md` |
| GT-010 | 策略-账务游标与人工 reconciliation | blocked_on_strategy_durable_accounting | 仅 `strategy/`、`position/` 生产策略状态/测试 | 当前多数策略缺 per-order durable cursor/CAS；Grid/Signal 适配按安全边界未改生产实现 | 先补策略账本/状态迁移与重启测试，再接 API |
| GT-010A | Grid/position 策略适配 | blocked_on_durable_fill_and_cas | 只读盘点；未改 `position/` 代码 | Grid slot 轮换无订单历史、runtime store 无 CAS；现有 order_fills 可作为成交证据候选，但需确认覆盖与 cycle/成本基础映射 | 架构设计阻塞记录 |
| GT-010B | Signal 策略适配 | blocked_on_terminal_order_history | 只读盘点；未改 `strategy/signal_*` 代码 | Signal 终态清除 active order；要有 durable owner-scoped 全订单账本及 CAS 才可安全补账 | 架构设计阻塞记录 |
| GT-010C | 耐久 owner intent 精确读回 | in_progress | `order/owned_intents.go` 与新增测试 | 启动已加载 journal 后按 order ID + CID 唯一读回完整 owner/状态，不泄密、不改 gate | GT-015、GT-013 |
| GT-011 | 后端-核账审计与解锁 API | partial_fail_closed | 新增 accounting/storage/web 与只读 runtime evidence provider | case/audit 存储、双阶段 API、双 revision、游标 suffix 和只读 evidence provider 已有；缺生产策略账、Gate bridge、专用审计读取 API | 继续实现；缺 adapter 时不注册伪服务 |
| GT-012 | 前端-人工核账操作台 | in_progress | 相关 `webui/src` 页面、service、测试、locale | 展示真实服务端读回证据；分离人工 reconcile 与二次解锁确认；完整 i18n，不暴露无条件绕过 | 前端测试、GT-013 |
| GT-015 | 独立 QA-跨层恢复验收 | pending_contract_alignment | 新测试文件与测试计划，不改生产实现 | 独立覆盖所有策略 cursor、人工缺口修复、权限/归属、重启、并发、审计失败、gate 与生产 wiring | Goal Lead 复跑、GT-013 |
| GT-013 | 评审-核账安全最终复核 | interim_review_complete; final_pending | 只读当前实现和测试 | 首轮确认无 P0；仍有 P1：生产策略账 CAS/幂等与 owner-scoped durable Gate bridge 未接线；经济结果缺手续费账本且成交数据源为 float64；审计无专用读取 API。不能安全解锁 | 完成精确经济账、策略适配、Gate 重启协议及审计读取后做最终审查 |
| GT-014 | Goal Lead-版本与整合 | in_progress | 版本/Changelog/产品概览/文档与必要集成 | 版本一致 `3.111.0-rc1157`；同一 diff 完成门禁；准确区分 Git/发布状态 | Reviewer 核验 |

## 依赖顺序

GT-009 已完成 → 用户批准策略级 durable readback/reconcile 扩展 → Goal Lead 冻结跨层 accounting contract → GT-010 策略能力与 GT-011 后端、GT-012 前端并行 → GT-015 独立验收 → GT-013 最终审查 → GT-014 版本/文档及交接。

## 禁止事项

不得自动重放策略账；不得由人工输入直接伪造“账已入”；不得无差别清理 OpeningGate；不得绕过 UNKNOWN、owner mismatch、venue/fill/fee/strategy ledger 证据缺口；不得访问真实账户、下单、提交、推送或发布。
