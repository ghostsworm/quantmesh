# ⚠️ 插件兼容性重要说明

## 问题说明

Go 的插件系统（`plugin` 包）有一个**严格的兼容性要求**：

**插件和主程序必须使用完全相同的 Go 版本和依赖版本编译！**

### 必须满足的条件

1. ✅ **Go 版本必须完全一致**
   - 主程序用 Go 1.25.4 → 插件也必须用 Go 1.25.4
   - 即使是 1.25.4 和 1.25.5 也不兼容！

2. ✅ **依赖包版本必须一致**
   - 即使 Go 版本相同，如果依赖包版本不同也会失败

3. ✅ **操作系统和架构必须匹配**
   - Linux 插件不能在 macOS 上运行
   - AMD64 插件不能在 ARM64 上运行

---

## 💡 解决方案

### 🎯 推荐方案：使用预编译版本（最简单）

我们提供预编译的二进制包，**已包含所有付费插件**，无需担心版本问题：

```bash
# 下载对应平台的预编译版本
wget https://github.com/yourorg/quantmesh/releases/download/v1.0.0/quantmesh-linux-amd64.tar.gz
tar -xzf quantmesh-linux-amd64.tar.gz
cd quantmesh-linux-amd64
./quantmesh
```

**优点**：
- ✅ 开箱即用，无需编译
- ✅ 版本已匹配，兼容性有保障
- ✅ 包含所有插件，无需额外配置

---

### 📝 方案 2：从源码编译（仅适合开发）

如果你需要从源码编译，必须：

1. **使用指定 Go 版本**：
   ```bash
   # 当前要求的 Go 版本
   go version  # 必须显示: go version go1.25.4 ...
   ```

2. **确保依赖版本一致**：
   ```bash
   # 更新依赖到指定版本
   go mod tidy
   go mod download
   ```

3. **重新编译插件**：
   ```bash
   cd quantmesh-premium/plugins/ai_strategy
   go build -buildmode=plugin -o ai_strategy.so .
   ```

---

## 🚨 常见错误

### 错误 1: Go 版本不匹配

```
plugin.Open: plugin was built with a different version of package internal/godebugs
```

**解决方法**：
- 使用预编译版本（推荐）
- 或确保插件和主程序使用相同的 Go 版本

### 错误 2: 依赖版本不匹配

```
plugin.Open: plugin was built with a different version of package quantmesh/logger
```

**解决方法**：
- 确保 `go.mod` 中的依赖版本一致
- 运行 `go mod tidy` 更新依赖

---

## 📋 版本信息

### 当前版本要求

- **Go 版本**: 1.25.4（必须精确匹配）
- **操作系统**: Linux, macOS
- **架构**: amd64, arm64

### 检查版本

```bash
# 检查 Go 版本
go version

# 检查主程序版本
./quantmesh --version

# 检查插件版本（如果插件支持）
strings plugins/ai_strategy.so | grep "go1\."
```

---

## 🔗 相关资源

- [详细兼容性文档](./docs/PLUGIN_COMPATIBILITY.md)
- [插件开发指南](./docs/PLUGIN_DEVELOPMENT_GUIDE.md)
- [预编译版本下载](https://github.com/yourorg/quantmesh/releases)

---

## ❓ 常见问题

**Q: 为什么不能像其他语言那样自由使用插件？**

A: 这是 Go 语言 `plugin` 包的设计限制。我们推荐使用预编译版本避免此问题。

**Q: 预编译版本包含哪些插件？**

A: 包含所有已发布的付费插件，且版本已匹配，可直接使用。

**Q: 如果我想自己编译怎么办？**

A: 可以，但必须确保使用完全相同的 Go 版本和依赖版本。建议参考 [插件开发指南](./docs/PLUGIN_DEVELOPMENT_GUIDE.md)。

---

**最后更新**: 2026-01-01

