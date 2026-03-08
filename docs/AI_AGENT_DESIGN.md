# QuantMesh AI Agent 系统设计文档

## 概述

本文档描述了 QuantMesh 的对话式策略配置 AI Agent 系统的设计和实现计划。该系统允许用户通过自然语言对话来配置交易策略，类似于 OpenHands/Claude Code 的交互方式。

## 1. 系统架构

### 1.1 整体架构图

```
┌─────────────────────────────────────────────────────────────────┐
│                         用户界面层                                │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐          │
│  │  聊天界面     │  │  配置预览     │  │  策略可视化    │          │
│  │  (Chat UI)   │  │(Config View) │  │(Visualization)│          │
│  └──────────────┘  └──────────────┘  └──────────────┘          │
└─────────────────────────────────────────────────────────────────┘
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│                      Agent 编排层                                 │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │           QuantMeshAgent (主 Agent)                       │   │
│  │  - 管理对话状态                                           │   │
│  │  - 协调子 Agent                                           │   │
│  │  - 执行 Tool Call                                         │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                   │                              │
│                                   ▼                              │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐          │
│  │策略配置 Agent │  │回测分析 Agent │  │风险评估 Agent │          │
│  └──────────────┘  └──────────────┘  └──────────────┘          │
└─────────────────────────────────────────────────────────────────┘
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│                      Tool 执行层                                  │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐           │
│  │参数管理  │ │策略操作  │ │回测执行  │ │风险分析  │           │
│  │  Tools   │ │  Tools   │ │  Tools   │ │  Tools   │           │
│  └──────────┘ └──────────┘ └──────────┘ └──────────┘           │
└─────────────────────────────────────────────────────────────────┘
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│                      核心服务层                                   │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐           │
│  │配置管理  │ │策略引擎  │ │回测系统  │ │风险控制  │           │
│  └──────────┘ └──────────┘ └──────────┘ └──────────┘           │
└─────────────────────────────────────────────────────────────────┘
```

### 1.2 核心组件

#### 1.2.1 Agent 框架 (agent/)

```go
package agent

// Agent 核心接口
type Agent interface {
    // 处理用户消息
    ProcessMessage(ctx context.Context, msg Message) (Response, error)

    // 执行工具调用
    ExecuteTool(ctx context.Context, call ToolCall) (ToolResult, error)

    // 管理对话状态
    GetState() ConversationState
    SetState(state ConversationState) error

    // 暂停/恢复
    Pause() error
    Resume() error
}

// 主 Agent 实现
type QuantMeshAgent struct {
    conversation *ConversationManager
    tools        *ToolRegistry
    planner      *TODOPlanner
    security     *SecurityAnalyzer
    llm          LLMClient
    subAgents    map[string]Agent

    mu           sync.RWMutex
    state        AgentState
}

// 对话状态管理（事件溯源）
type ConversationManager struct {
    state        ConversationState
    eventLog     []ConfigEvent
    contextMgr   *ContextManager

    // 事件存储
    eventStore   EventStore
}

// 事件定义
type ConfigEvent struct {
    ID          string                 `json:"id"`
    Timestamp   time.Time              `json:"timestamp"`
    Type        EventType              `json:"type"`
    Action      ConfigAction           `json:"action"`
    Result      ConfigResult           `json:"result"`
    Risk        SecurityLevel          `json:"risk"`
    Metadata    map[string]interface{} `json:"metadata"`
}

type EventType string

const (
    EventTypeUserMessage      EventType = "user_message"
    EventTypeAgentResponse    EventType = "agent_response"
    EventTypeToolCall         EventType = "tool_call"
   EventTypeParameterChange   EventType = "parameter_change"
    EventTypeValidationError  EventType = "validation_error"
    EventTypeRiskWarning      EventType = "risk_warning"
    EventTypeConfigApplied    EventType = "config_applied"
)

// TODO 列表规划器
type TODOPlanner struct {
    items      []ConfigTask
    current    int
    completed  []string

    mu         sync.Mutex
}

type ConfigTask struct {
    ID          string       `json:"id"`
    Content     string       `json:"content"`
    Status      TaskStatus   `json:"status"`
    Priority    int          `json:"priority"`
    Dependencies []string    `json:"dependencies,omitempty"`
    Context     TaskContext  `json:"context"`
}

type TaskStatus string

const (
    TaskStatusPending    TaskStatus = "pending"
    TaskStatusInProgress TaskStatus = "in_progress"
    TaskStatusCompleted  TaskStatus = "completed"
    TaskStatusFailed     TaskStatus = "failed"
    TaskStatusSkipped    TaskStatus = "skipped"
)
```

#### 1.2.2 Tool 系统 (agent/tools/)

```go
package tools

// Tool 基础接口
type Tool interface {
    // 工具元信息
    Name() string
    Description() string
    Category() ToolCategory

    // 参数 Schema
    ParameterSchema() JSONSchema

    // 执行工具
    Execute(ctx context.Context, params map[string]interface{}) (ToolResult, error)

    // 风险评估
    AssessRisk(params map[string]interface{}) SecurityLevel
}

// Tool 注册表
type ToolRegistry struct {
    tools      map[string]Tool
    permissions map[string]SecurityLevel

    mu         sync.RWMutex
}

// 核心 Tool 类别
type ToolCategory string

const (
    CategoryParameter   ToolCategory = "parameter"    // 参数管理
    CategoryStrategy    ToolCategory = "strategy"     // 策略操作
    CategoryBacktest    ToolCategory = "backtest"     // 回测执行
    CategoryRisk        ToolCategory = "risk"         // 风险分析
    CategoryMarket      ToolCategory = "market"       // 市场数据
    CategorySystem      ToolCategory = "system"       // 系统操作
)
```

#### 1.2.3 具体工具实现

```go
// 参数管理工具
type GetParametersTool struct {
    configStore ConfigStore
}

type SetParameterTool struct {
    configStore  ConfigStore
    validator    *ParameterValidator
    security     *SecurityAnalyzer
}

type ValidateParametersTool struct {
    validator *ParameterValidator
}

// 策略操作工具
type CreateStrategyTool struct {
    strategyMgr *StrategyManager
}

type UpdateStrategyTool struct {
    strategyMgr *StrategyManager
}

type DeleteStrategyTool struct {
    strategyMgr *StrategyManager
}

// 回测工具
type RunBacktestTool struct {
    backtestEngine *BacktestEngine
}

type CompareResultsTool struct {
    backtestEngine *BacktestEngine
}

// 风险分析工具
type AssessRiskTool struct {
    riskAnalyzer *RiskAnalyzer
}

type SuggestParametersTool struct {
    optimizer *ParameterOptimizer
}

// 市场数据工具
type GetMarketDataTool struct {
    marketData *MarketDataService
}

type AnalyzeTrendTool struct {
    trendAnalyzer *TrendAnalyzer
}

// 文档工具
type ExplainParameterTool struct {
    knowledgeBase *KnowledgeBase
}

type GetStrategyDocsTool struct {
    knowledgeBase *KnowledgeBase
}
```

### 1.3 LLM 集成

```go
package llm

// LLM 客户端接口
type LLMClient interface {
    // 生成响应（支持 Tool Call）
    Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error)

    // 流式生成
    GenerateStream(ctx context.Context, req GenerateRequest) (<-chan GenerateChunk, error)
}

type GenerateRequest struct {
    Messages      []Message              `json:"messages"`
    Tools         []ToolDefinition       `json:"tools,omitempty"`
    Temperature   float64                `json:"temperature,omitempty"`
    MaxTokens     int                    `json:"max_tokens,omitempty"`
    SystemPrompt  string                 `json:"system_prompt,omitempty"`
}

type Message struct {
    Role      string         `json:"role"`    // system, user, assistant, tool
    Content   string         `json:"content"`
    ToolCalls []ToolCall     `json:"tool_calls,omitempty"`
    ToolID    string         `json:"tool_id,omitempty"`
}

type ToolCall struct {
    ID       string                 `json:"id"`
    Name     string                 `json:"name"`
    Arguments map[string]interface{} `json:"arguments"`
}

type GenerateResponse struct {
    Message      string    `json:"message"`
    ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
    FinishReason string    `json:"finish_reason"`
    Usage        TokenUsage `json:"usage"`
}

// 支持的 LLM 提供商
type LLMProvider string

const (
    ProviderClaude     LLMProvider = "claude"
    ProviderOpenAI     LLMProvider = "openai"
    ProviderGemini     LLMProvider = "gemini"
    ProviderDeepSeek   LLMProvider = "deepseek"
    ProviderOpenRouter LLMProvider = "openrouter"
)

// Claude 实现
type ClaudeClient struct {
    apiKey  string
    baseURL string
    client  *http.Client
}

// OpenAI 实现
type OpenAIClient struct {
    apiKey  string
    baseURL string
    client  *http.Client
}
```

## 2. 对话流程设计

### 2.1 基础对话流程

```
用户: "我想配置一个 BTC 的网格策略"
  │
  ├─> Agent 分析意图
  │   └─> 识别: 创建网格策略
  │   └─> 创建 TODO: ["获取市场数据", "确定参数", "配置策略", "验证参数"]
  │
  ├─> 执行 Tool: get_market_data
  │   └─> BTC 当前价格: $45,000
  │   └─> 24h 波动率: 3.2%
  │
  ├─> Agent 响应
  │   └─> "好的！BTC 当前价格 $45,000，24h 波动率 3.2%。
  │        基于市场情况，我建议：
  │        - 价格区间: $42,000 - $48,000
  │        - 网格数量: 20 格
  │        - 投入金额: 您计划投入多少？"
  │
用户: "$10,000"
  │
  ├─> Agent 更新 TODO
  │   └─> ["获取市场数据 ✓", "确定参数 (进行中)", "配置策略", "验证参数"]
  │
  ├─> 执行 Tool: validate_parameters
  │   └─> 参数验证通过
  │   └─> 风险评估: 中等风险
  │
  ├─> Agent 响应
  │   └─> "收到！投入 $10,000，20 格网格。
  │        预计收益:
  │        - 单格利润: ~$50
  │        - 日均交易: ~40 次
  │        - 预期日收益: ~$200 (2%)
  │
  │        是否要调整任何参数？还是直接应用配置？"
```

### 2.2 复杂配置流程

```
用户: "我要做一个 BTC 的策略，趋势上涨的时候只卖不买，下跌的时候正常网格"
  │
  ├─> Agent 分析
  │   └─> 识别: 混合策略 (网格 + 趋势过滤)
  │   └─> 创建 TODO: [
  │        "配置网格策略",
  │        "配置趋势策略",
  │        "设置协作规则",
  │        "验证配置",
  │        "运行回测"
  │      ]
  │
  ├─> 子 Agent 协作
  │   ├─> 策略配置 Agent: 配置网格参数
  │   ├─> 趋势分析 Agent: 设置趋势判断
  │   └─> 风险评估 Agent: 分析组合风险
  │
  ├─> 执行 Tool: create_hybrid_strategy
  │   ├─> 主策略: grid_primary
  │   ├─> 信号策略: trend_signal
  │   └─> 协作规则:
  │       - 趋势向上 → deny_open(BUY)
  │       - 趋势向下 → resume_all
  │
  ├─> 执行 Tool: run_backtest
  │   └─> 回测过去 30 天数据
  │   └─> 对比: 单网格 vs 混合策略
  │
  └─> Agent 响应
      └─> "配置完成！回测结果对比：
           ┌─────────────┬──────────┬──────────┐
           │    指标      │ 单网格   │ 混合策略 │
           ├─────────────┼──────────┼──────────┤
           │ 总收益      │ +$1,200  │ +$1,850  │
           │ 最大回撤    │ -$800    │ -$450    │
           │ 夏普比率    │ 1.2      │ 1.8      │
           └─────────────┴──────────┴──────────┘

           混合策略在上涨行情中表现更好，
           是否应用此配置？"
```

## 3. Tool 定义规范

### 3.1 Tool Schema 定义

```json
{
  "name": "set_parameter",
  "description": "设置策略参数值",
  "category": "parameter",
  "parameters": {
    "type": "object",
    "properties": {
      "strategy_id": {
        "type": "string",
        "description": "策略 ID"
      },
      "parameter": {
        "type": "string",
        "description": "参数名称",
        "enum": ["price_interval", "grid_count", "order_quantity", "stop_loss", "take_profit"]
      },
      "value": {
        "type": "number",
        "description": "参数值"
      }
    },
    "required": ["strategy_id", "parameter", "value"]
  },
  "risk_level": "medium",
  "requires_confirmation": false
}
```

### 3.2 核心 Tools 列表

#### 3.2.1 参数管理类

| Tool Name | Description | Risk |
|-----------|-------------|------|
| `get_parameters` | 获取策略所有参数 | Low |
| `get_parameter` | 获取单个参数详情 | Low |
| `set_parameter` | 设置参数值 | Medium |
| `validate_parameters` | 验证参数组合 | Low |
| `suggest_parameters` | 智能参数建议 | Low |
| `reset_parameters` | 重置为默认值 | Medium |

#### 3.2.2 策略操作类

| Tool Name | Description | Risk |
|-----------|-------------|------|
| `create_strategy` | 创建新策略 | Low |
| `update_strategy` | 更新策略配置 | Medium |
| `delete_strategy` | 删除策略 | High |
| `clone_strategy` | 克隆策略 | Low |
| `enable_strategy` | 启用策略 | Low |
| `disable_strategy` | 禁用策略 | Medium |

#### 3.2.3 回测分析类

| Tool Name | Description | Risk |
|-----------|-------------|------|
| `run_backtest` | 运行回测 | Low |
| `compare_results` | 对比回测结果 | Low |
| `optimize_parameters` | 参数优化 | Low |
| `analyze_performance` | 性能分析 | Low |

#### 3.2.4 风险分析类

| Tool Name | Description | Risk |
|-----------|-------------|------|
| `assess_risk` | 评估策略风险 | Low |
| `calculate_position` | 计算仓位 | Low |
| `estimate_drawdown` | 估算回撤 | Low |
| `stress_test` | 压力测试 | Low |

#### 3.2.5 市场数据类

| Tool Name | Description | Risk |
|-----------|-------------|------|
| `get_market_data` | 获取市场数据 | Low |
| `analyze_trend` | 分析趋势 | Low |
| `get_volatility` | 获取波动率 | Low |
| `get_liquidity` | 获取流动性 | Low |

## 4. 安全和风控

### 4.1 风险等级定义

```go
type SecurityLevel int

const (
    SecurityLevelNone     SecurityLevel = 0  // 无风险
    SecurityLevelLow      SecurityLevel = 1  // 低风险
    SecurityLevelMedium   SecurityLevel = 2  // 中等风险
    SecurityLevelHigh     SecurityLevel = 3  // 高风险
    SecurityLevelCritical SecurityLevel = 4  // 严重风险
)
```

### 4.2 确认策略

```go
type ConfirmationPolicy struct {
    // 需要确认的风险等级
    ConfirmAboveLevel SecurityLevel

    // 高风险操作列表
    HighRiskOperations []string

    // 批量操作限制
    BatchOperationLimit int

    // 资金操作阈值
    CapitalChangeThreshold float64
}

func (cp *ConfirmationPolicy) RequiresConfirmation(tool Tool, params map[string]interface{}) bool {
    risk := tool.AssessRisk(params)
    if risk >= cp.ConfirmAboveLevel {
        return true
    }

    for _, op := range cp.HighRiskOperations {
        if tool.Name() == op {
            return true
        }
    }

    if capitalChange, ok := params["capital_change"].(float64); ok {
        if math.Abs(capitalChange) > cp.CapitalChangeThreshold {
            return true
        }
    }

    return false
}
```

## 5. 前端设计

### 5.1 聊天界面组件

```typescript
interface ChatMessage {
  id: string;
  role: 'user' | 'assistant' | 'system';
  content: string;
  timestamp: Date;
  toolCalls?: ToolCall[];
  metadata?: {
    risk?: SecurityLevel;
    confirmations?: Confirmation[];
  };
}

interface ToolCall {
  id: string;
  name: string;
  arguments: Record<string, any>;
  result?: any;
  status: 'pending' | 'executing' | 'completed' | 'failed';
  error?: string;
}

// React 组件
function AgentChatInterface() {
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [input, setInput] = useState('');
  const [isLoading, setIsLoading] = useState(false);
  const [configPreview, setConfigPreview] = useState<StrategyConfig | null>(null);

  // ... 实现
}
```

### 5.2 配置预览组件

```typescript
function ConfigPreview({ config }: { config: StrategyConfig }) {
  return (
    <Card>
      <CardHeader>
        <Heading size="md">配置预览</Heading>
      </CardHeader>
      <CardBody>
        <ParameterList parameters={config.parameters} />
        <RiskAssessment risk={config.risk} />
        <BacktestPreview result={config.backtestResult} />
      </CardBody>
    </Card>
  );
}
```

## 6. 实现路线图

### Phase 1: 基础框架 (2 周)
- [ ] Agent 核心框架
- [ ] Tool Registry
- [ ] LLM Client (Claude)
- [ ] 基础对话管理

### Phase 2: 核心 Tools (2 周)
- [ ] 参数管理 Tools
- [ ] 策略操作 Tools
- [ ] 市场数据 Tools

### Phase 3: 智能特性 (2 周)
- [ ] TODO Planner
- [ ] 风险评估系统
- [ ] 参数建议引擎
- [ ] 上下文管理

### Phase 4: 前端集成 (2 周)
- [ ] 聊天界面
- [ ] 配置预览
- [ ] 实时更新
- [ ] 响应式设计

### Phase 5: 高级功能 (2 周)
- [ ] 多策略配置
- [ ] 回测集成
- [ ] 参数优化
- [ ] 子 Agent 协作

### Phase 6: 测试和优化 (2 周)
- [ ] 单元测试
- [ ] 集成测试
- [ ] 性能优化
- [ ] 用户测试

## 7. 示例对话

### 示例 1: 简单网格配置

```
User: 帮我配一个 BTC 的网格策略

Agent: 好的！我来帮您配置 BTC 网格策略。

[执行 get_market_data]
当前 BTC 价格: $45,234
24h 波动率: 3.2%
建议价格区间: $43,000 - $47,000

基础参数建议：
- 网格数量: 15-20 格
- 单格投入: $100-200
- 投入总额: 您计划投入多少？

User: $10,000

Agent: 收到！投入 $10,000。

[执行 validate_parameters]
✓ 参数验证通过
✓ 风险评估: 中等风险

配置方案：
- 价格区间: $43,000 - $47,000
- 网格数量: 20 格
- 单格投入: $100
- 预计收益: 日均 ~$150-200

是否应用此配置？
```

### 示例 2: 混合策略配置

```
User: 我想要一个智能策略：上涨时只卖不买，下跌时正常网格

Agent: 明白！您需要混合策略：网格 + 趋势过滤。

[创建 TODO]
1. 配置网格策略 ✓
2. 配置趋势策略 ✓
3. 设置协作规则
4. 验证配置
5. 运行回测

[执行 create_hybrid_strategy]
主策略: 网格策略
  - 价格区间: $43,000 - $47,000
  - 网格数量: 20 格

信号策略: 趋势策略
  - 趋势周期: 60 K线
  - 趋势阈值: 0.5

协作规则:
1. 强势上涨 → 扩大网格 50%
2. 趋势转弱 → 停止买入
3. 明确下跌 → 只卖不买

[执行 run_backtest]
回测结果 (过去 30 天):
┌─────────────┬──────────┬──────────┐
│    指标      │ 单网格   │ 混合策略 │
├─────────────┼──────────┼──────────┤
│ 总收益      │ $1,200   │ $1,850   │
│ 最大回撤    │ -$800    │ -$450    │
│ 夏普比率    │ 1.2      │ 1.8      │
└─────────────┴──────────┴──────────┘

混合策略在上涨行情中表现更好，是否应用？

User: 应用

Agent: ✓ 配置已应用！

策略已保存到: bots/smart_grid_btctestnet/config.yaml

您可以：
1. 查看配置详情
2. 运行回测
3. 启动策略
```

## 8. 技术栈

### 后端
- **Go 1.21+**: 主要开发语言
- **gin**: Web API
- **WebSocket**: 实时通信
- **sqlite**: 对话历史存储

### 前端
- **React 18**: UI 框架
- **TypeScript**: 类型安全
- **Chakra UI**: 组件库
- **Vite**: 构建工具

### LLM
- **Claude API**: 主要 LLM
- **OpenAI API**: 备用 LLM
- **LangChain Go**: 工具编排 (可选)

### 基础设施
- **Redis**: 缓存和会话
- **PostgreSQL**: 持久化存储 (可选)
- **Prometheus**: 监控

## 9. API 设计

### 9.1 WebSocket API

```typescript
// 客户端发送消息
{
  "type": "message",
  "content": "帮我配置一个BTC网格策略"
}

// 服务器响应
{
  "type": "response",
  "content": "好的！我来帮您配置...",
  "tool_calls": [
    {
      "id": "call_1",
      "name": "get_market_data",
      "arguments": { "symbol": "BTC" }
    }
  ]
}

// 工具执行结果
{
  "type": "tool_result",
  "call_id": "call_1",
  "result": {
    "price": 45234,
    "volatility": 0.032
  }
}
```

### 9.2 REST API

```typescript
// 创建对话会话
POST /api/agent/sessions
{
  "bot_id": "optional-bot-id",
  "context": "optional-initial-context"
}

// 发送消息
POST /api/agent/sessions/:id/messages
{
  "content": "帮我配置策略",
  "stream": true
}

// 获取对话历史
GET /api/agent/sessions/:id/history

// 获取当前配置
GET /api/agent/sessions/:id/config

// 应用配置
POST /api/agent/sessions/:id/apply
```

## 10. 监控和日志

### 10.1 Agent 行为追踪

```go
type AgentMetrics struct {
    // 对话指标
    TotalConversations    int64         `json:"total_conversations"`
    AverageTurns          int64         `json:"average_turns"`
    SuccessRate           float64       `json:"success_rate"`

    // Tool 使用统计
    ToolUsage             map[string]int `json:"tool_usage"`
    ToolErrors            map[string]int `json:"tool_errors"`
    AverageToolLatency    time.Duration `json:"average_tool_latency"`

    // 用户满意度
    UserRating            float64       `json:"user_rating"`
    FeedbackCount         int64         `json:"feedback_count"`
}
```

### 10.2 调试和审计

```go
// 对话重放
func (cm *ConversationManager) Replay(sessionID string) error {
    events, _ := cm.eventStore.GetEvents(sessionID)

    for _, event := range events {
        logger.Debug("[Replay] Event: %s", event.Type)
        // 重新执行每个事件
    }

    return nil
}

// 导出对话
func (cm *ConversationManager) Export(sessionID string, format string) ([]byte, error) {
    events, _ := cm.eventStore.GetEvents(sessionID)

    switch format {
    case "json":
        return json.MarshalIndent(events, "", "  ")
    case "markdown":
        return cm.exportToMarkdown(events)
    default:
        return nil, fmt.Errorf("unsupported format: %s", format)
    }
}
```

---

## 总结

这个 AI Agent 系统将为 QuantMesh 提供强大而安全的对话式策略配置能力，结合了：

1. **事件溯源架构** - 确保可追溯和可审计
2. **TODO 列表规划** - 提供清晰的配置进度
3. **安全优先设计** - 风险评估和确认策略
4. **智能参数建议** - 基于市场数据的优化
5. **多策略支持** - 简单和复杂策略都能配置

系统设计遵循"简单可靠"的原则，从核心功能开始，逐步添加高级特性。
