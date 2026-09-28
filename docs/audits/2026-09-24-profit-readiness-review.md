# QuantMesh 实盘准备度审查（2026-09-24）

## 结论与范围

审查基线：`main` / `495876b370a086f1b49cacb32fdc5159d0cccfd8`，源码版本 `3.111.0-rc1`。

**从交易软件验收角度，当前不宜通过无人值守实盘放行。** 主要障碍不是缺少策略，而是开仓限制、止损执行、成交账本和重启恢复之间仍有缺口。现有测试通过并不覆盖这些异常场景；回放结果也不能替代真实成交和账户权益对账。

本轮是审查，不是修复或上线：未修改交易源码、配置、凭据和版本；未提交、推送、部署，也未向真实交易所提交订单。新增的仓库文件仅为本报告。

范围包括：网格 LONG/SHORT、信号策略、DCA、多策略执行器、订单不确定性、仓位/资金恢复、全局熔断、Web 鉴权、CI/CD，以及现有回放的盈利验证边界。不是逐行覆盖全部适配器，也不是线上渗透或实盘收益审计。

证据等级：

- **复现**：独立 mock / httptest 场景验证了实际执行结果。
- **代码**：已定位调用链和失败条件，未执行完整故障注入或线上验证。
- **既有报告**：引用仓库内已提交的回放结果，本轮未重新运行历史回放。

优先级：P1 为相应功能投入实盘前应解决的问题；P2 为工程与盈利验证门槛。这里的优先级不表示已经发生真实损失。

## 验证结果

| 检查 | 结果 | 边界 |
|---|---|---|
| `go test ./... -timeout=120s` | 通过 | 本机 Go 1.26.3 / darwin arm64；不是 Linux 发版验收 |
| `yarn test`（webui） | 33 个文件、144 项通过 | 不是完整交易流程 E2E |
| `yarn exec tsc --noEmit` | 退出码 1，只显示用法 | webui 缺少 tsconfig.json；不能称为“发现了类型错误”或“类型检查通过” |
| 隔离异常场景测试 | 11 个顶层用例均触发安全断言失败 | 这些 FAIL 是缺陷复现，不是已修复的回归测试；其中一项包含 trend / mean_reversion 两个子例 |

隔离用例通过 Go overlay 注入，未把失败测试写进业务包。最终结果在临时目录：

- [复现日志（Markdown）](/private/tmp/quantmesh-audit-20260924.5pJm08/results.md)
- [复现结果（JSON）](/private/tmp/quantmesh-audit-20260924.5pJm08/results.json)
- [测试运行器](/private/tmp/quantmesh-audit-20260924.5pJm08/run_audit.rb)
- overlay 和四个测试源文件同目录；临时目录可能被系统清理，正式修复时应将用例整理进对应测试包。

复现命令（基于上述基线和仍存在的临时文件）：

```sh
go test -overlay=/private/tmp/quantmesh-audit-20260924.5pJm08/overlay.json ./position ./risk ./strategy ./web -run '^TestAudit' -count=1 -timeout=60s
```

本轮未运行 race 检查、真实订单故障注入、重启端到端测试、目标 Linux 发布构建或真实账户权益对账。

## P1：投入相应实盘功能前修复

### R01：资金费率与趋势联动可以覆盖硬暂停

- 证据：**复现 + 代码**。
- 位置：[super_position_manager_adjust.go:328](/Users/rocky/Sites/btc/quantmesh-opensource/position/super_position_manager_adjust.go:328)、[同文件:461](/Users/rocky/Sites/btc/quantmesh-opensource/position/super_position_manager_adjust.go:461)。
- `skipBuying` 同时承载运行时暂停、价格边界、仓位上限、开仓计划和趋势限制。后面的“有利资金费率 + 顺势”分支无条件把它改回 false，注释所说“只放宽趋势过滤”与实际行为不符。
- 复现：运行时暂停标志已为 true，LONG 顺势且资金费率偏向系数 1.2，仍提交新开仓单。
- 影响：操作员/风控以为开仓已停止，但后续价格更新仍可能增加风险；同一布尔值中的其他硬约束也存在被覆盖的路径。
- 改进：硬限制和策略偏好分开，硬拒绝不可被后续加分/放宽规则解除；最终执行器再校验一次。
- 验收：手动暂停、熔断暂停、仓位数量/价值/层数上限、价格边界分别与有利费率交叉测试；任何硬限制下新增开仓数为 0，减仓仍可执行。

### R02：全局暂停没有覆盖非网格策略执行路径

- 证据：**策略层复现 + 调用链代码**，未运行真实熔断到交易所的全链路。
- 位置：[bot_manager.go:909](/Users/rocky/Sites/btc/quantmesh-opensource/bot_manager.go:909)、[multi_strategy_executor.go:241](/Users/rocky/Sites/btc/quantmesh-opensource/strategy/multi_strategy_executor.go:241)。
- `BotRuntime.PauseOpening` 更新展示配置并通知 SPM，没有给多策略执行器提供统一的运行时开仓拒绝状态；执行器只有资金预留等检查。信号策略仍可接收价格并下单。
- 复现：均值回归策略配置 `PauseOpening=true`，输入价格 100、100、90，仍提交 1 个开仓订单。
- 改进：所有策略都必须经过同一个开仓风控入口，不能依赖每个策略自行读取配置。
- 验收：对每一种注册策略触发全局暂停，确认开仓全部被拒绝，reduce-only 平仓不受阻；覆盖在途下单与暂停同时发生。

### R03：开仓门槛和行情风控提前返回，连已有仓位的止损也跳过

- 证据：**触发价路径复现 + 行情风控路径代码**。
- 位置：[super_position_manager_adjust.go:42](/Users/rocky/Sites/btc/quantmesh-opensource/position/super_position_manager_adjust.go:42)、[symbol_manager.go:1047](/Users/rocky/Sites/btc/quantmesh-opensource/symbol_manager.go:1047)。
- 触发价检查在持仓止损之前；行情/深度风控触发后直接 `continue`，不再调用策略价格处理和网格调整。
- 复现：已有 SHORT 入场价 40,000、触发价 50,000、现价 45,000、止损阈值 5%，没有提交平仓单。适用于恢复既有仓位或调整配置等场景，不能只按“空仓首次启动”理解触发价。
- 改进：把“管理既有风险”和“允许新增风险”分成两条流程。行情异常可禁开仓，但不应静默停掉持仓保护；行情不可用时要有独立降级策略。
- 验收：恢复持仓、跌破/越过触发价、深度异常、行情风控触发期间，止损检查和减仓状态机仍运行。

### R04：内部网格止损仍是提交限价单，不是确认风险解除

- 证据：**复现 + 代码**。
- 位置：[super_position_manager_adjust.go:91](/Users/rocky/Sites/btc/quantmesh-opensource/position/super_position_manager_adjust.go:91)、[super_position_manager_reconcile.go:267](/Users/rocky/Sites/btc/quantmesh-opensource/position/super_position_manager_reconcile.go:267)、[liquidation_price.go:96](/Users/rocky/Sites/btc/quantmesh-opensource/position/liquidation_price.go:96)。
- 内部止损调用 `LiquidateAll()` 后返回；已有的 `LiquidateAllVerified` 没有覆盖这条路径。限价保护区间可能使卖出止损价高于当前买盘。
- 复现：最近价格 50,000、盘口 best bid 47,000，止损 SELL 限价为 49,500，不能立即吃到买盘。盘口与最近价格偏离代表快速行情/价格滞后条件，不是普通平稳盘口。
- 影响：发出止损通知或下单成功不代表仓位已平；后续回调可能推进，但本路径没有完成期限或可靠兜底。
- 改进：异步、去重的平仓状态机，具备撤单/追踪/期限/允许的 IOC 或市价降级，以及最终仓位核验。不能直接在持有 SPM 全局锁的 tick 路径塞阻塞式验证。
- 验收：盘口跳空、部分成交、拒单、回报丢失时，最终进入“已确认平仓”或显式告警/人工处置状态，而不是把已挂单当成完成。

### R05：三处成交账本错误会污染收益和风控判断

证据：以下三项均已**复现**。

| 问题 | 实际结果 | 位置 |
|---|---|---|
| SHORT 已实现盈亏使用多头公式 | 卖出开仓 100、买回 90、数量 1，保存 PnL 为 -10，应为 +10（手续费另计） | [order_events.go:280](/Users/rocky/Sites/btc/quantmesh-opensource/position/super_position_manager_order_events.go:280) |
| 累计成交均价被当作新增成交均价再次加权 | 先成交 0.5、均价 100，最终累计 1、均价 110，内部记成 105 | [order_events.go:112](/Users/rocky/Sites/btc/quantmesh-opensource/position/super_position_manager_order_events.go:112) |
| DCA 在平仓订单提交时就记录交易/收益 | 入场 100、平仓挂单 110，尚无成交，TotalPnL 已是 10、TotalTrades 已是 1 | [dca_enhanced.go:865](/Users/rocky/Sites/btc/quantmesh-opensource/strategy/dca_enhanced.go:865) |

- Binance 推送的均价和数量都是累计字段，见 [websocket.go:474](/Users/rocky/Sites/btc/quantmesh-opensource/exchange/binance/websocket.go:474)，不是测试凭空构造的字段语义。
- 全局风控从本地 trades 取 `PnL - Fee`，见 [main_helpers.go:730](/Users/rocky/Sites/btc/quantmesh-opensource/main_helpers.go:730)。保存了交易所 PnL 字段也不会自动纠正这个读取口径。
- 改进：方向感知、按真实成交增量记账；用累计成交金额差或逐笔成交处理成本；订单接受与交易实现严格分离，费用单独入账。已存在的历史记录需要对账后重算，不能只修未来代码。
- 验收：LONG/SHORT/BOTH 空腿、分批开平仓、撤单、重复/乱序回报、真实手续费币种转换；交易所成交账与本地账逐笔吻合。

### R06：趋势/均值回归部分成交后撤单，会丢掉已经成交的仓位

- 证据：**两个策略均复现**。
- 位置：[trend_following.go:357](/Users/rocky/Sites/btc/quantmesh-opensource/strategy/trend_following.go:357)、[mean_reversion.go:320](/Users/rocky/Sites/btc/quantmesh-opensource/strategy/mean_reversion.go:320)。
- 只在 FILLED 时更新持仓；CANCELED 等终态先清掉 activeOrder/pendingAction。
- 复现：订单成交 0.4 后撤销剩余部分，两个策略的 `GetPositions()` 都返回空。
- 影响：实际交易所仍有仓位，策略却按空仓继续决策；也会丢失相应的退出管理。
- 改进：每次累计成交增长都更新仓位，撤单只结束未成交部分。平仓部分成交也必须同步减少持仓。
- 验收：开/平仓部分成交后撤销、过期、重复回报、重启，策略持仓均与交易所一致。

### R07：订单结果未知被降为普通失败，资金预留和槽位被释放

- 证据：**代码**，未注入真实网络故障。
- 位置：[executor_adapter.go:325](/Users/rocky/Sites/btc/quantmesh-opensource/order/executor_adapter.go:325)、[同文件:346](/Users/rocky/Sites/btc/quantmesh-opensource/order/executor_adapter.go:346)、[super_position_manager_adjust.go:1040](/Users/rocky/Sites/btc/quantmesh-opensource/position/super_position_manager_adjust.go:1040)、[multi_strategy_executor.go:300](/Users/rocky/Sites/btc/quantmesh-opensource/strategy/multi_strategy_executor.go:300)。
- 下单重试耗尽后虽然会按 ClientOrderID 回查，但“确定不存在”和“查询也失败”都落到 nil。只查挂单的回退无法找回已经完全成交的订单。
- 上层把这些情况作为失败释放资金/槽位；若交易所实际受理但回包丢失，后续新 ID 重下有重复敞口风险。
- 改进：显式 UNKNOWN 状态并持久化，保留占用，稳定使用同一个幂等键，通过成交历史和订单查询补偿；只有确认未受理才能释放。
- 验收：交易所受理后断网、查单超时、瞬时全成、重复 ID、重启恢复。要求同一个交易意图最多形成一次目标敞口。

### R08：信号策略 ClientOrderID 经返佣前缀截断后发生碰撞

- 证据：**复现**。
- 位置：[signal_trade_helpers.go:198](/Users/rocky/Sites/btc/quantmesh-opensource/strategy/signal_trade_helpers.go:198)、[utils/orderid.go:319](/Users/rocky/Sites/btc/quantmesh-opensource/utils/orderid.go:319)。
- ID 由策略名、动作、纳秒时间组成；Binance 前缀后超过长度时从尾部截断，丢掉用于区分请求的低位时间部分。
- 复现：两个不同的 `trend_open_long_...` 纳秒 ID 经前缀处理后相同。
- 改进：固定长度编码/哈希或可靠序列，长度预算在生成时一次完成；路由保留稳定的内部 ID 与交易所 ID 映射。
- 验收：多策略并发、大量连续生成、长名称、各交易所最大长度，以及已成交订单之后再次下单，均不碰撞、不误归属。

### R09：非网格策略重启恢复只有订单路由，缺少完整经济状态

- 证据：**代码**，尚需重启 E2E。
- 位置：[symbol_manager.go:997](/Users/rocky/Sites/btc/quantmesh-opensource/symbol_manager.go:997)、[multi_strategy_executor.go:572](/Users/rocky/Sites/btc/quantmesh-opensource/strategy/multi_strategy_executor.go:572)，以及 DCA、martingale、trend、mean_reversion 构造/启动逻辑。
- 当前恢复 NEW/PARTIALLY_FILLED 订单到策略路由，但 `RestoreOrderRoute` 不恢复订单资金占用、已成交持仓资本、DCA 层数/成本或信号策略 position。
- SPM 自身已有持仓恢复，不等于所有独立策略内部状态都得到恢复。
- 影响：重启后重新按首单/空仓决策、策略资金重新可用、退出条件丢失等风险。
- 改进：持久化策略检查点和成交事件，启动先恢复，再向交易所核账；不一致时只允许减仓/人工处理，不直接 `StartAll` 放开新交易。
- 验收：有持仓、挂单、部分成交、平仓中和进程异常退出五种状态重启，状态/资本/下一步动作保持一致。

### R10：回撤只在初始化取真实权益，遗漏未进入 trades 的成本

- 证据：**模拟权益变化复现 + 数据来源代码**。
- 位置：[risk/metrics_feeder.go:275](/Users/rocky/Sites/btc/quantmesh-opensource/risk/metrics_feeder.go:275)、[main_helpers.go:730](/Users/rocky/Sites/btc/quantmesh-opensource/main_helpers.go:730)。
- 第一次获取账户权益后，持续使用“基准权益 + 本地已实现收益 + 浮盈亏变化”，不持续读取权益源；本地源是交易记录而非完整资金流水。
- 复现：权益源从 1,000 变为 800，本地 trades/浮盈亏不变，回撤仍为 0%，权益接口只调用一次。
- 解释边界：这不能证明任意余额下降都应计为亏损；充值/提现应剔除。但如果变化来自资金费、借息、强平相关扣款或漏记成交，当前链路同样看不见，可能漏掉熔断。
- 同时，基准/高水位在内存中，重启后重建；运行时权益源只统计 futures，spot 不能借用“全局权益回撤已覆盖”的说法。
- 改进：实际权益定期核对 + 完整资金流水分类 + 持久化高水位；入出金做现金流调整，缺失/陈旧数据显式告警。
- 验收：资金费、利息、手续费、充值、提现、外部平仓、重启分别验证，既不漏损也不把提现误当亏损。

### R11：多个连接共用一个断线计时，一个重连可清掉另一个故障

- 证据：**代码**，未运行多连接故障注入。
- 位置：[risk/circuit_breaker.go:628](/Users/rocky/Sites/btc/quantmesh-opensource/risk/circuit_breaker.go:628)、[main_helpers.go:825](/Users/rocky/Sites/btc/quantmesh-opensource/main_helpers.go:825)。
- 全局只保存单个 `lastWSDisconnect`；任意 Reconnected 清零。主动 Stopped 也被映射为 Reconnected。
- 影响：连接 A 持续断开，B 重连或正常停止，可以抹掉 A 的故障计时，多 Bot/多账户部署尤其需要验证。
- 改进：按交易所、账户、数据流身份记录健康状态；明确已停止连接的注销语义，不能模拟全局恢复。
- 验收：A 断线/B 重连、A 断线/B 停止、重复断线通知、鉴权失败，熔断时钟都正确。

### R12：仓位层数上限是软限制，不包含挂单潜在成交

- 证据：**代码 + 既有回放报告**，本轮没有重跑回放。
- 位置：[super_position_manager_adjust.go:390](/Users/rocky/Sites/btc/quantmesh-opensource/position/super_position_manager_adjust.go:390)、[2026-09-17 回放报告:91](/Users/rocky/Sites/btc/quantmesh-opensource/docs/reports/2026-09-17-replay-calibration.md:91)。
- 限制主要检查当前已成交仓位，没有以“持仓 + 在途/未成交开仓单的最坏成交量”统一预留风险额度；接近层数上限时批量挂单也可能超过剩余额度。
- 既有报告已记录：20 层 × 150 USDT 的名义额度 3,000 USDT，最大实际持仓名义达 7,450 USDT。报告明确指出存量挂单继续成交和软上限语义。
- 改进：将目标层数与硬风险上限分开命名；硬上限涵盖已成交、挂单、结果未知订单，采用原子预留。撤单确认前不能归还额度。
- 验收：只剩一层额度时批量请求、多策略并发和行情快速扫单均不突破硬上限；界面不得把软目标展示为资金安全保证。

### R13：Web 鉴权在开发模式和初始化异常路径存在放行风险

- 证据：开发模式为**复现**；初始化异常路径为**代码**。
- 位置：[auth_middleware.go:81](/Users/rocky/Sites/btc/quantmesh-opensource/web/auth_middleware.go:81)、[api_setup.go:92](/Users/rocky/Sites/btc/quantmesh-opensource/web/api_setup.go:92)、[password_manager.go:188](/Users/rocky/Sites/btc/quantmesh-opensource/web/password_manager.go:188)。
- 开启 `local_dev_mode` 后直接跳过认证，没有验证请求来自回环或服务只绑定本地。复现中来自非回环测试地址、无 Cookie 的请求进入受保护 handler。
- `/api/setup/init` 是公开路由，由 handler 自行判断认证；当 HasPassword 查询出错，或相关管理器为空时，认证检查可以被跳过。HasPassword 的“已安装但数据库错误”返回 true + error，也会错过 `err == nil && hasPassword` 分支。
- 这不是“生产当前已开启开发模式”或“已有攻击”的结论；需要相应配置/故障与网络可达性才会形成可利用条件。本轮未覆盖真实配置。
- 改进：生产模式拒绝免认证启动，开发免认证强制本地绑定且考虑反向代理边界；初始化认证遇错误应拒绝操作，只允许显式首次设置凭证。
- 验收：外网请求、代理请求、数据库不可用、已安装标记存在、管理器未初始化均不能匿名修改交易配置。

## P2：工程与盈利验证

### R14：测试和发版门槛还不足以阻止上述问题上线

- 位置：[webui/package.json:8](/Users/rocky/Sites/btc/quantmesh-opensource/webui/package.json:8)、[ci.yml:53](/Users/rocky/Sites/btc/quantmesh-opensource/.github/workflows/ci.yml:53)、[ci.yml:149](/Users/rocky/Sites/btc/quantmesh-opensource/.github/workflows/ci.yml:149)、[cd.yml:23](/Users/rocky/Sites/btc/quantmesh-opensource/.github/workflows/cd.yml:23)。
- Vite build 不是 TypeScript 类型检查；目前缺 tsconfig 和 typecheck 脚本。现有前端单测通过不能覆盖创建 Bot、暂停、紧急平仓、参数热更新的完整交互。
- CI 测试后构建，但 CD 的 tag/manual 路径独立进行 build/release，没有同一提交通过测试的强制依赖；不能仅靠“通常会先跑 CI”。
- CI 以 `git describe --tags --always --dirty` 注入后端版本，浅检出时可能得到 hash，与前端包语义化版本不同。这里是构建路径风险，不是声称当前源码版本已经不一致。
- 建议：统一版本来源；API header/后端 `/version`/前端展示一致性断言；关键交易包 `-race`；把本轮异常场景纳入门禁；发布必须消费同一 SHA 的通过产物；增加最小交易控制 E2E 和重启恢复演练。

### R15：现有回放证明了部分工程行为，没有证明稳定净盈利

- 证据：**既有报告审阅**，不是新的策略收益测算。
- 位置：[2026-09-17 回放报告:22](/Users/rocky/Sites/btc/quantmesh-opensource/docs/reports/2026-09-17-replay-calibration.md:22)、[同报告:69](/Users/rocky/Sites/btc/quantmesh-opensource/docs/reports/2026-09-17-replay-calibration.md:69)。
- 当前主要样本是 2 个交易对、89 天，上涨和震荡为主，缺持续下跌；三个分段不是独立市场样本。
- K 线内价格路径是假设，没有真实队列、盘口与下单/撤单延迟；100% maker 是模型结果而非实盘保证；9 月资金费存在近似。
- 仓库已有 fee-aware、PostOnly、同构回放和相关校准工作，方向是有价值的；不能因为有这些机制就外推盈利，也不能因为本次缺陷就断言所有策略没有价值。
- 下一步应以真实成交核账为基础：逐单对照回放和执行；补充下跌、跳空、高资金费、流动性退化样本；做时间隔离的样本外验证、滚动验证和参数邻域稳定性检查。
- 经营看板至少区分：已实现收益、未实现损益、交易费、资金费、借息、执行滑点/冲击、运行成本、现金流调整后权益回撤。滑点用于解释实际成交与参考价差异，不能在已按实际成交价算的 PnL 中重复扣除。
- 按 Bot / 策略 / 市场状态分拆净贡献和风险占用，不以交易次数、已实现网格收益或漂亮胜率代替账户净权益。修复与对账完成后，再由负责人批准是否进入受限的真实环境验证；本报告不提供收益承诺或仓位建议。

## 建议实施顺序与放行标准

1. **统一风险约束**：先做 R01、R02、R03、R04、R12，使暂停不可覆盖、持仓保护不断档、硬额度真正可执行。
2. **建立可信成交账本**：R05、R06、R07、R08、R10，所有资金/仓位变化由可幂等的真实成交和现金流水驱动；同时核对历史账。
3. **完成故障恢复与控制面安全**：R09、R11、R13，重启与连接异常默认不能放大风险，鉴权异常默认拒绝写操作。
4. **固化验收和盈利证据**：R14、R15，测试门禁、目标环境构建、模拟故障/重启演练、真实记录校准和样本外验证缺一不可。

放行应有可核验的证据，而非只把本报告状态改成“已完成”：

- 本轮复现用例修复后通过，且覆盖其他方向、策略和边界组合。
- 订单账、策略仓位、资金占用与交易所记录逐项一致；不确定订单不释放风险额度。
- 所有暂停/熔断仅阻止开仓，不破坏减仓；任何放宽规则不得越过硬限制。
- 故障和重启中不重复下单、不丢已成交仓位、不清掉应保留的回撤历史。
- 受保护写接口在生产配置及依赖故障时均拒绝匿名请求。
- 同一提交在目标构建环境通过测试/构建，前后端版本一致；盈利判断基于现金流调整后的净权益与完整成本。

**本报告不代表已经完成上述修复，也不保证修复后策略一定盈利。软件正确性是讨论策略优势之前的必要条件。**
