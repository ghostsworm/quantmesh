package web

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"quantmesh/logger"
	"quantmesh/storage"
)

var (
	// localDevModeCache 本地开发模式缓存
	localDevModeCache struct {
		value bool
		mu    sync.RWMutex
	}
	// storageProvider 存储提供者（用于读取系统设置）
	storageProvider SystemSettingsProvider
)

// SystemSettingsProvider 系统设置提供者接口
type SystemSettingsProvider interface {
	GetSystemSettingBool(ctx context.Context, key string, defaultValue bool) (bool, error)
	GetSystemSettings(ctx context.Context, filter *storage.SystemSettingFilter) ([]*storage.SystemSetting, error)
	GetSystemSetting(ctx context.Context, key string) (*storage.SystemSetting, error)
	SetSystemSettingBool(ctx context.Context, key string, value bool) error
	SetSystemSettingString(ctx context.Context, key, value string) error
	SaveSystemSetting(ctx context.Context, key, value, settingType string) error
	DeleteSystemSetting(ctx context.Context, key string) error
}

// SetStorageProvider 设置存储提供者
func SetStorageProvider(provider SystemSettingsProvider) {
	storageProvider = provider
}

// isLocalDevMode 检查是否启用本地开发模式（免登录）
func isLocalDevMode() bool {
	localDevModeCache.mu.RLock()
	defer localDevModeCache.mu.RUnlock()

	if storageProvider == nil {
		return false
	}

	// 每次都从数据库读取最新值
	ctx := context.Background()
	enabled, err := storageProvider.GetSystemSettingBool(ctx, "local_dev_mode", false)
	if err != nil {
		logger.Warn("读取 local_dev_mode 设置失败: %v", err)
		return false
	}

	localDevModeCache.value = enabled
	return enabled
}

// refreshLocalDevModeCache 刷新本地开发模式缓存
func refreshLocalDevModeCache() {
	localDevModeCache.mu.Lock()
	defer localDevModeCache.mu.Unlock()

	if storageProvider != nil {
		ctx := context.Background()
		enabled, err := storageProvider.GetSystemSettingBool(ctx, "local_dev_mode", false)
		if err != nil {
			logger.Warn("刷新 local_dev_mode 设置失败: %v", err)
			return
		}
		localDevModeCache.value = enabled
		logger.Info("Local dev mode: %v", enabled)
	}
}

// authMiddleware 认证中间件
func authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 检查是否启用本地开发模式（免登录）
		if isLocalDevMode() {
			// 本地开发模式：设置默认用户，跳过认证
			c.Set("session", nil)
			c.Set("username", "local_dev_user")
			c.Set("local_dev_mode", true)
			logger.WriteWebLog("[AUTH] Local dev mode enabled, skipping authentication")
			c.Next()
			return
		}

		// 獲取會话管理器
		sm := GetSessionManager()
		if sm == nil {
			respondError(c, http.StatusInternalServerError, "error.session_manager_not_initialized")
			c.Abort()
			return
		}

		// 從请求中獲取會话
		session, exists := sm.GetSessionFromRequest(c.Request)
		if !exists || session == nil {
			// 认证失败日志：写入Web日志文件（而不是標准输出）
			cookies := c.Request.Cookies()
			logMessage := fmt.Sprintf("[AUTH] 认证失败，请求路径: %s, Cookie 數量: %d", c.Request.URL.Path, len(cookies))
			if len(cookies) > 0 {
				cookieInfo := ""
				for _, cookie := range cookies {
					val := cookie.Value
					if len(val) > 20 {
						val = val[:20] + "..."
					}
					if cookieInfo != "" {
						cookieInfo += ", "
					}
					cookieInfo += fmt.Sprintf("%s=%s", cookie.Name, val)
				}
				logMessage += fmt.Sprintf(", Cookies: [%s]", cookieInfo)
			}
			logger.WriteWebLog(logMessage)
			respondError(c, http.StatusUnauthorized, "error.not_logged_in")
			c.Abort()
			return
		}

		// 將會话信息存儲到上下文中，供后续处理使用
		c.Set("session", session)
		c.Set("username", session.Username)
		c.Set("local_dev_mode", false)

		c.Next()
	}
}

// optionalAuthMiddleware 可選认证中间件（如果已登錄则設置上下文，但不强制）
func optionalAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 检查是否启用本地开发模式
		if isLocalDevMode() {
			c.Set("local_dev_mode", true)
			c.Set("username", "local_dev_user")
		}

		sm := GetSessionManager()
		if sm != nil {
			session, exists := sm.GetSessionFromRequest(c.Request)
			if exists && session != nil {
				c.Set("session", session)
				c.Set("username", session.Username)
			}
		}
		c.Next()
	}
}
