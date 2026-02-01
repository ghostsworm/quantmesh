package web

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"quantmesh/logger"
)

var (
	// 全局密碼管理器（需要從 main.go 注入）
	globalPasswordManager *PasswordManager
)

// SetPasswordManager 設置密碼管理器
func SetPasswordManager(pm *PasswordManager) {
	globalPasswordManager = pm
}

// SetSessionManager 設置會话管理器（為了保持一致性，但實際使用 GetSessionManager）
func SetSessionManager(sm *SessionManager) {
	// 實際使用全局單例 GetSessionManager()
}

// getAuthStatus 獲取认证状態
// GET /api/auth/status
func getAuthStatus(c *gin.Context) {
	if globalPasswordManager == nil {
		// 密碼管理器未初始化時，返回特殊標記
		// 前端應該顯示錯誤提示而不是設置密碼頁面
		c.JSON(http.StatusOK, gin.H{
			"has_password":         false,
			"has_webauthn":         false,
			"password_manager_error": true, // 🔒 新增：標識密碼管理器初始化失敗
		})
		return
	}

	// 單用戶场景，使用固定用戶名
	username := "admin"
	hasPassword, _ := globalPasswordManager.HasPassword(username)

	// 检查是否有 WebAuthn 凭证
	hasWebAuthn := false
	if globalWebAuthnManager != nil {
		hasWebAuthn, _ = globalWebAuthnManager.HasCredentials(username)
	}

	// 检查當前會话
	isAuthenticated := false
	sm := GetSessionManager()
	if sm != nil {
		session, exists := sm.GetSessionFromRequest(c.Request)
		isAuthenticated = exists && session != nil
	}

	// 🔒 安全检查：檢測是否存在數據丟失的安全隱患
	securityCompromised := false
	if globalPasswordManager != nil {
		compromised, _ := globalPasswordManager.IsSecurityCompromised()
		securityCompromised = compromised
	}

	c.JSON(http.StatusOK, gin.H{
		"has_password":         hasPassword,
		"has_webauthn":         hasWebAuthn,
		"is_authenticated":     isAuthenticated,
		"security_compromised": securityCompromised, // 🔒 新增：標識是否存在安全隱患
	})
}

// setPassword 設置密碼（僅首次設置，已設置密碼后需要认证才能修改）
// POST /api/auth/password/set
func setPassword(c *gin.Context) {
	logger.WriteWebLog("[AUTH] 收到設置密碼请求")

	if globalPasswordManager == nil {
		logger.WriteWebLog("[AUTH] 密碼管理器未初始化 - 這通常表示服務器啟動時無法創建或訪問 data 目錄")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "密碼管理器未初始化",
			"code":    "PASSWORD_MANAGER_NOT_INITIALIZED",
			"details": "服務器無法初始化認證系統。請檢查：1) data 目錄是否存在且可寫 2) Docker 是否正確掛載了 data 卷 3) 查看服務器日誌中的 '初始化密碼管理器失败' 錯誤",
		})
		return
	}

	// 單用戶场景，使用固定用戶名
	username := "admin"

	// 🔒 安全检查：檢測是否存在數據丟失的安全隱患
	// 如果 .installed 標記存在但數據庫中沒有密碼記錄，則拒絕設置並返回安全警告
	compromised, _ := globalPasswordManager.IsSecurityCompromised()
	if compromised {
		logger.WriteWebLog("[AUTH] ⚠️ 安全警告：檢測到 .installed 標記但數據庫無密碼記錄，可能存在數據丟失")
		c.JSON(http.StatusForbidden, gin.H{
			"error":   "系統檢測到安全隱患：認證數據可能已丟失。請聯繫管理員檢查 data 目錄。",
			"code":    "SECURITY_COMPROMISED",
			"details": "系統之前已完成初始化，但認證數據庫（auth.db）中的密碼記錄已丟失。這可能是由於數據目錄未正確掛載、數據庫文件被刪除或損壞導致。",
		})
		return
	}
	
	// 🔒 安全检查：如果已經設置過密碼，则拒绝请求
	hasPassword, err := globalPasswordManager.HasPassword(username)
	if err != nil {
		logger.WriteWebLog(fmt.Sprintf("[AUTH] 检查密碼状態失败: %v", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "检查密碼状態失败"})
		return
	}
	
	if hasPassword {
		logger.WriteWebLog("[AUTH] ⚠️ 拒绝設置密碼请求：密碼已存在，请使用修改密碼接口")
		c.JSON(http.StatusForbidden, gin.H{
			"error": "密碼已設置，请使用修改密碼功能",
			"code": "PASSWORD_ALREADY_SET",
		})
		return
	}

	var req struct {
		Password string `json:"password" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		logger.WriteWebLog(fmt.Sprintf("[AUTH] 設置密碼请求参數無效: %v", err))
		c.JSON(http.StatusBadRequest, gin.H{"error": "無效的请求"})
		return
	}

	if err := globalPasswordManager.SetPassword(username, req.Password); err != nil {
		logger.WriteWebLog(fmt.Sprintf("[AUTH] 設置密碼失败: %v", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "設置密碼失败"})
		return
	}
	logger.WriteWebLog("[AUTH] ✅ 首次密碼已保存到數據库")

	// 首次設置密碼后自动創建會话（自动登錄）
	// 必須在 c.JSON() 之前設置 Cookie
	sm := GetSessionManager()
	if sm == nil {
		logger.WriteWebLog("[AUTH] 會话管理器未初始化")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "密碼設置成功"})
		return
	}

	ip := c.ClientIP()
	userAgent := c.GetHeader("User-Agent")
	logger.WriteWebLog(fmt.Sprintf("[AUTH] 創建會话: IP=%s, UserAgent=%s", ip, userAgent))

	session, err := sm.CreateSession(username, "admin", ip, userAgent)
	if err != nil {
		logger.WriteWebLog(fmt.Sprintf("[AUTH] 創建會话失败: %v", err))
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "密碼設置成功，但會话創建失败",
			"warning": "请手动登錄",
		})
		return
	}

	logger.WriteWebLog(fmt.Sprintf("[AUTH] 會话已創建，SessionID: %s...", session.SessionID[:20]))

	// 使用 Gin 的 SetCookie 方法設置會话Cookie
	// MaxAge: 24小時 = 86400秒
	c.SetCookie(
		"session_id",      // name
		session.SessionID, // value
		86400,             // maxAge (24小時)
		"/",               // path
		"",                // domain (空字符串表示當前域)
		false,             // secure (HTTP 环境設為 false)
		true,              // httpOnly
	)
	logger.WriteWebLog("[AUTH] Cookie 已通過 Gin 設置: Name=session_id, Path=/, MaxAge=86400, HttpOnly=true, Secure=false")

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "密碼設置成功"})
}

// verifyPassword 驗证密碼並創建會话
// POST /api/auth/password/verify
func verifyPassword(c *gin.Context) {
	if globalPasswordManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "密碼管理器未初始化"})
		return
	}

	var req struct {
		Password string `json:"password" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "無效的请求"})
		return
	}

	// 單用戶场景，使用固定用戶名
	username := "admin"
	valid, err := globalPasswordManager.VerifyPassword(username, req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "驗证密碼失败"})
		return
	}

	if !valid {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "密碼錯误"})
		return
	}

	// 創建會话
	sm := GetSessionManager()
	if sm != nil {
		ip := c.ClientIP()
		userAgent := c.GetHeader("User-Agent")
		session, err := sm.CreateSession(username, "admin", ip, userAgent)
		if err == nil {
			// 設置會话Cookie
			secure := c.Request.TLS != nil
			sm.SetSessionCookie(c.Writer, session.SessionID, secure)
		}
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// changePassword 修改密碼
// POST /api/auth/password/change
func changePassword(c *gin.Context) {
	if globalPasswordManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "密碼管理器未初始化"})
		return
	}

	// 检查是否已登錄
	sm := GetSessionManager()
	if sm == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "请先登錄"})
		return
	}

	session, exists := sm.GetSessionFromRequest(c.Request)
	if !exists || session == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "请先登錄"})
		return
	}

	var req struct {
		CurrentPassword string `json:"current_password" binding:"required"`
		NewPassword     string `json:"new_password" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "無效的请求"})
		return
	}

	// 驗证當前密碼
	valid, err := globalPasswordManager.VerifyPassword(session.Username, req.CurrentPassword)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "驗证密碼失败"})
		return
	}

	if !valid {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "當前密碼錯误"})
		return
	}

	// 設置新密碼
	if err := globalPasswordManager.SetPassword(session.Username, req.NewPassword); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "修改密碼失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "密碼修改成功"})
}

// logout 退出登錄
// POST /api/auth/logout
func logout(c *gin.Context) {
	sm := GetSessionManager()
	if sm == nil {
		c.JSON(http.StatusOK, gin.H{"success": true})
		return
	}

	// 獲取會话ID
	cookie, err := c.Cookie("session_id")
	if err == nil && cookie != "" {
		sm.DeleteSession(cookie)
	}

	// 清除Cookie
	sm.ClearSessionCookie(c.Writer)

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "已退出登錄"})
}
