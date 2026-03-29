# WebAuthn RPID 錯誤修復指南

## 問題描述

當您嘗試注册 WebAuthn 指紋時遇到以下錯誤：

```
The relying party ID is not a registrable domain suffix of, nor equal to the current domain. Subsequently, an attempt to fetch the .well-known/webauthn resource of the claimed RP ID failed.
```

这個錯誤表示 WebAuthn 的 **Relying Party ID (RPID)** 配置与實際訪問的域名不匹配。

## 根本原因

- **問題**: 代碼中 RPID 硬編碼為 `"localhost"`
- **實際情況**: 您通過 `qt.facev.app` 域名訪問應用
- **WebAuthn 要求**: RPID 必須与實際訪問域名匹配

## 解決方案

### 方案 1：配置文件修复（推薦）

1. **修改 `config.yaml`** 文件，添加域名配置：

```yaml
web:
  enabled: true
  host: "0.0.0.0"
  port: 28888
  domain: "qt.facev.app"  # 添加这行 - 设置为您的实际域名
  
  # 如果使用 HTTPS（推薦）
  tls:
    enabled: true
    cert_file: "/path/to/your/cert.pem"
    key_file: "/path/to/your/key.pem"
```

2. **重啟應用**:

```bash
# 停止應用
./stop.sh

# 啟動應用  
./start.sh
```

### 方案 2：環境變數（临時修復）

如果暫時無法修改配置文件，可以设置環境變數：

```bash
export DOMAIN=qt.facev.app
./start.sh
```

### 方案 3：代碼已自動修复

此次更新已經修改了代碼邏輯：

1. **優先級順序**:
   - 環境變數 `DOMAIN`
   - 配置文件 `web.domain`  
   - 配置文件 `web.host`
   - 後備方案 `localhost`

2. **自動检测 HTTPS**:
   - 根据 TLS 配置和端口自動生成正确的 Origin

## 验证修復

### 1. 检查日志

重啟后查看日誌，應該看到：

```
✅ WebAuthn 管理器已初始化 (rpID=qt.facev.app, rpOrigin=https://qt.facev.app)
```

### 2. 使用診斷工具

```bash
go run tools/webauthn_diagnose.go https://qt.facev.app
```

### 3. 测试指纹注册

1. 訪问設置頁面
2. 點击"注册新设備"
3. 填写設备名稱
4. 應該不再出現 RPID 錯誤

## 技術详情

### WebAuthn RPID 規則

1. **域名匹配**: RPID 必须是訪問域名的後缀
2. **HTTPS 要求**: 生產環境必须使用 HTTPS
3. **本地開发**: `localhost` 和 `127.0.0.1` 可以使用 HTTP

### 修复後的邏輯

```go
// 修復前（硬编码）
rpID := "localhost" // ❌ 總是 localhost

// 修復后（動態配置）
if domain := os.Getenv("DOMAIN"); domain != "" {
    rpID = domain  // ✅ 使用環境變數
} else if cfg.Web.Domain != "" {
    rpID = cfg.Web.Domain  // ✅ 使用配置文件
} else {
    rpID = "localhost"  // ✅ 後备方案
}
```

## 常見問題

### Q: 為什么需要 HTTPS？

A: WebAuthn 標準要求生產環境使用 HTTPS，只有本地開発（localhost）可以使用 HTTP。

### Q: 證書怎么配置？

A: 可以使用：
- Let's Encrypt 免费證書
- 反向代理（Nginx、Cloudflare）提供 HTTPS
- 云服务商的負载均衡器

### Q: 子域名支持嗎？

A: 支持。RPID 可以是頂级域名（如 `example.com`），會匹配所有子域名（如 `app.example.com`）。

### Q: 多域名怎么处理？

A: 每個域名需要单独的 RPID 配置。如果有多個域名，建议使用環境變數或多個配置文件。

## 升级日誌

- **版本 1.0.0**: 添加動態 RPID 配置支持
- **版本 1.0.0**: 添加 TLS 配置支持  
- **版本 1.0.0**: 添加 WebAuthn 診斷工具

## 相關文档

- [WebAuthn 实现文档](./webauthn-implementation.md)
- [配置文件说明](../config.example.yaml)
- [部署指南](../scripts/DEPLOY_README.md)

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
