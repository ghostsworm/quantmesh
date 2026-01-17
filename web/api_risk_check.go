package web

import (
	"math"
	"net/http"

	"github.com/gin-gonic/gin"
	"quantmesh/config"
)

// NewbieRiskCheckItem 新手检查项
type NewbieRiskCheckItem struct {
	Item    string `json:"item"`
	Score   int    `json:"score"`   // 0-100
	Level   string `json:"level"`   // "safe", "warning", "danger"
	Message string `json:"message"`
	Advice  string `json:"advice"`
}

// NewbieRiskReport 新手风险报告
type NewbieRiskReport struct {
	OverallScore int                   `json:"overallScore"`
	Results      []NewbieRiskCheckItem `json:"results"`
}

// getNewbieRiskCheck 获取新手体检报告
// GET /api/risk/newbie-check
func getNewbieRiskCheck(c *gin.Context) {
	if configManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "配置管理器未初始化"})
		return
	}

	cfg, err := configManager.GetConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取配置失败: " + err.Error()})
		return
	}

	results := []NewbieRiskCheckItem{}

	// 1. 杠杆安全度检查 (Leverage Safety)
	leverageItem := checkLeverage(cfg)
	results = append(results, leverageItem)

	// 2. 止损覆盖率检查 (Stop-Loss Coverage)
	stopLossItem := checkStopLoss(cfg)
	results = append(results, stopLossItem)

	// 3. 资金护城河检查 (Margin Buffer)
	marginBufferItem := checkMarginBuffer(cfg)
	results = append(results, marginBufferItem)

	// 4. 利润保护检查 (Profit Protection)
	profitProtectionItem := checkProfitProtection(cfg)
	results = append(results, profitProtectionItem)

	// 5. 环境检查 (Environment Prudence)
	environmentItem := checkEnvironment(cfg)
	results = append(results, environmentItem)

	// 计算总分
	totalScore := 0
	for _, item := range results {
		totalScore += item.Score
	}
	overallScore := int(math.Round(float64(totalScore) / float64(len(results))))

	c.JSON(http.StatusOK, NewbieRiskReport{
		OverallScore: overallScore,
		Results:      results,
	})
}

func checkLeverage(cfg *config.Config) NewbieRiskCheckItem {
	maxLev := cfg.RiskControl.MaxLeverage
	item := NewbieRiskCheckItem{Item: "杠杆倍数"}

	if maxLev <= 3 {
		item.Score = 100
		item.Level = "safe"
		item.Message = "杠杆设置非常安全"
		item.Advice = "当前杠杆倍数（3倍及以下）非常稳健，即便市场剧烈波动也有足够的缓冲空间。"
	} else if maxLev <= 5 {
		item.Score = 70
		item.Level = "warning"
		item.Message = "杠杆倍数适中"
		item.Advice = "5倍杠杆属于中等风险，新手建议保持在3倍以内以应对突发极端行情。"
	} else if maxLev <= 10 {
		item.Score = 30
		item.Level = "warning"
		item.Message = "杠杆倍数偏高"
		item.Advice = "10倍杠杆对新手来说风险较大，任何4%以上的反向波动都可能导致严重亏损甚至爆仓。"
	} else {
		item.Score = 0
		item.Level = "danger"
		item.Message = "杠杆倍数极高"
		item.Advice = "超过10倍的杠杆极度危险。强烈建议将其下调至3-5倍，以保护您的本金安全。"
	}
	return item
}

func checkStopLoss(cfg *config.Config) NewbieRiskCheckItem {
	item := NewbieRiskCheckItem{Item: "止损设置"}
	
	// 检查全局和各交易对
	globalStopLoss := cfg.Trading.GridRiskControl.Enabled && cfg.Trading.GridRiskControl.StopLossRatio > 0
	
	allSymbolsHaveStopLoss := true
	if len(cfg.Trading.Symbols) > 0 {
		for _, s := range cfg.Trading.Symbols {
			if !s.GridRiskControl.Enabled || s.GridRiskControl.StopLossRatio <= 0 {
				allSymbolsHaveStopLoss = false
				break
			}
		}
	} else {
		allSymbolsHaveStopLoss = globalStopLoss
	}

	if allSymbolsHaveStopLoss {
		item.Score = 100
		item.Level = "safe"
		item.Message = "止损逻辑已全面覆盖"
		item.Advice = "所有交易对均已设置自动止损，这是量化交易最坚实的防线。"
	} else if globalStopLoss {
		item.Score = 60
		item.Level = "warning"
		item.Message = "部分交易对缺少止损"
		item.Advice = "虽然全局开启了止损，但部分特定交易对可能未正确配置。建议为每个币种都设置明确的止损线。"
	} else {
		item.Score = 0
		item.Level = "danger"
		item.Message = "未开启自动止损"
		item.Advice = "量化交易的核心是控制风险。未开启止损就像在没有刹车的赛车上行驶，强烈建议开启 10%-15% 的硬性止损。"
	}
	return item
}

func checkMarginBuffer(cfg *config.Config) NewbieRiskCheckItem {
	safetyCheck := cfg.Trading.PositionSafetyCheck
	item := NewbieRiskCheckItem{Item: "资金护城河"}

	if safetyCheck >= 100 {
		item.Score = 100
		item.Level = "safe"
		item.Message = "资金储备充足"
		item.Advice = "您的配置能支持向下补仓100层以上，具有极强的抗风险能力。"
	} else if safetyCheck >= 50 {
		item.Score = 60
		item.Level = "warning"
		item.Message = "资金储备一般"
		item.Advice = "当前设置仅能支撑约50层补仓，在遇到30%以上的单边下跌时可能面临资金耗尽的风险。"
	} else {
		item.Score = 0
		item.Level = "danger"
		item.Message = "资金严重不足"
		item.Advice = "补仓层数设置过低。建议调低每单交易金额或增加账户保证金，确保能支撑至少80-100层补仓。"
	}
	return item
}

func checkProfitProtection(cfg *config.Config) NewbieRiskCheckItem {
	item := NewbieRiskCheckItem{Item: "利润保护"}
	
	// 只要有一个币种开启了提现策略或者全局开启了
	anyWithdrawalEnabled := false
	for _, s := range cfg.Trading.Symbols {
		if s.WithdrawalPolicy.Enabled {
			anyWithdrawalEnabled = true
			break
		}
	}

	if anyWithdrawalEnabled {
		item.Score = 100
		item.Level = "safe"
		item.Message = "已开启利润自动保护"
		item.Advice = "开启提现策略能让您在盈利时自动将部分资金转出，有效锁定胜果。"
	} else {
		item.Score = 0
		item.Level = "warning"
		item.Message = "未开启利润保护"
		item.Advice = "建议开启‘利润自动提现’或‘回本保护’，这能帮助新手养成良好的复利和避险习惯。"
	}
	return item
}

func checkEnvironment(cfg *config.Config) NewbieRiskCheckItem {
	item := NewbieRiskCheckItem{Item: "环境审慎度"}
	
	isTestnet := false
	for _, ex := range cfg.Exchanges {
		if ex.Testnet {
			isTestnet = true
			break
		}
	}

	if isTestnet {
		item.Score = 100
		item.Level = "safe"
		item.Message = "当前处于测试网环境"
		item.Advice = "在测试网磨炼策略是非常明智的选择，建议在测试网连续盈利30天后再转入实盘。"
	} else {
		item.Score = 50
		item.Level = "warning"
		item.Message = "当前处于实盘环境"
		item.Advice = "实盘环境每一分钱都是真实的，请务必确保您的参数已经过充分的回测和测试网验证。"
	}
	return item
}

// applyNewbieSecurityConfig 一键应用安全配置加固
// POST /api/risk/newbie-check/apply
func applyNewbieSecurityConfig(c *gin.Context) {
	if configManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "配置管理器未初始化"})
		return
	}

	cfg, err := configManager.GetConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取当前配置失败: " + err.Error()})
		return
	}

	// 复制一份配置进行修改
	newConfig := *cfg

	// 1. 强制下调最高杠杆
	if newConfig.RiskControl.MaxLeverage > 3 {
		newConfig.RiskControl.MaxLeverage = 3
	}
	// 同时也修改交易所配置中的杠杆
	for exName, exCfg := range newConfig.Exchanges {
		if exCfg.Leverage > 3 {
			exCfg.Leverage = 3
			newConfig.Exchanges[exName] = exCfg
		}
	}

	// 2. 开启全局止损 (10%)
	if !newConfig.Trading.GridRiskControl.Enabled || newConfig.Trading.GridRiskControl.StopLossRatio == 0 {
		newConfig.Trading.GridRiskControl.Enabled = true
		newConfig.Trading.GridRiskControl.StopLossRatio = 0.1
	}

	// 3. 为所有交易对开启止损 (10%)
	for i := range newConfig.Trading.Symbols {
		if !newConfig.Trading.Symbols[i].GridRiskControl.Enabled || newConfig.Trading.Symbols[i].GridRiskControl.StopLossRatio == 0 {
			newConfig.Trading.Symbols[i].GridRiskControl.Enabled = true
			newConfig.Trading.Symbols[i].GridRiskControl.StopLossRatio = 0.1
		}
	}

	// 4. 提高资金安全检查阈值
	if newConfig.Trading.PositionSafetyCheck < 100 {
		newConfig.Trading.PositionSafetyCheck = 100
	}
	for i := range newConfig.Trading.Symbols {
		if newConfig.Trading.Symbols[i].PositionSafetyCheck < 100 {
			newConfig.Trading.Symbols[i].PositionSafetyCheck = 100
		}
	}

	// 5. 开启默认利润保护 (可选，这里设为回本保护)
	for i := range newConfig.Trading.Symbols {
		if !newConfig.Trading.Symbols[i].WithdrawalPolicy.Enabled {
			newConfig.Trading.Symbols[i].WithdrawalPolicy.Enabled = true
			newConfig.Trading.Symbols[i].WithdrawalPolicy.PrincipalProtection.Enabled = true
			newConfig.Trading.Symbols[i].WithdrawalPolicy.PrincipalProtection.BreakevenProtection = true
		}
	}

	// 验证配置
	if err := newConfig.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "生成的加固配置无效: " + err.Error()})
		return
	}

	// 保存配置
	if err := configManager.UpdateConfig(&newConfig); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存加固配置失败: " + err.Error()})
		return
	}

	// 热更新
	if configHotReloader != nil {
		configHotReloader.UpdateConfig(&newConfig)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "安全配置已一键应用，杠杆已下调至3倍，已开启10%止损和保本保护。",
	})
}
