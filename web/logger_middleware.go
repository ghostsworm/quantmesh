package web

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"quantmesh/logger"
)

// GinLoggerMiddleware 自定义 Gin 日志中间件
// logAll=true 时全量输出；否则仅记录错误请求 (状态码 >= 400)
func GinLoggerMiddleware(logAll bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 记录请求开始时间
		start := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery

		// 处理请求
		c.Next()

		statusCode := c.Writer.Status()
		// 非 debug 模式只记录 4xx/5xx
		if !logAll && statusCode < 400 {
			return
		}

		// 计算请求处理时间
		latency := time.Since(start)
		
		// 获取客户端 IP
		clientIP := c.ClientIP()
		
		// 获取请求方法
		method := c.Request.Method
		
		// 拼接完整路径
		if raw != "" {
			path = path + "?" + raw
		}

		// 获取错误信息（如果有）
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

