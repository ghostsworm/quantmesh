# 实盘准备度复查：3.111.0-rc3 当前工作树

日期：2026-09-24。分支：`codex/profit-readiness-remediation`。基线 HEAD：`495876b370a086f1b49cacb32fdc5159d0cccfd8`。审查对象包括已有未提交整改，不是仅审查 HEAD。

## 结论

**当前不满足无人值守实盘放行条件，也没有足够证据证明可持续净盈利。** 优先工作应是让风险上限真正约束下单、让订单与账本可恢复、让界面如实报告结果，而不是继续叠加策略功能。

本轮重新检查交易准入、Bot 风控配置、退出/手工平仓、成交落库、重启恢复、权益来源、鉴权边界、前端测试、CI/CD 和已有盈利校准证据。没有修改业务源码、配置或版本，没有查找凭据、访问真实账户、下单、提交、推送、部署或操作生产数据库。仅新增本报告和仓库外临时模拟测试。

证据分级：**复现**为本轮本地模拟故障/竞态测试；**代码**为当前调用链检查；**历史报告**为已存在的回放资料，本轮未重新回放。不是所有交易所和策略的逐行穷尽审计，也不是线上渗透测试或 Linux 发布验收。

## P1：真实资金放行前必须修复

### A01：遗留手工平仓 API 将“已接受”误报为“已平仓”【复现】

- 位置：`main_helpers.go:635`，`main.go:537`，`web/api.go:1722`。
- `PlaceOrder` 返回 nil error 就增加 successCount，不核验订单终态、成交量或残余仓位。上层将其描述为“平仓完成”，并返回 HTTP 200。
- 本轮模拟市价单返回 NEW、没有成交且仓位仍为 1；实际得到 `success=1 failed=0 remaining=1 err=nil`。安全断言失败。
- 风险：操作员以为敞口已经消失，实际仍承担行情风险。此问题是仍被 API 调用的旧路径，不能因新进程退出平仓路径已修复而忽略。
- 改进：统一核实型平仓状态机；明确 submitted/partially_filled/confirmed/unknown/failed，不由接单 ACK 推导成交。
- 验收：接单未成交、部分成交、查询失败、取消未确认、超时及迟到成交都必须如实返回；查询确认仓位与成交结清后才能报告完成。

### A02：Bot 风控配置没有形成保存、执行、展示的一致状态【复现 + 代码】

- 启动：`symbol_manager.go:372` 从全局配置构造 localCfg；该流程复制 GridRiskControl，却没有把 Bot 的 OpenPositionControl 复制到实际 SPM 配置。
- 热更新：`bot_manager.go:879` 仅替换 br.Config 指针，不更新 SPM 的实际准入配置；真实限仓读取 `position/super_position_manager_adjust.go:340`。
- 假持久化：`web/api_bot_risk_control.go:295` 保存失败仅告警，`:326` 固定返回 persisted=true；`:391` 管理器缺失、找不到 Bot 等情况也可能返回 nil。本轮在没有持久化管理器时复现 HTTP 200、persisted=true。
- 并发：GetBotRiskControl 无锁返回共享指针，setter 持另一侧写锁。本轮 `-race` 在 `bot_manager.go:875` 与 `:886` 之间检测到 DATA RACE。API 合并请求还会直接修改 getter 返回对象。
- 口径：执行器用名义仓位价值，`bot_manager.go:1058` 和 `position/opening_controller.go:165` 用保证金比较同一个 MaxPositionValue；前端 `webui/src/components/BotRiskControlPanel.tsx:475` 也把保证金与该上限并列。假设 10 倍杠杆、名义仓位 1000、上限 500，执行层认为超限而状态页会按保证金 100 认为未超限。
- 网格配置的另一 setter `position/super_position_manager_reconcile.go:509` 直接修改共享配置，没有与 AdjustOrders 的读锁范围形成一致同步；本轮只对 Bot getter/setter 的竞态做了动态复现，不声称其他路径已逐个复现。
- 改进：使用不可变配置快照和明确配置版本，原子发布到真实执行层；API 分开报告 applied/persisted，失败不假报成功；统一名义价值口径，保证金独立显示。
- 验收：API 修改上限后，实际下单立即受约束；保存失败/重启后回读正确；并发读取更新通过 race；各方向和杠杆下状态页与执行判定一致。

### A03：在途订单硬额度组件尚未接入生产【代码】

- 位置：`order/owned_intents.go:47`、`order/exposure.go:13`、`symbol_manager.go:554`。
- 额度预留只在 exposureBook 非 nil 时执行。全仓 SetExposureBook 的调用只在测试中，生产构造器没有绑定额度账本、恢复旧订单/旧仓位或喂入估值行情。
- 当前网格限制只统计 filled 槽位（`position/super_position_manager_adjust.go:349`）；已经挂出的开仓单继续成交时，不能保证最坏成交敞口不超过配置上限。
- 历史校准报告 `docs/reports/2026-09-17-replay-calibration.md:91` 已记录软目标 3000 USDT、实际最大名义持仓 7450 USDT。它是历史场景证据，不是本轮重新测出的数字。
- 改进：物理下单前统一原子核算“已成交 + 未成交开仓余量 + UNKNOWN”，覆盖全部策略；恢复核实前拒绝新增风险；撤单 ACK 不释放额度。
- 验收：真实生产构造链下的并发开仓、部分成交、撤单超时、重启恢复均不会绕过准入预算。市价波动本身可能抬升已有仓位名义价值，必须与新增风险准入上限区分。

### A04：重启缺少完整订单、策略和资金恢复，且路由恢复归属过宽【代码】

- `order/owned_intents.go:33` 的意图和 UNKNOWN 保存在进程内 map，未形成提交前持久化与自动恢复闭环。
- `symbol_manager.go:1023` 只查 NEW/PARTIALLY_FILLED，各 2000 条、无分页；查询失败告警后仍继续 StartAll。
- `storage/sql_storage_orders.go:357` 不按 Bot/账户过滤；调用方未校验返回记录 BotID/Account。相同交易对多 Bot/账户的历史记录可能被恢复到错误运行时。
- `strategy/multi_strategy_executor.go:629` 只绑定订单路由，不恢复完整持仓成本、DCA 层数、累计成交游标和资本预留。
- 改进：持久意图日志、幂等成交事件与检查点；按账户/Bot/策略严格归属；分页核查；恢复不完整时保持新增风险阻断，保护性减仓仍可使用。
- 验收：在提交前后、接单 ACK 前后、部分成交、写库中断等位置模拟进程退出，重启后不重复开仓、不丢仓、不重复记账；超过 2000 条及同交易对多 Bot 数据互不污染。

### A05：成交落库失败没有可靠补偿，净收益和部分风控输入可能漏账【代码】

- `position/super_position_manager_order_events.go:347` 等三条保存路径失败仅告警，之后仍扣减费用、清理终态订单身份/游标并释放槽位。
- `main_helpers.go:661` 的日内成交收益输入来自 trades。漏写不仅影响看板，也会使依赖这些成交的日内收益/连续亏损计算不完整。
- 手续费异步补查不能替代成交事件可靠保存；不能假定重复回报会补偿已经推进的本地游标。
- 改进：可靠成交事件日志/事务 outbox、逐笔幂等身份、可重试入账和对账报警；真实手续费币种估值、资金费、借息和历史账更正应可追溯。
- 验收：临时数据库写失败、进程退出、重复/乱序成交后，恢复账本与交易所证据一致；“仓位已平”和“财务已结清”分别验收。

### A06：整体退出仍先扫撤账户委托，后停止策略【代码】

- `main.go:2715` 的独立 CancelOnExit 调用交易对级 CancelAllOrders，会影响同账户同交易对其他 Bot/人工委托。
- 进程平仓后才在 `main.go:2735` 停止运行时；策略与行情循环在此前仍可能提交新单，整体停机协调未闭合。
- 遗留手工平仓同样在 `main_helpers.go:603` 扫撤全部委托，撤单失败后仍继续提交平仓。
- 改进：先统一封锁新增开仓并排空在途提交，再按已证明归属撤单/平仓，确认或保留 UNKNOWN 后停止数据流；全账户清仓需独立、明确的操作语义。
- 验收：退出期间行情持续更新、人工委托共存、撤单迟到、平仓超时等场景下，没有新开仓、误撤其他订单或不明状态下重复补单。

### A07：权益核账有明确支持边界，但账户生命周期尚未闭合【代码，支持范围限制】

- `main_equity_observation.go:33` 按运行中 Bot 列表确定账户集合；停掉某账户最后一个 Bot 会改变范围。
- 当前 ReadAccountEvidence 的交易所实现只有 Binance，且 `exchange/binance/equity_snapshot.go:62` 限定 USDT 单资产合约。现货、多资产与其他交易所不具备相同的核账证据。
- 数据不完整/范围变化会保守拒绝并保持权益健康门控，这是安全保护，不能作为缺陷去直接解除。缺少的是支持矩阵、稳定账户清单和受控迁移/修复流程。
- 改进：配置账户集合与 Bot 生命周期解耦；明确凭据轮换、账户增减、历史流水更正和归档恢复流程；未核实状态在 UI 上显式显示。
- 验收：Bot 启停不意外重建回撤基准，账户范围变更必须可审计迁移，不支持的模式不能伪装为已核账。

## P2：发布可靠性与盈利证据

### A08：发布不强制消费同一提交的测试结果，RC 也可能标成正式版【代码】

- CI 有测试，但 CD 独立运行；`.github/workflows/cd.yml:186` 的 release 只依赖 build，没有同 SHA CI 成功证明或测试 job。
- CD 接受 v* 标签，`:314` 却固定 prerelease=false；check_tag 的 is_stable 只控制 Docker 分支，不控制 Release 预发布标志。
- `webui/package.json:6` 没有 typecheck 脚本，webui 未发现 tsconfig；Vite build 和单测不是 TypeScript 类型检查。
- CI 从 git describe 注入后端版本，CD 从 tag/manual 输入取版本，缺少与前端 package.json 的强制一致性校验。
- 改进：同 SHA 的测试与不可变构建产物作为发布前置；显式 RC 标志；类型检查、关键链路 race、API→执行层控制面 E2E、Linux 构建及版本/header 验证。
- 验收：失败测试的提交无法发布，RC 不冒充稳定版，下载产物的版本、提交和测试证明可对应。

### A09：已有回放不足以证明稳定净盈利【历史报告，证据缺口】

- `docs/reports/2026-09-17-replay-calibration.md:22`：2 个交易对、89 天，以上涨/震荡为主，缺持续下跌样本；三个分段不独立。
- `:69` 起列明 K 线内路径假设、无真实盘口/队列、下单/撤单/回报零延迟；100% maker 不是实盘承诺；部分资金费是近似值。
- 当前整改改变了风控与平仓行为，历史结果也不能直接代表 rc3 的执行结果。
- 改进：固定版本重跑基线；时间隔离样本外与滚动验证；参数邻域稳定性；下跌、跳空、费用升高、流动性恶化和执行延迟压力测试；经授权后才进行真实成交记录的只读校准。
- 盈利看板应分别展示已实现收益、浮动盈亏、交易费、资金费、借息、运行成本、现金流调整后净权益及回撤。已经按实际成交价计算的 PnL，不要再次重复扣除同一滑点。
- 验收：结果对合理费用/延迟/参数扰动仍有解释力，风险预算和亏损边界可核实。软件正确、安全可恢复、账本可信、策略有优势是四个不同条件，不能互相替代。

## 本轮重新执行的验证

| 检查 | 结果 | 边界 |
|---|---|---|
| 整仓 Go 回归，无审查用例排除 | 1294 个顶层用例通过、6 跳过、0 失败包；退出码 0 | 显式关闭真实交易所网络测试；本机非 race 回归 |
| 其中原始 TestAudit 回归 | 11/11 通过 | 不等于新增场景或全部整改闭环 |
| webui yarn test | 33 文件、144 项通过；退出码 0 | 不是类型检查或浏览器交易控制 E2E |
| 新增遗留平仓安全断言 | 失败 | 接单未成交仍报成功，剩余仓位 1 |
| 新增风控持久化安全断言 | 失败 | 无持久化管理器仍报 persisted=true |
| 新增 Bot 风控并发读取/更新，-race | 失败 | Getter :875 / Setter :886 DATA RACE |
| git diff --check | 通过 | 仅格式检查 |

跳过项：exchange 包的公开 Binance K 线与真实公共行情测试，以及 Bybit/OKX 各自 NewAdapter、AdapterBasicMethods 网络测试。没有将跳过项算成通过。

全仓结果：`/private/tmp/quantmesh-readiness-recheck.9JX55J/full/results.json` 和 `results.md`。临时复现文件与 overlay 位于该目录上一级；未加入正式业务测试树。临时目录可能被系统清理，后续修复时应整理成正式回归用例。

复现命令：

```sh
go test -overlay=/private/tmp/quantmesh-readiness-recheck.9JX55J/overlay.json . ./web -run '^TestRecheck(Legacy|Running)' -count=1 -timeout=60s
go test -race -overlay=/private/tmp/quantmesh-readiness-recheck.9JX55J/overlay.json . -run '^TestRecheckBotRiskConcurrentReadUpdate$' -count=1 -timeout=60s
QUANTMESH_NETWORK_TESTS=0 QUANTMESH_LIVE_EXCHANGE_TESTS=0 ruby scripts/verify_profit_readiness.rb /private/tmp/quantmesh-readiness-recheck.9JX55J/full --full
```

前两条是缺陷复现，当前预期退出码 1；第三条现有整仓回归退出码 0。不能通过降低断言让缺陷用例转绿。

## 已改善与未验收边界

原始硬暂停、SHORT 收益符号、累计成交均价、信号策略部分成交、内部止损/触发价、权益扣款感知、ClientOrderID 唯一性和开发模式鉴权的回归均通过。rc3 新增的退出查询失败、终态缺总量额度保留等正式测试也包含在本轮整仓回归内，不再重复列为原缺陷。

这不覆盖实际部署的代理/TLS/密钥权限、全部交易所账户模式、生产数据库恢复演练、完整重启故障 E2E、真实执行质量与盈利。没有检查到这些环境，不作已经安全或已经盈利的承诺。

建议顺序：先 A01–A03 的真实执行与风险上限，再 A04–A06 的恢复、记账与停机闭环；随后闭合 A07 的明确支持范围、A08 的发布门禁，最后以固定版本验收 A09。放行前每项都应有可重复的验收证据，而不是只标记“代码已改”。

## 后续整改说明

`3.111.0-rc4` 已移除 A01 中遗留 API 的接单计成功及扫撤全部委托路径，接入逐单/残仓核验，并增加账户内未知结果传播和重复请求保护。正式模拟及三轮定向竞态已通过。账户级平仓的完整策略账本结算、已有保护单协调与重启恢复仍未闭合，不把 A01 所在的完整 R04 范围标为完成；其他 A02–A09 仍以各项未完成边界为准。最新验证和范围见 [整改进度第十二批](2026-09-24-remediation-progress.md)。上文为 rc3 审查时证据，保留原始结果。

`3.111.0-rc5` 已修复 A02 的 Bot 限仓启动/热更新、共享指针竞态、持久化假成功及名义价值/保证金口径；额外覆盖 BOTH 和部分成交库存。整仓 1,318 项通过、6 跳过，定向竞态三轮和前端 146 项通过。通用止损/波动率字段消费者、配置版本可观测性及 API→执行→恢复的完整 E2E 仍待补，A03 硬额度生产接入仍未完成；不把这批当作全部风控闭环。边界和报告见 [整改进度第十三批](2026-09-24-remediation-progress.md)。

`3.111.0-rc6` 针对 A04 接入共享执行器的提交前持久意图、CAS 观察保存和 owner scope 分页恢复，删除跨账户/Bot 的 2000 条活动订单扫描路由；实时回报也校验归属并保留单调累计成交证据。整仓 1,331 项通过、6 跳过，定向竞态三轮通过。恢复后的非明确拒绝意图及旧交易历史仍保持独立阻断，尚未完成策略/费用/资本的经济重放和自动放行，不能将 A04 标为完成；详见 [整改进度第十四批](2026-09-24-remediation-progress.md) 和 [迁移边界](2026-09-24-intent-journal-migration.md)。
