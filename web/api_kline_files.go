package web

import (
	"net/http"
	"path/filepath"
	"strings"

	"quantmesh/monitor"

	"github.com/gin-gonic/gin"
)

var klineCollector *monitor.KlineCollector

// SetKlineCollector 设置K线收集器（从main.go注入）
func SetKlineCollector(collector *monitor.KlineCollector) {
	klineCollector = collector
}

// listKlineFilesHandler 列出所有K线数据文件
// GET /api/kline-files
func listKlineFilesHandler(c *gin.Context) {
	if klineCollector == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "K线收集器未初始化"})
		return
	}

	files, err := klineCollector.ListFiles()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"files":   files,
	})
}

// protectKlineFileHandler 保护K线文件
// POST /api/kline-files/:filename/protect
func protectKlineFileHandler(c *gin.Context) {
	filename := c.Param("filename")
	if filename == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "文件名不能为空"})
		return
	}

	storageProv := PickStorageProvider(c)
	if storageProv == nil || storageProv.GetStorage() == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "存储服务未就绪"})
		return
	}

	if err := storageProv.GetStorage().ProtectKlineFile(filename); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "文件已保护",
	})
}

// unprotectKlineFileHandler 取消保护K线文件
// DELETE /api/kline-files/:filename/protect
func unprotectKlineFileHandler(c *gin.Context) {
	filename := c.Param("filename")
	if filename == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "文件名不能为空"})
		return
	}

	storageProv := PickStorageProvider(c)
	if storageProv == nil || storageProv.GetStorage() == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "存储服务未就绪"})
		return
	}

	if err := storageProv.GetStorage().UnprotectKlineFile(filename); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "文件保护已取消",
	})
}

// downloadKlineFileHandler 下载K线文件
// GET /api/kline-files/:filename/download
func downloadKlineFileHandler(c *gin.Context) {
	filename := c.Param("filename")
	if filename == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "文件名不能为空"})
		return
	}

	// 安全检查：防止路径遍历攻击
	if strings.Contains(filename, "..") || strings.Contains(filename, "/") || strings.Contains(filename, "\\") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的文件名"})
		return
	}

	if klineCollector == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "K线收集器未初始化"})
		return
	}

	filepath := filepath.Join(klineCollector.GetDataDir(), filename)
	c.File(filepath)
}
