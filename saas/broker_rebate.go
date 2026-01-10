package saas

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"quantmesh/logger"
)

// BrokerRebateManager 经纪商返佣管理器
// 支持多交易所的经纪商返佣系统
type BrokerRebateManager struct {
	configs     map[string]*BrokerConfig // 交易所配置
	rebates     map[string]*RebateRecord // 返佣记录
	users       map[string]*UserRebate   // 用户返佣信息
	httpClient  *http.Client
	mu          sync.RWMutex
}

// BrokerConfig 经纪商配置
type BrokerConfig struct {
	Exchange      string  `json:"exchange"`       // 交易所名称
	BrokerID      string  `json:"broker_id"`      // 经纪商ID
	APIKey        string  `json:"api_key"`        // API Key
	SecretKey     string  `json:"secret_key"`     // Secret Key
	Passphrase    string  `json:"passphrase"`     // 部分交易所需要
	
	// 返佣设置
	InviteRebateRate   float64 `json:"invite_rebate_rate"`   // 邀请链接返佣率 (%)
	APIRebateRate      float64 `json:"api_rebate_rate"`      // API交易返佣率 (%)
	TotalRebateRate    float64 `json:"total_rebate_rate"`    // 总返佣率 (%)
	
	// 分成设置
	PlatformShareRate  float64 `json:"platform_share_rate"`  // 平台分成比例 (%)
	UserShareRate      float64 `json:"user_share_rate"`      // 用户分成比例 (%)
	
	// 状态
	Enabled     bool   `json:"enabled"`
	VerifiedAt  int64  `json:"verified_at"`
}

// RebateRecord 返佣记录
type RebateRecord struct {
	ID            string  `json:"id"`
	Exchange      string  `json:"exchange"`
	UserID        string  `json:"user_id"`
	TradeID       string  `json:"trade_id"`
	Symbol        string  `json:"symbol"`
	Side          string  `json:"side"`        // BUY/SELL
	Volume        float64 `json:"volume"`      // 交易量
	Commission    float64 `json:"commission"`  // 手续费
	RebateAmount  float64 `json:"rebate_amount"` // 返佣金额
	RebateType    string  `json:"rebate_type"` // invite/api
	Status        string  `json:"status"`      // pending/paid/failed
	CreatedAt     int64   `json:"created_at"`
	PaidAt        int64   `json:"paid_at"`
}

// UserRebate 用户返佣信息
type UserRebate struct {
	UserID           string  `json:"user_id"`
	InviteCode       string  `json:"invite_code"`       // 邀请码
	InviteLink       string  `json:"invite_link"`       // 邀请链接
	InvitedBy        string  `json:"invited_by"`        // 邀请人
	
	// 统计
	TotalVolume      float64 `json:"total_volume"`      // 总交易量
	TotalCommission  float64 `json:"total_commission"`  // 总手续费
	TotalRebate      float64 `json:"total_rebate"`      // 总返佣
	PendingRebate    float64 `json:"pending_rebate"`    // 待结算返佣
	PaidRebate       float64 `json:"paid_rebate"`       // 已结算返佣
	
	// 邀请统计
	InvitedCount     int     `json:"invited_count"`     // 邀请人数
	InvitedVolume    float64 `json:"invited_volume"`    // 邀请用户交易量
	InvitedRebate    float64 `json:"invited_rebate"`    // 邀请返佣
	
	CreatedAt        int64   `json:"created_at"`
	UpdatedAt        int64   `json:"updated_at"`
}

// RebateStats 返佣统计
type RebateStats struct {
	TotalVolume      float64 `json:"total_volume"`
	TotalCommission  float64 `json:"total_commission"`
	TotalRebate      float64 `json:"total_rebate"`
	PendingRebate    float64 `json:"pending_rebate"`
	PaidRebate       float64 `json:"paid_rebate"`
	UserCount        int     `json:"user_count"`
	TradeCount       int     `json:"trade_count"`
	
	// 按交易所统计
	ByExchange map[string]*ExchangeStats `json:"by_exchange"`
}

// ExchangeStats 交易所统计
type ExchangeStats struct {
	Exchange    string  `json:"exchange"`
	Volume      float64 `json:"volume"`
	Commission  float64 `json:"commission"`
	Rebate      float64 `json:"rebate"`
	TradeCount  int     `json:"trade_count"`
}

// NewBrokerRebateManager 创建经纪商返佣管理器
func NewBrokerRebateManager() *BrokerRebateManager {
	return &BrokerRebateManager{
		configs:    make(map[string]*BrokerConfig),
		rebates:    make(map[string]*RebateRecord),
		users:      make(map[string]*UserRebate),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// AddBrokerConfig 添加经纪商配置
func (m *BrokerRebateManager) AddBrokerConfig(config *BrokerConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 验证配置
	if config.Exchange == "" || config.BrokerID == "" {
		return fmt.Errorf("交易所名称和经纪商ID不能为空")
	}

	m.configs[config.Exchange] = config
	logger.Info("✅ 已添加 %s 经纪商配置: ID=%s, 总返佣率=%.2f%%",
		config.Exchange, config.BrokerID, config.TotalRebateRate)

	return nil
}

// GetBrokerConfig 获取经纪商配置
func (m *BrokerRebateManager) GetBrokerConfig(exchange string) *BrokerConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.configs[exchange]
}

// GenerateInviteLink 生成邀请链接
func (m *BrokerRebateManager) GenerateInviteLink(exchange, userID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	config, ok := m.configs[exchange]
	if !ok {
		return "", fmt.Errorf("未找到 %s 的经纪商配置", exchange)
	}

	// 生成邀请码
	inviteCode := generateInviteCode(userID, config.BrokerID)

	// 生成邀请链接
	var inviteLink string
	switch strings.ToLower(exchange) {
	case "binance":
		inviteLink = fmt.Sprintf("https://www.binance.com/en/register?ref=%s", inviteCode)
	case "okx":
		inviteLink = fmt.Sprintf("https://www.okx.com/join/%s", inviteCode)
	case "bybit":
		inviteLink = fmt.Sprintf("https://www.bybit.com/invite?ref=%s", inviteCode)
	case "bitmex":
		inviteLink = fmt.Sprintf("https://www.bitmex.com/register/%s", inviteCode)
	case "bitget":
		inviteLink = fmt.Sprintf("https://www.bitget.com/en/referral/register?from=%s", inviteCode)
	case "gate":
		inviteLink = fmt.Sprintf("https://www.gate.io/signup/%s", inviteCode)
	default:
		inviteLink = fmt.Sprintf("https://%s.com/register?ref=%s", exchange, inviteCode)
	}

	// 保存用户返佣信息
	if _, exists := m.users[userID]; !exists {
		m.users[userID] = &UserRebate{
			UserID:     userID,
			InviteCode: inviteCode,
			InviteLink: inviteLink,
			CreatedAt:  time.Now().Unix(),
		}
	} else {
		m.users[userID].InviteCode = inviteCode
		m.users[userID].InviteLink = inviteLink
	}

	logger.Info("📎 已为用户 %s 生成 %s 邀请链接: %s", userID, exchange, inviteLink)

	return inviteLink, nil
}

// RecordTrade 记录交易并计算返佣
func (m *BrokerRebateManager) RecordTrade(ctx context.Context, trade *TradeInfo) (*RebateRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	config, ok := m.configs[trade.Exchange]
	if !ok || !config.Enabled {
		return nil, nil // 未启用返佣
	}

	// 计算返佣
	rebateAmount := trade.Commission * config.TotalRebateRate / 100

	// 创建返佣记录
	record := &RebateRecord{
		ID:           generateRecordID(),
		Exchange:     trade.Exchange,
		UserID:       trade.UserID,
		TradeID:      trade.TradeID,
		Symbol:       trade.Symbol,
		Side:         trade.Side,
		Volume:       trade.Volume,
		Commission:   trade.Commission,
		RebateAmount: rebateAmount,
		RebateType:   trade.RebateType,
		Status:       "pending",
		CreatedAt:    time.Now().Unix(),
	}

	m.rebates[record.ID] = record

	// 更新用户统计
	if user, exists := m.users[trade.UserID]; exists {
		user.TotalVolume += trade.Volume
		user.TotalCommission += trade.Commission
		user.TotalRebate += rebateAmount
		user.PendingRebate += rebateAmount
		user.UpdatedAt = time.Now().Unix()
	}

	logger.Info("💰 记录返佣: 用户=%s, 交易所=%s, 交易量=%.2f, 手续费=%.4f, 返佣=%.4f",
		trade.UserID, trade.Exchange, trade.Volume, trade.Commission, rebateAmount)

	return record, nil
}

// TradeInfo 交易信息
type TradeInfo struct {
	Exchange   string  `json:"exchange"`
	UserID     string  `json:"user_id"`
	TradeID    string  `json:"trade_id"`
	Symbol     string  `json:"symbol"`
	Side       string  `json:"side"`
	Volume     float64 `json:"volume"`
	Commission float64 `json:"commission"`
	RebateType string  `json:"rebate_type"` // invite/api
}

// FetchRebatesFromExchange 从交易所获取返佣数据
func (m *BrokerRebateManager) FetchRebatesFromExchange(ctx context.Context, exchange string) error {
	config := m.GetBrokerConfig(exchange)
	if config == nil || !config.Enabled {
		return fmt.Errorf("未启用 %s 的经纪商返佣", exchange)
	}

	switch strings.ToLower(exchange) {
	case "binance":
		return m.fetchBinanceRebates(ctx, config)
	case "okx":
		return m.fetchOKXRebates(ctx, config)
	case "bybit":
		return m.fetchBybitRebates(ctx, config)
	default:
		return fmt.Errorf("暂不支持 %s 的返佣查询", exchange)
	}
}

// fetchBinanceRebates 获取 Binance 返佣数据
func (m *BrokerRebateManager) fetchBinanceRebates(ctx context.Context, config *BrokerConfig) error {
	baseURL := "https://api.binance.com"
	endpoint := "/sapi/v1/broker/rebate/recentRecord"

	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	params := url.Values{}
	params.Set("timestamp", timestamp)

	// 签名
	signature := m.signRequest(params.Encode(), config.SecretKey)
	params.Set("signature", signature)

	reqURL := fmt.Sprintf("%s%s?%s", baseURL, endpoint, params.Encode())
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return err
	}

	req.Header.Set("X-MBX-APIKEY", config.APIKey)

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Binance API 错误: %s", string(body))
	}

	var result struct {
		Data []struct {
			SubAccountID string  `json:"subAccountId"`
			Income       float64 `json:"income"`
			Asset        string  `json:"asset"`
			Time         int64   `json:"time"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	logger.Info("📊 获取 Binance 返佣记录: %d 条", len(result.Data))
	return nil
}

// fetchOKXRebates 获取 OKX 返佣数据
func (m *BrokerRebateManager) fetchOKXRebates(ctx context.Context, config *BrokerConfig) error {
	baseURL := "https://www.okx.com"
	endpoint := "/api/v5/broker/nd/rebate-per-orders"

	timestamp := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")

	// OKX 签名
	preHash := timestamp + "GET" + endpoint
	signature := m.signRequestHMAC(preHash, config.SecretKey)

	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+endpoint, nil)
	if err != nil {
		return err
	}

	req.Header.Set("OK-ACCESS-KEY", config.APIKey)
	req.Header.Set("OK-ACCESS-SIGN", signature)
	req.Header.Set("OK-ACCESS-TIMESTAMP", timestamp)
	req.Header.Set("OK-ACCESS-PASSPHRASE", config.Passphrase)

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("OKX API 错误: %s", string(body))
	}

	logger.Info("📊 获取 OKX 返佣记录成功")
	return nil
}

// fetchBybitRebates 获取 Bybit 返佣数据
func (m *BrokerRebateManager) fetchBybitRebates(ctx context.Context, config *BrokerConfig) error {
	baseURL := "https://api.bybit.com"
	endpoint := "/v5/broker/earning-record"

	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	params := url.Values{}
	params.Set("timestamp", timestamp)
	params.Set("api_key", config.APIKey)

	// 按字母顺序排列参数
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(k)
		sb.WriteString("=")
		sb.WriteString(params.Get(k))
		sb.WriteString("&")
	}
	queryString := strings.TrimSuffix(sb.String(), "&")

	signature := m.signRequest(queryString, config.SecretKey)
	params.Set("sign", signature)

	reqURL := fmt.Sprintf("%s%s?%s", baseURL, endpoint, params.Encode())
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return err
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Bybit API 错误: %s", string(body))
	}

	logger.Info("📊 获取 Bybit 返佣记录成功")
	return nil
}

// GetUserRebate 获取用户返佣信息
func (m *BrokerRebateManager) GetUserRebate(userID string) *UserRebate {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.users[userID]
}

// GetRebateStats 获取返佣统计
func (m *BrokerRebateManager) GetRebateStats() *RebateStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := &RebateStats{
		ByExchange: make(map[string]*ExchangeStats),
	}

	for _, record := range m.rebates {
		stats.TotalVolume += record.Volume
		stats.TotalCommission += record.Commission
		stats.TotalRebate += record.RebateAmount
		stats.TradeCount++

		if record.Status == "pending" {
			stats.PendingRebate += record.RebateAmount
		} else if record.Status == "paid" {
			stats.PaidRebate += record.RebateAmount
		}

		// 按交易所统计
		if _, exists := stats.ByExchange[record.Exchange]; !exists {
			stats.ByExchange[record.Exchange] = &ExchangeStats{
				Exchange: record.Exchange,
			}
		}
		exStats := stats.ByExchange[record.Exchange]
		exStats.Volume += record.Volume
		exStats.Commission += record.Commission
		exStats.Rebate += record.RebateAmount
		exStats.TradeCount++
	}

	stats.UserCount = len(m.users)

	return stats
}

// SettleRebates 结算返佣
func (m *BrokerRebateManager) SettleRebates(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	settledCount := 0
	settledAmount := 0.0

	for _, record := range m.rebates {
		if record.Status == "pending" {
			// TODO: 实际结算逻辑（转账到用户账户）
			record.Status = "paid"
			record.PaidAt = time.Now().Unix()
			settledCount++
			settledAmount += record.RebateAmount

			// 更新用户统计
			if user, exists := m.users[record.UserID]; exists {
				user.PendingRebate -= record.RebateAmount
				user.PaidRebate += record.RebateAmount
				user.UpdatedAt = time.Now().Unix()
			}
		}
	}

	if settledCount > 0 {
		logger.Info("💵 返佣结算完成: %d 笔, 总金额 %.4f USDT", settledCount, settledAmount)
	}

	return nil
}

// GetPendingRebates 获取待结算返佣列表
func (m *BrokerRebateManager) GetPendingRebates() []*RebateRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()

	pending := make([]*RebateRecord, 0)
	for _, record := range m.rebates {
		if record.Status == "pending" {
			pending = append(pending, record)
		}
	}

	return pending
}

// GetUserRebateHistory 获取用户返佣历史
func (m *BrokerRebateManager) GetUserRebateHistory(userID string) []*RebateRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()

	history := make([]*RebateRecord, 0)
	for _, record := range m.rebates {
		if record.UserID == userID {
			history = append(history, record)
		}
	}

	return history
}

// 辅助函数

func generateInviteCode(userID, brokerID string) string {
	data := userID + brokerID + strconv.FormatInt(time.Now().Unix(), 10)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])[:8] // 取前8位
}

func generateRecordID() string {
	return fmt.Sprintf("RB%d%s", time.Now().UnixNano(), generateRandomString(4))
}

func generateRandomString(length int) string {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	result := make([]byte, length)
	for i := range result {
		result[i] = charset[time.Now().UnixNano()%int64(len(charset))]
	}
	return string(result)
}

func (m *BrokerRebateManager) signRequest(data, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(data))
	return hex.EncodeToString(mac.Sum(nil))
}

func (m *BrokerRebateManager) signRequestHMAC(data, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(data))
	return hex.EncodeToString(mac.Sum(nil))
}

// ========== HTTP API 处理器 ==========

// BrokerRebateHandler HTTP API 处理器
type BrokerRebateHandler struct {
	manager *BrokerRebateManager
}

// NewBrokerRebateHandler 创建 API 处理器
func NewBrokerRebateHandler(manager *BrokerRebateManager) *BrokerRebateHandler {
	return &BrokerRebateHandler{manager: manager}
}

// HandleGetStats 获取返佣统计
func (h *BrokerRebateHandler) HandleGetStats(w http.ResponseWriter, r *http.Request) {
	stats := h.manager.GetRebateStats()
	json.NewEncoder(w).Encode(stats)
}

// HandleGetUserRebate 获取用户返佣信息
func (h *BrokerRebateHandler) HandleGetUserRebate(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		http.Error(w, "缺少 user_id 参数", http.StatusBadRequest)
		return
	}

	rebate := h.manager.GetUserRebate(userID)
	if rebate == nil {
		http.Error(w, "用户不存在", http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(rebate)
}

// HandleGenerateInviteLink 生成邀请链接
func (h *BrokerRebateHandler) HandleGenerateInviteLink(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Exchange string `json:"exchange"`
		UserID   string `json:"user_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "无效的请求体", http.StatusBadRequest)
		return
	}

	link, err := h.manager.GenerateInviteLink(req.Exchange, req.UserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{
		"invite_link": link,
	})
}

// HandleGetPendingRebates 获取待结算返佣
func (h *BrokerRebateHandler) HandleGetPendingRebates(w http.ResponseWriter, r *http.Request) {
	pending := h.manager.GetPendingRebates()
	json.NewEncoder(w).Encode(pending)
}

// HandleSettleRebates 结算返佣
func (h *BrokerRebateHandler) HandleSettleRebates(w http.ResponseWriter, r *http.Request) {
	if err := h.manager.SettleRebates(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{
		"status": "success",
	})
}
