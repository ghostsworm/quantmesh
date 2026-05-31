package web

import (
	"errors"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/gin-gonic/gin"
	"quantmesh/logger"
	"quantmesh/notify/aipipe"
	"quantmesh/notify/observability"
)

// slowHTTPRequestThreshold 超過此耗時的請求額外打 [GIN_SLOW]（對應瀏覽器里「等待服務器響應」過長時便於對照 journal / web 日志）
const slowHTTPRequestThreshold = 2 * time.Second

// GinLoggerMiddleware 自定义 Gin 日志中间件
// logAll=true 時全量输出；否则僅記錄錯误请求 (状態碼 >= 400)
func GinLoggerMiddleware(logAll bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 記錄请求开始時间
		start := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery

		// 处理请求
		c.Next()

		statusCode := c.Writer.Status()
		latency := time.Since(start)

		// 慢請求：無論是否 debug，均記錄（便於排查 TTFB 與後端處理時間）
		if latency >= slowHTTPRequestThreshold {
			fullPath := path
			if raw != "" {
				fullPath = path + "?" + raw
			}
			slowMsg := fmt.Sprintf("[GIN_SLOW] %d | %v | %s | %-7s %s",
				statusCode, latency, c.ClientIP(), c.Request.Method, fullPath)
			logger.Warn("%s", slowMsg)
			logger.WriteWebLog(slowMsg)
		}

		// 非 debug 模式只記錄 4xx/5xx（快請求且 2xx 則跳過）
		if !logAll && statusCode < 400 {
			return
		}

		// 獲取客戶端 IP
		clientIP := c.ClientIP()

		// 獲取请求方法
		method := c.Request.Method

		// 拼接完整路径
		if raw != "" {
			path = path + "?" + raw
		}

		// 獲取錯误信息（如果有）
		errorMessage := c.Errors.ByType(gin.ErrorTypePrivate).String()

		// 格式化日志消息
		var logMessage string
		if errorMessage != "" {
			logMessage = fmt.Sprintf("[GIN] %d | %v | %s | %-7s %s | Error: %s",
				statusCode,
				latency,
				clientIP,
				method,
				path,
				errorMessage,
			)
		} else {
			logMessage = fmt.Sprintf("[GIN] %d | %v | %s | %-7s %s",
				statusCode,
				latency,
				clientIP,
				method,
				path,
			)
		}

		// 写入 Web 日志文件
		logger.WriteWebLog(logMessage)

		// 5xx 上报 aipipe（4xx 通常是用户输入问题，不上报避免噪音）
		if statusCode >= 500 {
			err := fmt.Errorf("HTTP %d %s %s", statusCode, method, path)
			extra := fmt.Sprintf("client_ip=%s latency=%v", clientIP, latency)
			aipipe.ReportError(err, "http5xx", extra)
			observability.ReportError(err, "http5xx", extra)
		}
	}
}

// GinRecoveryMiddleware 捕获 handler panic，记录日志并上报，然后返回 500。
// 必须挂在所有业务路由之前。
func GinRecoveryMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				stack := string(debug.Stack())
				msg := fmt.Sprintf("panic in %s %s: %v", c.Request.Method, c.Request.URL.Path, r)
				logger.Error("%s\n%s", msg, stack)
				aipipe.ReportError(errors.New(msg), "panic", stack)
				observability.ReportError(errors.New(msg), "panic", stack)
				if !c.Writer.Written() {
					c.AbortWithStatusJSON(500, gin.H{"error": "internal server error"})
				} else {
					c.Abort()
				}
			}
		}()
		c.Next()
	}
}
