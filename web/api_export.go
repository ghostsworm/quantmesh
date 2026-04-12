package web

import (
	"archive/zip"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"quantmesh/config"
	"quantmesh/storage"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
)

// parseExportParams 解析導出通用参數
func parseExportParams(c *gin.Context) storage.ExportParams {
	format := storage.ExportFormat(strings.ToLower(c.DefaultQuery("format", "json")))
	if format != storage.ExportFormatCSV && format != storage.ExportFormatJSON {
		format = storage.ExportFormatJSON
	}

	var startTime, endTime time.Time
	if s := c.Query("start_time"); s != "" {
		startTime, _ = time.Parse(time.RFC3339, s)
	}
	if s := c.Query("end_time"); s != "" {
		endTime, _ = time.Parse(time.RFC3339, s)
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10000"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	account := GetCurrentAccountID()
	if a := c.Query("account"); a != "" {
		account = a
	}

	return storage.ExportParams{
		Format:    format,
		StartTime: startTime,
		EndTime:   endTime,
		BotID:     c.Query("bot_id"),
		Exchange:  c.Query("exchange"),
		Symbol:    c.Query("symbol"),
		Account:   account,
		Limit:     limit,
		Offset:    offset,
	}
}

// serveExport 設置下載响应头並返回數據
func serveExport(c *gin.Context, data []byte, contentType, filename string) {
	c.Header("Content-Disposition", `attachment; filename="`+filename+`"`)
	c.Data(http.StatusOK, contentType, data)
}

// exportConfigHandler 下載當前配置（脱敏）
// GET /api/export/config
func exportConfigHandler(c *gin.Context) {
	if configManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "配置管理器未初始化"})
		return
	}

	if globalConfig == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "配置未初始化"})
		return
	}

	cfg := globalConfig
	if cfg == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "配置无效"})
		return
	}

	sanitized := config.SanitizeForExport(cfg)
	data, err := yaml.Marshal(sanitized)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "序列化配置失败"})
		return
	}

	filename := "config_" + time.Now().Format("20060102_150405") + ".yaml"
	serveExport(c, data, "application/x-yaml", filename)
}

// exportTradesHandler 導出交易历史
// GET /api/export/trades
func exportTradesHandler(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil || storageProv.GetStorage() == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "存儲服務未就绪"})
		return
	}

	params := parseExportParams(c)
	exporter := storage.NewExporter(storageProv.GetStorage())

	data, contentType, err := exporter.ExportTrades(params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ext := ".json"
	if params.Format == storage.ExportFormatCSV {
		ext = ".csv"
	}
	filename := "trades_" + time.Now().Format("20060102_150405") + ext
	serveExport(c, data, contentType, filename)
}

// exportOrdersHandler 導出訂單歷史
// GET /api/export/orders
func exportOrdersHandler(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil || storageProv.GetStorage() == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "存儲服務未就绪"})
		return
	}

	params := parseExportParams(c)
	exporter := storage.NewExporter(storageProv.GetStorage())

	data, contentType, err := exporter.ExportOrders(params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ext := ".json"
	if params.Format == storage.ExportFormatCSV {
		ext = ".csv"
	}
	filename := "orders_" + time.Now().Format("20060102_150405") + ext
	serveExport(c, data, contentType, filename)
}

// exportPositionsHandler 導出持倉歷史
// GET /api/export/positions
func exportPositionsHandler(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil || storageProv.GetStorage() == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "存儲服務未就绪"})
		return
	}

	params := parseExportParams(c)
	exporter := storage.NewExporter(storageProv.GetStorage())

	data, contentType, err := exporter.ExportPositions(params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ext := ".json"
	if params.Format == storage.ExportFormatCSV {
		ext = ".csv"
	}
	filename := "positions_" + time.Now().Format("20060102_150405") + ext
	serveExport(c, data, contentType, filename)
}

// exportStatisticsHandler 導出统计數據
// GET /api/export/statistics
func exportStatisticsHandler(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil || storageProv.GetStorage() == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "存儲服務未就绪"})
		return
	}

	params := parseExportParams(c)
	exporter := storage.NewExporter(storageProv.GetStorage())

	data, contentType, err := exporter.ExportStatistics(params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ext := ".json"
	if params.Format == storage.ExportFormatCSV {
		ext = ".csv"
	}
	filename := "statistics_" + time.Now().Format("20060102_150405") + ext
	serveExport(c, data, contentType, filename)
}

// exportReconciliationHandler 導出對账历史
// GET /api/export/reconciliation
func exportReconciliationHandler(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil || storageProv.GetStorage() == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "存儲服務未就绪"})
		return
	}

	params := parseExportParams(c)
	exporter := storage.NewExporter(storageProv.GetStorage())

	data, contentType, err := exporter.ExportReconciliation(params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ext := ".json"
	if params.Format == storage.ExportFormatCSV {
		ext = ".csv"
	}
	filename := "reconciliation_" + time.Now().Format("20060102_150405") + ext
	serveExport(c, data, contentType, filename)
}

// exportRiskChecksHandler 導出风控检查历史
// GET /api/export/risk-checks
func exportRiskChecksHandler(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil || storageProv.GetStorage() == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "存儲服務未就绪"})
		return
	}

	params := parseExportParams(c)
	exporter := storage.NewExporter(storageProv.GetStorage())

	data, contentType, err := exporter.ExportRiskChecks(params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ext := ".json"
	if params.Format == storage.ExportFormatCSV {
		ext = ".csv"
	}
	filename := "risk_checks_" + time.Now().Format("20060102_150405") + ext
	serveExport(c, data, contentType, filename)
}

// exportSystemMetricsHandler 導出系统監控數據
// GET /api/export/system-metrics
func exportSystemMetricsHandler(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil || storageProv.GetStorage() == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "存儲服務未就绪"})
		return
	}

	params := parseExportParams(c)
	exporter := storage.NewExporter(storageProv.GetStorage())

	data, contentType, err := exporter.ExportSystemMetrics(params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ext := ".json"
	if params.Format == storage.ExportFormatCSV {
		ext = ".csv"
	}
	filename := "system_metrics_" + time.Now().Format("20060102_150405") + ext
	serveExport(c, data, contentType, filename)
}

// exportLogsHandler 導出应用日志
// GET /api/export/logs
func exportLogsHandler(c *gin.Context) {
	if logStorageProvider == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "日志存儲未初始化"})
		return
	}

	params := parseExportParams(c)
	format := strings.ToLower(c.DefaultQuery("format", "json"))

	var startTime, endTime time.Time
	if !params.StartTime.IsZero() {
		startTime = params.StartTime
	} else {
		startTime = time.Now().AddDate(0, 0, -7)
	}
	if !params.EndTime.IsZero() {
		endTime = params.EndTime
	} else {
		endTime = time.Now()
	}

	logs, _, err := logStorageProvider.GetLogs(storage.LogQueryParams{
		StartTime: startTime,
		EndTime:   endTime,
		Limit:     params.Limit,
		Offset:    params.Offset,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if format == "csv" {
		// 简單 CSV 格式
		var sb strings.Builder
		sb.WriteString("id,timestamp,level,message\n")
		for _, log := range logs {
			sb.WriteString(strconv.FormatInt(log.ID, 10) + ",")
			sb.WriteString(log.Timestamp.Format(time.RFC3339) + ",")
			sb.WriteString(log.Level + ",")
			sb.WriteString(`"` + strings.ReplaceAll(log.Message, `"`, `""`) + `"` + "\n")
		}
		filename := "logs_" + time.Now().Format("20060102_150405") + ".csv"
		serveExport(c, []byte(sb.String()), "text/csv", filename)
	} else {
		// JSON
		data, _ := jsonMarshalIndent(logs)
		filename := "logs_" + time.Now().Format("20060102_150405") + ".json"
		serveExport(c, data, "application/json", filename)
	}
}

// exportAuditLogsHandler 導出审计日志（合规交易审计 CSV/JSONL 文件）
// GET /api/export/audit-logs
// 從 data/audit 目錄读取並按日期範圍打包
func exportAuditLogsHandler(c *gin.Context) {
	params := parseExportParams(c)
	auditDir := "./data/audit"
	if globalConfig != nil {
		cfg := globalConfig
		if cfg != nil && cfg.Compliance.AuditLog.Directory != "" {
			auditDir = cfg.Compliance.AuditLog.Directory
		}
	}

	params.AuditDir = auditDir
	exporter := storage.NewExporter(nil) // 僅用於打包审计文件，不需要 storage

	zipData, err := exporter.CreateExportZip(params, nil, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	filename := "audit_logs_" + time.Now().Format("20060102_150405") + ".zip"
	serveExport(c, zipData, "application/zip", filename)
}

// exportAllHandler 導出全部數據（ZIP 打包）
// GET /api/export/all
func exportAllHandler(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil || storageProv.GetStorage() == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "存儲服務未就绪"})
		return
	}

	params := parseExportParams(c)
	auditDir := "./data/audit"
	configContent := []byte{}

	if globalConfig != nil {
		cfg := globalConfig
		if cfg != nil {
			sanitized := config.SanitizeForExport(cfg)
			configContent, _ = yaml.Marshal(sanitized)
		}
		if cfg != nil && cfg.Compliance.AuditLog.Directory != "" {
			auditDir = cfg.Compliance.AuditLog.Directory
		}
	}
	params.AuditDir = auditDir

	// 应用日志
	var logRecords interface{}
	if logStorageProvider != nil {
		startTime := params.StartTime
		endTime := params.EndTime
		if startTime.IsZero() {
			startTime = time.Now().AddDate(0, 0, -30)
		}
		if endTime.IsZero() {
			endTime = time.Now()
		}
		logs, _, _ := logStorageProvider.GetLogs(storage.LogQueryParams{
			StartTime: startTime,
			EndTime:   endTime,
			Limit:     5000,
			Offset:    0,
		})
		if len(logs) > 0 {
			logRecords = logs
		}
	}

	exporter := storage.NewExporter(storageProv.GetStorage())
	zipData, err := exporter.CreateExportZip(params, logRecords, configContent)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	filename := "quantmesh_export_" + time.Now().Format("20060102_150405") + ".zip"
	serveExport(c, zipData, "application/zip", filename)
}

// exportBacktestReportsHandler 導出回測報告（ZIP 打包）
// GET /api/export/backtest-reports
func exportBacktestReportsHandler(c *gin.Context) {
	reportsDir := "./backtest/reports"

	// 檢查目錄是否存在
	if _, err := os.Stat(reportsDir); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "回測報告目錄不存在"})
		return
	}

	// 創建臨時ZIP文件
	tempFile, err := os.CreateTemp("", "backtest_reports_*.zip")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "創建臨時文件失败"})
		return
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	// 打包所有報告文件
	zipWriter := zip.NewWriter(tempFile)
	defer zipWriter.Close()

	files, err := os.ReadDir(reportsDir)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "讀取報告目錄失败"})
		return
	}

	fileCount := 0
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		// 只打包 .md 和 .csv 文件
		filename := file.Name()
		if !strings.HasSuffix(filename, ".md") && !strings.HasSuffix(filename, ".csv") {
			continue
		}

		filePath := filepath.Join(reportsDir, filename)
		fileData, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		zipFile, err := zipWriter.Create(filename)
		if err != nil {
			continue
		}

		if _, err := zipFile.Write(fileData); err != nil {
			continue
		}

		fileCount++
	}

	if fileCount == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "沒有找到回測報告文件"})
		return
	}

	zipWriter.Close()
	tempFile.Close()

	// 讀取ZIP文件內容
	zipData, err := os.ReadFile(tempFile.Name())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "讀取ZIP文件失败"})
		return
	}

	filename := "backtest_reports_" + time.Now().Format("20060102_150405") + ".zip"
	serveExport(c, zipData, "application/zip", filename)
}

// jsonMarshalIndent 序列化為 JSON
func jsonMarshalIndent(v interface{}) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}
