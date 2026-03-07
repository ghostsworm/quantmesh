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
	ID          string                 `yaml:"id" json:"id"`
	Name        string                 `yaml:"name" json:"name"`
	Description string                 `yaml:"description" json:"description"`
	Category    string                 `yaml:"category" json:"category"`     // grid, dca, momentum, etc.
	StrategyType string                `yaml:"strategy_type" json:"strategy_type"`
	Config      map[string]interface{} `yaml:"config" json:"config"`
	Params      map[string]TemplateParam `yaml:"params" json:"params"`
	DefaultWeight float64              `yaml:"default_weight" json:"default_weight"`
}

// TemplateParam 模板参数定义
type TemplateParam struct {
	Name        string      `yaml:"name" json:"name"`
	Description string      `yaml:"description" json:"description"`
	Type        string      `yaml:"type" json:"type"`           // number, string, boolean, select
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

	// 网格策略模板
	m.templates["grid_basic"] = &StrategyTemplate{
		ID:          "grid_basic",
		Name:        "基础网格策略",
		Description: "经典网格策略，在价格区间内等距挂单",
		Category:    "grid",
		StrategyType: "grid",
		Config: map[string]interface{}{
			"grid_spacing": 100.0,
			"grid_levels": 10,
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
	}

	// 趋势跟踪模板
	m.templates["trend_following"] = &StrategyTemplate{
		ID:          "trend_following",
		Name:        "趋势跟踪策略",
		Description: "跟踪市场趋势，动态调整挂单方向",
		Category:    "grid",
		StrategyType: "trend_following",
		Config: map[string]interface{}{
			"trend_period": 60,
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
	}

	// 动量策略模板
	m.templates["momentum"] = &StrategyTemplate{
		ID:          "momentum",
		Name:        "动量策略",
		Description: "基于价格动量进行交易",
		Category:    "grid",
		StrategyType: "momentum",
		Config: map[string]interface{}{
			"momentum_period": 14,
			"momentum_threshold": 0.02,
		},
		Params: map[string]TemplateParam{
			"momentum_period": {
				Name:        "动量周期",
				Description: "计算动量的周期数",
				Type:        "number",
				Default:     14,
				Min:         ptrFloat64(5),
				Max:         ptrFloat64(50),
				Required:    true,
			},
			"momentum_threshold": {
				Name:        "动量阈值",
				Description: "触发动量信号的阈值",
				Type:        "number",
				Default:     0.02,
				Min:         ptrFloat64(0.001),
				Max:         ptrFloat64(0.1),
				Required:    true,
			},
		},
		DefaultWeight: 0.4,
	}

	// 定投策略模板
	m.templates["dca_regular"] = &StrategyTemplate{
		ID:          "dca_regular",
		Name:        "定期定投策略",
		Description: "定期定额买入，适合长期持有",
		Category:    "dca",
		StrategyType: "dca",
		Config: map[string]interface{}{
			"dca_amount": 100,
			"dca_interval": 60,
		},
		Params: map[string]TemplateParam{
			"dca_amount": {
				Name:        "定投金额",
				Description: "每次定投的金额（USDT）",
				Type:        "number",
				Default:     100.0,
				Min:         ptrFloat64(10),
				Required:    true,
			},
			"dca_interval": {
				Name:        "定投间隔",
				Description: "定投间隔（分钟）",
				Type:        "number",
				Default:     60,
				Min:         ptrFloat64(1),
				Max:         ptrFloat64(1440),
				Required:    true,
			},
		},
		DefaultWeight: 1.0,
	}

	// 马丁格尔策略模板
	m.templates["martingale"] = &StrategyTemplate{
		ID:          "martingale",
		Name:        "马丁格尔策略",
		Description: "亏损后加倍投入，风险较高",
		Category:    "dca",
		StrategyType: "martingale",
		Config: map[string]interface{}{
			"base_amount": 50,
			"multiplier": 2,
			"max_levels": 5,
		},
		Params: map[string]TemplateParam{
			"base_amount": {
				Name:        "基础金额",
				Description: "首次下单金额（USDT）",
				Type:        "number",
				Default:     50.0,
				Min:         ptrFloat64(10),
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
	}

	// 均值回归模板
	m.templates["mean_reversion"] = &StrategyTemplate{
		ID:          "mean_reversion",
		Name:        "均值回归策略",
		Description: "价格偏离均值时进行交易",
		Category:    "grid",
		StrategyType: "mean_reversion",
		Config: map[string]interface{}{
			"mean_period": 20,
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
