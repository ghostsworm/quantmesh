# 移动端访问指南

本指南将帮助你在移动设备上安全地访问 QuantMesh 做市商系统。

## 📱 方案概览

QuantMesh 提供 PWA（渐进式 Web 应用）方案，无需安装 APK，直接通过浏览器访问即可获得类原生应用的体验。

### 优势

- ✅ **安全可靠** - API 密钥存储在你自己的服务器，不经过第三方
- ✅ **跨平台** - iOS、Android、iPad 均可使用
- ✅ **免安装** - 无需下载 APK，避免假冒应用风险
- ✅ **实时更新** - 系统更新自动生效，无需手动升级
- ✅ **离线访问** - 支持基础功能离线查看

## 🌐 访问方式

### 方式一：本地网络访问（推荐）

适用场景：服务器和手机在同一局域网（如家庭 WiFi）

1. **启动服务器**
   ```bash
   cd /path/to/opensqt_market_maker
   ./quantmesh
   ```

2. **查看服务器 IP**
   ```bash
   # macOS/Linux
   ifconfig | grep "inet "
   # 或
   ip addr show
   ```

3. **手机访问**
   - 打开浏览器（推荐 Chrome/Safari）
   - 输入：`http://服务器IP:28888`
   - 例如：`http://192.168.1.100:28888`

### 方式二：Tailscale VPN（最安全）

Tailscale 提供零配置的点对点加密 VPN，非常适合远程访问。

#### 安装步骤

1. **服务器端**
   ```bash
   # macOS
   brew install tailscale
   
   # Ubuntu/Debian
   curl -fsSL https://tailscale.com/install.sh | sh
   
   # 启动并登录
   sudo tailscale up
   ```

2. **手机端**
   - iOS: App Store 搜索 "Tailscale"
   - Android: Google Play 搜索 "Tailscale"
   - 安装后使用同一账号登录

3. **访问系统**
   - 在 Tailscale 应用中查看服务器的 Tailscale IP（通常是 100.x.x.x）
   - 浏览器访问：`http://100.x.x.x:28888`

#### 优势
- ✅ 端到端加密
- ✅ 无需公网 IP
- ✅ 无需配置路由器
- ✅ 支持多设备
- ✅ 免费版足够个人使用

### 方式三：Cloudflare Tunnel（公网访问）

适用场景：需要在任何地方访问，且有域名

#### 配置步骤

1. **安装 cloudflared**
   ```bash
   # macOS
   brew install cloudflare/cloudflare/cloudflared
   
   # Linux
   wget https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-amd64.deb
   sudo dpkg -i cloudflared-linux-amd64.deb
   ```

2. **登录 Cloudflare**
   ```bash
   cloudflared tunnel login
   ```

3. **创建隧道**
   ```bash
   cloudflared tunnel create quantmesh
   ```

4. **配置隧道**
   创建 `~/.cloudflared/config.yml`:
   ```yaml
   tunnel: <你的隧道ID>
   credentials-file: /Users/你的用户名/.cloudflared/<隧道ID>.json
   
   ingress:
     - hostname: quantmesh.yourdomain.com
       service: http://localhost:28888
     - service: http_status:404
   ```

5. **配置 DNS**
   ```bash
   cloudflared tunnel route dns quantmesh quantmesh.yourdomain.com
   ```

6. **启动隧道**
   ```bash
   cloudflared tunnel run quantmesh
   ```

7. **访问系统**
   - 浏览器访问：`https://quantmesh.yourdomain.com`
   - Cloudflare 自动提供 HTTPS

#### 优势
- ✅ 自动 HTTPS
- ✅ 隐藏服务器真实 IP
- ✅ 免费使用
- ✅ DDoS 防护

### 方式四：frp 内网穿透

适用场景：没有公网 IP，需要临时远程访问

#### 配置步骤

1. **准备一台有公网 IP 的服务器（VPS）**

2. **服务器端配置**
   ```bash
   # 下载 frp
   wget https://github.com/fatedier/frp/releases/download/v0.52.0/frp_0.52.0_linux_amd64.tar.gz
   tar -xzf frp_0.52.0_linux_amd64.tar.gz
   cd frp_0.52.0_linux_amd64
   
   # 编辑 frps.ini
   cat > frps.ini << EOF
   [common]
   bind_port = 7000
   dashboard_port = 7500
   dashboard_user = admin
   dashboard_pwd = your_password
   token = your_secret_token
   EOF
   
   # 启动服务端
   ./frps -c frps.ini
   ```

3. **客户端配置（你的做市服务器）**
   ```bash
   # 下载 frp
   wget https://github.com/fatedier/frp/releases/download/v0.52.0/frp_0.52.0_darwin_amd64.tar.gz  # macOS
   tar -xzf frp_0.52.0_darwin_amd64.tar.gz
   cd frp_0.52.0_darwin_amd64
   
   # 编辑 frpc.ini
   cat > frpc.ini << EOF
   [common]
   server_addr = your_vps_ip
   server_port = 7000
   token = your_secret_token
   
   [quantmesh]
   type = http
   local_ip = 127.0.0.1
   local_port = 28888
   custom_domains = quantmesh.yourdomain.com
   EOF
   
   # 启动客户端
   ./frpc -c frpc.ini
   ```

4. **访问系统**
   - 浏览器访问：`http://quantmesh.yourdomain.com`

## 📲 安装 PWA 应用

### iOS (Safari)

1. 访问系统网址
2. 点击底部分享按钮 (⬆️)
3. 向下滚动，选择"添加到主屏幕"
4. 自定义名称，点击"添加"
5. 在主屏幕找到应用图标，点击打开

### Android (Chrome)

1. 访问系统网址
2. 点击右上角菜单 (⋮)
3. 选择"添加到主屏幕"或"安装应用"
4. 确认安装
5. 在应用抽屉或主屏幕找到图标

### 桌面浏览器 (Chrome/Edge)

1. 访问系统网址
2. 地址栏右侧会出现安装图标 (⊕)
3. 点击安装
4. 应用将作为独立窗口运行

## 🔒 安全最佳实践

### 1. API 密钥安全

- ✅ **禁用提现权限** - 在交易所后台禁用 API 的提现和转账权限
- ✅ **启用 IP 白名单** - 限制 API 只能从特定 IP 访问
- ✅ **使用子账户** - 创建专门的子账户用于做市，限制资金规模
- ✅ **定期更换密钥** - 每月更换一次 API 密钥

### 2. 网络安全

- ✅ **使用 HTTPS** - 生产环境必须使用 HTTPS（Cloudflare Tunnel 自动提供）
- ✅ **强密码** - 设置复杂的登录密码
- ✅ **双因素认证** - 启用 WebAuthn 生物识别登录
- ✅ **VPN 访问** - 优先使用 Tailscale 等 VPN 方案

### 3. 设备安全

- ✅ **设备加密** - 启用手机的全盘加密
- ✅ **屏幕锁** - 设置自动锁屏
- ✅ **生物识别** - 使用指纹或面容解锁
- ✅ **定期更新** - 保持系统和浏览器最新

### 4. 操作安全

- ✅ **敏感操作确认** - 平仓、修改配置等操作需要二次确认
- ✅ **审计日志** - 定期检查操作日志
- ✅ **异常监控** - 设置余额、持仓异常通知

## 🔧 故障排查

### 无法访问

1. **检查服务器是否运行**
   ```bash
   ps aux | grep quantmesh
   ```

2. **检查端口是否开放**
   ```bash
   netstat -an | grep 28888
   ```

3. **检查防火墙**
   ```bash
   # macOS
   sudo /usr/libexec/ApplicationFirewall/socketfilterfw --getglobalstate
   
   # Linux
   sudo ufw status
   ```

4. **检查网络连接**
   ```bash
   ping 服务器IP
   ```

### PWA 无法安装

1. **确保使用 HTTPS** - PWA 要求 HTTPS（localhost 除外）
2. **清除浏览器缓存** - 设置 > 隐私 > 清除浏览数据
3. **更新浏览器** - 确保使用最新版本
4. **检查 manifest.json** - 确保文件正确加载

### 性能问题

1. **启用 Service Worker** - 检查浏览器控制台
2. **减少轮询频率** - 调整数据刷新间隔
3. **使用 WiFi** - 避免使用移动数据
4. **关闭后台应用** - 释放手机内存

## 📞 获取帮助

- GitHub Issues: https://github.com/your-repo/issues
- 文档: https://github.com/your-repo/docs
- 社区: Telegram/Discord

## ⚠️ 重要提醒

1. **API 密钥始终存储在你自己的服务器上**，不会经过任何第三方
2. **系统启动时会自动检测 API 权限**，如发现提现权限会警告
3. **所有敏感操作都有审计日志**，可随时查看
4. **建议使用 VPN 方案**（Tailscale）而非公网暴露
5. **定期备份配置文件**和数据库

---

**安全第一，谨慎操作！**

