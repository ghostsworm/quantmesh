//go:build ignore

package main

import (
	"fmt"
	"os"
	"time"

	"quantmesh/backtest"
	"quantmesh/config"
	"quantmesh/logger"
)

func main() {
	// 加載配置：默認當前目錄 config.yaml（可由 docs/config/examples/config.example.yaml 複製後填寫）；可設 QUANTMESH_CONFIG_YAML 覆蓋路徑
	cfgPath := os.Getenv("QUANTMESH_CONFIG_YAML")
	if cfgPath == "" {
		cfgPath = "config.yaml"
	}
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		logger.Error("❌ 加載配置失败: %v", err)
		return
	}

	logger.Info("📊 市场數據分析 - 3分钟周期")
	logger.Info("=" + string(make([]rune, 70)))
	logger.Info("")

	// 计算時间範圍（最近 7 天）
	endTime := time.Now()
	startTime := endTime.AddDate(0, 0, -7)

	symbol := "BTCUSDT"
	interval := "3m"

	// 從配置文件獲取 Binance 配置
	binanceExchange := cfg.Exchanges["binance"]
	binanceConfig := map[string]string{
		"api_key":    binanceExchange.APIKey,
		"secret_key": binanceExchange.SecretKey,
		"testnet":    fmt.Sprintf("%v", binanceExchange.Testnet),
	}

	// 獲取歷史數據
	logger.Info("📥 獲取歷史數據...")
	candles, err := backtest.GetHistoricalData(symbol, interval, startTime, endTime, binanceConfig)
	if err != nil {
		logger.Error("❌ 獲取歷史數據失败: %v", err)
		return
	}

	if len(candles) == 0 {
		logger.Error("❌ 没有獲取到數據")
		return
	}

	logger.Info("✅ 獲取到 %d 根 K 線", len(candles))
	logger.Info("")

	// 分析數據
	logger.Info("=" + string(make([]rune, 70)))
	logger.Info("📈 市场趋势分析")
	logger.Info("=" + string(make([]rune, 70)))
	logger.Info("")

	// 基本信息
	firstCandle := candles[0]
	lastCandle := candles[len(candles)-1]

	startPrice := firstCandle.Open
	endPrice := lastCandle.Close

	startTimeStr := time.Unix(firstCandle.Timestamp/1000, 0).Format("2006-01-02 15:04")
	endTimeStr := time.Unix(lastCandle.Timestamp/1000, 0).Format("2006-01-02 15:04")

	logger.Info("📅 時间範圍:")
	logger.Info("   开始: %s", startTimeStr)
	logger.Info("   結束: %s", endTimeStr)
	logger.Info("   時长: %.1f 天", float64(lastCandle.Timestamp-firstCandle.Timestamp)/(1000*86400))
	logger.Info("")

	// 價格分析
	logger.Info("💰 價格走势:")
	logger.Info("   起始價格: $%.2f", startPrice)
	logger.Info("   結束價格: $%.2f", endPrice)

	priceChange := endPrice - startPrice
	priceChangePercent := (priceChange / startPrice) * 100

	if priceChange > 0 {
		logger.Info("   價格變化: +$%.2f (+%.2f%%) 📈 上涨", priceChange, priceChangePercent)
	} else {
		logger.Info("   價格變化: -$%.2f (%.2f%%) 📉 下跌", -priceChange, priceChangePercent)
	}
	logger.Info("")

	// 找出最高点和最低点
	var highestPrice, lowestPrice float64
	var highestTime, lowestTime int64

	highestPrice = candles[0].High
	lowestPrice = candles[0].Low
	highestTime = candles[0].Timestamp
	lowestTime = candles[0].Timestamp

	for _, candle := range candles {
		if candle.High > highestPrice {
			highestPrice = candle.High
			highestTime = candle.Timestamp
		}
		if candle.Low < lowestPrice {
			lowestPrice = candle.Low
			lowestTime = candle.Timestamp
		}
	}

	highTimeStr := time.Unix(highestTime/1000, 0).Format("2006-01-02 15:04")
	lowTimeStr := time.Unix(lowestTime/1000, 0).Format("2006-01-02 15:04")

	logger.Info("🔝 最高点:")
	logger.Info("   價格: $%.2f", highestPrice)
	logger.Info("   時间: %s", highTimeStr)
	logger.Info("")

	logger.Info("🔻 最低点:")
	logger.Info("   價格: $%.2f", lowestPrice)
	logger.Info("   時间: %s", lowTimeStr)
	logger.Info("")

	// 振幅分析
	amplitude := highestPrice - lowestPrice
	amplitudePercent := (amplitude / lowestPrice) * 100

	logger.Info("📊 振幅分析:")
	logger.Info("   價格区间: $%.2f - $%.2f", lowestPrice, highestPrice)
	logger.Info("   振幅: $%.2f (%.2f%%)", amplitude, amplitudePercent)
	logger.Info("")

	// 從最高点到最低点的跌幅
	highToLowDrop := highestPrice - lowestPrice
	highToLowDropPercent := (highToLowDrop / highestPrice) * 100

	logger.Info("📉 最大回撤（從最高点到最低点）:")
	logger.Info("   跌幅: $%.2f (%.2f%%)", highToLowDrop, highToLowDropPercent)

	if highestTime < lowestTime {
		logger.Info("   顺序: 先涨到最高点，后跌到最低点")
	} else {
		logger.Info("   顺序: 先跌到最低点，后涨到最高点")
	}
	logger.Info("")

	// 波动性分析
	logger.Info("📈 波动性分析:")

	// 计算平均價格
	var totalPrice float64
	for _, candle := range candles {
		totalPrice += candle.Close
	}
	avgPrice := totalPrice / float64(len(candles))

	// 计算標准差
	var variance float64
	for _, candle := range candles {
		diff := candle.Close - avgPrice
		variance += diff * diff
	}
	variance /= float64(len(candles))
	stdDev := 0.0
	for i := 0; i < 10; i++ {
		stdDev = (stdDev + variance/stdDev) / 2
		if stdDev == 0 {
			stdDev = 1
			break
		}
	}

	volatility := (stdDev / avgPrice) * 100

	logger.Info("   平均價格: $%.2f", avgPrice)
	logger.Info("   標准差: $%.2f", stdDev)
	logger.Info("   波动率: %.2f%%", volatility)
	logger.Info("")

	// 趋势判断
	logger.Info("=" + string(make([]rune, 70)))
	logger.Info("🎯 市场趋势判断")
	logger.Info("=" + string(make([]rune, 70)))
	logger.Info("")

	// 计算简單移动平均線
	period := 20
	if len(candles) < period {
		period = len(candles) / 2
	}

	var recentAvg, earlyAvg float64
	for i := len(candles) - period; i < len(candles); i++ {
		recentAvg += candles[i].Close
	}
	recentAvg /= float64(period)

	for i := 0; i < period && i < len(candles); i++ {
		earlyAvg += candles[i].Close
	}
	earlyAvg /= float64(period)

	logger.Info("📊 移动平均分析（%d 周期）:", period)
	logger.Info("   前期平均: $%.2f", earlyAvg)
	logger.Info("   近期平均: $%.2f", recentAvg)

	trendChange := recentAvg - earlyAvg
	trendChangePercent := (trendChange / earlyAvg) * 100

	if trendChange > 0 {
		logger.Info("   趋势: 上升 +%.2f%%", trendChangePercent)
	} else {
		logger.Info("   趋势: 下降 %.2f%%", trendChangePercent)
	}
	logger.Info("")

	// 市场状態判断
	logger.Info("🔍 市场状態:")

	var marketState string
	var stateEmoji string

	if amplitudePercent < 2 {
		marketState = "窄幅震荡"
		stateEmoji = "😴"
	} else if amplitudePercent < 5 {
		marketState = "正常震荡"
		stateEmoji = "📊"
	} else if amplitudePercent < 10 {
		marketState = "高波动"
		stateEmoji = "⚡"
	} else {
		marketState = "极端波动"
		stateEmoji = "🌪️"
	}

	logger.Info("   状態: %s %s (振幅 %.2f%%)", marketState, stateEmoji, amplitudePercent)
	logger.Info("")

	// 策略建议
	logger.Info("=" + string(make([]rune, 70)))
	logger.Info("💡 策略建议")
	logger.Info("=" + string(make([]rune, 70)))
	logger.Info("")

	if priceChangePercent > 5 {
		logger.Info("✅ 市场趋势: 明显上涨")
		logger.Info("   推荐策略: 趋势跟踪策略（顺势而為）")
		logger.Info("   风險提示: 注意回呼风險")
	} else if priceChangePercent < -5 {
		logger.Info("⚠️ 市场趋势: 明显下跌")
		logger.Info("   推荐策略: 空倉观望或做空")
		logger.Info("   风險提示: 下跌趋势中做多风險极高")
	} else if amplitudePercent > 5 {
		logger.Info("📊 市场趋势: 高波动震荡")
		logger.Info("   推荐策略: 均值回归策略（低買高賣）")
		logger.Info("   风險提示: 設置好止损，控制單笔亏损")
	} else {
		logger.Info("😴 市场趋势: 窄幅震荡")
		logger.Info("   推荐策略: 网格交易或观望")
		logger.Info("   风險提示: 交易频繁，手续费成本高")
	}

	logger.Info("")
	logger.Info("=" + string(make([]rune, 70)))
	logger.Info("📌 總結")
	logger.Info("=" + string(make([]rune, 70)))
	logger.Info("")

	fmt.Printf("在这 %.1f 天的時间里：\n", float64(lastCandle.Timestamp-firstCandle.Timestamp)/(1000*86400))
	fmt.Printf("• 價格從 $%.2f %s 到 $%.2f\n", startPrice,
		map[bool]string{true: "上涨", false: "下跌"}[priceChange > 0], endPrice)
	fmt.Printf("• 变化幅度: %.2f%%\n", priceChangePercent)
	fmt.Printf("• 最高点: $%.2f，最低点: $%.2f\n", highestPrice, lowestPrice)
	fmt.Printf("• 最大振幅: %.2f%%\n", amplitudePercent)
	fmt.Printf("• 市场状態: %s\n", marketState)
	fmt.Printf("\n")
	fmt.Printf("这就是為什麼回测結果都是亏损的原因：\n")

	if priceChangePercent < -5 {
		fmt.Printf("❌ 市场处於下跌趋势，大部分做多策略都會亏损\n")
		fmt.Printf("💡 建议：等待市场企稳后再测試，或测試更长時间周期\n")
	} else if amplitudePercent < 3 {
		fmt.Printf("❌ 市場波動太小，策略难以捕捉有效信号\n")
		fmt.Printf("💡 建议：选擇波动更大的時间段進行回测\n")
	} else {
		fmt.Printf("⚠️ 市场环境不适合當前策略参數\n")
		fmt.Printf("💡 建议：优化策略参數或选擇不同的市场环境\n")
	}
}
