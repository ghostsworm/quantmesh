package main

import (
	"fmt"
	"time"

	"quantmesh/backtest"
	"quantmesh/config"
	"quantmesh/logger"
)

func main() {
	// 加载配置
	cfg, err := config.LoadConfig("config.yaml")
	if err != nil {
		logger.Error("❌ 加载配置失败: %v", err)
		return
	}

	logger.Info("📊 市场数据分析 - 3分钟周期")
	logger.Info("=" + string(make([]rune, 70)))
	logger.Info("")

	// 计算时间范围（最近 7 天）
	endTime := time.Now()
	startTime := endTime.AddDate(0, 0, -7)

	symbol := "BTCUSDT"
	interval := "3m"

	// 从配置文件获取 Binance 配置
	binanceExchange := cfg.Exchanges["binance"]
	binanceConfig := map[string]string{
		"api_key":    binanceExchange.APIKey,
		"secret_key": binanceExchange.SecretKey,
		"testnet":    fmt.Sprintf("%v", binanceExchange.Testnet),
	}

	// 获取历史数据
	logger.Info("📥 获取历史数据...")
	candles, err := backtest.GetHistoricalData(symbol, interval, startTime, endTime, binanceConfig)
	if err != nil {
		logger.Error("❌ 获取历史数据失败: %v", err)
		return
	}

	if len(candles) == 0 {
		logger.Error("❌ 没有获取到数据")
		return
	}

	logger.Info("✅ 获取到 %d 根 K 线", len(candles))
	logger.Info("")

	// 分析数据
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

	logger.Info("📅 时间范围:")
	logger.Info("   开始: %s", startTimeStr)
	logger.Info("   结束: %s", endTimeStr)
	logger.Info("   时长: %.1f 天", float64(lastCandle.Timestamp-firstCandle.Timestamp)/(1000*86400))
	logger.Info("")

	// 价格分析
	logger.Info("💰 价格走势:")
	logger.Info("   起始价格: $%.2f", startPrice)
	logger.Info("   结束价格: $%.2f", endPrice)

	priceChange := endPrice - startPrice
	priceChangePercent := (priceChange / startPrice) * 100

	if priceChange > 0 {
		logger.Info("   价格变化: +$%.2f (+%.2f%%) 📈 上涨", priceChange, priceChangePercent)
	} else {
		logger.Info("   价格变化: -$%.2f (%.2f%%) 📉 下跌", -priceChange, priceChangePercent)
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
	logger.Info("   价格: $%.2f", highestPrice)
	logger.Info("   时间: %s", highTimeStr)
	logger.Info("")

	logger.Info("🔻 最低点:")
	logger.Info("   价格: $%.2f", lowestPrice)
	logger.Info("   时间: %s", lowTimeStr)
	logger.Info("")

	// 振幅分析
	amplitude := highestPrice - lowestPrice
	amplitudePercent := (amplitude / lowestPrice) * 100

	logger.Info("📊 振幅分析:")
	logger.Info("   价格区间: $%.2f - $%.2f", lowestPrice, highestPrice)
	logger.Info("   振幅: $%.2f (%.2f%%)", amplitude, amplitudePercent)
	logger.Info("")

	// 从最高点到最低点的跌幅
	highToLowDrop := highestPrice - lowestPrice
	highToLowDropPercent := (highToLowDrop / highestPrice) * 100

	logger.Info("📉 最大回撤（从最高点到最低点）:")
	logger.Info("   跌幅: $%.2f (%.2f%%)", highToLowDrop, highToLowDropPercent)

	if highestTime < lowestTime {
		logger.Info("   顺序: 先涨到最高点，后跌到最低点")
	} else {
		logger.Info("   顺序: 先跌到最低点，后涨到最高点")
	}
	logger.Info("")

	// 波动性分析
	logger.Info("📈 波动性分析:")

	// 计算平均价格
	var totalPrice float64
	for _, candle := range candles {
		totalPrice += candle.Close
	}
	avgPrice := totalPrice / float64(len(candles))

	// 计算标准差
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

	logger.Info("   平均价格: $%.2f", avgPrice)
	logger.Info("   标准差: $%.2f", stdDev)
	logger.Info("   波动率: %.2f%%", volatility)
	logger.Info("")

	// 趋势判断
	logger.Info("=" + string(make([]rune, 70)))
	logger.Info("🎯 市场趋势判断")
	logger.Info("=" + string(make([]rune, 70)))
	logger.Info("")

	// 计算简单移动平均线
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

	// 市场状态判断
	logger.Info("🔍 市场状态:")

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

	logger.Info("   状态: %s %s (振幅 %.2f%%)", marketState, stateEmoji, amplitudePercent)
	logger.Info("")

	// 策略建议
	logger.Info("=" + string(make([]rune, 70)))
	logger.Info("💡 策略建议")
	logger.Info("=" + string(make([]rune, 70)))
	logger.Info("")

	if priceChangePercent > 5 {
		logger.Info("✅ 市场趋势: 明显上涨")
		logger.Info("   推荐策略: 趋势跟踪策略（顺势而为）")
		logger.Info("   风险提示: 注意回调风险")
	} else if priceChangePercent < -5 {
		logger.Info("⚠️ 市场趋势: 明显下跌")
		logger.Info("   推荐策略: 空仓观望或做空")
		logger.Info("   风险提示: 下跌趋势中做多风险极高")
	} else if amplitudePercent > 5 {
		logger.Info("📊 市场趋势: 高波动震荡")
		logger.Info("   推荐策略: 均值回归策略（低买高卖）")
		logger.Info("   风险提示: 设置好止损，控制单笔亏损")
	} else {
		logger.Info("😴 市场趋势: 窄幅震荡")
		logger.Info("   推荐策略: 网格交易或观望")
		logger.Info("   风险提示: 交易频繁，手续费成本高")
	}

	logger.Info("")
	logger.Info("=" + string(make([]rune, 70)))
	logger.Info("📌 总结")
	logger.Info("=" + string(make([]rune, 70)))
	logger.Info("")

	fmt.Printf("在这 %.1f 天的时间里：\n", float64(lastCandle.Timestamp-firstCandle.Timestamp)/(1000*86400))
	fmt.Printf("• 价格从 $%.2f %s 到 $%.2f\n", startPrice,
		map[bool]string{true: "上涨", false: "下跌"}[priceChange > 0], endPrice)
	fmt.Printf("• 变化幅度: %.2f%%\n", priceChangePercent)
	fmt.Printf("• 最高点: $%.2f，最低点: $%.2f\n", highestPrice, lowestPrice)
	fmt.Printf("• 最大振幅: %.2f%%\n", amplitudePercent)
	fmt.Printf("• 市场状态: %s\n", marketState)
	fmt.Printf("\n")
	fmt.Printf("这就是为什么回测结果都是亏损的原因：\n")

	if priceChangePercent < -5 {
		fmt.Printf("❌ 市场处于下跌趋势，大部分做多策略都会亏损\n")
		fmt.Printf("💡 建议：等待市场企稳后再测试，或测试更长时间周期\n")
	} else if amplitudePercent < 3 {
		fmt.Printf("❌ 市场波动太小，策略难以捕捉有效信号\n")
		fmt.Printf("💡 建议：选择波动更大的时间段进行回测\n")
	} else {
		fmt.Printf("⚠️ 市场环境不适合当前策略参数\n")
		fmt.Printf("💡 建议：优化策略参数或选择不同的市场环境\n")
	}
}
