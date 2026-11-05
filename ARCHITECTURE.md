# QuantMesh 架构入口

当前版本：`3.112.0-rc1`（开发中，未发布）。详细历史架构见 [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)，本文记录当前应用边界与本轮验证链路。

## 应用与持久化

- Go 1.25.4；`cmd/quantmesh` 启动 `internal/app`，装配交易所、策略、存储和 Web 服务。
- React / TypeScript WebUI 使用现有 Chakra 组件和 i18n 资源，由后端嵌入提供；HTTP API 沿用当前认证与访问控制，不新增匿名入口。
- 交易配置、任务与业务数据使用现有 SQLStorage（SQLite / MySQL 等）。本轮没有 DDL 变更，沿用启动迁移机制。
- 实盘策略/订单/仓位与回测隔离：`backtest/replay` 实例化模拟交易所、模拟时钟及 `position.SuperPositionManager`，不连接真实账户或提交真实订单。

## 网页网格样本外验证

`WebUI 优化任务 → POST /api/backtest/optim/tasks → optimrun.OptimTaskManager → optimizer.UniversalOptimizer.Run → replay walk-forward → replay.RunGridTaskContext → 模拟 SPM / 撮合`。

- 请求将 `replay_walk_forward` 放入原 `search_space` JSON，完整保存训练/测试/步长、杠杆、固定基线参数；无需迁移任务表。
- 新 grid 优化必须显式指定 replay walk-forward。非 grid 的原路径和既有 legacy 结果读取保持兼容；独立旧 `/api/optimizer/run` 的 legacy walk-forward 不冒充本入口。
- 首版只支持已有 replay 的单向合约网格参数与有界笛卡尔积；32 个候选、12 折、100000 根 K 线、2000000 次 K 线评估。拒绝未接线轴和外部 aggTrade 文件路径，避免静默忽略或使用未冻结行情。
- 下载后冻结 OHLCV 副本并校验规则时间序列。每折采用完整 K 线半开窗口 train `[s,s+train)`、test `[s+train,s+train+test)`，step=test；测试窗不重叠。
- 训练按 `净收益率(%) − 最大回撤(%)` 选择唯一候选，同分按规范轴顺序稳定决胜；选中后不再根据测试结果重排。最大回撤取 replay 的逐 tick 统计。
- 每次评估创建全新 replay 状态，各窗同额本金、空仓启动；warm-up 只读取当前窗内且在当前市场 tick 前闭合的 K 线。操作等待时钟不能提前开放特征数据；取消传到 replay 主循环并拒绝成功部分结果。
- 实验固定基线、费用和数据，分别输出四组指标：选中参数的训练/测试、基线的训练/测试。期末持仓按市值计入净收益，未模拟强平手续费；不复利拼接独立窗口。
- 结果包含协议版本、数据/配置/参数/实验 SHA-256、逐折边界与候选训练结果。独立 Ruby 验证器另绑定源码基线与 dirty 工作树摘要，保存原始样本和 JSON/Markdown 报告。

## 证据边界

K 线内路径、成交量分配和排队为模型假设；没有真实历史盘口、网络延迟或市场冲击。历史结算费率只能进入费用结算，当前入口拒绝将其作为预测资金费信号。模拟结果及基线优劣不构成实盘、FundingCarry 或盈利验收。细节见 [replay ADR](docs/decisions/2026-09-17-replay-backtest.md) 与 [本轮报告](docs/reports/2026-11-05-replay-walkforward.md)。
