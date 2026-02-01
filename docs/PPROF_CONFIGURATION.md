# pprof 性能分析配置指南

## 概述

QuantMesh 已集成 Go 原生的 pprof 性能分析工具，可以通过配置控制是否启用、是否需要认证以及 IP 访问限制。

## 配置说明

**重要**: pprof **默认关闭**，必须显式配置 `enabled: true` 才会启用。

在 `config.yaml` 中添加以下配置：

```yaml
web:
  enabled: true
  host: "0.0.0.0"
  port: 28888
  
  # pprof 性能分析配置（默认关闭，需要显式启用）
  pprof:
    enabled: false            # 是否启用 pprof，默认 false（必须设置为 true 才会启用）
    require_auth: true         # 是否需要认证，默认 true（启用时建议开启认证）
    allowed_ips:               # IP 白名单（可选，为空则允许所有 IP）
      - "127.0.0.1"
      - "::1"
      # - "192.168.1.100"      # 示例：允许特定内网 IP
```

## 配置项说明

### `enabled`
- **类型**: `bool`
- **默认值**: `false`（**默认关闭**）
- **说明**: 是否启用 pprof 端点。**必须显式设置为 `true` 才会启用**
- **建议**: 
  - 开发/测试环境：设置为 `true`
  - 生产环境：保持 `false`（默认关闭，安全考虑）

### `require_auth`
- **类型**: `bool`
- **默认值**: `true`
- **说明**: 访问 pprof 端点是否需要登录认证
- **建议**: 启用 pprof 时建议设置为 `true`

### `allowed_ips`
- **类型**: `[]string`
- **默认值**: `["127.0.0.1", "::1"]`
- **说明**: IP 白名单，只有列表中的 IP 可以访问 pprof 端点
- **特殊值**: 
  - `"*"` - 允许所有 IP（不推荐）
  - `"127.0.0.1"` - 本地 IPv4
  - `"::1"` - 本地 IPv6
- **建议**: 生产环境建议只允许本地或特定内网 IP

## 使用示例

### 开发环境配置

```yaml
web:
  pprof:
    enabled: true
    require_auth: false        # 开发环境可以不需要认证
    allowed_ips:
      - "127.0.0.1"
      - "::1"
      - "192.168.1.0/24"       # 允许整个内网段（如果支持）
```

### 生产环境配置（推荐）

```yaml
web:
  # 不配置 pprof 或设置为 false（默认关闭）
  # pprof:
  #   enabled: false
```

或者如果必须启用（用于调试）：
<｜tool▁calls▁begin｜><｜tool▁call▁begin｜>
run_terminal_cmd

```yaml
web:
  pprof:
    enabled: true
    require_auth: true         # 必须认证
    allowed_ips:
      - "127.0.0.1"           # 只允许本地访问
      - "YOUR_OFFICE_IP"       # 或特定 IP
```

## 访问 pprof 端点

启用后，可以通过以下方式访问：

### 1. Web UI（推荐）

```bash
# 启动 pprof Web UI
go tool pprof -http=:8080 http://localhost:28888/debug/pprof/profile?seconds=30

# 或分析堆内存
go tool pprof -http=:8080 http://localhost:28888/debug/pprof/heap
```

### 2. 命令行工具

```bash
# CPU 分析
go tool pprof http://localhost:28888/debug/pprof/profile?seconds=30

# 内存分析
go tool pprof http://localhost:28888/debug/pprof/heap

# Goroutine 分析
go tool pprof http://localhost:28888/debug/pprof/goroutine
```

### 3. 直接访问（需要认证）

如果启用了认证，需要先登录 Web UI，然后访问：
- `http://localhost:28888/debug/pprof/` - pprof 主页
- `http://localhost:28888/debug/pprof/heap` - 堆内存
- `http://localhost:28888/debug/pprof/profile` - CPU profile

## 安全建议

1. **生产环境禁用**: 默认 `enabled: false`，生产环境建议保持禁用
2. **启用认证**: 如果必须启用，设置 `require_auth: true`
3. **IP 白名单**: 限制访问 IP，只允许可信的 IP
4. **防火墙规则**: 在 Nginx 或防火墙层面限制访问
5. **临时启用**: 仅在需要调试时临时启用，调试完成后立即禁用

## 性能影响

- **CPU profiling**: 会增加约 5-10% 的 CPU 负载
- **内存 profiling**: 影响较小
- **Goroutine profiling**: 影响很小

建议只在需要调试性能问题时启用。

## 相关文档

- [pprof 使用指南](PPROF_GUIDE.md) - 详细的 pprof 使用说明
- [Nginx 配置指南](NGINX_CONFIG.md) - 如何在 Nginx 中限制 pprof 访问
