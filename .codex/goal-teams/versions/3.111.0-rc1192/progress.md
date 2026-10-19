# RC1192 进度

- 基线：`origin/main` `1f1e8fac1b7978872def71f6252db135bf728672`（PR #9 已合入）；实现提交 `e41998ff7293fda84230e5199ea8ba591c965783`。
- 本地版本：`3.111.0-rc1192`。
- 已完成：收益报告 fail-closed 扩展到 raw fills、账户 scope 库存、稳定订单身份、逐订单 side/累计数量；权益未按日和净外部资金流勾稽时不显示“已核实”。全仓 Go 测试、关键包 race、vet/build、WebUI 318 项测试与生产构建通过。
- 未完成：逐订单费用归属、权益日账来源；FundingCarry `release_pending` 新 runtime 恢复；MySQL 专项（缺一次性测试 DSN及破坏性 schema opt-in）、同 SHA CI。
- Git：已在 `fix/profit-recovery-rc1192` 提交并推送；rebase 到上述 main。PR 尚未创建：当前 CLI 身份无 collaborator 权限，GitHub 返回 `must be a collaborator (createPullRequest)`；该分支目前无对应 Actions run。不得据此称 CI 通过或合并。
- 工作树：`--help/`、RC1084–RC1191 历史目录、`doc-capsules.jsonl` 等既有未跟踪材料仍保留，未纳入提交。
- 边界：没有真实账户访问、真实下单、发布或盈利验收。
