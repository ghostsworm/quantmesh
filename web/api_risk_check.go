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

// NewbieRiskReport 新手风險报告
type NewbieRiskReport struct {
	OverallScore int                   `json:"overallScore"`
	Results      []NewbieRiskCheckItem `json:"results"`
}

// getNewbieRiskCheck 獲取新手体检报告
// GET /api/risk/newbie-check
func getNewbieRiskCheck(c *gin.Context) {
	if configManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "配置管理器未初始化"})
		return
	}

	cfg, err := configManager.GetConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "獲取配置失败: " + err.Error()})
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

	// 计算總分
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
	item := NewbieRiskCheckItem{Item: "杠杆倍數"}

	if maxLev <= 3 {
		item.Score = 100
		item.Level = "safe"
		item.Message = "杠杆設置非常安全"
		item.Advice = "當前杠杆倍數（3倍及以下）非常稳健，即便市场剧烈波动也有足够的缓冲空间。"
	} else if maxLev <= 5 {
		item.Score = 70
		item.Level = "warning"
		item.Message = "杠杆倍數适中"
		item.Advice = "5倍杠杆属於中等风險，新手建议保持在3倍以内以应對突发极端行情。"
	} else if maxLev <= 10 {
		item.Score = 30
		item.Level = "warning"
		item.Message = "杠杆倍數偏高"
		item.Advice = "10倍杠杆對新手来說风險较大，任何4%以上的反向波动都可能導致严重亏损甚至爆倉。"
	} else {
		item.Score = 0
		item.Level = "danger"
		item.Message = "杠杆倍數极高"
		item.Advice = "超過10倍的杠杆极度危險。强烈建议將其下調至3-5倍，以保护您的本金安全。"
	}
	return item
}

func checkStopLoss(cfg *config.Config) NewbieRiskCheckItem {
	item := NewbieRiskCheckItem{Item: "止损設置"}
	
	// 检查全局和各交易對
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
		item.Advice = "所有交易對均已設置自动止损，这是量化交易最坚實的防線。"
	} else if globalStopLoss {
		item.Score = 60
		item.Level = "warning"
		item.Message = "部分交易對缺少止损"
		item.Advice = "雖然全局开啟了止损，但部分特定交易對可能未正确配置。建议為每個币种都設置明确的止损線。"
	} else {
		item.Score = 0
		item.Level = "danger"
		item.Message = "未开啟自动止损"
		item.Advice = "量化交易的核心是控制风險。未开啟止损就像在没有刹车的赛车上行驶，强烈建议开啟 10%-15% 的硬性止损。"
	}
	return item
}

func checkMarginBuffer(cfg *config.Config) NewbieRiskCheckItem {
	safetyCheck := cfg.Trading.PositionSafetyCheck
	item := NewbieRiskCheckItem{Item: "资金护城河"}

	if safetyCheck >= 100 {
		item.Score = 100
		item.Level = "safe"
		item.Message = "资金儲备充足"
		item.Advice = "您的配置能支援向下补倉100层以上，具有极强的抗风險能力。"
	} else if safetyCheck >= 50 {
		item.Score = 60
		item.Level = "warning"
		item.Message = "资金儲备一般"
		item.Advice = "當前設置僅能支撑约50层补倉，在遇到30%以上的單边下跌時可能面临资金耗尽的风險。"
	} else {
		item.Score = 0
		item.Level = "danger"
		item.Message = "资金严重不足"
		item.Advice = "补倉层數設置過低。建议調低每單交易金額或增加账戶保证金，确保能支撑至少80-100层补倉。"
	}
	return item
}

func checkProfitProtection(cfg *config.Config) NewbieRiskCheckItem {
	item := NewbieRiskCheckItem{Item: "利润保护"}
	
	// 只要有一個币种开啟了提現策略或者全局开啟了
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
		item.Message = "已开啟利润自动保护"
		item.Advice = "开啟提現策略能让您在盈利時自动將部分资金轉出，有效鎖定胜果。"
	} else {
		item.Score = 0
		item.Level = "warning"
		item.Message = "未开啟利润保护"
		item.Advice = "建议开啟‘利润自动提現’或‘回本保护’，这能帮助新手养成良好的複利和避險习惯。"
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
		item.Message = "當前处於測試網环境"
		item.Advice = "在測試網磨炼策略是非常明智的选擇，建议在測試網连续盈利30天后再轉入實盘。"
	} else {
		item.Score = 50
		item.Level = "warning"
		item.Message = "當前处於實盘环境"
		item.Advice = "實盘环境每一分钱都是真實的，请務必确保您的参數已經過充分的回测和測試網驗证。"
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": "獲取當前配置失败: " + err.Error()})
		return
	}

	// 複制一份配置進行修改
	newConfig := *cfg

	// 1. 强制下調最高杠杆
	if newConfig.RiskControl.MaxLeverage > 3 {
		newConfig.RiskControl.MaxLeverage = 3
	}
	// 同時也修改交易所配置中的杠杆
	for exName, exCfg := range newConfig.Exchanges {
		if exCfg.Leverage > 3 {
			exCfg.Leverage = 3
			newConfig.Exchanges[exName] = exCfg
		}
	}

	// 2. 开啟全局止损 (10%)
	if !newConfig.Trading.GridRiskControl.Enabled || newConfig.Trading.GridRiskControl.StopLossRatio == 0 {
		newConfig.Trading.GridRiskControl.Enabled = true
		newConfig.Trading.GridRiskControl.StopLossRatio = 0.1
	}

	// 3. 為所有交易對开啟止损 (10%)
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

	// 5. 开啟默认利润保护 (可選，这里設為回本保护)
	for i := range newConfig.Trading.Symbols {
		if !newConfig.Trading.Symbols[i].WithdrawalPolicy.Enabled {
			newConfig.Trading.Symbols[i].WithdrawalPolicy.Enabled = true
			newConfig.Trading.Symbols[i].WithdrawalPolicy.PrincipalProtection.Enabled = true
			newConfig.Trading.Symbols[i].WithdrawalPolicy.PrincipalProtection.BreakevenProtection = true
		}
	}

	// 驗证配置
	if err := newConfig.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "生成的加固配置無效: " + err.Error()})
		return
	}

	// 保存配置
	if err := configManager.UpdateConfig(&newConfig); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存加固配置失败: " + err.Error()})
		return
	}

	// 保存到历史
	if configHistoryMgr != nil {
		currentContent, err := os.ReadFile(configManager.GetConfigPath())
		if err == nil {
			description := "通過 Web UI 執行新手安全加固"
			_, _ = configHistoryMgr.SaveHistory(string(currentContent), description, "web")
		}
	}

	// 热更新
	if configHotReloader != nil {
		configHotReloader.UpdateConfig(&newConfig)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "安全配置已一键应用，杠杆已下調至3倍，已开啟10%止损和保本保护。",
	})
}
