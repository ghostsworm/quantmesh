# 安全修复：配置初始化和密码设置接口保护

## 问题描述

**严重安全漏洞**：在之前的版本中，以下两个接口是公开的（不需要认证）：

1. `POST /api/setup/init` - 配置初始化接口
2. `POST /api/auth/password/set` - 密码设置接口

这意味着任何人都可以：
- 重新初始化你的系统配置（覆盖交易所API密钥、交易对等）
- 重置你的登录密码

**风险等级**：🔴 **严重**

如果你的系统已经部署到公网，攻击者可能已经：
- 修改了你的配置
- 重置了你的密码
- 获取了系统控制权

## 修复内容

### 1. 密码设置接口 (`/api/auth/password/set`)

**修复前**：
- 任何人都可以随时调用此接口设置/重置密码

**修复后**：
- ✅ 仅在**首次设置**（未设置密码）时可用
- ✅ 如果已设置密码，返回 403 Forbidden 错误
- ✅ 已设置密码后，必须使用 `/api/auth/password/change` 接口（需要认证）

### 2. 配置初始化接口 (`/api/setup/init`)

**修复前**：
- 任何人都可以随时调用此接口重新初始化配置

**修复后**：
- ✅ 首次设置（未设置密码）时可用
- ✅ 已设置密码后，**必须先登录**才能修改配置
- ✅ 未认证的请求返回 401 Unauthorized 错误

## 安全检查逻辑

```go
// 检查是否已设置密码
hasPassword, _ := globalPasswordManager.HasPassword("admin")

if hasPassword {
    // 已设置密码，检查是否已登录
    session, exists := sm.GetSessionFromRequest(c.Request)
    if !exists || session == nil {
        // 未登录，拒绝请求
        return 401 Unauthorized
    }
    // 已登录，允许操作
}
// 未设置密码，允许首次设置
```

## 影响范围

### 受影响的版本
- 所有之前的版本（在此修复之前）

### 修复版本
- 2026-01-25 及之后的版本

## 紧急应对措施

> **配置形态**：当前发行版主配置权威在 **`app_config`**（主库）。若生产环境**没有**磁盘 `config.yaml`，下文涉及该文件的检查请改为核对 **数据库备份**、**Web 导出**或 **`data/*.db` 修改时间**。

如果你的系统已经部署到公网，**立即执行以下步骤**：

### 1. 检查是否被攻击

```bash
# 检查认证数据库的修改时间
ls -la data/auth.db

# 检查配置文件的修改时间（若仍使用磁盘 YAML）
ls -la config.yaml 2>/dev/null || true

# 查看系统日志，搜索可疑的设置密码和配置初始化请求
grep "设置密码" logs/*.log
grep "配置初始化" logs/*.log
grep "SECURITY" logs/*.log
```

### 2. 如果发现异常

**立即停止系统**：
```bash
./stop.sh
```

**检查配置**（有磁盘副本时）：
```bash
cat config.yaml
```

查看是否有不属于你的：
- API Key / Secret Key
- 交易对配置
- 交易所配置

**检查认证数据库**：
```bash
sqlite3 data/auth.db "SELECT username, created_at FROM users;"
```

### 3. 恢复配置

如果配置被篡改，从备份恢复：
```bash
# 查看备份
ls -la backups/

# 恢复最近的备份
cp backups/config_backup_YYYYMMDD_HHMMSS.yaml config.yaml
```

### 4. 重置密码

删除认证数据库，重新设置密码：
```bash
rm data/auth.db
# 重启系统后重新设置密码
```

### 5. 更新到修复版本

```bash
# 拉取最新代码
git pull origin main

# 重新编译
go build -o quantmesh

# 重启系统
./stop.sh
./start.sh
```

## 安全建议

### 1. 网络安全

**不要将系统直接暴露到公网！**

推荐的部署方式：

#### 方案A：使用VPN（推荐）
```
你的设备 -> VPN -> 服务器（内网）
```

#### 方案B：使用反向代理 + IP白名单
```nginx
# nginx 配置示例
location /api/ {
    # 只允许特定IP访问
    allow 1.2.3.4;  # 你的IP
    deny all;
    
    proxy_pass http://localhost:8080;
}
```

#### 方案C：使用SSH隧道
```bash
# 在本地运行
ssh -L 8080:localhost:8080 user@your-server

# 然后访问 http://localhost:8080
```

### 2. 密码安全

- ✅ 使用强密码（至少12位，包含大小写字母、数字、特殊字符）
- ✅ 定期更换密码
- ✅ 不要在多个系统使用相同密码
- ✅ 考虑使用 WebAuthn（生物识别/硬件密钥）

### 3. 配置文件安全

```bash
# 设置配置文件权限，只有所有者可读写
chmod 600 config.yaml

# 设置认证数据库权限
chmod 600 data/auth.db
```

### 4. 定期备份

系统会自动备份配置文件到 `backups/` 目录，但建议：

```bash
# 定期备份到其他位置
cp config.yaml /path/to/secure/backup/config_$(date +%Y%m%d).yaml
cp data/auth.db /path/to/secure/backup/auth_$(date +%Y%m%d).db
```

### 5. 监控日志

定期检查日志中的可疑活动：

```bash
# 查看所有认证相关日志
grep "AUTH" logs/*.log

# 查看所有安全相关日志
grep "SECURITY" logs/*.log

# 查看失败的登录尝试
grep "密码错误" logs/*.log
```

## 技术细节

### 修改的文件

1. `web/api_auth.go`
   - 修改 `setPassword` 函数
   - 添加密码已存在检查

2. `web/api_setup.go`
   - 修改 `initSetupHandler` 函数
   - 添加认证检查

### 测试方法

#### 测试1：首次设置密码（应该成功）

```bash
# 删除认证数据库
rm data/auth.db

# 重启系统
./stop.sh && ./start.sh

# 设置密码（应该成功）
curl -X POST http://localhost:8080/api/auth/password/set \
  -H "Content-Type: application/json" \
  -d '{"password":"your-password"}'

# 预期响应：{"success":true,"message":"密码设置成功"}
```

#### 测试2：再次设置密码（应该失败）

```bash
# 再次尝试设置密码（应该失败）
curl -X POST http://localhost:8080/api/auth/password/set \
  -H "Content-Type: application/json" \
  -d '{"password":"another-password"}'

# 预期响应：
# {"error":"密码已设置，请使用修改密码功能","code":"PASSWORD_ALREADY_SET"}
# HTTP 状态码：403
```

#### 测试3：未登录时初始化配置（应该失败）

```bash
# 未登录时尝试初始化配置（应该失败）
curl -X POST http://localhost:8080/api/setup/init \
  -H "Content-Type: application/json" \
  -d '{
    "exchange":"binance",
    "api_key":"test",
    "secret_key":"test",
    "symbol":"BTCUSDT",
    "price_interval":0.5,
    "order_quantity":0.001,
    "buy_window_size":10
  }'

# 预期响应：
# {"success":false,"message":"系统已初始化，需要登录后才能修改配置"}
# HTTP 状态码：401
```

#### 测试4：登录后初始化配置（应该成功）

```bash
# 先登录
curl -X POST http://localhost:8080/api/auth/password/verify \
  -H "Content-Type: application/json" \
  -d '{"password":"your-password"}' \
  -c cookies.txt

# 使用会话初始化配置（应该成功）
curl -X POST http://localhost:8080/api/setup/init \
  -H "Content-Type: application/json" \
  -b cookies.txt \
  -d '{
    "exchange":"binance",
    "api_key":"test",
    "secret_key":"test",
    "symbol":"BTCUSDT",
    "price_interval":0.5,
    "order_quantity":0.001,
    "buy_window_size":10
  }'

# 预期响应：{"success":true,...}
```

## 常见问题

### Q: 我忘记密码了怎么办？

A: 删除认证数据库重新设置：
```bash
./stop.sh
rm data/auth.db
./start.sh
# 然后重新设置密码
```

### Q: 我想修改配置但忘记登录了？

A: 必须先登录才能修改配置：
1. 访问 Web UI
2. 输入密码登录
3. 然后修改配置

### Q: 如何知道系统是否被攻击过？

A: 检查以下几点：
1. `data/auth.db` 的修改时间是否异常
2. `config.yaml` 的修改时间是否异常
3. 日志中是否有来自陌生IP的请求
4. 配置文件中的API Key是否是你的

### Q: 我的系统已经部署到公网，现在怎么办？

A: 立即执行"紧急应对措施"部分的所有步骤！

## 总结

这个安全修复确保了：

1. ✅ 密码只能在首次设置时通过公开接口设置
2. ✅ 配置只能在首次设置或登录后修改
3. ✅ 防止未授权的配置覆盖
4. ✅ 防止未授权的密码重置

**重要提醒**：
- 🔴 立即更新到修复版本
- 🔴 检查系统是否被攻击
- 🔴 不要将系统直接暴露到公网
- 🔴 使用强密码
- 🔴 定期检查日志

## 更新日期

2026-01-25

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
