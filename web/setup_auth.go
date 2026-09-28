package web

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func authorizeSetupMutation(c *gin.Context) bool {
	deny := func(status int, message string) bool {
		c.AbortWithStatusJSON(status, SetupInitResponse{Success: false, Message: message})
		return false
	}
	if globalPasswordManager == nil {
		return deny(http.StatusServiceUnavailable, "认证服务不可用，拒绝修改配置")
	}
	hasPassword, err := globalPasswordManager.HasPassword("admin")
	if err != nil {
		return deny(http.StatusServiceUnavailable, "无法核实认证状态，拒绝修改配置")
	}
	if !hasPassword && isDirectLoopbackRequest(c.Request) {
		// First installation only: a direct local connection (or SSH tunnel).
		// Remote installation must first establish an authenticated session.
		return true
	}
	sm := GetSessionManager()
	if sm == nil {
		return deny(http.StatusServiceUnavailable, "会话服务不可用，拒绝修改配置")
	}
	session, exists := sm.GetSessionFromRequest(c.Request)
	if !exists || session == nil {
		return deny(http.StatusUnauthorized, "需要登录后才能修改配置；首次本地设置请使用回环连接")
	}
	return true
}
