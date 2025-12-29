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

	r := gin.Default()

	// 添加 i18n 中间件
	r.Use(I18nMiddleware())

	// 设置路由
	SetupRoutes(r)

	// 配置服务器
	addr := fmt.Sprintf("%s:%d", cfg.Web.Host, cfg.Web.Port)
	server := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
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
		logger.Info("🌐 Web服务器启动在 http://%s:%d", ws.cfg.Web.Host, ws.cfg.Web.Port)
		if err := ws.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("❌ Web服务器启动失败: %v", err)
		}
	}()

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

