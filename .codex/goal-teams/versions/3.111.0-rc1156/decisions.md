# Goal Teams Decisions

| 时间 | 决策 | 原因 | 决策人 | 影响范围 |
|---|---|---|---|---|
| 2026-10-09 | 确认按 rc1156 团队分工推进启动订单恢复闭环 | 用户明确回复“我确认了，你继续” | 用户 | 按 plan.md 分派需求分析、后端、独立 QA 与安全评审；实现仍受 tasklist 门槛约束 |
| 2026-10-10 | 维持 fail-closed；不因本地订单终态标签单独解除恢复封锁 | 终态必须由 venue 精确复查，且相关策略/成交账本须先持久化 | Goal Lead（依已确认计划的安全不变量） | 所有订单类型及异常恢复路径 |
| 2026-10-10 | 不自动撤销启动时发现的活动委托 | 归属、已成交部分、剩余数量和策略资金不能由普通 open-order 快照完整推断 | Goal Lead（依已确认计划的安全不变量） | 启动活动订单接管范围；若需求分析发现需要额外业务决策，则停止询问 |
| 2026-10-10 | 将 normal durable settle 后 executor map 残留视为本切片首要 P1，禁止仅补 retry callback | 独立评审沿生产调用链证实 `SettleIntent` 留下 settled map 项，完整 bootstrap rebind guard 因 map 非空失败；recovery-only helper 会删项而造成测试代表性缺口 | Goal Lead（独立源码审查结论） | 先定义已结算 CID 的耐久回收/刷新语义；不得放宽未结 intent 防重绑检查 |
| 2026-10-10 | 活动 owner 订单仍阻止 ExposureBook Seed，直到终态和完整账户核账 | 当前没有统一、耐久、可重建的活动订单风险预留模型 | Goal Lead（维持原 fail-closed 计划边界） | 不接管活动单、不估算剩余风险、不自动撤单 |
| 2026-10-10 | 正成交/部分成交链调整为 fills/fees durable first，再策略经济账、intent settlement 与完整 bootstrap | 当前 callback 的策略账先于异步 fill capture；用户明确确认纳入本切片 | 用户 | backend/QA 覆盖全部正数量累计更新与 terminal 收敛；若无法避免策略响应/保护性减仓退化则暂停并报告 |
