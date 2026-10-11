# RC1159 验收基线

## A. 代码与安全闭环

- [ ] RC1158 `BotState.updated_at` 时间参数以 MySQL 原生时间类型写入，MySQL driver `time.Time`、DATETIME/TIMESTAMP 读回正确，SQLite RFC3339 兼容；真实隔离 MySQL round-trip 必须执行且不可 skip。
- [ ] 生产策略清单逐项对应 durable owner/order identity、完整 fills、币种化 fees、cursor/revision、幂等 reconciliation receipt 和重启读回。
- [ ] 任何策略能力缺项时明确 unsupported，订单隔离和相关开仓门持续封锁；不得做伪全覆盖。
- [ ] 核账案、操作、证据摘要、reconcile receipt、操作者与 gate 状态均耐久。崩溃任意阶段重启后只能继续幂等状态机，不会先开 gate 后补审计。
- [ ] 管理员鉴权、scope、一次性确认/幂等键及 TOCTOU revision 服务端校验；不信任浏览器提交的成交或账务成功声明。
- [ ] 解锁只释放准确 order/owner 的 quarantine source，不影响 manual/risk/startup/unknown 等其它 source；不开新仓，不重放回调；保护性 reduce-only close 仍可走。
- [ ] 手续费币种、零手续费知识状态、base-asset fee、部分成交、资金费/借贷成本和 UNKNOWN 均有显式语义；未知仍拒绝解锁。
- [ ] 前端两阶段操作完整 i18n，状态来自服务端，隐藏/禁用条件只是 UX，后端负责最终校验。

## B. 同源代码测试

- [ ] RC1159 源码指纹固定；`go test ./...`、相关包 `-race`、`go vet ./...`、MySQL 强制验收均在该指纹通过，明确报告有意跳过项。
- [ ] `yarn typecheck`、`yarn test`、构建/验证脚本通过。
- [ ] 独立 QA 和安全 Reviewer 对最终相同 diff 留下可追溯结论；剩余每个 unsupported 策略均有明示。

## C. 盈利证据

- [ ] 报告输入绑定 commit/version、策略配置 hash、交易所/账户范围标识（脱敏）、区间、数据导入时间和原始来源 hash。
- [ ] 报告可从逐笔成交重算成交额、手续费（含资产币种）、借贷利息、资金费、已实现/未实现 P&L、余额/权益差异、滑点和策略归因。
- [ ] 数据缺页/重复、费用未知、外部资金流/人工仓位无法归属时标为 incomplete，不从价格回测或 UI 总收益推断已实现盈利。
- [ ] 仿真/fixture 仅验证算法与完整性规则。没有用户提供/授权的真实账户账单，不得通过“真实盈利验收”。

## D. 交付状态

- [ ] 报告未提交/已提交/已推送/tag/CI/CD/发布资产分别状态；同提交 CI 和最终产物 readback 才能称发布验证完成。
- [ ] 未获明确授权不得提交、推送、发布、部署、连接真实账户或下单。
