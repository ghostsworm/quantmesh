# 统计功能快速配置指南（简化版）

## 🎯 快速获取 PostHog Project API Key

### 步骤 1：注册账户

1. 访问：https://posthog.com/signup
2. 点击右上角的 **"Get started - free"** 橙色按钮
3. 使用 GitHub 账号登录（最快）或邮箱注册

### 步骤 2：创建项目

注册后会自动进入项目创建页面，或者：

1. 登录后，点击右上角的 **"Projects"** 或 **"Settings"**
2. 如果没有项目，会提示创建新项目
3. 输入项目名称（如：`QuantMesh`）
4. 点击 **"Create project"**

### 步骤 3：获取 API Key

1. 在项目页面，点击左侧菜单的 **"Project Settings"**（项目设置）
2. 或者直接访问：https://app.posthog.com/project/settings
3. 找到 **"Project API Key"** 部分
4. 复制 API Key（类似：`phc_xxxxxxxxxxxxxxxxxxxx`）

### 步骤 4：配置到代码中

编辑 `utils/telemetry.go`，找到这一行：

```go
ProjectID: "YOUR_POSTHOG_PROJECT_ID", // 需要替换为实际的 Project ID
```

替换为：

```go
ProjectID: "phc_你的实际API密钥", // 替换这里
```

或者在安装脚本中设置环境变量：

```bash
export QUANTMESH_TELEMETRY_PROJECT_ID="phc_你的实际API密钥"
```

## 🔄 更简单的替代方案

如果觉得 PostHog 太复杂，可以使用以下更简单的方案：

### 方案 1：使用简单的 HTTP 端点（推荐）

如果你有自己的服务器，可以创建一个简单的统计端点：

```go
// 修改 utils/telemetry.go
DefaultTelemetryConfig = TelemetryConfig{
    Enabled:   true,
    Endpoint:  "https://your-server.com/api/telemetry", // 你的服务器地址
    ProjectID: "quantmesh", // 项目标识
}
```

### 方案 2：使用 Google Analytics（更简单）

虽然主要用于网站，但也可以用于事件追踪：

```go
// 使用 Google Analytics Measurement Protocol
Endpoint: "https://www.google-analytics.com/mp/collect",
ProjectID: "G-XXXXXXXXXX", // Google Analytics Measurement ID
```

### 方案 3：使用 Plausible（更简洁）

Plausible 界面更简洁，但主要用于网站分析：

1. 注册：https://plausible.io/
2. 创建网站
3. 获取 API Key

### 方案 4：完全禁用（最简单）

如果不需要统计，可以直接禁用：

```go
// utils/telemetry.go
DefaultTelemetryConfig = TelemetryConfig{
    Enabled: false, // 改为 false
    // ...
}
```

或者设置环境变量：

```bash
export QUANTMESH_DISABLE_TELEMETRY=1
```

## 📍 PostHog 快速导航

如果已经在 PostHog 中，快速找到 Project API Key：

1. **直接链接**：https://app.posthog.com/project/settings
2. **菜单路径**：左侧菜单 → Settings → Project Settings → Project API Key
3. **快捷键**：登录后直接访问 `/project/settings`

## 💡 提示

- PostHog 的免费层对开源项目很友好
- API Key 以 `phc_` 开头
- 如果找不到，可以在项目设置页面搜索 "API Key"
- 统计功能默认不启用（需要配置 Project ID），所以不配置也不会影响使用

## 🚀 测试

配置完成后，可以测试：

1. 运行安装脚本：`sudo ./install.sh`
2. 或启动程序：`./quantmesh`
3. 在 PostHog 控制台的 "Events" 页面查看是否有 `install` 或 `startup` 事件

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
