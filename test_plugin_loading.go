package main

import (
	"fmt"
	"log"

	"quantmesh/logger"
	"quantmesh/plugin"
)

func main() {
	fmt.Println("🧪 QuantMesh 插件加载测试")
	fmt.Println(string(make([]rune, 50)) + "=")
	fmt.Println()

	// 初始化日志
	logger.SetLevel(logger.INFO)

	// 1. 创建插件加载器
	loader := plugin.NewPluginLoader()
	logger.Info("✅ 插件加载器已创建")

	// 2. 定义插件目录和 License
	pluginDir := "../quantmesh-premium/plugins"
	licenses := map[string]string{
		"ai_strategy":    "", // 测试时可以为空
		"multi_strategy": "",
		"advanced_risk":  "",
	}

	// 3. 批量加载插件
	logger.Info("📂 从目录加载插件: %s", pluginDir)
	err := loader.LoadPluginsFromDirectory(pluginDir, licenses)
	if err != nil {
		log.Fatalf("❌ 加载插件失败: %v", err)
	}

	// 4. 列出已加载的插件
	loadedPlugins := loader.ListPlugins()
	logger.Info("📦 成功加载 %d 个插件:", len(loadedPlugins))
	for i, p := range loadedPlugins {
		logger.Info("  %d. %s (版本: %s)", i+1, p.Name, p.Version)
		logger.Info("     路径: %s", p.Path)
	}

	// 5. 测试初始化每个插件
	fmt.Println()
	logger.Info("🔧 测试插件初始化...")

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

	// 6. 测试获取插件
	fmt.Println()
	logger.Info("🔍 测试获取插件实例...")
	for _, p := range loadedPlugins {
		plugin, err := loader.GetPlugin(p.Name)
		if err != nil {
			logger.Error("❌ 获取插件 %s 失败: %v", p.Name, err)
		} else {
			logger.Info("✅ 成功获取插件: %s", plugin.Name)
		}
	}

	// 7. 清理
	fmt.Println()
	logger.Info("🧹 卸载所有插件...")
	loader.UnloadAll()

	fmt.Println()
	fmt.Println("🎉 插件加载测试完成!")
}
