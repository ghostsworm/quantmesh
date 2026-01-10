package web

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"quantmesh/logger"
)

// authMiddleware 认证中间件
func authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 获取会话管理器
		sm := GetSessionManager()
		if sm == nil {
			respondError(c, http.StatusInternalServerError, "error.session_manager_not_initialized")
			c.Abort()
			return
		}

		// 从请求中获取会话
		session, exists := sm.GetSessionFromRequest(c.Request)
		if !exists || session == nil {
			// 认证失败日志：写入Web日志文件（而不是标准输出）
			cookies := c.Request.Cookies()
			logMessage := fmt.Sprintf("[AUTH] 认证失败，请求路径: %s, Cookie 数量: %d", c.Request.URL.Path, len(cookies))
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

		// 将会话信息存储到上下文中，供后续处理使用
		c.Set("session", session)
		c.Set("username", session.Username)

		c.Next()
	}
}

// optionalAuthMiddleware 可选认证中间件（如果已登录则设置上下文，但不强制）
func optionalAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
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
