package web

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"quantmesh/logger"
)

var (
	// 全局密码管理器（需要从 main.go 注入）
	globalPasswordManager *PasswordManager
)

// SetPasswordManager 设置密码管理器
func SetPasswordManager(pm *PasswordManager) {
	globalPasswordManager = pm
}

// SetSessionManager 设置会话管理器（为了保持一致性，但实际使用 GetSessionManager）
func SetSessionManager(sm *SessionManager) {
	// 实际使用全局单例 GetSessionManager()
}

// getAuthStatus 获取认证状态
// GET /api/auth/status
func getAuthStatus(c *gin.Context) {
	if globalPasswordManager == nil {
		c.JSON(http.StatusOK, gin.H{
			"has_password": false,
			"has_webauthn": false,
		})
		return
	}

	// 单用户场景，使用固定用户名
	username := "admin"
	hasPassword, _ := globalPasswordManager.HasPassword(username)

	// 检查是否有 WebAuthn 凭证
	hasWebAuthn := false
	if globalWebAuthnManager != nil {
		hasWebAuthn, _ = globalWebAuthnManager.HasCredentials(username)
	}

	// 检查当前会话
	isAuthenticated := false
	sm := GetSessionManager()
	if sm != nil {
		session, exists := sm.GetSessionFromRequest(c.Request)
		isAuthenticated = exists && session != nil
	}

	c.JSON(http.StatusOK, gin.H{
		"has_password":     hasPassword,
		"has_webauthn":     hasWebAuthn,
		"is_authenticated": isAuthenticated,
	})
}

// setPassword 设置密码
// POST /api/auth/password/set
func setPassword(c *gin.Context) {
	logger.WriteWebLog("[AUTH] 收到设置密码请求")

	if globalPasswordManager == nil {
		logger.WriteWebLog("[AUTH] 密码管理器未初始化")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "密码管理器未初始化"})
		return
	}

	var req struct {
		Password string `json:"password" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		logger.WriteWebLog(fmt.Sprintf("[AUTH] 设置密码请求参数无效: %v", err))
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求"})
		return
	}

	// 单用户场景，使用固定用户名
	username := "admin"
	if err := globalPasswordManager.SetPassword(username, req.Password); err != nil {
		logger.WriteWebLog(fmt.Sprintf("[AUTH] 设置密码失败: %v", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "设置密码失败"})
		return
	}
	logger.WriteWebLog("[AUTH] 密码已保存到数据库")

	// 首次设置密码后自动创建会话（自动登录）
	// 必须在 c.JSON() 之前设置 Cookie
	sm := GetSessionManager()
	if sm == nil {
		logger.WriteWebLog("[AUTH] 会话管理器未初始化")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "密码设置成功"})
		return
	}

	ip := c.ClientIP()
	userAgent := c.GetHeader("User-Agent")
	logger.WriteWebLog(fmt.Sprintf("[AUTH] 创建会话: IP=%s, UserAgent=%s", ip, userAgent))

	session, err := sm.CreateSession(username, "admin", ip, userAgent)
	if err != nil {
		logger.WriteWebLog(fmt.Sprintf("[AUTH] 创建会话失败: %v", err))
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "密码设置成功，但会话创建失败",
			"warning": "请手动登录",
		})
		return
	}

	logger.WriteWebLog(fmt.Sprintf("[AUTH] 会话已创建，SessionID: %s...", session.SessionID[:20]))

	// 使用 Gin 的 SetCookie 方法设置会话Cookie
	// MaxAge: 24小时 = 86400秒
	c.SetCookie(
		"session_id",      // name
		session.SessionID, // value
		86400,             // maxAge (24小时)
		"/",               // path
		"",                // domain (空字符串表示当前域)
		false,             // secure (HTTP 环境设为 false)
		true,              // httpOnly
	)
	logger.WriteWebLog("[AUTH] Cookie 已通过 Gin 设置: Name=session_id, Path=/, MaxAge=86400, HttpOnly=true, Secure=false")

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "密码设置成功"})
}

// verifyPassword 验证密码并创建会话
// POST /api/auth/password/verify
func verifyPassword(c *gin.Context) {
	if globalPasswordManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "密码管理器未初始化"})
		return
	}

	var req struct {
		Password string `json:"password" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求"})
		return
	}

	// 单用户场景，使用固定用户名
	username := "admin"
	valid, err := globalPasswordManager.VerifyPassword(username, req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "验证密码失败"})
		return
	}

	if !valid {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "密码错误"})
		return
	}

	// 创建会话
	sm := GetSessionManager()
	if sm != nil {
		ip := c.ClientIP()
		userAgent := c.GetHeader("User-Agent")
		session, err := sm.CreateSession(username, "admin", ip, userAgent)
		if err == nil {
			// 设置会话Cookie
			secure := c.Request.TLS != nil
			sm.SetSessionCookie(c.Writer, session.SessionID, secure)
		}
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// changePassword 修改密码
// POST /api/auth/password/change
func changePassword(c *gin.Context) {
	if globalPasswordManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "密码管理器未初始化"})
		return
	}

	// 检查是否已登录
	sm := GetSessionManager()
	if sm == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "请先登录"})
		return
	}

	session, exists := sm.GetSessionFromRequest(c.Request)
	if !exists || session == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "请先登录"})
		return
	}

	var req struct {
		CurrentPassword string `json:"current_password" binding:"required"`
		NewPassword     string `json:"new_password" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求"})
		return
	}

	// 验证当前密码
	valid, err := globalPasswordManager.VerifyPassword(session.Username, req.CurrentPassword)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "验证密码失败"})
		return
	}

	if !valid {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "当前密码错误"})
		return
	}

	// 设置新密码
	if err := globalPasswordManager.SetPassword(session.Username, req.NewPassword); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "修改密码失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "密码修改成功"})
}

// logout 退出登录
// POST /api/auth/logout
func logout(c *gin.Context) {
	sm := GetSessionManager()
	if sm == nil {
		c.JSON(http.StatusOK, gin.H{"success": true})
		return
	}

	// 获取会话ID
	cookie, err := c.Cookie("session_id")
	if err == nil && cookie != "" {
		sm.DeleteSession(cookie)
	}

	// 清除Cookie
	sm.ClearSessionCookie(c.Writer)

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "已退出登录"})
}
