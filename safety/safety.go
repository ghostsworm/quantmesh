package safety

import (
	"context"
	"fmt"
	"strings"

	"quantmesh/exchange"
	"quantmesh/logger"
)

const (
	DefaultMaxLeverage = 10 // 默认最大允許杠杆倍數
)

// CheckAccountSafety 检查账戶安全性（支援所有交易所）
// 参數：
//   - ex: 交易所接口
//   - symbol: 交易對
//   - currentPrice: 當前币價
//   - orderAmount: 每笔交易金額（USDT/USDC）
//   - priceInterval: 價格間隔（買入價和賣出價的差值）
//   - feeRate: 手续费率
//   - requiredPositions: 要求的最少持倉數量（預設見 config.DefaultPositionSafetyCheck）
//   - priceDecimals: 價格小數位數（用於格式化显示）
//   - maxLeverage: 最大允許杠杆倍數（0表示不限制，默认使用DefaultMaxLeverage）
func CheckAccountSafety(ex exchange.IExchange, symbol string, currentPrice, orderAmount, priceInterval, feeRate float64, requiredPositions, priceDecimals int, maxLeverage int) error {
	logger.Info("🔒 ===== 开始持倉安全性检查 =====")

	// 從交易所接口獲取计價币种（支援U本位和币本位合約）
	quoteCurrency := ex.GetQuoteAsset()

	// 1. 獲取帳戶信息
	ctx := context.Background()
	account, err := ex.GetAccount(ctx)
	if err != nil {
		return fmt.Errorf("獲取帳戶信息失败: %w%s", err, accountAPIErrorHint(err))
	}

	// 2. 獲取交易對的杠杆倍數和持倉資訊
	var leverage int = 1 // 默认1倍杠杆
	var positionAmt float64 = 0

	// 尝試獲取持倉信息
	positions, err := ex.GetPositions(ctx, symbol)
	if err == nil && positions != nil {
		for _, p := range positions {
			if p.Symbol == symbol {
				positionAmt = p.Size
				if p.Leverage > 0 {
					leverage = p.Leverage
				}
				break
			}
		}
	}

	// 如果持倉中没有找到杠杆倍數，尝試從帳戶資訊中獲取
	if leverage == 1 && account.AccountLeverage > 0 {
		leverage = account.AccountLeverage
		logger.Info("ℹ️ 從帳戶資訊中獲取杠杆倍數: %dx", leverage)
	}

	// 🔥 如果當前账戶有持倉，跳過安全检查（认為用戶知道风險）
	if positionAmt != 0 {
		logger.Info("⚠️ 检测到當前持倉: %.4f，跳過安全性检查", positionAmt)
		logger.Info("🔒 ===== 持倉安全性检查完成（已跳過） =====")
		return nil
	}
	accountBalance := account.AvailableBalance
	if accountBalance <= 0 {
		return fmt.Errorf("账戶餘額不足，當前餘額: %.2f %s", accountBalance, quoteCurrency)
	}
	logger.Info("💰 账戶餘額: %.2f %s (交易對: %s)", accountBalance, quoteCurrency, symbol)
	// 如果是币安交易所，尝試獲取更准确的杠杆信息
	exchangeName := ex.GetName()
	if leverage == 1 && exchangeName == "Binance" {
		// 尝試通過币安特定的方法獲取杠杆（如果獲取失败也没关系，使用默认值）
		if binanceLeverage := tryGetBinanceLeverage(ex, symbol); binanceLeverage > 0 {
			leverage = binanceLeverage
		}
	}

	logger.Info("📊 交易所: %s, 交易對: %s, 當前杠杆倍數: %dx, 當前持倉: %.4f", exchangeName, symbol, leverage, positionAmt)

	// 3. 强制杠杆倍數检查（現貨無杠杆，跳過此檢查並強制為 1x）
	marketType := ex.GetMarketType()
	if marketType == "spot" {
		leverage = 1
		logger.Info("ℹ️ 現貨市場，跳過杠杆倍數檢查（恒為 1x）")
	} else {
		// 如果 maxLeverage 為 0，使用默认值；如果 maxLeverage > 0，使用配置值
		effectiveMaxLeverage := maxLeverage
		if effectiveMaxLeverage == 0 {
			effectiveMaxLeverage = DefaultMaxLeverage
		}

		if effectiveMaxLeverage > 0 && leverage > effectiveMaxLeverage {
			return fmt.Errorf("您的账戶杠杆倍率太高（%dx），风險太大，禁止开倉。最大允許杠杆倍數: %dx（可在配置文件中修改 risk_control.max_leverage）", leverage, effectiveMaxLeverage)
		}
	}

	// 4. 计算最大可持有倉位
	// 🔥 固定金額模式：orderAmount 是每笔交易的金額（USDT/USDC）
	// 公式：最大可持有倉位 = (账戶餘額 * 杠杆倍數) / 每笔金額
	// 例如：餘額3000，杠杆10倍，每笔投入30U
	// 最大可持有 = (3000 * 10) / 30 = 1000倉
	maxAvailableMargin := accountBalance * float64(leverage)
	costPerPosition := orderAmount // 每倉成本就是配置的金額
	maxPositions := maxAvailableMargin / costPerPosition

	// 如果未設置小數位數，使用默认值2
	if priceDecimals <= 0 {
		priceDecimals = 2
	}

	// 根據當前價格计算實際购買數量（用於显示）
	orderQuantity := orderAmount / currentPrice

	logger.Info("📈 當前币價: %.*f, 每笔金額: %.2f %s, 每笔數量: %.4f", priceDecimals, currentPrice, orderAmount, quoteCurrency, orderQuantity)
	logger.Info("💵 最大可用保证金: %.2f %s (餘額 %.2f × 杠杆 %dx)", maxAvailableMargin, quoteCurrency, accountBalance, leverage)
	logger.Info("📦 每倉成本: %.2f %s (固定金額模式)", costPerPosition, quoteCurrency)
	logger.Info("🎯 最大可持有倉位: %.0f 倉", maxPositions)
	logger.Info("✅ 要求最少持有: %d 倉", requiredPositions)

	// 5. 驗证是否满足要求
	if maxPositions < float64(requiredPositions) {
		// 與上方公式一致：需 balance*leverage >= requiredPositions*orderAmount ⇒ 最低可用餘額 ≈ requiredPositions*orderAmount/leverage
		levF := float64(leverage)
		if levF < 1 {
			levF = 1
		}
		requiredBalanceApprox := float64(requiredPositions) * costPerPosition / levF
		notionalApprox := float64(requiredPositions) * costPerPosition
		return fmt.Errorf(
			"持倉安全检查失败：您的账戶餘額不足，请补充足够保证金或調整配置参數，最少足够向下购買持有 %d 倉。當前最大可持有: %.0f 倉。"+
				"當前可用餘額: %.2f %s；滿足 %d 倉按每倉 %.2f %s、%dx 杠杆估算約需可用餘額 %.2f %s（合約名義約 %.2f %s）。",
			requiredPositions, maxPositions,
			accountBalance, quoteCurrency,
			requiredPositions, costPerPosition, quoteCurrency, leverage, requiredBalanceApprox, quoteCurrency, notionalApprox, quoteCurrency,
		)
	}

	logger.Info("✅ 持倉安全性检查通過：可以安全持有至少 %d 倉", requiredPositions)

	// 6. 手续费率安全检查
	buyFeeRate := feeRate
	sellFeeRate := feeRate

	logger.Info("💳 手续费率检查: 交易對=%s, 買入费率=%.4f%%, 賣出费率=%.4f%%",
		symbol, buyFeeRate*100, sellFeeRate*100)

	// 计算每笔交易的利润和手续费
	// 🔥 固定金額模式：每笔買入金額固定，數量根據價格动態计算
	buyPrice := currentPrice
	sellPrice := currentPrice + priceInterval

	// 買入時：投入固定金額，買到的數量 = orderAmount / buyPrice
	buyQuantity := orderAmount / buyPrice
	// 賣出時：賣出價 = buyPrice + priceInterval
	sellQuantity := buyQuantity // 賣出數量等於買入數量

	// 利润 = 賣出金額 - 買入金額
	buyAmount := orderAmount               // 買入金額固定
	sellAmount := sellPrice * sellQuantity // 賣出金額
	profitPerTrade := sellAmount - buyAmount

	// 手续费 = 買入手续费 + 賣出手续费
	buyFee := buyAmount * buyFeeRate
	sellFee := sellAmount * sellFeeRate
	totalFee := buyFee + sellFee

	// 计算總手续费率（買入费率 + 賣出费率）
	totalFeeRate := buyFeeRate + sellFeeRate

	// 计算利润占買入價的比例（利润率）
	profitRate := priceInterval / buyPrice

	logger.Info("💰 每笔交易分析 (固定金額模式):")
	logger.Info("   買入價: %.*f, 賣出價: %.*f, 價格差: %.*f", priceDecimals, buyPrice, priceDecimals, sellPrice, priceDecimals, priceInterval)
	logger.Info("   買入金額: %.2f %s, 買入數量: %.4f", buyAmount, quoteCurrency, buyQuantity)
	logger.Info("   賣出金額: %.2f %s, 賣出數量: %.4f", sellAmount, quoteCurrency, sellQuantity)
	logger.Info("   每笔利润: %.4f %s (賣出 %.2f - 買入 %.2f)", profitPerTrade, quoteCurrency, sellAmount, buyAmount)
	logger.Info("   利润率: %.4f%% (價格差 %.*f / 買入價 %.*f)", profitRate*100, priceDecimals, priceInterval, priceDecimals, buyPrice)
	logger.Info("   買入手续费: %.4f %s (金額 %.2f × 费率 %.4f%%)", buyFee, quoteCurrency, buyAmount, buyFeeRate*100)
	logger.Info("   賣出手续费: %.4f %s (金額 %.2f × 费率 %.4f%%)", sellFee, quoteCurrency, sellAmount, sellFeeRate*100)
	logger.Info("   總手续费: %.4f %s (费率: %.4f%%)", totalFee, quoteCurrency, totalFeeRate*100)

	netProfit := profitPerTrade - totalFee
	logger.Info("   净利润: %.4f %s (利润 %.4f - 手续费 %.4f)", netProfit, quoteCurrency, profitPerTrade, totalFee)

	// 驗证利润是否足够支付手续费（净利润必須為正）
	if netProfit <= 0 {
		logger.Error("❌ 錯误：每笔净利润為负或為零 (%.4f %s)，無法盈利！", netProfit, quoteCurrency)
		logger.Error("   建议：增加價格間隔或降低手续费率")
		logger.Error("   當前價格间隔: %.*f, 手续费率: %.4f%%", priceDecimals, priceInterval, totalFeeRate*100)
		return fmt.Errorf("每笔净利润為负或為零 (%.4f %s)，系统拒绝啟动", netProfit, quoteCurrency)
	}

	logger.Info("✅ 手续费率安全检查通過：每笔净利润 %.4f %s", netProfit, quoteCurrency)

	logger.Info("🔒 ===== 持倉安全性检查完成 =====")

	return nil
}

// tryGetBinanceLeverage 尝試獲取币安的杠杆信息（可選功能，失败不影响主流程）
func tryGetBinanceLeverage(ex exchange.IExchange, symbol string) int {
	// 由於币安适配器可能有特定的方法，这里我们通過反射或類型断言来獲取
	// 如果失败，返回0表示無法獲取

	// 这里可以根據實際情况實現，暂時返回0让其使用默认值
	// 后续可以扩展：通過反射或扩展接口来獲取特定交易所的杠杆信息

	return 0 // 表示無法獲取，使用默认值
}

// accountAPIErrorHint 當 API 返回空響應（如 <APIError> rsp= ）時，附加排查建議
func accountAPIErrorHint(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	// go-binance 在 HTTP 4xx/5xx 且響應體非 JSON 時會輸出 <APIError> rsp=xxx
	if !strings.Contains(s, "<APIError>") {
		return ""
	}
	if idx := strings.Index(s, "rsp="); idx >= 0 {
		rsp := strings.TrimSpace(s[idx+4:])
		if rsp == "" || len(rsp) < 3 {
			return "（提示：API 返回空響應，常見原因：網絡/地區限制、API Key 與 testnet 不匹配、IP 白名單、Key 無效。詳見 rdocs/articles/zh/07-常见问题.md Q22）"
		}
	}
	return ""
}
