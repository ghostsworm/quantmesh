package web

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-webauthn/webauthn/webauthn"
)

var (
	// 全局 WebAuthn 管理器（需要从 main.go 注入）
	globalWebAuthnManager *WebAuthnManager
)

// SetWebAuthnManager 设置 WebAuthn 管理器
func SetWebAuthnManager(wm *WebAuthnManager) {
	globalWebAuthnManager = wm
}

// WebAuthnSessionStore WebAuthn 会话数据临时存储
var webauthnSessionStore = struct {
	sync.RWMutex
	data map[string]*webauthn.SessionData
}{
	data: make(map[string]*webauthn.SessionData),
}

// saveWebAuthnSession 保存会话数据
func saveWebAuthnSession(key string, sessionData *webauthn.SessionData) {
	webauthnSessionStore.Lock()
	defer webauthnSessionStore.Unlock()
	webauthnSessionStore.data[key] = sessionData

	// 5分钟后自动清理
	go func() {
		time.Sleep(5 * time.Minute)
		webauthnSessionStore.Lock()
		delete(webauthnSessionStore.data, key)
		webauthnSessionStore.Unlock()
	}()
}

// getWebAuthnSession 获取会话数据
func getWebAuthnSession(key string) *webauthn.SessionData {
	webauthnSessionStore.RLock()
	defer webauthnSessionStore.RUnlock()
	return webauthnSessionStore.data[key]
}

// deleteWebAuthnSession 删除会话数据
func deleteWebAuthnSession(key string) {
	webauthnSessionStore.Lock()
	defer webauthnSessionStore.Unlock()
	delete(webauthnSessionStore.data, key)
}

// beginWebAuthnRegistration 开始 WebAuthn 注册
// POST /api/webauthn/register/begin
func beginWebAuthnRegistration(c *gin.Context) {
	if globalWebAuthnManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "WebAuthn 管理器未初始化"})
		return
	}

	// 检查是否已登录（需要密码验证）
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
		DeviceName string `json:"device_name"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求"})
		return
	}

	// 获取用户
	user, err := globalWebAuthnManager.GetUser(session.Username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取用户失败"})
		return
	}

	// 生成注册选项
	options, sessionData, err := globalWebAuthnManager.webauthn.BeginRegistration(user)
	if err != nil {
		if globalWebAuthnManager.log != nil {
			globalWebAuthnManager.log.Errorf("生成 WebAuthn 注册选项失败: %v", err)
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成注册选项失败"})
		return
	}

	// 保存会话数据
	sessionKey := "webauthn_reg_" + session.Username + "_" + time.Now().Format("20060102150405")
	saveWebAuthnSession(sessionKey, sessionData)

	// 序列化选项
	optionsJSON, err := json.Marshal(options)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "序列化选项失败"})
		return
	}

	var optionsMap map[string]interface{}
	if err := json.Unmarshal(optionsJSON, &optionsMap); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "处理选项失败"})
		return
	}

	// 提取 publicKey 选项
	var publicKeyOptions map[string]interface{}
	if response, ok := optionsMap["Response"].(map[string]interface{}); ok {
		if publicKey, ok := response["PublicKey"].(map[string]interface{}); ok {
			publicKeyOptions = publicKey
		} else if publicKey, ok := response["publicKey"].(map[string]interface{}); ok {
			publicKeyOptions = publicKey
		} else {
			publicKeyOptions = response
		}
	} else if publicKey, ok := optionsMap["PublicKey"].(map[string]interface{}); ok {
		publicKeyOptions = publicKey
	} else if publicKey, ok := optionsMap["publicKey"].(map[string]interface{}); ok {
		publicKeyOptions = publicKey
	} else {
		publicKeyOptions = optionsMap
	}

	c.JSON(http.StatusOK, gin.H{
		"success":     true,
		"options":     publicKeyOptions,
		"session_key": sessionKey,
		"device_name": req.DeviceName,
	})
}

// finishWebAuthnRegistration 完成 WebAuthn 注册
// POST /api/webauthn/register/finish
func finishWebAuthnRegistration(c *gin.Context) {
	if globalWebAuthnManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "WebAuthn 管理器未初始化"})
		return
	}

	// TODO: 实现 WebAuthn 注册完成流程
	c.JSON(http.StatusNotImplemented, gin.H{"error": "WebAuthn 功能需要添加依赖库"})
}

// beginWebAuthnLogin 开始 WebAuthn 登录
// POST /api/webauthn/login/begin
func beginWebAuthnLogin(c *gin.Context) {
	if globalWebAuthnManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "WebAuthn 管理器未初始化"})
		return
	}

	var req struct {
		Username string `json:"username"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求"})
		return
	}

	// 检查用户是否存在
	_, err := globalWebAuthnManager.GetUser(req.Username)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "用户不存在或未注册 WebAuthn"})
		return
	}

	// 获取用户
	user, err := globalWebAuthnManager.GetUser(req.Username)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "用户不存在或未注册 WebAuthn"})
		return
	}

	// 生成登录选项
	options, sessionData, err := globalWebAuthnManager.webauthn.BeginLogin(user)
	if err != nil {
		if globalWebAuthnManager.log != nil {
			globalWebAuthnManager.log.Errorf("生成 WebAuthn 登录选项失败: %v", err)
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成登录选项失败"})
		return
	}

	// 保存会话数据
	sessionKey := "webauthn_login_" + req.Username + "_" + time.Now().Format("20060102150405")
	saveWebAuthnSession(sessionKey, sessionData)

	// 序列化选项
	optionsJSON, err := json.Marshal(options)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "序列化选项失败"})
		return
	}

	var optionsMap map[string]interface{}
	if err := json.Unmarshal(optionsJSON, &optionsMap); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "处理选项失败"})
		return
	}

	// 提取 publicKey 选项
	var publicKeyOptions map[string]interface{}
	if response, ok := optionsMap["Response"].(map[string]interface{}); ok {
		if publicKey, ok := response["PublicKey"].(map[string]interface{}); ok {
			publicKeyOptions = publicKey
		} else if publicKey, ok := response["publicKey"].(map[string]interface{}); ok {
			publicKeyOptions = publicKey
		} else {
			publicKeyOptions = response
		}
	} else if publicKey, ok := optionsMap["PublicKey"].(map[string]interface{}); ok {
		publicKeyOptions = publicKey
	} else if publicKey, ok := optionsMap["publicKey"].(map[string]interface{}); ok {
		publicKeyOptions = publicKey
	} else {
		publicKeyOptions = optionsMap
	}

	c.JSON(http.StatusOK, gin.H{
		"success":     true,
		"options":     publicKeyOptions,
		"session_key": sessionKey,
	})
}

// finishWebAuthnLogin 完成 WebAuthn 登录（需要密码验证）
// POST /api/webauthn/login/finish
func finishWebAuthnLogin(c *gin.Context) {
	if globalWebAuthnManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "WebAuthn 管理器未初始化"})
		return
	}

	// 读取请求体
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "读取请求体失败"})
		return
	}

	var req struct {
		Username   string                 `json:"username"`
		SessionKey string                 `json:"session_key"`
		Response   map[string]interface{} `json:"response"`
		Password   string                 `json:"password"` // 需要密码验证
	}

	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
		return
	}

	// 验证密码
	if globalPasswordManager != nil {
		valid, err := globalPasswordManager.VerifyPassword(req.Username, req.Password)
		if err != nil || !valid {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "密码错误"})
			return
		}
	}

	// 从临时存储获取 sessionData
	sessionData := getWebAuthnSession(req.SessionKey)
	if sessionData == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "会话已过期，请重新开始登录"})
		return
	}

	// 获取用户
	user, err := globalWebAuthnManager.GetUser(req.Username)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "用户不存在"})
		return
	}

	// 将 response 转换为 JSON，作为新的请求体传递给 webauthn 库
	responseBytes, err := json.Marshal(req.Response)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "处理响应失败"})
		return
	}

	// 创建新的请求体供 webauthn 库使用
	r := c.Request
	r.Body = io.NopCloser(bytes.NewBuffer(responseBytes))

	// 验证并完成登录
	credential, err := globalWebAuthnManager.webauthn.FinishLogin(user, *sessionData, r)
	if err != nil {
		if globalWebAuthnManager.log != nil {
			globalWebAuthnManager.log.Errorf("完成 WebAuthn 登录失败: %v", err)
		}
		c.JSON(http.StatusUnauthorized, gin.H{"error": "登录失败: " + err.Error()})
		return
	}

	// 更新凭证计数器并删除会话数据
	credentialIDBase64 := base64.RawURLEncoding.EncodeToString(credential.ID)
	counter := credential.Authenticator.SignCount
	if err := globalWebAuthnManager.UpdateCredentialCounter(credentialIDBase64, counter); err != nil {
		if globalWebAuthnManager.log != nil {
			globalWebAuthnManager.log.Warnf("更新凭证计数器失败: %v", err)
		}
	}
	deleteWebAuthnSession(req.SessionKey)

	// 创建会话
	sm := GetSessionManager()
	if sm != nil {
		ip := c.ClientIP()
		userAgent := c.GetHeader("User-Agent")
		session, err := sm.CreateSession(req.Username, "admin", ip, userAgent)
		if err == nil {
			secure := c.Request.TLS != nil
			sm.SetSessionCookie(c.Writer, session.SessionID, secure)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"user": map[string]interface{}{
			"username": req.Username,
			"role":     "admin",
		},
	})
}

// listWebAuthnCredentials 列出所有凭证
// GET /api/webauthn/credentials
func listWebAuthnCredentials(c *gin.Context) {
	if globalWebAuthnManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "WebAuthn 管理器未初始化"})
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

	credentials, err := globalWebAuthnManager.ListCredentials(session.Username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取凭证列表失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "credentials": credentials})
}

// deleteWebAuthnCredential 删除凭证
// POST /api/webauthn/credentials/delete
func deleteWebAuthnCredential(c *gin.Context) {
	if globalWebAuthnManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "WebAuthn 管理器未初始化"})
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
		CredentialID string `json:"credential_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求"})
		return
	}

	if err := globalWebAuthnManager.DeleteCredential(req.CredentialID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除凭证失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "凭证已删除"})
}

