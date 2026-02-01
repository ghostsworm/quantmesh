package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"quantmesh/plugin"
)

func main() {
	// 命令行参數
	licenseKey := flag.String("key", "", "許可证密钥")

	flag.Parse()

	// 驗证必填参數
	if *licenseKey == "" {
		fmt.Println("錯误: 必須指定 -key 参數")
		flag.Usage()
		os.Exit(1)
	}

	// 解析許可证
	info, err := plugin.ParseLicense(*licenseKey)
	if err != nil {
		fmt.Printf("❌ 許可证解析失败: %v\n", err)
		os.Exit(1)
	}

	// 显示許可证信息
	fmt.Println("✅ 許可证解析成功!")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("插件名称: %s\n", info.PluginName)
	fmt.Printf("客戶ID:   %s\n", info.CustomerID)
	fmt.Printf("签发時间: %s\n", info.IssuedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("有效期至: %s\n", info.ExpiryDate.Format("2006-01-02 15:04:05"))
	fmt.Printf("最大實例: %d\n", info.MaxInstances)
	fmt.Printf("授权功能: %v\n", info.Features)
	if info.MachineID != "" {
		fmt.Printf("机器ID:   %s\n", info.MachineID)
	}
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	// 检查是否過期
	if time.Now().After(info.ExpiryDate) {
		fmt.Printf("\n⚠️  警告: 許可证已過期 (%s)\n", info.ExpiryDate.Format("2006-01-02"))
	} else {
		daysLeft := int(time.Until(info.ExpiryDate).Hours() / 24)
		fmt.Printf("\n✅ 許可证有效 (剩餘 %d 天)\n", daysLeft)
	}

	// 驗证签名
	validator := plugin.NewLicenseValidator()
	if err := validator.ValidatePlugin(info.PluginName, *licenseKey); err != nil {
		fmt.Printf("\n❌ 許可证驗证失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✅ 許可证签名驗证通過")
}
