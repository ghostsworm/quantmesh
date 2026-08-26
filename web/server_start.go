package web

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"quantmesh/config"
	"quantmesh/logger"
)

// WebServer Web服務器
type WebServer struct {
	server *http.Server
	cfg    *config.Config
}

// NewWebServer 創建Web服務器
func NewWebServer(cfg *config.Config) *WebServer {
	if !cfg.Web.Enabled {
		return nil
	}

	// 設置Gin模式
	if cfg.System.LogLevel == "debug" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	// 初始化 Web 日志文件
	if err := logger.InitWebLogger(); err != nil {
		logger.Warn("⚠️ 初始化 Web 日志文件失败: %v，Web 请求日志將不會被記錄", err)
	}

	// 使用 gin.New() 代替 gin.Default()，手动添加中间件
	r := gin.New()
	
	// 添加 Recovery 中间件（panic 恢複 + 可观测性上报）
	r.Use(GinRecoveryMiddleware())
	
	// 添加自定义日志中间件
	// debug 模式输出全量请求日志；非 debug 僅記錄异常
	r.Use(GinLoggerMiddleware(cfg.System.LogLevel == "debug"))

	// 添加 i18n 中间件
	r.Use(I18nMiddleware())

	// 初始化 AI Agent Manager
	// 优先使用 web.ai 配置，如果为空则从 ai.gemini_api_key 读取
	llmProvider := cfg.Web.AI.LLMProvider
	llmAPIKey := cfg.Web.AI.LLMAPIKey
	llmModel := cfg.Web.AI.LLMModel

	// 如果 web.ai 未配置，尝试使用全局 AI（含 ai.upstreams / default_upstream）
	if llmAPIKey == "" {
		r := config.ResolveGlobalAI(cfg)
		if r.Provider == "gemini" && r.APIKey != "" {
			llmProvider = "gemini"
			llmAPIKey = r.APIKey
			if llmModel == "" {
				llmModel = "gemini-2.5-flash"
			}
			logger.Info("🔄 AI Agent 使用全局 AI 配置（gemini）")
		}
	}

	if llmAPIKey != "" && llmProvider != "" {
		err := InitAgentManager(AgentManagerConfig{
			LLMProvider: llmProvider,
			LLMAPIKey:   llmAPIKey,
			LLMModel:    llmModel,
		})
		if err != nil {
			logger.Warn("⚠️ AI Agent Manager 初始化失败: %v", err)
		} else {
			logger.Info("✅ AI Agent Manager 初始化成功")
		}
	} else {
		logger.Info("ℹ️ AI Agent 未配置（需要在 web.ai 中设置 llm_provider 和 llm_api_key，或在 ai 中设置 gemini_api_key）")
	}

	// 設置路由（傳入配置以便 pprof 可以讀取配置）
	SetupRoutesWithConfig(r, cfg)

	// 配置服務器
	// 注意：AI 生成配置等长時间操作需要较长的超時時间
	addr := fmt.Sprintf("%s:%d", cfg.Web.Host, cfg.Web.Port)
	server := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  120 * time.Second, // 2 分钟读取超時
		WriteTimeout: 180 * time.Second, // 3 分钟写入超時（AI 请求可能需要较长時间）
		IdleTimeout:  120 * time.Second,
	}

	return &WebServer{
		server: server,
		cfg:    cfg,
	}
}

// Start 啟动Web服務器
func (ws *WebServer) Start(ctx context.Context) error {
	if ws == nil {
		return nil
	}

	go func() {
		logger.Info("🌐 Web服務器正在啟动，監听地址: http://%s:%d", ws.cfg.Web.Host, ws.cfg.Web.Port)
		if err := ws.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("❌ Web服務器啟动失败: %v", err)
		}
	}()
	// 给 goroutine 一点時间啟动，确保日志能输出
	time.Sleep(100 * time.Millisecond)

	// 等待context取消
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := ws.server.Shutdown(shutdownCtx); err != nil {
			logger.Error("❌ Web服務器关闭失败: %v", err)
		} else {
			logger.Info("✅ Web服務器已关闭")
		}
	}()

	return nil
}

// Stop 停止Web服務器
func (ws *WebServer) Stop() {
	if ws == nil || ws.server == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := ws.server.Shutdown(ctx); err != nil {
		logger.Error("❌ Web服務器关闭失败: %v", err)
	}
}
