# QuantMesh 架构入口

当前版本：`3.112.0-rc12`（开发中，未发布）。详细历史架构见 [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)，本文记录当前应用边界与本轮验证链路。

## 应用与持久化

- Go 1.25.4；`cmd/quantmesh` 启动 `internal/app`，装配交易所、策略、存储和 Web 服务。
- React / TypeScript WebUI 使用现有 Chakra 组件和 i18n 资源，由后端嵌入提供；HTTP API 沿用当前认证与访问控制，不新增匿名入口。
- 交易配置、任务与业务数据使用现有 SQLStorage（SQLite / MySQL 等）。日账新增证据/报告版本表及 SQLite/MySQL 成对迁移，沿用启动自动迁移机制，失败阻止启动。
- 实盘策略/订单/仓位与回测隔离：`backtest/replay` 实例化模拟交易所、模拟时钟及 `position.SuperPositionManager`，不连接真实账户或提交真实订单。
- `strategy` 承载策略和开仓 Gate；`accounting` 定义恢复事实与授权；`storage` 以 SQLite/MySQL 保存恢复 episode、hold、操作凭据和投影 outbox；`web` 提供受会话权限保护的 HTTP API。

## FundingCarry 管理员解锁续办

恢复状态以数据库为准，runtime generation 的 capability 是账户范围内的写入 fence；generation ID 仅是非秘密审计标识，不能代替 capability。Bot 操作锁负责单实例生命周期串行化，数据库 generation 锁与 CAS 负责跨连接、跨实例竞争。

正常释放分为三个步骤：准备原释放操作并提交 `release_pending`；事务释放 durable hold 并写入 `released`；清理当前 runtime 的精确 recovery Gate。准备提交后中断时，新 runtime 只保留核账能力；普通 takeover 不处理 `release_pending/released`。专用管理员续办在事务中校验原 operation/receipt、scope、证据、owner 和 revisions，追加独立 owner 续办审计，并原子完成原释放操作。之后只移除 `episode_id + recovery_generation` 对应的 Gate。它不启动策略，也不调用交易执行。

续办审计扩展现有 JSON payload，无新增表或 DDL 迁移。原释放授权身份和证据不被改写；新管理员操作使用独立 operation 与 receipt。幂等重试仍验证当前 generation 及提交后证据，证据变化时停止清理。API actor 由管理员 session 注入，客户端不能提交身份或账务事实替代证明。

详细接口、状态转换、故障边界与验证记录见 [FundingCarry 解锁续办协议](docs/funding-recovery-release-resume.md)。本变更未部署，不表示真实交易或盈利验收。

## 网页网格样本外验证

`WebUI 优化任务 → POST /api/backtest/optim/tasks → optimrun.OptimTaskManager → optimizer.UniversalOptimizer.Run → replay walk-forward → replay.RunGridTaskContext → 模拟 SPM / 撮合`。

- 请求将 `replay_walk_forward` 放入原 `search_space` JSON，完整保存训练/测试/步长、杠杆、固定基线参数；无需迁移任务表。
- 新 grid 优化必须显式指定 replay walk-forward。非 grid 的原路径和既有 legacy 结果读取保持兼容；独立旧 `/api/optimizer/run` 的 legacy walk-forward 不冒充本入口。
- 首版只支持已有 replay 的单向合约网格参数与有界笛卡尔积；32 个候选、12 折、100000 根 K 线、2000000 次 K 线评估。拒绝未接线轴和外部 aggTrade 文件路径，避免静默忽略或使用未冻结行情。
- replay 任务只读本地 data.binance.vision U 本位合约 1 分钟文件，目录由 `backtest_data.binance_vision_dir` 指定，缺省 `data/kline/binance_vision`（已在 `.gitignore` 内）。`go run ./tools/visionfetch -symbol BTCUSDT -from 2026-01-01 -to 2026-01-03` 从 data.binance.vision 下载这些文件和官方校验文件；任一文件校验失败、服务器没有该文件，或某一天不是完整的 1440 根 1 分钟 K 线，本次下载的全部新文件都会删除。已经在目录里但不完整的一天也不会被当作已完成而跳过。回放任务本身不下载。创建回放任务时先做这一次读取，读不到就不保存任务，也不占运行名额；任务开跑时会再读一次。返回给页面的失败原因只说明缺哪一天或校验失败，不包含行情目录；目录只写在服务端日志。每个文件必须带 `sha256sum` 校验文件，缺一天、校验和不符或同一天同时存在 zip 与 csv 都整次失败，不回退到交易所接口，也不补造 K 线。zip 里只接受唯一一个 csv，条目名必须是把 zip 文件名的 `.zip` 换成 `.csv`，不能带目录或 `..`；1 分钟日文件解压前不能超过 2 MiB，一个月的资金费不能超过 256 KiB。请求的更长周期由完整 1 分钟桶精确聚合，桶内缺一根即失败。
- 资金费只在调用方已经开启时，用同一校验规则读到的历史结算序列替换固定费率，只计结算成本；没有该目录，或窗口覆盖的月份都没有资金费文件时，沿用固定费率，不发明零费率序列。窗口里只要有一个月份有资金费文件，其余月份也必须有，缺一个月整次失败，不能用别的月份顶上。来源标识、已校验文件名与哈希写入结果和实验摘要，不写入 `base_params`。
- 普通回测任务和非 replay 的优化任务仍走原有的未校验缓存与交易所下载路径，本轮不改。
- 读入后冻结 OHLCV 副本并校验规则时间序列。每折采用完整 K 线半开窗口 train `[s,s+train)`、test `[s+train,s+train+test)`，step=test；测试窗不重叠。
- 训练按 `净收益率(%) − 最大回撤(%)` 选择唯一候选，同分按规范轴顺序稳定决胜；选中后不再根据测试结果重排。最大回撤取 replay 的逐 tick 统计。
- 每次评估创建全新 replay 状态，各窗同额本金、空仓启动；warm-up 只读取当前窗内且在当前市场 tick 前闭合的 K 线。操作等待时钟不能提前开放特征数据；取消传到 replay 主循环并拒绝成功部分结果。
- 实验固定基线、费用和数据，分别输出四组指标：选中参数的训练/测试、基线的训练/测试。期末持仓按市值计入净收益，未模拟强平手续费；不复利拼接独立窗口。
- 结果包含协议版本、数据/配置/参数/实验 SHA-256、逐折边界与候选训练结果。独立 Ruby 验证器另绑定源码基线与 dirty 工作树摘要，保存原始样本和 JSON/Markdown 报告。

## 日账核验

`accounting/daily` 以精确十进制核对单账户、USDT、固定三钱包（spot、spot_margin、futures）的历史自然日权益变化；`storage` 保存不可变证据/报告版本与当前指针，迁移在启动时自动执行；`web` 提供只读 `GET /api/profit/daily-reconciliation`。`GET /api/profit/daily-reconciliation/latest` 由服务端按盈利汇总同一授权范围解析最近一个已闭合自然日，只读已发布报告；没有报告返回未发布，不返回已核实的零。核算恒等式、数据流与故障边界见 [日账 ADR](docs/decisions/2026-11-05-daily-equity-reconciliation.md) 与 [架构说明](docs/ARCHITECTURE.md)。单日通过不解锁汇总或提现权限。

## 证据边界

K 线内路径、成交量分配和排队为模型假设；没有真实历史盘口、网络延迟或市场冲击。历史结算费率只能进入费用结算，当前入口拒绝将其作为预测资金费信号。模拟结果及基线优劣不构成实盘、FundingCarry 或盈利验收。细节见 [replay ADR](docs/decisions/2026-09-17-replay-backtest.md) 与 [本轮报告](docs/reports/2026-11-05-replay-walkforward.md)。
