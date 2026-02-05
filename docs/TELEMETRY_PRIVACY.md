# 统计功能隐私说明

## 🔒 地理位置和 IP 地址处理

### 问题：PostHog 中的经纬度是怎么收集的？

**简短回答**：我们的代码**不会主动发送经纬度数据**。PostHog 服务端可能会根据 IP 地址自动推断地理位置，但我们已经采取了措施来最小化这个风险。

### 详细说明

#### 1. 前端代码（`webui/src/services/telemetry.ts`）

✅ **已禁用 IP 捕获**：
```typescript
posthog.init(POSTHOG_PROJECT_ID, {
  // ...
  ip_capture: false,  // 禁用 IP 地址捕获和地理位置推断
})
```

这意味着：
- PostHog JavaScript SDK 不会捕获 IP 地址
- PostHog 服务端不会根据 IP 推断地理位置（经纬度、城市等）

#### 2. 后端代码（`utils/telemetry.go`）

✅ **IP 地址仅用于本地生成 distinct_id**：
- IP 地址从 `ip4.dev` 获取，仅用于生成 `distinct_id`（格式：`hostname-ip-os-arch-version`）
- **IP 地址不会包含在发送给 PostHog 的 payload 中**
- `distinct_id` 中包含 IP，但这是为了区分不同的部署实例，不是地理位置数据

```go
// IP 仅用于生成 distinct_id，不发送到 PostHog
distinctID := hostname + "-" + ip + "-" + runtime.GOOS + "-" + runtime.GOARCH + "-" + version

payload := map[string]interface{}{
    "properties": map[string]interface{}{
        "timestamp": eventData.Timestamp.Format(time.RFC3339),
        "version":   eventData.Version,
        "os":        eventData.OS,
        "arch":      eventData.Arch,
        // 注意：不包含 IP 地址
    },
}
```

#### 3. PostHog 服务端行为

⚠️ **潜在风险**：
- PostHog 服务端可能会从 HTTP 请求头（如 `X-Forwarded-For`）中获取 IP 地址
- 如果获取到 IP 地址，PostHog 可能会自动推断地理位置（经纬度、城市、国家等）
- 这是 PostHog 的默认行为，不是我们的代码主动发送的

### 隐私保护措施

我们已经采取了以下措施来保护隐私：

1. ✅ **前端禁用 IP 捕获**：通过 `ip_capture: false` 配置
2. ✅ **后端不发送 IP**：IP 地址不包含在 payload 中
3. ✅ **最小化数据收集**：只收集必要的信息（事件类型、版本、OS、架构）
4. ✅ **完全可选**：用户可以通过环境变量禁用统计功能

### 如何进一步保护隐私

如果你仍然担心地理位置暴露，可以采取以下措施：

#### 方法 1：完全禁用统计功能（推荐）

```bash
# 后端
export QUANTMESH_DISABLE_TELEMETRY=1

# 前端（在 webui/.env.local 中）
VITE_DISABLE_TELEMETRY=1
```

#### 方法 2：在 PostHog 项目设置中禁用地理位置推断

1. 登录 PostHog 控制台
2. 进入 **Project Settings** → **Data Management**
3. 查找 "IP Geolocation" 或 "Location Data" 设置
4. 禁用地理位置推断功能

#### 方法 3：使用自托管 PostHog

如果你完全控制 PostHog 实例，可以：
- 禁用地理位置推断功能
- 完全控制数据收集和处理
- 确保数据不会泄露

### 数据收集总结

| 数据类型 | 是否收集 | 用途 | 隐私风险 |
|---------|---------|------|---------|
| 事件类型 | ✅ | 统计安装/启动次数 | 低 |
| 时间戳 | ✅ | 分析使用趋势 | 低 |
| 版本号 | ✅ | 了解版本分布 | 低 |
| 操作系统 | ✅ | 了解平台分布 | 低 |
| 架构 | ✅ | 了解架构分布 | 低 |
| IP 地址 | ❌ | 不发送（仅用于本地 distinct_id） | - |
| 经纬度 | ❌ | 不收集 | - |
| 城市/国家 | ❌ | 不收集（前端已禁用） | - |
| 用户 ID | ❌ | 不收集 | - |
| API 密钥 | ❌ | 不收集 | - |
| 交易数据 | ❌ | 不收集 | - |

### 常见问题

**Q: PostHog 中看到的经纬度是从哪里来的？**

A: 如果 PostHog 中显示了经纬度数据，可能是：
1. PostHog 服务端从 HTTP 请求头中获取了 IP 地址
2. PostHog 根据 IP 地址自动推断的地理位置
3. 这不是我们的代码主动发送的

**Q: 如何确认是否收集了地理位置数据？**

A: 
1. 检查前端代码：确认 `ip_capture: false` 配置
2. 检查后端代码：确认 payload 中不包含 IP 地址
3. 在 PostHog 中查看事件属性：如果看到 `$geoip_*` 或 `$ip` 字段，说明 PostHog 服务端可能获取了 IP

**Q: 服务器部署会暴露服务器位置吗？**

A: 理论上，如果 PostHog 服务端从 HTTP 请求头获取了 IP，可能会推断出服务器的大致位置（通常是城市级别，精度不高）。但：
- 前端已禁用 IP 捕获
- 后端不发送 IP 地址
- 如果仍担心，可以完全禁用统计功能

**Q: 个人用户会暴露位置吗？**

A: 对于个人用户：
- 前端已禁用 IP 捕获，不会主动发送位置信息
- 如果使用代理或 VPN，IP 地址可能指向代理服务器位置，而不是真实位置
- 如果仍担心，可以禁用统计功能

### 总结

1. ✅ **我们的代码不会主动发送经纬度数据**
2. ✅ **前端已禁用 IP 捕获**（`ip_capture: false`）
3. ✅ **后端不发送 IP 地址**（仅用于本地生成 distinct_id）
4. ⚠️ **PostHog 服务端可能从 HTTP 请求头获取 IP 并推断地理位置**（这是 PostHog 的默认行为）
5. ✅ **用户可以通过环境变量完全禁用统计功能**

如果你对隐私有严格要求，建议：
- 使用 `QUANTMESH_DISABLE_TELEMETRY=1` 完全禁用统计功能
- 或者使用自托管 PostHog，完全控制数据收集和处理
