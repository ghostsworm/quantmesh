package feerate

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"quantmesh/config"
	"quantmesh/logger"
)

// FetchFromExchangeAPI 使用配置中的 API 密鑰從交易所拉取用戶實際 Maker/Taker 費率（目前支援 binance、bitget）。
func FetchFromExchangeAPI(cfg *config.Config, exchangeName, symbol string) (makerFee, takerFee float64, err error) {
	if cfg == nil {
		return 0, 0, fmt.Errorf("配置為空")
	}
	exCfg, ok := cfg.Exchanges[exchangeName]
	if !ok {
		return 0, 0, fmt.Errorf("未找到交易所 %s 配置", exchangeName)
	}
	if exCfg.APIKey == "" || exCfg.SecretKey == "" {
		return 0, 0, fmt.Errorf("交易所 API 密钥未配置")
	}
	switch exchangeName {
	case "binance":
		return fetchBinanceFeeRate(exCfg.APIKey, exCfg.SecretKey, symbol)
	case "bitget":
		return fetchBitgetFeeRate(exCfg.APIKey, exCfg.SecretKey, exCfg.Passphrase, symbol)
	default:
		return 0, 0, fmt.Errorf("暂不支持从 %s 自动获取费率", exchangeName)
	}
}

func fetchBinanceFeeRate(apiKey, secretKey, symbol string) (makerFee, takerFee float64, err error) {
	timestamp := time.Now().UnixMilli()
	queryString := fmt.Sprintf("symbol=%s&timestamp=%d", symbol, timestamp)

	mac := hmac.New(sha256.New, []byte(secretKey))
	mac.Write([]byte(queryString))
	signature := hex.EncodeToString(mac.Sum(nil))

	url := fmt.Sprintf("https://fapi.binance.com/fapi/v1/commissionRate?%s&signature=%s", queryString, signature)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, 0, err
	}
	req.Header.Set("X-MBX-APIKEY", apiKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		logger.Warn("Binance commissionRate API 返回 %d: %s", resp.StatusCode, string(body))
		return 0, 0, fmt.Errorf("API 返回 %d", resp.StatusCode)
	}

	var data struct {
		MakerCommissionRate string `json:"makerCommissionRate"`
		TakerCommissionRate string `json:"takerCommissionRate"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return 0, 0, fmt.Errorf("解析费率失败: %v", err)
	}

	makerFee, _ = strconv.ParseFloat(data.MakerCommissionRate, 64)
	takerFee, _ = strconv.ParseFloat(data.TakerCommissionRate, 64)
	return makerFee, takerFee, nil
}

func fetchBitgetFeeRate(apiKey, secretKey, passphrase, symbol string) (makerFee, takerFee float64, err error) {
	timestamp := fmt.Sprintf("%d", time.Now().UnixMilli())
	path := fmt.Sprintf("/api/v2/common/trade-rate?symbol=%s&businessType=futures", symbol)
	message := timestamp + "GET" + path

	mac := hmac.New(sha256.New, []byte(secretKey))
	mac.Write([]byte(message))
	signature := hex.EncodeToString(mac.Sum(nil))

	url := "https://api.bitget.com" + path
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, 0, err
	}
	req.Header.Set("ACCESS-KEY", apiKey)
	req.Header.Set("ACCESS-SIGN", signature)
	req.Header.Set("ACCESS-PASSPHRASE", passphrase)
	req.Header.Set("ACCESS-TIMESTAMP", timestamp)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		logger.Warn("Bitget trade-rate API 返回 %d: %s", resp.StatusCode, string(body))
		return 0, 0, fmt.Errorf("API 返回 %d", resp.StatusCode)
	}

	var data struct {
		Data struct {
			MakerFeeRate string `json:"makerFeeRate"`
			TakerFeeRate string `json:"takerFeeRate"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return 0, 0, fmt.Errorf("解析费率失败: %v", err)
	}

	makerFee, _ = strconv.ParseFloat(data.Data.MakerFeeRate, 64)
	takerFee, _ = strconv.ParseFloat(data.Data.TakerFeeRate, 64)
	return makerFee, takerFee, nil
}
