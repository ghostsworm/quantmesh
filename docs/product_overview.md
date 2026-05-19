# QuantMesh 产品概览

## 目录

- [产品简介](#产品简介)
- [核心特性](#核心特性)
- [交易策略](#交易策略)
- [交易所集成](#交易所集成)
- [风险管理](#风险管理)
- [回测系统](#回测系统)
- [监控告警](#监控告警)
- [Web 界面](#web-界面)
- [AI 智能体](#ai-智能体)
- [高可用架构](#高可用架构)
- [数据库支持](#数据库支持)
- [安全特性](#安全特性)
- [插件系统](#插件系统)
- [新闻监控](#新闻监控)
- [波动率检测](#波动率检测)
- [技术指标](#技术指标)
- [部署方式](#部署方式)
- [国际化](#国际化)

---

## 产品简介

QuantMesh 是一款高性能、低延迟的加密货币做市系统，采用 Go 语言开发。该系统专门为永续合约市场提供流动性，使用先进的网格策略。系统已经过实战检验，处理超过 1 亿美元的交易量，支持 20+ 主流交易所。

### 技术亮点

- **毫秒级响应**：WebSocket 驱动的低延迟架构
- **多策略引擎**：支持同时运行多种交易策略
- **智能风控**：多层次风险管理系统
- **AI 赋能**：集成 Google Gemini 进行市场分析
- **现代化界面**：全功能 React Web 界面
- **高可用性**：支持集群部署和故障转移

### 近期优化

- **3.105.0-rc33**：趋势跟踪、均值回归、动量策略补齐自动下单闭环，开仓 `BUY`、平仓 `SELL`，合约平仓带 `ReduceOnly`；成交回报后才确认本地仓位，并在状态数据中暴露 pending action 与自动交易模式。
- **3.105.0-rc32**：策略状态接口改为读取真实运行态，网格、DCA、马丁、组合及当时尚未自动执行的信号型策略 Start/Stop 后会同步状态；并拆除均值回归/趋势可视化和信号路径中的嵌套锁，降低长期运行卡死和状态误报风险。
- **3.105.0-rc31**：公开 K 线交易所工厂从 Binance-only 扩展到全部已声明接入的交易所，Web 未启动对应 Bot 时也会优先尝试对应交易所的公开行情/K 线适配器；新增 live 公开行情检查，便于持续排查“名义接入但实际 API 不可用”的落差。
- **3.105.0-rc30**：Bot 风控 API 增加负数/越界比例硬校验，网格风控改为 patch 语义避免部分更新清空止损参数；动态止损手动调整接口未实现时返回 501 而非假成功；前端认证与初始化服务错误文案走 i18n 并移除调试日志。
- **3.105.0-rc29**：资金分配器增加金融级防护，固定资金池超额时按总资金比例缩放，负配置与负预留/释放不再污染可用资金；DCA / 马丁止损平仓保持 reduce-only 但取消 post-only，优先降低风险敞口。
- **3.105.0-rc28**：组合策略自适应权重再平衡增加定时器兜底，避免间隔配置被覆盖为 0 或负数后触发 ticker panic。
- **3.105.0-rc27**：订单清理与深度监控补充定时器安全默认值，避免间隔漏配时触发 `time.NewTicker(0)` 崩溃；空 context 会安全兜底，增强长期风控任务稳定性。
- **3.105.0-rc26**：订单同步、资金费率监控、复合风控与开仓控制器补齐长期任务生命周期保护，支持安全停止/重启，并在 context 取消后清理运行状态，降低后台定时任务静默失效风险。
- **3.105.0-rc25**：订单轮询在 context 取消退出后会自动清理运行状态，并允许空 context 安全启动，避免后台轮询已停止但服务状态仍显示运行、无法重启。
- **3.105.0-rc24**：订单状态轮询能正确读取 `int64` 订单 ID 和指针槽位，并支持安全重启与无效轮询间隔兜底，避免外部成交/撤单长期未同步到本地仓位账本。
- **3.105.0-rc23**：动态止损启用波动率检查或盈利追踪但间隔漏配时，会使用安全默认值，避免 `time.NewTicker(0)` 让进程崩溃；Bot 提供者为空时跳过检查，增强风控模块容错性。
- **3.105.0-rc22**：动态止损管理器支持停止后安全重启，并且活跃止损槽位查询返回防御性拷贝，避免长期运行时检查器静默退出或外部调用篡改内部止损状态。
- **3.105.0-rc21**：全局熔断器会忽略重复触发，避免反复撤单/平仓；恢复过程先完成内部状态切换再恢复 Bot，并尊重手动恢复要求，降低长期运行中的重复风控动作和死锁风险。
- **3.105.0-rc20**：紧急中心的减仓动作不再假成功；缺少按比例减仓接口时会保守执行全平保护，并明确记录兜底结果。
- **3.105.0-rc19**：DCA / 马丁平仓单进入 closing 状态，等待成交回报后才清理内部仓位；平仓取消会保留原仓位，避免限价平仓未成交却误判空仓后再次开仓。
- **3.105.0-rc18**：DCA 的限时暂停会按时恢复；DCA / 马丁遇到执行器跳过下单时不会污染仓位层级状态，避免长期运行中的永久暂停、panic 或状态漂移。
- **3.105.0-rc17**：多策略执行器会区分卖出平仓与卖开空；SHORT / BOTH 以及 short 类策略的 SELL 开仓会参与资金预估与预留；对冲目标归零会被继续下发并执行，避免空侧策略绕过资金池或对冲腿残留旧仓位。
- **3.105.0-rc16**：BOTH 双向网格的智能挂单管理会同时识别买开多和卖开空，避免长期运行时空侧开仓挂单脱离距离/数量控制。
- **3.105.0-rc15**：统一 LONG / SHORT / BOTH 方向归一化，修复旧配置迁移和状态接口误把 BOTH 视为 LONG 的问题；BOTH 双向网格新增开仓总量保护，并补充多腿/空腿 reduce-only 平仓方向测试。
- **3.105.0-rc14**：Web 公共行情、参数建议与优化器价格接口统一使用带超时的 HTTP 客户端，避免交易所公共 API 异常时阻塞 Web 请求。

---

## 核心特性

### 1. 低延迟交易

- WebSocket 实时价格推送
- 毫秒级订单执行
- 1000+ 订单/秒处理能力
- 优化的网络通信

### 2. 多交易所支持

- 20+ 主流交易所集成
- 统一的交易接口
- 现货和合约支持
- 测试网支持

### 3. 策略多样性

- 网格交易策略
- DCA 定投策略
- 马丁格尔策略
- 均值回归策略
- 动量策略
- 趋势跟踪策略
- 组合策略

### 4. 智能风控

- 多层风险控制
- 实时仓位监控
- 自动止损止盈
- 资金安全检查

### 5. AI 智能分析

- 自然语言交互
- 智能参数推荐
- 市场情绪分析
- 新闻事件解读

---

## 交易策略

### 网格策略 (Grid Strategy)

QuantMesh 的核心策略，提供多种网格交易模式：

#### 基础网格功能

- **等差网格**：固定价格间距
- **等比网格**：按比例增长
- **无限网格**：不限制网格数量
- **分批建仓**：逐步进入市场

#### 高级网格特性

- **超级槽位系统**：P1/P2 槽位管理
- **动态网格移动**：API 控制网格位置
- **网格风险控制**：
  - 止损/止盈设置
  - 追踪止损
  - 最大网格层数限制
  - 最大挂单数量限制
- **价格范围软限制**：防止极端价格开仓
- **触发价格激活**：达到指定价格后启动
- **趋势过滤**：根据趋势过滤买卖决策

#### 网格控制 API

- `/api/grid/move_up` - 向上移动网格
- `/api/grid/move_down` - 向下移动网格
- `/api/grid/shift` - 动态调整网格
- `/api/grid/status` - 查询网格状态

### DCA 策略 (Dollar Cost Averaging)

- **平均成本法**：定期定额投资
- **加仓机制**：下跌时分批买入
- **利润分配**：智能利润分配
- **动态调整**：根据市场情况调整

### 马丁格尔策略 (Martingale)

- **亏损恢复**：通过增加仓位 recover
- **系数控制**：可配置马丁系数
- **风险限制**：最大层数限制
- **资金管理**：防止过度杠杆

### 均值回归策略 (Mean Reversion)

- **均值计算**：统计价格均值
- **超买超卖检测**：识别极端价格
- **回归交易**：价格回归时交易
- **波动率适应**：根据波动率调整

### 动量策略 (Momentum)

- **趋势跟踪**：基于动量跟随趋势
- **多时间框架**：多个时间周期分析
- **确认过滤**：多重确认机制
- **强度评估**：趋势强度判断

### 趋势跟踪策略 (Trend Following)

- **趋势检测**：智能趋势识别算法
- **多周期分析**：短中长期趋势判断
- **入场信号**：精确的入场时机
- **出场管理**：智能出场策略

### 组合策略 (Combo Strategy)

- **多策略组合**：同时运行多种策略
- **资金分配**：策略间资金分配
- **并行执行**：策略并发运行
- **风险分散**：降低单一策略风险

### 高级策略功能

- **资本动态分配**：策略间动态资金调整
- **对冲协调**：多仓位对冲管理
- **策略执行适配器**：统一的策略执行接口
- **多策略引擎**：并发策略管理

---

## 交易所集成

### 支持的交易所 (20+)

#### 主流交易所

| 交易所 | 现货 | 合约 | 测试网 | 状态 |
|--------|------|------|--------|------|
| **Binance** | ✅ | ✅ | ✅ | 完全支持 |
| **OKX** | ✅ | ✅ | ✅ | 完全支持 |
| **Bybit** | ✅ | ✅ | ✅ | 完全支持 |
| **Bitget** | ✅ | ✅ | ✅ | 完全支持 |
| **Gate.io** | ✅ | ✅ | ✅ | 完全支持 |
| **Huobi (HTX)** | ✅ | ✅ | ✅ | 完全支持 |

#### 其他支持交易所

- Kraken, KuCoin, Bitfinex
- MEXC, BingX, Deribit
- BitMEX, Phemex, WOO X
- CoinEx, Bitrue, XT.COM
- BTCC, AscendEX, Poloniex
- Crypto.com, Whitebit
- Bitkub, Coinsph

### 集成特性

#### 统一接口层

```go
// 统一的交易所接口
type Exchange interface {
    // 订单管理
    PlaceOrder(order *Order) (*OrderResult, error)
    CancelOrder(orderID string) error
    QueryOrder(orderID string) (*Order, error)

    // 账户信息
    GetBalance() (*Balance, error)
    GetPositions() ([]Position, error)

    // 市场数据
    GetTicker(symbol string) (*Ticker, error)
    GetOrderBook(symbol string) (*OrderBook, error)
}
```

#### WebSocket 实时数据

- 实时价格推送
- 订单簿深度数据
- 成交记录
- 账户更新
- 订单状态更新

#### 高级功能

- **价格尖峰过滤**：防止异常价格
- **模拟交易模式**：无需真实资金测试
- **API 权限检查**：自动验证 API 权限
- **错误重试机制**：智能错误处理
- **速率限制**：防止 API 限流

---

## 风险管理

### 多层风控体系

#### 1. 主动市场风险监控

**文件**: `monitor/market_risk_monitor.go`

- **K 线成交量异常检测**
  - 成交量突增/突降检测
  - 异常波动识别
  - 自动暂停交易
- **恢复阈值监控**
  - 市场恢复正常后自动恢复
  - 可配置的恢复条件
- **多币种健康追踪**
  - 同时监控多个交易对
  - 独立的风险判断
  - 统一的风险报告

#### 2. 网格风险控制 (每个交易对)

**配置路径**: `trading.symbols[].grid_risk_control`

```yaml
grid_risk_control:
  # 止损配置
  stop_loss_ratio: 0.10        # 10% 止损
  stop_loss_trigger_price: 45000 # 触发价格

  # 止盈配置
  take_profit_ratio: 0.20      # 20% 止盈
  take_profit_trailing: 0.05   # 5% 追踪止盈

  # 网格限制
  max_grid_layers: 20          # 最大网格层数
  max_open_orders: 100         # 最大挂单数

  # 趋势过滤
  enable_trend_filter: true    # 启用趋势过滤
  trend_period: 15             # 趋势周期
  min_trend_strength: 0.7      # 最小趋势强度
```

#### 3. 账户安全

**文件**: `risk/account_safety.go`

- **交易前余额检查**：确保有足够资金
- **杠杆限制**：防止过度杠杆
- **仓位安全验证**：检查仓位合理性
- **保证金监控**：实时监控保证金水平

#### 4. 仓位对账 (Position Reconciliation)

**文件**: `reconciliation/position_reconcil.go`

- **定期同步**：每分钟与交易所同步
- **本地 vs 远程对比**：发现状态不一致
- **自动订单清理**：清理孤儿订单
- **仓位修正机制**：自动修正仓位错误

#### 5. 资金费率监控

**文件**: `monitor/funding_rate_monitor.go`

- **费率阈值监控**：监控高资金费率
- **费率偏向调整**：根据费率调整策略
- **趋势同步**：与趋势指标协同

#### 6. 深度监控

**文件**: `monitor/depth_monitor.go`

- **订单簿深度分析**：评估流动性
- **流动性空洞检测**：发现深度不足
- **价差监控**：监控买卖价差
- **深度调整**：根据深度调整策略

### 风控 API

```
GET  /api/risk/status           # 获取当前风险状态
GET  /api/risk/monitor          # 风险监控数据
GET  /api/risk/history          # 风险检查历史
POST /api/risk/pause            # 暂停交易
POST /api/risk/resume           # 恢复交易
```

### 波动率风控

**配置路径**: `trading.open_position_control.bot_risk_control.volatility_pause_config`

```yaml
volatility_pause_config:
  # 暂停触发条件
  pause_on_high_volatility: true      # 高波动时暂停
  pause_on_extreme_volatility: true   # 极端波动时暂停
  pause_on_sudden_increase: true      # 突然增加时暂停

  # 策略方向过滤
  pause_on_downtrend: true   # 做多策略+高波动+下跌时暂停
  pause_on_uptrend: false    # 做空策略+高波动+上涨时暂停

  # 自动恢复
  auto_resume_on_normal: true  # 波动率正常时自动恢复
  resume_threshold: 2.0        # 恢复阈值 (%)

  # 趋势判断配置
  trend_check_period: 15       # 趋势检查周期 (分钟)
  trend_down_threshold: 2.0    # 下跌趋势阈值 (%)
  trend_up_threshold: 2.0      # 上涨趋势阈值 (%)
```

---

## 回测系统

### 全面回测功能

#### 1. 历史 K 线测试

**文件**: `backtest/data_manager.go`

- **多时间框架支持**：1m, 3m, 5m, 15m, 1h, 4h, 1d
- **历史数据获取**：自动从交易所获取
- **高效缓存机制**：
  - 本地文件缓存
  - 数据索引管理
  - 增量更新
- **多交易所数据**：支持 Binance、OKX 等

#### 2. 多策略回测

**文件**: `backtest/multi_strategy_backtest.go`

- **并行策略测试**：同时测试多个策略
- **策略组合测试**：测试策略组合效果
- **资金分配模拟**：模拟策略间资金分配
- **性能对比**：自动生成性能对比报告

#### 3. 风险分析指标

**文件**: `backtest/analyzer.go`

系统计算 20+ 风险指标：

| 指标类别 | 指标名称 | 说明 |
|---------|---------|------|
| **收益指标** | 总收益率 | 整体盈利百分比 |
| | 年化收益率 | 年化收益表现 |
| | 平均收益率 | 每笔交易平均收益 |
| **风险指标** | 最大回撤 | 最大亏损幅度 |
| | 夏普比率 | 风险调整后收益 |
| | 索提诺比率 | 下行风险调整收益 |
| | 卡尔玛比率 | 回撤调整收益 |
| **交易指标** | 胜率 | 盈利交易占比 |
| | 盈亏比 | 平均盈利/平均亏损 |
| | 交易次数 | 总交易笔数 |
| | 平均持仓时间 | 每笔交易时长 |
| **统计指标** | 标准差 | 收益波动程度 |
| | 偏度 | 收益分布偏斜 |
| | 峰度 | 收益分布峰度 |
| | VaR | 风险价值 |
| | CVaR | 条件风险价值 |

#### 4. 报告生成

**文件**: `backtest/report_generator.go`

- **Markdown 报告**：
  ```markdown
  # 回测报告

  ## 策略参数
  - 交易对: BTCUSDT
  - 时间范围: 2024-01-01 至 2024-12-31
  - 初始资金: 10000 USDT

  ## 收益指标
  - 总收益率: 45.6%
  - 年化收益率: 52.3%
  - 最大回撤: -12.3%

  ## 交易统计
  - 总交易次数: 1,234
  - 胜率: 65.4%
  - 盈亏比: 1.8

  ## 风险指标
  - 夏普比率: 2.3
  - 索提诺比率: 3.1
  - 卡尔玛比率: 3.7
  ```

- **CSV 导出**：权益曲线数据
- **图表生成**：收益曲线、回撤图
- **详细交易记录**：每笔交易的详细信息

#### 5. 智能参数优化

**文件**: `backtest/optimizer.go`

- **通用优化器**：支持多种优化算法
- **参数空间探索**：网格搜索、随机搜索
- **自动回测调度**：批量回测任务
- **性能对比**：自动排序最优参数

### 回测 API

```
POST /api/backtest/create         # 创建回测任务
GET  /api/backtest/{id}           # 获取回测结果
GET  /api/backtest/list           # 获取回测列表
POST /api/backtest/optimize       # 参数优化
GET  /api/backtest/report/{id}    # 获取报告
GET  /api/backtest/equity/{id}    # 获取权益曲线
```

### 回测特性

| 特性 | 说明 |
|------|------|
| **数据源灵活** | 支持 Binance、本地文件、多种格式 |
| **缓存管理** | 高效的数据缓存机制 |
| **日内测试** | 分钟级精细测试 |
| **任务管理** | 队列式回测执行 |
| **结果存储** | JSON 格式存储分析 |
| **多策略支持** | 可同时测试多个策略 |
| **风险分析** | 20+ 风险指标计算 |
| **报告导出** | Markdown/CSV 格式 |

---

## 监控告警

### 系统监控

#### 1. 看门狗系统

**文件**: `monitor/watchdog.go`

- **健康检查**：定期检查系统状态
- **进程监控**：监控进程运行状态
- **自动恢复**：故障时自动恢复
- **服务状态 API**：`GET /api/watchdog/status`

#### 2. 指标收集

**文件**: `monitor/metrics.go`

- **Prometheus 集成**：
  ```go
  // 交易指标
  trading_total_orders_total
  trading_successful_orders_total
  trading_failed_orders_total
  trading_volume_total

  // 性能指标
  order_duration_seconds
  api_latency_seconds
  websocket_messages_total

  // 系统指标
  goroutines_count
  memory_usage_bytes
  gc_duration_seconds
  ```

- **性能指标**：
  - 订单执行延迟
  - API 调用延迟
  - WebSocket 消息处理速度
  - 内存使用情况

- **交易统计**：
  - 总交易量
  - 成交订单数
  - 失败订单数
  - 盈亏统计

#### 3. 每日快照

**文件**: `monitor/daily_snapshot.go`

- **每日盈亏分解**：
  - 每个交易对的盈亏
  - 每个策略的盈亏
  - 手续费统计
- **仓位快照**：每日结束时仓位记录
- **交易汇总**：每日交易统计

### 告警系统

#### 1. 阈值监控

**配置示例**：
```yaml
monitoring:
  alerts:
    # 价格告警
    price_alert:
      enabled: true
      threshold_change: 5.0  # 5% 变动告警

    # 成交量告警
    volume_alert:
      enabled: true
      threshold_multiplier: 2.0  # 2倍成交量告警

    # 仓位告警
    position_alert:
      enabled: true
      max_position_ratio: 0.8  # 80% 仓位告警

    # 盈亏告警
    pnl_alert:
      enabled: true
      loss_threshold: -1000  # 亏损超过 1000 USDT 告警
```

#### 2. 事件中心

**文件**: `event/event_center.go`

- **价格波动事件**：
  - 短期大幅波动
  - 突破关键价位
  - 价格异常

- **交易事件**：
  - 大额成交
  - 连续成交
  - 订单失败

- **系统事件**：
  - 连接断开
  - API 错误
  - 系统异常

- **事件过滤**：按类型、重要性过滤
- **事件聚合**：相似事件聚合展示

#### 3. 新闻告警

**文件**: `monitor/news_monitor.go`

- **市场事件检测**：新闻事件识别
- **情绪分析**：正负面情绪判断
- **预测验证**：新闻后的价格验证

### 监控 API

```
GET  /api/status                 # 实例状态
GET  /api/statuses               # 多实例状态
GET  /api/statistics             # 交易统计
GET  /api/statistics/daily       # 每日统计
GET  /api/statistics/anomalous-trades  # 异常交易检测
GET  /api/events                 # 事件列表
GET  /api/alerts                 # 告警列表
```

---

## Web 界面

### 现代 React 界面

#### 1. 仪表盘概览

**组件**: `webui/src/components/Dashboard.tsx`

- **实时交易状态**：
  - 当前运行状态
  - 活跃策略数量
  - 实时盈亏
- **盈亏可视化**：
  - 实时盈亏图表
  - 今日/本周/本月盈亏
- **仓位追踪**：
  - 当前仓位列表
  - 仓位价值
  - 未实现盈亏
- **性能指标**：
  - 胜率
  - 平均收益率
  - 最大回撤

#### 2. 策略配置

**组件**: `webui/src/components/StrategyConfig.tsx`

- **交互式策略设置**：
  - 参数调整界面
  - 实时验证
  - 参数建议
- **策略模板**：
  - 预设策略模板
  - 快速启动
  - 参数导入导出
- **配置验证**：
  - 实时参数检查
  - 错误提示
  - 优化建议

#### 3. 实时监控

**组件**: `webui/src/components/RealTimeMonitor.tsx`

- **实时订单簿**：
  - 买卖盘深度
  - 价格分布
  - 流动性分析
- **价格图表**：
  - K 线图表
  - 技术指标
  - 绘图工具
- **仓位追踪**：
  - 实时仓位变化
  - 盈亏更新
  - 止损止盈状态
- **交易历史**：
  - 成交记录
  - 订单状态
  - 历史查询

#### 4. 风险管理界面

**组件**: `webui/src/components/RiskManagement.tsx`

- **风控配置**：
  - 止损止盈设置
  - 仓位限制
  - 风险参数
- **告警设置**：
  - 告警阈值
  - 通知方式
  - 告警历史
- **仓位限制**：
  - 最大仓位设置
  - 杠杆限制
  - 风险敞口

### 高级功能

#### 1. 策略可视化

**组件**: `webui/src/components/GridVisualization.tsx`

- **网格策略可视化**：
  - 网格线展示
  - 当前价格位置
  - 成交网格高亮
- **仓位管理 UI**：
  - 仓位分布图
  - 成本基础
  - 盈利区间
- **订单流展示**：
  - 订单队列
  - 成交流向
  - 深度变化
- **性能图表**：
  - 收益曲线
  - 回撤图
  - 资金曲线

#### 2. 配置管理

**组件**: `webui/src/components/ConfigManager.tsx`

- **YAML 编辑器**：
  - 语法高亮
  - 错误提示
  - 自动补全
- **配置验证**：
  - 实时验证
  - 错误标记
  - 修复建议
- **导入导出**：
  - 配置文件上传
  - 配置下载
  - 版本对比
- **配置历史**：
  - 版本管理
  - 回滚功能
  - 变更对比

#### 3. 数据管理

**组件**: `webui/src/components/DataManager.tsx`

- **K 线文件管理**：
  - 文件列表
  - 数据预览
  - 文件上传
- **数据导出工具**：
  - 交易记录导出
  - 盈亏数据导出
  - 自定义导出
- **图表库集成**：
  - TradingView
  - ECharts
  - D3.js
- **实时更新**：
  - WebSocket 推送
  - 自动刷新
  - 增量更新

### 国际化支持

**组件**: `webui/src/i18n/`

支持 21 种语言：

- **英语** (en-US, en-GB)
- **中文** (zh-CN, zh-TW)
- **日语** (ja-JP)
- **韩语** (ko-KR)
- **西班牙语** (es-ES)
- **法语** (fr-FR)
- **德语** (de-DE)
- **俄语** (ru-RU)
- **阿拉伯语** (ar-SA)
- **印地语** (hi-IN)
- 等...

**特性**：
- RTL 语言支持
- 动态语言切换
- 本地化内容
- 社区贡献

### UI 组件列表

| 组件 | 功能 | 路径 |
|------|------|------|
| **Dashboard** | 仪表盘概览 | `/dashboard` |
| **StrategyConfig** | 策略配置 | `/strategy` |
| **RealTimeMonitor** | 实时监控 | `/monitor` |
| **RiskManagement** | 风险管理 | `/risk` |
| **Backtest** | 回测系统 | `/backtest` |
| **NewsMonitor** | 新闻监控 | `/news` |
| **Events** | 事件中心 | `/events` |
| **Statistics** | 统计分析 | `/statistics` |
| **Settings** | 系统设置 | `/settings` |
| **AIChat** | AI 对话 | `/ai-chat` |

---

## AI 智能体

### 对话式 AI 系统

#### 1. QuantMesh AI 智能体

**文件**: `agent/quantmesh_agent.go`

- **自然语言策略配置**：
  ```javascript
  用户: "帮我配置一个 BTC 网格策略"
  AI: "好的！我来为您配置 BTCUSDT 网格策略。
       建议参数：
       - 价格区间: 40000-50000
       - 网格数量: 20
       - 每格数量: 100 USDT
       是否应用此配置？"
  ```

- **交互式交易助手**：
  - 策略调整建议
  - 风险评估
  - 参数优化
- **上下文感知对话**：
  - 记住对话历史
  - 理解用户意图
  - 多轮对话支持

#### 2. AI 工具集

**文件**: `agent/tools/`

| 工具名称 | 功能 | 参数 |
|---------|------|------|
| **get_parameters** | 获取策略参数 | symbol |
| **set_parameters** | 设置策略参数 | symbol, params |
| **analyze_market** | 市场分析 | symbol, timeframe |
| **assess_risk** | 风险评估 | symbol, position_size |
| **get_news** | 获取新闻 | symbol, limit |
| **optimize_strategy** | 策略优化 | strategy_name |
| **configure_volatility** | 波动率配置 | symbol, enable |
| **get_volatility_preset** | 获取波动率预设 | symbol |
| **list_volatility_presets** | 列出所有预设 | - |

#### 3. 技能系统

**文件**: `docs/AI_AGENT_TOOLS_SKILLS.md`

**技能 1: 策略配置向导**
- 引导用户完成策略配置
- 参数建议和验证
- 配置应用

**技能 2: 市场分析**
- 价格趋势分析
- 市场情绪判断
- 技术指标分析

**技能 3: 波动率保护设置**
- 自动配置波动率检测
- 预设应用
- 风控建议

### AI 服务

#### 1. 市场分析

**文件**: `agent/services/market_analysis.go`

- **价格趋势分析**：
  - 短期/中期/长期趋势
  - 支撑阻力位
  - 突破信号
- **市场情绪**：
  - 恐慌贪婪指数
  - 社交媒体情绪
  - 资金流向
- **新闻解读**：
  - 新闻摘要
  - 影响评估
  - 预测验证

#### 2. 参数优化

**文件**: `agent/services/parameter_optimizer.go`

- **AI 辅助参数调优**：
  - 基于历史数据
  - 风险偏好匹配
  - 市场条件适应
- **性能预测**：
  - 预期收益
  - 风险评估
  - 置信区间
- **策略优化**：
  - 参数组合优化
  - 多目标优化
  - 约束条件

#### 3. Gemini 集成

**文件**: `agent/ai/gemini_client.go`

- **Google Gemini AI 客户端**：
  ```go
  type GeminiClient struct {
      client *genai.Client
      model  string
  }

  func (c *GeminiClient) AnalyzeMarket(ctx context.Context,
      symbol string, data *MarketData) (*MarketAnalysis, error) {
      // AI 市场分析
  }
  ```

- **自然语言处理**：
  - 用户意图理解
  - 策略描述解析
  - 对话管理
- **市场解读**：
  - 技术指标解释
  - 形态识别
  - 交易信号生成

### AI 功能特性

| 功能 | 说明 |
|------|------|
| **市场解读** | 实时市场分析 |
| **新闻分析** | 新闻源情绪分析 |
| **参数顾问** | 智能参数建议 |
| **风险评估** | AI 驱动风险评价 |
| **任务自动化** | AI 驱动任务管理 |
| **对话管理** | 多轮对话支持 |
| **上下文记忆** | 会话历史管理 |
| **意图识别** | 用户意图理解 |

---

## 高可用架构

### 集群支持

#### 1. 多实例配置

**配置文件**: `docs/config/examples/config-ha-example.yaml`

```yaml
instance:
  id: "quantmesh-1"           # 实例唯一标识
  cluster_mode: true          # 启用集群模式
  load_balancer: "nginx"      # 负载均衡器

cluster:
  instances:
    - id: "quantmesh-1"
      address: "http://192.168.1.10:8080"
      role: "master"
    - id: "quantmesh-2"
      address: "http://192.168.1.11:8080"
      role: "slave"
    - id: "quantmesh-3"
      address: "http://192.168.1.12:8080"
      role: "slave"
```

- **实例 ID 管理**：唯一标识每个实例
- **负载均衡**：Nginx/HAProxy 集成
- **故障转移**：自动故障检测和转移
- **分布式协调**：实例间协调机制

#### 2. 分布式锁

**文件**: `lock/distributed_lock.go`

支持多种后端：

| 后端 | 实现 | 特性 |
|------|------|------|
| **Redis** | `lock/redis_lock.go` | 高性能，持久化 |
| **Database** | `lock/db_lock.go` | 简单，无需额外依赖 |
| **Etcd** | `lock/etcd_lock.go` | 强一致性 |

```go
// 使用示例
lock, err := lock.NewDistributedLock(lock.Redis, config)
if err != nil {
    return err
}

// 获取锁
acquired, err := lock.TryLock(ctx, "critical_section", 30*time.Second)
if !acquired {
    return errors.New("failed to acquire lock")
}

// 执行临界区代码
// ...

// 释放锁
err = lock.Unlock(ctx, "critical_section")
```

- **临界区保护**：保护关键操作
- **死锁预防**：超时机制
- **超时处理**：自动锁释放
- **可重入锁**：同一线程可重入

#### 3. 会话管理

**文件**: `session/distributed_session.go`

- **分布式会话**：
  - 跨实例会话共享
  - 会话持久化
  - 会话复制
- **负载均衡兼容**：
  - Sticky Session
  - Session Affinity
  - 无状态模式

### 数据持久化

#### 1. 数据库选项

| 数据库 | 适用场景 | 特性 |
|--------|---------|------|
| **SQLite** | 单实例 | 文件存储，嵌入式 |
| **PostgreSQL** | 生产环境 | 高性能，功能丰富 |
| **MySQL 8** | 生产环境 | 优化，全文搜索 |

#### 2. 数据同步

- **多实例数据同步**：
  - 实时数据同步
  - 冲突解决
  - 数据一致性
- **备份恢复**：
  - 自动备份
  - 增量备份
  - 快速恢复

### 高可用 API

```
GET  /api/statuses              # 多实例状态
GET  /api/cluster/health        # 集群健康
GET  /api/cluster/instances     # 实例列表
POST /api/cluster/promote       # 提升为主节点
POST /api/cluster/demote        # 降级为从节点
```

---

## 数据库支持

### 支持的数据库

#### 1. SQLite

**配置**: `database.type: "sqlite"`

- **默认选项**：开箱即用
- **文件存储**：`quantmesh.db`
- **嵌入式数据库**：无需独立服务
- **适用场景**：
  - 单实例部署
  - 开发测试
  - 小规模应用

**特性**：
- 零配置
- 跨平台
- 事务支持
- 轻量级

#### 2. PostgreSQL

**配置**: `database.type: "postgresql"`

```yaml
database:
  type: "postgresql"
  host: "localhost"
  port: 5432
  database: "quantmesh"
  username: "quantmesh"
  password: "your_password"
  ssl_mode: "require"
  max_open_conns: 100
  max_idle_conns: 10
```

- **生产就绪**：企业级数据库
- **高级特性**：
  - JSON 支持
  - 全文搜索
  - 地理数据
  - 数组类型
- **性能优化**：
  - 连接池
  - 查询优化
  - 索引策略

**适用场景**：
- 高并发场景
- 大规模数据
- 复杂查询

#### 3. MySQL 8

**配置**: `database.type: "mysql8"`

```yaml
database:
  type: "mysql8"
  host: "localhost"
  port: 3306
  database: "quantmesh"
  username: "quantmesh"
  password: "your_password"
  charset: "utf8mb4"
  max_open_conns: 100
  max_idle_conns: 10
```

- **高性能**：优化的查询引擎
- **全文搜索**：内置全文索引
- **JSON 支持**：JSON 数据类型
- **优化特性**：
  - 查询缓存
  - 分区表
  - 读写分离

**适用场景**：
- 高性能要求
- 大数据量
- Web 应用

**参考文档**: `docs/MYSQL_8_GUIDE.md`

### 数据库特性

#### 1. 连接池管理

```go
type DatabaseConfig struct {
    MaxOpenConns int           // 最大连接数
    MaxIdleConns int           // 最大空闲连接
    ConnMaxLifetime time.Duration  // 连接最大生命周期
    ConnMaxIdleTime time.Duration   // 连接最大空闲时间
}
```

#### 2. 迁移支持

```bash
# 执行迁移
./quantmesh migrate up

# 回滚迁移
./quantmesh migrate down

# 查看迁移状态
./quantmesh migrate status
```

#### 3. 数据导出/导入

```bash
# 备份数据
./quantmesh backup create

# 恢复数据
./quantmesh backup restore <backup_file>

# 导出数据
./quantmesh export --format json --output data.json
```

#### 4. 性能优化

- **索引优化**：常用查询字段索引
- **查询优化**：优化慢查询
- **连接池**：高效的连接管理
- **缓存策略**：减少数据库访问

---

## 安全特性

### 认证和授权

#### 1. WebAuthn 支持

**文件**: `web/handler/webauthn.go`

- **无密码认证**：
  ```javascript
  // 注册 WebAuthn
  const registration = await WebAuthn.register({
    username: "user@example.com",
    displayName: "User Name"
  });

  // 使用 WebAuthn 登录
  const authentication = await WebAuthn.authenticate({
    username: "user@example.com"
  });
  ```

- **FIDO2/U2F 支持**：
  - 硬件密钥（YubiKey）
  - 生物识别（指纹、Face ID）
  - 移动设备认证

- **特性**：
  - 抗钓鱼攻击
  - 中间人攻击防护
  - 重放攻击防护

#### 2. 会话管理

**文件**: `web/handler/session.go`

- **安全会话处理**：
  - 安全 Cookie（HttpOnly, Secure, SameSite）
  - 会话 ID 随机化
  - 会话超时
- **CSRF 保护**：
  - CSRF Token
  - SameSite Cookie
  - 验证来源
- **XSS 防护**：
  - 输入验证和转义
  - Content Security Policy
  - XSS Token

#### 3. API 安全

**文件**: `web/middleware/security.go`

- **速率限制**：
  ```go
  // API 速率限制
  rateLimiter := middleware.NewRateLimiter(rateLimiter.Config{
      Requests: 100,
      Window:   time.Minute,
  })
  ```

- **输入验证**：
  - 参数类型验证
  - 长度限制
  - 格式检查
- **安全头**：
  ```http
  X-Frame-Options: DENY
  X-Content-Type-Options: nosniff
  X-XSS-Protection: 1; mode=block
  Strict-Transport-Security: max-age=31536000
  ```
- **CORS 保护**：
  - 白名单域名
  - 方法限制
  - 头部限制

### 数据安全

#### 1. 加密

**文件**: `config/encryption.go`

- **配置加密**：
  ```yaml
  # 加密敏感配置
  encrypted_config:
    api_key: "encrypted:base64encoded_data"
    api_secret: "encrypted:base64encoded_data"
  ```

- **敏感数据保护**：
  - API 密钥加密存储
  - 数据库密码加密
  - 交易密钥加密

- **TLS/SSL 支持**：
  ```yaml
  web:
    tls:
      enabled: true
      cert_file: "/path/to/cert.pem"
      key_file: "/path/to/key.pem"
  ```

#### 2. 隐私保护

**文件**: `telemetry/privacy.go`

- **GDPR 合规**：
  - 数据最小化
  - 用户同意
  - 数据删除权
- **匿名遥测**：
  ```go
  type TelemetryConfig struct {
      Enabled     bool
      AnonymousID bool
      NoIP        bool
      NoLocation  bool
  }
  ```
- **用户同意管理**：
  - 明确同意
  - 可撤销
  - 记录保存

#### 3. 网络安全

**文件**: `docs/SSH_TUNNEL_ACCESS.md`

- **SSH 隧道支持**：
  ```bash
  # 创建 SSH 隧道
  ssh -L 8080:localhost:8080 user@server

  # 通过隧道访问
  curl http://localhost:8080/api/status
  ```

- **VPN 集成**：
  - WireGuard 支持
  - OpenVPN 支持
  - VPN 隧道配置

- **防火墙配置**：
  ```yaml
  firewall:
    enabled: true
    allowed_ips:
      - "192.168.1.0/24"
      - "10.0.0.0/8"
    blocked_ips:
      - "0.0.0.0/0"
  ```

- **安全 API 端点**：
  - 认证要求
  - 权限检查
  - 审计日志

---

## 插件系统

### 可扩展架构

#### 1. 插件加载器

**文件**: `plugin/loader.go`

```go
type PluginLoader struct {
    plugins map[string]Plugin
    config  *Config
}

func (l *PluginLoader) LoadPlugin(path string) error {
    // 加载插件
    plug, err := plugin.Open(path)
    if err != nil {
        return err
    }

    // 获取插件符号
    symPlugin, err := plug.Lookup("Plugin")
    if err != nil {
        return err
    }

    // 初始化插件
    p := symPlugin.(Plugin)
    return p.Init(l.config)
}
```

- **动态插件加载**：
  - 运行时加载
  - 热部署
  - 版本管理

- **插件验证**：
  - 签名验证
  - 依赖检查
  - 兼容性检查

- **依赖管理**：
  - 依赖解析
  - 版本冲突处理
  - 自动下载

#### 2. 插件接口

**文件**: `plugin/interface.go`

```go
type Plugin interface {
    // 插件信息
    Name() string
    Version() string
    Description() string

    // 生命周期
    Init(config *Config) error
    Start() error
    Stop() error

    // 功能
    Execute(ctx context.Context, params map[string]interface{}) (interface{}, error)
}

// 策略插件
type StrategyPlugin interface {
    Plugin
    OnTick(tick *Tick) (*Signal, error)
    OnOrder(order *Order) error
}

// 指标插件
type IndicatorPlugin interface {
    Plugin
    Calculate(data []float64) float64
}

// 交易所插件
type ExchangePlugin interface {
    Plugin
    Connect() error
    PlaceOrder(order *Order) (*OrderResult, error)
}
```

#### 3. 插件示例

**目录**: `plugins/examples/`

- **Demo 策略插件**：`demo_strategy/`
- **自定义交易所适配器**：`custom_exchange/`
- **技术指标插件**：`custom_indicator/`
- **可视化插件**：`custom_visualization/`

### 插件特性

#### 1. 许可管理

**文件**: `plugin/license.go`

- **商业许可支持**：
  ```go
  type License struct {
      Key         string
      Product     string
      ExpiresAt   time.Time
      Features    []string
      MaxCapacity int
  }

  func ValidateLicense(key string) (*License, error) {
      // 验证许可密钥
      // 检查过期时间
      // 验证功能权限
  }
  ```

#### 2. 云验证

**文件**: `plugin/validation_server.go`

- **插件验证服务器**：
  - 许可验证
  - 版本检查
  - 更新通知

#### 3. API 兼容性

- **版本化 API**：
  ```go
  type APIv1 struct{}
  type APIv2 struct{}

  func (p *Plugin) GetAPI(version string) (interface{}, error) {
      switch version {
      case "v1":
          return &APIv1{}, nil
      case "v2":
          return &APIv2{}, nil
      default:
          return nil, errors.New("unsupported API version")
      }
  }
  ```

#### 4. 安全沙箱

- **资源限制**：
  - CPU 限制
  - 内存限制
  - 网络限制
- **权限控制**：
  - 文件系统访问
  - 网络访问
  - API 调用

#### 5. 性能监控

- **执行时间**：
  ```go
  type PluginMetrics struct {
      ExecutionTime time.Duration
      MemoryUsage   uint64
      CallCount     int64
      ErrorCount    int64
  }
  ```
- **错误追踪**
- **资源使用**

---

## 新闻监控

### 综合新闻系统

#### 1. 新闻收集器

**文件**: `monitor/collector/collector.go`

- **RSS 源聚合**：
  ```yaml
  news_sources:
    - name: "CoinDesk"
      url: "https://www.coindesk.com/arc/outboundfeeds/rss/"
      type: "rss"
    - name: "Cointelegraph"
      url: "https://cointelegraph.com/rss"
      type: "rss"
    - name: "CryptoNews"
      url: "https://cryptonews.com/news/feed/"
      type: "rss"
  ```

- **多新闻源**：
  - CoinDesk
  - Cointelegraph
  - CryptoNews
  - Bitcoin.com
  - Decrypt

- **实时更新**：
  - 定时抓取
  - 增量更新
  - 去重处理

- **过滤和分类**：
  - 关键词过滤
  - 情绪分类
  - 重要性评分

#### 2. Gemini 新闻分析器

**文件**: `ai/gemini_news_analyzer.go`

- **AI 驱动情绪分析**：
  ```go
  type NewsAnalysis struct {
      Sentiment    string  // "positive", "negative", "neutral"
      Confidence   float64 // 0-1
      Topics       []string
      Impact       string  // "high", "medium", "low"
      Summary      string
  }

  func (a *GeminiAnalyzer) AnalyzeNews(ctx context.Context,
      news *NewsItem) (*NewsAnalysis, error) {
      // AI 分析新闻
      // 提取情绪
      // 识别主题
      // 评估影响
  }
  ```

- **事件提取**：
  - 公司事件
  - 监管事件
  - 技术事件
  - 市场事件

- **影响评估**：
  - 价格影响预测
  - 市场情绪影响
  - 交易量影响

- **预测验证**：
  - 新闻后价格跟踪
  - 准确率统计
  - 模型优化

#### 3. 市场情报

**文件**: `monitor/market_intelligence.go`

- **恐惧贪婪指数**：
  ```go
  type FearAndGreedIndex struct {
      Value       int     // 0-100
      ValueText   string  // "Extreme Fear", "Fear", etc.
      Timestamp   time.Time
  }
  ```

- **Reddit 分析**：
  - r/Bitcoin 情绪
  - r/CryptoCurrency 讨论
  - 热门话题

- **Polymarket 集成**：
  - 预测市场数据
  - 概率分析
  - 事件合约

- **宏观事件追踪**：
  - 美联储政策
  - 经济数据
  - 地缘政治

### 新闻特性

#### 1. 关键词监控

```yaml
keyword_monitoring:
  keywords:
    - "SEC"
    - "ETF"
    - "regulation"
    - "ban"
    - "hack"
    - "exploit"

  actions:
    - type: "alert"
      threshold: 3  # 3 个关键词触发告警
    - type: "pause_trading"
      keywords: ["ban", "SEC"]
```

#### 2. 情绪分析

- **正面/负面/中性评分**：
  ```go
  type SentimentScore struct {
      Positive  float64 // 0-1
      Negative  float64 // 0-1
      Neutral   float64 // 0-1
      Overall   string  // "bullish", "bearish", "neutral"
  }
  ```

#### 3. 事件相关性

- **新闻-价格相关性**：
  ```go
  type NewsPriceCorrelation struct {
      NewsItem    *NewsItem
      PriceChange float64
      Correlation float64 // -1 to 1
      Confidence  float64 // 0-1
  }
  ```

#### 4. 历史分析

- **新闻影响随时间**：
  - 短期影响（1小时）
  - 中期影响（1天）
  - 长期影响（1周）

#### 5. 多语言支持

- **国际新闻源**：
  - 英语
  - 中文
  - 日语
  - 韩语
  - 俄语

---

## 波动率检测

### 高级波动率系统

#### 1. 波动率制度检测

**文件**: `indicators/volatility_regime.go`

- **多周期波动率计算**：
  ```go
  type VolatilityRegime struct {
      ShortVolatility  float64  // 短期波动率
      MediumVolatility float64  // 中期波动率
      LongVolatility   float64  // 长期波动率
      CurrentRegime    RegimeLevel
      PreviousRegime   RegimeLevel
      ChangeTime       time.Time
  }

  type RegimeLevel int
  const (
      RegimeLow      RegimeLevel = 0
      RegimeNormal   RegimeLevel = 1
      RegimeHigh     RegimeLevel = 2
      RegimeExtreme  RegimeLevel = 3
  )
  ```

- **制度分类**：
  - LOW: 低波动
  - NORMAL: 正常波动
  - HIGH: 高波动
  - EXTREME: 极端波动

- **突然变化检测**：
  ```go
  type VolatilityChange struct {
      PreviousRegime  RegimeLevel
      NewRegime       RegimeLevel
      ChangePercent   float64
      IsSudden        bool
      Timestamp       time.Time
  }
  ```

- **趋势分析**：
  - 上涨趋势
  - 下跌趋势
  - 横盘整理

#### 2. 动态调整

**文件**: `strategy/dynamic_adjuster.go`

- **自动策略参数调整**：
  ```go
  type DynamicAdjustment struct {
      BaseGridSpacing    float64
      AdjustedSpacing    float64
      BaseOrderSize      float64
      AdjustedOrderSize  float64
      VolatilityMultiplier float64
  }

  func (da *DynamicAdjuster) AdjustForVolatility(volatility float64) {
      // 根据波动率调整参数
      // 高波动 -> 增加网格间距
      // 低波动 -> 减少网格间距
  }
  ```

- **风险响应**：
  - 高波动 -> 降低仓位
  - 低波动 -> 增加仓位

- **仓位规模调整**：
  - 波动率自适应
  - 风险敞口控制

- **网格参数优化**：
  - 动态网格数量
  - 自适应价格区间

#### 3. 波动率告警

**文件**: `monitor/volatility_monitor.go`

- **制度变化通知**：
  ```go
  type VolatilityAlert struct {
      Symbol        string
      PreviousRegime RegimeLevel
      NewRegime     RegimeLevel
      Timestamp     time.Time
      Message       string
  }
  ```

- **波动率尖峰告警**：
  - 突然增加通知
  - 阈值告警

- **恢复通知**：
  - 波动率正常化
  - 自动恢复交易

- **历史追踪**：
  - 波动率历史
  - 制度变化历史
  - 告警历史

### 波动率特性

#### 1. 品种预设

**文件**: `indicators/volatility_presets.go`

| 品种 | 低 | 正常 | 高 | 极端 |
|------|-----|------|-----|------|
| **BTC/ETH** | <1.5% | <4.0% | <7.0% | ≥15.0% |
| **黄金** | <0.5% | <1.5% | <3.0% | ≥6.0% |
| **稳定币** | <0.1% | <0.3% | <0.5% | ≥1.0% |
| **Meme 币** | <5.0% | <10.0% | <20.0% | ≥40.0% |

#### 2. 实时监控

- **持续波动率追踪**：
  - 每分钟更新
  - 多时间框架
  - 滚动窗口

#### 3. 预测分析

- **波动率预测**：
  - GARCH 模型
  - 历史波动率
  - 隐含波动率

#### 4. 策略适应

- **自动参数变化**：
  - 无需手动调整
  - 实时响应
  - 风险优化

#### 5. 风险缓解

- **基于波动率的风控**：
  - 动态止损
  - 仓位限制
  - 暂停交易

---

## 技术指标

### 综合指标库 (50+)

#### 1. 趋势指标

| 指标 | 全称 | 参数 | 用途 |
|------|------|------|------|
| **MACD** | 移动平均收敛散度 | 快线、慢线、信号线 | 趋势跟踪 |
| **ADX** | 平均趋向指数 | 周期 | 趋势强度 |
| **DMI** | 趋向指标 | 周期 | 趋势方向 |
| **SMA** | 简单移动平均 | 周期 | 趋势识别 |
| **EMA** | 指数移动平均 | 周期 | 趋势识别 |
| **WMA** | 加权移动平均 | 周期 | 趋势识别 |
| **Parabolic SAR** | 抛物线转向 | 加速因子 | 趋势反转 |
| **Ichimoku** | 一目均衡表 | 多参数 | 综合分析 |

**实现示例**：
```go
// MACD 计算
func CalculateMACD(prices []float64, fast, slow, signal int) (*MACD, error) {
    emaFast := CalculateEMA(prices, fast)
    emaSlow := CalculateEMA(prices, slow)
    macdLine := emaFast - emaSlow
    signalLine := CalculateEMA(macdLine, signal)
    histogram := macdLine - signalLine

    return &MACD{
        MACD:       macdLine,
        Signal:     signalLine,
        Histogram:  histogram,
    }, nil
}
```

#### 2. 动量指标

| 指标 | 全称 | 参数 | 用途 |
|------|------|------|------|
| **RSI** | 相对强弱指数 | 周期 | 超买超卖 |
| **Stochastic** | 随机指标 | K周期、D周期 | 动量 |
| **CCI** | 顺势指标 | 周期 | 超买超卖 |
| **MFI** | 资金流量指数 | 周期 | 成交量动量 |
| **Williams %R** | 威廉指标 | 周期 | 超买超卖 |
| **ROC** | 变动率 | 周期 | 动量 |

**实现示例**：
```go
// RSI 计算
func CalculateRSI(prices []float64, period int) (*RSI, error) {
    if len(prices) < period {
        return nil, errors.New("insufficient data")
    }

    gains := make([]float64, 0)
    losses := make([]float64, 0)

    for i := 1; i < len(prices); i++ {
        change := prices[i] - prices[i-1]
        if change > 0 {
            gains = append(gains, change)
            losses = append(losses, 0)
        } else {
            gains = append(gains, 0)
            losses = append(losses, -change)
        }
    }

    avgGain := Average(gains[:period])
    avgLoss := Average(losses[:period])

    rs := avgGain / avgLoss
    rsi := 100 - (100 / (1 + rs))

    return &RSI{Value: rsi}, nil
}
```

#### 3. 波动率指标

| 指标 | 全称 | 参数 | 用途 |
|------|------|------|------|
| **ATR** | 平均真实波幅 | 周期 | 波动率 |
| **Bollinger Bands** | 布林带 | 周期、标准差 | 波动率 |
| **Keltner Channel** | 肯特纳通道 | 周期、倍数 | 波动率 |
| **Standard Deviation** | 标准差 | 周期 | 波动率 |
| **Volatility Regime** | 波动率制度 | 多周期 | 制度识别 |

**实现示例**：
```go
// 布林带计算
func CalculateBollingerBands(prices []float64, period, stdDev int) (*BollingerBands, error) {
    sma := CalculateSMA(prices, period)
    std := CalculateStdDev(prices, period)

    upper := sma + (std * float64(stdDev))
    lower := sma - (std * float64(stdDev))

    return &BollingerBands{
        Upper:  upper,
        Middle: sma,
        Lower:  lower,
        Bandwidth: (upper - lower) / sma * 100,
    }, nil
}
```

#### 4. 成交量指标

| 指标 | 全称 | 参数 | 用途 |
|------|------|------|------|
| **OBV** | 能量潮 | - | 成交量趋势 |
| **VWAP** | 成交量加权平均价 | - | 平均价格 |
| **Money Flow Index** | 资金流量指数 | 周期 | 成交量动量 |
| **Volume Profile** | 成交量分布 | - | 支撑阻力 |
| **Chaikin MF** | 蔡金资金流 | 周期 | 资金流向 |

### 指标特性

#### 1. 多时间框架分析

- **分钟级**：1m, 3m, 5m, 15m
- **小时级**：1h, 4h
- **日级**：1d
- **周级**：1w

#### 2. 自定义参数

```yaml
indicators:
  rsi:
    period: 14
    overbought: 70
    oversold: 30

  macd:
    fast_period: 12
    slow_period: 26
    signal_period: 9

  bollinger_bands:
    period: 20
    std_dev: 2
```

#### 3. 策略集成

```go
type Strategy struct {
    Indicators []Indicator
    Signals    []Signal

    func (s *Strategy) OnTick(tick *Tick) {
        // 更新所有指标
        for _, ind := range s.Indicators {
            ind.Update(tick)
        }

        // 生成交易信号
        for _, sig := range s.Signals {
            signal := sig.Generate()
            if signal.IsValid() {
                s.Execute(signal)
            }
        }
    }
}
```

#### 4. 实时计算

- WebSocket 价格推送
- 增量计算
- 缓存优化

#### 5. 可视化

```javascript
// ECharts 集成
const chart = echarts.init(document.getElementById('main'));
chart.setOption({
    series: [{
        type: 'candlestick',
        data: ohlcvData,
        indicators: [
            { type: 'MA', period: 20 },
            { type: 'BOLL', period: 20 }
        ]
    }]
});
```

---

## 部署方式

### 多种部署方法

#### 1. Docker 部署

**文件**: `docker-compose.yml`

```yaml
version: '3.8'
services:
  quantmesh:
    image: quantmesh:latest
    container_name: quantmesh
    ports:
      - "8080:8080"
    volumes:
      - ./config:/app/config
      - ./data:/app/data
    environment:
      - QUANTMESH_ENV=production
      - DATABASE_TYPE=postgresql
      - DATABASE_HOST=db
    depends_on:
      - db
      - redis

  db:
    image: postgres:15
    environment:
      POSTGRES_DB: quantmesh
      POSTGRES_USER: quantmesh
      POSTGRES_PASSWORD: your_password
    volumes:
      - db_data:/var/lib/postgresql/data

  redis:
    image: redis:7
    command: redis-server --appendonly yes
    volumes:
      - redis_data:/data

volumes:
  db_data:
  redis_data:
```

- **Docker Compose 支持**
- **容器化部署**
- **环境变量配置**
- **卷挂载**

#### 2. 源码部署

```bash
# 克隆仓库
git clone https://github.com/yourusername/quantmesh.git
cd quantmesh

# 安装依赖
go mod download

# 编译
go build -o quantmesh cmd/quantmesh/main.go

# 运行（首参为可选 YAML 路径；主配置权威见 app_config）
./quantmesh
# 或: ./quantmesh ./my-import.yaml
```

- **Go 编译**
- **直接执行**
- **开发模式**
- **生产构建**

#### 3. 云部署

**AWS 部署**：
```yaml
# terraform/main.tf
resource "aws_ecs_task_definition" "quantmesh" {
  family = "quantmesh"
  network_mode = "awsvpc"
  requires_compatibilities = ["FARGATE"]
  cpu = "2048"
  memory = "4096"

  container_definitions = jsonencode([
    {
      name = "quantmesh"
      image = "quantmesh:latest"
      portMappings = [{ containerPort = 8080 }]
      environment = [
        { name = "DATABASE_TYPE", value = "postgresql" }
      ]
    }
  ])
}
```

- **AWS/GCP/Azure 支持**
- **Kubernetes 就绪**
- **负载均衡**
- **自动伸缩**

**Kubernetes 部署**：
```yaml
# k8s/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: quantmesh
spec:
  replicas: 3
  selector:
    matchLabels:
      app: quantmesh
  template:
    metadata:
      labels:
        app: quantmesh
    spec:
      containers:
      - name: quantmesh
        image: quantmesh:latest
        ports:
        - containerPort: 8080
        env:
        - name: DATABASE_TYPE
          value: "postgresql"
---
apiVersion: v1
kind: Service
metadata:
  name: quantmesh-service
spec:
  selector:
    app: quantmesh
  ports:
  - port: 80
    targetPort: 8080
  type: LoadBalancer
```

### 部署特性

#### 1. 热重载

```yaml
# 配置热重载
watcher:
  enabled: true
  config_paths:
    - "/app/config/*.yaml"
  reload_command: "SIGHUP"
```

#### 2. 健康检查

```go
// 健康检查端点
func (s *Server) HealthCheck(c *gin.Context) {
    status := map[string]string{
        "status": "healthy",
        "version": s.Version,
        "uptime": time.Since(s.StartTime).String(),
    }

    // 检查数据库连接
    if err := s.db.Ping(); err != nil {
        status["database"] = "unhealthy"
        c.JSON(503, status)
        return
    }

    // 检查交易所连接
    if !s.exchange.IsConnected() {
        status["exchange"] = "unhealthy"
        c.JSON(503, status)
        return
    }

    c.JSON(200, status)
}
```

#### 3. 日志管理

```yaml
logging:
  level: "info"
  format: "json"
  outputs:
    - type: "stdout"
    - type: "file"
      path: "/var/log/quantmesh/app.log"
    - type: "syslog"
      address: "localhost:514"
```

#### 4. 监控集成

```yaml
monitoring:
  prometheus:
    enabled: true
    port: 9090
    path: "/metrics"

  grafana:
    enabled: true
    dashboards:
      - "overview"
      - "performance"
      - "trading"
```

#### 5. 备份恢复

```bash
# 自动备份
./quantmesh backup create --schedule "0 2 * * *"

# 恢复
./quantmesh backup restore --file backup_20240101.tar.gz
```

---

## 国际化

### 多语言支持

#### 1. 支持的语言 (21)

| 语言 | 代码 | 状态 |
|------|------|------|
| **英语 (美国)** | en-US | ✅ 完整 |
| **英语 (英国)** | en-GB | ✅ 完整 |
| **中文 (简体)** | zh-CN | ✅ 完整 |
| **中文 (繁体)** | zh-TW | ✅ 完整 |
| **日语** | ja-JP | ✅ 完整 |
| **韩语** | ko-KR | ✅ 完整 |
| **西班牙语** | es-ES | ✅ 完整 |
| **法语** | fr-FR | ✅ 完整 |
| **德语** | de-DE | ✅ 完整 |
| **俄语** | ru-RU | ✅ 完整 |
| **阿拉伯语** | ar-SA | ✅ RTL 支持 |
| **印地语** | hi-IN | ✅ 完整 |
| **葡萄牙语** | pt-BR | ✅ 完整 |
| **意大利语** | it-IT | ✅ 完整 |
| **荷兰语** | nl-NL | ✅ 完整 |
| **土耳其语** | tr-TR | ✅ 完整 |
| **波兰语** | pl-PL | ✅ 完整 |
| **越南语** | vi-VN | ✅ 完整 |
| **泰语** | th-TH | ✅ 完整 |
| **印尼语** | id-ID | ✅ 完整 |
| **乌克兰语** | uk-UA | ✅ 完整 |

#### 2. 本地化特性

**RTL 语言支持**：
```css
/* 阿拉伯语 RTL 支持 */
[dir="rtl"] {
    direction: rtl;
    text-align: right;
}

[dir="rtl"] .margin-left {
    margin-left: 0;
    margin-right: 1rem;
}
```

**货币格式化**：
```javascript
// 货币格式化
function formatCurrency(amount, currency, locale) {
    return new Intl.NumberFormat(locale, {
        style: 'currency',
        currency: currency,
        minimumFractionDigits: 2,
        maximumFractionDigits: 8
    }).format(amount);
}

// 示例
formatCurrency(1234.56, 'USD', 'en-US');  // "$1,234.56"
formatCurrency(1234.56, 'EUR', 'de-DE');  // "1.234,56 €"
formatCurrency(1234.56, 'CNY', 'zh-CN');  // "¥1,234.56"
```

**日期/时间本地化**：
```javascript
// 日期本地化
function formatDateTime(date, locale, timeZone) {
    return new Intl.DateTimeFormat(locale, {
        year: 'numeric',
        month: 'long',
        day: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
        timeZone: timeZone
    }).format(date);
}

// 示例
formatDateTime(new Date(), 'en-US', 'America/New_York');
// "March 8, 2026, 02:30 PM"

formatDateTime(new Date(), 'zh-CN', 'Asia/Shanghai');
// "2026年3月8日 下午2:30"
```

#### 3. 翻译管理

**目录结构**：
```
webui/src/i18n/
├── locales/
│   ├── en-US.json
│   ├── zh-CN.json
│   ├── ja-JP.json
│   └── ...
├── index.ts
└── utils.ts
```

**翻译文件格式**：
```json
{
    "common": {
        "save": "Save",
        "cancel": "Cancel",
        "delete": "Delete"
    },
    "dashboard": {
        "title": "Dashboard",
        "total_balance": "Total Balance",
        "today_pnl": "Today's P&L"
    },
    "strategy": {
        "grid": {
            "name": "Grid Strategy",
            "upper_price": "Upper Price",
            "lower_price": "Lower Price"
        }
    }
}
```

**动态语言切换**：
```typescript
import i18n from './i18n';

// 切换语言
function changeLanguage(lang: string) {
    i18n.changeLanguage(lang);
    localStorage.setItem('language', lang);
    document.documentElement.lang = lang;
}

// 获取当前语言
function getCurrentLanguage(): string {
    return localStorage.getItem('language') || 'en-US';
}
```

#### 4. 社区贡献

**贡献流程**：
1. Fork 仓库
2. 创建翻译分支
3. 添加语言文件
4. 提交 Pull Request
5. 审核合并

**翻译工具**：
- i18n-ally (VS Code)
- i18n-browser (Chrome)
- Crowdin (平台)

### 国际特性

#### 1. 时区支持

```typescript
// 时区转换
function convertTimeZone(date: Date, from: string, to: string): Date {
    const tzDate = new Date(date.toLocaleString('en-US', { timeZone: from }));
    return new Date(tzDate.toLocaleString('en-US', { timeZone: to }));
}

// 示例
const utcDate = new Date();
const tokyoDate = convertTimeZone(utcDate, 'UTC', 'Asia/Tokyo');
const nyDate = convertTimeZone(utcDate, 'UTC', 'America/New_York');
```

#### 2. 区域设置

```yaml
# 区域配置
regional:
  locale: "zh-CN"
  timezone: "Asia/Shanghai"
  currency: "CNY"
  date_format: "YYYY-MM-DD"
  time_format: "HH:mm:ss"
  number_format:
    decimal_separator: "."
    thousands_separator: ","
```

#### 3. 货币支持

- **多货币显示**：USD, CNY, JPY, EUR 等
- **自动转换**：基于实时汇率
- **格式化显示**：本地化格式

#### 4. 文化适应

- **节假日**：不同国家节假日
- **交易时间**：市场开放时间
- **合规要求**：区域法规

---

## 总结

QuantMesh 是一个全面的、企业级加密货币做市平台，结合了复杂的交易策略、先进的风险管理、AI 驱动的洞察和用户友好的界面。支持 20+ 交易所、毫秒级性能和实战验证的可靠性，专为个人交易者和机构运营设计。

### 系统优势

#### 技术优势

- **低延迟 WebSocket 架构**：毫秒级响应时间
- **多策略执行引擎**：并发运行多个策略
- **全面风险管理**：多层风控系统
- **AI 市场分析**：智能决策支持
- **全功能 React 界面**：现代化用户体验
- **高可用集群**：故障转移和负载均衡
- **可扩展插件系统**：灵活定制
- **国际化支持**：21 种语言

#### 业务优势

- **多交易所支持**：20+ 主流交易所
- **策略多样性**：7 种核心策略 + 组合策略
- **智能风控**：波动率检测 + 趋势过滤 + 自动止损
- **回测系统**：20+ 风险指标 + 参数优化
- **AI 助手**：自然语言交互 + 智能建议
- **移动友好**：PWA 支持 + 响应式设计

#### 适用场景

**个人交易者**：
- 自动化交易策略
- 智能风险控制
- 盈利优化

**机构运营**：
- 大规模交易
- 风险管理
- 合规报告

**量化团队**：
- 策略研发
- 回测分析
- 性能优化

### 技术栈

**后端**：
- Go 1.21+
- WebSocket
- gRPC
- PostgreSQL/MySQL

**前端**：
- React 18
- TypeScript
- ECharts
- TailwindCSS

**AI/ML**：
- Google Gemini
- TensorFlow
- scikit-learn

**部署**：
- Docker
- Kubernetes
- Nginx

---

**文档版本**: v3.61.3
**最后更新**: 2026-03-08
**维护者**: QuantMesh Team

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
