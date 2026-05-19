package web

import (
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"

	"quantmesh/config"
	"quantmesh/position"

	"github.com/gin-gonic/gin"
)

// enrichOpeningStatus 用開倉管理狀態（opening_paused / pause_reason）豐富 SystemStatus
func enrichOpeningStatus(st *SystemStatus) {
	if symbolManagerProvider == nil || st.Exchange == "" || st.Symbol == "" {
		return
	}
	rtInterface, exists := symbolManagerProvider.Get(st.Exchange, st.Symbol)
	if !exists {
		return
	}
	rtVal := reflect.ValueOf(rtInterface)
	if rtVal.Kind() == reflect.Ptr {
		rtVal = rtVal.Elem()
	}
	spmField := rtVal.FieldByName("SuperPositionManager")
	if !spmField.IsValid() || spmField.IsNil() {
		return
	}
	if spm, ok := spmField.Interface().(*position.SuperPositionManager); ok && spm != nil {
		st.OpeningPaused = spm.IsOpeningPaused()
		st.PauseReason = spm.GetOpeningPauseReason()
	}
}

func getStatus(c *gin.Context) {
	exchange := c.Query("exchange")
	symbol := c.Query("symbol")
	marketType := c.Query("market_type")
	if marketType == "" {
		marketType = "futures"
	}

	// 如果指定了 exchange 和 symbol，尝試獲取對应的状態（按 exchange:symbol:market_type 精確匹配）
	if exchange != "" && symbol != "" {
		statusMu.RLock()
		st, ok := resolveStatusBySymbol(exchange, symbol, marketType)
		if ok && st != nil {
			copySt := *st
			statusMu.RUnlock()
			enrichOpeningStatus(&copySt)
			c.JSON(http.StatusOK, &copySt)
			return
		}
		statusMu.RUnlock()

		// 没有找到运行中的状態，检查配置中是否有該 exchange:symbol:market_type
		if globalConfig != nil {
			cfg := globalConfig
			if cfg != nil {
				for _, symCfg := range cfg.Trading.Symbols {
					if strings.EqualFold(symCfg.Exchange, exchange) &&
						strings.EqualFold(symCfg.Symbol, symbol) &&
						strings.EqualFold(symCfg.GetMarketType(), marketType) {
						// 配置中存在但未运行，返回正确的状態信息（含 market_type）
						c.JSON(http.StatusOK, &SystemStatus{
							Running:       false,
							Exchange:      exchange,
							Symbol:        symbol,
							MarketType:    marketType,
							CurrentPrice:  0,
							TotalPnL:      0,
							TotalTrades:   0,
							RiskTriggered: false,
							Uptime:        0,
						})
						return
					}
				}
			}
		}

		// 配置中也没有找到，返回未运行状態（但包含请求的 exchange、symbol、market_type）
		c.JSON(http.StatusOK, &SystemStatus{
			Running:       false,
			Exchange:      exchange,
			Symbol:        symbol,
			MarketType:    marketType,
			CurrentPrice:  0,
			TotalPnL:      0,
			TotalTrades:   0,
			RiskTriggered: false,
			Uptime:        0,
		})
		return
	}

	// 没有指定 exchange 和 symbol，使用原来的逻辑
	status := pickStatus(c)
	if status == nil {
		c.JSON(http.StatusOK, &SystemStatus{
			Running: false,
		})
		return
	}
	c.JSON(http.StatusOK, status)
}

// StatusesResponse 批量状態响应
type StatusesResponse struct {
	Statuses []SystemStatus `json:"statuses"`
}

// getStatuses 批量返回所有交易對的系统状態（用於概览页一次拉取）
// GET /api/statuses
// key 為 exchange:symbol:market_type，避免同交易所同币种的現貨/合約互相覆蓋
func getStatuses(c *gin.Context) {
	statusMap := make(map[string]SystemStatus)

	// 1) 先從配置中構造"未运行"的默认状態，key 含 market_type
	if globalConfig != nil {
		cfg := globalConfig
		if cfg != nil {
			for _, sym := range cfg.Trading.Symbols {
				if sym.Symbol == "" {
					continue
				}
				ex := sym.Exchange
				if ex == "" {
					ex = cfg.App.CurrentExchange
				}
				if ex == "" {
					continue
				}
				mt := sym.GetMarketType()
				if mt == "" {
					mt = "futures"
				}
				key := makeSymbolKey(ex, sym.Symbol, mt)
				if _, exists := statusMap[key]; !exists {
					statusMap[key] = SystemStatus{
						Running:       false,
						Exchange:      strings.ToLower(ex),
						Symbol:        sym.Symbol,
						MarketType:    mt,
						CurrentPrice:  0,
						TotalPnL:      0,
						TotalTrades:   0,
						RiskTriggered: false,
						Uptime:        0,
					}
				}
			}

			// 兼容舊的單交易對配置
			if len(cfg.Trading.Symbols) == 0 && cfg.Trading.Symbol != "" && cfg.App.CurrentExchange != "" {
				key := makeSymbolKey(cfg.App.CurrentExchange, cfg.Trading.Symbol, "futures")
				if _, exists := statusMap[key]; !exists {
					statusMap[key] = SystemStatus{
						Running:       false,
						Exchange:      strings.ToLower(cfg.App.CurrentExchange),
						Symbol:        cfg.Trading.Symbol,
						MarketType:    "futures",
						CurrentPrice:  0,
						TotalPnL:      0,
						TotalTrades:   0,
						RiskTriggered: false,
						Uptime:        0,
					}
				}
			}
		}
	}

	// 2) 再用运行中状態覆盖，key 使用註冊時的 exchange:symbol:market_type
	statusMu.RLock()
	for key, st := range statusBySymbol {
		if st == nil {
			continue
		}
		copySt := *st
		copySt.Exchange = strings.ToLower(copySt.Exchange)
		statusMap[key] = copySt
	}
	statusMu.RUnlock()

	// 3) 向后兼容：如果没有多交易對數據，使用舊的單交易對状態
	if len(statusMap) == 0 && currentStatus != nil {
		statusMu.RLock()
		copySt := *currentStatus
		statusMu.RUnlock()
		copySt.Exchange = strings.ToLower(copySt.Exchange)
		mt := copySt.MarketType
		if mt == "" {
			mt = "futures"
		}
		key := makeSymbolKey(copySt.Exchange, copySt.Symbol, mt)
		statusMap[key] = copySt
	}

	// 4) 填充開倉管理狀態（opening_paused / pause_reason）
	for key, st := range statusMap {
		enrichOpeningStatus(&st)
		statusMap[key] = st
	}

	// 5) 轉為 slice 並排序
	statuses := make([]SystemStatus, 0, len(statusMap))
	for _, st := range statusMap {
		statuses = append(statuses, st)
	}
	sort.Slice(statuses, func(i, j int) bool {
		if statuses[i].Exchange == statuses[j].Exchange {
			return strings.ToLower(statuses[i].Symbol) < strings.ToLower(statuses[j].Symbol)
		}
		return statuses[i].Exchange < statuses[j].Exchange
	})

	c.JSON(http.StatusOK, StatusesResponse{Statuses: statuses})
}

// SymbolItem 用於返回可用的交易所/交易對列表
type SymbolItem struct {
	Exchange     string  `json:"exchange"`
	Symbol       string  `json:"symbol"`
	IsActive     bool    `json:"is_active"`
	CurrentPrice float64 `json:"current_price"`
	MarketType   string  `json:"market_type,omitempty"` // 市場類型：spot/futures
	Direction    string  `json:"direction,omitempty"`   // 交易方向：LONG/SHORT，預設 LONG
}

// getSymbols 返回可用的交易對列表
func getSymbols(c *gin.Context) {
	// 使用 map 来去重，key 為 exchange:symbol
	symbolMap := make(map[string]*SymbolItem)
	activeList := make([]SymbolItem, 0)
	inactiveList := make([]SymbolItem, 0)

	// 首先從配置文件中读取所有配置的交易對
	if globalConfig != nil {
		cfg := globalConfig
		if cfg != nil {
			// 從交易對配置中读取
			for _, sym := range cfg.Trading.Symbols {
				if sym.Symbol == "" {
					continue
				}
				exchange := sym.Exchange
				if exchange == "" {
					exchange = cfg.App.CurrentExchange
				}
				if exchange == "" {
					continue
				}
				marketType := sym.GetMarketType()
				key := makeSymbolKey(exchange, sym.Symbol, marketType)
				if _, exists := symbolMap[key]; !exists {
					symbolMap[key] = &SymbolItem{
						Exchange:     strings.ToLower(exchange),
						Symbol:       sym.Symbol,
						IsActive:     false, // 默认未运行，后面會更新
						CurrentPrice: 0,
						MarketType:   marketType,
						Direction:    sym.GetDirection(),
					}
				}
			}
			// 如果只有單交易對配置
			if len(cfg.Trading.Symbols) == 0 && cfg.Trading.Symbol != "" {
				exchange := cfg.App.CurrentExchange
				if exchange != "" {
					key := makeSymbolKey(exchange, cfg.Trading.Symbol, "futures")
					if _, exists := symbolMap[key]; !exists {
						symbolMap[key] = &SymbolItem{
							Exchange:     strings.ToLower(exchange),
							Symbol:       cfg.Trading.Symbol,
							IsActive:     false,
							CurrentPrice: 0,
							MarketType:   "futures", // 舊版單交易對配置默認為合約
							Direction:    config.NormalizeDirection(cfg.Trading.Direction),
						}
					}
				}
			}
		}
	}

	// 然后從运行状態中更新（确保正在运行的交易對状態正确）
	statusMu.RLock()
	for registeredKey, st := range statusBySymbol {
		if st == nil {
			continue
		}
		// 使用註冊時的 key（已含 market_type），避免重建 key 時丟失 market_type
		if item, exists := symbolMap[registeredKey]; exists {
			// 更新已存在的交易對状態
			item.IsActive = st.Running
			item.CurrentPrice = st.CurrentPrice
		} else {
			// 添加新的运行中的交易對
			mt := st.MarketType
			if mt == "" {
				mt = "futures"
			}
			symbolMap[registeredKey] = &SymbolItem{
				Exchange:     strings.ToLower(st.Exchange),
				Symbol:       st.Symbol,
				IsActive:     st.Running,
				CurrentPrice: st.CurrentPrice,
				MarketType:   mt,
			}
		}
	}
	statusMu.RUnlock()

	// 向后兼容：如果没有多交易對數據，使用舊的單交易對状態
	if len(symbolMap) == 0 && currentStatus != nil {
		key := strings.ToLower(fmt.Sprintf("%s:%s", currentStatus.Exchange, currentStatus.Symbol))
		symbolMap[key] = &SymbolItem{
			Exchange:     strings.ToLower(currentStatus.Exchange),
			Symbol:       currentStatus.Symbol,
			IsActive:     currentStatus.Running,
			CurrentPrice: currentStatus.CurrentPrice,
		}
	}

	// 轉换為列表並分组
	for _, item := range symbolMap {
		if item.IsActive {
			activeList = append(activeList, *item)
		} else {
			inactiveList = append(inactiveList, *item)
		}
	}

	// 活跃的交易對排在前面
	list := make([]SymbolItem, 0)
	list = append(list, activeList...)
	list = append(list, inactiveList...)

	c.JSON(http.StatusOK, gin.H{"symbols": list})
}

// getVersion 返回版本号（不需要认证）
func getVersion(c *gin.Context) {
	version := appVersion
	if version == "" {
		version = "unknown"
	}
	c.JSON(http.StatusOK, gin.H{"version": version})
}

// versionHeaderMiddleware 在响应头中設置 X-App-Version，便於排查
func versionHeaderMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		v := appVersion
		if v == "" {
			v = "unknown"
		}
		c.Header("X-App-Version", v)
		c.Next()
	}
}

// effectiveConfigForExchangeList 與 GET /api/config/json 一致：優先使用 Web 保存後的
// FileConfigManager 內存配置；僅在尚未初始化時回退 globalConfig（啟動時注入）。
// 避免「後台已寫入主庫與 fcm.currentConfig，但 globalConfig 未同步」導致新建 Bot 下拉里看不到剛配置的交易所。
func effectiveConfigForExchangeList() *config.Config {
	if cfg := GetConfig(); cfg != nil {
		return cfg
	}
	return globalConfig
}

// getExchanges 返回所有配置的交易所列表
func getExchanges(c *gin.Context) {
	exchangeSet := make(map[string]bool)

	// 首先從當前運行配置读取所有配置的交易所（含 Web 保存後的最新 exchanges 鍵）
	cfg := effectiveConfigForExchangeList()
	if cfg != nil {
		// 從配置的 exchanges 中读取
		for ex := range cfg.Exchanges {
			if ex != "" {
				exchangeSet[strings.ToLower(ex)] = true
			}
		}
		// 從交易對配置中读取交易所
		for _, sym := range cfg.Trading.Symbols {
			if sym.Exchange != "" {
				exchangeSet[strings.ToLower(sym.Exchange)] = true
			} else if cfg.App.CurrentExchange != "" {
				exchangeSet[strings.ToLower(cfg.App.CurrentExchange)] = true
			}
		}
		// 如果只有單交易對配置
		if len(cfg.Trading.Symbols) == 0 && cfg.Trading.Symbol != "" {
			if cfg.App.CurrentExchange != "" {
				exchangeSet[strings.ToLower(cfg.App.CurrentExchange)] = true
			}
		}
	}

	// 然后從运行状態中读取（确保正在运行的交易所也在列表中）
	statusMu.RLock()
	for _, st := range statusBySymbol {
		if st == nil {
			continue
		}
		exchangeSet[strings.ToLower(st.Exchange)] = true
	}
	statusMu.RUnlock()

	// 向后兼容
	if len(exchangeSet) == 0 && currentStatus != nil {
		exchangeSet[strings.ToLower(currentStatus.Exchange)] = true
	}

	exchanges := make([]string, 0, len(exchangeSet))
	for ex := range exchangeSet {
		exchanges = append(exchanges, ex)
	}

	// 排序交易所列表（可選，但有助於一致性）
	sort.Strings(exchanges)

	c.JSON(http.StatusOK, gin.H{"exchanges": exchanges})
}
