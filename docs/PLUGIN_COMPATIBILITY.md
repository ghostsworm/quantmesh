# 插件兼容性问题与解决方案

## ⚠️ 问题说明

Go 的 `plugin` 包有一个**严重的兼容性限制**：

### 必须满足的条件

1. **Go 版本必须完全一致**
   - 插件和主程序必须使用**完全相同**的 Go 版本编译
   - 例如：主程序用 Go 1.25.4，插件也必须用 Go 1.25.4

2. **依赖包版本必须一致**
   - 即使 Go 版本相同，如果依赖包的版本不同，也会失败
   - 错误信息：`plugin was built with a different version of package xxx`

3. **操作系统和架构必须匹配**
   - Linux 插件不能在 macOS 上运行
   - AMD64 插件不能在 ARM64 上运行

### 典型错误

```
plugin.Open: plugin was built with a different version of package internal/godebugs
plugin.Open: plugin was built with a different version of package quantmesh/logger
```

---

## 💡 解决方案

### 方案 1: 提供预编译二进制（推荐）⭐

**优点**：
- 客户无需关心 Go 版本
- 开箱即用，最佳用户体验
- 减少支持负担

**实现方式**：

1. **发布时提供预编译版本**：
   ```
   quantmesh-opensource-releases/
   ├── v1.0.0/
   │   ├── linux-amd64/
   │   │   ├── quantmesh
   │   │   └── plugins/
   │   │       ├── ai_strategy.so
   │   │       ├── multi_strategy.so
   │   │       └── advanced_risk.so
   │   ├── linux-arm64/
   │   ├── darwin-amd64/
   │   └── darwin-arm64/
   ```

2. **自动化构建脚本**：
   ```bash
   # 为多个平台编译
   ./scripts/build-all-platforms.sh
   ```

3. **版本说明文档**：
   - 明确告知客户使用预编译版本
   - 只有需要自行开发插件时才需要源码编译

---

### 方案 2: 多版本插件包

**优点**：
- 支持客户使用不同 Go 版本

**缺点**：
- 插件包体积大（多版本）
- 管理复杂

**实现方式**：

为每个支持的 Go 版本提供插件：
```
plugins/
├── go1.25.4/
│   ├── ai_strategy.so
│   └── multi_strategy.so
├── go1.24.0/
│   ├── ai_strategy.so
│   └── multi_strategy.so
└── version.json  # 版本映射
```

运行时根据主程序 Go 版本选择对应插件目录。

---

### 方案 3: 版本检测与警告（当前方案增强）

**实现**：

在插件加载时检测版本并给出清晰提示：

```go
func (l *PluginLoader) LoadPlugin(...) error {
    // 1. 检测 Go 版本
    if err := l.checkGoVersion(pluginPath); err != nil {
        return fmt.Errorf("Go 版本不匹配: %v\n提示: 请使用预编译版本或确保 Go 版本为 %s", 
            err, runtime.Version())
    }
    
    // 2. 检测依赖版本
    if err := l.checkDependencies(pluginPath); err != nil {
        return fmt.Errorf("依赖版本不匹配: %v\n提示: 请确保依赖包版本与主程序一致", err)
    }
    
    // 继续加载...
}
```

---

### 方案 4: 替代架构（长期方案）

如果兼容性问题太严重，考虑迁移到：

1. **gRPC 服务**：
   - 插件作为独立服务运行
   - 通过 gRPC 通信
   - 完全语言无关

2. **HTTP API**：
   - 插件作为 HTTP 服务
   - 更容易部署和监控

3. **嵌入源码**：
   - 付费插件作为 Go 源码提供
   - 客户编译时嵌入
   - 失去动态加载的优势

---

## 📋 推荐方案组合

### 短期（立即实施）

1. ✅ **提供预编译二进制包**
   - GitHub Releases 提供多平台版本
   - Docker 镜像包含预编译版本

2. ✅ **完善版本检测**
   - 加载失败时给出清晰的错误提示
   - 引导客户使用预编译版本

3. ✅ **文档说明**
   - README 中明确说明兼容性要求
   - 提供预编译版本下载链接

### 中期（3-6个月）

4. ✅ **自动化构建流程**
   - CI/CD 自动构建多平台版本
   - 自动发布到 GitHub Releases

5. ✅ **Docker 化**
   - 官方 Docker 镜像包含所有插件
   - 确保环境一致性

### 长期（考虑中）

6. ⚠️ **评估替代架构**
   - 如果问题持续，考虑 gRPC/HTTP 方案
   - 评估性能和维护成本

---

## 🛠️ 立即行动

### 1. 创建构建脚本

创建 `scripts/build-release.sh`：

```bash
#!/bin/bash
# 构建多平台发布版本

VERSION=${1:-"dev"}
PLATFORMS=(
    "linux/amd64"
    "linux/arm64"
    "darwin/amd64"
    "darwin/arm64"
)

for platform in "${PLATFORMS[@]}"; do
    GOOS=${platform%/*}
    GOARCH=${platform#*/}
    OUTPUT="release/v${VERSION}/${GOOS}-${GOARCH}"
    
    echo "Building ${platform}..."
    GOOS=$GOOS GOARCH=$GOARCH go build -o "${OUTPUT}/quantmesh" ./cmd/main.go
    
    # 编译插件
    # ...
done
```

### 2. 更新 README

在 `quantmesh-opensource/README.md` 中添加：

```markdown
## 📦 安装方式

### 方式 1: 使用预编译版本（推荐）

下载对应平台的预编译版本，包含所有付费插件：

```bash
# Linux AMD64
wget https://github.com/yourorg/quantmesh/releases/download/v1.0.0/quantmesh-linux-amd64.tar.gz
tar -xzf quantmesh-linux-amd64.tar.gz
./quantmesh
```

### 方式 2: 从源码编译

**⚠️ 重要提示**：如果使用付费插件，必须使用与插件相同的 Go 版本编译。

当前要求：
- Go 版本：**1.25.4**（必须完全匹配）
- 依赖版本：见 `go.mod`

如果版本不匹配，请使用预编译版本。
```

### 3. 版本检测工具

创建 `scripts/check-plugin-compatibility.sh`：

```bash
#!/bin/bash
# 检查插件兼容性

MAIN_VERSION=$(go version | awk '{print $3}')
echo "主程序 Go 版本: $MAIN_VERSION"

for plugin in plugins/*.so; do
    if [ -f "$plugin" ]; then
        echo "检查插件: $plugin"
        # 使用 go tool 检查插件版本
        # ...
    fi
done
```

---

## 📊 客户沟通

### 在 License 管理后台添加提示

在生成 License 时，添加版本要求说明：

```
⚠️ 重要提示：
使用此 License 需要：
- Go 版本: 1.25.4（精确匹配）
- 或使用官方预编译版本（推荐）

预编译版本下载: https://github.com/yourorg/quantmesh/releases
```

---

## 🔍 检测当前问题

运行以下命令检查当前的兼容性状态：

```bash
# 检查主程序 Go 版本
go version

# 检查插件要求的 Go 版本
go tool buildid plugins/ai_strategy.so

# 检查依赖版本
go list -m all
```

---

## ✅ 总结

**当前最佳实践**：
1. ✅ 优先提供预编译二进制包
2. ✅ 明确文档说明版本要求
3. ✅ 版本不匹配时给出友好提示
4. ✅ 引导客户使用预编译版本

**未来考虑**：
- 如果兼容性问题严重影响用户体验，考虑迁移到 gRPC/HTTP 架构

---

**最后更新**: 2026-01-01

