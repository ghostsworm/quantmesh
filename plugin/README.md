# QuantMesh 插件系统

## 📖 概述

QuantMesh 插件系统允许你扩展系统功能，支持以下类型的插件：

- **策略插件**: 自定义交易策略
- **AI插件**: 市场分析和预测
- **风控插件**: 自定义风险控制逻辑
- **信号源插件**: 外部信号接入 (如 Polymarket)

## 🏗️ 架构设计

### 插件类型

```
开源插件 (免费)
├── 示例策略
├── 基础工具
└── 社区贡献

闭源插件 (商业许可)
├── 高级AI策略
├── 机器学习优化
├── 专业信号源
└── 企业级功能
```

### 工作原理

```
1. 编译时链接
   ├── 开源插件: 直接编译到主程序
   └── 闭源插件: 作为独立的 Go 包引入

2. 运行时加载
   ├── 插件注册到全局注册表
   ├── 许可证验证 (商业插件)
   └── 动态初始化和启动

3. 许可证保护
   ├── 加密存储
   ├── 签名验证
   └── 机器绑定 (可选)
```

## 🚀 快速开始

### 1. 创建免费插件

```go
package myplugin

import (
    "quantmesh/plugin"
    "quantmesh/strategy"
)

// 定义插件
type MyStrategyPlugin struct {
    metadata *plugin.PluginMetadata
    strategy strategy.Strategy
}

func NewMyStrategyPlugin() *MyStrategyPlugin {
    return &MyStrategyPlugin{
        metadata: &plugin.PluginMetadata{
            Name:        "my_strategy",
            Version:     "1.0.0",
            Author:      "Your Name",
            Description: "我的自定义策略",
            Type:        plugin.PluginTypeStrategy,
            License:     "free",
            RequiresKey: false,
        },
    }
}

// 实现 Plugin 接口
func (p *MyStrategyPlugin) GetMetadata() *plugin.PluginMetadata {
    return p.metadata
}

func (p *MyStrategyPlugin) Initialize(cfg *config.Config, params map[string]interface{}) error {
    // 初始化逻辑
    p.strategy = NewMyStrategy()
    return nil
}

func (p *MyStrategyPlugin) Validate(licenseKey string) error {
    return nil // 免费插件不需要验证
}

func (p *MyStrategyPlugin) GetStrategy() strategy.Strategy {
    return p.strategy
}

func (p *MyStrategyPlugin) Close() error {
    return p.strategy.Stop()
}
```

### 2. 注册和使用插件

```go
package main

import (
    "quantmesh/plugin"
    "myplugin"
)

func main() {
    // 创建插件加载器
    loader := plugin.NewPluginLoader(cfg)
    
    // 加载免费插件
    myPlugin := myplugin.NewMyStrategyPlugin()
    err := loader.LoadStrategyPlugin(
        myPlugin,
        "",  // 免费插件不需要许可证
        map[string]interface{}{
            "weight": 1.0,
            "fixed_pool": 1000.0,
        },
        strategyManager,
        executor,
        exchange,
    )
    
    if err != nil {
        log.Fatal(err)
    }
}
```

### 3. 创建商业插件

```go
type PremiumPlugin struct {
    metadata  *plugin.PluginMetadata
    validator *plugin.LicenseValidator
}

func NewPremiumPlugin() *PremiumPlugin {
    return &PremiumPlugin{
        metadata: &plugin.PluginMetadata{
            Name:        "premium_plugin",
            Version:     "2.0.0",
            Type:        plugin.PluginTypeStrategy,
            License:     "commercial",
            RequiresKey: true, // 需要许可证
        },
        validator: plugin.NewLicenseValidator(),
    }
}

func (p *PremiumPlugin) Validate(licenseKey string) error {
    // 验证许可证
    return p.validator.ValidatePlugin(p.metadata.Name, licenseKey)
}
```

### 4. 使用商业插件

```go
// 加载商业插件
premiumPlugin := NewPremiumPlugin()

// 需要提供有效的许可证密钥
licenseKey := "eyJwbHVnaW5fbmFtZSI6InByZW1pdW1fcGx1Z2luIiwiY3VzdG9tZXJfaWQiOiJDVVNUMDAxIi4uLg=="

err := loader.LoadStrategyPlugin(
    premiumPlugin,
    licenseKey, // 商业许可证
    params,
    strategyManager,
    executor,
    exchange,
)
```

## 🔐 许可证系统

### 许可证生成 (服务器端)

```go
package main

import (
    "time"
    "quantmesh/plugin"
)

func generateLicense() {
    licenseKey, err := plugin.GenerateLicense(
        "premium_ai_strategy",           // 插件名称
        "CUST001",                       // 客户ID
        time.Now().AddDate(1, 0, 0),    // 1年有效期
        5,                               // 最多5个实例
        []string{"ai", "optimization"},  // 授权功能
        "abc123def456",                  // 机器ID (可选)
        "your-secret-key",               // 密钥
    )
    
    if err != nil {
        panic(err)
    }
    
    fmt.Println("许可证密钥:", licenseKey)
}
```

### 许可证验证 (客户端)

```go
validator := plugin.NewLicenseValidator()

// 验证许可证
err := validator.ValidatePlugin("premium_ai_strategy", licenseKey)
if err != nil {
    log.Fatal("许可证验证失败:", err)
}

// 检查功能授权
if validator.CheckFeature("premium_ai_strategy", "ai") {
    // 使用AI功能
}
```

### 许可证格式

```json
{
  "plugin_name": "premium_ai_strategy",
  "customer_id": "CUST001",
  "expiry_date": "2026-12-31T23:59:59Z",
  "max_instances": 5,
  "features": ["ai", "optimization", "backtesting"],
  "issued_at": "2025-01-01T00:00:00Z",
  "machine_id": "abc123def456",
  "signature": "a1b2c3d4e5f6..."
}
```

## 📦 插件分发

### 方案1: 编译时链接 (推荐)

**开源插件**:
```bash
# 用户直接编译
git clone https://github.com/yourname/quantmesh-plugin-example
cd quantmesh_market_maker
go build -o quantmesh
```

**闭源插件**:
```bash
# 提供预编译的 .a 静态库
# 或提供加密的 Go 源码包

# 客户购买后获得访问权限
go get github.com/quantmesh-pro/premium-ai-strategy@latest
go build -o quantmesh
```

### 方案2: Go Plugin (动态库)

```go
// 编译插件为 .so 文件
go build -buildmode=plugin -o premium.so premium_plugin.go

// 运行时加载
p, err := plugin.Open("premium.so")
if err != nil {
    panic(err)
}

symbol, err := p.Lookup("NewPremiumPlugin")
if err != nil {
    panic(err)
}

newPlugin := symbol.(func() plugin.StrategyPlugin)
premiumPlugin := newPlugin()
```

**注意**: Go Plugin 仅支持 Linux/macOS，且版本兼容性要求严格。

### 方案3: gRPC 插件 (进程隔离)

```
主程序 (quantmesh)
    ↓ gRPC
插件进程 (premium-plugin-server)
```

优点: 完全隔离，跨语言支持
缺点: 性能开销，复杂度高

## 🛡️ 安全措施

### 1. 代码混淆

```bash
# 使用 garble 混淆 Go 代码
go install mvdan.cc/garble@latest
garble build -o premium_plugin.a
```

### 2. 许可证加密

- AES-256-GCM 加密存储
- SHA-256 签名验证
- 机器ID绑定

### 3. 网络验证 (可选)

```go
// 在线验证许可证
func (v *LicenseValidator) ValidateOnline(licenseKey string) error {
    resp, err := http.Post(
        "https://license.quantmesh.com/validate",
        "application/json",
        bytes.NewBuffer([]byte(licenseKey)),
    )
    // 处理响应
}
```

## 📝 配置文件

```yaml
# config.yaml
plugins:
  enabled: true
  directory: "./plugins"
  
  # 插件列表
  plugins:
    - name: "example_strategy"
      enabled: true
      license_key: ""  # 免费插件不需要
      params:
        weight: 1.0
        fixed_pool: 1000.0
    
    - name: "premium_ai_strategy"
      enabled: true
      license_key: "eyJwbHVnaW5fbmFtZSI6InByZW1pdW1fYWlfc3RyYXRlZ3kiLCJjdXN0b21lcl9pZCI6IkNVU1QwMDEiLCJleHBpcnlfZGF0ZSI6IjIwMjYtMTItMzFUMjM6NTk6NTlaIiwibWF4X2luc3RhbmNlcyI6NSwi..."
      params:
        weight: 2.0
        ai_model: "gpt-4"
```

## 🔧 开发工具

### 插件脚手架

```bash
# 创建新插件
./scripts/create_plugin.sh my_strategy

# 生成许可证
./scripts/generate_license.sh premium_ai_strategy CUST001 365

# 验证插件
./scripts/validate_plugin.sh my_strategy
```

### 测试插件

```go
func TestMyPlugin(t *testing.T) {
    plugin := NewMyStrategyPlugin()
    
    // 测试元数据
    metadata := plugin.GetMetadata()
    assert.Equal(t, "my_strategy", metadata.Name)
    
    // 测试初始化
    err := plugin.Initialize(cfg, nil)
    assert.NoError(t, err)
    
    // 测试策略
    strategy := plugin.GetStrategy()
    assert.NotNil(t, strategy)
}
```

## 📚 示例项目

### 开源示例
- `examples/example_strategy_plugin.go` - 基础策略插件
- `examples/signal_plugin.go` - 信号源插件
- `examples/risk_plugin.go` - 风控插件

### 商业插件 (需要购买)
- `quantmesh-pro/ai-strategy` - AI驱动策略
- `quantmesh-pro/ml-optimizer` - 机器学习优化
- `quantmesh-pro/sentiment-analyzer` - 情绪分析

## 🤝 社区贡献

欢迎贡献开源插件！

1. Fork 项目
2. 创建插件分支: `git checkout -b plugin/my-strategy`
3. 提交代码: `git commit -am 'Add my strategy plugin'`
4. 推送分支: `git push origin plugin/my-strategy`
5. 创建 Pull Request

## 📞 商业支持

购买商业插件或定制开发:
- 📧 Email: commercial@quantmesh.com
- 🌐 Website: https://quantmesh.com/plugins
- 💬 Telegram: @quantmesh_support

## ⚖️ 许可证

- 插件系统框架: AGPL-3.0 (开源)
- 商业插件: 专有许可证 (需购买)

