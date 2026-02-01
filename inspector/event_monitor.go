package inspector

import (
	"fmt"
	"math"
	"sync"
	"time"

	"quantmesh/monitor"
)

// InspectorEventPayload 觸發立即通知的事件負載
type InspectorEventPayload struct {
	EventType string
	Title     string
	Message   string
	Data      map[string]interface{}
	Snapshot  *InspectionSnapshot
	At        time.Time
}

// EventThresholds 事件觸發閾值配置
type EventThresholds struct {
	PnLAlert           float64 // 單筆盈虧超過此值立即通知（USDT）
	RiskScoreChange    float64 // 新聞風險評分變化超過此值
	FundingRateAlert   float64 // 資金費率絕對值超過此值（如 0.001 = 0.1%）
	CorrelationChange  float64 // 黃金與 BTC 相關性變化超過此值
	BalanceChangePct   float64 // 賬戶餘額變化百分比超過此值（如 5 = 5%）
}

// DefaultEventThresholds 預設閾值
func DefaultEventThresholds() EventThresholds {
	return EventThresholds{
		PnLAlert:          100,
		RiskScoreChange:   20,
		FundingRateAlert:  0.001,
		CorrelationChange:  0.2,
		BalanceChangePct:  5,
	}
}

// EventMonitor 事件監測器：比對當前快照與上一快照，產出需立即通知的事件
type EventMonitor struct {
	mu         sync.Mutex
	last       *InspectionSnapshot
	thresholds EventThresholds
}

// NewEventMonitor 創建事件監測器
func NewEventMonitor(thresholds EventThresholds) *EventMonitor {
	if thresholds.PnLAlert <= 0 {
		thresholds.PnLAlert = 100
	}
	if thresholds.RiskScoreChange <= 0 {
		thresholds.RiskScoreChange = 20
	}
	if thresholds.FundingRateAlert <= 0 {
		thresholds.FundingRateAlert = 0.001
	}
	if thresholds.CorrelationChange <= 0 {
		thresholds.CorrelationChange = 0.2
	}
	return &EventMonitor{thresholds: thresholds}
}

// Check 傳入當前快照，返回應立即通知的事件列表，並更新內部上次快照
func (e *EventMonitor) Check(current *InspectionSnapshot) []InspectorEventPayload {
	if current == nil {
		return nil
	}
	e.mu.Lock()
	prev := e.last
	e.last = cloneSnapshotForCompare(current)
	e.mu.Unlock()

	var out []InspectorEventPayload
	now := current.Timestamp

	// 風控觸發/恢復
	if prev != nil {
		if current.RiskStatus.Triggered && !prev.RiskStatus.Triggered {
			out = append(out, InspectorEventPayload{
				EventType: EventRiskTriggered,
				Title:     "風控已觸發",
				Message:   current.RiskStatus.Reason,
				Data:      map[string]interface{}{"reason": current.RiskStatus.Reason},
				Snapshot:  current,
				At:        now,
			})
		} else if !current.RiskStatus.Triggered && prev.RiskStatus.Triggered {
			out = append(out, InspectorEventPayload{
				EventType: EventRiskRecovered,
				Title:     "風控已恢復",
				Message:   "風控條件已解除，交易可繼續。",
				Snapshot:  current,
				At:        now,
			})
		}
	}

	// 新聞風險評分突變
	for asset, curRisk := range current.NewsRisk {
		if curRisk == nil {
			continue
		}
		var prevScore float64
		if prev != nil && prev.NewsRisk != nil {
			if r := prev.NewsRisk[asset]; r != nil {
				prevScore = r.OverallRiskScore
			}
		}
		delta := math.Abs(curRisk.OverallRiskScore - prevScore)
		if delta >= e.thresholds.RiskScoreChange {
			out = append(out, InspectorEventPayload{
				EventType: EventNewsRiskChange,
				Title:     "新聞風險評分變化",
				Message:   "資產 " + asset + " 風險評分變化 " + formatFloat(delta) + "，當前 " + formatFloat(curRisk.OverallRiskScore) + "/100，建議: " + curRisk.Recommendation,
				Data:      map[string]interface{}{"asset": asset, "score": curRisk.OverallRiskScore, "change": delta},
				Snapshot:  current,
				At:        now,
			})
		}
	}

	// 資金費率異常
	for sym, m := range current.MarketData {
		if math.Abs(m.FundingRate) >= e.thresholds.FundingRateAlert {
			out = append(out, InspectorEventPayload{
				EventType: EventFundingRateAlert,
				Title:     "資金費率異常",
				Message:   sym + " 資金費率 " + formatFloat(m.FundingRate*100) + "%，超過閾值。",
				Data:      map[string]interface{}{"symbol": sym, "funding_rate": m.FundingRate},
				Snapshot:  current,
				At:        now,
			})
		}
	}

	// 賬戶餘額異常變動
	if prev != nil && prev.AccountSummary.TotalBalance > 0 {
		pct := math.Abs(current.AccountSummary.TotalBalance-prev.AccountSummary.TotalBalance) / prev.AccountSummary.TotalBalance * 100
		if pct >= e.thresholds.BalanceChangePct {
			out = append(out, InspectorEventPayload{
				EventType: EventBalanceChange,
				Title:     "賬戶餘額變動",
				Message:   "總餘額變化 " + formatFloat(pct) + "%，當前 " + formatFloat(current.AccountSummary.TotalBalance) + " USDT。",
				Data:      map[string]interface{}{"prev": prev.AccountSummary.TotalBalance, "current": current.AccountSummary.TotalBalance, "pct": pct},
				Snapshot:  current,
				At:        now,
			})
		}
	}

	// 黃金與 BTC 相關性突變
	if current.GoldAnalysis != nil && prev != nil && prev.GoldAnalysis != nil {
		delta := math.Abs(current.GoldAnalysis.CorrelationWithBTC - prev.GoldAnalysis.CorrelationWithBTC)
		if delta >= e.thresholds.CorrelationChange {
			out = append(out, InspectorEventPayload{
				EventType: EventGoldCorrelationChange,
				Title:     "黃金與 BTC 相關性變化",
				Message:   "相關性變化 " + formatFloat(delta) + "，當前 " + formatFloat(current.GoldAnalysis.CorrelationWithBTC) + "。",
				Data:      map[string]interface{}{"correlation": current.GoldAnalysis.CorrelationWithBTC, "change": delta},
				Snapshot:  current,
				At:        now,
			})
		}
	}

	// 單筆盈虧告警：需從外部交易事件得知，此處不從快照推斷；可由 main 在 OrderFilled 時檢查 PnL 後調用 NotifyUrgent

	return out
}

// cloneSnapshotForCompare 淺拷貝快照用於下次比對（僅保留比對所需字段）
func cloneSnapshotForCompare(s *InspectionSnapshot) *InspectionSnapshot {
	if s == nil {
		return nil
	}
	c := &InspectionSnapshot{
		Timestamp:      s.Timestamp,
		AccountSummary: s.AccountSummary,
		RiskStatus:     s.RiskStatus,
	}
	if len(s.NewsRisk) > 0 {
		c.NewsRisk = make(map[string]*monitor.NewsRiskAssessment)
		for k, v := range s.NewsRisk {
			c.NewsRisk[k] = v
		}
	}
	if len(s.MarketData) > 0 {
		c.MarketData = make(map[string]MarketInfo)
		for k, v := range s.MarketData {
			c.MarketData[k] = v
		}
	}
	if s.GoldAnalysis != nil {
		c.GoldAnalysis = &GoldAnalysis{
			CorrelationWithBTC: s.GoldAnalysis.CorrelationWithBTC,
		}
	}
	return c
}

// 避免 inspector 引用 monitor 的 NewsRiskAssessment 在 clone 時形成循環，改為僅複製我們比對用到的字段
// cloneSnapshotForCompare 已改為僅複製 RiskStatus、AccountSummary、NewsRisk（指針）、MarketData、GoldAnalysis.CorrelationWithBTC
// 需在 types 中 NewsRisk 為 map[string]*monitor.NewsRiskAssessment，所以 clone 時直接賦值指針即可

func formatFloat(f float64) string {
	if math.Abs(f) >= 100 || (math.Abs(f) > 0 && math.Abs(f) < 0.01) {
		return fmt.Sprintf("%.2f", f)
	}
	return fmt.Sprintf("%.4f", f)
}
