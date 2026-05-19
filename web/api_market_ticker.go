package web

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// MarketTickerResponse 市场行情响应（当前价、标记价、24h 高低）
type MarketTickerResponse struct {
	MarkPrice  float64 `json:"mark_price"` // 标记价格（合约）或最新价（现货）
	LastPrice  float64 `json:"last_price"` // 最新成交价
	High24h    float64 `json:"high_24h"`   // 24h 最高价（波峰）
	Low24h     float64 `json:"low_24h"`    // 24h 最低价（波谷）
	Exchange   string  `json:"exchange"`
	Symbol     string  `json:"symbol"`
	MarketType string  `json:"market_type"` // spot | futures
}

// getMarketTicker 获取市场行情 GET /api/market/ticker?exchange=&symbol=&market_type=
func getMarketTicker(c *gin.Context) {
	exchangeName := c.DefaultQuery("exchange", "binance")
	symbol := c.Query("symbol")
	marketType := c.DefaultQuery("market_type", "futures")
	if symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 symbol 参数"})
		return
	}
	if marketType != "spot" && marketType != "futures" {
		marketType = "futures"
	}

	var markPrice, lastPrice, high24h, low24h float64
	var err error
	ctx := c.Request.Context()
	switch exchangeName {
	case "binance":
		markPrice, lastPrice, high24h, low24h, err = fetchBinanceTicker(ctx, symbol, marketType)
	case "bitget":
		markPrice, lastPrice, high24h, low24h, err = fetchBitgetTicker(ctx, symbol, marketType)
	case "bybit":
		markPrice, lastPrice, high24h, low24h, err = fetchBybitTicker(ctx, symbol, marketType)
	case "okx":
		markPrice, lastPrice, high24h, low24h, err = fetchOKXTicker(ctx, symbol, marketType)
	default:
		markPrice, lastPrice, high24h, low24h, err = fetchBinanceTicker(ctx, symbol, marketType)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("获取行情失败: %v", err)})
		return
	}

	c.JSON(http.StatusOK, MarketTickerResponse{
		MarkPrice:  markPrice,
		LastPrice:  lastPrice,
		High24h:    high24h,
		Low24h:     low24h,
		Exchange:   exchangeName,
		Symbol:     symbol,
		MarketType: marketType,
	})
}

func fetchBinanceTicker(ctx context.Context, symbol, marketType string) (mark, last, high24h, low24h float64, err error) {
	if marketType == "spot" {
		url := fmt.Sprintf("https://api.binance.com/api/v3/ticker/24hr?symbol=%s", symbol)
		var data struct {
			LastPrice string `json:"lastPrice"`
			HighPrice string `json:"highPrice"`
			LowPrice  string `json:"lowPrice"`
		}
		if e := fetchPublicJSON(ctx, url, &data); e != nil {
			return 0, 0, 0, 0, fmt.Errorf("解析 spot ticker 失败")
		}
		last, _ = strconv.ParseFloat(data.LastPrice, 64)
		high24h, _ = strconv.ParseFloat(data.HighPrice, 64)
		low24h, _ = strconv.ParseFloat(data.LowPrice, 64)
		mark = last // 现货无标记价，用最新价
		return mark, last, high24h, low24h, nil
	}
	// futures: 24hr + premiumIndex
	url24 := fmt.Sprintf("https://fapi.binance.com/fapi/v1/ticker/24hr?symbol=%s", symbol)
	var data24 struct {
		LastPrice string `json:"lastPrice"`
		HighPrice string `json:"highPrice"`
		LowPrice  string `json:"lowPrice"`
	}
	if e := fetchPublicJSON(ctx, url24, &data24); e != nil {
		return 0, 0, 0, 0, fmt.Errorf("解析 futures 24hr 失败")
	}
	last, _ = strconv.ParseFloat(data24.LastPrice, 64)
	high24h, _ = strconv.ParseFloat(data24.HighPrice, 64)
	low24h, _ = strconv.ParseFloat(data24.LowPrice, 64)

	urlMark := fmt.Sprintf("https://fapi.binance.com/fapi/v1/premiumIndex?symbol=%s", symbol)
	var dataMark struct {
		MarkPrice string `json:"markPrice"`
	}
	if e := fetchPublicJSON(ctx, urlMark, &dataMark); e != nil {
		mark = last
		return mark, last, high24h, low24h, nil
	}
	if dataMark.MarkPrice != "" {
		mark, _ = strconv.ParseFloat(dataMark.MarkPrice, 64)
	} else {
		mark = last
	}
	return mark, last, high24h, low24h, nil
}

func fetchBitgetTicker(ctx context.Context, symbol, marketType string) (mark, last, high24h, low24h float64, err error) {
	if marketType == "spot" {
		url := fmt.Sprintf("https://api.bitget.com/api/v2/spot/market/tickers?symbol=%s", symbol)
		var data struct {
			Data []struct {
				LastPr string `json:"lastPr"`
				High24 string `json:"high24h"`
				Low24  string `json:"low24h"`
			} `json:"data"`
		}
		if e := fetchPublicJSON(ctx, url, &data); e != nil || len(data.Data) == 0 {
			return 0, 0, 0, 0, fmt.Errorf("无法获取 Bitget spot ticker")
		}
		d := data.Data[0]
		last, _ = strconv.ParseFloat(d.LastPr, 64)
		high24h, _ = strconv.ParseFloat(d.High24, 64)
		low24h, _ = strconv.ParseFloat(d.Low24, 64)
		mark = last
		return mark, last, high24h, low24h, nil
	}
	url := fmt.Sprintf("https://api.bitget.com/api/v2/mix/market/ticker?productType=USDT-FUTURES&symbol=%s", symbol)
	var data struct {
		Data []struct {
			LastPr    string `json:"lastPr"`
			High24    string `json:"high24h"`
			Low24     string `json:"low24h"`
			MarkPrice string `json:"markPrice"`
		} `json:"data"`
	}
	if e := fetchPublicJSON(ctx, url, &data); e != nil || len(data.Data) == 0 {
		return 0, 0, 0, 0, fmt.Errorf("无法获取 Bitget futures ticker")
	}
	d := data.Data[0]
	last, _ = strconv.ParseFloat(d.LastPr, 64)
	high24h, _ = strconv.ParseFloat(d.High24, 64)
	low24h, _ = strconv.ParseFloat(d.Low24, 64)
	if d.MarkPrice != "" {
		mark, _ = strconv.ParseFloat(d.MarkPrice, 64)
	} else {
		mark = last
	}
	return mark, last, high24h, low24h, nil
}

func fetchBybitTicker(ctx context.Context, symbol, marketType string) (mark, last, high24h, low24h float64, err error) {
	category := "linear"
	if marketType == "spot" {
		category = "spot"
	}
	url := fmt.Sprintf("https://api.bybit.com/v5/market/tickers?category=%s&symbol=%s", category, symbol)
	var data struct {
		Result struct {
			List []struct {
				LastPrice    string `json:"lastPrice"`
				HighPrice24h string `json:"highPrice24h"`
				LowPrice24h  string `json:"lowPrice24h"`
				MarkPrice    string `json:"markPrice"`
			} `json:"list"`
		} `json:"result"`
	}
	if e := fetchPublicJSON(ctx, url, &data); e != nil || len(data.Result.List) == 0 {
		return 0, 0, 0, 0, fmt.Errorf("无法获取 Bybit ticker")
	}
	d := data.Result.List[0]
	last, _ = strconv.ParseFloat(d.LastPrice, 64)
	high24h, _ = strconv.ParseFloat(d.HighPrice24h, 64)
	low24h, _ = strconv.ParseFloat(d.LowPrice24h, 64)
	if d.MarkPrice != "" {
		mark, _ = strconv.ParseFloat(d.MarkPrice, 64)
	} else {
		mark = last
	}
	return mark, last, high24h, low24h, nil
}

func fetchOKXTicker(ctx context.Context, symbol, marketType string) (mark, last, high24h, low24h float64, err error) {
	var instId string
	if marketType == "spot" {
		if len(symbol) > 4 {
			instId = symbol[:len(symbol)-4] + "-" + symbol[len(symbol)-4:]
		} else {
			instId = symbol
		}
	} else {
		instId = convertToOKXInstID(symbol)
	}
	url := fmt.Sprintf("https://www.okx.com/api/v5/market/ticker?instId=%s", instId)
	var data struct {
		Data []struct {
			Last    string `json:"last"`
			High24h string `json:"high24h"`
			Low24h  string `json:"low24h"`
			SodUtc8 string `json:"sodUtc8"` // 部分接口用不同字段
		} `json:"data"`
	}
	if e := fetchPublicJSON(ctx, url, &data); e != nil || len(data.Data) == 0 {
		return 0, 0, 0, 0, fmt.Errorf("无法获取 OKX ticker")
	}
	d := data.Data[0]
	last, _ = strconv.ParseFloat(d.Last, 64)
	high24h, _ = strconv.ParseFloat(d.High24h, 64)
	low24h, _ = strconv.ParseFloat(d.Low24h, 64)
	mark = last // OKX ticker 可能无单独 mark，用 last
	return mark, last, high24h, low24h, nil
}
