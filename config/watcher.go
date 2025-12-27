package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// ConfigWatcher 配置文件监控器
type ConfigWatcher struct {
	configPath    string
	watcher       *fsnotify.Watcher
	hotReloader   *HotReloader
	backupManager *BackupManager
	mu            sync.RWMutex
	isWatching    bool
	lastModTime   time.Time
	updateChan    chan *Config
	errorChan     chan error
}

// NewConfigWatcher 创建配置监控器
func NewConfigWatcher(configPath string, hotReloader *HotReloader, backupManager *BackupManager) (*ConfigWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("创建文件监控器失败: %v", err)
	}

	// 获取配置文件所在目录
	configDir := filepath.Dir(configPath)
	if configDir == "" || configDir == "." {
		// 使用当前目录
		var err error
		configDir, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("获取当前目录失败: %v", err)
		}
		configPath = filepath.Join(configDir, filepath.Base(configPath))
	}

	// 获取初始修改时间
	var lastModTime time.Time
	if info, err := os.Stat(configPath); err == nil {
		lastModTime = info.ModTime()
	}

	cw := &ConfigWatcher{
		configPath:    configPath,
		watcher:       watcher,
		hotReloader:   hotReloader,
		backupManager: backupManager,
		lastModTime:   lastModTime,
		updateChan:    make(chan *Config, 1),
		errorChan:     make(chan error, 10),
	}

	return cw, nil
}

// Start 开始监控配置文件
func (cw *ConfigWatcher) Start(ctx context.Context) error {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	if cw.isWatching {
		return fmt.Errorf("配置监控器已经在运行")
	}

	// 添加配置文件所在目录到监控
	configDir := filepath.Dir(cw.configPath)
	if err := cw.watcher.Add(configDir); err != nil {
		return fmt.Errorf("添加监控目录失败: %v", err)
	}

	cw.isWatching = true

	// 启动监控协程
	go cw.watchLoop(ctx)

	return nil
}

// Stop 停止监控
func (cw *ConfigWatcher) Stop() error {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	if !cw.isWatching {
		return nil
	}

	cw.isWatching = false
	return cw.watcher.Close()
}

// watchLoop 监控循环
func (cw *ConfigWatcher) watchLoop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second) // 每秒检查一次
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case event, ok := <-cw.watcher.Events:
			if !ok {
				return
			}
			// 检查是否是目标配置文件的变化
			if event.Name == cw.configPath {
				if event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create {
					// 延迟处理，避免文件正在写入时读取
					time.Sleep(100 * time.Millisecond)
					cw.handleConfigChange(ctx)
				}
			}

		case err, ok := <-cw.watcher.Errors:
			if !ok {
				return
			}
			select {
			case cw.errorChan <- err:
			default:
			}

		case <-ticker.C:
			// 定期检查文件修改时间（作为备用机制）
			cw.checkFileModTime(ctx)
		}
	}
}

// handleConfigChange 处理配置文件变化
func (cw *ConfigWatcher) handleConfigChange(ctx context.Context) {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	// 检查文件修改时间，避免重复处理
	info, err := os.Stat(cw.configPath)
	if err != nil {
		select {
		case cw.errorChan <- fmt.Errorf("获取文件信息失败: %v", err):
		default:
		}
		return
	}

	modTime := info.ModTime()
	if modTime.Equal(cw.lastModTime) || modTime.Before(cw.lastModTime) {
		// 文件未真正修改
		return
	}

	cw.lastModTime = modTime

	// 重新加载配置
	newConfig, err := LoadConfig(cw.configPath)
	if err != nil {
		select {
		case cw.errorChan <- fmt.Errorf("重新加载配置失败: %v", err):
		default:
		}
		return
	}

	// 验证配置
	if err := newConfig.Validate(); err != nil {
		select {
		case cw.errorChan <- fmt.Errorf("配置验证失败: %v", err):
		default:
		}
		return
	}

	// 尝试热更新
	diff, err := cw.hotReloader.UpdateConfig(newConfig)
	if err != nil {
		select {
		case cw.errorChan <- fmt.Errorf("配置热更新失败: %v", err):
		default:
		}
		return
	}

	// 如果有需要重启的变更，通过channel通知
	if diff != nil && diff.RequiresRestart {
		select {
		case cw.updateChan <- newConfig:
		default:
		}
	}
}

// checkFileModTime 检查文件修改时间（备用机制）
func (cw *ConfigWatcher) checkFileModTime(ctx context.Context) {
	cw.mu.RLock()
	lastModTime := cw.lastModTime
	cw.mu.RUnlock()

	info, err := os.Stat(cw.configPath)
	if err != nil {
		return
	}

	if info.ModTime().After(lastModTime) {
		cw.handleConfigChange(ctx)
	}
}

// GetUpdateChan 获取配置更新通道
func (cw *ConfigWatcher) GetUpdateChan() <-chan *Config {
	return cw.updateChan
}

// GetErrorChan 获取错误通道
func (cw *ConfigWatcher) GetErrorChan() <-chan error {
	return cw.errorChan
}

