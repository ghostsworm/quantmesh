# RC1192 任务清单

## 1. 收益口径与报表

- [x] 修复跨自然月周收益窗口。
- [x] 枚举 raw fills，检测孤立成交、缺少 raw fills 的 trade、未估值费用及交易手续费总额差异。
- [x] 枚举数据库已存账户 scope；未归档或无归属历史使账户历史完整标志失败。
- [x] 无有效买卖订单号的 trade 使 execution coverage 失败。
- [x] 权益未与净外部资金流做日级勾稽时，累计和日/周/月收益“已核实”标志 fail-closed。
- [x] 按订单核对 raw fill side 与配对 trade 累计数量；方向/数量不符不能通过 execution coverage。
- [ ] 逐订单手续费归属核对，防止账户总费用相抵后掩盖单订单错记；策略归属尚需更强的 producer/source evidence。
- [ ] 正式实现持久化日级权益与外部资金流勾稽及差异解释。

## 2. FundingCarry 人工核账与解锁

- [x] 已有生产预览/确认、精确 Gate 清理、持久 receipt/CAS/readback 路径及状态响应修正。
- [ ] 解决 crash 后 `release_pending` 由同 scope 新 runtime 显式审计接管/续办的 dead-end。
- [ ] release CAS 并发、Gate 清理失败、重启/多句柄、重复操作及部分成功故障注入。
- [ ] 证明持久 hold 与其他 manual/risk Gate 在所有失败路径保持，且不自动重放订单。

## 3. 策略验收与交付

- [x] 保留“未证明盈利”的策略验收报告及数据/成本假设缺口。
- [ ] 固定最终代码、配置和数据摘要，补独立样本外与恶劣市场/执行成本敏感性结果。
- [x] 全仓 Go、关键包 race、vet、构建、前端验证。
- [ ] 隔离 MySQL 与同 SHA 远端 CI（本机缺一次性 DSN/破坏性 schema opt-in；远端需提交后验证）。
- [ ] 仅 stage 本次明确文件；rebase 最新 `origin/main`、push 并通过正式 PR CI 后合并。无 CI 绿灯不合并/发布。
