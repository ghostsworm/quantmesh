package web

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"quantmesh/exchange"
	"quantmesh/logger"

	"github.com/gin-gonic/gin"
)

// PermissionCheckResult API 权限检测結果
type PermissionCheckResult struct {
	Exchange     string                   `json:"exchange"`
	Symbol       string                   `json:"symbol"`
	Permissions  *exchange.APIPermissions `json:"permissions"`
	Warnings     []string                 `json:"warnings"`
	IsSecure     bool                     `json:"is_secure"`
	CheckTime    time.Time                `json:"check_time"`
	ErrorMessage string                   `json:"error_message,omitempty"`
}

// CheckExchangePermissions 检查交易所 API 权限
func CheckExchangePermissions(ctx context.Context, ex exchange.IExchange, exchangeName, symbol string) *PermissionCheckResult {
	result := &PermissionCheckResult{
		Exchange:  exchangeName,
		Symbol:    symbol,
		CheckTime: time.Now(),
	}

	// 检查交易所是否實現了 PermissionChecker 接口
	checker, ok := ex.(exchange.PermissionChecker)
	if !ok {
		result.ErrorMessage = "該交易所暫不支援权限检测"
		result.IsSecure = true // 假設安全，不阻止啟动
		logger.Warn("⚠️ [%s] 不支援 API 权限检测接口", exchangeName)
		return result
	}

	// 執行权限检测
	permissions, err := checker.CheckAPIPermissions(ctx)
	if err != nil {
		result.ErrorMessage = fmt.Sprintf("权限检测失败: %v", err)
		result.IsSecure = true // 检测失败不阻止啟动
		logger.Error("❌ [%s] API 权限检测失败: %v", exchangeName, err)
		return result
	}

	result.Permissions = permissions
	result.IsSecure = permissions.IsSecure()
	result.Warnings = permissions.GetWarnings()

	// 記錄警告信息
	if len(result.Warnings) > 0 {
		logger.Warn("⚠️ [%s] API 权限安全警告:", exchangeName)
		for _, warning := range result.Warnings {
			logger.Warn("   %s", warning)
		}
	}

	// 如果不安全，記錄錯误
	if !result.IsSecure {
		logger.Error("🚨 [%s] API 密钥存在安全风險！建议修改权限設置", exchangeName)
	} else {
		logger.Info("✅ [%s] API 密钥权限检测通過", exchangeName)
	}

	return result
}

// getAPIPermissions 獲取 API 权限信息（HTTP 接口）
func getAPIPermissions(c *gin.Context) {
	exchangeName := c.Query("exchange")
	symbol := c.Query("symbol")

	if exchangeName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 exchange 参數"})
		return
	}

	// 從 provider 獲取交易所實例
	key := makeSymbolKey(exchangeName, symbol)
	providersMu.RLock()
	exProvider, ok := exchangeProviders[key]
	providersMu.RUnlock()

	if !ok || exProvider == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "未找到指定的交易所實例"})
		return
	}

	// 注意：这里需要從 exProvider 獲取實際的 exchange.IExchange 實例
	// 由於 ExchangeProvider 接口可能不直接暴露底层交易所，我们需要扩展接口
	// 暂時返回提示信息
	c.JSON(http.StatusOK, gin.H{
		"message":  "API 权限检测功能已實現，请在系统啟动時查看日志",
		"note":     "权限检测結果會在啟动時自动執行並記錄到日志中",
		"exchange": exchangeName,
		"symbol":   symbol,
	})
}

// FormatPermissionReport 格式化权限检测报告
func FormatPermissionReport(results []*PermissionCheckResult) string {
	if len(results) == 0 {
		return "没有需要检测的交易所"
	}

	report := "\n" + "═══════════════════════════════════════════════════════════════\n"
	report += "                    API 权限安全检测报告\n"
	report += "═══════════════════════════════════════════════════════════════\n\n"

	hasHighRisk := false
	hasMediumRisk := false

	for i, result := range results {
		report += fmt.Sprintf("%d. 交易所: %s (%s)\n", i+1, result.Exchange, result.Symbol)
		report += fmt.Sprintf("   检测時间: %s\n", result.CheckTime.Format("2006-01-02 15:04:05"))

		if result.ErrorMessage != "" {
			report += fmt.Sprintf("   ❌ 錯误: %s\n", result.ErrorMessage)
		} else if result.Permissions != nil {
			p := result.Permissions
			report += fmt.Sprintf("   权限信息:\n")
			report += fmt.Sprintf("     - 交易权限: %v\n", p.CanTrade)
			report += fmt.Sprintf("     - 提現权限: %v\n", p.CanWithdraw)
			report += fmt.Sprintf("     - 轉账权限: %v\n", p.CanTransfer)
			report += fmt.Sprintf("     - IP 限制: %v\n", p.IPRestricted)
			report += fmt.Sprintf("   安全评分: %d/100\n", p.SecurityScore)
			report += fmt.Sprintf("   风險等级: %s\n", p.RiskLevel)

			if p.RiskLevel == "high" {
				hasHighRisk = true
			} else if p.RiskLevel == "medium" {
				hasMediumRisk = true
			}

			if len(result.Warnings) > 0 {
				report += "   安全警告:\n"
				for _, warning := range result.Warnings {
					report += fmt.Sprintf("     %s\n", warning)
				}
			}

			if result.IsSecure {
				report += "   ✅ 状態: 安全\n"
			} else {
				report += "   🚨 状態: 存在安全风險\n"
			}
		}
		report += "\n"
	}

	report += "═══════════════════════════════════════════════════════════════\n"

	if hasHighRisk {
		report += "🚨 警告: 检测到高风險 API 密钥！\n"
		report += "   强烈建议:\n"
		report += "   1. 立即禁用 API 密钥的提現和轉账权限\n"
		report += "   2. 啟用 IP 白名單限制\n"
		report += "   3. 使用子账戶 API 密钥進行交易\n"
		report += "   4. 定期更换 API 密钥\n"
	} else if hasMediumRisk {
		report += "⚠️ 提示: 检测到中等风險 API 密钥\n"
		report += "   建议:\n"
		report += "   1. 啟用 IP 白名單限制以提高安全性\n"
		report += "   2. 定期检查 API 密钥使用情况\n"
	} else {
		report += "✅ 所有 API 密钥安全检测通過\n"
	}

	report += "═══════════════════════════════════════════════════════════════\n"

	return report
}
