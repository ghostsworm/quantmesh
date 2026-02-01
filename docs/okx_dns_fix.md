# OKX DNS 解析问题修复指南

## 问题描述
`www.okx.com` 被错误解析到 `169.254.0.2`（链路本地地址），导致无法访问 OKX API。

## 原因分析
代理工具（Clash）的 DNS 劫持功能配置不当，导致返回错误的 CNAME 记录。

## 解决方案

### 方案 1：修改 Clash DNS 配置（推荐）

在你的 Clash 配置文件中添加或修改 DNS 部分：

```yaml
dns:
  enable: true
  listen: 0.0.0.0:53
  enhanced-mode: fake-ip  # 或 redir-host
  fake-ip-range: 198.18.0.1/16
  fake-ip-filter:
    - '*.okx.com'
    - '*.okpool.top'
    - '*.local'
  nameserver:
    - 1.1.1.1
    - 8.8.8.8
    - 223.5.5.5
  fallback:
    - https://1.1.1.1/dns-query
    - https://dns.google/dns-query
    - tls://223.5.5.5:853
  fallback-filter:
    geoip: true
    ipcidr:
      - 240.0.0.0/4
```

**关键点：**
- 将 `*.okx.com` 和 `*.okpool.top` 加入 `fake-ip-filter`，避免 DNS 劫持
- 使用多个可靠的 DNS 服务器

### 方案 2：临时修复 - 修改系统 DNS

```bash
# macOS 临时修改 DNS
sudo networksetup -setdnsservers Wi-Fi 1.1.1.1 8.8.8.8

# 或者修改 /etc/resolv.conf（需要 root 权限）
```

### 方案 3：使用 hosts 文件强制解析

```bash
# 编辑 /etc/hosts 文件
sudo nano /etc/hosts

# 添加以下内容（使用 dig @1.1.1.1 www.okx.com 获取的最新 IP）
104.18.43.174 www.okx.com
172.64.144.82 www.okx.com
```

### 方案 4：禁用 Clash 的 DNS 劫持

如果不需要 Clash 的 DNS 功能，可以禁用：

```yaml
dns:
  enable: false
```

然后使用系统 DNS 或手动配置 DNS。

## 验证修复

```bash
# 检查 DNS 解析
dig www.okx.com +short

# 应该返回真实的 IP 地址，而不是 169.254.x.x
# 正确结果示例：
# 104.18.43.174
# 172.64.144.82

# 测试连接
curl -v https://www.okx.com/
```

## 注意事项

1. **fake-ip 模式**：如果使用 fake-ip 模式，必须将需要正确解析的域名加入 `fake-ip-filter`
2. **DNS 服务器选择**：推荐使用 Cloudflare (1.1.1.1) 或 Google (8.8.8.8)
3. **代理规则**：确保 OKX 域名规则在 DNS 配置之后生效
