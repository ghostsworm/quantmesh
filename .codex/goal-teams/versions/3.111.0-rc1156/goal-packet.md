# 团队目标包

- 目标：闭合普通 Bot 自有活动委托在启动及运行时终态之后的 owner-scoped 恢复与 ExposureBook 核账。
- 版本：`3.111.0-rc1156`（bugfix 候选；只有实际实现修复时才更新代码版本）。
- 成功标准：以 `tasklist.md` 的完成标准为准；未知/部分成交/外部委托均不得误解除封锁；订单、成交费用、策略库存和共享暴露有持久证据；生产接线由独立 QA 验证，最终差异由独立 Reviewer 审核。
- 语言：团队文档、测试说明与人工交接使用简体中文；代码标识符和源日志保持原样。
- 已发现文档：根 `AGENTS.md`、`CHANGELOG.md`、`product-overview.md`、`docs/audits/2026-10-03-remediation-progress.md`、本版本 `plan.md` 与 `tasklist.md`。
- 允许范围：用户已确认 rc1156 扩展分工；后端只改 tasklist GT-005 列出的七个文件，QA 只改 GT-006 列出的两个测试文件，Reviewer 只读并追加独立报告；Goal Lead 维护版本化 SPEC、版本、CHANGELOG、产品概览、验收及整合文档。超文件范围先停下请求批准。
- 禁止范围：真实账户或生产数据库访问、任何交易/撤单、部署/发布、绕过 gate、清理未追踪 `--help/` 或 `.codex/` 资料、未获授权的提交/推送/标签。
- 关键不变量：intent 身份完全匹配；完整且新鲜的 venue 快照；每个正累计成交更新先持久化 fills/fees 再做策略账；terminal intent 只在两类账本 durable 后结算；CAS 不确定状态不得丢失 owner/revision；重试串行、幂等且可观察；完整 bootstrap 成功才放行。
- 当前成员：需求分析、后端、QA、独立 Reviewer；每项工作依 `tasklist.md` 分派及记录。
- 停止条件：任何 owner/成交数量/终态冲突，数据不完整，账户状态不明，无法保证耐久写先于结算，或引入 deadlock/race/失去保护性减仓能力。
- 交付：Requirement Specification Card、架构设计、测试计划、代码与生产接线测试、独立评审、版本/变更记录及未闭合验收项。
