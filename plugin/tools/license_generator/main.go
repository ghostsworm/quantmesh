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
	pluginName := flag.String("plugin", "", "插件名称")
	customerID := flag.String("customer", "", "客戶ID")
	days := flag.Int("days", 365, "有效天數")
	maxInstances := flag.Int("instances", 1, "最大實例數")
	features := flag.String("features", "*", "授权功能 (逗号分隔)")
	machineID := flag.String("machine", "", "机器ID (可選)")
	secretKey := flag.String("secret", "quantmesh-secret-key-2025", "密钥")

	flag.Parse()

	// 驗证必填参數
	if *pluginName == "" || *customerID == "" {
		fmt.Println("錯误: 必須指定 -plugin 和 -customer 参數")
		flag.Usage()
		os.Exit(1)
	}

	// 解析功能列表
	featureList := []string{}
	if *features != "" {
		featureList = append(featureList, *features)
	}

	// 计算過期時间
	expiryDate := time.Now().AddDate(0, 0, *days)

	// 生成許可证
	licenseKey, err := plugin.GenerateLicense(
		*pluginName,
		*customerID,
		expiryDate,
		*maxInstances,
		featureList,
		*machineID,
		*secretKey,
	)

	if err != nil {
		fmt.Printf("❌ 生成許可证失败: %v\n", err)
		os.Exit(1)
	}

	// 输出結果
	fmt.Println("✅ 許可证生成成功!")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("插件名称: %s\n", *pluginName)
	fmt.Printf("客戶ID:   %s\n", *customerID)
	fmt.Printf("有效期至: %s\n", expiryDate.Format("2006-01-02"))
	fmt.Printf("最大實例: %d\n", *maxInstances)
	fmt.Printf("授权功能: %s\n", *features)
	if *machineID != "" {
		fmt.Printf("机器ID:   %s\n", *machineID)
	}
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("\n許可证密钥:")
	fmt.Println(licenseKey)
	fmt.Println("\n请將此密钥提供给客戶")
}
