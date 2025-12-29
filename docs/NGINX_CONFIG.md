# Nginx 反向代理配置指南

本文档介绍如何使用 Nginx 作为 QuantMesh 的反向代理，提供 HTTPS、Rate Limiting、IP 白名单等安全功能。

## 基础配置

### 1. 安装 Nginx

```bash
# macOS
brew install nginx

# Ubuntu/Debian
sudo apt update
sudo apt install nginx

# CentOS/RHEL
sudo yum install nginx
```

### 2. 基础反向代理配置

创建配置文件 `/etc/nginx/sites-available/quantmesh`：

```nginx
# QuantMesh 反向代理配置
upstream quantmesh_backend {
    # 单实例配置
    server 127.0.0.1:28888 max_fails=3 fail_timeout=30s;
    
    # 多实例配置（负载均衡）
    # server 127.0.0.1:28888 weight=1;
    # server 127.0.0.1:28889 weight=1;
    # server 127.0.0.1:28890 weight=1 backup;  # 备用实例
    
    # 保持连接
    keepalive 32;
}

server {
    listen 80;
    server_name your-domain.com;
    
    # 重定向 HTTP 到 HTTPS
    return 301 https://$server_name$request_uri;
}

server {
    listen 443 ssl http2;
    server_name your-domain.com;
    
    # SSL 证书配置
    ssl_certificate /path/to/ssl/cert.pem;
    ssl_certificate_key /path/to/ssl/key.pem;
    
    # SSL 安全配置
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers 'ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384';
    ssl_prefer_server_ciphers on;
    ssl_session_cache shared:SSL:10m;
    ssl_session_timeout 10m;
    
    # HSTS (可选，强制 HTTPS)
    add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;
    
    # 安全头
    add_header X-Frame-Options "SAMEORIGIN" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header X-XSS-Protection "1; mode=block" always;
    add_header Referrer-Policy "no-referrer-when-downgrade" always;
    
    # 日志
    access_log /var/log/nginx/quantmesh_access.log;
    error_log /var/log/nginx/quantmesh_error.log;
    
    # 客户端请求限制
    client_max_body_size 10M;
    client_body_timeout 60s;
    client_header_timeout 60s;
    
    # 代理配置
    location / {
        proxy_pass http://quantmesh_backend;
        proxy_http_version 1.1;
        
        # 传递真实客户端信息
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        
        # WebSocket 支持
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        
        # 超时配置
        proxy_connect_timeout 60s;
        proxy_send_timeout 60s;
        proxy_read_timeout 60s;
        
        # 缓冲配置
        proxy_buffering off;
        proxy_request_buffering off;
    }
    
    # Prometheus metrics（可选：限制访问）
    location /metrics {
        # 只允许 Prometheus 服务器访问
        allow 127.0.0.1;
        allow 10.0.0.0/8;  # 内网
        deny all;
        
        proxy_pass http://quantmesh_backend;
        proxy_set_header Host $host;
    }
    
    # pprof 端点（调试用，生产环境建议禁用或严格限制）
    location /debug/pprof/ {
        # 只允许特定 IP 访问
        allow 127.0.0.1;
        allow YOUR_OFFICE_IP;
        deny all;
        
        proxy_pass http://quantmesh_backend;
        proxy_set_header Host $host;
    }
}
```

### 3. 启用配置

```bash
# 创建符号链接
sudo ln -s /etc/nginx/sites-available/quantmesh /etc/nginx/sites-enabled/

# 测试配置
sudo nginx -t

# 重载配置
sudo nginx -s reload

# 或重启 Nginx
sudo systemctl restart nginx
```

## Rate Limiting（速率限制）

### 防止暴力破解

```nginx
# 在 http 块中定义限流区域
http {
    # 限制登录接口：每个 IP 每分钟最多 5 次请求
    limit_req_zone $binary_remote_addr zone=login_limit:10m rate=5r/m;
    
    # 限制 API 接口：每个 IP 每秒最多 10 次请求
    limit_req_zone $binary_remote_addr zone=api_limit:10m rate=10r/s;
    
    # 限制 WebSocket 连接：每个 IP 最多 10 个并发连接
    limit_conn_zone $binary_remote_addr zone=ws_limit:10m;
    
    server {
        # ... 其他配置 ...
        
        # 登录接口限流
        location ~ ^/api/(auth|webauthn)/ {
            limit_req zone=login_limit burst=10 nodelay;
            limit_req_status 429;
            
            proxy_pass http://quantmesh_backend;
            # ... 其他 proxy 配置 ...
        }
        
        # API 接口限流
        location /api/ {
            limit_req zone=api_limit burst=20 nodelay;
            limit_req_status 429;
            
            proxy_pass http://quantmesh_backend;
            # ... 其他 proxy 配置 ...
        }
        
        # WebSocket 连接限制
        location /ws {
            limit_conn ws_limit 10;
            limit_conn_status 429;
            
            proxy_pass http://quantmesh_backend;
            # ... WebSocket 配置 ...
        }
    }
}
```

### 自定义错误页面

```nginx
# 429 Too Many Requests
error_page 429 /429.html;
location = /429.html {
    root /var/www/errors;
    internal;
}
```

创建 `/var/www/errors/429.html`：

```html
<!DOCTYPE html>
<html>
<head>
    <title>Too Many Requests</title>
</head>
<body>
    <h1>429 Too Many Requests</h1>
    <p>您的请求过于频繁，请稍后再试。</p>
</body>
</html>
```

## IP 白名单

### 方法 1：基于 IP 地址

```nginx
server {
    # ... 其他配置 ...
    
    # 全局 IP 白名单
    allow 1.2.3.4;           # 办公室 IP
    allow 5.6.7.8;           # 家庭 IP
    allow 10.0.0.0/8;        # 内网
    allow 172.16.0.0/12;     # 内网
    allow 192.168.0.0/16;    # 内网
    deny all;
    
    location / {
        proxy_pass http://quantmesh_backend;
        # ... 其他配置 ...
    }
}
```

### 方法 2：基于 geo 模块（按地理位置）

```nginx
http {
    # 定义地理位置白名单
    geo $allowed_country {
        default 0;
        CN 1;  # 中国
        US 1;  # 美国
        JP 1;  # 日本
    }
    
    server {
        # ... 其他配置 ...
        
        location / {
            if ($allowed_country = 0) {
                return 403;
            }
            
            proxy_pass http://quantmesh_backend;
            # ... 其他配置 ...
        }
    }
}
```

### 方法 3：动态 IP 白名单（使用 map）

```nginx
http {
    # 从文件读取 IP 白名单
    map $remote_addr $ip_whitelist {
        include /etc/nginx/ip_whitelist.conf;
        default 0;
    }
    
    server {
        # ... 其他配置 ...
        
        location / {
            if ($ip_whitelist = 0) {
                return 403;
            }
            
            proxy_pass http://quantmesh_backend;
            # ... 其他配置 ...
        }
    }
}
```

创建 `/etc/nginx/ip_whitelist.conf`：

```nginx
1.2.3.4 1;
5.6.7.8 1;
10.0.0.0/8 1;
```

## SSL/TLS 配置

### 使用 Let's Encrypt 免费证书

#### 1. 安装 Certbot

```bash
# Ubuntu/Debian
sudo apt install certbot python3-certbot-nginx

# CentOS/RHEL
sudo yum install certbot python3-certbot-nginx

# macOS
brew install certbot
```

#### 2. 获取证书

```bash
# 自动配置 Nginx
sudo certbot --nginx -d your-domain.com

# 或手动获取证书
sudo certbot certonly --nginx -d your-domain.com
```

#### 3. 自动续期

```bash
# 测试续期
sudo certbot renew --dry-run

# 添加到 crontab
sudo crontab -e

# 每天检查并续期
0 0 * * * certbot renew --quiet
```

### SSL 最佳实践配置

```nginx
server {
    listen 443 ssl http2;
    server_name your-domain.com;
    
    # 证书配置
    ssl_certificate /etc/letsencrypt/live/your-domain.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/your-domain.com/privkey.pem;
    ssl_trusted_certificate /etc/letsencrypt/live/your-domain.com/chain.pem;
    
    # SSL 协议和加密套件
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers 'ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-CHACHA20-POLY1305:ECDHE-RSA-CHACHA20-POLY1305:DHE-RSA-AES128-GCM-SHA256:DHE-RSA-AES256-GCM-SHA384';
    ssl_prefer_server_ciphers off;
    
    # SSL 会话缓存
    ssl_session_cache shared:SSL:50m;
    ssl_session_timeout 1d;
    ssl_session_tickets off;
    
    # OCSP Stapling
    ssl_stapling on;
    ssl_stapling_verify on;
    resolver 8.8.8.8 8.8.4.4 valid=300s;
    resolver_timeout 5s;
    
    # HSTS
    add_header Strict-Transport-Security "max-age=63072000; includeSubDomains; preload" always;
    
    # ... 其他配置 ...
}
```

## 负载均衡

### 轮询（默认）

```nginx
upstream quantmesh_backend {
    server 127.0.0.1:28888;
    server 127.0.0.1:28889;
    server 127.0.0.1:28890;
}
```

### 加权轮询

```nginx
upstream quantmesh_backend {
    server 127.0.0.1:28888 weight=3;  # 权重 3
    server 127.0.0.1:28889 weight=2;  # 权重 2
    server 127.0.0.1:28890 weight=1;  # 权重 1
}
```

### IP Hash（会话保持）

```nginx
upstream quantmesh_backend {
    ip_hash;
    server 127.0.0.1:28888;
    server 127.0.0.1:28889;
    server 127.0.0.1:28890;
}
```

### 最少连接

```nginx
upstream quantmesh_backend {
    least_conn;
    server 127.0.0.1:28888;
    server 127.0.0.1:28889;
    server 127.0.0.1:28890;
}
```

### 健康检查

```nginx
upstream quantmesh_backend {
    server 127.0.0.1:28888 max_fails=3 fail_timeout=30s;
    server 127.0.0.1:28889 max_fails=3 fail_timeout=30s;
    server 127.0.0.1:28890 max_fails=3 fail_timeout=30s backup;
    
    # 健康检查
    check interval=3000 rise=2 fall=3 timeout=1000 type=http;
    check_http_send "HEAD /api/health HTTP/1.0\r\n\r\n";
    check_http_expect_alive http_2xx http_3xx;
}
```

## 日志分析

### 自定义日志格式

```nginx
http {
    # 定义日志格式
    log_format quantmesh '$remote_addr - $remote_user [$time_local] '
                         '"$request" $status $body_bytes_sent '
                         '"$http_referer" "$http_user_agent" '
                         '$request_time $upstream_response_time '
                         '$upstream_addr $upstream_status';
    
    server {
        access_log /var/log/nginx/quantmesh_access.log quantmesh;
        # ... 其他配置 ...
    }
}
```

### 日志分析工具

#### GoAccess（实时日志分析）

```bash
# 安装
sudo apt install goaccess

# 实时分析
goaccess /var/log/nginx/quantmesh_access.log -o /var/www/html/report.html --log-format=COMBINED --real-time-html

# 访问报告
open http://your-server/report.html
```

## 安全加固清单

- [ ] 启用 HTTPS（TLS 1.2+）
- [ ] 配置 HSTS
- [ ] 添加安全响应头
- [ ] 配置 Rate Limiting
- [ ] 设置 IP 白名单
- [ ] 限制 pprof 端点访问
- [ ] 限制 metrics 端点访问
- [ ] 配置防火墙规则
- [ ] 启用访问日志
- [ ] 定期更新 SSL 证书
- [ ] 隐藏 Nginx 版本号
- [ ] 禁用不必要的 HTTP 方法
- [ ] 配置超时限制
- [ ] 启用 gzip 压缩（可选）

### 隐藏 Nginx 版本号

```nginx
http {
    server_tokens off;
    # ... 其他配置 ...
}
```

### 禁用不必要的 HTTP 方法

```nginx
server {
    # ... 其他配置 ...
    
    location / {
        limit_except GET POST PUT DELETE {
            deny all;
        }
        
        proxy_pass http://quantmesh_backend;
        # ... 其他配置 ...
    }
}
```

## 监控 Nginx

### 启用 stub_status

```nginx
server {
    listen 127.0.0.1:8080;
    
    location /nginx_status {
        stub_status on;
        access_log off;
        allow 127.0.0.1;
        deny all;
    }
}
```

访问 `http://127.0.0.1:8080/nginx_status` 查看状态。

### 集成到 Prometheus

使用 [nginx-prometheus-exporter](https://github.com/nginxinc/nginx-prometheus-exporter)：

```bash
# 下载并运行 exporter
docker run -p 9113:9113 nginx/nginx-prometheus-exporter:latest -nginx.scrape-uri=http://127.0.0.1:8080/nginx_status
```

在 Prometheus 配置中添加：

```yaml
scrape_configs:
  - job_name: 'nginx'
    static_configs:
      - targets: ['localhost:9113']
```

## 故障排查

### 常见问题

1. **502 Bad Gateway**
   - 检查后端服务是否运行
   - 检查 upstream 配置是否正确
   - 查看 error.log

2. **504 Gateway Timeout**
   - 增加超时时间
   - 检查后端服务响应时间

3. **WebSocket 连接失败**
   - 确认已配置 Upgrade 和 Connection 头
   - 检查超时设置

### 调试命令

```bash
# 测试配置
sudo nginx -t

# 查看错误日志
sudo tail -f /var/log/nginx/error.log

# 查看访问日志
sudo tail -f /var/log/nginx/quantmesh_access.log

# 重载配置
sudo nginx -s reload

# 查看 Nginx 进程
ps aux | grep nginx
```

## 参考资源

- [Nginx 官方文档](https://nginx.org/en/docs/)
- [Mozilla SSL Configuration Generator](https://ssl-config.mozilla.org/)
- [Let's Encrypt 文档](https://letsencrypt.org/docs/)
- [Nginx Rate Limiting](https://www.nginx.com/blog/rate-limiting-nginx/)

