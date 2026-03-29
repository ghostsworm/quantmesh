# 深度交易系统链路审计（并发 / 重启 / 多 Bot / 多交易所）

先说一句不玩虚的：这套链路主体能跑，但在“并发 + 重启 + 多 bot / 多交易所”组合拳下，有几处会直接把账打花。

## 严重（P0）

### 1) 订单主键只按 `order_id` 全局唯一，跨交易所/账户会互相覆盖

- 路径 + 函数：`storage/sqlite.go` `createTables()`、`SaveOrder()`
- 证据摘要：
  - `orders` 表定义 `order_id BIGINT UNIQUE`
  - `SaveOrder` 使用 `ON CONFLICT(order_id)` upsert
  - 未把 `exchange` / `account` / `symbol` 纳入唯一键
- 风险：
  - 不同交易所（甚至不同账户）出现相同 `order_id` 时，历史订单、策略字段、订单来源被覆盖
  - 造成“回报串单”和审计错配
- 修复建议：
  - 唯一键升级为复合键：`(exchange, account, symbol, order_id)`（`account` 无则降级为子账户/uid 占位）
  - `ON CONFLICT` 同步改为复合键冲突目标
  - 为历史数据做一次冲突扫描和迁移脚本（防止线上静默覆盖）

### 2) `BotManager` 运行时 map 无并发保护，存在数据竞争与运行时崩溃风险

- 路径 + 函数：`bot_manager.go` `StartBot()`、`StopBot()`、`Get()`、`List()`、`AddRuntime()`、`StopAll()`
- 证据摘要：
  - `bm.runtimes` 在多个读写函数中直接访问
  - 未见 `sync.Mutex` / `sync.RWMutex` 保护
- 风险：
  - Web/API 并发启停、查询时可能触发 `concurrent map read and map write`
  - 轻则状态错乱，重则进程直接崩
- 修复建议：
  - 为 `runtimes` 加 `RWMutex`，统一封装读写入口
  - `StopAll` 采用“先拷贝 key，再逐个停”的两段式，避免锁内长耗时阻塞
  - 增加 `-race` 并发单测覆盖启停 + 查询交叉场景

## 高（P1）

### 3) 订单回报到策略实例不是“精确路由”，而是广播 + 各策略自判，重启后更容易失配

- 路径 + 函数：
  - `symbol_manager.go` `startSymbolRuntime()`（订单流回调）
  - `strategy/strategy.go` `StrategyManager.OnOrderUpdate()`
  - `strategy/dca_enhanced.go` `OnOrderUpdate()`
  - `strategy/martingale.go` `OnOrderUpdate()`
- 证据摘要：
  - 回报进入 `StrategyManager.OnOrderUpdate()` 后，对所有策略并发广播
  - DCA/Martingale 仅按 `orderID` 在内存数组中匹配
  - 未见持久化的 `orderID/clientOrderID -> strategy实例` 恢复逻辑
- 风险：
  - 重启后策略内存状态清空，回报无法命中原层级
  - 多策略并发时依赖“谁认得这个 orderID”处理，稳定性弱
- 修复建议：
  - 引入中心化路由表：`(exchange, account, symbol, order_id|client_order_id) -> strategy_instance_id`
  - 下单成功前写“预路由”，成功后补全交易所 `order_id`
  - 重启时从 DB 重建路由并回放未完成订单状态

### 4) 资金释放链路存在“秒成交竞态”窗口，可能出现预留资金不释放

- 路径 + 函数：
  - `strategy/multi_strategy_executor.go` `PlaceOrder()`、`BatchPlaceOrdersWithDetails()`、`ReleaseOrderCapitalByOrderID()`
  - `symbol_manager.go` 订单流回调 `multiExecutor.ReleaseOrderCapitalByOrderID(...)`
- 证据摘要：
  - `orderID -> strategy/reservedAmount` 映射在下单返回后才写入
  - 订单流可能先到（秒成交），释放调用先执行时查不到映射
  - 未见后续补偿机制
- 风险：
  - 可用资金“只减不增”再次出现
  - 策略被假性卡死（这类 bug 最会“阴人”）
- 修复建议：
  - 下单前先写“pending reservation”（以 `clientOrderID` 为主键）
  - 回报优先按 `clientOrderID` 释放；收到 `orderID` 后做键合并
  - 增加“兜底释放任务”：扫描超时 pending，按状态机补偿

## 中（P2）

### 5) `clientOrderId` 设计声明“<=18字符”与实现不一致，前缀截断后可能无法反解析

- 路径 + 函数：`utils/orderid.go` `GenerateOrderID()`、`AddBrokerPrefix()`、`ParseOrderID()`
- 证据摘要：
  - `GenerateOrderID` 实际长度受 `priceInt` 位数影响，未硬限制
  - `AddBrokerPrefix` 超长会截断
  - `SuperPositionManager.OnOrderUpdate()` 强依赖 `parseClientOrderID()`
- 风险：
  - 一旦被截断到不满足解析格式，回报可能被忽略或落错槽位
  - 订单状态与仓位状态脱节
- 修复建议：
  - 统一协议：固定长度（例如 base36 + 校验位）
  - `prefix` 与业务载荷分段编码，禁止盲截断
  - 增加解析失败降级路径（fallback 到 DB 路由表）

### 6) 风控绑定粒度“混搭”：开仓限额偏 bot，市场/深度风控偏全局监控对

- 路径 + 函数：
  - `position/super_position_manager.go` `AdjustOrders()`（`OpenPositionControl.BotRiskControl`）
  - `safety/risk_monitor.go` `NewRiskMonitor()`、`Start()`
  - `safety/depth_monitor.go` `getMonitorSymbols()`
- 证据摘要：
  - Bot 风控（仓位/层数/开仓挂单）在 bot 配置检查
  - Risk/Depth 监控读取 `cfg.RiskControl.MonitorSymbols` 全局列表
- 风险：
  - 监控对异常可触发 runtime 暂停开仓，语义上不是纯 bot 实体隔离
  - 迪丽热巴来了也会问：这是 bot 级，还是“看盘组”级？
- 修复建议：
  - 风控模型分层：`global` / `exchange-symbol` / `bot-instance`
  - 监控告警和执行动作拆分（观察者与执行者解耦）
  - API 与配置显式标注作用域，避免“同名不同义”

### 7) Bot 风控配置更新未完整持久化，重启后行为可能回退

- 路径 + 函数：`web/api_bot_risk_control.go` `updateBotRiskControl()`、`persistGridRiskControlToConfig()`
- 证据摘要：
  - `SetBotRiskControl()` 仅更新运行时
  - 仅见 `GridRiskControl` 持久化，未见 `BotRiskControl` 完整落盘
- 风险：
  - 重启后风控阈值“失忆”，恢复行为与预期不一致
- 修复建议：
  - 新增 `persistBotRiskControlToConfig()` 与事务化落盘
  - 更新接口返回“runtime 已生效 + config 已持久化”双状态
  - 补充重启回归测试（更新 -> 重启 -> 校验）

### 8) 部分查询入口默认 futures，spot 运行时可能取错实例

- 路径 + 函数：`main.go` `symbolManagerWebAdapter.GetEx()`
- 证据摘要：
  - `marketType == ""` 时默认 `"futures"`
  - 多处 Web API 通过 `Get(exchange, symbol)` 取运行时
- 风险：
  - spot/futures 同名交易对场景下，恢复/同步/查询链路可能命中错误 runtime
- 修复建议：
  - 查询入口必须显式 `market_type`，无值直接返回参数错误
  - 兼容期可按“精确匹配 > 唯一候选 > 拒绝歧义”三段策略

## 你关注的 4 个问题，直接回答

### 1) `clientOrderId` 如何生成 + 关联上下文

- 生成链路：
  - `SuperPositionManager.generateClientOrderID()`
  - `utils.GenerateOrderIDWithSource()`
- 核心编码：
  - `price_int + side + timestamp + seq`
  - 可加 `_SL` 表示止损来源
- 上下文关联：
  - 策略上下文并不编码在 `clientOrderId` 内
  - 通过下单请求字段 `StrategyName/StrategyType/OrderSource` 入库
  - `exchangeExecutorAdapter.BatchPlaceOrdersWithDetails()` 的 `strategyMap` 仅当前批次内存有效

### 2) 订单回报如何路由到策略实例

- 首先由 `startSymbolRuntime()` 的订单流回调进入 `superPositionManager.OnOrderUpdate()`（按 `clientOrderId` 解析槽位）
- 同时调用 `strategyManager.OnOrderUpdate()`，广播给所有策略
- 各策略自行按 `orderID` 或状态判定处理，不是中心化精确路由

### 3) 风控规则绑定 bot 还是交易对

- Bot 绑定：
  - `OpenPositionControl.BotRiskControl`（仓位/层数/开仓挂单限制）
- 交易对/全局监控绑定：
  - `RiskMonitor/DepthMonitor` 使用全局 `MonitorSymbols`
  - 不是纯 bot 独享维度

### 4) 并发与重启恢复是否会错配

- 会，而且是现实风险：
  - `BotManager` 并发 map
  - 策略路由广播 + 内存态
  - 资金释放竞态
  - 订单主键设计
  - 风控配置持久化不全
- 这些点在“高频 + 重启 + 多实例”场景会被放大

## 建议的修复优先级（落地顺序）

1. P0 先行：订单复合唯一键 + BotManager 并发安全
2. P1 闭环：订单中心路由表 + 资金释放竞态补偿
3. P2 清债：clientOrderId 协议统一、风控作用域分层、配置持久化补齐、查询入口去默认值
4. 验证要求：每项至少包含并发单测、重启恢复测试、跨交易所回归用例

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
