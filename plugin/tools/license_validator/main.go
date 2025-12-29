package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"quantmesh/plugin"
)

func main() {
	// 命令行参数
	licenseKey := flag.String("key", "", "许可证密钥")

	flag.Parse()

	// 验证必填参数
	if *licenseKey == "" {
		fmt.Println("错误: 必须指定 -key 参数")
		flag.Usage()
		os.Exit(1)
	}

	// 解析许可证
	info, err := plugin.ParseLicense(*licenseKey)
	if err != nil {
		fmt.Printf("❌ 许可证解析失败: %v\n", err)
		os.Exit(1)
	}

	// 显示许可证信息
	fmt.Println("✅ 许可证解析成功!")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("插件名称: %s\n", info.PluginName)
	fmt.Printf("客户ID:   %s\n", info.CustomerID)
	fmt.Printf("签发时间: %s\n", info.IssuedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("有效期至: %s\n", info.ExpiryDate.Format("2006-01-02 15:04:05"))
	fmt.Printf("最大实例: %d\n", info.MaxInstances)
	fmt.Printf("授权功能: %v\n", info.Features)
	if info.MachineID != "" {
		fmt.Printf("机器ID:   %s\n", info.MachineID)
	}
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	// 检查是否过期
	if time.Now().After(info.ExpiryDate) {
		fmt.Printf("\n⚠️  警告: 许可证已过期 (%s)\n", info.ExpiryDate.Format("2006-01-02"))
	} else {
		daysLeft := int(time.Until(info.ExpiryDate).Hours() / 24)
		fmt.Printf("\n✅ 许可证有效 (剩余 %d 天)\n", daysLeft)
	}

	// 验证签名
	validator := plugin.NewLicenseValidator()
	if err := validator.ValidatePlugin(info.PluginName, *licenseKey); err != nil {
		fmt.Printf("\n❌ 许可证验证失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✅ 许可证签名验证通过")
}

