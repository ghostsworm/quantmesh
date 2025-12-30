# QuantMesh 插件开发指南

本指南介绍如何为 QuantMesh 开发自定义插件。

## 📋 目录

- [插件系统概述](#插件系统概述)
- [开发环境准备](#开发环境准备)
- [创建插件](#创建插件)
- [插件接口](#插件接口)
- [构建和测试](#构建和测试)
- [License 集成](#license-集成)
- [最佳实践](#最佳实践)

## 插件系统概述

QuantMesh 插件系统允许你扩展系统功能而无需修改核心代码。插件通过 Go 的 `plugin` 包动态加载。

### 插件类型

1. **AI 策略插件**: 提供 AI 驱动的市场分析和决策
2. **多策略插件**: 实现各种交易策略(动量、均值回归等)
3. **风控插件**: 高级风险管理和投资组合优化

## 开发环境准备

### 前置要求

- Go 1.21+
- Linux 或 macOS (Windows 不支持 Go plugins)
- Git

### 安装依赖

```bash
go mod download
```

## 创建插件

### 1. 项目结构

```
my-plugin/
├── go.mod
├── main.go           # 插件入口
├── strategy.go       # 策略实现
└── README.md
```

### 2. 实现插件接口

所有插件必须实现基础接口:

```go
package main

import "context"

// Plugin 基础接口
type Plugin interface {
    Name() string
    Version() string
    Initialize(config map[string]interface{}) error
    Close() error
}

// 实现插件
type MyPlugin struct {
    config map[string]interface{}
}

// NewPlugin 插件入口函数 (必须)
func NewPlugin() interface{} {
    return &MyPlugin{}
}

func (p *MyPlugin) Name() string {
    return "my_plugin"
}

func (p *MyPlugin) Version() string {
    return "1.0.0"
}

func (p *MyPlugin) Initialize(config map[string]interface{}) error {
    p.config = config
    // 初始化逻辑
    return nil
}

func (p *MyPlugin) Close() error {
    // 清理资源
    return nil
}
```

### 3. 实现具体功能

#### AI 策略插件示例

```go
type AIStrategyPlugin struct {
    *MyPlugin
}

func (p *AIStrategyPlugin) AnalyzeMarket(
    ctx context.Context,
    symbol string,
    timeframe string,
) (map[string]interface{}, error) {
    // 实现市场分析逻辑
    return map[string]interface{}{
        "signal": "buy",
        "confidence": 0.85,
        "reason": "Strong uptrend detected",
    }, nil
}

func (p *AIStrategyPlugin) OptimizeParameters(
    ctx context.Context,
    currentParams map[string]interface{},
) (map[string]interface{}, error) {
    // 实现参数优化逻辑
    return map[string]interface{}{
        "price_interval": 2.5,
        "order_quantity": 35.0,
    }, nil
}
```

## 插件接口

### 基础接口

```go
type Plugin interface {
    Name() string
    Version() string
    Initialize(config map[string]interface{}) error
    Close() error
}
```

### AI 策略接口

```go
type AIStrategyPlugin interface {
    Plugin
    AnalyzeMarket(ctx context.Context, symbol string, timeframe string) (map[string]interface{}, error)
    OptimizeParameters(ctx context.Context, currentParams map[string]interface{}) (map[string]interface{}, error)
    AnalyzeRisk(ctx context.Context, position float64, marketData map[string]interface{}) (map[string]interface{}, error)
    MakeDecision(ctx context.Context, marketCondition map[string]interface{}) (string, error)
}
```

### 策略接口

```go
type StrategyPlugin interface {
    Plugin
    GetStrategy(name string) (interface{}, error)
    ListStrategies() []string
    ExecuteStrategy(ctx context.Context, strategyName string, params map[string]interface{}) (map[string]interface{}, error)
}
```

## 构建和测试

### 构建插件

```bash
# 构建为 .so 文件
go build -buildmode=plugin -o my_plugin.so main.go

# 验证插件
file my_plugin.so
```

### 测试插件

```bash
# 复制到 plugins 目录
cp my_plugin.so /path/to/quantmesh/plugins/

# 配置 config.yaml
plugins:
  enabled: true
  directory: "./plugins"
  licenses:
    my_plugin: "YOUR_LICENSE_KEY"
  config:
    my_plugin:
      api_key: "your_api_key"

# 启动 QuantMesh
./quantmesh
```

### 调试

```bash
# 查看日志
tail -f logs/quantmesh.log | grep plugin

# 检查插件是否加载
curl http://localhost:8080/api/plugins
```

## License 集成

### 1. 在插件中验证 License

```go
func (p *MyPlugin) Initialize(config map[string]interface{}) error {
    // License 验证由主程序处理
    // 插件只需要正常初始化
    
    apiKey := config["api_key"].(string)
    if apiKey == "" {
        return errors.New("API key is required")
    }
    
    return nil
}
```

### 2. 获取 License

联系 QuantMesh 团队购买商业 License:

- 📧 Email: contact@quantmesh.io
- 🌐 Website: https://quantmesh.io/commercial

### 3. License 格式

```
BASE64(JSON({
    "plugin_name": "my_plugin",
    "customer_id": "customer123",
    "plan": "professional",
    "expiry_date": "2025-12-31T23:59:59Z",
    "signature": "..."
}))
```

## 最佳实践

### 1. 错误处理

```go
func (p *MyPlugin) AnalyzeMarket(...) (map[string]interface{}, error) {
    defer func() {
        if r := recover(); r != nil {
            log.Printf("Panic in AnalyzeMarket: %v", r)
        }
    }()
    
    // 实现逻辑
}
```

### 2. 并发安全

```go
type MyPlugin struct {
    mu     sync.RWMutex
    cache  map[string]interface{}
}

func (p *MyPlugin) GetData(key string) interface{} {
    p.mu.RLock()
    defer p.mu.RUnlock()
    return p.cache[key]
}
```

### 3. 资源管理

```go
func (p *MyPlugin) Initialize(config map[string]interface{}) error {
    // 初始化资源
    p.httpClient = &http.Client{Timeout: 10 * time.Second}
    return nil
}

func (p *MyPlugin) Close() error {
    // 清理资源
    if p.httpClient != nil {
        p.httpClient.CloseIdleConnections()
    }
    return nil
}
```

### 4. 日志记录

```go
import "log"

func (p *MyPlugin) AnalyzeMarket(...) (map[string]interface{}, error) {
    log.Printf("[%s] Analyzing market for %s", p.Name(), symbol)
    // 实现逻辑
}
```

### 5. 配置验证

```go
func (p *MyPlugin) Initialize(config map[string]interface{}) error {
    required := []string{"api_key", "endpoint"}
    for _, key := range required {
        if _, exists := config[key]; !exists {
            return fmt.Errorf("missing required config: %s", key)
        }
    }
    return nil
}
```

## 示例插件

完整的插件示例请参考:

- [AI 策略插件](https://github.com/quantmesh/quantmesh-premium/tree/main/plugins/ai_strategy)
- [多策略插件](https://github.com/quantmesh/quantmesh-premium/tree/main/plugins/multi_strategy)
- [高级风控插件](https://github.com/quantmesh/quantmesh-premium/tree/main/plugins/advanced_risk)

## 常见问题

### Q: 插件加载失败?

A: 检查:
1. Go 版本是否匹配 (必须与主程序相同)
2. 是否在 Linux/macOS 上构建
3. 是否实现了 `NewPlugin()` 函数
4. License 是否有效

### Q: 如何更新插件?

A: 
1. 构建新版本的 .so 文件
2. 停止 QuantMesh
3. 替换旧的 .so 文件
4. 重启 QuantMesh

### Q: 插件可以访问数据库吗?

A: 可以,但建议通过主程序提供的接口访问,而不是直接访问数据库。

## 技术支持

如有问题,请联系:

- 📧 Email: support@quantmesh.io
- 💬 Discord: https://discord.gg/quantmesh
- 📚 文档: https://docs.quantmesh.io

---

Copyright © 2025 QuantMesh Team. All Rights Reserved.

