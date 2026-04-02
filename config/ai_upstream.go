package config

import "fmt"

// AIUpstreamProfile 命名上游（與 ai.NewAIClient 工廠約定一致）
type AIUpstreamProfile struct {
	Provider string `yaml:"provider" json:"provider"`
	Model    string `yaml:"model" json:"model"`
	APIKey   string `yaml:"api_key" json:"api_key"`
	BaseURL  string `yaml:"base_url" json:"base_url"`
}

// AIResolvedUpstream 解析後的 provider/model/api_key/base_url，供調用方使用
type AIResolvedUpstream struct {
	Provider string
	Model    string
	APIKey   string
	BaseURL  string
	Source   string // "flat" 或 "profile:<name>"
}

// profileAPIKeyFallback 當 profile 未填 api_key 時，從全局 ai 扁平字段回退（與文檔一致）
func profileAPIKeyFallback(c *Config, provider string) string {
	p := provider
	if p == "" {
		p = "gemini"
	}
	if p == "gemini" {
		if c.AI.GeminiAPIKey != "" {
			return c.AI.GeminiAPIKey
		}
		return c.AI.APIKey
	}
	return c.AI.APIKey
}

func resolveFlatGlobalAI(c *Config) AIResolvedUpstream {
	p := c.AI.Provider
	if p == "" {
		p = "gemini"
	}
	key := profileAPIKeyFallback(c, p)
	return AIResolvedUpstream{
		Provider: p,
		Model:    "",
		APIKey:   key,
		BaseURL:  c.AI.BaseURL,
		Source:   "flat",
	}
}

// ResolveAIUpstreamByRef 按名稱解析 ai.upstreams 中的條目；ref 為空則返回錯誤
func ResolveAIUpstreamByRef(c *Config, ref string) (AIResolvedUpstream, error) {
	if ref == "" {
		return AIResolvedUpstream{}, fmt.Errorf("upstream_ref 為空")
	}
	if c.AI.Upstreams == nil {
		return AIResolvedUpstream{}, fmt.Errorf("ai.upstreams 未配置")
	}
	p, ok := c.AI.Upstreams[ref]
	if !ok {
		return AIResolvedUpstream{}, fmt.Errorf("上游 %q 在 ai.upstreams 中不存在", ref)
	}
	prov := p.Provider
	if prov == "" {
		prov = "gemini"
	}
	key := p.APIKey
	if key == "" {
		key = profileAPIKeyFallback(c, prov)
	}
	return AIResolvedUpstream{
		Provider: prov,
		Model:    p.Model,
		APIKey:   key,
		BaseURL:  p.BaseURL,
		Source:   "profile:" + ref,
	}, nil
}

// ResolveGlobalAI 全局默認上游：無 upstreams 或 default_upstream 為空且存在 upstreams 時使用扁平字段；
// 若 default_upstream 非空則使用對應 profile。
func ResolveGlobalAI(c *Config) AIResolvedUpstream {
	hasUp := len(c.AI.Upstreams) > 0
	if hasUp && c.AI.DefaultUpstream != "" {
		if r, err := ResolveAIUpstreamByRef(c, c.AI.DefaultUpstream); err == nil {
			return r
		}
	}
	if hasUp && c.AI.DefaultUpstream == "" {
		return resolveFlatGlobalAI(c)
	}
	return resolveFlatGlobalAI(c)
}

// ResolveGlobalGeminiAPIKey 供仍綁定 Google Gemini HTTP 的 API 使用；若默認上游非 gemini 則返回空
func ResolveGlobalGeminiAPIKey(c *Config) string {
	r := ResolveGlobalAI(c)
	if r.Provider != "" && r.Provider != "gemini" {
		return ""
	}
	return r.APIKey
}

// ResolveInspectorAI 智子巡檢：優先 inspector.ai.upstream_ref，否則全局 + inspector.ai.provider/model 覆蓋
func ResolveInspectorAI(c *Config) AIResolvedUpstream {
	if c.Inspector.AI.UpstreamRef != "" {
		r, err := ResolveAIUpstreamByRef(c, c.Inspector.AI.UpstreamRef)
		if err != nil {
			return resolveFlatGlobalAI(c)
		}
		if c.Inspector.AI.Model != "" {
			r.Model = c.Inspector.AI.Model
		}
		if c.Inspector.AI.Provider != "" {
			r.Provider = c.Inspector.AI.Provider
		}
		return r
	}
	r := ResolveGlobalAI(c)
	if c.Inspector.AI.Provider != "" {
		r.Provider = c.Inspector.AI.Provider
	}
	if c.Inspector.AI.Model != "" {
		r.Model = c.Inspector.AI.Model
	}
	return r
}

// ResolveInspectorGeminiAPIKey 智子巡檢仍使用 Gemini 客戶端時的 key；非 gemini 則返回空
func ResolveInspectorGeminiAPIKey(c *Config) string {
	r := ResolveInspectorAI(c)
	if r.Provider != "" && r.Provider != "gemini" {
		return ""
	}
	return r.APIKey
}

// ResolveModuleAI 按子模塊 optional upstream_ref 解析；未設置則回退全局
func ResolveModuleAI(c *Config, upstreamRef string) AIResolvedUpstream {
	if upstreamRef != "" {
		if r, err := ResolveAIUpstreamByRef(c, upstreamRef); err == nil {
			return r
		}
	}
	return ResolveGlobalAI(c)
}

// ApplyNewsMonitorAIFromUpstreamRef 在 Validate 中調用：若設置 upstream_ref 則從 profile 合併到 news_monitor.ai_provider
func ApplyNewsMonitorAIFromUpstreamRef(c *Config) error {
	ref := c.NewsMonitor.AIProvider.UpstreamRef
	if ref == "" {
		return nil
	}
	if c.AI.Upstreams == nil {
		return fmt.Errorf("news_monitor.ai_provider.upstream_ref=%q 但 ai.upstreams 未配置", ref)
	}
	p, ok := c.AI.Upstreams[ref]
	if !ok {
		return fmt.Errorf("news_monitor.ai_provider.upstream_ref=%q 在 ai.upstreams 中不存在", ref)
	}
	prov := p.Provider
	if prov == "" {
		prov = "gemini"
	}
	c.NewsMonitor.AIProvider.Provider = prov
	if p.Model != "" {
		c.NewsMonitor.AIProvider.Model = p.Model
	}
	key := p.APIKey
	if key == "" {
		key = profileAPIKeyFallback(c, prov)
	}
	c.NewsMonitor.AIProvider.APIKey = key
	if p.BaseURL != "" {
		c.NewsMonitor.AIProvider.BaseURL = p.BaseURL
	}
	return nil
}

// ValidateAIUpstreamRefs 校驗 default_upstream 與各 optional upstream_ref 是否存在
func ValidateAIUpstreamRefs(c *Config) error {
	if c.AI.DefaultUpstream != "" {
		if c.AI.Upstreams == nil {
			return fmt.Errorf("ai.default_upstream=%q 已設置但 ai.upstreams 為空", c.AI.DefaultUpstream)
		}
		if _, ok := c.AI.Upstreams[c.AI.DefaultUpstream]; !ok {
			return fmt.Errorf("ai.default_upstream=%q 在 ai.upstreams 中不存在", c.AI.DefaultUpstream)
		}
	}
	if ref := c.Inspector.AI.UpstreamRef; ref != "" {
		if c.AI.Upstreams == nil {
			return fmt.Errorf("inspector.ai.upstream_ref=%q 但 ai.upstreams 未配置", ref)
		}
		if _, ok := c.AI.Upstreams[ref]; !ok {
			return fmt.Errorf("inspector.ai.upstream_ref=%q 在 ai.upstreams 中不存在", ref)
		}
	}
	mods := []struct {
		ref string
		tag string
	}{
		{c.AI.Modules.MarketAnalysis.UpstreamRef, "ai.modules.market_analysis.upstream_ref"},
		{c.AI.Modules.ParameterOptimization.UpstreamRef, "ai.modules.parameter_optimization.upstream_ref"},
		{c.AI.Modules.RiskAnalysis.UpstreamRef, "ai.modules.risk_analysis.upstream_ref"},
		{c.AI.Modules.SentimentAnalysis.UpstreamRef, "ai.modules.sentiment_analysis.upstream_ref"},
		{c.AI.Modules.StrategyGeneration.UpstreamRef, "ai.modules.strategy_generation.upstream_ref"},
		{c.AI.Modules.PolymarketSignal.UpstreamRef, "ai.modules.polymarket_signal.upstream_ref"},
	}
	for _, m := range mods {
		if m.ref == "" {
			continue
		}
		if c.AI.Upstreams == nil {
			return fmt.Errorf("%s=%q 但 ai.upstreams 未配置", m.tag, m.ref)
		}
		if _, ok := c.AI.Upstreams[m.ref]; !ok {
			return fmt.Errorf("%s=%q 在 ai.upstreams 中不存在", m.tag, m.ref)
		}
	}
	return nil
}
