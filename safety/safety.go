package safety

import (
	"context"
	"fmt"
	"opensqt/exchange"
	"opensqt/logger"
)

const (
	MaxLeverage = 10 // 最大允许杠杆倍数（硬编码）
)

// CheckAccountSafety 检查账户安全性（支持所有交易所）
// 参数：
//   - ex: 交易所接口
//   - symbol: 交易对
//   - currentPrice: 当前币价
//   - orderAmount: 每笔交易金额（USDT/USDC）
//   - priceInterval: 价格间隔（买入价和卖出价的差值）
//   - feeRate: 手续费率
//   - requiredPositions: 要求的最少持仓数量（默认100）
//   - priceDecimals: 价格小数位数（用于格式化显示）
func CheckAccountSafety(ex exchange.IExchange, symbol string, currentPrice, orderAmount, priceInterval, feeRate float64, requiredPositions, priceDecimals int) error {
	logger.Info("🔒 ===== 开始持仓安全性检查 =====")

	// 从交易所接口获取计价币种（支持U本位和币本位合约）
	quoteCurrency := ex.GetQuoteAsset()

	// 1. 获取账户信息
	ctx := context.Background()
	account, err := ex.GetAccount(ctx)
	if err != nil {
		return fmt.Errorf("获取账户信息失败: %w", err)
	}

	// 2. 获取交易对的杠杆倍数和持仓信息
	var leverage int = 1 // 默认1倍杠杆
	var positionAmt float64 = 0

	// 尝试获取持仓信息
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

	// 如果持仓中没有找到杠杆倍数，尝试从账户信息中获取
	if leverage == 1 && account.AccountLeverage > 0 {
		leverage = account.AccountLeverage
		logger.Info("ℹ️ 从账户信息中获取杠杆倍数: %dx", leverage)
	}

	// 🔥 如果当前账户有持仓，跳过安全检查（认为用户知道风险）
	if positionAmt != 0 {
		logger.Info("⚠️ 检测到当前持仓: %.4f，跳过安全性检查", positionAmt)
		logger.Info("🔒 ===== 持仓安全性检查完成（已跳过） =====")
		return nil
	}
	accountBalance := account.AvailableBalance
	if accountBalance <= 0 {
		return fmt.Errorf("账户余额不足，当前余额: %.2f %s", accountBalance, quoteCurrency)
	}
	logger.Info("💰 账户余额: %.2f %s (交易对: %s)", accountBalance, quoteCurrency, symbol)
	// 如果是币安交易所，尝试获取更准确的杠杆信息
	exchangeName := ex.GetName()
	if leverage == 1 && exchangeName == "Binance" {
		// 尝试通过币安特定的方法获取杠杆（如果获取失败也没关系，使用默认值）
		if binanceLeverage := tryGetBinanceLeverage(ex, symbol); binanceLeverage > 0 {
			leverage = binanceLeverage
		}
	}

	logger.Info("📊 交易所: %s, 交易对: %s, 当前杠杆倍数: %dx, 当前持仓: %.4f", exchangeName, symbol, leverage, positionAmt)

	// 3. 强制杠杆倍数检查（硬编码最多10倍）
	if leverage > MaxLeverage {
		return fmt.Errorf("您的账户杠杆倍率太高（%dx），风险太大，禁止开仓。最大允许杠杆倍数: %dx", leverage, MaxLeverage)
	}

	// 4. 计算最大可持有仓位
	// 🔥 固定金额模式：orderAmount 是每笔交易的金额（USDT/USDC）
	// 公式：最大可持有仓位 = (账户余额 * 杠杆倍数) / 每笔金额
	// 例如：余额3000，杠杆10倍，每笔投入30U
	// 最大可持有 = (3000 * 10) / 30 = 1000仓
	maxAvailableMargin := accountBalance * float64(leverage)
	costPerPosition := orderAmount // 每仓成本就是配置的金额
	maxPositions := maxAvailableMargin / costPerPosition

	// 如果未设置小数位数，使用默认值2
	if priceDecimals <= 0 {
		priceDecimals = 2
	}

	// 根据当前价格计算实际购买数量（用于显示）
	orderQuantity := orderAmount / currentPrice

	logger.Info("📈 当前币价: %.*f, 每笔金额: %.2f %s, 每笔数量: %.4f", priceDecimals, currentPrice, orderAmount, quoteCurrency, orderQuantity)
	logger.Info("💵 最大可用保证金: %.2f %s (余额 %.2f × 杠杆 %dx)", maxAvailableMargin, quoteCurrency, accountBalance, leverage)
	logger.Info("📦 每仓成本: %.2f %s (固定金额模式)", costPerPosition, quoteCurrency)
	logger.Info("🎯 最大可持有仓位: %.0f 仓", maxPositions)
	logger.Info("✅ 要求最少持有: %d 仓", requiredPositions)

	// 5. 验证是否满足要求
	if maxPositions < float64(requiredPositions) {
		return fmt.Errorf("持仓安全检查失败：您的账户余额不足，请补充足够保证金或调整配置参数，最少足够向下购买持有 %d 仓。当前最大可持有: %.0f 仓", requiredPositions, maxPositions)
	}

	logger.Info("✅ 持仓安全性检查通过：可以安全持有至少 %d 仓", requiredPositions)

	// 6. 手续费率安全检查
	buyFeeRate := feeRate
	sellFeeRate := feeRate

	logger.Info("💳 手续费率检查: 交易对=%s, 买入费率=%.4f%%, 卖出费率=%.4f%%",
		symbol, buyFeeRate*100, sellFeeRate*100)

	// 计算每笔交易的利润和手续费
	// 🔥 固定金额模式：每笔买入金额固定，数量根据价格动态计算
	buyPrice := currentPrice
	sellPrice := currentPrice + priceInterval

	// 买入时：投入固定金额，买到的数量 = orderAmount / buyPrice
	buyQuantity := orderAmount / buyPrice
	// 卖出时：卖出价 = buyPrice + priceInterval
	sellQuantity := buyQuantity // 卖出数量等于买入数量

	// 利润 = 卖出金额 - 买入金额
	buyAmount := orderAmount               // 买入金额固定
	sellAmount := sellPrice * sellQuantity // 卖出金额
	profitPerTrade := sellAmount - buyAmount

	// 手续费 = 买入手续费 + 卖出手续费
	buyFee := buyAmount * buyFeeRate
	sellFee := sellAmount * sellFeeRate
	totalFee := buyFee + sellFee

	// 计算总手续费率（买入费率 + 卖出费率）
	totalFeeRate := buyFeeRate + sellFeeRate

	// 计算利润占买入价的比例（利润率）
	profitRate := priceInterval / buyPrice

	logger.Info("💰 每笔交易分析 (固定金额模式):")
	logger.Info("   买入价: %.*f, 卖出价: %.*f, 价格差: %.*f", priceDecimals, buyPrice, priceDecimals, sellPrice, priceDecimals, priceInterval)
	logger.Info("   买入金额: %.2f %s, 买入数量: %.4f", buyAmount, quoteCurrency, buyQuantity)
	logger.Info("   卖出金额: %.2f %s, 卖出数量: %.4f", sellAmount, quoteCurrency, sellQuantity)
	logger.Info("   每笔利润: %.4f %s (卖出 %.2f - 买入 %.2f)", profitPerTrade, quoteCurrency, sellAmount, buyAmount)
	logger.Info("   利润率: %.4f%% (价格差 %.*f / 买入价 %.*f)", profitRate*100, priceDecimals, priceInterval, priceDecimals, buyPrice)
	logger.Info("   买入手续费: %.4f %s (金额 %.2f × 费率 %.4f%%)", buyFee, quoteCurrency, buyAmount, buyFeeRate*100)
	logger.Info("   卖出手续费: %.4f %s (金额 %.2f × 费率 %.4f%%)", sellFee, quoteCurrency, sellAmount, sellFeeRate*100)
	logger.Info("   总手续费: %.4f %s (费率: %.4f%%)", totalFee, quoteCurrency, totalFeeRate*100)

	netProfit := profitPerTrade - totalFee
	logger.Info("   净利润: %.4f %s (利润 %.4f - 手续费 %.4f)", netProfit, quoteCurrency, profitPerTrade, totalFee)

	// 验证利润是否足够支付手续费（净利润必须为正）
	if netProfit <= 0 {
		logger.Error("❌ 错误：每笔净利润为负或为零 (%.4f %s)，无法盈利！", netProfit, quoteCurrency)
		logger.Error("   建议：增加价格间隔或降低手续费率")
		logger.Error("   当前价格间隔: %.*f, 手续费率: %.4f%%", priceDecimals, priceInterval, totalFeeRate*100)
		return fmt.Errorf("每笔净利润为负或为零 (%.4f %s)，系统拒绝启动", netProfit, quoteCurrency)
	}

	logger.Info("✅ 手续费率安全检查通过：每笔净利润 %.4f %s", netProfit, quoteCurrency)

	logger.Info("🔒 ===== 持仓安全性检查完成 =====")

	return nil
}

// tryGetBinanceLeverage 尝试获取币安的杠杆信息（可选功能，失败不影响主流程）
func tryGetBinanceLeverage(ex exchange.IExchange, symbol string) int {
	// 由于币安适配器可能有特定的方法，这里我们通过反射或类型断言来获取
	// 如果失败，返回0表示无法获取

	// 这里可以根据实际情况实现，暂时返回0让其使用默认值
	// 后续可以扩展：通过反射或扩展接口来获取特定交易所的杠杆信息

	return 0 // 表示无法获取，使用默认值
}
