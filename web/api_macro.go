package web

import (
	"net/http"

	"quantmesh/macro"

	"github.com/gin-gonic/gin"
)

// macroEventProviderInterface 宏觀事件 API 提供者接口
type macroEventProviderInterface interface {
	GetImpactSummary() macro.MacroImpactSummary
}

var macroEventProvider macroEventProviderInterface

// macroEventProviderAdapter 適配 MacroEventFetcher 為 API 提供者
type macroEventProviderAdapter struct {
	fetcher *macro.MacroEventFetcher
}

func (a *macroEventProviderAdapter) GetImpactSummary() macro.MacroImpactSummary {
	if a.fetcher == nil {
		return macro.MacroImpactSummary{}
	}
	return a.fetcher.GetImpactSummary()
}

// SetMacroEventFetcher 設置宏觀事件拉取器（由 main 注入）
func SetMacroEventFetcher(f *macro.MacroEventFetcher) {
	if f != nil {
		macroEventProvider = &macroEventProviderAdapter{fetcher: f}
	} else {
		macroEventProvider = nil
	}
}

// GetMacroEventProvider 獲取宏觀事件提供者（供 safety.CompositeRiskController 注入）
func GetMacroEventProvider() macroEventProviderInterface {
	return macroEventProvider
}

// getMacroEvents 獲取宏觀事件列表
// GET /api/macro/events
func getMacroEvents(c *gin.Context) {
	if macroEventProvider == nil {
		c.JSON(http.StatusOK, gin.H{
			"events":       []interface{}{},
			"last_fetched": nil,
			"enabled":      false,
		})
		return
	}
	adapter, ok := macroEventProvider.(*macroEventProviderAdapter)
	if !ok || adapter.fetcher == nil {
		c.JSON(http.StatusOK, gin.H{
			"events":       []interface{}{},
			"last_fetched": nil,
			"enabled":      false,
		})
		return
	}
	events := adapter.fetcher.GetEvents()
	lastFetched := adapter.fetcher.GetLastFetched()
	// 轉换為 JSON 友好格式
	eventsJSON := make([]interface{}, 0, len(events))
	for _, e := range events {
		eventsJSON = append(eventsJSON, gin.H{
			"id":                e.ID,
			"title":             e.Title,
			"description":       e.Description,
			"category":          string(e.Category),
			"category_label":   e.CategoryLabel,
			"probability":       e.Probability,
			"probability_delta": e.ProbabilityDelta,
			"volume":            e.Volume,
			"volume_24hr":      e.Volume24hr,
			"liquidity":         e.Liquidity,
			"source_url":        e.SourceURL,
			"end_date":          e.EndDate,
			"last_updated":      e.LastUpdated,
			"market_count":      e.MarketCount,
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"events":       eventsJSON,
		"last_fetched": lastFetched,
		"enabled":      true,
	})
}

// getMacroImpact 獲取宏觀影響匯總
// GET /api/macro/impact
func getMacroImpact(c *gin.Context) {
	if macroEventProvider == nil {
		c.JSON(http.StatusOK, gin.H{
			"composite_risk_score": 0,
			"event_count":          0,
			"high_impact_count":    0,
			"assessments":          []interface{}{},
			"last_fetched":         nil,
			"enabled":              false,
		})
		return
	}
	summary := macroEventProvider.GetImpactSummary()
	assessmentsJSON := make([]interface{}, 0, len(summary.Assessments))
	for _, a := range summary.Assessments {
		assessmentsJSON = append(assessmentsJSON, gin.H{
			"event_id":           a.EventID,
			"event_title":        a.EventTitle,
			"category":           string(a.Category),
			"probability":        a.Probability,
			"probability_delta":  a.ProbabilityDelta,
			"risk_score":         a.RiskScore,
			"impact_direction":   a.ImpactDirection,
			"crypto_impact":      string(a.CryptoImpact),
			"reason":             a.Reason,
			"weight":             a.Weight,
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"composite_risk_score": summary.CompositeRiskScore,
		"event_count":         summary.EventCount,
		"high_impact_count":   summary.HighImpactCount,
		"assessments":         assessmentsJSON,
		"last_fetched":        summary.LastFetched,
		"enabled":             true,
	})
}
