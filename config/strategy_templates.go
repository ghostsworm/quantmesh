package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"
)

// StrategyTemplate 策略配置模板
type StrategyTemplate struct {
	ID            string                   `yaml:"id" json:"id"`
	Name          string                   `yaml:"name" json:"name"`
	Description   string                   `yaml:"description" json:"description"`
	Category      string                   `yaml:"category" json:"category"` // grid, dca, momentum, etc.
	StrategyType  string                   `yaml:"strategy_type" json:"strategy_type"`
	Config        map[string]interface{}   `yaml:"config" json:"config"`
	Params        map[string]TemplateParam `yaml:"params" json:"params"`
	DefaultWeight float64                  `yaml:"default_weight" json:"default_weight"`
	// 新增字段
	Symbols    []string `yaml:"symbols,omitempty" json:"symbols,omitempty"`         // 推荐币种: BTCUSDT, ETHUSDT, etc.
	Difficulty string   `yaml:"difficulty,omitempty" json:"difficulty,omitempty"`   // beginner, intermediate, advanced
	RiskLevel  string   `yaml:"risk_level,omitempty" json:"risk_level,omitempty"`   // low, medium, high
	Tags       []string `yaml:"tags,omitempty" json:"tags,omitempty"`               // 标签: conservative, aggressive, trending, etc.
	MinCapital float64  `yaml:"min_capital,omitempty" json:"min_capital,omitempty"` // 最低资金要求（U）
}

// TemplateParam 模板参数定义
type TemplateParam struct {
	Name        string      `yaml:"name" json:"name"`
	Description string      `yaml:"description" json:"description"`
	StrategyID  string      `yaml:"strategy_id,omitempty" json:"strategy_id,omitempty"`
	Type        string      `yaml:"type" json:"type"` // number, string, boolean, select
	Default     interface{} `yaml:"default" json:"default"`
	Min         *float64    `yaml:"min,omitempty" json:"min,omitempty"`
	Max         *float64    `yaml:"max,omitempty" json:"max,omitempty"`
	Options     []string    `yaml:"options,omitempty" json:"options,omitempty"` // For select type
	Required    bool        `yaml:"required" json:"required"`
}

// StrategyTemplateManager 策略模板管理器
type StrategyTemplateManager struct {
	templatesDir string
	templates    map[string]*StrategyTemplate
	mu           sync.RWMutex
}

// NewStrategyTemplateManager 创建策略模板管理器
func NewStrategyTemplateManager(baseDir string) (*StrategyTemplateManager, error) {
	templatesDir := filepath.Join(baseDir, "templates", "strategies")
	if err := os.MkdirAll(templatesDir, 0755); err != nil {
		return nil, err
	}

	manager := &StrategyTemplateManager{
		templatesDir: templatesDir,
		templates:    make(map[string]*StrategyTemplate),
	}

	// 加载内置模板
	manager.initBuiltinTemplates()

	return manager, nil
}

// initBuiltinTemplates 初始化内置策略模板
func (m *StrategyTemplateManager) initBuiltinTemplates() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// ==================== 基础网格策略系列 ====================

	m.templates["grid_basic_btc"] = &StrategyTemplate{
		ID:           "grid_basic_btc",
		Name:         "BTC 基础网格",
		Description:  "适合 BTC 的经典网格策略，在价格区间内等距挂单，适合横盘震荡行情",
		Category:     "grid",
		StrategyType: "grid",
		Config: map[string]interface{}{
			"grid_spacing": 500.0,
			"grid_levels":  10,
		},
		Params: map[string]TemplateParam{
			"price_interval": {
				Name:        "价格间隔",
				Description: "每档价格差（U），BTC 建议 300-800",
				Type:        "number",
				Default:     500.0,
				Min:         ptrFloat64(100),
				Max:         ptrFloat64(2000),
				Required:    true,
			},
			"order_quantity": {
				Name:        "订单金额",
				Description: "每笔订单金额（U）",
				Type:        "number",
				Default:     200.0,
				Min:         ptrFloat64(50),
				Required:    true,
			},
		},
		DefaultWeight: 1.0,
		Symbols:       []string{"BTCUSDT"},
		Difficulty:    "beginner",
		RiskLevel:     "low",
		Tags:          []string{"conservative", "sideways"},
		MinCapital:    2000,
	}

	m.templates["grid_basic_eth"] = &StrategyTemplate{
		ID:           "grid_basic_eth",
		Name:         "ETH 基础网格",
		Description:  "适合 ETH 的经典网格策略，ETH 波动相对较小，可以设置更小的网格间距",
		Category:     "grid",
		StrategyType: "grid",
		Config: map[string]interface{}{
			"grid_spacing": 20.0,
			"grid_levels":  15,
		},
		Params: map[string]TemplateParam{
			"price_interval": {
				Name:        "价格间隔",
				Description: "每档价格差（U），ETH 建议 10-50",
				Type:        "number",
				Default:     20.0,
				Min:         ptrFloat64(5),
				Max:         ptrFloat64(100),
				Required:    true,
			},
			"order_quantity": {
				Name:        "订单金额",
				Description: "每笔订单金额（U）",
				Type:        "number",
				Default:     100.0,
				Min:         ptrFloat64(20),
				Required:    true,
			},
		},
		DefaultWeight: 1.0,
		Symbols:       []string{"ETHUSDT"},
		Difficulty:    "beginner",
		RiskLevel:     "low",
		Tags:          []string{"conservative", "sideways"},
		MinCapital:    1000,
	}

	m.templates["grid_basic_gold"] = &StrategyTemplate{
		ID:           "grid_basic_gold",
		Name:         "黄金网格策略",
		Description:  "适合黄金/稳定币对的基础网格，黄金波动较小，适合保守型投资者",
		Category:     "grid",
		StrategyType: "grid",
		Config: map[string]interface{}{
			"grid_spacing": 2.0,
			"grid_levels":  8,
		},
		Params: map[string]TemplateParam{
			"price_interval": {
				Name:        "价格间隔",
				Description: "每档价格差（U），黄金建议 1-5",
				Type:        "number",
				Default:     2.0,
				Min:         ptrFloat64(0.5),
				Max:         ptrFloat64(10),
				Required:    true,
			},
			"order_quantity": {
				Name:        "订单金额",
				Description: "每笔订单金额（U）",
				Type:        "number",
				Default:     100.0,
				Min:         ptrFloat64(50),
				Required:    true,
			},
		},
		DefaultWeight: 1.0,
		Symbols:       []string{"PAXGUSDT", "XAUTUSDT"},
		Difficulty:    "beginner",
		RiskLevel:     "low",
		Tags:          []string{"conservative", "safehaven", "sideways"},
		MinCapital:    1000,
	}

	// ==================== 组合策略系列 ====================

	m.templates["combo_grid_dca_btc"] = &StrategyTemplate{
		ID:           "combo_grid_dca_btc",
		Name:         "BTC 网格+DCA增强",
		Description:  "网格策略（70%）+ ATR分层DCA增强策略（30%）；按价格与风险参数执行，不是定时定额定投。",
		Category:     "combo",
		StrategyType: "combo",
		Config: map[string]interface{}{
			"strategies": []map[string]interface{}{
				{"type": "grid", "weight": 0.7},
				{"type": "dca_enhanced", "weight": 0.3},
			},
		},
		Params: map[string]TemplateParam{
			"price_interval": {
				Name:        "网格间距",
				Description: "网格每档价格差（U）",
				StrategyID:  "grid",
				Type:        "number",
				Default:     500.0,
				Min:         ptrFloat64(200),
				Required:    true,
			},
			"grid_order_qty": {
				Name:        "网格单笔金额",
				Description: "网格每笔订单（U）",
				StrategyID:  "grid",
				Type:        "number",
				Default:     200.0,
				Min:         ptrFloat64(50),
				Required:    true,
			},
			"base_order_amount": {
				Name:        "DCA基础订单金额",
				Description: "基础订单名义金额（U）",
				StrategyID:  "dca_enhanced",
				Type:        "number",
				Default:     50.0,
				Min:         ptrFloat64(10),
				Required:    true,
			},
			"safety_order_amount": {
				Name:        "DCA补仓订单金额",
				Description: "每层补仓订单名义金额（U）",
				StrategyID:  "dca_enhanced",
				Type:        "number",
				Default:     100.0,
				Min:         ptrFloat64(10),
				Required:    true,
			},
			"max_safety_orders": {
				Name:        "最大补仓层数",
				Description: "DCA最多执行的补仓层数",
				StrategyID:  "dca_enhanced",
				Type:        "number",
				Default:     4,
				Min:         ptrFloat64(1),
				Required:    true,
			},
		},
		DefaultWeight: 1.0,
		Symbols:       []string{"BTCUSDT"},
		Difficulty:    "intermediate",
		RiskLevel:     "medium",
		Tags:          []string{"balanced", "longterm"},
		MinCapital:    3000,
	}

	m.templates["combo_grid_trend_eth"] = &StrategyTemplate{
		ID:           "combo_grid_trend_eth",
		Name:         "ETH 网格+趋势组合",
		Description:  "网格策略（80%）与趋势跟踪（20%）组合；本模板不启用止盈或止损，需在 Bot 网格风控设置中单独配置。",
		Category:     "combo",
		StrategyType: "combo",
		Config: map[string]interface{}{
			"strategies": []map[string]interface{}{
				{"type": "grid", "weight": 0.8},
				{"type": "trend_following", "weight": 0.2},
			},
		},
		Params:        map[string]TemplateParam{},
		DefaultWeight: 1.0,
		Symbols:       []string{"ETHUSDT"},
		Difficulty:    "intermediate",
		RiskLevel:     "medium",
		Tags:          []string{"balanced", "trending"},
		MinCapital:    2000,
	}

	// ==================== 高级策略系列 ====================

	m.templates["martingale_grid_btc"] = &StrategyTemplate{
		ID:           "martingale_grid_btc",
		Name:         "BTC 马丁格尔策略",
		Description:  "高风险逆势加仓策略；按百分比间距递增加仓，层数与倍数需谨慎设置。",
		Category:     "grid",
		StrategyType: "martingale",
		Config: map[string]interface{}{
			"initial_amount": 100.0,
			"multiplier":     1.5,
			"max_levels":     8,
			"price_step":     2.0,
		},
		Params: map[string]TemplateParam{
			"initial_amount": {
				Name:        "初始金额",
				Description: "首笔订单金额（U）",
				Type:        "number",
				Default:     100.0,
				Min:         ptrFloat64(50),
				Required:    true,
			},
			"multiplier": {
				Name:        "倍增系数",
				Description: "每跌一格的倍增系数（1.2-2.0）",
				Type:        "number",
				Default:     1.5,
				Min:         ptrFloat64(1.2),
				Max:         ptrFloat64(2.0),
				Required:    true,
			},
			"max_levels": {
				Name:        "最大层数",
				Description: "最大加仓层数",
				Type:        "number",
				Default:     8.0,
				Min:         ptrFloat64(3),
				Max:         ptrFloat64(15),
				Required:    true,
			},
			"price_step": {
				Name:        "加仓间距",
				Description: "相对上次入场价的百分比间距",
				Type:        "number",
				Default:     2.0,
				Min:         ptrFloat64(0.1),
				Max:         ptrFloat64(50),
				Required:    true,
			},
		},
		DefaultWeight: 1.0,
		Symbols:       []string{"BTCUSDT"},
		Difficulty:    "advanced",
		RiskLevel:     "high",
		Tags:          []string{"aggressive", "highrisk"},
		MinCapital:    10000,
	}

	m.templates["momentum_grid_eth"] = &StrategyTemplate{
		ID:           "momentum_grid_eth",
		Name:         "ETH RSI 动量策略",
		Description:  "基于 RSI 超卖/超买信号进行开平仓；这是信号策略，不是网格策略。",
		Category:     "grid",
		StrategyType: "momentum",
		Config: map[string]interface{}{
			"rsi_period":   14,
			"overbought":   70.0,
			"oversold":     30.0,
			"order_amount": 100.0,
		},
		Params: map[string]TemplateParam{
			"rsi_period": {
				Name:        "RSI 周期",
				Description: "计算 RSI 的价格周期数",
				Type:        "number",
				Default:     14,
				Min:         ptrFloat64(2),
				Max:         ptrFloat64(100),
				Required:    true,
			},
			"overbought": {
				Name:        "超买阈值",
				Description: "RSI 高于此值时触发平仓",
				Type:        "number",
				Default:     70.0,
				Min:         ptrFloat64(50),
				Max:         ptrFloat64(100),
				Required:    true,
			},
			"oversold": {
				Name:        "超卖阈值",
				Description: "RSI 低于此值时触发开仓",
				Type:        "number",
				Default:     30.0,
				Min:         ptrFloat64(0),
				Max:         ptrFloat64(50),
				Required:    true,
			},
			"order_amount": {
				Name:        "单笔金额",
				Description: "每次信号开仓的名义金额（U）",
				Type:        "number",
				Default:     100.0,
				Min:         ptrFloat64(10),
				Required:    true,
			},
		},
		DefaultWeight: 0.8,
		Symbols:       []string{"ETHUSDT"},
		Difficulty:    "intermediate",
		RiskLevel:     "medium",
		Tags:          []string{"trending", "smart"},
		MinCapital:    2000,
	}

	// ==================== 保守型策略系列 ====================

	m.templates["dca_regular_btc"] = &StrategyTemplate{
		ID:           "dca_regular_btc",
		Name:         "BTC DCA增强策略",
		Description:  "按 ATR 价格层级执行分层订单，并受止盈止损参数约束；不是按时间定期买入。",
		Category:     "dca",
		StrategyType: "dca_enhanced",
		Config: map[string]interface{}{
			"base_order_amount":   50,
			"safety_order_amount": 100,
			"max_safety_orders":   4,
			"atr_period":          14,
			"atr_multiplier":      1.5,
			"total_take_profit":   2,
			"stop_loss":           10,
		},
		Params: map[string]TemplateParam{
			"base_order_amount": {
				Name:        "基础订单金额",
				Description: "基础订单名义金额（U）",
				Type:        "number",
				Default:     50.0,
				Min:         ptrFloat64(10),
				Required:    true,
			},
			"safety_order_amount": {
				Name:        "补仓订单金额",
				Description: "每层补仓订单名义金额（U）",
				Type:        "number",
				Default:     100.0,
				Min:         ptrFloat64(10),
				Required:    true,
			},
			"max_safety_orders": {
				Name:        "最大补仓层数",
				Description: "最多执行的补仓层数",
				Type:        "number",
				Default:     4,
				Min:         ptrFloat64(1),
				Required:    true,
			},
		},
		DefaultWeight: 1.0,
		Symbols:       []string{"BTCUSDT", "ETHUSDT", "PAXGUSDT"},
		Difficulty:    "beginner",
		RiskLevel:     "medium",
		Tags:          []string{"conservative", "longterm", "passive"},
		MinCapital:    500,
	}

	// ==================== 原有模板保持兼容 ====================

	m.templates["grid_basic"] = &StrategyTemplate{
		ID:           "grid_basic",
		Name:         "通用基础网格",
		Description:  "经典网格策略，在价格区间内等距挂单",
		Category:     "grid",
		StrategyType: "grid",
		Config: map[string]interface{}{
			"grid_spacing": 100.0,
			"grid_levels":  10,
		},
		Params: map[string]TemplateParam{
			"price_interval": {
				Name:        "价格间隔",
				Description: "每档价格差，如 100 表示每档相差 100 USDT",
				Type:        "number",
				Default:     100.0,
				Min:         ptrFloat64(0.1),
				Required:    true,
			},
			"order_quantity": {
				Name:        "订单金额",
				Description: "每笔订单的金额（USDT）",
				Type:        "number",
				Default:     100.0,
				Min:         ptrFloat64(10),
				Required:    true,
			},
		},
		DefaultWeight: 1.0,
		Symbols:       []string{},
		Difficulty:    "beginner",
		RiskLevel:     "low",
		Tags:          []string{"conservative"},
		MinCapital:    1000,
	}

	m.templates["trend_following"] = &StrategyTemplate{
		ID:           "trend_following",
		Name:         "趋势跟踪策略",
		Description:  "跟踪市场趋势，动态调整挂单方向",
		Category:     "grid",
		StrategyType: "trend_following",
		Config: map[string]interface{}{
			"trend_period":    60,
			"trend_threshold": 0.5,
		},
		Params: map[string]TemplateParam{
			"trend_period": {
				Name:        "趋势周期",
				Description: "计算趋势的K线周期数",
				Type:        "number",
				Default:     60,
				Min:         ptrFloat64(5),
				Max:         ptrFloat64(200),
				Required:    true,
			},
			"trend_threshold": {
				Name:        "趋势阈值",
				Description: "触发趋势信号的阈值",
				Type:        "number",
				Default:     0.5,
				Min:         ptrFloat64(0.1),
				Max:         ptrFloat64(2.0),
				Required:    true,
			},
		},
		DefaultWeight: 0.5,
		Symbols:       []string{"BTCUSDT", "ETHUSDT"},
		Difficulty:    "intermediate",
		RiskLevel:     "medium",
		Tags:          []string{"trending"},
		MinCapital:    2000,
	}

	m.templates["momentum"] = &StrategyTemplate{
		ID:           "momentum",
		Name:         "RSI 动量策略",
		Description:  "基于 RSI 超买/超卖信号进行交易；参数与实际动量运行时一致。",
		Category:     "grid",
		StrategyType: "momentum",
		Config: map[string]interface{}{
			"rsi_period":   14,
			"overbought":   70.0,
			"oversold":     30.0,
			"order_amount": 100.0,
		},
		Params: map[string]TemplateParam{
			"rsi_period": {
				Name:        "RSI 周期",
				Description: "计算 RSI 的价格周期数",
				Type:        "number",
				Default:     14,
				Min:         ptrFloat64(2),
				Max:         ptrFloat64(100),
				Required:    true,
			},
			"overbought": {
				Name:        "超买阈值",
				Description: "RSI 高于此值时触发平仓",
				Type:        "number",
				Default:     70.0,
				Min:         ptrFloat64(50),
				Max:         ptrFloat64(100),
				Required:    true,
			},
			"oversold": {
				Name:        "超卖阈值",
				Description: "RSI 低于此值时触发开仓",
				Type:        "number",
				Default:     30.0,
				Min:         ptrFloat64(0),
				Max:         ptrFloat64(50),
				Required:    true,
			},
			"order_amount": {
				Name:        "单笔金额",
				Description: "每次信号开仓的名义金额（U）",
				Type:        "number",
				Default:     100.0,
				Min:         ptrFloat64(10),
				Required:    true,
			},
		},
		DefaultWeight: 0.4,
		Symbols:       []string{"ETHUSDT", "BTCUSDT"},
		Difficulty:    "intermediate",
		RiskLevel:     "medium",
		Tags:          []string{"trending", "smart"},
		MinCapital:    2000,
	}

	m.templates["dca_regular"] = &StrategyTemplate{
		ID:           "dca_regular",
		Name:         "DCA 增强策略",
		Description:  "按 ATR 价格层级执行分层订单，并受止盈止损参数约束；不是按时间定期买入。",
		Category:     "dca",
		StrategyType: "dca_enhanced",
		Config: map[string]interface{}{
			"base_order_amount":   50,
			"safety_order_amount": 100,
			"max_safety_orders":   4,
			"atr_period":          14,
			"atr_multiplier":      1.5,
			"total_take_profit":   2,
			"stop_loss":           10,
		},
		Params: map[string]TemplateParam{
			"base_order_amount": {
				Name:        "基础订单金额",
				Description: "基础订单名义金额（U）",
				Type:        "number",
				Default:     50.0,
				Min:         ptrFloat64(10),
				Required:    true,
			},
			"safety_order_amount": {
				Name:        "补仓订单金额",
				Description: "每层补仓订单名义金额（U）",
				Type:        "number",
				Default:     100.0,
				Min:         ptrFloat64(10),
				Required:    true,
			},
			"max_safety_orders": {
				Name:        "最大补仓层数",
				Description: "最多执行的补仓层数",
				Type:        "number",
				Default:     4,
				Min:         ptrFloat64(1),
				Required:    true,
			},
		},
		DefaultWeight: 1.0,
		Symbols:       []string{},
		Difficulty:    "beginner",
		RiskLevel:     "low",
		Tags:          []string{"conservative", "longterm"},
		MinCapital:    500,
	}

	m.templates["martingale"] = &StrategyTemplate{
		ID:           "martingale",
		Name:         "马丁格尔策略",
		Description:  "⚠️ 亏损后按倍数加仓，风险较高；配置项与运行时一致。",
		Category:     "dca",
		StrategyType: "martingale",
		Config: map[string]interface{}{
			"initial_amount": 100,
			"multiplier":     2,
			"max_levels":     5,
			"price_step":     2,
		},
		Params: map[string]TemplateParam{
			"initial_amount": {
				Name:        "初始金额",
				Description: "首次下单金额（USDT）",
				Type:        "number",
				Default:     100.0,
				Min:         ptrFloat64(10),
				Required:    true,
			},
			"price_step": {
				Name:        "加仓间距",
				Description: "相对上次入场价的百分比间距",
				Type:        "number",
				Default:     2.0,
				Min:         ptrFloat64(0.1),
				Max:         ptrFloat64(50),
				Required:    true,
			},
			"multiplier": {
				Name:        "倍增系数",
				Description: "每次亏损后的倍增系数",
				Type:        "number",
				Default:     2.0,
				Min:         ptrFloat64(1.1),
				Max:         ptrFloat64(3.0),
				Required:    true,
			},
			"max_levels": {
				Name:        "最大层数",
				Description: "最大加仓层数",
				Type:        "number",
				Default:     5,
				Min:         ptrFloat64(2),
				Max:         ptrFloat64(10),
				Required:    true,
			},
		},
		DefaultWeight: 1.0,
		Symbols:       []string{},
		Difficulty:    "advanced",
		RiskLevel:     "high",
		Tags:          []string{"aggressive", "highrisk"},
		MinCapital:    5000,
	}

	m.templates["mean_reversion"] = &StrategyTemplate{
		ID:           "mean_reversion",
		Name:         "均值回归策略",
		Description:  "价格偏离均值时进行交易",
		Category:     "grid",
		StrategyType: "mean_reversion",
		Config: map[string]interface{}{
			"mean_period":       20,
			"std_dev_threshold": 2.0,
		},
		Params: map[string]TemplateParam{
			"mean_period": {
				Name:        "均值周期",
				Description: "计算移动平均的周期",
				Type:        "number",
				Default:     20,
				Min:         ptrFloat64(5),
				Max:         ptrFloat64(100),
				Required:    true,
			},
			"std_dev_threshold": {
				Name:        "标准差阈值",
				Description: "触发交易的标准差倍数",
				Type:        "number",
				Default:     2.0,
				Min:         ptrFloat64(0.5),
				Max:         ptrFloat64(3.0),
				Required:    true,
			},
		},
		DefaultWeight: 0.6,
		Symbols:       []string{"BTCUSDT", "ETHUSDT"},
		Difficulty:    "intermediate",
		RiskLevel:     "medium",
		Tags:          []string{"sideways", "smart"},
		MinCapital:    2000,
	}
}

// ListTemplates 列出所有可用模板
func (m *StrategyTemplateManager) ListTemplates() []*StrategyTemplate {
	m.mu.RLock()
	defer m.mu.RUnlock()

	templates := make([]*StrategyTemplate, 0, len(m.templates))
	for _, t := range m.templates {
		templates = append(templates, t)
	}
	return templates
}

// GetTemplate 获取指定模板
func (m *StrategyTemplateManager) GetTemplate(id string) (*StrategyTemplate, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	template, ok := m.templates[id]
	return template, ok
}

// GetTemplatesByCategory 按类别获取模板
func (m *StrategyTemplateManager) GetTemplatesByCategory(category string) []*StrategyTemplate {
	m.mu.RLock()
	defer m.mu.RUnlock()

	templates := make([]*StrategyTemplate, 0)
	for _, t := range m.templates {
		if t.Category == category {
			templates = append(templates, t)
		}
	}
	return templates
}

// ApplyTemplate 应用模板到 Bot 配置
func (m *StrategyTemplateManager) ApplyTemplate(templateID string, botConfig *BotConfigFile) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	template, ok := m.templates[templateID]
	if !ok {
		return os.ErrNotExist
	}

	// 创建新策略配置
	newStrategy := BotStrategyConfig{
		Type:     template.StrategyType,
		Enabled:  true,
		Weight:   template.DefaultWeight,
		Params:   make(map[string]interface{}),
		Settings: make(map[string]interface{}),
	}

	// 应用模板配置
	for k, v := range template.Config {
		newStrategy.Params[k] = v
	}

	// 应用模板参数
	for k, p := range template.Params {
		newStrategy.Params[k] = p.Default
	}

	// 添加到 Bot 配置
	botConfig.Strategies = append(botConfig.Strategies, newStrategy)

	// 更新策略模式
	if len(botConfig.Strategies) > 1 {
		botConfig.StrategyMode = "multi"
	}

	return nil
}

// SaveCustomTemplate 保存自定义模板
func (m *StrategyTemplateManager) SaveCustomTemplate(template *StrategyTemplate) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 保存到文件
	templatePath := filepath.Join(m.templatesDir, template.ID+".yaml")
	data, err := yaml.Marshal(template)
	if err != nil {
		return err
	}

	if err := os.WriteFile(templatePath, data, 0644); err != nil {
		return err
	}

	// 添加到内存
	m.templates[template.ID] = template

	return nil
}

// DeleteCustomTemplate 删除自定义模板
func (m *StrategyTemplateManager) DeleteCustomTemplate(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 只能删除自定义模板（以 custom_ 开头的）
	if len(id) <= 8 || id[:8] != "custom_" {
		return os.ErrInvalid
	}

	templatePath := filepath.Join(m.templatesDir, id+".yaml")
	if err := os.Remove(templatePath); err != nil {
		return err
	}

	delete(m.templates, id)
	return nil
}

// ExportTemplate 导出模板为 JSON
func (m *StrategyTemplateManager) ExportTemplate(id string) ([]byte, error) {
	template, ok := m.GetTemplate(id)
	if !ok {
		return nil, os.ErrNotExist
	}

	return json.MarshalIndent(template, "", "  ")
}

// ImportTemplate 从 JSON 导入模板
func (m *StrategyTemplateManager) ImportTemplate(jsonData []byte) error {
	var template StrategyTemplate
	if err := json.Unmarshal(jsonData, &template); err != nil {
		return err
	}

	// 验证模板 ID
	if template.ID == "" {
		return os.ErrInvalid
	}

	return m.SaveCustomTemplate(&template)
}

// ptrFloat64 返回 float64 指针的辅助函数
func ptrFloat64(v float64) *float64 {
	return &v
}
