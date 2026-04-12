package web

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"

	"quantmesh/feerate"

	"github.com/gin-gonic/gin"
)

// ParamAdvisorResponse 参數建议响应
type ParamAdvisorResponse struct {
	CurrentPrice float64       `json:"current_price"`
	MakerFee     float64       `json:"maker_fee"`
	TakerFee     float64       `json:"taker_fee"`
	FeeSource    string        `json:"fee_source"` // "exchange_api" | "config" | "default"
	Exchange     string        `json:"exchange"`
	Symbol       string        `json:"symbol"`
	Suggestions  ParamSuggestion `json:"suggestions"`
}

// ParamSuggestion 参數建议
type ParamSuggestion struct {
	PriceInterval        RangeAdvice `json:"price_interval"`
	OrderQuantity        RangeAdvice `json:"order_quantity"`
	MinProfitableInterval float64    `json:"min_profitable_interval"` // 最低盈利所需的 price_interval
	BreakevenFeeRate     float64    `json:"breakeven_fee_rate"`      // 盈亏平衡的等效单边费率
}

// RangeAdvice 范围建议
type RangeAdvice struct {
	Min         float64 `json:"min"`
	Recommended float64 `json:"recommended"`
	Max         float64 `json:"max"`
	Reason      string  `json:"reason"`
}

// FetchFeeResponse 获取手续费率响应
type FetchFeeResponse struct {
	MakerFee  float64 `json:"maker_fee"`
	TakerFee  float64 `json:"taker_fee"`
	FeeSource string  `json:"fee_source"` // "exchange_api" | "config" | "default"
	Exchange  string  `json:"exchange"`
	Symbol    string  `json:"symbol"`
}

// getParamAdvisor 获取参数建议 GET /api/config/param-advisor
func getParamAdvisor(c *gin.Context) {
	exchangeName := c.DefaultQuery("exchange", "binance")
	symbol := c.Query("symbol")
	if symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 symbol 参数"})
		return
	}

	// 用户可手動传入 maker/taker fee，若有则直接使用
	makerFeeStr := c.Query("maker_fee")
	takerFeeStr := c.Query("taker_fee")

	// 1. 获取当前价格
	price, err := fetchCurrentPrice(exchangeName, symbol)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("获取价格失败: %v", err)})
		return
	}

	// 2. 获取手续费率
	var makerFee, takerFee float64
	feeSource := "default"

	if makerFeeStr != "" && takerFeeStr != "" {
		// 用户直接传入
		makerFee, _ = strconv.ParseFloat(makerFeeStr, 64)
		takerFee, _ = strconv.ParseFloat(takerFeeStr, 64)
		feeSource = "user_input"
	} else {
		// 先尝试从配置获取
		makerFee, takerFee, feeSource = getFeeRateFromConfig(exchangeName)
	}

	// 3. 计算参数建议
	suggestions := calculateSuggestions(price, makerFee, takerFee, exchangeName)

	resp := ParamAdvisorResponse{
		CurrentPrice: price,
		MakerFee:     makerFee,
		TakerFee:     takerFee,
		FeeSource:    feeSource,
		Exchange:     exchangeName,
		Symbol:       symbol,
		Suggestions:  suggestions,
	}
	c.JSON(http.StatusOK, resp)
}

// getExchangeFees 从交易所 API 获取手续费率 GET /api/config/exchange-fees
func getExchangeFees(c *gin.Context) {
	exchangeName := c.DefaultQuery("exchange", "binance")
	symbol := c.Query("symbol")
	if symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 symbol 参数"})
		return
	}

	makerFee, takerFee, err := fetchFeeFromExchangeAPI(exchangeName, symbol)
	if err != nil {
		// 回退到配置值
		configMaker, configTaker, source := getFeeRateFromConfig(exchangeName)
		c.JSON(http.StatusOK, FetchFeeResponse{
			MakerFee:  configMaker,
			TakerFee:  configTaker,
			FeeSource: source,
			Exchange:  exchangeName,
			Symbol:    symbol,
		})
		return
	}

	c.JSON(http.StatusOK, FetchFeeResponse{
		MakerFee:  makerFee,
		TakerFee:  takerFee,
		FeeSource: "exchange_api",
		Exchange:  exchangeName,
		Symbol:    symbol,
	})
}

// fetchCurrentPrice 获取当前价格（支持多交易所）
func fetchCurrentPrice(exchangeName, symbol string) (float64, error) {
	switch exchangeName {
	case "binance":
		return fetchBinancePrice(symbol)
	case "bitget":
		return fetchBitgetPrice(symbol)
	case "bybit":
		return fetchBybitPrice(symbol)
	case "okx":
		return fetchOKXPrice(symbol)
	default:
		// 默认尝试 Binance
		return fetchBinancePrice(symbol)
	}
}

// fetchBinancePrice 从 Binance 获取价格
func fetchBinancePrice(symbol string) (float64, error) {
	url := fmt.Sprintf("https://api.binance.com/api/v3/ticker/price?symbol=%s", symbol)
	resp, err := http.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var data struct {
		Price string `json:"price"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return 0, fmt.Errorf("解析价格失败")
	}
	price, err := strconv.ParseFloat(data.Price, 64)
	if err != nil || price <= 0 {
		return 0, fmt.Errorf("无效价格")
	}
	return price, nil
}

// fetchBitgetPrice 从 Bitget 获取价格
func fetchBitgetPrice(symbol string) (float64, error) {
	// Bitget futures 用 USDT 结尾的品种名
	url := fmt.Sprintf("https://api.bitget.com/api/v2/mix/market/ticker?productType=USDT-FUTURES&symbol=%s", symbol)
	resp, err := http.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var data struct {
		Data []struct {
			LastPr string `json:"lastPr"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &data); err != nil || len(data.Data) == 0 {
		// 回退到 spot
		return fetchBitgetSpotPrice(symbol)
	}
	price, err := strconv.ParseFloat(data.Data[0].LastPr, 64)
	if err != nil || price <= 0 {
		return fetchBitgetSpotPrice(symbol)
	}
	return price, nil
}

func fetchBitgetSpotPrice(symbol string) (float64, error) {
	url := fmt.Sprintf("https://api.bitget.com/api/v2/spot/market/tickers?symbol=%s", symbol)
	resp, err := http.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var data struct {
		Data []struct {
			LastPr string `json:"lastPr"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &data); err != nil || len(data.Data) == 0 {
		return 0, fmt.Errorf("无法获取 Bitget 价格")
	}
	price, err := strconv.ParseFloat(data.Data[0].LastPr, 64)
	if err != nil || price <= 0 {
		return 0, fmt.Errorf("无效 Bitget 价格")
	}
	return price, nil
}

// fetchBybitPrice 从 Bybit 获取价格
func fetchBybitPrice(symbol string) (float64, error) {
	url := fmt.Sprintf("https://api.bybit.com/v5/market/tickers?category=linear&symbol=%s", symbol)
	resp, err := http.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var data struct {
		Result struct {
			List []struct {
				LastPrice string `json:"lastPrice"`
			} `json:"list"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &data); err != nil || len(data.Result.List) == 0 {
		return 0, fmt.Errorf("无法获取 Bybit 价格")
	}
	price, err := strconv.ParseFloat(data.Result.List[0].LastPrice, 64)
	if err != nil || price <= 0 {
		return 0, fmt.Errorf("无效 Bybit 价格")
	}
	return price, nil
}

// fetchOKXPrice 从 OKX 获取价格
func fetchOKXPrice(symbol string) (float64, error) {
	// OKX 使用 BTC-USDT 格式，需转换
	instId := convertToOKXInstID(symbol)
	url := fmt.Sprintf("https://www.okx.com/api/v5/market/ticker?instId=%s", instId)
	resp, err := http.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var data struct {
		Data []struct {
			Last string `json:"last"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &data); err != nil || len(data.Data) == 0 {
		return 0, fmt.Errorf("无法获取 OKX 价格")
	}
	price, err := strconv.ParseFloat(data.Data[0].Last, 64)
	if err != nil || price <= 0 {
		return 0, fmt.Errorf("无效 OKX 价格")
	}
	return price, nil
}

// convertToOKXInstID 将 BTCUSDT 格式转为 BTC-USDT-SWAP
func convertToOKXInstID(symbol string) string {
	// 简单的转换逻辑
	if len(symbol) > 4 {
		base := symbol[:len(symbol)-4]
		quote := symbol[len(symbol)-4:]
		if quote == "USDT" || quote == "USDC" || quote == "U" {
			return base + "-" + quote + "-SWAP"
		}
	}
	return symbol
}

// fetchFeeFromExchangeAPI 从交易所 API 获取手续费率（與 Bot 啟動時邏輯共用 feerate 包）
func fetchFeeFromExchangeAPI(exchangeName, symbol string) (makerFee, takerFee float64, err error) {
	if globalConfig == nil {
		return 0, 0, fmt.Errorf("全局配置未初始化")
	}
	return feerate.FetchFromExchangeAPI(globalConfig, exchangeName, symbol)
}

// getFeeRateFromConfig 从配置中获取手续费率
func getFeeRateFromConfig(exchangeName string) (makerFee, takerFee float64, source string) {
	if globalConfig != nil {
		cfg := globalConfig
		if cfg != nil {
			if exCfg, ok := cfg.Exchanges[exchangeName]; ok {
				if exCfg.FeeRate > 0 {
					// 配置中的 fee_rate 是单一值，视为 taker 费率；maker 取其 40% 作为默认估算
					return exCfg.FeeRate * 0.4, exCfg.FeeRate, "config"
				}
			}
		}
	}
	// 返回行业默认值
	return getDefaultFeeRates(exchangeName)
}

// getDefaultFeeRates 获取默认手续费率（行业标准估计值）
func getDefaultFeeRates(exchangeName string) (makerFee, takerFee float64, source string) {
	switch exchangeName {
	case "binance":
		return 0.0002, 0.0005, "default" // 0.02% / 0.05%
	case "bitget":
		return 0.0002, 0.0006, "default" // 0.02% / 0.06%
	case "bybit":
		return 0.0002, 0.00055, "default" // 0.02% / 0.055%
	case "okx":
		return 0.0002, 0.0005, "default" // 0.02% / 0.05%
	default:
		return 0.0002, 0.0005, "default"
	}
}

// calculateSuggestions 根据价格和手续费率计算参数建议
func calculateSuggestions(price, makerFee, takerFee float64, exchangeName string) ParamSuggestion {
	if price <= 0 {
		return ParamSuggestion{}
	}

	// 确保费率有效
	if makerFee <= 0 {
		makerFee = 0.0002
	}
	if takerFee <= 0 {
		takerFee = 0.0005
	}

	// ======== Price Interval 建议 ========
	// 盈亏平衡: 一个完整交易的买入+卖出手续费
	// 保守估算: 买卖都用 taker 费率
	totalFeeRate := makerFee + takerFee
	breakEvenInterval := price * totalFeeRate

	// 建议区间:
	// - Min: 2x 盈亏平衡（留足安全边际）
	// - Recommended: 3x 盈亏平衡（较好的收益/频率平衡）
	// - Max: 基于价格百分比 (0.5%)
	minInterval := roundToSignificant(breakEvenInterval*2, price)
	recommendedInterval := roundToSignificant(breakEvenInterval*3, price)
	maxInterval := roundToSignificant(price*0.005, price)

	// 确保 min < recommended < max
	if recommendedInterval <= minInterval {
		recommendedInterval = minInterval * 1.5
	}
	if maxInterval <= recommendedInterval {
		maxInterval = recommendedInterval * 2
	}

	// ======== Order Quantity 建议 ========
	// 最低单笔金额：确保手续费不会过高（至少赚 0.1 USDT per trade）
	minProfit := 0.1 // 每笔最低期望利润 0.1 USDT
	// 期望利润 = order_qty * (interval/price - totalFeeRate)
	// 用 recommended interval 计算
	profitPerUnit := recommendedInterval/price - totalFeeRate
	var minOrderQty float64
	if profitPerUnit > 0 {
		minOrderQty = minProfit / profitPerUnit
	} else {
		minOrderQty = 100
	}

	// 交易所最低订单限制
	exchangeMinOrder := getExchangeMinOrder(exchangeName)
	if minOrderQty < exchangeMinOrder {
		minOrderQty = exchangeMinOrder
	}

	// 推荐金额和最大金额
	recommendedOrderQty := math.Max(minOrderQty*2, exchangeMinOrder*2)
	maxOrderQty := math.Max(recommendedOrderQty*5, 1000)

	// 取整到合理的步长
	minOrderQty = roundOrderQty(minOrderQty)
	recommendedOrderQty = roundOrderQty(recommendedOrderQty)
	maxOrderQty = roundOrderQty(maxOrderQty)

	return ParamSuggestion{
		PriceInterval: RangeAdvice{
			Min:         minInterval,
			Recommended: recommendedInterval,
			Max:         maxInterval,
		},
		OrderQuantity: RangeAdvice{
			Min:         minOrderQty,
			Recommended: recommendedOrderQty,
			Max:         maxOrderQty,
		},
		MinProfitableInterval: roundToSignificant(breakEvenInterval, price),
		BreakevenFeeRate:      totalFeeRate,
	}
}

// getExchangeMinOrder 获取交易所最低订单金额 (USDT)
func getExchangeMinOrder(exchangeName string) float64 {
	switch exchangeName {
	case "binance":
		return 100 // Binance 合约最低 100 USDT
	case "bitget":
		return 5 // Bitget 最低 5 USDT
	case "bybit":
		return 5 // Bybit 最低 5 USDT
	case "okx":
		return 5 // OKX 最低约 5 USDT
	default:
		return 10
	}
}

// roundToSignificant 将 interval 四舍五入到合理精度
func roundToSignificant(value, price float64) float64 {
	if value <= 0 || price <= 0 {
		return value
	}

	// 根据价格量级确定精度
	if price >= 10000 {
		// BTC 等高价品种，精确到整数或十位
		if value >= 100 {
			return math.Round(value/10) * 10
		}
		return math.Round(value)
	} else if price >= 100 {
		// ETH 等中价品种，精确到小数点后1位
		return math.Round(value*10) / 10
	} else if price >= 1 {
		// 中低价品种
		return math.Round(value*100) / 100
	}
	// 低价品种，精确到小数点后4位
	return math.Round(value*10000) / 10000
}

// roundOrderQty 将订单金额取整到合理步长
func roundOrderQty(qty float64) float64 {
	if qty <= 10 {
		return math.Ceil(qty)
	} else if qty <= 100 {
		return math.Ceil(qty/5) * 5
	} else if qty <= 500 {
		return math.Ceil(qty/10) * 10
	}
	return math.Ceil(qty/50) * 50
}
