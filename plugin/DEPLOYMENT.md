# 商业插件部署方案

## 🎯 目标

为 QuantMesh 提供安全、可控的商业插件分发和授权机制。

## 📦 推荐方案：私有 Go Module + 许可证验证

### 方案架构

```
┌─────────────────────────────────────────────────────────────┐
│                    QuantMesh 开源核心                        │
│  (GitHub Public: github.com/yourname/quantmesh)             │
│  - 基础框架                                                  │
│  - 简单策略                                                  │
│  - 插件系统                                                  │
└─────────────────────────────────────────────────────────────┘
                          ↓ 导入
┌─────────────────────────────────────────────────────────────┐
│              商业插件 (私有 Go Module)                       │
│  (GitHub Private: github.com/quantmesh-pro/premium-ai)      │
│  - 高级AI策略                                                │
│  - 机器学习优化                                              │
│  - 许可证验证                                                │
└─────────────────────────────────────────────────────────────┘
                          ↓ 验证
┌─────────────────────────────────────────────────────────────┐
│                   许可证服务器 (可选)                        │
│  (https://license.quantmesh.com)                            │
│  - 在线验证                                                  │
│  - 使用统计                                                  │
│  - 自动更新                                                  │
└─────────────────────────────────────────────────────────────┘
```

## 🚀 实施步骤

### 步骤1: 创建私有插件仓库

```bash
# 1. 创建私有仓库
mkdir quantmesh-plugin-premium-ai
cd quantmesh-plugin-premium-ai

# 2. 初始化 Go Module
go mod init github.com/quantmesh-pro/premium-ai

# 3. 创建插件代码
cat > plugin.go << 'EOF'
package premiumai

import (
    "context"
    "quantmesh/config"
    "quantmesh/plugin"
    "quantmesh/position"
    "quantmesh/strategy"
)

type PremiumAIPlugin struct {
    metadata  *plugin.PluginMetadata
    strategy  strategy.Strategy
    validator *plugin.LicenseValidator
}

func NewPlugin() *PremiumAIPlugin {
    return &PremiumAIPlugin{
        metadata: &plugin.PluginMetadata{
            Name:        "premium_ai_strategy",
            Version:     "2.0.0",
            Author:      "QuantMesh Pro Team",
            Description: "高级AI驱动策略",
            Type:        plugin.PluginTypeStrategy,
            License:     "commercial",
            RequiresKey: true,
        },
        validator: plugin.NewLicenseValidator(),
    }
}

func (p *PremiumAIPlugin) GetMetadata() *plugin.PluginMetadata {
    return p.metadata
}

func (p *PremiumAIPlugin) Initialize(cfg *config.Config, params map[string]interface{}) error {
    // 你的核心逻辑
    p.strategy = NewPremiumAIStrategy(cfg, params)
    return nil
}

func (p *PremiumAIPlugin) Validate(licenseKey string) error {
    return p.validator.ValidatePlugin(p.metadata.Name, licenseKey)
}

func (p *PremiumAIPlugin) GetStrategy() strategy.Strategy {
    return p.strategy
}

func (p *PremiumAIPlugin) Close() error {
    if p.strategy != nil {
        return p.strategy.Stop()
    }
    return nil
}

// PremiumAIStrategy 实现
type PremiumAIStrategy struct {
    // 你的核心策略代码
}

func NewPremiumAIStrategy(cfg *config.Config, params map[string]interface{}) *PremiumAIStrategy {
    // 实现你的高级策略
    return &PremiumAIStrategy{}
}

// 实现 strategy.Strategy 接口
// ...
EOF

# 4. 添加依赖
cat > go.mod << 'EOF'
module github.com/quantmesh-pro/premium-ai

go 1.21

require (
    quantmesh v0.0.0
)

replace quantmesh => github.com/yourname/quantmesh v1.0.0
EOF

# 5. 推送到 GitHub 私有仓库
git init
git add .
git commit -m "Initial commit"
git remote add origin git@github.com:quantmesh-pro/premium-ai.git
git push -u origin main
```

### 步骤2: 客户使用流程

#### 2.1 购买流程

```
1. 客户访问 https://quantmesh.com/pricing
2. 选择商业插件并支付
3. 系统自动:
   - 生成许可证密钥
   - 添加客户 GitHub 账号到私有仓库
   - 发送邮件包含:
     * 许可证密钥
     * 安装说明
     * GitHub 仓库访问链接
```

#### 2.2 安装流程

```bash
# 1. 配置 GitHub 访问权限
# 客户需要先设置 GitHub Personal Access Token
export GOPRIVATE=github.com/quantmesh-pro/*

# 2. 克隆主项目
git clone https://github.com/yourname/quantmesh.git
cd quantmesh

# 3. 添加商业插件依赖
go get github.com/quantmesh-pro/premium-ai@latest

# 4. 在 main.go 中导入
# import "github.com/quantmesh-pro/premium-ai"

# 5. 配置许可证
cat >> config.yaml << 'EOF'
plugins:
  enabled: true
  plugins:
    - name: "premium_ai_strategy"
      enabled: true
      license_key: "eyJwbHVnaW5fbmFtZSI6InByZW1pdW1fYWlfc3RyYXRlZ3kiLCJjdXN0b21lcl9pZCI6IkNVU1QwMDEi..."
      params:
        weight: 2.0
        ai_model: "gpt-4"
EOF

# 6. 编译运行
go build -o quantmesh
./quantmesh
```

### 步骤3: 在主程序中集成

修改 `main.go`:

```go
package main

import (
    "quantmesh/plugin"
    
    // 商业插件导入 (客户购买后才能访问)
    premiumai "github.com/quantmesh-pro/premium-ai"
)

func loadCommercialPlugins(
    loader *plugin.PluginLoader,
    strategyManager *strategy.StrategyManager,
    executor position.OrderExecutorInterface,
    exchange position.IExchange,
) error {
    // 从配置读取插件信息
    for _, pluginCfg := range cfg.Plugins.Plugins {
        if !pluginCfg.Enabled {
            continue
        }

        switch pluginCfg.Name {
        case "premium_ai_strategy":
            plugin := premiumai.NewPlugin()
            err := loader.LoadStrategyPlugin(
                plugin,
                pluginCfg.LicenseKey,
                pluginCfg.Params,
                strategyManager,
                executor,
                exchange,
            )
            if err != nil {
                logger.Error("❌ 加载插件 %s 失败: %v", pluginCfg.Name, err)
                return err
            }
            logger.Info("✅ 插件 %s 已加载", pluginCfg.Name)
        }
    }

    return nil
}
```

## 🔐 许可证管理

### 生成许可证

```bash
# 服务器端生成许可证
./scripts/generate_license.sh premium_ai_strategy CUST001 365 5

# 输出:
# ✅ 许可证生成成功!
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# 插件名称: premium_ai_strategy
# 客户ID:   CUST001
# 有效期至: 2026-12-28
# 最大实例: 5
# 授权功能: *
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# 
# 许可证密钥:
# eyJwbHVnaW5fbmFtZSI6InByZW1pdW1fYWlfc3RyYXRlZ3kiLCJjdXN0b21lcl9pZCI6IkNVU1QwMDEi...
```

### 验证许可证

```bash
# 客户端验证许可证
go run plugin/tools/license_validator.go -key="eyJwbHVnaW5fbmFtZSI6..."

# 输出:
# ✅ 许可证解析成功!
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# 插件名称: premium_ai_strategy
# 客户ID:   CUST001
# 签发时间: 2025-12-28 10:00:00
# 有效期至: 2026-12-28 23:59:59
# 最大实例: 5
# 授权功能: [*]
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# ✅ 许可证有效 (剩余 365 天)
# ✅ 许可证签名验证通过
```

## 🛡️ 安全措施

### 1. 代码混淆

```bash
# 安装 garble
go install mvdan.cc/garble@latest

# 混淆编译商业插件
cd quantmesh-plugin-premium-ai
garble build -o premium_ai.a

# 或混淆整个程序
cd quantmesh
garble build -o quantmesh_pro
```

### 2. 访问控制

```yaml
# GitHub 私有仓库设置
Settings > Manage Access:
  - 只添加付费客户的 GitHub 账号
  - 设置 Read Only 权限
  - 定期审计访问日志
```

### 3. 许可证绑定

```go
// 生成绑定机器的许可证
licenseKey, _ := plugin.GenerateLicense(
    "premium_ai_strategy",
    "CUST001",
    time.Now().AddDate(1, 0, 0),
    1, // 只允许1个实例
    []string{"*"},
    getMachineID(), // 绑定到客户的机器
    secretKey,
)
```

### 4. 在线验证 (可选)

```go
// 在插件初始化时在线验证
func (p *PremiumAIPlugin) Initialize(cfg *config.Config, params map[string]interface{}) error {
    // 在线验证许可证
    if err := p.validateOnline(); err != nil {
        return fmt.Errorf("在线验证失败: %v", err)
    }
    
    // 启动定期验证
    go p.periodicOnlineValidation()
    
    return nil
}

func (p *PremiumAIPlugin) validateOnline() error {
    resp, err := http.Post(
        "https://license.quantmesh.com/validate",
        "application/json",
        bytes.NewBuffer([]byte(fmt.Sprintf(`{
            "plugin": "%s",
            "license": "%s",
            "machine_id": "%s",
            "version": "%s"
        }`, p.metadata.Name, p.licenseKey, getMachineID(), p.metadata.Version))),
    )
    
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    
    if resp.StatusCode != 200 {
        return fmt.Errorf("许可证无效")
    }
    
    return nil
}
```

## 📊 商业模式

### 定价策略

```yaml
免费版 (开源):
  - 基础网格策略
  - 单交易所支持
  - 社区支持
  价格: $0

专业版:
  - 包含所有免费功能
  - 高级AI策略插件
  - 参数优化插件
  - 邮件支持
  价格: $299/月 或 $2,999/年

企业版:
  - 包含所有专业功能
  - 定制开发
  - 私有部署
  - 专属技术团队
  - 7x24 支持
  价格: $5,000+/月
```

### 许可证类型

```go
// 个人许可证
GenerateLicense(
    "premium_ai_strategy",
    "PERSONAL001",
    time.Now().AddDate(1, 0, 0),
    1, // 1个实例
    []string{"*"},
    machineID, // 绑定机器
    secretKey,
)

// 团队许可证
GenerateLicense(
    "premium_ai_strategy",
    "TEAM001",
    time.Now().AddDate(1, 0, 0),
    5, // 5个实例
    []string{"*"},
    "", // 不绑定机器
    secretKey,
)

// 企业许可证
GenerateLicense(
    "premium_ai_strategy",
    "ENTERPRISE001",
    time.Now().AddDate(1, 0, 0),
    -1, // 无限实例
    []string{"*"},
    "", // 不绑定机器
    secretKey,
)
```

## 🔄 更新和维护

### 插件更新

```bash
# 发布新版本
cd quantmesh-plugin-premium-ai
git tag v2.1.0
git push origin v2.1.0

# 客户更新
cd quantmesh
go get github.com/quantmesh-pro/premium-ai@v2.1.0
go build
```

### 自动更新检查

```go
// 在主程序中添加更新检查
func checkPluginUpdates() {
    for _, plugin := range loadedPlugins {
        latestVersion := getLatestVersion(plugin.Name)
        if latestVersion > plugin.Version {
            logger.Info("🔔 插件 %s 有新版本: %s (当前: %s)",
                plugin.Name, latestVersion, plugin.Version)
        }
    }
}
```

## 📞 客户支持

### 支持渠道

```
1. 文档: https://docs.quantmesh.com
2. Email: support@quantmesh.com
3. Telegram: @quantmesh_support
4. GitHub Issues (开源部分)
5. 私有 Slack (企业客户)
```

### 故障排查

```bash
# 检查插件状态
./quantmesh --list-plugins

# 验证许可证
./quantmesh --validate-license premium_ai_strategy

# 查看日志
tail -f logs/quantmesh.log | grep plugin
```

## 🎯 总结

这个方案的优势:

✅ **安全**: 私有仓库 + 许可证双重保护
✅ **简单**: 客户使用标准 Go 工具链
✅ **灵活**: 支持多种许可证类型
✅ **可控**: 可以随时撤销客户访问权限
✅ **专业**: 符合软件行业最佳实践

实施成本:

💰 **低**: 只需要 GitHub 私有仓库 (免费或 $4/月)
⏱️ **快**: 1-2天即可完成基础设施搭建
🔧 **易**: 无需复杂的许可证服务器

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
