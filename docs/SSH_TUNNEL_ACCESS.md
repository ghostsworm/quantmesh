# SSH 隧道访问指南

## 配置说明

系统现在只监听在 `127.0.0.1:28888`（localhost），这意味着：
- ✅ **安全**：外部无法直接访问
- ✅ **简单**：不需要配置防火墙
- ✅ **灵活**：通过 SSH 隧道访问

## 访问方法

### 方法1: SSH 隧道（推荐）

在你的**本地电脑**上运行：

```bash
# 建立 SSH 隧道
ssh -L 28888:localhost:28888 root@facev.app

# 保持这个终端窗口打开
```

然后在浏览器访问：
```
http://localhost:28888
```

**优点**：
- 最安全
- 不需要额外配置
- 所有流量都经过 SSH 加密

### 方法2: SSH 隧道（后台运行）

如果你想让隧道在后台运行：

```bash
# 后台运行 SSH 隧道
ssh -fNL 28888:localhost:28888 root@facev.app

# 检查隧道是否运行
ps aux | grep "ssh.*28888"

# 关闭隧道
pkill -f "ssh.*28888.*facev.app"
```

### 方法3: SSH 配置文件（自动化）

编辑 `~/.ssh/config`：

```
Host quantmesh
    HostName facev.app
    User root
    LocalForward 28888 localhost:28888
    ServerAliveInterval 60
    ServerAliveCountMax 3
```

然后只需运行：
```bash
ssh quantmesh
```

访问：`http://localhost:28888`

## 配置详情

### 服务器配置

文件（若使用磁盘导入副本；权威在 **`app_config`**）：`/root/quantmesh/config.yaml`

```yaml
web:
    enabled: true
    host: 127.0.0.1  # 只监听 localhost
    port: 28888
```

### 验证配置

在服务器上运行：
```bash
# 检查监听地址
ss -tlnp | grep quantmesh

# 应该看到：
# LISTEN 0 4096 127.0.0.1:28888 0.0.0.0:* users:(("quantmesh",pid=xxx,fd=20))
```

## 常见问题

### Q: 为什么访问 http://facev.app:28888 无法连接？

A: 因为系统现在只监听 localhost，外部无法直接访问。这是**故意的安全设置**。你需要通过 SSH 隧道访问。

### Q: SSH 隧道断开后怎么办？

A: 重新运行 SSH 隧道命令即可：
```bash
ssh -L 28888:localhost:28888 root@facev.app
```

### Q: 可以同时有多个人访问吗？

A: 可以，每个人在自己的电脑上建立 SSH 隧道即可。

### Q: 如何让其他人访问？

**方案1**: 给他们服务器 SSH 权限，让他们自己建立隧道

**方案2**: 如果需要让没有 SSH 权限的人访问，可以：
1. 修改配置改回 `host: 0.0.0.0`
2. 配置防火墙限制 IP
3. 使用 Nginx 反向代理 + 认证

### Q: 我在服务器上可以访问吗？

A: 可以，在服务器上运行：
```bash
curl http://localhost:28888/api/version
```

## 安全优势

使用 localhost + SSH 隧道的优势：

1. **零暴露**：Web 服务不暴露到公网
2. **SSH 加密**：所有流量都经过 SSH 加密
3. **SSH 认证**：使用 SSH 密钥认证，比密码更安全
4. **无需防火墙**：不需要配置复杂的防火墙规则
5. **审计日志**：SSH 登录都有日志记录

## 恢复公网访问（不推荐）

如果你确实需要公网访问：

```bash
# 1. 修改配置
ssh root@facev.app
cd /root/quantmesh
sed -i 's/host: 127.0.0.1/host: 0.0.0.0/' config.yaml  # 改后需 migrate 或 Web 保存

# 2. 重启服务
systemctl restart quantmesh

# 3. 配置防火墙（必须！）
ufw allow from YOUR_IP to any port 28888
ufw enable
```

**但强烈建议使用 SSH 隧道方式！**

## 自动化脚本

创建一个本地脚本 `connect-quantmesh.sh`：

```bash
#!/bin/bash
echo "🔗 连接到 QuantMesh..."
echo "访问地址: http://localhost:28888"
echo "按 Ctrl+C 断开连接"
ssh -L 28888:localhost:28888 root@facev.app
```

使用：
```bash
chmod +x connect-quantmesh.sh
./connect-quantmesh.sh
```

## 监控和维护

### 检查服务状态

```bash
ssh root@facev.app "systemctl status quantmesh"
```

### 查看日志

```bash
ssh root@facev.app "tail -f /root/quantmesh/logs/quantmesh.log"
```

### 重启服务

```bash
ssh root@facev.app "systemctl restart quantmesh"
```

## 总结

✅ **当前配置**：
- 监听地址：`127.0.0.1:28888`
- 访问方式：SSH 隧道
- 安全级别：⭐⭐⭐⭐⭐

✅ **优势**：
- 最安全的访问方式
- 不需要配置防火墙
- 所有流量加密
- 简单易用

✅ **使用方法**：
```bash
# 本地运行
ssh -L 28888:localhost:28888 root@facev.app

# 浏览器访问
http://localhost:28888
```

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
