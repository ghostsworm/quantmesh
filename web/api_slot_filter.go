package web

import (
	"net/http"
	"quantmesh/config"
	"strconv"

	"github.com/gin-gonic/gin"
)

// getSlotFilter 獲取槽位過濾器
// GET /api/bots/:id/slot-filter
func getSlotFilter(c *gin.Context) {
	botID := c.Param("id")

	if botExtendedProvider == nil {
		respondError(c, http.StatusServiceUnavailable, "error.bot_manager_unavailable")
		return
	}

	bot, ok := botExtendedProvider.GetBot(botID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Bot not found"})
		return
	}

	filter := bot.GetSlotFilter()
	if filter == nil {
		filter = &config.SlotFilterConfig{Rules: []config.SlotFilterRule{}}
	}

	c.JSON(http.StatusOK, filter)
}

// setSlotFilter 設置槽位過濾器
// POST /api/bots/:id/slot-filter
func setSlotFilter(c *gin.Context) {
	botID := c.Param("id")

	if botExtendedProvider == nil {
		respondError(c, http.StatusServiceUnavailable, "error.bot_manager_unavailable")
		return
	}

	bot, ok := botExtendedProvider.GetBot(botID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Bot not found"})
		return
	}

	var req config.SlotFilterConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	bot.SetSlotFilter(&req)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// addSlotFilterRule 添加過濾規則
// POST /api/bots/:id/slot-filter/rules
func addSlotFilterRule(c *gin.Context) {
	botID := c.Param("id")

	if botExtendedProvider == nil {
		respondError(c, http.StatusServiceUnavailable, "error.bot_manager_unavailable")
		return
	}

	bot, ok := botExtendedProvider.GetBot(botID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Bot not found"})
		return
	}

	var rule config.SlotFilterRule
	if err := c.ShouldBindJSON(&rule); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 驗證規則類型
	if rule.Type != "exclude" && rule.Type != "include" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "type must be 'exclude' or 'include'"})
		return
	}

	filter := bot.GetSlotFilter()
	if filter == nil {
		filter = &config.SlotFilterConfig{Rules: []config.SlotFilterRule{}}
	}
	filter.Rules = append(filter.Rules, rule)

	bot.SetSlotFilter(filter)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// removeSlotFilterRule 刪除過濾規則
// DELETE /api/bots/:id/slot-filter/rules/:index
func removeSlotFilterRule(c *gin.Context) {
	botID := c.Param("id")
	indexStr := c.Param("index")

	index, err := strconv.Atoi(indexStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid index"})
		return
	}

	if botExtendedProvider == nil {
		respondError(c, http.StatusServiceUnavailable, "error.bot_manager_unavailable")
		return
	}

	bot, ok := botExtendedProvider.GetBot(botID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Bot not found"})
		return
	}

	filter := bot.GetSlotFilter()
	if filter == nil || index < 0 || index >= len(filter.Rules) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid rule index"})
		return
	}

	// 刪除規則
	filter.Rules = append(filter.Rules[:index], filter.Rules[index+1:]...)
	bot.SetSlotFilter(filter)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// getBotSlots 獲取Bot的所有槽位信息
// GET /api/bots/:id/slots
func getBotSlots(c *gin.Context) {
	botID := c.Param("id")

	if botExtendedProvider == nil {
		respondError(c, http.StatusServiceUnavailable, "error.bot_manager_unavailable")
		return
	}

	bot, ok := botExtendedProvider.GetBot(botID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Bot not found"})
		return
	}

	slots := bot.GetSlots()
	c.JSON(http.StatusOK, gin.H{"slots": slots})
}

// toggleSlotEnabled 切換槽位啟用狀態
// POST /api/bots/:id/slots/toggle
func toggleSlotEnabled(c *gin.Context) {
	botID := c.Param("id")

	if botExtendedProvider == nil {
		respondError(c, http.StatusServiceUnavailable, "error.bot_manager_unavailable")
		return
	}

	bot, ok := botExtendedProvider.GetBot(botID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Bot not found"})
		return
	}

	var req struct {
		Price   float64 `json:"price" binding:"required"`
		Enabled bool    `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	filter := bot.GetSlotFilter()
	if filter == nil {
		filter = &config.SlotFilterConfig{Rules: []config.SlotFilterRule{}}
	}

	if req.Enabled {
		// 移除該價格的禁用規則
		newRules := []config.SlotFilterRule{}
		for _, rule := range filter.Rules {
			if rule.Type == "exclude" {
				// 從規則中移除該價格
				newPrices := []float64{}
				for _, p := range rule.Prices {
					if p != req.Price {
						newPrices = append(newPrices, p)
					}
				}
				if len(newPrices) > 0 {
					rule.Prices = newPrices
					newRules = append(newRules, rule)
				}
			} else {
				newRules = append(newRules, rule)
			}
		}
		filter.Rules = newRules
	} else {
		// 添加該價格的禁用規則
		filter.Rules = append(filter.Rules, config.SlotFilterRule{
			Type:   "exclude",
			Prices: []float64{req.Price},
			Reason: "manual_disable",
		})
	}

	bot.SetSlotFilter(filter)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
