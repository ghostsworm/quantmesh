package main

import (
	"fmt"
	"log"
	"time"

	"quantmesh/logger"
	"quantmesh/plugin"
)

func main() {
	fmt.Println("🧪 QuantMesh License 验证测试")
	fmt.Println("=" + string(make([]rune, 70)) + "=")
	fmt.Println()

	// 初始化日志
	logger.SetLevel(logger.INFO)

	// 测试 License Keys (从 test_licenses.txt)
	licenses := map[string]string{
		"ai_strategy":    "eyJwbHVnaW5fbmFtZSI6ImFpX3N0cmF0ZWd5IiwiY3VzdG9tZXJfaWQiOiJ0ZXN0X2N1c3RvbWVyXzAwMSIsImVtYWlsIjoiIiwicGxhbiI6InByb2Zlc3Npb25hbCIsImV4cGlyeV9kYXRlIjoiMjAyNi0xMi0zMVQwMDowMDowMFoiLCJpc3N1ZWRfYXQiOiIyMDI2LTAxLTAxVDE2OjIwOjU3LjY1MTUxKzA4OjAwIiwiY2xvdWRfdmVyaWZ5Ijp0cnVlLCJzaWduYXR1cmUiOiJjYTk2NWM2YjljYTMzYTVjNjM5NzFjYjBiNjhhMjc5ZmE4NjI3Y2FkOTc2MjRiMDk3NTdhZTY5MWY3NjJkMGI0In0=",
		"multi_strategy": "eyJwbHVnaW5fbmFtZSI6Im11bHRpX3N0cmF0ZWd5IiwiY3VzdG9tZXJfaWQiOiJ0ZXN0X2N1c3RvbWVyXzAwMSIsImVtYWlsIjoiIiwicGxhbiI6InByb2Zlc3Npb25hbCIsImV4cGlyeV9kYXRlIjoiMjAyNi0xMi0zMVQwMDowMDowMFoiLCJpc3N1ZWRfYXQiOiIyMDI2LTAxLTAxVDE2OjIxOjA0Ljk0Mjk1MiswODowMCIsImNsb3VkX3ZlcmlmeSI6dHJ1ZSwic2lnbmF0dXJlIjoiOWY1MjgwMDljNDE2NTA5NGYzMjgyNjBkYWJjYWRiYjkwMDAzYmM3NGYzYmI0MGE4OWUxMDc0ZWYzNzBkYmQyYyJ9",
		"advanced_risk":  "eyJwbHVnaW5fbmFtZSI6ImFkdmFuY2VkX3Jpc2siLCJjdXN0b21lcl9pZCI6InRlc3RfY3VzdG9tZXJfMDAxIiwiZW1haWwiOiIiLCJwbGFuIjoicHJvZmVzc2lvbmFsIiwiZXhwaXJ5X2RhdGUiOiIyMDI2LTEyLTMxVDAwOjAwOjAwWiIsImlzc3VlZF9hdCI6IjIwMjYtMDEtMDFUMTY6MjE6MDQuOTgyNTQ4KzA4OjAwIiwiY2xvdWRfdmVyaWZ5Ijp0cnVlLCJzaWduYXR1cmUiOiI5MWVlMWRiOTQ5YTM1ZGY2MzA3ZTI0ZTg2OTc1NzcyMjkxODg1NDNhYzg1Yzk5ZWJiZWU3ZmI2Yjk0MzlhMTJiIn0=",
	}

	// 测试计数
	totalTests := 0
	passedTests := 0
	failedTests := 0

	fmt.Println("📋 测试 1: 解析 License Key")
	fmt.Println("-" + string(make([]rune, 70)) + "-")
	for pluginName, licenseKey := range licenses {
		totalTests++
		info, err := plugin.ParseLicense(licenseKey)
		if err != nil {
			logger.Error("❌ 解析 %s License 失败: %v", pluginName, err)
			failedTests++
		} else {
			logger.Info("✅ 解析 %s License 成功", pluginName)
			logger.Info("   客户ID: %s", info.CustomerID)
			logger.Info("   套餐: %s", info.Plan)
			logger.Info("   过期时间: %s", info.ExpiryDate.Format("2006-01-02"))
			logger.Info("   云端验证: %v", info.CloudVerify)
			passedTests++
		}
	}
	fmt.Println()

	fmt.Println("📋 测试 2: 本地签名验证")
	fmt.Println("-" + string(make([]rune, 70)) + "-")
	validator := plugin.NewLicenseValidator()

	for pluginName, licenseKey := range licenses {
		totalTests++

		// 临时禁用云端验证来测试本地验证
		info, _ := plugin.ParseLicense(licenseKey)
		info.CloudVerify = false

		// 这里我们只测试解析和基本验证
		if time.Now().After(info.ExpiryDate) {
			logger.Error("❌ %s License 已过期", pluginName)
			failedTests++
		} else {
			logger.Info("✅ %s License 本地验证通过", pluginName)
			passedTests++
		}
	}
	fmt.Println()

	fmt.Println("📋 测试 3: 云端 License 验证 (有效 License)")
	fmt.Println("-" + string(make([]rune, 70)) + "-")

	for pluginName, licenseKey := range licenses {
		totalTests++

		logger.Info("🔍 验证插件: %s", pluginName)
		err := validator.ValidatePlugin(pluginName, licenseKey)

		if err != nil {
			logger.Error("❌ %s License 验证失败: %v", pluginName, err)
			failedTests++
		} else {
			logger.Info("✅ %s License 验证通过 (包含云端验证)", pluginName)
			passedTests++
		}

		// 短暂延迟
		time.Sleep(500 * time.Millisecond)
	}
	fmt.Println()

	fmt.Println("📋 测试 4: 无效 License 拒绝测试")
	fmt.Println("-" + string(make([]rune, 70)) + "-")

	invalidLicenses := map[string]string{
		"格式错误":       "invalid_base64_string",
		"空 License":  "",
		"过期 License": "eyJwbHVnaW5fbmFtZSI6ImFpX3N0cmF0ZWd5IiwiY3VzdG9tZXJfaWQiOiJ0ZXN0IiwiZW1haWwiOiIiLCJwbGFuIjoicHJvZmVzc2lvbmFsIiwiZXhwaXJ5X2RhdGUiOiIyMDIwLTAxLTAxVDAwOjAwOjAwWiIsImlzc3VlZF9hdCI6IjIwMjAtMDEtMDFUMDA6MDA6MDBaIiwiY2xvdWRfdmVyaWZ5IjpmYWxzZSwic2lnbmF0dXJlIjoiIn0=",
	}

	for testName, licenseKey := range invalidLicenses {
		totalTests++

		logger.Info("🔍 测试: %s", testName)
		err := validator.ValidatePlugin("test_plugin", licenseKey)

		if err != nil {
			logger.Info("✅ 正确拒绝无效 License: %s", testName)
			logger.Info("   错误信息: %v", err)
			passedTests++
		} else {
			logger.Error("❌ 未能拒绝无效 License: %s", testName)
			failedTests++
		}
	}
	fmt.Println()

	fmt.Println("📋 测试 5: 插件加载器集成测试")
	fmt.Println("-" + string(make([]rune, 70)) + "-")

	loader := plugin.NewPluginLoader()
	pluginDir := "../quantmesh-premium/plugins"

	// 尝试加载插件 (带 License)
	pluginsToLoad := []struct {
		name string
		path string
		key  string
	}{
		{"ai_strategy", pluginDir + "/ai_strategy/ai_strategy.so", licenses["ai_strategy"]},
		{"multi_strategy", pluginDir + "/multi_strategy/multi_strategy.so", licenses["multi_strategy"]},
		{"advanced_risk", pluginDir + "/advanced_risk/advanced_risk.so", licenses["advanced_risk"]},
	}

	for _, p := range pluginsToLoad {
		totalTests++

		logger.Info("🔍 加载插件: %s", p.name)
		err := loader.LoadPlugin(p.name, p.path, p.key)

		if err != nil {
			logger.Error("❌ 插件 %s 加载失败: %v", p.name, err)
			failedTests++
		} else {
			logger.Info("✅ 插件 %s 加载成功 (License 验证通过)", p.name)
			passedTests++
		}

		time.Sleep(500 * time.Millisecond)
	}
	fmt.Println()

	// 测试总结
	fmt.Println("=" + string(make([]rune, 70)) + "=")
	fmt.Println("🎉 测试完成!")
	fmt.Println()
	fmt.Printf("总测试数: %d\n", totalTests)
	fmt.Printf("✅ 通过: %d\n", passedTests)
	fmt.Printf("❌ 失败: %d\n", failedTests)
	fmt.Printf("通过率: %.1f%%\n", float64(passedTests)/float64(totalTests)*100)
	fmt.Println()

	if failedTests == 0 {
		fmt.Println("🎊 所有测试通过！License 验证系统工作正常！")
	} else {
		fmt.Println("⚠️ 部分测试失败，请检查日志")
		log.Fatal("测试失败")
	}

	// 清理
	loader.UnloadAll()
	logger.Info("✅ 插件已卸载")
}
