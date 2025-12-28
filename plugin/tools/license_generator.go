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
	pluginName := flag.String("plugin", "", "插件名称")
	customerID := flag.String("customer", "", "客户ID")
	days := flag.Int("days", 365, "有效天数")
	maxInstances := flag.Int("instances", 1, "最大实例数")
	features := flag.String("features", "*", "授权功能 (逗号分隔)")
	machineID := flag.String("machine", "", "机器ID (可选)")
	secretKey := flag.String("secret", "quantmesh-secret-key-2025", "密钥")

	flag.Parse()

	// 验证必填参数
	if *pluginName == "" || *customerID == "" {
		fmt.Println("错误: 必须指定 -plugin 和 -customer 参数")
		flag.Usage()
		os.Exit(1)
	}

	// 解析功能列表
	featureList := []string{}
	if *features != "" {
		featureList = append(featureList, *features)
	}

	// 计算过期时间
	expiryDate := time.Now().AddDate(0, 0, *days)

	// 生成许可证
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
		fmt.Printf("❌ 生成许可证失败: %v\n", err)
		os.Exit(1)
	}

	// 输出结果
	fmt.Println("✅ 许可证生成成功!")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("插件名称: %s\n", *pluginName)
	fmt.Printf("客户ID:   %s\n", *customerID)
	fmt.Printf("有效期至: %s\n", expiryDate.Format("2006-01-02"))
	fmt.Printf("最大实例: %d\n", *maxInstances)
	fmt.Printf("授权功能: %s\n", *features)
	if *machineID != "" {
		fmt.Printf("机器ID:   %s\n", *machineID)
	}
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("\n许可证密钥:")
	fmt.Println(licenseKey)
	fmt.Println("\n请将此密钥提供给客户")
}

