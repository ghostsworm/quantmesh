package web

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"quantmesh/logger"
)

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
		// 非 debug 模式只記錄 4xx/5xx
		if !logAll && statusCode < 400 {
			return
		}

		// 计算请求处理時间
		latency := time.Since(start)
		
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
	}
}

