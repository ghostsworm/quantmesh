# 🔴 紧急安全警告

## 严重安全漏洞已修复

**日期**: 2026-01-25  
**严重程度**: 🔴 **严重**

### 问题

在之前的版本中，以下接口**没有认证保护**：

1. `POST /api/setup/init` - 任何人都可以重新初始化你的配置
2. `POST /api/auth/password/set` - 任何人都可以重置你的密码

**如果你的系统已部署到公网，攻击者可能已经控制了你的系统！**

### 立即行动

#### 1. 运行安全检查

```bash
cd /Users/rocky/Sites/btc/quantmesh-opensource
./scripts/security_check.sh
```

#### 2. 检查是否被攻击

查看以下文件的修改时间是否异常：
```bash
ls -la data/auth.db
ls -la config.yaml 2>/dev/null || true  # 若无磁盘 YAML，请检查 data/*.db / Web 导出
```

查看日志中是否有可疑活动：
```bash
grep "设置密码\|配置初始化\|SECURITY" logs/*.log | tail -20
```

#### 3. 如果发现异常

**立即停止系统**：
```bash
./scripts/local/stop.sh
```

**检查配置**：
```bash
cat config.yaml  # 若存在；否则核对 app_config / Web
```

确认 API Key、Secret Key、交易对是否是你设置的。

**从备份恢复**（如果配置被篡改）：
```bash
ls backups/
cp backups/config_backup_YYYYMMDD_HHMMSS.yaml my-import.yaml && ./quantmesh --migrate-app-config my-import.yaml
```

**重置密码**：
```bash
rm data/auth.db
```

#### 4. 更新到修复版本

```bash
# 拉取最新代码
git pull origin main

# 重新编译
go build -o quantmesh

# 重启系统
./scripts/local/start.sh
```

### 修复内容

✅ 密码设置接口：只能在首次设置时使用  
✅ 配置初始化接口：已设置密码后需要登录才能修改  
✅ 添加详细的安全日志  

### 安全建议

🔴 **不要将系统直接暴露到公网！**

推荐方案：
- 使用 VPN 访问
- 使用 SSH 隧道
- 使用反向代理 + IP 白名单

### 详细文档

查看完整的安全修复文档：
```bash
cat docs/SECURITY_FIX_SETUP_AUTH.md
```

### 联系

如果你发现系统被攻击，请立即：
1. 停止系统
2. 检查配置和日志
3. 从备份恢复
4. 更新到最新版本
5. 加强网络安全措施

---

**再次强调：如果你的系统已部署到公网，请立即执行上述检查步骤！**
