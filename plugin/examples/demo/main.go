//go:build tools

package main

import (
	"fmt"
	"time"

	"quantmesh/plugin"
	"quantmesh/plugin/examples"
)

// 这是一個演示如何使用插件系统的完整示例

func main() {
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("QuantMesh 插件系统演示")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	// 1. 創建插件加載器
	loader := plugin.NewPluginLoader()
	fmt.Println("✅ 插件加載器已創建")

	// 3. 演示免费插件
	fmt.Println("\n📦 演示1: 加載免费插件")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	demoFreePlugin(loader)

	// 4. 演示商业插件
	fmt.Println("\n🔐 演示2: 加載商业插件")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	demoCommercialPlugin(loader)

	// 5. 演示許可证生成和驗证
	fmt.Println("\n🔑 演示3: 許可证生成和驗证")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	demoLicenseSystem()

	// 6. 列出所有插件
	fmt.Println("\n📋 演示4: 列出所有已加載的插件")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	listPlugins(loader)

	// 7. 清理
	fmt.Println("\n🧹 清理资源...")
	loader.UnloadAll()
	fmt.Println("✅ 演示完成!")
}

// demoFreePlugin 演示免费插件
func demoFreePlugin(loader *plugin.PluginLoader) {
	// 創建免费插件實例
	freePlugin := examples.NewExampleStrategyPlugin()

	fmt.Printf("插件名称: %s\n", freePlugin.GetMetadata().Name)
	fmt.Printf("版本:     %s\n", freePlugin.GetMetadata().Version)
	fmt.Printf("作者:     %s\n", freePlugin.GetMetadata().Author)
	fmt.Printf("許可证:   %s\n", freePlugin.GetMetadata().License)
	fmt.Printf("需要密钥: %v\n", freePlugin.GetMetadata().RequiresKey)

	// 注意：LoadPlugin 需要插件文件路径，这里只是演示插件元數據
	// 實際使用時，需要先编譯插件為 .so 文件，然后使用路径加載
	fmt.Println("\n💡 提示：要實際加載插件，需要：")
	fmt.Println("   1. 將插件编譯為 .so 文件")
	fmt.Println("   2. 使用 loader.LoadPlugin(pluginName, pluginPath, licenseKey) 加載")
	fmt.Println("   3. 插件路径示例: \"./plugins/example_strategy.so\"")
}

// demoCommercialPlugin 演示商业插件
func demoCommercialPlugin(loader *plugin.PluginLoader) {
	// 創建商业插件實例
	commercialPlugin := examples.NewPremiumAIStrategyPlugin()

	fmt.Printf("插件名称: %s\n", commercialPlugin.GetMetadata().Name)
	fmt.Printf("版本:     %s\n", commercialPlugin.GetMetadata().Version)
	fmt.Printf("許可证:   %s\n", commercialPlugin.GetMetadata().License)
	fmt.Printf("需要密钥: %v\n", commercialPlugin.GetMetadata().RequiresKey)

	// 生成测試許可证
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
		fmt.Printf("❌ 生成許可证失败: %v\n", err)
		return
	}

	fmt.Println("\n生成的許可证密钥:")
	fmt.Println(licenseKey[:80] + "...")

	// 注意：LoadPlugin 需要插件文件路径，这里只是演示插件元數據和許可证生成
	// 實際使用時，需要先编譯插件為 .so 文件，然后使用路径加載
	fmt.Println("\n💡 提示：要實際加載商业插件，需要：")
	fmt.Println("   1. 將插件编譯為 .so 文件")
	fmt.Println("   2. 使用 loader.LoadPlugin(pluginName, pluginPath, licenseKey) 加載")
	fmt.Println("   3. 提供有效的許可证密钥")
}

// demoLicenseSystem 演示許可证系统
func demoLicenseSystem() {
	// 1. 生成許可证
	fmt.Println("步骤1: 生成許可证")
	licenseKey, err := plugin.GenerateLicense(
		"test_plugin",
		"CUST001",
		time.Now().AddDate(1, 0, 0), // 1年有效期
		5,                           // 最多5個實例
		[]string{"feature1", "feature2", "feature3"},
		"",
		"quantmesh-secret-key-2025",
	)

	if err != nil {
		fmt.Printf("❌ 生成失败: %v\n", err)
		return
	}

	fmt.Printf("✅ 許可证已生成 (长度: %d 字符)\n", len(licenseKey))
	fmt.Printf("前80個字符: %s...\n", licenseKey[:80])

	// 2. 解析許可证
	fmt.Println("\n步骤2: 解析許可证")
	info, err := plugin.ParseLicense(licenseKey)
	if err != nil {
		fmt.Printf("❌ 解析失败: %v\n", err)
		return
	}

	fmt.Printf("✅ 許可证解析成功:\n")
	fmt.Printf("   插件名称: %s\n", info.PluginName)
	fmt.Printf("   客戶ID:   %s\n", info.CustomerID)
	fmt.Printf("   签发時间: %s\n", info.IssuedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("   過期時间: %s\n", info.ExpiryDate.Format("2006-01-02 15:04:05"))
	fmt.Printf("   最大實例: %d\n", info.MaxInstances)
	fmt.Printf("   授权功能: %v\n", info.Features)

	// 3. 驗证許可证
	fmt.Println("\n步骤3: 驗证許可证")
	validator := plugin.NewLicenseValidator()
	err = validator.ValidatePlugin("test_plugin", licenseKey)
	if err != nil {
		fmt.Printf("❌ 驗证失败: %v\n", err)
		return
	}

	fmt.Println("✅ 許可证驗证通過!")

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

	// 5. 测試過期許可证
	fmt.Println("\n步骤5: 测試過期許可证")
	expiredLicense, _ := plugin.GenerateLicense(
		"test_plugin",
		"CUST002",
		time.Now().AddDate(0, 0, -1), // 昨天過期
		1,
		[]string{"*"},
		"",
		"quantmesh-secret-key-2025",
	)

	err = validator.ValidatePlugin("test_plugin", expiredLicense)
	if err != nil {
		fmt.Printf("✅ 正确检测到過期許可证: %v\n", err)
	} else {
		fmt.Println("❌ 未能检测到過期許可证")
	}
}

// listPlugins 列出所有插件
func listPlugins(loader *plugin.PluginLoader) {
	plugins := loader.ListPlugins()

	if len(plugins) == 0 {
		fmt.Println("未加載任何插件")
		return
	}

	fmt.Printf("已加載 %d 個插件:\n\n", len(plugins))

	for i, p := range plugins {
		fmt.Printf("%d. %s\n", i+1, p.Name)
		fmt.Printf("   版本:     %s\n", p.Version)
		fmt.Printf("   路径:     %s\n", p.Path)
		fmt.Printf("   許可证:   %s\n", func() string {
			if p.LicenseKey != "" {
				return "已提供"
			}
			return "未提供"
		}())
		fmt.Println()
	}
}
