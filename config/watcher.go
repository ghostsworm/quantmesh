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

// ConfigWatcher 配置文件監控器
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

// NewConfigWatcher 創建配置監控器
func NewConfigWatcher(configPath string, hotReloader *HotReloader, backupManager *BackupManager) (*ConfigWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("創建文件監控器失败: %v", err)
	}

	// 獲取配置文件所在目錄
	configDir := filepath.Dir(configPath)
	if configDir == "" || configDir == "." {
		// 使用當前目錄
		var err error
		configDir, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("獲取當前目錄失败: %v", err)
		}
		configPath = filepath.Join(configDir, filepath.Base(configPath))
	}

	// 獲取初始修改時间
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

// Start 开始監控配置文件
func (cw *ConfigWatcher) Start(ctx context.Context) error {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	if cw.isWatching {
		return fmt.Errorf("配置監控器已經在运行")
	}

	// 添加配置文件所在目錄到監控
	configDir := filepath.Dir(cw.configPath)
	if err := cw.watcher.Add(configDir); err != nil {
		return fmt.Errorf("添加監控目錄失败: %v", err)
	}

	cw.isWatching = true

	// 啟动監控协程
	go cw.watchLoop(ctx)

	return nil
}

// Stop 停止監控
func (cw *ConfigWatcher) Stop() error {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	if !cw.isWatching {
		return nil
	}

	cw.isWatching = false
	return cw.watcher.Close()
}

// watchLoop 監控循环
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
			// 检查是否是目標配置文件的变化
			if event.Name == cw.configPath {
				if event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create {
					// 延迟处理，避免文件正在写入時读取
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
			// 定期检查文件修改時间（作為备用机制）
			cw.checkFileModTime(ctx)
		}
	}
}

// handleConfigChange 处理配置文件变化
func (cw *ConfigWatcher) handleConfigChange(ctx context.Context) {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	// 检查文件修改時间，避免重複处理
	info, err := os.Stat(cw.configPath)
	if err != nil {
		select {
		case cw.errorChan <- fmt.Errorf("獲取文件信息失败: %v", err):
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

	// 重新加載配置
	newConfig, err := LoadConfig(cw.configPath)
	if err != nil {
		select {
		case cw.errorChan <- fmt.Errorf("重新加載配置失败: %v", err):
		default:
		}
		return
	}

	// 驗证配置
	if err := newConfig.Validate(); err != nil {
		select {
		case cw.errorChan <- fmt.Errorf("配置驗证失败: %v", err):
		default:
		}
		return
	}

	// 尝試热更新
	diff, err := cw.hotReloader.UpdateConfig(newConfig)
	if err != nil {
		select {
		case cw.errorChan <- fmt.Errorf("配置热更新失败: %v", err):
		default:
		}
		return
	}

	// 如果有需要重啟的变更，通過channel通知
	if diff != nil && diff.RequiresRestart {
		select {
		case cw.updateChan <- newConfig:
		default:
		}
	}
}

// checkFileModTime 检查文件修改時间（备用机制）
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

// GetUpdateChan 獲取配置更新通道
func (cw *ConfigWatcher) GetUpdateChan() <-chan *Config {
	return cw.updateChan
}

// GetErrorChan 獲取錯误通道
func (cw *ConfigWatcher) GetErrorChan() <-chan error {
	return cw.errorChan
}
