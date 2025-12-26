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
			c.JSON(http.StatusInternalServerError, gin.H{"error": "会话管理器未初始化"})
			c.Abort()
			return
		}

		// 从请求中获取会话
		session, exists := sm.GetSessionFromRequest(c.Request)
		if !exists || session == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录，请先登录"})
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

