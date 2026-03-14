package web

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"quantmesh/exchange"
	"quantmesh/logger"
	"quantmesh/metrics"
	"quantmesh/storage"
	"quantmesh/utils"

	"github.com/gin-gonic/gin"
)

// getFixSessions 获取 FIX 会话状态列表
// GET /api/fix/sessions
func getFixSessions(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		storageProv = storageServiceProvider
	}
	if storageProv == nil || storageProv.GetStorage() == nil {
		c.JSON(http.StatusOK, gin.H{"sessions": []interface{}{}, "total_count": 0})
		return
	}
	st := storageProv.GetStorage()

	limit := 100
	offset := 0
	if l, err := strconv.Atoi(c.DefaultQuery("limit", "100")); err == nil && l > 0 {
		limit = l
	}
	if o, err := strconv.Atoi(c.DefaultQuery("offset", "0")); err == nil && o >= 0 {
		offset = o
	}

	sessions, err := st.ListFixSessionStates(limit, offset)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "error.query_failed", err)
		return
	}
	resp := make([]map[string]interface{}, 0, len(sessions))
	for _, s := range sessions {
		botID := s.BotID
		if botID == "" {
			botID = getFixSessionBotBinding(s.SessionID)
		}
		item := map[string]interface{}{
			"session_id":        s.SessionID,
			"bot_id":            botID,
			"role":              s.Role,
			"begin_string":      s.BeginString,
			"sender_comp_id":    s.SenderCompID,
			"target_comp_id":    s.TargetCompID,
			"next_sender_seq":   s.NextSenderSeq,
			"next_target_seq":   s.NextTargetSeq,
			"is_logged_on":      s.IsLoggedOn,
			"last_logon_at":     nil,
			"last_heartbeat_at": nil,
			"updated_at":        utils.ToUTC8(s.UpdatedAt),
		}
		if s.LastLogonAt != nil {
			item["last_logon_at"] = utils.ToUTC8(*s.LastLogonAt)
		}
		if s.LastHeartbeatAt != nil {
			item["last_heartbeat_at"] = utils.ToUTC8(*s.LastHeartbeatAt)
		}
		resp = append(resp, item)
	}
	c.JSON(http.StatusOK, gin.H{
		"sessions":    resp,
		"total_count": len(resp),
	})
}

// getFixOrderLinks 获取 FIX 主订单映射列表
// GET /api/fix/orders
func getFixOrderLinks(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		storageProv = storageServiceProvider
	}
	if storageProv == nil || storageProv.GetStorage() == nil {
		c.JSON(http.StatusOK, gin.H{"orders": []interface{}{}, "total_count": 0})
		return
	}
	st := storageProv.GetStorage()

	sessionID := c.Query("session_id")
	ordStatus := c.Query("ord_status")
	limit := 100
	offset := 0
	if l, err := strconv.Atoi(c.DefaultQuery("limit", "100")); err == nil && l > 0 {
		limit = l
	}
	if o, err := strconv.Atoi(c.DefaultQuery("offset", "0")); err == nil && o >= 0 {
		offset = o
	}

	links, err := st.ListFixOrderLinks(sessionID, ordStatus, limit, offset)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "error.query_failed", err)
		return
	}
	resp := make([]map[string]interface{}, 0, len(links))
	for _, l := range links {
		resp = append(resp, map[string]interface{}{
			"id":                l.ID,
			"session_id":        l.SessionID,
			"cl_ord_id":         l.ClOrdID,
			"orig_cl_ord_id":    l.OrigClOrdID,
			"bot_id":            l.BotID,
			"exchange":          l.Exchange,
			"symbol":            l.Symbol,
			"side":              l.Side,
			"internal_order_id": l.InternalOrderID,
			"last_exec_id":      l.LastExecID,
			"ord_status":        l.OrdStatus,
			"cum_qty":           l.CumQty,
			"leaves_qty":        l.LeavesQty,
			"avg_px":            l.AvgPx,
			"created_at":        utils.ToUTC8(l.CreatedAt),
			"updated_at":        utils.ToUTC8(l.UpdatedAt),
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"orders":      resp,
		"total_count": len(resp),
	})
}

type fixLogonRequest struct {
	SessionID      string `json:"session_id" binding:"required"`
	BotID          string `json:"bot_id" binding:"required"`
	Role           string `json:"role"`
	BeginString    string `json:"begin_string"`
	SenderCompID   string `json:"sender_comp_id"`
	TargetCompID   string `json:"target_comp_id"`
	ResetSeqNumFlg bool   `json:"reset_seq_num_flg"`
}

type fixHeartbeatRequest struct {
	SessionID string `json:"session_id" binding:"required"`
}

type fixNewOrderRequest struct {
	SessionID    string  `json:"session_id" binding:"required"`
	ClOrdID      string  `json:"cl_ord_id" binding:"required"`
	Symbol       string  `json:"symbol"`
	Side         string  `json:"side" binding:"required"`
	Price        float64 `json:"price" binding:"required,gt=0"`
	OrderQty     float64 `json:"order_qty" binding:"required,gt=0"`
	OrdType      string  `json:"ord_type"`
	ReduceOnly   bool    `json:"reduce_only"`
	PostOnly     bool    `json:"post_only"`
	StrategyName string  `json:"strategy_name"`
	StrategyType string  `json:"strategy_type"`
}

type fixCancelOrderRequest struct {
	SessionID   string `json:"session_id" binding:"required"`
	ClOrdID     string `json:"cl_ord_id" binding:"required"`
	OrigClOrdID string `json:"orig_cl_ord_id" binding:"required"`
}

type fixReplaceOrderRequest struct {
	SessionID   string  `json:"session_id" binding:"required"`
	ClOrdID     string  `json:"cl_ord_id" binding:"required"`
	OrigClOrdID string `json:"orig_cl_ord_id" binding:"required"`
	Price       float64 `json:"price" binding:"required,gt=0"`
	OrderQty    float64 `json:"order_qty" binding:"required,gt=0"`
}

var (
	fixSessionBotBinding   = make(map[string]string)
	fixSessionBindingMutex sync.RWMutex
)

func isFixEnabled() bool {
	if globalConfig == nil {
		return true // 未注入配置时保持原行为
	}
	if globalConfig.Fix.Enabled == nil {
		return true
	}
	return *globalConfig.Fix.Enabled
}

func getFixHeartbeatTimeout() time.Duration {
	if globalConfig == nil || globalConfig.Fix.HeartbeatTimeoutSec <= 0 {
		return 120 * time.Second
	}
	return time.Duration(globalConfig.Fix.HeartbeatTimeoutSec) * time.Second
}

func setFixSessionBotBinding(sessionID, botID string) {
	fixSessionBindingMutex.Lock()
	defer fixSessionBindingMutex.Unlock()
	if botID == "" {
		delete(fixSessionBotBinding, sessionID)
		return
	}
	fixSessionBotBinding[sessionID] = botID
}

func getFixSessionBotBinding(sessionID string) string {
	fixSessionBindingMutex.RLock()
	defer fixSessionBindingMutex.RUnlock()
	return fixSessionBotBinding[sessionID]
}

// fixEnabledMiddleware 当 config.fix.enabled=false 时返回 503
func fixEnabledMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isFixEnabled() {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "FIX protocol is disabled (config.fix.enabled=false)"})
			c.Abort()
			return
		}
		c.Next()
	}
}

// fixLogonSession FIX 登录与会话绑定
// POST /api/fix/sessions/logon
func fixLogonSession(c *gin.Context) {
	var req fixLogonRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_request", err)
		return
	}
	if botManagerProvider == nil {
		respondError(c, http.StatusServiceUnavailable, "error.not_supported", fmt.Errorf("bot manager not available"))
		return
	}
	if _, ok := botManagerProvider.GetBot(req.BotID); !ok {
		respondError(c, http.StatusBadRequest, "error.invalid_bot_id")
		return
	}

	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		storageProv = storageServiceProvider
	}
	if storageProv == nil || storageProv.GetStorage() == nil {
		respondError(c, http.StatusServiceUnavailable, "error.storage_not_found", fmt.Errorf("storage unavailable"))
		return
	}
	st := storageProv.GetStorage()

	now := utils.NowUTC()
	current, err := st.GetFixSessionState(req.SessionID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "error.query_failed", err)
		return
	}
	nextSenderSeq := int64(1)
	nextTargetSeq := int64(1)
	if current != nil {
		nextSenderSeq = current.NextSenderSeq
		nextTargetSeq = current.NextTargetSeq
	}
	if req.ResetSeqNumFlg {
		nextSenderSeq = 1
		nextTargetSeq = 1
	}
	role := req.Role
	if role == "" {
		role = "acceptor"
	}
	beginString := req.BeginString
	if beginString == "" {
		beginString = "FIX.4.4"
	}
	state := &storage.FixSessionState{
		SessionID:       req.SessionID,
		BotID:           req.BotID,
		Role:            role,
		BeginString:     beginString,
		SenderCompID:    req.SenderCompID,
		TargetCompID:    req.TargetCompID,
		NextSenderSeq:   nextSenderSeq,
		NextTargetSeq:   nextTargetSeq,
		IsLoggedOn:      true,
		LastLogonAt:     &now,
		LastHeartbeatAt: &now,
		UpdatedAt:       now,
	}
	if err := st.UpsertFixSessionState(state); err != nil {
		respondError(c, http.StatusInternalServerError, "error.save_failed", err)
		return
	}
	setFixSessionBotBinding(req.SessionID, req.BotID) // 内存兜底，进程内立即可用
	logger.Info("FIX logon: session_id=%s bot_id=%s reset_seq=%v", req.SessionID, req.BotID, req.ResetSeqNumFlg)
	metrics.GetPrometheusMetrics().RecordFixSessionLogon(req.SessionID, req.BotID)
	c.JSON(http.StatusOK, gin.H{
		"ok":                true,
		"session_id":        state.SessionID,
		"bot_id":            req.BotID,
		"next_sender_seq":   state.NextSenderSeq,
		"next_target_seq":   state.NextTargetSeq,
		"is_logged_on":      true,
		"last_logon_at":     utils.ToUTC8(now),
		"last_heartbeat_at": utils.ToUTC8(now),
	})
}

// fixHeartbeat FIX 心跳
// POST /api/fix/sessions/heartbeat
func fixHeartbeat(c *gin.Context) {
	var req fixHeartbeatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_request", err)
		return
	}
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		storageProv = storageServiceProvider
	}
	if storageProv == nil || storageProv.GetStorage() == nil {
		respondError(c, http.StatusServiceUnavailable, "error.storage_not_found", fmt.Errorf("storage unavailable"))
		return
	}
	st := storageProv.GetStorage()
	state, err := st.GetFixSessionState(req.SessionID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "error.query_failed", err)
		return
	}
	if state == nil || !state.IsLoggedOn {
		respondError(c, http.StatusBadRequest, "error.invalid_session")
		return
	}
	now := utils.NowUTC()
	state.LastHeartbeatAt = &now
	state.UpdatedAt = now
	if err := st.UpsertFixSessionState(state); err != nil {
		respondError(c, http.StatusInternalServerError, "error.save_failed", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "session_id": req.SessionID, "last_heartbeat_at": utils.ToUTC8(now)})
}

// fixLogoutSession FIX 主动登出
// POST /api/fix/sessions/logout
func fixLogoutSession(c *gin.Context) {
	var req fixHeartbeatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_request", err)
		return
	}
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		storageProv = storageServiceProvider
	}
	if storageProv == nil || storageProv.GetStorage() == nil {
		respondError(c, http.StatusServiceUnavailable, "error.storage_not_found", fmt.Errorf("storage unavailable"))
		return
	}
	st := storageProv.GetStorage()
	state, err := st.GetFixSessionState(req.SessionID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "error.query_failed", err)
		return
	}
	if state == nil {
		c.JSON(http.StatusOK, gin.H{"ok": true, "session_id": req.SessionID, "message": "session not found or already logged out"})
		return
	}
	state.IsLoggedOn = false
	state.UpdatedAt = utils.NowUTC()
	if err := st.UpsertFixSessionState(state); err != nil {
		respondError(c, http.StatusInternalServerError, "error.save_failed", err)
		return
	}
	setFixSessionBotBinding(req.SessionID, "")
	logger.Info("FIX logout: session_id=%s", req.SessionID)
	c.JSON(http.StatusOK, gin.H{"ok": true, "session_id": req.SessionID})
}

// fixNewOrder FIX 新单
// POST /api/fix/orders/new
func fixNewOrder(c *gin.Context) {
	var req fixNewOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_request", err)
		return
	}
	st, state, botDetail, ex, symbol, err := resolveFixExecutionContext(c, req.SessionID)
	if err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_session", err)
		return
	}
	if req.Symbol != "" && !strings.EqualFold(req.Symbol, symbol) {
		respondError(c, http.StatusBadRequest, "error.invalid_request", fmt.Errorf("symbol mismatch with bound bot"))
		return
	}
	side := strings.ToUpper(strings.TrimSpace(req.Side))
	if side != "BUY" && side != "SELL" {
		respondError(c, http.StatusBadRequest, "error.invalid_request", fmt.Errorf("side must be BUY or SELL"))
		return
	}
	ordType := strings.ToUpper(strings.TrimSpace(req.OrdType))
	if ordType == "" {
		ordType = "LIMIT"
	}
	if ordType != "LIMIT" && ordType != "MARKET" {
		respondError(c, http.StatusBadRequest, "error.invalid_request", fmt.Errorf("ord_type must be LIMIT or MARKET"))
		return
	}

	orderReq := &exchange.OrderRequest{
		Symbol:        symbol,
		Side:          exchange.Side(side),
		Type:          exchange.OrderType(ordType),
		TimeInForce:   exchange.TimeInForceGTC,
		Quantity:      req.OrderQty,
		Price:         req.Price,
		ReduceOnly:    req.ReduceOnly,
		PostOnly:      req.PostOnly,
		ClientOrderID: req.ClOrdID,
		StrategyName:  req.StrategyName,
		StrategyType:  req.StrategyType,
	}
	if ordType == "MARKET" {
		orderReq.Price = 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	exOrder, placeErr := ex.PlaceOrder(ctx, orderReq)
	now := utils.NowUTC()
	reportStatus := "REJECTED"
	execType := "8"
	var internalOrderID int64
	var cumQty, leavesQty, avgPx float64
	reportText := ""
	if placeErr == nil && exOrder != nil {
		reportStatus = mapExchangeOrderStatus(string(exOrder.Status))
		if reportStatus == "NEW" {
			execType = "0"
		}
		internalOrderID = exOrder.OrderID
		cumQty = exOrder.ExecutedQty
		if exOrder.Quantity > exOrder.ExecutedQty {
			leavesQty = exOrder.Quantity - exOrder.ExecutedQty
		}
		avgPx = exOrder.AvgPrice
	} else if placeErr != nil {
		reportText = placeErr.Error()
	}

	_ = st.UpsertFixOrderLink(&storage.FixOrderLink{
		SessionID:       req.SessionID,
		ClOrdID:         req.ClOrdID,
		BotID:           botDetail.BotID,
		Exchange:        botDetail.Exchange,
		Symbol:          symbol,
		Side:            side,
		InternalOrderID: internalOrderID,
		LastExecID:      fmt.Sprintf("exec-%d", now.UnixNano()),
		OrdStatus:       reportStatus,
		CumQty:          cumQty,
		LeavesQty:       leavesQty,
		AvgPx:           avgPx,
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	bumpFixSessionSeq(state, now)
	_ = st.UpsertFixSessionState(state)

	statusCode := http.StatusOK
	orderStatus := "ok"
	if placeErr != nil {
		statusCode = http.StatusBadRequest
		orderStatus = "reject"
	}
	metrics.GetPrometheusMetrics().RecordFixOrder(req.SessionID, "new", orderStatus)
	c.JSON(statusCode, gin.H{
		"session_id":      req.SessionID,
		"cl_ord_id":       req.ClOrdID,
		"exec_type":       execType,
		"ord_status":      reportStatus,
		"order_id":        internalOrderID,
		"cum_qty":         cumQty,
		"leaves_qty":      leavesQty,
		"avg_px":          avgPx,
		"text":            reportText,
		"next_sender_seq": state.NextSenderSeq,
		"next_target_seq": state.NextTargetSeq,
	})
}

// fixCancelOrder FIX 撤单
// POST /api/fix/orders/cancel
func fixCancelOrder(c *gin.Context) {
	var req fixCancelOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_request", err)
		return
	}
	st, state, botDetail, ex, symbol, err := resolveFixExecutionContext(c, req.SessionID)
	if err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_session", err)
		return
	}
	orig, err := st.GetFixOrderLinkByClOrdID(req.SessionID, req.OrigClOrdID)
	if err != nil || orig == nil {
		respondError(c, http.StatusBadRequest, "error.invalid_request", fmt.Errorf("orig_cl_ord_id not found"))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	cancelErr := ex.CancelOrder(ctx, symbol, orig.InternalOrderID)
	now := utils.NowUTC()
	status := "CANCELED"
	execType := "4"
	text := ""
	if cancelErr != nil {
		status = "REJECTED"
		execType = "8"
		text = cancelErr.Error()
	}
	_ = st.UpsertFixOrderLink(&storage.FixOrderLink{
		SessionID:       req.SessionID,
		ClOrdID:         req.ClOrdID,
		OrigClOrdID:     req.OrigClOrdID,
		BotID:           botDetail.BotID,
		Exchange:        botDetail.Exchange,
		Symbol:          symbol,
		Side:            orig.Side,
		InternalOrderID: orig.InternalOrderID,
		LastExecID:      fmt.Sprintf("exec-%d", now.UnixNano()),
		OrdStatus:       status,
		CumQty:          orig.CumQty,
		LeavesQty:       0,
		AvgPx:           orig.AvgPx,
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	bumpFixSessionSeq(state, now)
	_ = st.UpsertFixSessionState(state)
	code := http.StatusOK
	cancelStatus := "ok"
	if cancelErr != nil {
		code = http.StatusBadRequest
		cancelStatus = "reject"
	}
	metrics.GetPrometheusMetrics().RecordFixOrder(req.SessionID, "cancel", cancelStatus)
	c.JSON(code, gin.H{
		"session_id":      req.SessionID,
		"cl_ord_id":       req.ClOrdID,
		"orig_cl_ord_id":  req.OrigClOrdID,
		"exec_type":       execType,
		"ord_status":      status,
		"order_id":        orig.InternalOrderID,
		"text":            text,
		"next_sender_seq": state.NextSenderSeq,
		"next_target_seq": state.NextTargetSeq,
	})
}

// fixReplaceOrder FIX 改单（撤旧挂新）
// POST /api/fix/orders/replace
func fixReplaceOrder(c *gin.Context) {
	var req fixReplaceOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_request", err)
		return
	}
	st, state, botDetail, ex, symbol, err := resolveFixExecutionContext(c, req.SessionID)
	if err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_session", err)
		return
	}
	orig, err := st.GetFixOrderLinkByClOrdID(req.SessionID, req.OrigClOrdID)
	if err != nil || orig == nil {
		respondError(c, http.StatusBadRequest, "error.invalid_request", fmt.Errorf("orig_cl_ord_id not found"))
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = ex.CancelOrder(ctx, symbol, orig.InternalOrderID)
	orderReq := &exchange.OrderRequest{
		Symbol:        symbol,
		Side:          exchange.Side(strings.ToUpper(orig.Side)),
		Type:          exchange.OrderTypeLimit,
		TimeInForce:   exchange.TimeInForceGTC,
		Quantity:      req.OrderQty,
		Price:         req.Price,
		ClientOrderID: req.ClOrdID,
	}
	newOrder, placeErr := ex.PlaceOrder(ctx, orderReq)
	now := utils.NowUTC()
	status := "REPLACED"
	execType := "5"
	var orderID int64
	text := ""
	if placeErr != nil || newOrder == nil {
		status = "REJECTED"
		execType = "8"
		if placeErr != nil {
			text = placeErr.Error()
		}
	} else {
		orderID = newOrder.OrderID
	}
	_ = st.UpsertFixOrderLink(&storage.FixOrderLink{
		SessionID:       req.SessionID,
		ClOrdID:         req.ClOrdID,
		OrigClOrdID:     req.OrigClOrdID,
		BotID:           botDetail.BotID,
		Exchange:        botDetail.Exchange,
		Symbol:          symbol,
		Side:            orig.Side,
		InternalOrderID: orderID,
		LastExecID:      fmt.Sprintf("exec-%d", now.UnixNano()),
		OrdStatus:       status,
		CumQty:          0,
		LeavesQty:       req.OrderQty,
		AvgPx:           0,
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	bumpFixSessionSeq(state, now)
	_ = st.UpsertFixSessionState(state)
	code := http.StatusOK
	replaceStatus := "ok"
	if placeErr != nil {
		code = http.StatusBadRequest
		replaceStatus = "reject"
	}
	metrics.GetPrometheusMetrics().RecordFixOrder(req.SessionID, "replace", replaceStatus)
	c.JSON(code, gin.H{
		"session_id":      req.SessionID,
		"cl_ord_id":       req.ClOrdID,
		"orig_cl_ord_id":  req.OrigClOrdID,
		"exec_type":       execType,
		"ord_status":      status,
		"order_id":        orderID,
		"text":            text,
		"next_sender_seq": state.NextSenderSeq,
		"next_target_seq": state.NextTargetSeq,
	})
}

func resolveFixExecutionContext(c *gin.Context, sessionID string) (storage.Storage, *storage.FixSessionState, *BotDetailResponse, exchange.IExchange, string, error) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		storageProv = storageServiceProvider
	}
	if storageProv == nil || storageProv.GetStorage() == nil {
		return nil, nil, nil, nil, "", fmt.Errorf("storage unavailable")
	}
	st := storageProv.GetStorage()
	state, err := st.GetFixSessionState(sessionID)
	if err != nil {
		return nil, nil, nil, nil, "", err
	}
	if state == nil || !state.IsLoggedOn {
		return nil, nil, nil, nil, "", fmt.Errorf("session not logged on")
	}
	// 心跳超时判定：超过阈值则标记失活并拒单
	if state.LastHeartbeatAt != nil && time.Since(*state.LastHeartbeatAt) > getFixHeartbeatTimeout() {
		state.IsLoggedOn = false
		state.UpdatedAt = utils.NowUTC()
		_ = st.UpsertFixSessionState(state)
		logger.Warn("FIX session timeout: session_id=%s last_heartbeat=%v", sessionID, utils.ToUTC8(*state.LastHeartbeatAt))
		metrics.GetPrometheusMetrics().RecordFixSessionTimeout(sessionID)
		return nil, nil, nil, nil, "", fmt.Errorf("session heartbeat timeout; please logon again")
	}
	botID := state.BotID
	if botID == "" {
		botID = getFixSessionBotBinding(sessionID)
	}
	if botID == "" {
		return nil, nil, nil, nil, "", fmt.Errorf("session bot binding missing; please logon again")
	}
	if botManagerProvider == nil {
		return nil, nil, nil, nil, "", fmt.Errorf("bot manager unavailable")
	}
	botDetail, ok := botManagerProvider.GetBot(botID)
	if !ok || botDetail == nil {
		return nil, nil, nil, nil, "", fmt.Errorf("bound bot not found")
	}
	if symbolManagerProvider == nil {
		return nil, nil, nil, nil, "", fmt.Errorf("symbol manager unavailable")
	}
	rt, found := symbolManagerProvider.GetEx(botDetail.Exchange, botDetail.Symbol, botDetail.MarketType)
	if !found {
		return nil, nil, nil, nil, "", fmt.Errorf("bot runtime not running")
	}
	ex, err := extractExchangeFromRuntime(rt)
	if err != nil {
		return nil, nil, nil, nil, "", err
	}
	return st, state, botDetail, ex, botDetail.Symbol, nil
}

func extractExchangeFromRuntime(rt interface{}) (exchange.IExchange, error) {
	v := reflect.ValueOf(rt)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	field := v.FieldByName("Exchange")
	if !field.IsValid() || field.IsNil() {
		return nil, fmt.Errorf("runtime exchange not available")
	}
	ex, ok := field.Interface().(exchange.IExchange)
	if !ok {
		return nil, fmt.Errorf("runtime exchange type assertion failed")
	}
	return ex, nil
}

func mapExchangeOrderStatus(status string) string {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "NEW":
		return "NEW"
	case "PARTIALLY_FILLED":
		return "PARTIALLY_FILLED"
	case "FILLED":
		return "FILLED"
	case "CANCELED", "CANCELLED":
		return "CANCELED"
	case "EXPIRED":
		return "EXPIRED"
	default:
		return "REJECTED"
	}
}

func bumpFixSessionSeq(state *storage.FixSessionState, now time.Time) {
	if state == nil {
		return
	}
	if state.NextSenderSeq <= 0 {
		state.NextSenderSeq = 1
	}
	if state.NextTargetSeq <= 0 {
		state.NextTargetSeq = 1
	}
	state.NextSenderSeq++
	state.NextTargetSeq++
	state.UpdatedAt = now
}
