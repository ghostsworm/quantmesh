package inspector

import (
	"math"
	"time"

	"quantmesh/storage"
)

const (
	AssetTypeGold = "commodity_gold"
	AssetTypeBTC  = "crypto_btc"
)

// GoldAnalyzer 黃金專項分析器
type GoldAnalyzer struct {
	GoldSymbol   string
	BTCSymbol    string
	GetPrice     PriceProvider
	Storage      storage.Storage
	GetNewsRisk  NewsRiskProvider
}

// Analyze 產出黃金專項分析
func (g *GoldAnalyzer) Analyze() *GoldAnalysis {
	now := time.Now()
	out := &GoldAnalysis{LastUpdated: now}

	if g.GetPrice == nil {
		return out
	}
	goldSym := g.GoldSymbol
	if goldSym == "" {
		goldSym = "PAXGUSDT"
	}
	btcSym := g.BTCSymbol
	if btcSym == "" {
		btcSym = "BTCUSDT"
	}

	out.CurrentPrice = g.GetPrice(goldSym)
	if out.CurrentPrice <= 0 {
		return out
	}

	// 24h 漲跌：從價格歷史或近似
	price24hAgo := out.CurrentPrice
	if g.Storage != nil {
		if h, err := g.Storage.GetPriceAtTime(AssetTypeGold, goldSym, now.Add(-24*time.Hour), 30*time.Minute); err == nil && h != nil {
			price24hAgo = h.Price
		}
	}
	out.Change24h = out.CurrentPrice - price24hAgo
	if price24hAgo > 0 {
		out.Change24hPct = (out.Change24h / price24hAgo) * 100
	}

	// 與 BTC 相關性（簡化：從近期價格序列計算相關係數）
	if g.Storage != nil {
		end := now
		start := now.Add(-7 * 24 * time.Hour)
		goldHistory, _ := g.Storage.GetPriceHistory(AssetTypeGold, goldSym, start, end, 100)
		btcHistory, _ := g.Storage.GetPriceHistory(AssetTypeBTC, btcSym, start, end, 100)
		out.CorrelationWithBTC = correlationFromHistory(goldHistory, btcHistory)
	}

	// 避險情緒指數：黃金風險低、加密風險高時指數高
	out.SafeHavenIndex = 50
	if g.GetNewsRisk != nil {
		goldRisk := g.GetNewsRisk(goldSym)
		btcRisk := g.GetNewsRisk(btcSym)
		gScore := 0.0
		bScore := 0.0
		if goldRisk != nil {
			gScore = goldRisk.OverallRiskScore
		}
		if btcRisk != nil {
			bScore = btcRisk.OverallRiskScore
		}
		// 避險指數：BTC 風險高時黃金更受青睞，黃金風險低時加分
		out.SafeHavenIndex = 50 + (bScore/2) - (gScore / 2)
		if out.SafeHavenIndex < 0 {
			out.SafeHavenIndex = 0
		}
		if out.SafeHavenIndex > 100 {
			out.SafeHavenIndex = 100
		}
	}

	// 黃金相關新聞
	if g.GetNewsRisk != nil {
		if a := g.GetNewsRisk(goldSym); a != nil {
			out.GoldMarketNews = a.RelatedNews
			if len(out.GoldMarketNews) == 0 {
				out.GoldMarketNews = a.RecentHighRiskNews
			}
		}
	}

	return out
}

// correlationFromHistory 從兩段價格歷史計算簡單相關係數（按時間對齊取交集）
func correlationFromHistory(a, b []*storage.PriceHistory) float64 {
	if a == nil || b == nil || len(a) < 2 || len(b) < 2 {
		return 0
	}
	// 簡化：用最後 N 個點按 recorded_at 對齊太複雜，這裡用索引對齊（假設採樣間隔一致）
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	var sumX, sumY, sumXY, sumX2, sumY2 float64
	for i := 0; i < n; i++ {
		x := a[i].Price
		y := b[i].Price
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
		sumY2 += y * y
	}
	den := (float64(n)*sumX2 - sumX*sumX) * (float64(n)*sumY2 - sumY*sumY)
	if den <= 0 {
		return 0
	}
	r := (float64(n)*sumXY - sumX*sumY) / math.Sqrt(den)
	if r < -1 {
		r = -1
	}
	if r > 1 {
		r = 1
	}
	return r
}
