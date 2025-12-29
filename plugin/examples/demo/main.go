package main

import (
	"fmt"
	"time"

	"quantmesh/config"
	"quantmesh/plugin"
	"quantmesh/plugin/examples"
)

// 这是一个演示如何使用插件系统的完整示例

func main() {
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("QuantMesh 插件系统演示")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	// 1. 创建配置
	cfg := &config.Config{}

	// 2. 创建插件加载器
	loader := plugin.NewPluginLoader(cfg)
	fmt.Println("✅ 插件加载器已创建")

	// 3. 演示免费插件
	fmt.Println("\n📦 演示1: 加载免费插件")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	demoFreePlugin(loader)

	// 4. 演示商业插件
	fmt.Println("\n🔐 演示2: 加载商业插件")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	demoCommercialPlugin(loader)

	// 5. 演示许可证生成和验证
	fmt.Println("\n🔑 演示3: 许可证生成和验证")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	demoLicenseSystem()

	// 6. 列出所有插件
	fmt.Println("\n📋 演示4: 列出所有已加载的插件")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	listPlugins(loader)

	// 7. 清理
	fmt.Println("\n🧹 清理资源...")
	loader.GetRegistry().CloseAll()
	fmt.Println("✅ 演示完成!")
}

// demoFreePlugin 演示免费插件
func demoFreePlugin(loader *plugin.PluginLoader) {
	// 创建免费插件实例
	freePlugin := examples.NewExampleStrategyPlugin()

	fmt.Printf("插件名称: %s\n", freePlugin.GetMetadata().Name)
	fmt.Printf("版本:     %s\n", freePlugin.GetMetadata().Version)
	fmt.Printf("作者:     %s\n", freePlugin.GetMetadata().Author)
	fmt.Printf("许可证:   %s\n", freePlugin.GetMetadata().License)
	fmt.Printf("需要密钥: %v\n", freePlugin.GetMetadata().RequiresKey)

	// 加载插件
	err := loader.LoadPlugin(
		freePlugin,
		"", // 免费插件不需要许可证
		map[string]interface{}{
			"param1": "value1",
			"param2": 123,
		},
	)

	if err != nil {
		fmt.Printf("❌ 加载失败: %v\n", err)
	} else {
		fmt.Println("✅ 加载成功!")
	}
}

// demoCommercialPlugin 演示商业插件
func demoCommercialPlugin(loader *plugin.PluginLoader) {
	// 创建商业插件实例
	commercialPlugin := examples.NewPremiumAIStrategyPlugin()

	fmt.Printf("插件名称: %s\n", commercialPlugin.GetMetadata().Name)
	fmt.Printf("版本:     %s\n", commercialPlugin.GetMetadata().Version)
	fmt.Printf("许可证:   %s\n", commercialPlugin.GetMetadata().License)
	fmt.Printf("需要密钥: %v\n", commercialPlugin.GetMetadata().RequiresKey)

	// 生成测试许可证
	licenseKey, err := plugin.GenerateLicense(
		"premium_ai_strategy",
		"DEMO001",
		time.Now().AddDate(0, 0, 30), // 30天有效期
		1,
		[]string{"ai", "optimization"},
		"",
		"quantmesh-secret-key-2025",
	)

	if err != nil {
		fmt.Printf("❌ 生成许可证失败: %v\n", err)
		return
	}

	fmt.Println("\n生成的许可证密钥:")
	fmt.Println(licenseKey[:80] + "...")

	// 尝试加载 (会失败，因为这只是演示)
	err = loader.LoadPlugin(
		commercialPlugin,
		licenseKey,
		map[string]interface{}{
			"ai_model": "gpt-4",
		},
	)

	if err != nil {
		fmt.Printf("\n⚠️  预期的失败: %v\n", err)
		fmt.Println("(这是正常的，因为商业插件的实际代码未实现)")
	} else {
		fmt.Println("✅ 加载成功!")
	}
}

// demoLicenseSystem 演示许可证系统
func demoLicenseSystem() {
	// 1. 生成许可证
	fmt.Println("步骤1: 生成许可证")
	licenseKey, err := plugin.GenerateLicense(
		"test_plugin",
		"CUST001",
		time.Now().AddDate(1, 0, 0), // 1年有效期
		5,                            // 最多5个实例
		[]string{"feature1", "feature2", "feature3"},
		"",
		"quantmesh-secret-key-2025",
	)

	if err != nil {
		fmt.Printf("❌ 生成失败: %v\n", err)
		return
	}

	fmt.Printf("✅ 许可证已生成 (长度: %d 字符)\n", len(licenseKey))
	fmt.Printf("前80个字符: %s...\n", licenseKey[:80])

	// 2. 解析许可证
	fmt.Println("\n步骤2: 解析许可证")
	info, err := plugin.ParseLicense(licenseKey)
	if err != nil {
		fmt.Printf("❌ 解析失败: %v\n", err)
		return
	}

	fmt.Printf("✅ 许可证解析成功:\n")
	fmt.Printf("   插件名称: %s\n", info.PluginName)
	fmt.Printf("   客户ID:   %s\n", info.CustomerID)
	fmt.Printf("   签发时间: %s\n", info.IssuedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("   过期时间: %s\n", info.ExpiryDate.Format("2006-01-02 15:04:05"))
	fmt.Printf("   最大实例: %d\n", info.MaxInstances)
	fmt.Printf("   授权功能: %v\n", info.Features)

	// 3. 验证许可证
	fmt.Println("\n步骤3: 验证许可证")
	validator := plugin.NewLicenseValidator()
	err = validator.ValidatePlugin("test_plugin", licenseKey)
	if err != nil {
		fmt.Printf("❌ 验证失败: %v\n", err)
		return
	}

	fmt.Println("✅ 许可证验证通过!")

	// 4. 检查功能授权
	fmt.Println("\n步骤4: 检查功能授权")
	features := []string{"feature1", "feature2", "feature3", "feature4"}
	for _, feature := range features {
		authorized := validator.CheckFeature("test_plugin", feature)
		if authorized {
			fmt.Printf("✅ 功能 '%s' 已授权\n", feature)
		} else {
			fmt.Printf("❌ 功能 '%s' 未授权\n", feature)
		}
	}

	// 5. 测试过期许可证
	fmt.Println("\n步骤5: 测试过期许可证")
	expiredLicense, _ := plugin.GenerateLicense(
		"test_plugin",
		"CUST002",
		time.Now().AddDate(0, 0, -1), // 昨天过期
		1,
		[]string{"*"},
		"",
		"quantmesh-secret-key-2025",
	)

	err = validator.ValidatePlugin("test_plugin", expiredLicense)
	if err != nil {
		fmt.Printf("✅ 正确检测到过期许可证: %v\n", err)
	} else {
		fmt.Println("❌ 未能检测到过期许可证")
	}
}

// listPlugins 列出所有插件
func listPlugins(loader *plugin.PluginLoader) {
	registry := loader.GetRegistry()
	plugins := registry.List()

	if len(plugins) == 0 {
		fmt.Println("未加载任何插件")
		return
	}

	fmt.Printf("已加载 %d 个插件:\n\n", len(plugins))

	for i, meta := range plugins {
		fmt.Printf("%d. %s\n", i+1, meta.Name)
		fmt.Printf("   版本:     %s\n", meta.Version)
		fmt.Printf("   作者:     %s\n", meta.Author)
		fmt.Printf("   类型:     %s\n", meta.Type)
		fmt.Printf("   许可证:   %s\n", meta.License)
		fmt.Printf("   描述:     %s\n", meta.Description)
		fmt.Println()
	}
}

