# QuantMesh AI Agent 内置 Tools 和 Skills

## 📋 概述

QuantMesh AI Agent 系统包含丰富的内置 Tools 和 Skills，用于支持对话式策略配置。

## 🛠️ 内置 Tools (Tools)

### 参数管理类 (Parameter Tools)

#### 1. get_parameters
**功能**: 获取策略的所有参数配置

**参数**:
```json
{
  "strategy_id": "string (optional) - 策略 ID"
}
```

**返回**:
```json
{
  "parameters": "object - 当前参数值",
  "defaults": "object - 默认值",
  "constraints": "object - 参数约束",
  "documentation": "object - 参数文档"
}
```

**示例**:
```javascript
// 用户: "查看网格策略的参数"
agent.executeTool("get_parameters", {
  strategy_id: "grid_bot_1"
})
```

---

#### 2. set_parameter
**功能**: 设置单个策略参数

**参数**:
```json
{
  "strategy_id": "string - 策略 ID (必需)",
  "parameter": "string - 参数名称 (必需)",
  "value": "number/string - 参数值 (必需)"
}
```

**风险等级**: Medium

**示例**:
```javascript
// 用户: "把网格间距改成 5"
agent.executeTool("set_parameter", {
  strategy_id: "grid_bot_1",
  parameter: "price_interval",
  value: 5.0
})
```

---

#### 3. validate_parameters
**功能**: 验证策略参数组合是否有效

**参数**:
```json
{
  "parameters": "object - 参数对象 (必需)"
}
```

**返回**:
```json
{
  "valid": "boolean - 是否有效",
  "errors": "array - 错误列表",
  "warnings": "array - 警告列表",
  "suggestions": "array - 改进建议"
}
```

**示例**:
```javascript
// 用户: "验证一下这些参数对不对"
agent.executeTool("validate_parameters", {
  parameters: {
    price_interval: 5.0,
    grid_count: 20,
    order_quantity: 50
  }
})
```

---

#### 4. suggest_parameters
**功能**: 基于市场条件智能推荐策略参数

**参数**:
```json
{
  "strategy_type": "string - 策略类型 (必需)",
  "symbol": "string - 交易对 (必需)",
  "capital": "number - 投入资金 (可选)",
  "risk_profile": "string - 风险偏好 (可选)"
}
```

**策略类型**: grid, dca, martingale, momentum, trend_following

**风险偏好**: conservative, moderate, aggressive

**返回**:
```json
{
  "suggestions": [
    {
      "parameter": "string - 参数名",
      "value": "any - 推荐值",
      "reason": "string - 推荐理由",
      "confidence": "number - 置信度 (0-1)"
    }
  ],
  "reasoning": "string - 推荐逻辑说明"
}
```

**示例**:
```javascript
// 用户: "帮我推荐 BTC 网格策略的参数"
agent.executeTool("suggest_parameters", {
  strategy_type: "grid",
  symbol: "BTCUSDT",
  capital: 10000,
  risk_profile: "moderate"
})
```

---

### 波动率配置类 (Volatility Tools)

#### 18. configure_volatility_detection
**功能**: 配置波动率检测和自动暂停开仓功能

**参数**:
```json
{
  "symbol": "string - 交易对符号 (必需)",
  "enable_detection": "boolean - 是否启用波动率检测",
  "use_preset": "boolean - 是否使用内置预设 (推荐)",
  "custom_thresholds": "object - 自定义阈值 (不使用预设时)",
  "pause_on_high": "boolean - 高波动时是否自动暂停开仓",
  "pause_on_extreme": "boolean - 极端波动时是否自动暂停开仓",
  "pause_on_downtrend": "boolean - 做多策略在高波动下跌时是否暂停开仓",
  "pause_on_uptrend": "boolean - 做空策略在高波动上涨时是否暂停开仓",
  "auto_resume": "boolean - 波动率回归正常时是否自动恢复开仓"
}
```

**风险等级**: Medium

**内置预设**:
- **BTC/ETH**: 高波动品种（低<1.5%, 正常<4%, 高<7%, 极端≥15%）
- **黄金 (PAXG)**: 中等波动品种（低<0.5%, 正常<1.5%, 高<3%, 极端≥6%）
- **稳定币**: 低波动品种（低<0.1%, 正常<0.3%, 高<0.8%, 极端≥2%）
- **Meme 币**: 极高波动品种（低<5%, 正常<10%, 高<20%, 极端≥40%）

**示例**:
```javascript
// 用户: "帮我的 BTC 网格策略配置波动率检测"
agent.executeTool("configure_volatility_detection", {
  symbol: "BTCUSDT",
  enable_detection: true,
  use_preset: true,
  pause_on_high: true,
  pause_on_extreme: true,
  pause_on_downtrend: true,
  auto_resume: true
})
```

**AI 对话示例**:
```
用户: "帮我的 BTC 网格策略配置波动率检测"

Agent: 好的！我来为 BTCUSDT 配置波动率检测。

📊 使用 BTC 预设配置:
   - 低波动: < 1.5%
   - 正常波动: < 4.0%
   - 高波动: < 7.0%
   - 极端波动: ≥ 15.0%

💡 建议配置:
   ✓ 高波动时暂停开仓
   ✓ 极端波动时暂停开仓
   ✓ 做多策略在下跌时暂停开仓
   ✓ 波动率回归正常时自动恢复

是否应用此配置？
```

---

#### 19. get_volatility_preset
**功能**: 查询交易对的波动率预设配置

**参数**:
```json
{
  "symbol": "string - 交易对符号 (必需)"
}
```

**返回**:
```json
{
  "symbol": "string",
  "preset_name": "string",
  "thresholds": {
    "low": "number",
    "normal": "number",
    "high": "number",
    "extreme": "number"
  },
  "description": "string - 预设说明"
}
```

**示例**:
```javascript
// 用户: "BTC 的波动率阈值是什么？"
agent.executeTool("get_volatility_preset", {
  symbol: "BTCUSDT"
})
```

---

#### 20. list_volatility_presets
**功能**: 列出所有可用的波动率预设

**参数**: 无

**返回**: 所有预设及其配置说明

**示例**:
```javascript
// 用户: "有哪些内置的波动率预设？"
agent.executeTool("list_volatility_presets", {})
```

---

### 策略操作类 (Strategy Tools)

#### 5. create_strategy
**功能**: 创建新的交易策略

**参数**:
```json
{
  "strategy_type": "string - 策略类型 (必需)",
  "symbol": "string - 交易对 (必需)",
  "name": "string - 策略名称 (可选)",
  "config": "object - 策略配置 (可选)"
}
```

**风险等级**: Low

**示例**:
```javascript
// 用户: "创建一个 ETH 的网格策略"
agent.executeTool("create_strategy", {
  strategy_type: "grid",
  symbol: "ETHUSDT",
  name: "ETH网格策略"
})
```

---

#### 6. update_strategy
**功能**: 更新现有策略配置

**参数**:
```json
{
  "strategy_id": "string - 策略 ID (必需)",
  "config": "object - 新配置 (必需)"
}
```

**风险等级**: Medium

**示例**:
```javascript
// 用户: "更新网格策略配置"
agent.executeTool("update_strategy", {
  strategy_id: "grid_bot_1",
  config: {
    price_interval: 10.0,
    grid_count: 15
  }
})
```

---

#### 7. delete_strategy
**功能**: 删除策略

**参数**:
```json
{
  "strategy_id": "string - 策略 ID (必需)"
}
```

**风险等级**: High ⚠️

**示例**:
```javascript
// 用户: "删除测试策略"
agent.executeTool("delete_strategy", {
  strategy_id: "test_strategy"
})
```

---

#### 8. clone_strategy
**功能**: 克隆现有策略

**参数**:
```json
{
  "strategy_id": "string - 源策略 ID (必需)",
  "new_name": "string - 新策略名称 (必需)",
  "symbol": "string - 新交易对 (可选)"
}
```

**风险等级**: Low

**示例**:
```javascript
// 用户: "克隆这个策略到 ETH"
agent.executeTool("clone_strategy", {
  strategy_id: "grid_bot_1",
  new_name: "ETH网格策略",
  symbol: "ETHUSDT"
})
```

---

### 回测分析类 (Backtest Tools)

#### 9. run_backtest
**功能**: 运行策略回测

**参数**:
```json
{
  "strategy_id": "string - 策略 ID (必需)",
  "symbol": "string - 交易对 (必需)",
  "interval": "string - K线周期 (必需)",
  "start_date": "string - 开始日期 (必需)",
  "end_date": "string - 结束日期 (必需)",
  "capital": "number - 初始资金 (必需)"
}
```

**风险等级**: Low

**返回**:
```json
{
  "task_id": "string - 回测任务 ID",
  "status": "string - 状态",
  "estimated_time": "number - 预计时间(秒)"
}
```

**示例**:
```javascript
// 用户: "回测一下过去一个月的表现"
agent.executeTool("run_backtest", {
  strategy_id: "grid_bot_1",
  symbol: "BTCUSDT",
  interval: "1h",
  start_date: "2025-02-08",
  end_date: "2025-03-08",
  capital: 10000
})
```

---

#### 10. compare_results
**功能**: 对比多个回测结果

**参数**:
```json
{
  "backtest_ids": "array - 回测任务 ID 列表 (必需)",
  "metrics": "array - 对比指标 (可选)"
}
```

**指标**: total_return, max_drawdown, sharpe_ratio, win_rate

**示例**:
```javascript
// 用户: "对比这几个回测结果"
agent.executeTool("compare_results", {
  backtest_ids: ["bt_1", "bt_2", "bt_3"],
  metrics: ["total_return", "max_drawdown"]
})
```

---

#### 11. optimize_parameters
**功能**: 优化策略参数

**参数**:
```json
{
  "strategy_id": "string - 策略 ID (必需)",
  "symbol": "string - 交易对 (必需)",
  "optimization_target": "string - 优化目标 (必需)",
  "parameter_ranges": "object - 参数范围 (必需)"
}
```

**优化目标**: total_return, sharpe_ratio, profit_factor

**示例**:
```javascript
// 用户: "优化一下参数以最大化收益"
agent.executeTool("optimize_parameters", {
  strategy_id: "grid_bot_1",
  symbol: "BTCUSDT",
  optimization_target: "total_return",
  parameter_ranges: {
    price_interval: [3, 10],
    grid_count: [10, 30]
  }
})
```

---

### 风险分析类 (Risk Tools)

#### 12. assess_risk
**功能**: 评估策略风险

**参数**:
```json
{
  "strategy_id": "string - 策略 ID (必需)",
  "config": "object - 策略配置 (可选)"
}
```

**返回**:
```json
{
  "risk_level": "string - 风险等级",
  "risk_score": "number - 风险评分 (0-100)",
  "factors": [
    {
      "name": "string - 风险因素",
      "impact": "number - 影响程度 (0-1)",
      "description": "string - 描述"
    }
  ],
  "mitigations": "array - 风险缓解建议",
  "alternatives": "array - 替代方案"
}
```

**示例**:
```javascript
// 用户: "评估一下这个策略的风险"
agent.executeTool("assess_risk", {
  strategy_id: "grid_bot_1"
})
```

---

#### 13. calculate_position
**功能**: 计算建议仓位

**参数**:
```json
{
  "strategy_id": "string - 策略 ID (必需)",
  "capital": "number - 可用资金 (必需)",
  "leverage": "number - 杠杆倍数 (可选)"
}
```

**返回**:
```json
{
  "recommended_position": "number - 建议仓位",
  "max_position": "number - 最大仓位",
  "margin_required": "number - 所需保证金",
  "risk_ratio": "number - 风险比率"
}
```

**示例**:
```javascript
// 用户: "计算一下应该用多少仓位"
agent.executeTool("calculate_position", {
  strategy_id: "grid_bot_1",
  capital: 10000
})
```

---

### 市场数据类 (Market Tools)

#### 14. get_market_data
**功能**: 获取实时市场数据

**参数**:
```json
{
  "symbol": "string - 交易对 (必需)",
  "fields": "array - 需要的字段 (可选)"
}
```

**字段**: price, volume, volatility, orderbook, funding_rate

**返回**:
```json
{
  "symbol": "string - 交易对",
  "price": "number - 当前价格",
  "volume_24h": "number - 24小时成交量",
  "volatility_24h": "number - 24小时波动率",
  "change_24h": "number - 24小时涨跌",
  "orderbook": "object - 订单簿",
  "timestamp": "string - 时间戳"
}
```

**示例**:
```javascript
// 用户: "BTC 现在什么价格？"
agent.executeTool("get_market_data", {
  symbol: "BTCUSDT",
  fields: ["price", "volatility"]
})
```

---

#### 15. analyze_trend
**功能**: 分析市场趋势

**参数**:
```json
{
  "symbol": "string - 交易对 (必需)",
  "period": "string - 分析周期 (必需)",
  "indicators": "array - 技术指标 (可选)"
}
```

**周期**: 1h, 4h, 1d, 1w

**指标**: EMA, MACD, RSI, ADX, Bollinger Bands

**返回**:
```json
{
  "trend_direction": "string - 趋势方向 (up/down/sideways)",
  "trend_strength": "number - 趋势强度 (0-1)",
  "support_level": "number - 支撑位",
  "resistance_level": "number - 阻力位",
  "signals": "array - 交易信号"
}
```

**示例**:
```javascript
// 用户: "分析一下 BTC 的趋势"
agent.executeTool("analyze_trend", {
  symbol: "BTCUSDT",
  period: "1d",
  indicators: ["EMA", "MACD", "RSI"]
})
```

---

### 系统操作类 (System Tools)

#### 16. get_bot_list
**功能**: 获取所有 Bot 列表

**参数**: 无

**示例**:
```javascript
// 用户: "查看所有的 Bot"
agent.executeTool("get_bot_list", {})
```

---

#### 17. get_bot_status
**功能**: 获取 Bot 运行状态

**参数**:
```json
{
  "bot_id": "string - Bot ID (必需)"
}
```

**返回**:
```json
{
  "bot_id": "string",
  "status": "string - 状态 (running/stopped/error)",
  "strategy": "object - 策略信息",
  "performance": "object - 性能指标",
  "uptime": "number - 运行时长"
}
```

**示例**:
```javascript
// 用户: "Bot 1 现在状态怎么样？"
agent.executeTool("get_bot_status", {
  bot_id: "bot_1"
})
```

---

## 🎯 Skills (高级功能组合)

Skills 是多个 Tools 的组合，用于完成复杂的配置任务。

### Skill 1: configure_grid_strategy
**描述**: 配置网格策略的完整流程

**执行步骤**:
1. get_market_data - 获取市场数据
2. suggest_parameters - 获取参数建议
3. validate_parameters - 验证参数
4. assess_risk - 评估风险
5. create_strategy - 创建策略

**对话示例**:
```
用户: "帮我配置一个 BTC 网格策略，投入 $10,000"

Agent: 我来帮您配置 BTC 网格策略。

[步骤 1/5] 获取市场数据...
当前 BTC 价格: $45,234
24h 波动率: 3.2%

[步骤 2/5] 生成参数建议...
基于市场条件，建议:
- 价格区间: $43,000 - $47,000
- 网格数量: 20 格
- 单格投入: $100

[步骤 3/5] 验证参数...
✓ 参数验证通过

[步骤 4/5] 评估风险...
风险等级: 中等
风险评分: 45/100

[步骤 5/5] 创建策略...
✓ 策略创建成功

配置摘要:
- 策略 ID: grid_btc_001
- 预计日收益: ~$150-200
- 最大回撤: ~$400

是否应用此配置？
```

---

### Skill 2: optimize_existing_strategy
**描述**: 优化现有策略参数

**执行步骤**:
1. get_parameters - 获取当前参数
2. run_backtest - 回测当前参数
3. optimize_parameters - 参数优化
4. compare_results - 对比结果
5. update_strategy - 应用优化

**对话示例**:
```
用户: "优化一下我的网格策略参数"

Agent: 我来帮您优化网格策略参数。

[步骤 1/5] 获取当前参数...
当前参数: price_interval=5, grid_count=20

[步骤 2/5] 回测当前配置...
当前配置收益: +$1,200

[步骤 3/5] 优化参数...
优化中...测试 20 组参数组合
找到最优解: price_interval=8, grid_count=15

[步骤 4/5] 对比结果...
┌─────────────┬──────────┬──────────┐
│    指标      │ 当前     │ 优化后   │
├─────────────┼──────────┼──────────┤
│ 总收益      │ $1,200   │ $1,450   │
│ 最大回撤    │ $800     │ $550     │
│ 夏普比率    │ 1.2      │ 1.5      │
└─────────────┴──────────┴──────────┘

[步骤 5/5] 应用优化...
✓ 优化后的参数已应用

预计收益提升: +20.8%
是否保存此配置？
```

---

### Skill 3: setup_volatility_protection
**描述**: 设置波动率保护和自动暂停开仓（新增）

**执行步骤**:
1. get_volatility_preset - 获取波动率预设
2. analyze_trend - 分析当前市场趋势
3. configure_volatility_detection - 配置波动率检测
4. validate_parameters - 验证配置
5. generate_summary - 生成配置摘要

**对话示例**:
```
用户: "为我的黄金网格策略设置波动率保护"

Agent: 好的！我来为黄金 (PAXGUSDT) 设置波动率保护。

[步骤 1/5] 获取波动率预设...
📊 黄金预设: 低<0.5%, 正常<1.5%, 高<3%, 极端≥6%
💡 黄金波动相对温和，适合中等保护策略

[步骤 2/5] 分析当前市场趋势...
当前趋势: 横盘整理
建议: 启用高波动暂停即可

[步骤 3/5] 配置波动率检测...
✓ 启用波动率检测
✓ 高波动时自动暂停开仓
✓ 极端波动时自动暂停开仓
✓ 波动率回归正常时自动恢复

[步骤 4/5] 验证配置...
✓ 配置验证通过

[步骤 5/5] 配置摘要...
🛡️ 保护策略已启用

预期效果:
• 正常波动时策略正常运行
• 高波动时自动暂停开仓，保护资金
• 极端波动时立即暂停，避免大亏
• 波动率回归正常时自动恢复

是否应用此配置？
```

---

### Skill 4: setup_hybrid_strategy
**描述**: 设置混合策略（网格 + 趋势过滤）

**执行步骤**:
1. analyze_trend - 分析趋势
2. create_strategy - 创建网格策略
3. create_strategy - 创建趋势策略
4. suggest_parameters - 建议协作规则
5. validate_parameters - 验证配置

**对话示例**:
```
用户: "我要一个智能策略：上涨时只卖不买，下跌时正常网格"

Agent: 明白！您需要混合策略：网格 + 趋势过滤。

[步骤 1/5] 分析趋势特征...
BTC 近期趋势: 震荡上行
建议使用 EMA(12,26) 判断趋势

[步骤 2/5] 创建网格策略...
✓ 主策略 (网格) 已创建

[步骤 3/5] 创建趋势策略...
✓ 信号策略 (趋势) 已创建

[步骤 4/5] 配置协作规则...
规则 1: 强势上涨 → 扩大网格 50%
规则 2: 趋势转弱 → 停止买入
规则 3: 明确下跌 → 只卖不买

[步骤 5/5] 验证配置...
✓ 配置验证通过

回测对比 (过去 30 天):
┌─────────────┬──────────┬──────────┐
│    指标      │ 单网格   │ 混合策略 │
├─────────────┼──────────┼──────────┤
│ 总收益      │ $1,200   │ $1,850   │
│ 最大回撤    │ $800     │ $450     │
│ 夏普比率    │ 1.2      │ 1.8      │
└─────────────┴──────────┴──────────┘

混合策略在上涨行情中表现更好，是否应用？
```

---

### Skill 5: risk_check_and_adjust
**描述**: 风险检查和调整

**执行步骤**:
1. get_bot_status - 获取 Bot 状态
2. assess_risk - 评估风险
3. get_market_data - 获取市场数据
4. suggest_parameters - 调整建议
5. update_strategy - 应用调整

**对话示例**:
```
用户: "检查一下我的策略风险"

Agent: 正在检查您的策略风险...

[步骤 1/5] 获取 Bot 状态...
Bot 状态: 运行中
当前仓位: 多头 0.5 BTC
未实现盈亏: +$250

[步骤 2/5] 风险评估...
风险等级: 中等 ⚠️
风险评分: 55/100

风险因素:
1. 仓位集中度: 中等 (影响: 0.6)
2. 市场波动性: 高 (影响: 0.8)
3. 止损设置: 未设置 (影响: 0.9)

[步骤 3/5] 市场分析...
当前市场波动率: 4.5% (较高)
趋势: 震荡上行

[步骤 4/5] 调整建议...
建议措施:
1. 设置止损: 15%
2. 减少仓位: 30%
3. 启用趋势过滤

[步骤 5/5] 准备调整...
是否应用风险调整措施？

可选措施:
- [ ] 立即止损平仓
- [ ] 减少仓位
- [ ] 设置自动止损
- [ ] 暂停新开仓
```

---

## 📊 Tool 风险等级分类

### 无风险 (None)
- get_parameters
- get_market_data
- analyze_trend
- get_bot_list
- get_bot_status
- get_volatility_preset
- list_volatility_presets

### 低风险 (Low)
- suggest_parameters
- create_strategy
- clone_strategy
- run_backtest
- compare_results
- optimize_parameters
- calculate_position
- validate_parameters

### 中等风险 (Medium)
- set_parameter
- update_strategy
- assess_risk
- configure_volatility_detection

### 高风险 (High)
- delete_strategy
- 需要大额资金调整的操作

### 严重风险 (Critical)
- 清空所有策略
- 大额资金转移
- 杠杆调整到危险水平

## 🔧 Tool 扩展

### 添加自定义 Tool

```go
// 1. 实现 Tool 接口
type MyCustomTool struct {
    BaseTool
}

func (t *MyCustomTool) Execute(ctx context.Context, params map[string]interface{}) (types.ToolResult, error) {
    // 实现逻辑
    return types.ToolResult{Result: ...}, nil
}

// 2. 注册到 ToolRegistry
toolRegistry.Register(NewMyCustomTool())
```

### 创建自定义 Skill

```go
// Skill 是多个 Tool 的编排
func (agent *QuantMeshAgent) ExecuteSkill(skillName string, params map[string]interface{}) error {
    switch skillName {
    case "my_custom_skill":
        return agent.executeMyCustomSkill(params)
    // ...
    }
}
```

## 📝 总结

QuantMesh AI Agent 内置了：

- **20 个核心 Tools** - 覆盖参数、策略、回测、风险、市场、波动率等各个方面
- **5 个高级 Skills** - 完成复杂的多步骤配置任务
- **5 级风险评估** - 确保操作安全
- **可扩展架构** - 轻松添加自定义 Tools 和 Skills
- **内置波动率预设** - 针对不同品种的优化配置

这些 Tools 和 Skills 让用户可以通过自然语言完成几乎所有策略配置和管理任务！

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
