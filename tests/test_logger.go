//go:build ignore
// +build ignore

package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"quantmesh/logger"
	"quantmesh/web"
)

func main() {
	fmt.Println("====================================")
	fmt.Println("测试日志分离功能")
	fmt.Println("====================================")

	// 清理旧日志
	os.RemoveAll("logs")
	os.MkdirAll("logs", 0755)

	// 设置日志级别为 DEBUG (这样应用日志才会写入文件)
	logger.SetLevel(logger.DEBUG)

	// 初始化 Web 日志
	if err := logger.InitWebLogger(); err != nil {
		fmt.Printf("初始化 Web 日志失败: %v\n", err)
		return
	}

	// 写入一些应用日志
	logger.Info("应用启动")
	logger.Info("应用初始化完成")
	logger.Warn("这是一个警告")

	// 设置 Gin 为 Release 模式
	gin.SetMode(gin.ReleaseMode)

	// 创建 Gin 实例
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(web.GinLoggerMiddleware())

	// 添加测试路由
	r.GET("/ok", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})
	r.GET("/notfound", func(c *gin.Context) {
		c.JSON(404, gin.H{"error": "not found"})
	})
	r.GET("/error", func(c *gin.Context) {
		c.JSON(500, gin.H{"error": "internal server error"})
	})

	// 启动服务器（后台）
	go func() {
		if err := r.Run(":19999"); err != nil {
			fmt.Printf("启动服务器失败: %v\n", err)
		}
	}()

	// 等待服务器启动
	time.Sleep(2 * time.Second)

	// 发送测试请求
	fmt.Println("\n发送测试请求...")

	// 成功请求（不应记录到 Web 日志）
	fmt.Println("1. 发送成功请求 (200)...")
	resp, _ := http.Get("http://localhost:19999/ok")
	if resp != nil {
		io.ReadAll(resp.Body)
		resp.Body.Close()
	}

	// 404 请求（应该记录到 Web 日志）
	fmt.Println("2. 发送 404 请求...")
	resp, _ = http.Get("http://localhost:19999/notfound")
	if resp != nil {
		io.ReadAll(resp.Body)
		resp.Body.Close()
	}

	// 500 请求（应该记录到 Web 日志）
	fmt.Println("3. 发送 500 请求...")
	resp, _ = http.Get("http://localhost:19999/error")
	if resp != nil {
		io.ReadAll(resp.Body)
		resp.Body.Close()
	}

	// 写入更多应用日志
	logger.Info("请求处理完成")
	logger.Error("模拟一个错误日志")

	// 等待日志写入
	time.Sleep(1 * time.Second)

	// 关闭日志
	logger.Close()

	// 显示日志内容
	fmt.Println("\n====================================")
	fmt.Println("应用日志内容:")
	fmt.Println("====================================")
	content, _ := os.ReadFile(fmt.Sprintf("logs/app-quantmesh-%s.log", time.Now().Format("2006-01-02")))
	fmt.Println(string(content))

	fmt.Println("\n====================================")
	fmt.Println("Web 日志内容:")
	fmt.Println("====================================")
	content, _ = os.ReadFile(fmt.Sprintf("logs/web-gin-%s.log", time.Now().Format("2006-01-02")))
	if len(content) > 0 {
		fmt.Println(string(content))
	} else {
		fmt.Println("(无内容 - 可能是因为只记录错误请求)")
	}

	fmt.Println("\n====================================")
	fmt.Println("测试完成!")
	fmt.Println("====================================")
	fmt.Println("预期结果:")
	fmt.Println("  1. 应用日志包含: 应用启动、警告、错误等应用层日志")
	fmt.Println("  2. Web 日志只包含: 404 和 500 错误请求")
	fmt.Println("  3. 200 成功请求不应出现在 Web 日志中")
}

