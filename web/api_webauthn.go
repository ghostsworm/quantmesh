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

// normalizeWebAuthnResponse 规范化 WebAuthn 响应，将数组格式转换为 base64url 字符串
func normalizeWebAuthnResponse(response map[string]interface{}) map[string]interface{} {
	if response == nil {
		return nil
	}

	normalized := make(map[string]interface{})

	// 复制所有字段
	for k, v := range response {
		normalized[k] = v
	}

	// 转换 rawId：如果是数组，转换为 base64url 字符串
	if rawId, ok := normalized["rawId"]; ok {
		if rawIdArray, ok := rawId.([]interface{}); ok {
			// 转换为字节数组
			bytes := make([]byte, len(rawIdArray))
			for i, v := range rawIdArray {
				if num, ok := v.(float64); ok {
					bytes[i] = byte(num)
				} else {
					return nil // 无效的数组元素
				}
			}
			// 转换为 base64url 字符串
			normalized["rawId"] = base64.RawURLEncoding.EncodeToString(bytes)
			if globalWebAuthnManager != nil && globalWebAuthnManager.log != nil {
				globalWebAuthnManager.log.Debugf("[WebAuthn注册] 转换 rawId: 数组[%d] -> base64url字符串[%d]", len(rawIdArray), len(normalized["rawId"].(string)))
			}
		}
		// 如果已经是字符串，保持不变
	}

	// 转换 response 对象
	if resp, ok := normalized["response"].(map[string]interface{}); ok {
		normalizedResp := make(map[string]interface{})

		// 转换 attestationObject
		if attObj, ok := resp["attestationObject"]; ok {
			if attObjArray, ok := attObj.([]interface{}); ok {
				bytes := make([]byte, len(attObjArray))
				for i, v := range attObjArray {
					if num, ok := v.(float64); ok {
						bytes[i] = byte(num)
					} else {
						return nil
					}
				}
				normalizedResp["attestationObject"] = base64.RawURLEncoding.EncodeToString(bytes)
				if globalWebAuthnManager != nil && globalWebAuthnManager.log != nil {
					globalWebAuthnManager.log.Debugf("[WebAuthn注册] 转换 attestationObject: 数组[%d] -> base64url字符串[%d]", len(attObjArray), len(normalizedResp["attestationObject"].(string)))
				}
			} else {
				normalizedResp["attestationObject"] = attObj
			}
		}

		// 转换 clientDataJSON
		if clientData, ok := resp["clientDataJSON"]; ok {
			if clientDataArray, ok := clientData.([]interface{}); ok {
				bytes := make([]byte, len(clientDataArray))
				for i, v := range clientDataArray {
					if num, ok := v.(float64); ok {
						bytes[i] = byte(num)
					} else {
						return nil
					}
				}
				normalizedResp["clientDataJSON"] = base64.RawURLEncoding.EncodeToString(bytes)
				if globalWebAuthnManager != nil && globalWebAuthnManager.log != nil {
					globalWebAuthnManager.log.Debugf("[WebAuthn注册] 转换 clientDataJSON: 数组[%d] -> base64url字符串[%d]", len(clientDataArray), len(normalizedResp["clientDataJSON"].(string)))
				}
			} else {
				normalizedResp["clientDataJSON"] = clientData
			}
		}

		// 复制其他字段
		for k, v := range resp {
			if k != "attestationObject" && k != "clientDataJSON" {
				normalizedResp[k] = v
			}
		}

		normalized["response"] = normalizedResp
	}

	return normalized
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

	// 读取请求体
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		if globalWebAuthnManager.log != nil {
			globalWebAuthnManager.log.Errorf("[WebAuthn注册] 读取请求体失败: %v", err)
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "读取请求体失败"})
		return
	}

	if globalWebAuthnManager.log != nil {
		globalWebAuthnManager.log.Debugf("[WebAuthn注册] 收到注册完成请求，请求体长度: %d 字节", len(bodyBytes))
		// 记录请求体前500个字符（避免日志过长）
		bodyPreview := string(bodyBytes)
		if len(bodyPreview) > 500 {
			bodyPreview = bodyPreview[:500] + "...(截断)"
		}
		globalWebAuthnManager.log.Debugf("[WebAuthn注册] 请求体预览: %s", bodyPreview)
	}

	var req struct {
		SessionKey string                 `json:"session_key"`
		DeviceName string                 `json:"device_name"`
		Response   map[string]interface{} `json:"response"`
	}

	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		if globalWebAuthnManager.log != nil {
			globalWebAuthnManager.log.Errorf("[WebAuthn注册] JSON 解析失败: %v, 请求体: %s", err, string(bodyBytes))
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
		return
	}

	if globalWebAuthnManager.log != nil {
		globalWebAuthnManager.log.Debugf("[WebAuthn注册] 解析请求成功 - SessionKey: %s, DeviceName: %s", req.SessionKey, req.DeviceName)
		// 记录 response 的结构信息
		if req.Response != nil {
			responseKeys := make([]string, 0, len(req.Response))
			for k := range req.Response {
				responseKeys = append(responseKeys, k)
			}
			globalWebAuthnManager.log.Debugf("[WebAuthn注册] Response 包含字段: %v", responseKeys)

			// 检查关键字段
			if id, ok := req.Response["id"].(string); ok {
				globalWebAuthnManager.log.Debugf("[WebAuthn注册] Response.id: %s", id)
			}
			if rawId, ok := req.Response["rawId"]; ok {
				if rawIdArray, ok := rawId.([]interface{}); ok {
					globalWebAuthnManager.log.Debugf("[WebAuthn注册] Response.rawId 类型: []interface{}, 长度: %d", len(rawIdArray))
				} else {
					globalWebAuthnManager.log.Debugf("[WebAuthn注册] Response.rawId 类型: %T", rawId)
				}
			}
			if resp, ok := req.Response["response"].(map[string]interface{}); ok {
				respKeys := make([]string, 0, len(resp))
				for k := range resp {
					respKeys = append(respKeys, k)
				}
				globalWebAuthnManager.log.Debugf("[WebAuthn注册] Response.response 包含字段: %v", respKeys)
			}
		} else {
			globalWebAuthnManager.log.Warnf("[WebAuthn注册] Response 为 nil")
		}
	}

	// 从临时存储获取 sessionData
	sessionData := getWebAuthnSession(req.SessionKey)
	if sessionData == nil {
		if globalWebAuthnManager.log != nil {
			globalWebAuthnManager.log.Warnf("[WebAuthn注册] 会话数据不存在 - SessionKey: %s", req.SessionKey)
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "会话已过期，请重新开始注册"})
		return
	}

	if globalWebAuthnManager.log != nil {
		globalWebAuthnManager.log.Debugf("[WebAuthn注册] 会话数据获取成功 - Challenge 长度: %d", len(sessionData.Challenge))
	}

	// 获取用户
	user, err := globalWebAuthnManager.GetUser(session.Username)
	if err != nil {
		if globalWebAuthnManager.log != nil {
			globalWebAuthnManager.log.Errorf("[WebAuthn注册] 获取用户失败 - Username: %s, Error: %v", session.Username, err)
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "获取用户失败"})
		return
	}

	if globalWebAuthnManager.log != nil {
		globalWebAuthnManager.log.Debugf("[WebAuthn注册] 用户获取成功 - Username: %s, 已有凭证数: %d", session.Username, len(user.WebAuthnCredentials()))
	}

	// 转换数组格式为 base64url 字符串格式（兼容旧版本前端）
	normalizedResponse := normalizeWebAuthnResponse(req.Response)
	if normalizedResponse == nil {
		if globalWebAuthnManager.log != nil {
			globalWebAuthnManager.log.Errorf("[WebAuthn注册] 规范化 Response 失败")
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "处理响应失败"})
		return
	}

	// 将 response 转换为 JSON，作为新的请求体传递给 webauthn 库
	responseBytes, err := json.Marshal(normalizedResponse)
	if err != nil {
		if globalWebAuthnManager.log != nil {
			globalWebAuthnManager.log.Errorf("[WebAuthn注册] 序列化 Response 失败: %v", err)
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "处理响应失败"})
		return
	}

	if globalWebAuthnManager.log != nil {
		globalWebAuthnManager.log.Debugf("[WebAuthn注册] Response 序列化成功，长度: %d 字节", len(responseBytes))
		// 记录序列化后的 JSON 预览（前500字符）
		responsePreview := string(responseBytes)
		if len(responsePreview) > 500 {
			responsePreview = responsePreview[:500] + "...(截断)"
		}
		globalWebAuthnManager.log.Debugf("[WebAuthn注册] 序列化后的 Response 预览: %s", responsePreview)
	}

	// 创建新的请求体供 webauthn 库使用
	r := c.Request
	r.Body = io.NopCloser(bytes.NewBuffer(responseBytes))

	// 验证并完成注册
	if globalWebAuthnManager.log != nil {
		globalWebAuthnManager.log.Debugf("[WebAuthn注册] 开始调用 FinishRegistration")
	}
	credential, err := globalWebAuthnManager.webauthn.FinishRegistration(user, *sessionData, r)
	if err != nil {
		if globalWebAuthnManager.log != nil {
			globalWebAuthnManager.log.Errorf("[WebAuthn注册] 完成 WebAuthn 注册失败: %v", err)
			globalWebAuthnManager.log.Errorf("[WebAuthn注册] 错误详情 - Username: %s, SessionKey: %s, DeviceName: %s",
				session.Username, req.SessionKey, req.DeviceName)
			globalWebAuthnManager.log.Errorf("[WebAuthn注册] Response 结构: id=%v, rawId类型=%T, response类型=%T",
				req.Response["id"], req.Response["rawId"], req.Response["response"])
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "注册失败: " + err.Error()})
		return
	}

	// 保存凭证到数据库
	if globalWebAuthnManager.log != nil {
		credentialID := base64.RawURLEncoding.EncodeToString(credential.ID)
		globalWebAuthnManager.log.Infof("[WebAuthn注册] 准备保存凭证 - Username: %s, DeviceName: %s, CredentialID: %s",
			session.Username, req.DeviceName, credentialID)
	}

	if err := globalWebAuthnManager.SaveCredential(session.Username, session.Username, credential, req.DeviceName); err != nil {
		if globalWebAuthnManager.log != nil {
			globalWebAuthnManager.log.Errorf("[WebAuthn注册] 保存凭证失败: %v", err)
			globalWebAuthnManager.log.Errorf("[WebAuthn注册] 保存失败详情 - Username: %s, DeviceName: %s",
				session.Username, req.DeviceName)
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存凭证失败: " + err.Error()})
		return
	}

	if globalWebAuthnManager.log != nil {
		credentialID := base64.RawURLEncoding.EncodeToString(credential.ID)
		globalWebAuthnManager.log.Infof("[WebAuthn注册] 凭证保存成功 - Username: %s, DeviceName: %s, CredentialID: %s",
			session.Username, req.DeviceName, credentialID)
	}

	// 删除临时会话数据
	deleteWebAuthnSession(req.SessionKey)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "WebAuthn 注册成功",
	})
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
