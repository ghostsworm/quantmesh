package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"plugin"
	"runtime"
	"strings"
	"sync"

	"quantmesh/logger"
)

// PluginLoader 插件加載器
type PluginLoader struct {
	validator *LicenseValidator
	plugins   map[string]*LoadedPlugin
	mu        sync.RWMutex
}

// LoadedPlugin 已加載的插件
type LoadedPlugin struct {
	Name       string
	Version    string
	Plugin     interface{}
	LicenseKey string
	Path       string
}

// NewPluginLoader 創建插件加載器
func NewPluginLoader() *PluginLoader {
	return &PluginLoader{
		validator: NewLicenseValidator(),
		plugins:   make(map[string]*LoadedPlugin),
	}
}

// LoadPlugin 加載插件
func (l *PluginLoader) LoadPlugin(pluginName, pluginPath, licenseKey string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// 1. 驗证 License
	if licenseKey != "" {
		if err := l.validator.ValidatePlugin(pluginName, licenseKey); err != nil {
			return fmt.Errorf("License 驗证失败: %v", err)
		}
		logger.Info("✅ 插件 %s License 驗证通過", pluginName)
	} else {
		logger.Warn("⚠️ 插件 %s 未提供 License Key,跳過驗证", pluginName)
	}

	// 2. 检查插件文件是否存在
	if _, err := os.Stat(pluginPath); os.IsNotExist(err) {
		return fmt.Errorf("插件文件不存在: %s", pluginPath)
	}

	// 3. 加載插件 .so 文件（带版本检测）
	p, err := plugin.Open(pluginPath)
	if err != nil {
		// 检测是否是版本不匹配錯误
		errMsg := err.Error()
		if strings.Contains(errMsg, "different version") || strings.Contains(errMsg, "incompatible") {
			currentGoVersion := runtime.Version()
			return fmt.Errorf(
				"插件版本不匹配: %v\n"+
					"⚠️  原因: 插件和主程序使用了不同的 Go 版本或依赖版本\n"+
					"📌 當前 Go 版本: %s\n"+
					"💡 解决方案:\n"+
					"   1. 使用預编譯版本（推荐）- 無需担心版本问题\n"+
					"   2. 确保使用相同的 Go 版本重新编譯插件和主程序\n"+
					"   3. 检查依赖包版本是否一致",
				err, currentGoVersion)
		}
		return fmt.Errorf("加載插件失败: %v", err)
	}

	// 4. 查找插件入口函數
	symbol, err := p.Lookup("NewPlugin")
	if err != nil {
		return fmt.Errorf("插件入口函數 NewPlugin 不存在: %v", err)
	}

	// 5. 調用入口函數創建插件實例
	newPluginFunc, ok := symbol.(func() interface{})
	if !ok {
		return fmt.Errorf("NewPlugin 函數签名不正确")
	}

	pluginInstance := newPluginFunc()

	// 6. 獲取插件信息
	var name, version string
	if nameGetter, ok := pluginInstance.(interface{ Name() string }); ok {
		name = nameGetter.Name()
	} else {
		name = pluginName
	}

	if versionGetter, ok := pluginInstance.(interface{ Version() string }); ok {
		version = versionGetter.Version()
	} else {
		version = "unknown"
	}

	// 7. 保存已加載的插件
	l.plugins[pluginName] = &LoadedPlugin{
		Name:       name,
		Version:    version,
		Plugin:     pluginInstance,
		LicenseKey: licenseKey,
		Path:       pluginPath,
	}

	logger.Info("✅ 插件加載成功: %s (版本: %s)", name, version)
	return nil
}

// LoadPluginsFromDirectory 從目錄加載所有插件（遞归查找子目錄）
func (l *PluginLoader) LoadPluginsFromDirectory(dir string, licenses map[string]string) error {
	// 检查目錄是否存在
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		logger.Warn("插件目錄不存在: %s", dir)
		return nil
	}

	// 遍历目錄
	files, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("读取插件目錄失败: %v", err)
	}

	loadedCount := 0
	for _, file := range files {
		// 如果是子目錄，遞归查找
		if file.IsDir() {
			subDir := filepath.Join(dir, file.Name())
			if err := l.LoadPluginsFromDirectory(subDir, licenses); err != nil {
				logger.Warn("⚠️ 遞归加載子目錄 %s 失败: %v", subDir, err)
			}
			continue
		}

		// 只加載 .so 文件 (Linux/macOS)
		if filepath.Ext(file.Name()) != ".so" {
			continue
		}

		pluginName := file.Name()[:len(file.Name())-3] // 去掉 .so 后缀
		pluginPath := filepath.Join(dir, file.Name())
		licenseKey := licenses[pluginName]

		if err := l.LoadPlugin(pluginName, pluginPath, licenseKey); err != nil {
			logger.Error("❌ 加載插件 %s 失败: %v", pluginName, err)
			continue
		}

		loadedCount++
	}

	if loadedCount > 0 {
		logger.Info("📦 從目錄 %s 加載了 %d 個插件", dir, loadedCount)
	}
	return nil
}

// GetPlugin 獲取已加載的插件
func (l *PluginLoader) GetPlugin(pluginName string) (*LoadedPlugin, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	p, exists := l.plugins[pluginName]
	if !exists {
		return nil, fmt.Errorf("插件未加載: %s", pluginName)
	}

	return p, nil
}

// ListPlugins 列出所有已加載的插件
func (l *PluginLoader) ListPlugins() []*LoadedPlugin {
	l.mu.RLock()
	defer l.mu.RUnlock()

	plugins := make([]*LoadedPlugin, 0, len(l.plugins))
	for _, p := range l.plugins {
		plugins = append(plugins, p)
	}

	return plugins
}

// UnloadPlugin 卸載插件
func (l *PluginLoader) UnloadPlugin(pluginName string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	p, exists := l.plugins[pluginName]
	if !exists {
		return fmt.Errorf("插件未加載: %s", pluginName)
	}

	// 如果插件實現了 Close 方法,調用它
	if closer, ok := p.Plugin.(interface{ Close() error }); ok {
		if err := closer.Close(); err != nil {
			logger.Warn("⚠️ 关闭插件 %s 時出錯: %v", pluginName, err)
		}
	}

	delete(l.plugins, pluginName)
	logger.Info("✅ 插件已卸載: %s", pluginName)

	return nil
}

// UnloadAll 卸載所有插件
func (l *PluginLoader) UnloadAll() {
	l.mu.Lock()
	defer l.mu.Unlock()

	for name, p := range l.plugins {
		if closer, ok := p.Plugin.(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil {
				logger.Warn("⚠️ 关闭插件 %s 時出錯: %v", name, err)
			}
		}
	}

	l.plugins = make(map[string]*LoadedPlugin)
	logger.Info("✅ 所有插件已卸載")
}

// InitializePlugin 初始化插件
func (l *PluginLoader) InitializePlugin(pluginName string, config map[string]interface{}) error {
	p, err := l.GetPlugin(pluginName)
	if err != nil {
		return err
	}

	// 如果插件實現了 Initialize 方法,調用它
	if initializer, ok := p.Plugin.(interface {
		Initialize(map[string]interface{}) error
	}); ok {
		if err := initializer.Initialize(config); err != nil {
			return fmt.Errorf("初始化插件 %s 失败: %v", pluginName, err)
		}
		logger.Info("✅ 插件 %s 初始化成功", pluginName)
	}

	return nil
}

// CallPluginMethod 調用插件方法 (通用接口)
func (l *PluginLoader) CallPluginMethod(pluginName, methodName string, args ...interface{}) (interface{}, error) {
	_, err := l.GetPlugin(pluginName)
	if err != nil {
		return nil, err
	}

	// 这里需要使用反射来調用方法
	// 為了简化,我们提供一些常用的方法調用接口

	return nil, fmt.Errorf("通用方法調用暂未實現,请使用具体的插件接口")
}
