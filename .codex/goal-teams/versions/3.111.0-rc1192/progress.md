# RC1192 进度

- 工作基线：`c52902380e5126de8fadaed7d5a57688bb6627a1`（开发分支尚需 fetch/rebase 到最新 `origin/main`）。
- 本地版本：`3.111.0-rc1192`。
- 已完成：收益报告 fail-closed 扩展到 raw fills、账户 scope 库存、稳定订单身份、逐订单 side/累计数量；权益未按日和净外部资金流勾稽时不显示“已核实”。全仓 Go 测试、关键包 race、vet/build、WebUI 318 项测试与生产构建通过。
- 未完成：逐订单费用归属、权益日账来源；FundingCarry `release_pending` 新 runtime 恢复；MySQL 专项（缺一次性测试 DSN及破坏性 schema opt-in）、同 SHA CI。
- Git：本记录生成时未提交、未推送、未合并、未打 tag。`--help/`、历史版本目录和既有报告等未跟踪内容保留，不应使用 `git add .`。
- 边界：没有真实账户访问、真实下单、发布或盈利验收。
