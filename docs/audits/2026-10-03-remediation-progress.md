# 实盘准备度整改进度续篇

历史记录见 [原整改进度](2026-09-24-remediation-progress.md)。此处继续原 R01–R15 范围，不代表范围缩减或真实盈利验收。

## rc940：借款调用中断的身份与待恢复意图

- 四种真实反向开仓隔离回归先复现 ACK 身份丢失：调用取消、运行所有权丢失、正 ID 同时返回错误、取消后保存失败。
- 正 ID 先保留本地；当前所有权仍有效时保存恢复元数据，取消后标记 UNKNOWN；已失效所有者不覆盖 durable。带错误 ACK 不查询、卖出、对冲或还款，不冒充已核实本金事件。
- 未核实借款时普通错误返回不再清除 in-flight 意图；只有债务确认并保存后才进入原有结束流程。
- 验证：相关 Funding Carry / 解码 / 关闭路径两轮 race 通过（50.578s）；最终新增断言及缺失身份/意图测试另两轮 race 通过；strategy vet、diff 检查、Ruby 门禁9项/58断言通过；前端 yarn verify 类型检查、44文件224测试及 Vite/PWA 构建通过。
- 十包报告 `/private/tmp/quantmesh-trading-race-rc940-final/results.json` 与 `results.md` 读回 1880 pass、8 MySQL skip、0失败、无缺包/解析错误；版本 rc940，source_commit=4817e36b、source_dirty=true。这是提交前源码回归，不是同提交严格 MySQL 验收，不能继承 rc926 验收。
- 原子世代 fencing、ACK 保存失败后的完整耐久接管、自动核账补偿仍未闭合；取消 context 下保存元数据不是重新获得钱包 lease。未连接真实账户或生产库，未下单、发布或验收盈利能力。
