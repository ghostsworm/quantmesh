# QuantMesh 插件系统集成指南

## 📋 目录

1. [快速开始](#快速开始)
2. [在 main.go 中集成](#在-maingo-中集成)
3. [配置文件](#配置文件)
4. [商业插件分发](#商业插件分发)
5. [最佳实践](#最佳实践)

## 🚀 快速开始

### 1. 在 main.go 中添加插件支持

```go
package main

import (
    "quantmesh/plugin"
    "quantmesh/plugin/examples"
    // 导入你的插件
    // "quantmesh/plugins/premium_ai_strategy"
)

func main() {
    // ... 现有的初始化代码 ...

    // 创建插件加载器
    pluginLoader := plugin.NewPluginLoader(cfg)

    // 加载免费插件
    if err := loadFreePlugins(pluginLoader, strategyManager, executor, ex); err != nil {
        logger.Error("❌ 加载免费插件失败: %v", err)
    }

    // 加载商业插件
    if err := loadCommercialPlugins(pluginLoader, strategyManager, executor, ex); err != nil {
        logger.Error("❌ 加载商业插件失败: %v", err)
    }

    // 列出已加载的插件
    listLoadedPlugins(pluginLoader)

    // ... 继续现有的启动流程 ...
}

// loadFreePlugins 加载免费插件
func loadFreePlugins(
    loader *plugin.PluginLoader,
    strategyManager *strategy.StrategyManager,
    executor position.OrderExecutorInterface,
    exchange position.IExchange,
) error {
    logger.Info("📦 加载免费插件...")

    // 示例策略插件
    examplePlugin := examples.NewExampleStrategyPlugin()
    err := loader.LoadStrategyPlugin(
        examplePlugin,
        "", // 免费插件不需要许可证
        map[string]interface{}{
            "weight":      1.0,
            "fixed_pool":  1000.0,
        },
        strategyManager,
        executor,
        exchange,
    )
    if err != nil {
        logger.Warn("⚠️ 加载示例插件失败: %v", err)
    }

    return nil
}

// loadCommercialPlugins 加载商业插件
func loadCommercialPlugins(
    loader *plugin.PluginLoader,
    strategyManager *strategy.StrategyManager,
    executor position.OrderExecutorInterface,
    exchange position.IExchange,
) error {
    logger.Info("🔐 加载商业插件...")

    // 从配置文件读取插件配置
    if cfg.Plugins == nil || !cfg.Plugins.Enabled {
        logger.Info("插件系统未启用")
        return nil
    }

    for _, pluginCfg := range cfg.Plugins.Plugins {
        if !pluginCfg.Enabled {
            continue
        }

        // 根据插件名称加载对应的插件
        switch pluginCfg.Name {
        case "premium_ai_strategy":
            // plugin := premium_ai_strategy.NewPlugin()
            // err := loader.LoadStrategyPlugin(
            //     plugin,
            //     pluginCfg.LicenseKey,
            //     pluginCfg.Params,
            //     strategyManager,
            //     executor,
            //     exchange,
            // )
            // if err != nil {
            //     logger.Error("❌ 加载插件 %s 失败: %v", pluginCfg.Name, err)
            // }
            logger.Info("✅ 插件 %s 已加载", pluginCfg.Name)

        default:
            logger.Warn("⚠️ 未知插件: %s", pluginCfg.Name)
        }
    }

    return nil
}

// listLoadedPlugins 列出已加载的插件
func listLoadedPlugins(loader *plugin.PluginLoader) {
    registry := loader.GetRegistry()
    plugins := registry.List()

    if len(plugins) == 0 {
        logger.Info("未加载任何插件")
        return
    }

    logger.Info("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
    logger.Info("已加载的插件:")
    for _, meta := range plugins {
        licenseType := "免费"
        if meta.RequiresKey {
            licenseType = "商业"
        }
        logger.Info("  • %s v%s (%s) - %s", meta.Name, meta.Version, licenseType, meta.Description)
    }
    logger.Info("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
}
```

### 2. 更新配置结构

在 `config/config.go` 中添加插件配置：

```go
// Config 配置结构
type Config struct {
    // ... 现有字段 ...
    
    Plugins *PluginsConfig `yaml:"plugins"`
}

// PluginsConfig 插件配置
type PluginsConfig struct {
    Enabled   bool           `yaml:"enabled"`
    Directory string         `yaml:"directory"`
    Plugins   []PluginConfig `yaml:"plugins"`
}

// PluginConfig 单个插件配置
type PluginConfig struct {
    Name       string                 `yaml:"name"`
    Enabled    bool                   `yaml:"enabled"`
    LicenseKey string                 `yaml:"license_key"`
    Params     map[string]interface{} `yaml:"params"`
}
```

### 3. 更新配置文件

在 `config.yaml` 中添加：

```yaml
# 插件配置
plugins:
  enabled: true
  directory: "./plugins"
  
  plugins:
    # 免费插件示例
    - name: "example_strategy"
      enabled: true
      license_key: ""
      params:
        weight: 1.0
        fixed_pool: 1000.0
    
    # 商业插件示例
    - name: "premium_ai_strategy"
      enabled: false
      license_key: "eyJwbHVnaW5fbmFtZSI6InByZW1pdW1fYWlfc3RyYXRlZ3kiLCJjdXN0b21lcl9pZCI6IkNVU1QwMDEiLCJleHBpcnlfZGF0ZSI6IjIwMjYtMTItMzFUMjM6NTk6NTlaIiwibWF4X2luc3RhbmNlcyI6NSwi..."
      params:
        weight: 2.0
        ai_model: "gpt-4"
        optimization_level: "high"
```

## 📦 商业插件分发

### 方案1: 私有 Git 仓库 (推荐)

```bash
# 1. 创建私有仓库
git init quantmesh-plugin-premium-ai
cd quantmesh-plugin-premium-ai

# 2. 添加插件代码
# ... 开发你的插件 ...

# 3. 推送到私有仓库 (GitHub/GitLab)
git remote add origin git@github.com:quantmesh-pro/premium-ai-strategy.git
git push -u origin main

# 4. 客户购买后获得访问权限
# 添加客户的 GitHub 账号到仓库的 Collaborators

# 5. 客户使用
go get github.com/quantmesh-pro/premium-ai-strategy@latest
```

**优点**:
- 版本控制
- 自动更新
- 访问控制
- 审计日志

### 方案2: 预编译二进制 + 许可证

```bash
# 1. 编译插件为静态库
cd quantmesh-plugin-premium-ai
go build -buildmode=archive -o premium_ai.a

# 2. 加密二进制文件
openssl enc -aes-256-cbc -salt -in premium_ai.a -out premium_ai.a.enc -k "your-password"

# 3. 分发给客户
# - premium_ai.a.enc (加密的二进制)
# - install.sh (安装脚本)
# - license.key (许可证密钥)

# 4. 客户安装
./install.sh --license=license.key
```

### 方案3: 许可证服务器

```go
// 在线验证许可证
type OnlineLicenseValidator struct {
    serverURL string
}

func (v *OnlineLicenseValidator) Validate(pluginName, licenseKey string) error {
    resp, err := http.Post(
        v.serverURL + "/validate",
        "application/json",
        bytes.NewBuffer([]byte(fmt.Sprintf(`{
            "plugin": "%s",
            "license": "%s",
            "machine_id": "%s"
        }`, pluginName, licenseKey, getMachineID()))),
    )
    
    if err != nil {
        return err
    }
    
    defer resp.Body.Close()
    
    if resp.StatusCode != 200 {
        return fmt.Errorf("许可证验证失败")
    }
    
    return nil
}
```

## 🔧 开发工作流

### 开发免费插件

```bash
# 1. 创建插件
./scripts/create_plugin.sh my_strategy

# 2. 开发插件
cd plugins/my_strategy
# 编辑 plugin.go

# 3. 测试插件
go test -v

# 4. 在 main.go 中注册
# import "quantmesh/plugins/my_strategy"

# 5. 编译运行
cd ../..
go build -o quantmesh
./quantmesh
```

### 开发商业插件

```bash
# 1. 创建插件
./scripts/create_plugin.sh premium_strategy

# 2. 修改为商业插件
cd plugins/premium_strategy
# 编辑 plugin.go:
#   License: "commercial"
#   RequiresKey: true

# 3. 生成许可证
cd ../..
./scripts/generate_license.sh premium_strategy CUST001 365 5

# 4. 测试许可证验证
go run plugin/tools/license_validator.go -key="<生成的许可证>"

# 5. 打包分发
# 选择上述分发方案之一
```

## 🛡️ 安全最佳实践

### 1. 代码混淆

```bash
# 安装 garble
go install mvdan.cc/garble@latest

# 混淆编译
garble build -o quantmesh_pro
```

### 2. 许可证保护

```go
// 在插件初始化时验证
func (p *Plugin) Initialize(cfg *config.Config, params map[string]interface{}) error {
    // 1. 验证许可证
    if err := p.validator.ValidatePlugin(p.metadata.Name, p.licenseKey); err != nil {
        return fmt.Errorf("许可证验证失败: %v", err)
    }
    
    // 2. 检查功能授权
    if !p.validator.CheckFeature(p.metadata.Name, "ai") {
        return fmt.Errorf("未授权 AI 功能")
    }
    
    // 3. 定期重新验证 (防止破解)
    go p.periodicValidation()
    
    return nil
}

func (p *Plugin) periodicValidation() {
    ticker := time.NewTicker(1 * time.Hour)
    defer ticker.Stop()
    
    for range ticker.C {
        if err := p.validator.ValidatePlugin(p.metadata.Name, p.licenseKey); err != nil {
            logger.Error("❌ 许可证验证失败，停止插件: %v", err)
            p.Close()
            return
        }
    }
}
```

### 3. 机器绑定

```go
// 生成绑定机器的许可证
licenseKey, _ := plugin.GenerateLicense(
    "premium_ai_strategy",
    "CUST001",
    time.Now().AddDate(1, 0, 0),
    1, // 只允许1个实例
    []string{"*"},
    getMachineID(), // 绑定到特定机器
    secretKey,
)
```

## 📊 插件监控

### 添加插件统计

```go
type PluginStatistics struct {
    LoadTime      time.Time
    CallCount     int64
    ErrorCount    int64
    LastError     error
    LastErrorTime time.Time
}

// 在插件中添加统计
func (p *Plugin) trackCall() {
    atomic.AddInt64(&p.stats.CallCount, 1)
}

func (p *Plugin) trackError(err error) {
    atomic.AddInt64(&p.stats.ErrorCount, 1)
    p.stats.LastError = err
    p.stats.LastErrorTime = time.Now()
}
```

### 添加 Web API

```go
// GET /api/plugins
func (s *Server) handleGetPlugins(c *gin.Context) {
    registry := plugin.GetRegistry()
    plugins := registry.List()
    
    c.JSON(200, gin.H{
        "plugins": plugins,
    })
}

// GET /api/plugins/:name
func (s *Server) handleGetPlugin(c *gin.Context) {
    name := c.Param("name")
    registry := plugin.GetRegistry()
    
    plugin, err := registry.Get(name)
    if err != nil {
        c.JSON(404, gin.H{"error": "插件未找到"})
        return
    }
    
    c.JSON(200, gin.H{
        "metadata": plugin.GetMetadata(),
        // "statistics": plugin.GetStatistics(),
    })
}
```

## 🔄 插件更新

### 自动更新检查

```go
type PluginUpdater struct {
    registry *plugin.PluginRegistry
    updateURL string
}

func (u *PluginUpdater) CheckUpdates() ([]UpdateInfo, error) {
    plugins := u.registry.List()
    var updates []UpdateInfo
    
    for _, meta := range plugins {
        // 检查远程版本
        latestVersion, err := u.getLatestVersion(meta.Name)
        if err != nil {
            continue
        }
        
        if latestVersion > meta.Version {
            updates = append(updates, UpdateInfo{
                Name:           meta.Name,
                CurrentVersion: meta.Version,
                LatestVersion:  latestVersion,
            })
        }
    }
    
    return updates, nil
}
```

## 📝 完整示例

查看 `plugin/examples/` 目录获取完整的示例代码：

- `example_strategy_plugin.go` - 免费策略插件示例
- `premium_ai_plugin.go` - 商业AI插件示例 (仅结构)

## 🆘 故障排查

### 常见问题

**Q: 插件加载失败**
```
A: 检查以下几点:
1. 插件是否正确实现了 Plugin 接口
2. 许可证是否有效
3. 依赖是否正确导入
4. go.mod 是否配置正确
```

**Q: 许可证验证失败**
```
A: 检查:
1. 许可证是否过期
2. 机器ID是否匹配
3. 签名密钥是否一致
```

**Q: 插件无法访问主程序功能**
```
A: 确保:
1. 正确传递了 config, executor, exchange 参数
2. 接口定义一致
3. 没有循环依赖
```

## 📞 技术支持

- 📧 Email: support@quantmesh.com
- 💬 Telegram: @quantmesh_support
- 📖 文档: https://docs.quantmesh.com/plugins

