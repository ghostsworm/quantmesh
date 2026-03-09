package web

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"quantmesh/agent"
	"quantmesh/agent/types"
	"quantmesh/logger"
)

// Agent upgrader for WebSocket connections
var agentUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // 生产环境需要验证 Origin
	},
}

var (
	agentManager     *AgentManager
	agentManagerOnce sync.Once
)

// AgentManager Agent 管理器
type AgentManager struct {
	sessions map[string]*agent.QuantMeshAgent
	mu        sync.RWMutex
	config    AgentManagerConfig
}

// AgentManagerConfig Agent 管理器配置
type AgentManagerConfig struct {
	LLMProvider string `json:"llm_provider"`
	LLMAPIKey   string `json:"llm_api_key"`
	LLMModel    string `json:"llm_model"`
}

// InitAgentManager 初始化 Agent 管理器
func InitAgentManager(config AgentManagerConfig) error {
	var err error
	agentManagerOnce.Do(func() {
		agentManager = &AgentManager{
			sessions: make(map[string]*agent.QuantMeshAgent),
			config:   config,
		}
		logger.Info("✅ Agent Manager 初始化完成")
	})
	return err
}

// GetAgentManager 获取 Agent 管理器
func GetAgentManager() *AgentManager {
	return agentManager
}

// createSession 创建会话
// POST /api/agent/sessions
func createSession(c *gin.Context) {
	if agentManager == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "AI_AGENT_NOT_CONFIGURED",
			"message": "AI 聊天功能未配置。请在配置文件的 web.ai 部分设置 llm_provider 和 llm_api_key",
		})
		return
	}

	var req struct {
		BotID   string                 `json:"bot_id"`
		Context map[string]interface{} `json:"context"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_request", err)
		return
	}

	// 创建 Agent 配置
	agentConfig := agent.AgentConfig{
		LLMProvider: agentManager.config.LLMProvider,
		LLMAPIKey:   agentManager.config.LLMAPIKey,
		LLMModel:    agentManager.config.LLMModel,
		MaxTokens:   4096,
		Temperature: 0.7,
	}

	// 创建 Agent
	quantmeshAgent, err := agent.NewQuantMeshAgent(agentConfig)
	if err != nil {
		logger.Error("创建 Agent 失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create agent"})
		return
	}

	// 生成会话 ID
	sessionID := generateSessionID()

	// 保存会话
	agentManager.mu.Lock()
	agentManager.sessions[sessionID] = quantmeshAgent
	agentManager.mu.Unlock()

	// 如果有 BotID，加载 Bot 配置作为上下文
	if req.BotID != "" {
		// 加载 Bot 配置并设置到 Agent 上下文
		context := loadBotContext(req.BotID)
		quantmeshAgent.SetState(types.ConversationState{
			Context: context,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"session_id": sessionID,
		"message":    "Session created successfully",
	})
}

// sendMessage 发送消息
// POST /api/agent/sessions/:id/messages
func sendMessage(c *gin.Context) {
	if agentManager == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "AI_AGENT_NOT_CONFIGURED",
			"message": "AI 聊天功能未配置。请在配置文件的 web.ai 部分设置 llm_provider 和 llm_api_key",
		})
		return
	}

	sessionID := c.Param("id")

	// 获取 Agent
	agentManager.mu.RLock()
	quantmeshAgent, ok := agentManager.sessions[sessionID]
	agentManager.mu.RUnlock()

	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Session not found"})
		return
	}

	var req struct {
		Content string                 `json:"content" binding:"required"`
		Stream   bool                   `json:"stream"`
		Images   []types.ImageData      `json:"images"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_request", err)
		return
	}

	// 创建用户消息
	msg := types.Message{
		Role:    "user",
		Content: req.Content,
	}

	// 如果有图片，使用多模态生成
	var response types.Response
	var err error

	if len(req.Images) > 0 {
		// 使用多模态 API（仅 Gemini 支持）
		llmClient := quantmeshAgent.GetLLMClient()
		generateReq := types.GenerateRequest{
			Messages:     []types.LLMMessage{{Role: "user", Content: req.Content}},
			MaxTokens:    4096,
			Temperature:  0.7,
		}

		llmResp, err := llmClient.GenerateWithImage(c.Request.Context(), req.Content, req.Images, generateReq)
		if err != nil {
			logger.Error("多模态生成失败: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process message with images"})
			return
		}

		// 转换 LLM 响应为 Agent 响应
		response = types.Response{
			Message:   llmResp.Message,
			ToolCalls: llmResp.ToolCalls,
			NeedsMore: false,
			Images:    llmResp.Images,
			Files:     llmResp.Files,
		}
	} else {
		// 普通文本处理
		response, err = quantmeshAgent.ProcessMessage(c.Request.Context(), msg)
	}

	if err != nil {
		logger.Error("处理消息失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process message"})
		return
	}

	// 构建响应
	resp := gin.H{
		"message":     response.Message,
		"tool_calls":  response.ToolCalls,
		"needs_more":  response.NeedsMore,
		"suggestions": response.Suggestions,
		"images":      response.Images,
		"files":       response.Files,
	}

	// 获取当前配置状态
	if state := quantmeshAgent.GetState(); state.CurrentConfig != nil {
		resp["config_preview"] = state.CurrentConfig
	}

	c.JSON(http.StatusOK, resp)
}

// listSessions 列出所有会话
// GET /api/agent/sessions
func listSessions(c *gin.Context) {
	if agentManager == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "AI_AGENT_NOT_CONFIGURED",
			"message": "AI 聊天功能未配置。请在配置文件的 web.ai 部分设置 llm_provider 和 llm_api_key",
		})
		return
	}

	agentManager.mu.RLock()
	defer agentManager.mu.RUnlock()

	sessions := make([]gin.H, 0, len(agentManager.sessions))
	for id, agent := range agentManager.sessions {
		state := agent.GetState()
		sessions = append(sessions, gin.H{
			"id":         id,
			"created_at": state.CreatedAt,
			"updated_at": state.UpdatedAt,
			"message_count": len(state.Messages),
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"sessions": sessions,
	})
}

// getSessionHistory 获取会话历史
// GET /api/agent/sessions/:id/history
func getSessionHistory(c *gin.Context) {
	if agentManager == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "AI_AGENT_NOT_CONFIGURED",
			"message": "AI 聊天功能未配置。请在配置文件的 web.ai 部分设置 llm_provider 和 llm_api_key",
		})
		return
	}

	sessionID := c.Param("id")

	agentManager.mu.RLock()
	quantmeshAgent, ok := agentManager.sessions[sessionID]
	agentManager.mu.RUnlock()

	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Session not found"})
		return
	}

	state := quantmeshAgent.GetState()

	c.JSON(http.StatusOK, gin.H{
		"session_id": sessionID,
		"messages":   state.Messages,
		"created_at": state.CreatedAt,
		"updated_at": state.UpdatedAt,
	})
}

// getSessionConfig 获取会话配置
// GET /api/agent/sessions/:id/config
func getSessionConfig(c *gin.Context) {
	if agentManager == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "AI_AGENT_NOT_CONFIGURED",
			"message": "AI 聊天功能未配置。请在配置文件的 web.ai 部分设置 llm_provider 和 llm_api_key",
		})
		return
	}

	sessionID := c.Param("id")

	agentManager.mu.RLock()
	quantmeshAgent, ok := agentManager.sessions[sessionID]
	agentManager.mu.RUnlock()

	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Session not found"})
		return
	}

	state := quantmeshAgent.GetState()

	c.JSON(http.StatusOK, gin.H{
		"config": state.CurrentConfig,
		"todos":  state.TODOList,
	})
}

// applyConfig 应用配置
// POST /api/agent/sessions/:id/apply
func applyConfig(c *gin.Context) {
	if agentManager == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "AI_AGENT_NOT_CONFIGURED",
			"message": "AI 聊天功能未配置。请在配置文件的 web.ai 部分设置 llm_provider 和 llm_api_key",
		})
		return
	}

	sessionID := c.Param("id")

	agentManager.mu.RLock()
	quantmeshAgent, ok := agentManager.sessions[sessionID]
	agentManager.mu.RUnlock()

	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Session not found"})
		return
	}

	state := quantmeshAgent.GetState()

	// 这里应该将配置应用到实际的 Bot
	// 暂时返回成功
	logger.Info("应用配置: %+v", state.CurrentConfig)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Configuration applied successfully",
	})
}

// deleteSession 删除会话
// DELETE /api/agent/sessions/:id
func deleteSession(c *gin.Context) {
	if agentManager == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "AI_AGENT_NOT_CONFIGURED",
			"message": "AI 聊天功能未配置。请在配置文件的 web.ai 部分设置 llm_provider 和 llm_api_key",
		})
		return
	}

	sessionID := c.Param("id")

	agentManager.mu.Lock()
	delete(agentManager.sessions, sessionID)
	agentManager.mu.Unlock()

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Session deleted successfully",
	})
}

// handleAgentWebSocket Agent WebSocket 处理
// GET /api/agent/sessions/:id/ws
func handleAgentWebSocket(c *gin.Context) {
	if agentManager == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "AI_AGENT_NOT_CONFIGURED",
			"message": "AI 聊天功能未配置。请在配置文件的 web.ai 部分设置 llm_provider 和 llm_api_key",
		})
		return
	}

	sessionID := c.Param("id")

	conn, err := agentUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.Error("WebSocket 升级失败: %v", err)
		return
	}
	defer conn.Close()

	// 获取 Agent
	agentManager.mu.RLock()
	quantmeshAgent, ok := agentManager.sessions[sessionID]
	agentManager.mu.RUnlock()

	if !ok {
		conn.WriteJSON(gin.H{"error": "Session not found"})
		return
	}

	// 处理 WebSocket 消息
	for {
		var msg struct {
			Type    string                 `json:"type"`
			Content string                 `json:"content"`
			Data    map[string]interface{} `json:"data"`
		}

		if err := conn.ReadJSON(&msg); err != nil {
			logger.Error("读取 WebSocket 消息失败: %v", err)
			break
		}

		switch msg.Type {
		case "message":
			// 处理用户消息
			response, err := quantmeshAgent.ProcessMessage(c.Request.Context(), types.Message{
				Role:    "user",
				Content: msg.Content,
			})

			if err != nil {
				conn.WriteJSON(gin.H{
					"type":  "error",
					"error": err.Error(),
				})
				continue
			}

			// 发送响应
			conn.WriteJSON(gin.H{
				"type":     "response",
				"message":  response.Message,
				"tool_calls": response.ToolCalls,
			})

		case "ping":
			conn.WriteJSON(gin.H{"type": "pong"})
		}
	}
}

// 辅助函数
func generateSessionID() string {
	return fmt.Sprintf("session_%d", time.Now().UnixNano())
}

func loadBotContext(botID string) map[string]interface{} {
	// 加载 Bot 配置并转换为上下文
	// 这里需要调用实际的 Bot 配置加载逻辑
	return map[string]interface{}{
		"bot_id": botID,
	}
}
