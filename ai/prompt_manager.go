package ai

import (
	"fmt"
	"sync"

	"quantmesh/logger"
	"quantmesh/storage"
)

// PromptManager 提示词管理器
type PromptManager struct {
	storage storage.Storage
	cache   map[string]*AIPromptTemplate
	mu      sync.RWMutex
}

// AIPromptTemplate AI提示词模板（简化版，避免循环依赖）
type AIPromptTemplate struct {
	Module       string
	Template     string
	SystemPrompt string
}

// NewPromptManager 创建提示词管理器
func NewPromptManager(storage storage.Storage) *PromptManager {
	pm := &PromptManager{
		storage: storage,
		cache:   make(map[string]*AIPromptTemplate),
	}
	
	// 初始化默认提示词
	pm.initDefaultPrompts()
	
	// 加载缓存
	pm.ReloadCache()
	
	return pm
}

// GetPrompt 获取提示词（优先从缓存）
// 返回: template, systemPrompt, error
func (pm *PromptManager) GetPrompt(module string) (string, string, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	
	if template, ok := pm.cache[module]; ok {
		return template.Template, template.SystemPrompt, nil
	}
	
	// 如果缓存中没有，尝试从数据库加载
	if pm.storage != nil {
		dbTemplate, err := pm.storage.GetAIPromptTemplate(module)
		if err != nil {
			return "", "", err
		}
		if dbTemplate != nil {
			// 转换为内部格式
			template := &AIPromptTemplate{
				Module:       dbTemplate.Module,
				Template:     dbTemplate.Template,
				SystemPrompt: dbTemplate.SystemPrompt,
			}
			// 更新缓存
			pm.mu.RUnlock()
			pm.mu.Lock()
			pm.cache[module] = template
			pm.mu.Unlock()
			pm.mu.RLock()
			return template.Template, template.SystemPrompt, nil
		}
	}
	
	// 如果都没有，返回默认提示词
	template, systemPrompt := pm.getDefaultPrompt(module)
	return template, systemPrompt, nil
}

// UpdatePrompt 更新提示词（更新数据库和缓存）
func (pm *PromptManager) UpdatePrompt(module, template, systemPrompt string) error {
	if pm.storage == nil {
		return fmt.Errorf("存储服务未初始化")
	}
	
	// 转换为存储格式
	promptTemplate := &storage.AIPromptTemplate{
		Module:        module,
		Template:      template,
		SystemPrompt:  systemPrompt,
	}
	
	// 更新数据库
	if err := pm.storage.SetAIPromptTemplate(promptTemplate); err != nil {
		return fmt.Errorf("更新数据库失败: %w", err)
	}
	
	// 更新缓存
	pm.mu.Lock()
	pm.cache[module] = &AIPromptTemplate{
		Module:       module,
		Template:     template,
		SystemPrompt: systemPrompt,
	}
	pm.mu.Unlock()
	
	logger.Info("✅ 已更新AI提示词模板: %s", module)
	return nil
}

// ReloadCache 重新加载所有提示词到缓存
func (pm *PromptManager) ReloadCache() error {
	if pm.storage == nil {
		return nil
	}
	
	templates, err := pm.storage.GetAllAIPromptTemplates()
	if err != nil {
		return fmt.Errorf("加载提示词失败: %w", err)
	}
	
	pm.mu.Lock()
	pm.cache = make(map[string]*AIPromptTemplate)
	for _, dbTemplate := range templates {
		pm.cache[dbTemplate.Module] = &AIPromptTemplate{
			Module:       dbTemplate.Module,
			Template:     dbTemplate.Template,
			SystemPrompt: dbTemplate.SystemPrompt,
		}
	}
	pm.mu.Unlock()
	
	logger.Info("✅ 已加载 %d 个AI提示词模板到缓存", len(templates))
	return nil
}

// GetAllPrompts 获取所有提示词
func (pm *PromptManager) GetAllPrompts() (map[string]*AIPromptTemplate, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	
	result := make(map[string]*AIPromptTemplate)
	for k, v := range pm.cache {
		result[k] = v
	}
	
	return result, nil
}

// initDefaultPrompts 初始化默认提示词（如果数据库中没有）
func (pm *PromptManager) initDefaultPrompts() {
	if pm.storage == nil {
		return
	}
	
	modules := []string{
		"market_analysis",
		"parameter_optimization",
		"risk_analysis",
		"sentiment_analysis",
	}
	
	for _, module := range modules {
		// 检查是否已存在
		existing, err := pm.storage.GetAIPromptTemplate(module)
		if err != nil {
			logger.Warn("⚠️ 检查提示词模板失败: %v", err)
			continue
		}
		
		// 如果不存在，插入默认值
		if existing == nil {
			template, systemPrompt := pm.getDefaultPrompt(module)
			defaultTemplate := &storage.AIPromptTemplate{
				Module:       module,
				Template:     template,
				SystemPrompt: systemPrompt,
			}
			
			if err := pm.storage.SetAIPromptTemplate(defaultTemplate); err != nil {
				logger.Warn("⚠️ 初始化默认提示词失败 %s: %v", module, err)
			} else {
				logger.Info("✅ 已初始化默认提示词模板: %s", module)
			}
		}
	}
}

// getDefaultPrompt 获取默认提示词
func (pm *PromptManager) getDefaultPrompt(module string) (string, string) {
	switch module {
	case "market_analysis":
		return `你是一个专业的加密货币市场分析师。请分析以下市场数据并给出专业的市场分析。

交易对: %s
当前价格: %.2f
成交量: %.2f

请分析市场趋势（up/down/side）、给出价格预测、交易信号（buy/sell/hold）以及分析理由。

请以JSON格式返回，格式如下：
{
  "trend": "up|down|side",
  "confidence": 0.0-1.0,
  "signal": "buy|sell|hold",
  "reasoning": "分析理由"
}`, "你是一个专业的加密货币市场分析师。"
		
	case "parameter_optimization":
		return `你是一个专业的量化交易参数优化专家。请根据以下数据优化交易参数。

当前参数:
- 价格间隔: %.2f
- 买单窗口: %d
- 卖单窗口: %d
- 订单金额: %.2f

历史表现:
- 总交易数: %d
- 胜率: %.2f%%
- 总盈亏: %.2f
- 最大回撤: %.2f%%

请推荐优化的参数值，并说明预期改进和理由。

请以JSON格式返回，格式如下：
{
  "recommended_params": {
    "price_interval": 数值,
    "buy_window_size": 数值,
    "sell_window_size": 数值,
    "order_quantity": 数值
  },
  "expected_improvement": 预期改进百分比,
  "confidence": 0.0-1.0,
  "reasoning": "优化理由"
}`, "你是一个专业的量化交易参数优化专家。"
		
	case "risk_analysis":
		return `你是一个专业的风险管理专家。请分析以下交易风险。

交易对: %s
当前价格: %.2f
账户余额: %.2f
已用保证金: %.2f
市场波动率: %.2f%%
持仓数量: %d
未完成订单: %d

请评估风险等级（low/medium/high/critical）、风险评分（0-1）、警告和建议。

请以JSON格式返回，格式如下：
{
  "risk_score": 0.0-1.0,
  "risk_level": "low|medium|high|critical",
  "warnings": ["警告1", "警告2"],
  "recommendations": ["建议1", "建议2"],
  "reasoning": "分析理由"
}`, "你是一个专业的风险管理专家。"
		
	case "sentiment_analysis":
		return `你是一个专业的市场情绪分析师。请分析以下市场情绪数据。

交易对: %s

新闻摘要:
%s

%s%s

请分析市场情绪（-1到1，-1极度悲观，1极度乐观）、趋势（bullish/bearish/neutral）、关键因素和理由。

请以JSON格式返回，格式如下：
{
  "sentiment_score": -1.0到1.0,
  "trend": "bullish|bearish|neutral",
  "key_factors": ["因素1", "因素2"],
  "news_summary": "新闻摘要",
  "reasoning": "分析理由"
}`, "你是一个专业的市场情绪分析师。"
		
	default:
		return "", ""
	}
}

