package web

import (
	"net/http"

	"github.com/gin-gonic/gin"
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
		// 调试日志：打印请求中的所有 Cookie
		cookies := c.Request.Cookies()
		println("✗ 认证失败，请求路径:", c.Request.URL.Path)
		println("  Cookie 数量:", len(cookies))
		for _, cookie := range cookies {
			val := cookie.Value
			if len(val) > 20 {
				val = val[:20] + "..."
			}
			println("  - Cookie:", cookie.Name, "=", val)
		}
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

