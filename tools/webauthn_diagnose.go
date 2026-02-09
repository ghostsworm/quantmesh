//go:build tools

package main

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
)

// WebAuthn 診斷工具
// 幫助用戶檢查 WebAuthn 配置是否正確
func main() {
	fmt.Println("🔍 WebAuthn 配置診斷工具")
	fmt.Println("=====================================")
	
	if len(os.Args) < 2 {
		fmt.Println("❌ 使用方法: go run webauthn_diagnose.go <访问的URL>")
		fmt.Println("   示例: go run webauthn_diagnose.go https://qt.facev.app")
		fmt.Println("   示例: go run webauthn_diagnose.go http://localhost:28888")
		os.Exit(1)
	}
	
	accessURL := os.Args[1]
	
	// 解析URL
	u, err := url.Parse(accessURL)
	if err != nil {
		fmt.Printf("❌ 無效的URL: %v\n", err)
		os.Exit(1)
	}
	
	fmt.Printf("📍 分析URL: %s\n", accessURL)
	fmt.Printf("   - 协议: %s\n", u.Scheme)
	fmt.Printf("   - 域名: %s\n", u.Hostname())
	fmt.Printf("   - 端口: %s\n", u.Port())
	fmt.Println()
	
	// 檢查协议
	fmt.Println("🔒 协议檢查:")
	if u.Scheme == "https" {
		fmt.Println("   ✅ 使用 HTTPS - WebAuthn 支持")
	} else if u.Scheme == "http" {
		if u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" {
			fmt.Println("   ⚠️  使用 HTTP - 仅本地开发支持 WebAuthn")
		} else {
			fmt.Println("   ❌ 使用 HTTP - 生产环境 WebAuthn 需要 HTTPS")
		}
	} else {
		fmt.Printf("   ❌ 不支持的协议: %s\n", u.Scheme)
	}
	fmt.Println()
	
	// 域名分析
	fmt.Println("🌐 域名分析:")
	hostname := u.Hostname()
	if hostname == "" {
		fmt.Println("   ❌ 無效的域名")
		os.Exit(1)
	}
	
	// 檢查是否是IP地址
	if net.ParseIP(hostname) != nil {
		fmt.Printf("   ⚠️  IP地址: %s - WebAuthn 偏好使用域名\n", hostname)
		if hostname != "127.0.0.1" && hostname != "localhost" {
			fmt.Println("   ❌ 公网IP不支持 WebAuthn，需要使用域名")
		}
	} else {
		fmt.Printf("   ✅ 域名: %s - 適合 WebAuthn\n", hostname)
		
		// 檢查域名層級
		parts := strings.Split(hostname, ".")
		if len(parts) >= 2 {
			fmt.Printf("   ✅ 有效的域名結構（%d 層）\n", len(parts))
		} else {
			fmt.Printf("   ⚠️  域名結構可能有問題（%d 層）\n", len(parts))
		}
	}
	fmt.Println()
	
	// 生成建議的配置
	fmt.Println("⚙️  建議的配置:")
	fmt.Println("config.yaml:")
	fmt.Println("web:")
	fmt.Printf("  domain: \"%s\"\n", hostname)
	if u.Scheme == "https" {
		fmt.Println("  tls:")
		fmt.Println("    enabled: true")
		fmt.Println("    cert_file: \"/path/to/your/cert.pem\"")
		fmt.Println("    key_file: \"/path/to/your/key.pem\"")
	}
	fmt.Println()
	
	// 環境變數建議
	fmt.Println("或者使用環境變數:")
	fmt.Printf("export DOMAIN=%s\n", hostname)
	fmt.Println()
	
	// 常見問題解決
	fmt.Println("🛠️  常見問題解決:")
	fmt.Println("1. RPID 不匹配錯誤:")
	fmt.Println("   - 確保配置的 domain 与訪問的域名完全一致")
	fmt.Printf("   - 當前需要配置: %s\n", hostname)
	fmt.Println()
	
	fmt.Println("2. WebAuthn 不工作:")
	fmt.Println("   - 生產環境必須使用 HTTPS")
	fmt.Println("   - 確保瀏覽器支持 WebAuthn")
	fmt.Println("   - 確保設備支持生物識別")
	fmt.Println()
	
	fmt.Println("3. 證書配置（HTTPS）:")
	fmt.Println("   - 使用 Let's Encrypt 免費證書")
	fmt.Println("   - 或使用反向代理（如 Nginx、Cloudflare）提供 HTTPS")
	fmt.Println()
	
	fmt.Println("✅ 診斷完成！根據上述建議修改配置後重啟應用。")
}