# WebSocket 代理支持说明

## 为什么 gorilla/websocket 对 SOCKS5 支持有限？

### 技术原因

1. **协议差异**：
   - **HTTP/HTTPS 代理**：使用 HTTP CONNECT 方法建立隧道
   - **SOCKS5 代理**：使用完全不同的协议（SOCKS5 握手协议）

2. **gorilla/websocket 的限制**：
   - `websocket.Dialer.Proxy` 字段使用的是 `http.ProxyURL()`
   - `http.ProxyURL()` 只支持 HTTP/HTTPS 代理协议
   - 如果传入 `socks5://` URL，`http.ProxyURL()` 无法识别，连接会失败

3. **当前代码的问题**：
   ```go
   dialer.Proxy = http.ProxyURL(proxyURL)  // 这里只支持 http/https
   ```
   如果 proxyURL 是 `socks5://127.0.0.1:7895`，这个函数无法正确处理。

### 解决方案

#### 方案 1：使用 HTTP 代理（推荐，最简单）

大多数代理工具（如 Clash、V2Ray、Shadowsocks）都支持 HTTP 模式：

**设置方法**：
```bash
# 使用 HTTP 代理（注意是 http://，不是 socks5://）
export https_proxy=http://127.0.0.1:7895
export http_proxy=http://127.0.0.1:7895
```

**为什么这样能工作**：
- 代理工具通常同时提供 SOCKS5 和 HTTP 两种端口
- 例如：Clash 默认 SOCKS5 端口是 7891，HTTP 端口是 7890
- 使用 HTTP 端口，gorilla/websocket 就能正常工作

#### 方案 2：添加 SOCKS5 支持（需要额外代码）

如果需要支持 SOCKS5，可以使用 `golang.org/x/net/proxy` 库：

**实现步骤**：
1. 检测代理协议类型
2. 如果是 SOCKS5，使用 `golang.org/x/net/proxy` 创建特殊的 Dialer
3. 如果是 HTTP/HTTPS，使用现有的 `http.ProxyURL()` 方式

**示例代码**：
```go
import (
    "golang.org/x/net/proxy"
    "net"
)

func getProxyDialer() *websocket.Dialer {
    dialer := &websocket.Dialer{
        HandshakeTimeout: 10 * time.Second,
    }
    
    proxyURL := getProxyFromEnv()
    if proxyURL == nil {
        return dialer
    }
    
    // 检测协议类型
    switch proxyURL.Scheme {
    case "socks5", "socks5h":
        // 使用 SOCKS5 代理
        dialer.NetDial = func(network, addr string) (net.Conn, error) {
            socksDialer, err := proxy.SOCKS5("tcp", proxyURL.Host, nil, proxy.Direct)
            if err != nil {
                return nil, err
            }
            return socksDialer.Dial(network, addr)
        }
    case "http", "https":
        // 使用 HTTP/HTTPS 代理
        dialer.Proxy = http.ProxyURL(proxyURL)
    }
    
    return dialer
}
```

### 推荐做法

**对于大多数用户**：使用方案 1（HTTP 代理）

1. 检查你的代理工具配置，找到 HTTP 端口
2. 设置环境变量使用 HTTP 端口：
   ```bash
   export https_proxy=http://127.0.0.1:7895  # 使用 HTTP 端口
   ```
3. 这样就能正常工作，无需修改代码

**如果你的代理工具只提供 SOCKS5**：
- 可以考虑使用方案 2，添加 SOCKS5 支持
- 或者使用代理转换工具，将 SOCKS5 转换为 HTTP 代理

### 常见代理工具端口配置

| 代理工具 | SOCKS5 端口 | HTTP 端口 | 说明 |
|---------|-----------|----------|------|
| Clash | 7891 | 7890 | 推荐使用 7890（HTTP） |
| V2Ray | 1080 | 通常需要配置 | 需要启用 HTTP 入站 |
| Shadowsocks | 1080 | 通常需要配置 | 需要配合其他工具 |

### 总结

- **gorilla/websocket 默认只支持 HTTP/HTTPS 代理**
- **SOCKS5 需要额外代码支持**
- **推荐使用 HTTP 代理端口，最简单可靠**

