//go:build ignore

package main

import (
	"fmt"
	"log"

	"quantmesh/logger"
	"quantmesh/plugin"
)

func main() {
	fmt.Println("🧪 QuantMesh 插件加載测試")
	fmt.Println(string(make([]rune, 50)) + "=")
	fmt.Println()

	// 初始化日志
	logger.SetLevel(logger.INFO)

	// 1. 創建插件加載器
	loader := plugin.NewPluginLoader()
	logger.Info("✅ 插件加載器已創建")

	// 2. 定义插件目錄和 License
	pluginDir := "../quantmesh-premium/plugins"
	licenses := map[string]string{
		"ai_strategy":    "", // 测試時可以為空
		"multi_strategy": "",
		"advanced_risk":  "",
	}

	// 3. 批量加載插件
	logger.Info("📂 從目錄加載插件: %s", pluginDir)
	err := loader.LoadPluginsFromDirectory(pluginDir, licenses)
	if err != nil {
		log.Fatalf("❌ 加載插件失败: %v", err)
	}

	// 4. 列出已加載的插件
	loadedPlugins := loader.ListPlugins()
	logger.Info("📦 成功加載 %d 個插件:", len(loadedPlugins))
	for i, p := range loadedPlugins {
		logger.Info("  %d. %s (版本: %s)", i+1, p.Name, p.Version)
		logger.Info("     路径: %s", p.Path)
	}

	// 5. 测試初始化每個插件
	fmt.Println()
	logger.Info("🔧 测試插件初始化...")

	configs := map[string]map[string]interface{}{
		"ai_strategy": {
			"gemini_api_key":    "test_gemini_key",
			"openai_api_key":    "test_openai_key",
			"analysis_interval": 300,
		},
		"multi_strategy": {
			"default_strategy": "grid",
			"enable_momentum":  false,
		},
		"advanced_risk": {
			"enable_ml_risk_model": true,
			"risk_threshold":       0.7,
		},
	}

	for _, p := range loadedPlugins {
		cfg, exists := configs[p.Name]
		if !exists {
			cfg = make(map[string]interface{})
		}

		err := loader.InitializePlugin(p.Name, cfg)
		if err != nil {
			logger.Warn("⚠️ 初始化插件 %s 失败: %v", p.Name, err)
		} else {
			logger.Info("✅ 插件 %s 初始化成功", p.Name)
		}
	}

	// 6. 测試獲取插件
	fmt.Println()
	logger.Info("🔍 测試獲取插件實例...")
	for _, p := range loadedPlugins {
		plugin, err := loader.GetPlugin(p.Name)
		if err != nil {
			logger.Error("❌ 獲取插件 %s 失败: %v", p.Name, err)
		} else {
			logger.Info("✅ 成功獲取插件: %s", plugin.Name)
		}
	}

	// 7. 清理
	fmt.Println()
	logger.Info("🧹 卸載所有插件...")
	loader.UnloadAll()

	fmt.Println()
	fmt.Println("🎉 插件加載测試完成!")
}
