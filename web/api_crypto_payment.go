package web

import (
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"quantmesh/saas"
)

var (
	cryptoPaymentService *saas.CryptoPaymentService
)

// SetCryptoPaymentService 設置加密貨幣支付服務
func SetCryptoPaymentService(cps *saas.CryptoPaymentService) {
	cryptoPaymentService = cps
}

func cryptoPaymentUnavailable(c *gin.Context) bool {
	if cryptoPaymentService != nil {
		return false
	}
	c.JSON(http.StatusServiceUnavailable, gin.H{"error": "加密貨幣支付服務未配置"})
	return true
}

func cryptoPaymentUserID(c *gin.Context) string {
	if c.GetBool("local_dev_mode") {
		return ""
	}
	if userID := c.GetString("user_id"); userID != "" {
		return userID
	}
	return c.GetString("username")
}

// createCoinbasePaymentHandler 創建 Coinbase Commerce 支付
// POST /api/payment/crypto/coinbase/create
func createCoinbasePaymentHandler(c *gin.Context) {
	if cryptoPaymentUnavailable(c) {
		return
	}
	var req struct {
		Plan  string `json:"plan" binding:"required"`
		Email string `json:"email" binding:"required"`
	}

	if err := c.BindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "無效的请求参數"})
		return
	}

	userID := cryptoPaymentUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "需要有效的用戶身份"})
		return
	}

	// 獲取套餐價格
	prices := map[string]float64{
		"starter":      49.00,
		"professional": 199.00,
		"enterprise":   999.00,
	}

	amount, exists := prices[req.Plan]
	if !exists {
		c.JSON(400, gin.H{"error": "無效的套餐"})
		return
	}

	// 創建 Coinbase Charge
	payment, err := cryptoPaymentService.CreateCoinbaseCharge(userID, req.Email, req.Plan, amount)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{
		"payment_id":  payment.ID,
		"charge_id":   payment.ChargeID,
		"payment_url": payment.PaymentAddress,
		"amount":      payment.Amount,
		"currency":    payment.Currency,
		"expires_at":  payment.ExpiresAt,
		"status":      payment.Status,
		"message":     "请在支付页面完成付款",
	})
}

// createDirectPaymentHandler 創建直接钱包支付
// POST /api/payment/crypto/direct/create
func createDirectPaymentHandler(c *gin.Context) {
	if cryptoPaymentUnavailable(c) {
		return
	}
	var req struct {
		Plan           string `json:"plan" binding:"required"`
		Email          string `json:"email" binding:"required"`
		CryptoCurrency string `json:"crypto_currency" binding:"required"` // BTC, ETH, USDT, USDC
	}

	if err := c.BindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "無效的请求参數"})
		return
	}

	userID := cryptoPaymentUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "需要有效的用戶身份"})
		return
	}

	// 獲取套餐價格
	prices := map[string]float64{
		"starter":      49.00,
		"professional": 199.00,
		"enterprise":   999.00,
	}

	amount, exists := prices[req.Plan]
	if !exists {
		c.JSON(400, gin.H{"error": "無效的套餐"})
		return
	}

	// 創建直接支付
	payment, err := cryptoPaymentService.CreateDirectPayment(
		userID, req.Email, req.Plan, req.CryptoCurrency, amount,
	)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{
		"payment_id":      payment.ID,
		"crypto_currency": payment.CryptoCurrency,
		"crypto_amount":   payment.CryptoAmount,
		"payment_address": payment.PaymentAddress,
		"amount_usd":      payment.Amount,
		"expires_at":      payment.ExpiresAt,
		"status":          payment.Status,
		"message":         "请向指定地址轉账,並保存交易哈希",
		"instructions": map[string]string{
			"step1": "複制支付地址",
			"step2": "使用钱包轉账指定金額",
			"step3": "提交交易哈希等待确认",
		},
	})
}

// getPaymentStatusHandler 獲取支付状態
// GET /api/payment/crypto/:id
func getPaymentStatusHandler(c *gin.Context) {
	if cryptoPaymentUnavailable(c) {
		return
	}
	paymentID := c.Param("id")

	var id int
	if _, err := fmt.Sscanf(paymentID, "%d", &id); err != nil {
		c.JSON(400, gin.H{"error": "無效的支付ID"})
		return
	}

	payment, err := cryptoPaymentService.GetPayment(id)
	if err != nil {
		c.JSON(404, gin.H{"error": "支付記錄不存在"})
		return
	}

	// 驗证权限
	userID := cryptoPaymentUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "需要有效的用戶身份"})
		return
	}
	if payment.UserID != userID {
		c.JSON(403, gin.H{"error": "無权访问"})
		return
	}

	c.JSON(200, gin.H{
		"payment": payment,
	})
}

// listUserPaymentsHandler 列出用戶的所有支付
// GET /api/payment/crypto/list
func listUserPaymentsHandler(c *gin.Context) {
	if cryptoPaymentUnavailable(c) {
		return
	}
	userID := cryptoPaymentUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "需要有效的用戶身份"})
		return
	}

	payments, err := cryptoPaymentService.ListUserPayments(userID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{
		"payments": payments,
		"total":    len(payments),
	})
}

// submitTransactionHashHandler 提交交易哈希 (直接支付)
// POST /api/payment/crypto/:id/submit-tx
func submitTransactionHashHandler(c *gin.Context) {
	if cryptoPaymentUnavailable(c) {
		return
	}
	paymentID := c.Param("id")

	var id int
	if _, err := fmt.Sscanf(paymentID, "%d", &id); err != nil {
		c.JSON(400, gin.H{"error": "無效的支付ID"})
		return
	}

	var req struct {
		TransactionHash string `json:"transaction_hash" binding:"required"`
	}

	if err := c.BindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "無效的请求参數"})
		return
	}

	userID := cryptoPaymentUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "需要有效的用戶身份"})
		return
	}
	if err := cryptoPaymentService.SubmitDirectTransactionHash(id, userID, req.TransactionHash); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{
		"message":          "交易哈希已提交,等待管理员确认",
		"payment_id":       id,
		"transaction_hash": req.TransactionHash,
		"status":           "pending_confirmation",
	})
}

// confirmDirectPaymentHandler 确认直接支付 (管理员)
// POST /api/payment/crypto/:id/confirm
func confirmDirectPaymentHandler(c *gin.Context) {
	if cryptoPaymentUnavailable(c) {
		return
	}
	paymentID := c.Param("id")

	var id int
	if _, err := fmt.Sscanf(paymentID, "%d", &id); err != nil {
		c.JSON(400, gin.H{"error": "無效的支付ID"})
		return
	}

	var req struct {
		TransactionHash string `json:"transaction_hash" binding:"required"`
	}

	if err := c.BindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "無效的请求参數"})
		return
	}

	sessionValue, ok := c.Get("session")
	session, isSession := sessionValue.(*Session)
	if !ok || !isSession || session.Role != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "需要管理員權限"})
		return
	}

	// 确认支付
	if err := cryptoPaymentService.ConfirmDirectPayment(id, req.TransactionHash); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{
		"message":    "支付已确认",
		"payment_id": id,
	})
}

// coinbaseWebhookHandler Coinbase Commerce Webhook
// POST /api/payment/crypto/webhook/coinbase
func coinbaseWebhookHandler(c *gin.Context) {
	if cryptoPaymentUnavailable(c) {
		return
	}
	// 读取 webhook 數據
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(400, gin.H{"error": "無法读取请求体"})
		return
	}

	// 獲取签名
	signature := c.GetHeader("X-CC-Webhook-Signature")

	// 处理 webhook
	if err := cryptoPaymentService.HandleCoinbaseWebhook(body, signature); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"received": true})
}

// getSupportedCryptoCurrenciesHandler 獲取支援的加密貨幣
// GET /api/payment/crypto/currencies
func getSupportedCryptoCurrenciesHandler(c *gin.Context) {
	currencies := []map[string]interface{}{
		{
			"symbol":      "BTC",
			"name":        "Bitcoin",
			"network":     "Bitcoin",
			"decimals":    8,
			"min_amount":  0.0001,
			"recommended": true,
		},
		{
			"symbol":      "ETH",
			"name":        "Ethereum",
			"network":     "Ethereum",
			"decimals":    18,
			"min_amount":  0.001,
			"recommended": true,
		},
		{
			"symbol":      "USDT",
			"name":        "Tether",
			"network":     "Ethereum (ERC20)",
			"decimals":    6,
			"min_amount":  10.0,
			"recommended": true,
		},
		{
			"symbol":      "USDC",
			"name":        "USD Coin",
			"network":     "Ethereum (ERC20)",
			"decimals":    6,
			"min_amount":  10.0,
			"recommended": true,
		},
	}

	c.JSON(200, gin.H{
		"currencies": currencies,
	})
}
