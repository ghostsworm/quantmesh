# 统计功能配置指南

## 🚀 快速开始

### 1. 注册 PostHog 账户

1. 访问 https://posthog.com/signup
2. 注册免费账户（开源项目免费）
3. 创建新项目

### 2. 获取 Project API Key

1. 登录 PostHog 控制台
2. 进入项目设置（Project Settings）
3. 找到 "Project API Key"
4. 复制 API Key

### 3. 配置统计功能

#### 方法一：环境变量（**强烈推荐，生产环境必须使用**）

**后端配置：**

在启动程序前设置环境变量：

```bash
export QUANTMESH_TELEMETRY_PROJECT_ID="your_posthog_api_key_here"
```

或者在 `.env` 文件中配置（确保 `.env` 文件已添加到 `.gitignore`）：

```bash
QUANTMESH_TELEMETRY_PROJECT_ID=your_posthog_api_key_here
```

**前端配置：**

在 `webui/.env` 或 `webui/.env.local` 文件中配置（这些文件已添加到 `.gitignore`）：

```bash
VITE_QUANTMESH_TELEMETRY_PROJECT_ID=your_posthog_api_key_here
VITE_QUANTMESH_TELEMETRY_HOST=https://us.i.posthog.com
```

#### 方法二：使用默认值（仅用于开发/演示）

代码中已包含默认的 Project API Key，可以直接使用，但**不推荐在生产环境使用**。

⚠️ **安全提示**：
- PostHog Project API Key 是公开的客户端密钥，暴露在前端代码中是正常的
- 但为了避免被滥用（发送垃圾数据），生产环境建议：
  1. 使用环境变量配置不同的 API Key
  2. 在 PostHog 中设置速率限制和过滤规则
  3. 定期监控异常事件

## 🔒 隐私设置

### 禁用统计

#### 方法一：环境变量

```bash
export QUANTMESH_DISABLE_TELEMETRY=1
```

#### 方法二：修改代码

编辑 `utils/telemetry.go`：

```go
DefaultTelemetryConfig = TelemetryConfig{
    Enabled: false,  // 改为 false
    // ...
}
```

## 📊 查看统计

1. 登录 PostHog 控制台
2. 进入 "Events" 页面
3. 筛选事件：
   - `install` - 安装事件
   - `startup` - 启动事件

## 🛠️ 自托管 PostHog（可选）

如果你想完全控制数据，可以自托管 PostHog：

1. 部署 PostHog：https://posthog.com/docs/self-host
2. 获取自托管实例的 API Key
3. 修改 `utils/telemetry.go` 中的 `Endpoint`：

```go
DefaultTelemetryConfig = TelemetryConfig{
    Enabled:   true,
    Endpoint:  "https://your-posthog-instance.com/capture/", // 你的自托管地址
    ProjectID: "your_posthog_api_key_here",
}
```

## 📝 注意事项

1. **完全可选**：统计功能默认不启用（需要配置 Project ID）
2. **透明性**：所有代码都可以审查
3. **隐私保护**：只收集最少的信息
4. **不阻塞**：统计发送是异步的，不会影响安装或启动速度
5. **失败处理**：如果统计服务不可用，不会影响主程序运行

## 🔍 代码位置

- `utils/telemetry.go` - 统计发送逻辑
- `scripts/install.sh` - 安装脚本中的统计调用
- `main.go` - 程序启动时的统计调用

所有代码都可以审查，确保没有后门或恶意行为。
