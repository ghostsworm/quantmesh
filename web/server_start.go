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

// WebServer Web服务器
type WebServer struct {
	server *http.Server
	cfg    *config.Config
}

// NewWebServer 创建Web服务器
func NewWebServer(cfg *config.Config) *WebServer {
	if !cfg.Web.Enabled {
		return nil
	}

	// 设置Gin模式
	if cfg.System.LogLevel == "debug" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	// 初始化 Web 日志文件
	if err := logger.InitWebLogger(); err != nil {
		logger.Warn("⚠️ 初始化 Web 日志文件失败: %v，Web 请求日志将不会被记录", err)
	}

	// 使用 gin.New() 代替 gin.Default()，手动添加中间件
	r := gin.New()
	
	// 添加 Recovery 中间件（panic 恢复）
	r.Use(gin.Recovery())
	
	// 添加自定义日志中间件
	// debug 模式输出全量请求日志；非 debug 仅记录异常
	r.Use(GinLoggerMiddleware(cfg.System.LogLevel == "debug"))

	// 添加 i18n 中间件
	r.Use(I18nMiddleware())

	// 设置路由
	SetupRoutes(r)

	// 配置服务器
	// 注意：AI 生成配置等长时间操作需要较长的超时时间
	addr := fmt.Sprintf("%s:%d", cfg.Web.Host, cfg.Web.Port)
	server := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  120 * time.Second, // 2 分钟读取超时
		WriteTimeout: 180 * time.Second, // 3 分钟写入超时（AI 请求可能需要较长时间）
		IdleTimeout:  120 * time.Second,
	}

	return &WebServer{
		server: server,
		cfg:    cfg,
	}
}

// Start 启动Web服务器
func (ws *WebServer) Start(ctx context.Context) error {
	if ws == nil {
		return nil
	}

	go func() {
		logger.Info("🌐 Web服务器正在启动，监听地址: http://%s:%d", ws.cfg.Web.Host, ws.cfg.Web.Port)
		if err := ws.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("❌ Web服务器启动失败: %v", err)
		}
	}()
	// 给 goroutine 一点时间启动，确保日志能输出
	time.Sleep(100 * time.Millisecond)

	// 等待context取消
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := ws.server.Shutdown(shutdownCtx); err != nil {
			logger.Error("❌ Web服务器关闭失败: %v", err)
		} else {
			logger.Info("✅ Web服务器已关闭")
		}
	}()

	return nil
}

// Stop 停止Web服务器
func (ws *WebServer) Stop() {
	if ws == nil || ws.server == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := ws.server.Shutdown(ctx); err != nil {
		logger.Error("❌ Web服务器关闭失败: %v", err)
	}
}
