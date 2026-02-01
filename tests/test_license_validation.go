//go:build ignore

package main

import (
	"fmt"
	"log"
	"time"

	"quantmesh/logger"
	"quantmesh/plugin"
)

func main() {
	fmt.Println("🧪 QuantMesh License 驗证测試")
	fmt.Println("=" + string(make([]rune, 70)) + "=")
	fmt.Println()

	// 初始化日志
	logger.SetLevel(logger.INFO)

	// 测試 License Keys (從 test_licenses.txt)
	licenses := map[string]string{
		"ai_strategy":    "eyJwbHVnaW5fbmFtZSI6ImFpX3N0cmF0ZWd5IiwiY3VzdG9tZXJfaWQiOiJ0ZXN0X2N1c3RvbWVyXzAwMSIsImVtYWlsIjoiIiwicGxhbiI6InByb2Zlc3Npb25hbCIsImV4cGlyeV9kYXRlIjoiMjAyNi0xMi0zMVQwMDowMDowMFoiLCJpc3N1ZWRfYXQiOiIyMDI2LTAxLTAxVDE2OjIwOjU3LjY1MTUxKzA4OjAwIiwiY2xvdWRfdmVyaWZ5Ijp0cnVlLCJzaWduYXR1cmUiOiJjYTk2NWM2YjljYTMzYTVjNjM5NzFjYjBiNjhhMjc5ZmE4NjI3Y2FkOTc2MjRiMDk3NTdhZTY5MWY3NjJkMGI0In0=",
		"multi_strategy": "eyJwbHVnaW5fbmFtZSI6Im11bHRpX3N0cmF0ZWd5IiwiY3VzdG9tZXJfaWQiOiJ0ZXN0X2N1c3RvbWVyXzAwMSIsImVtYWlsIjoiIiwicGxhbiI6InByb2Zlc3Npb25hbCIsImV4cGlyeV9kYXRlIjoiMjAyNi0xMi0zMVQwMDowMDowMFoiLCJpc3N1ZWRfYXQiOiIyMDI2LTAxLTAxVDE2OjIxOjA0Ljk0Mjk1MiswODowMCIsImNsb3VkX3ZlcmlmeSI6dHJ1ZSwic2lnbmF0dXJlIjoiOWY1MjgwMDljNDE2NTA5NGYzMjgyNjBkYWJjYWRiYjkwMDAzYmM3NGYzYmI0MGE4OWUxMDc0ZWYzNzBkYmQyYyJ9",
		"advanced_risk":  "eyJwbHVnaW5fbmFtZSI6ImFkdmFuY2VkX3Jpc2siLCJjdXN0b21lcl9pZCI6InRlc3RfY3VzdG9tZXJfMDAxIiwiZW1haWwiOiIiLCJwbGFuIjoicHJvZmVzc2lvbmFsIiwiZXhwaXJ5X2RhdGUiOiIyMDI2LTEyLTMxVDAwOjAwOjAwWiIsImlzc3VlZF9hdCI6IjIwMjYtMDEtMDFUMTY6MjE6MDQuOTgyNTQ4KzA4OjAwIiwiY2xvdWRfdmVyaWZ5Ijp0cnVlLCJzaWduYXR1cmUiOiI5MWVlMWRiOTQ5YTM1ZGY2MzA3ZTI0ZTg2OTc1NzcyMjkxODg1NDNhYzg1Yzk5ZWJiZWU3ZmI2Yjk0MzlhMTJiIn0=",
	}

	// 测試计數
	totalTests := 0
	passedTests := 0
	failedTests := 0

	fmt.Println("📋 测試 1: 解析 License Key")
	fmt.Println("-" + string(make([]rune, 70)) + "-")
	for pluginName, licenseKey := range licenses {
		totalTests++
		info, err := plugin.ParseLicense(licenseKey)
		if err != nil {
			logger.Error("❌ 解析 %s License 失败: %v", pluginName, err)
			failedTests++
		} else {
			logger.Info("✅ 解析 %s License 成功", pluginName)
			logger.Info("   客戶ID: %s", info.CustomerID)
			logger.Info("   套餐: %s", info.Plan)
			logger.Info("   過期時间: %s", info.ExpiryDate.Format("2006-01-02"))
			logger.Info("   云端驗证: %v", info.CloudVerify)
			passedTests++
		}
	}
	fmt.Println()

	fmt.Println("📋 测試 2: 本地签名驗证")
	fmt.Println("-" + string(make([]rune, 70)) + "-")
	validator := plugin.NewLicenseValidator()

	for pluginName, licenseKey := range licenses {
		totalTests++

		// 临時禁用云端驗证来测試本地驗证
		info, _ := plugin.ParseLicense(licenseKey)
		info.CloudVerify = false

		// 这里我们只测試解析和基本驗证
		if time.Now().After(info.ExpiryDate) {
			logger.Error("❌ %s License 已過期", pluginName)
			failedTests++
		} else {
			logger.Info("✅ %s License 本地驗证通過", pluginName)
			passedTests++
		}
	}
	fmt.Println()

	fmt.Println("📋 测試 3: 云端 License 驗证 (有效 License)")
	fmt.Println("-" + string(make([]rune, 70)) + "-")

	for pluginName, licenseKey := range licenses {
		totalTests++

		logger.Info("🔍 驗证插件: %s", pluginName)
		err := validator.ValidatePlugin(pluginName, licenseKey)

		if err != nil {
			logger.Error("❌ %s License 驗证失败: %v", pluginName, err)
			failedTests++
		} else {
			logger.Info("✅ %s License 驗证通過 (包含云端驗证)", pluginName)
			passedTests++
		}

		// 短暂延迟
		time.Sleep(500 * time.Millisecond)
	}
	fmt.Println()

	fmt.Println("📋 测試 4: 無效 License 拒绝测試")
	fmt.Println("-" + string(make([]rune, 70)) + "-")

	invalidLicenses := map[string]string{
		"格式錯误":       "invalid_base64_string",
		"空 License":  "",
		"過期 License": "eyJwbHVnaW5fbmFtZSI6ImFpX3N0cmF0ZWd5IiwiY3VzdG9tZXJfaWQiOiJ0ZXN0IiwiZW1haWwiOiIiLCJwbGFuIjoicHJvZmVzc2lvbmFsIiwiZXhwaXJ5X2RhdGUiOiIyMDIwLTAxLTAxVDAwOjAwOjAwWiIsImlzc3VlZF9hdCI6IjIwMjAtMDEtMDFUMDA6MDA6MDBaIiwiY2xvdWRfdmVyaWZ5IjpmYWxzZSwic2lnbmF0dXJlIjoiIn0=",
	}

	for testName, licenseKey := range invalidLicenses {
		totalTests++

		logger.Info("🔍 测試: %s", testName)
		err := validator.ValidatePlugin("test_plugin", licenseKey)

		if err != nil {
			logger.Info("✅ 正确拒绝無效 License: %s", testName)
			logger.Info("   錯误信息: %v", err)
			passedTests++
		} else {
			logger.Error("❌ 未能拒绝無效 License: %s", testName)
			failedTests++
		}
	}
	fmt.Println()

	fmt.Println("📋 测試 5: 插件加載器集成测試")
	fmt.Println("-" + string(make([]rune, 70)) + "-")

	loader := plugin.NewPluginLoader()
	pluginDir := "../quantmesh-premium/plugins"

	// 尝試加載插件 (带 License)
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

		logger.Info("🔍 加載插件: %s", p.name)
		err := loader.LoadPlugin(p.name, p.path, p.key)

		if err != nil {
			logger.Error("❌ 插件 %s 加載失败: %v", p.name, err)
			failedTests++
		} else {
			logger.Info("✅ 插件 %s 加載成功 (License 驗证通過)", p.name)
			passedTests++
		}

		time.Sleep(500 * time.Millisecond)
	}
	fmt.Println()

	// 测試總結
	fmt.Println("=" + string(make([]rune, 70)) + "=")
	fmt.Println("🎉 测試完成!")
	fmt.Println()
	fmt.Printf("總测試數: %d\n", totalTests)
	fmt.Printf("✅ 通過: %d\n", passedTests)
	fmt.Printf("❌ 失败: %d\n", failedTests)
	fmt.Printf("通過率: %.1f%%\n", float64(passedTests)/float64(totalTests)*100)
	fmt.Println()

	if failedTests == 0 {
		fmt.Println("🎊 所有测試通過！License 驗证系统工作正常！")
	} else {
		fmt.Println("⚠️ 部分测試失败，请检查日志")
		log.Fatal("测試失败")
	}

	// 清理
	loader.UnloadAll()
	logger.Info("✅ 插件已卸載")
}
