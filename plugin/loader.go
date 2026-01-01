package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"plugin"
	"sync"
	
	"quantmesh/logger"
)

// PluginLoader 插件加载器
type PluginLoader struct {
	validator *LicenseValidator
	plugins   map[string]*LoadedPlugin
	mu        sync.RWMutex
}

// LoadedPlugin 已加载的插件
type LoadedPlugin struct {
	Name      string
	Version   string
	Plugin    interface{}
	LicenseKey string
	Path      string
}

// NewPluginLoader 创建插件加载器
func NewPluginLoader() *PluginLoader {
	return &PluginLoader{
		validator: NewLicenseValidator(),
		plugins:   make(map[string]*LoadedPlugin),
	}
}

// LoadPlugin 加载插件
func (l *PluginLoader) LoadPlugin(pluginName, pluginPath, licenseKey string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	
	// 1. 验证 License
	if licenseKey != "" {
		if err := l.validator.ValidatePlugin(pluginName, licenseKey); err != nil {
			return fmt.Errorf("License 验证失败: %v", err)
		}
		logger.Info("✅ 插件 %s License 验证通过", pluginName)
	} else {
		logger.Warn("⚠️ 插件 %s 未提供 License Key,跳过验证", pluginName)
	}
	
	// 2. 检查插件文件是否存在
	if _, err := os.Stat(pluginPath); os.IsNotExist(err) {
		return fmt.Errorf("插件文件不存在: %s", pluginPath)
	}
	
	// 3. 加载插件 .so 文件
	p, err := plugin.Open(pluginPath)
	if err != nil {
		return fmt.Errorf("加载插件失败: %v", err)
	}
	
	// 4. 查找插件入口函数
	symbol, err := p.Lookup("NewPlugin")
	if err != nil {
		return fmt.Errorf("插件入口函数 NewPlugin 不存在: %v", err)
	}
	
	// 5. 调用入口函数创建插件实例
	newPluginFunc, ok := symbol.(func() interface{})
	if !ok {
		return fmt.Errorf("NewPlugin 函数签名不正确")
	}
	
	pluginInstance := newPluginFunc()
	
	// 6. 获取插件信息
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
	
	// 7. 保存已加载的插件
	l.plugins[pluginName] = &LoadedPlugin{
		Name:       name,
		Version:    version,
		Plugin:     pluginInstance,
		LicenseKey: licenseKey,
		Path:       pluginPath,
	}
	
	logger.Info("✅ 插件加载成功: %s (版本: %s)", name, version)
	return nil
}

// LoadPluginsFromDirectory 从目录加载所有插件（递归查找子目录）
func (l *PluginLoader) LoadPluginsFromDirectory(dir string, licenses map[string]string) error {
	// 检查目录是否存在
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		logger.Warn("插件目录不存在: %s", dir)
		return nil
	}
	
	// 遍历目录
	files, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("读取插件目录失败: %v", err)
	}
	
	loadedCount := 0
	for _, file := range files {
		// 如果是子目录，递归查找
		if file.IsDir() {
			subDir := filepath.Join(dir, file.Name())
			if err := l.LoadPluginsFromDirectory(subDir, licenses); err != nil {
				logger.Warn("⚠️ 递归加载子目录 %s 失败: %v", subDir, err)
			}
			continue
		}
		
		// 只加载 .so 文件 (Linux/macOS)
		if filepath.Ext(file.Name()) != ".so" {
			continue
		}
		
		pluginName := file.Name()[:len(file.Name())-3] // 去掉 .so 后缀
		pluginPath := filepath.Join(dir, file.Name())
		licenseKey := licenses[pluginName]
		
		if err := l.LoadPlugin(pluginName, pluginPath, licenseKey); err != nil {
			logger.Error("❌ 加载插件 %s 失败: %v", pluginName, err)
			continue
		}
		
		loadedCount++
	}
	
	if loadedCount > 0 {
		logger.Info("📦 从目录 %s 加载了 %d 个插件", dir, loadedCount)
	}
	return nil
}

// GetPlugin 获取已加载的插件
func (l *PluginLoader) GetPlugin(pluginName string) (*LoadedPlugin, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	
	p, exists := l.plugins[pluginName]
	if !exists {
		return nil, fmt.Errorf("插件未加载: %s", pluginName)
	}
	
	return p, nil
}

// ListPlugins 列出所有已加载的插件
func (l *PluginLoader) ListPlugins() []*LoadedPlugin {
	l.mu.RLock()
	defer l.mu.RUnlock()
	
	plugins := make([]*LoadedPlugin, 0, len(l.plugins))
	for _, p := range l.plugins {
		plugins = append(plugins, p)
	}
	
	return plugins
}

// UnloadPlugin 卸载插件
func (l *PluginLoader) UnloadPlugin(pluginName string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	
	p, exists := l.plugins[pluginName]
	if !exists {
		return fmt.Errorf("插件未加载: %s", pluginName)
	}
	
	// 如果插件实现了 Close 方法,调用它
	if closer, ok := p.Plugin.(interface{ Close() error }); ok {
		if err := closer.Close(); err != nil {
			logger.Warn("⚠️ 关闭插件 %s 时出错: %v", pluginName, err)
		}
	}
	
	delete(l.plugins, pluginName)
	logger.Info("✅ 插件已卸载: %s", pluginName)
	
	return nil
}

// UnloadAll 卸载所有插件
func (l *PluginLoader) UnloadAll() {
	l.mu.Lock()
	defer l.mu.Unlock()
	
	for name, p := range l.plugins {
		if closer, ok := p.Plugin.(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil {
				logger.Warn("⚠️ 关闭插件 %s 时出错: %v", name, err)
			}
		}
	}
	
	l.plugins = make(map[string]*LoadedPlugin)
	logger.Info("✅ 所有插件已卸载")
}

// InitializePlugin 初始化插件
func (l *PluginLoader) InitializePlugin(pluginName string, config map[string]interface{}) error {
	p, err := l.GetPlugin(pluginName)
	if err != nil {
		return err
	}
	
	// 如果插件实现了 Initialize 方法,调用它
	if initializer, ok := p.Plugin.(interface{ Initialize(map[string]interface{}) error }); ok {
		if err := initializer.Initialize(config); err != nil {
			return fmt.Errorf("初始化插件 %s 失败: %v", pluginName, err)
		}
		logger.Info("✅ 插件 %s 初始化成功", pluginName)
	}
	
	return nil
}

// CallPluginMethod 调用插件方法 (通用接口)
func (l *PluginLoader) CallPluginMethod(pluginName, methodName string, args ...interface{}) (interface{}, error) {
	_, err := l.GetPlugin(pluginName)
	if err != nil {
		return nil, err
	}
	
	// 这里需要使用反射来调用方法
	// 为了简化,我们提供一些常用的方法调用接口
	
	return nil, fmt.Errorf("通用方法调用暂未实现,请使用具体的插件接口")
}

