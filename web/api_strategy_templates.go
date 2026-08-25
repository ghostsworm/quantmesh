package web

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"quantmesh/config"
	"quantmesh/logger"
)

var strategyTemplateManager *config.StrategyTemplateManager

// InitStrategyTemplateManager 初始化策略模板管理器
func InitStrategyTemplateManager(baseDir string) error {
	var err error
	strategyTemplateManager, err = config.NewStrategyTemplateManager(baseDir)
	if err != nil {
		return err
	}
	logger.Info("✅ 策略模板管理器已初始化")
	return nil
}

// getStrategyTemplates 获取所有策略模板
// GET /api/strategy-templates?category=grid&symbol=BTCUSDT&difficulty=beginner&risk_level=low
func getStrategyTemplates(c *gin.Context) {
	if strategyTemplateManager == nil {
		c.JSON(http.StatusOK, gin.H{"templates": []interface{}{}})
		return
	}

	templates := strategyTemplateManager.ListTemplates()

	// 支持筛选
	category := c.Query("category")
	symbol := c.Query("symbol")
	difficulty := c.Query("difficulty")
	riskLevel := c.Query("risk_level")
	tag := c.Query("tag")

	// 过滤模板
	filtered := make([]*config.StrategyTemplate, 0)
	for _, t := range templates {
		// 分类筛选
		if category != "" && t.Category != category {
			continue
		}

		// 币种筛选
		if symbol != "" && len(t.Symbols) > 0 {
			found := false
			for _, s := range t.Symbols {
				if s == symbol {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		// 难度筛选
		if difficulty != "" && t.Difficulty != difficulty {
			continue
		}

		// 风险等级筛选
		if riskLevel != "" && t.RiskLevel != riskLevel {
			continue
		}

		// 标签筛选
		if tag != "" && len(t.Tags) > 0 {
			found := false
			for _, ttag := range t.Tags {
				if ttag == tag {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		filtered = append(filtered, t)
	}

	c.JSON(http.StatusOK, gin.H{"templates": filtered})
}

// getStrategyTemplate 获取指定策略模板
// GET /api/strategy-templates/:id
func getStrategyTemplate(c *gin.Context) {
	templateID := c.Param("id")
	if templateID == "" {
		respondError(c, http.StatusBadRequest, "error.invalid_template_id")
		return
	}

	if strategyTemplateManager == nil {
		respondError(c, http.StatusServiceUnavailable, "error.template_manager_unavailable")
		return
	}

	template, ok := strategyTemplateManager.GetTemplate(templateID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Template not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"template": template})
}

// getStrategyTemplatesByCategory 按类别获取策略模板
// GET /api/strategy-templates/category/:category
func getStrategyTemplatesByCategory(c *gin.Context) {
	category := c.Param("category")
	if category == "" {
		respondError(c, http.StatusBadRequest, "error.invalid_category")
		return
	}

	if strategyTemplateManager == nil {
		c.JSON(http.StatusOK, gin.H{"templates": []interface{}{}})
		return
	}

	templates := strategyTemplateManager.GetTemplatesByCategory(category)
	c.JSON(http.StatusOK, gin.H{"templates": templates})
}

// ApplyTemplateRequest 应用模板请求
type ApplyTemplateRequest struct {
	TemplateID string `json:"template_id" binding:"required"`
}

// applyStrategyTemplate 应用策略模板到 Bot
// POST /api/bots/:id/apply-template
func applyStrategyTemplate(c *gin.Context) {
	botID := c.Param("id")
	if botID == "" {
		respondError(c, http.StatusBadRequest, "error.invalid_bot_id")
		return
	}

	if botConfigManager == nil {
		respondError(c, http.StatusServiceUnavailable, "error.bot_config_manager_unavailable")
		return
	}

	if strategyTemplateManager == nil {
		respondError(c, http.StatusServiceUnavailable, "error.template_manager_unavailable")
		return
	}

	var req ApplyTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_request", err)
		return
	}

	// 检查 Bot 是否正在运行
	if botManagerProvider() != nil {
		if bot, ok := botManagerProvider().GetBot(botID); ok && bot.Running {
			c.JSON(http.StatusConflict, gin.H{
				"error":     "bot_running",
				"error_key": "error.bot_running_cannot_apply_template",
				"message":   "Cannot apply template while bot is running",
			})
			return
		}
	}

	// 加载 Bot 配置
	botConfig, err := botConfigManager.LoadBotConfig(botID)
	if err != nil {
		respondError(c, http.StatusNotFound, "error.bot_config_not_found", err)
		return
	}

	// 应用模板
	if err := strategyTemplateManager.ApplyTemplate(req.TemplateID, botConfig); err != nil {
		logger.Error("应用模板失败: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "failed_to_apply_template",
			"message": err.Error(),
		})
		return
	}

	// 保存配置
	if err := botConfigManager.SaveBotConfig(botConfig); err != nil {
		logger.Error("保存配置失败: %v", err)
		respondError(c, http.StatusInternalServerError, "error.config_save_failed", err)
		return
	}

	// 同步到主配置
	syncBotConfigToMain(botID, botConfig)

	logger.Info("✅ Bot %s 已应用模板 %s", botID, req.TemplateID)
	c.JSON(http.StatusOK, gin.H{
		"ok":         true,
		"bot_id":     botID,
		"template_id": req.TemplateID,
	})
}

// saveCustomTemplate 保存自定义策略模板
// POST /api/strategy-templates/custom
func saveCustomTemplate(c *gin.Context) {
	if strategyTemplateManager == nil {
		respondError(c, http.StatusServiceUnavailable, "error.template_manager_unavailable")
		return
	}

	var template config.StrategyTemplate
	if err := c.ShouldBindJSON(&template); err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_request", err)
		return
	}

	// 验证模板 ID
	if template.ID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Template ID is required"})
		return
	}

	// 自定义模板必须以 custom_ 开头
	if len(template.ID) <= 8 || template.ID[:8] != "custom_" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Custom template ID must start with 'custom_'",
		})
		return
	}

	if err := strategyTemplateManager.SaveCustomTemplate(&template); err != nil {
		logger.Error("保存自定义模板失败: %v", err)
		respondError(c, http.StatusInternalServerError, "error.save_template_failed", err)
		return
	}

	logger.Info("✅ 自定义模板 %s 已保存", template.ID)
	c.JSON(http.StatusOK, gin.H{
		"ok":         true,
		"template_id": template.ID,
	})
}

// deleteCustomTemplate 删除自定义策略模板
// DELETE /api/strategy-templates/:id
func deleteCustomTemplate(c *gin.Context) {
	templateID := c.Param("id")
	if templateID == "" {
		respondError(c, http.StatusBadRequest, "error.invalid_template_id")
		return
	}

	if strategyTemplateManager == nil {
		respondError(c, http.StatusServiceUnavailable, "error.template_manager_unavailable")
		return
	}

	if err := strategyTemplateManager.DeleteCustomTemplate(templateID); err != nil {
		logger.Error("删除自定义模板失败: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "failed_to_delete_template",
			"message": err.Error(),
		})
		return
	}

	logger.Info("✅ 自定义模板 %s 已删除", templateID)
	c.JSON(http.StatusOK, gin.H{
		"ok":         true,
		"template_id": templateID,
	})
}

// exportStrategyTemplate 导出策略模板
// GET /api/strategy-templates/:id/export
func exportStrategyTemplate(c *gin.Context) {
	templateID := c.Param("id")
	if templateID == "" {
		respondError(c, http.StatusBadRequest, "error.invalid_template_id")
		return
	}

	if strategyTemplateManager == nil {
		respondError(c, http.StatusServiceUnavailable, "error.template_manager_unavailable")
		return
	}

	data, err := strategyTemplateManager.ExportTemplate(templateID)
	if err != nil {
		logger.Error("导出模板失败: %v", err)
		c.JSON(http.StatusNotFound, gin.H{"error": "Template not found"})
		return
	}

	c.Header("Content-Type", "application/json")
	c.Header("Content-Disposition", "attachment; filename="+templateID+".json")
	c.Data(http.StatusOK, "application/json", data)
}

// ImportTemplateRequest 导入模板请求
type ImportTemplateRequest struct {
	TemplateID string `json:"template_id"`
	JSONData   string `json:"json_data" binding:"required"`
}

// importStrategyTemplate 导入策略模板
// POST /api/strategy-templates/import
func importStrategyTemplate(c *gin.Context) {
	if strategyTemplateManager == nil {
		respondError(c, http.StatusServiceUnavailable, "error.template_manager_unavailable")
		return
	}

	var req ImportTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_request", err)
		return
	}

	// 导入模板
	if err := strategyTemplateManager.ImportTemplate([]byte(req.JSONData)); err != nil {
		logger.Error("导入模板失败: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "failed_to_import_template",
			"message": err.Error(),
		})
		return
	}

	logger.Info("✅ 策略模板已导入")
	c.JSON(http.StatusOK, gin.H{
		"ok": true,
	})
}
