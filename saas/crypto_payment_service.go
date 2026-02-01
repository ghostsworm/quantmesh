package saas

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"quantmesh/logger"
)

// CryptoPaymentService 加密货币支付服务
type CryptoPaymentService struct {
	db                    *sql.DB
	coinbaseAPIKey        string
	coinbaseWebhookSecret string
	httpClient            *http.Client

	// 直接钱包地址 (备选方案)
	walletAddresses map[string]string
}

// PaymentMethod 支付方式
type PaymentMethod string

const (
	PaymentMethodCoinbase PaymentMethod = "coinbase"
	PaymentMethodDirect   PaymentMethod = "direct"
)

// CryptoPayment 加密货币支付记录
type CryptoPayment struct {
	ID              int        `json:"id"`
	UserID          string     `json:"user_id"`
	Email           string     `json:"email"`
	Plan            string     `json:"plan"`
	Amount          float64    `json:"amount"`
	Currency        string     `json:"currency"`        // USD
	CryptoCurrency  string     `json:"crypto_currency"` // BTC, ETH, USDT
	CryptoAmount    float64    `json:"crypto_amount"`
	PaymentMethod   string     `json:"payment_method"`
	Status          string     `json:"status"`    // pending/completed/expired/cancelled
	ChargeID        string     `json:"charge_id"` // Coinbase Charge ID
	PaymentAddress  string     `json:"payment_address"`
	TransactionHash string     `json:"transaction_hash"`
	ExpiresAt       time.Time  `json:"expires_at"`
	CompletedAt     *time.Time `json:"completed_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// NewCryptoPaymentService 创建加密货币支付服务
func NewCryptoPaymentService(db *sql.DB, coinbaseAPIKey string) *CryptoPaymentService {
	return &CryptoPaymentService{
		db:             db,
		coinbaseAPIKey: coinbaseAPIKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		walletAddresses: map[string]string{
			"BTC":  "bc1q...", // 你的 BTC 地址
			"ETH":  "0x...",   // 你的 ETH 地址
			"USDT": "0x...",   // 你的 USDT (ERC20) 地址
			"USDC": "0x...",   // 你的 USDC 地址
		},
	}
}

// CreateCoinbaseCharge 创建 Coinbase Commerce 支付
func (s *CryptoPaymentService) CreateCoinbaseCharge(userID, email, plan string, amount float64) (*CryptoPayment, error) {
	// 1. 创建 Coinbase Charge
	chargeData := map[string]interface{}{
		"name":         fmt.Sprintf("QuantMesh %s Plan", plan),
		"description":  fmt.Sprintf("QuantMesh %s subscription for %s", plan, email),
		"pricing_type": "fixed_price",
		"local_price": map[string]interface{}{
			"amount":   fmt.Sprintf("%.2f", amount),
			"currency": "USD",
		},
		"metadata": map[string]string{
			"user_id": userID,
			"email":   email,
			"plan":    plan,
		},
		"redirect_url": "https://quantmesh.cloud/payment/success",
		"cancel_url":   "https://quantmesh.cloud/payment/cancel",
	}

	jsonData, _ := json.Marshal(chargeData)

	req, err := http.NewRequest("POST", "https://api.commerce.coinbase.com/charges", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CC-Api-Key", s.coinbaseAPIKey)
	req.Header.Set("X-CC-Version", "2018-03-22")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("创建 Coinbase Charge 失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Coinbase API 错误 (HTTP %d): %s", resp.StatusCode, body)
	}

	var result struct {
		Data struct {
			ID        string            `json:"id"`
			Code      string            `json:"code"`
			HostedURL string            `json:"hosted_url"`
			ExpiresAt string            `json:"expires_at"`
			Addresses map[string]string `json:"addresses"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	// 2. 保存到数据库
	expiresAt, _ := time.Parse(time.RFC3339, result.Data.ExpiresAt)

	payment := &CryptoPayment{
		UserID:         userID,
		Email:          email,
		Plan:           plan,
		Amount:         amount,
		Currency:       "USD",
		PaymentMethod:  string(PaymentMethodCoinbase),
		Status:         "pending",
		ChargeID:       result.Data.ID,
		PaymentAddress: result.Data.HostedURL,
		ExpiresAt:      expiresAt,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	err = s.db.QueryRow(`
		INSERT INTO crypto_payments (
			user_id, email, plan, amount, currency, payment_method,
			status, charge_id, payment_address, expires_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id
	`, payment.UserID, payment.Email, payment.Plan, payment.Amount, payment.Currency,
		payment.PaymentMethod, payment.Status, payment.ChargeID, payment.PaymentAddress,
		payment.ExpiresAt, payment.CreatedAt, payment.UpdatedAt,
	).Scan(&payment.ID)

	if err != nil {
		return nil, fmt.Errorf("保存支付记录失败: %v", err)
	}

	logger.Info("✅ Coinbase Charge 创建成功: %s (用户: %s)", result.Data.ID, userID)

	return payment, nil
}

// CreateDirectPayment 创建直接钱包支付
func (s *CryptoPaymentService) CreateDirectPayment(userID, email, plan, cryptoCurrency string, amount float64) (*CryptoPayment, error) {
	// 获取钱包地址
	walletAddress, exists := s.walletAddresses[cryptoCurrency]
	if !exists {
		return nil, fmt.Errorf("不支持的加密货币: %s", cryptoCurrency)
	}

	// 计算加密货币金额 (这里简化处理,实际应该调用汇率 API)
	cryptoAmount := s.calculateCryptoAmount(amount, cryptoCurrency)

	payment := &CryptoPayment{
		UserID:         userID,
		Email:          email,
		Plan:           plan,
		Amount:         amount,
		Currency:       "USD",
		CryptoCurrency: cryptoCurrency,
		CryptoAmount:   cryptoAmount,
		PaymentMethod:  string(PaymentMethodDirect),
		Status:         "pending",
		PaymentAddress: walletAddress,
		ExpiresAt:      time.Now().Add(24 * time.Hour),
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	err := s.db.QueryRow(`
		INSERT INTO crypto_payments (
			user_id, email, plan, amount, currency, crypto_currency, crypto_amount,
			payment_method, status, payment_address, expires_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING id
	`, payment.UserID, payment.Email, payment.Plan, payment.Amount, payment.Currency,
		payment.CryptoCurrency, payment.CryptoAmount, payment.PaymentMethod, payment.Status,
		payment.PaymentAddress, payment.ExpiresAt, payment.CreatedAt, payment.UpdatedAt,
	).Scan(&payment.ID)

	if err != nil {
		return nil, fmt.Errorf("保存支付记录失败: %v", err)
	}

	logger.Info("✅ 直接支付创建成功: ID=%d (用户: %s, 币种: %s)", payment.ID, userID, cryptoCurrency)

	return payment, nil
}

// HandleCoinbaseWebhook 处理 Coinbase Webhook
func (s *CryptoPaymentService) HandleCoinbaseWebhook(webhookData []byte, signature string) error {
	// 1. 验证签名 (生产环境必须验证)
	// if !s.verifyCoinbaseSignature(webhookData, signature) {
	//     return errors.New("无效的 webhook 签名")
	// }

	// 2. 解析 webhook 数据
	var webhook struct {
		Event struct {
			Type string `json:"type"`
			Data struct {
				ID       string `json:"id"`
				Code     string `json:"code"`
				Metadata struct {
					UserID string `json:"user_id"`
					Email  string `json:"email"`
					Plan   string `json:"plan"`
				} `json:"metadata"`
			} `json:"data"`
		} `json:"event"`
	}

	if err := json.Unmarshal(webhookData, &webhook); err != nil {
		return err
	}

	// 3. 处理不同类型的事件
	switch webhook.Event.Type {
	case "charge:confirmed":
		// 支付确认
		return s.completePayment(webhook.Event.Data.ID)

	case "charge:failed":
		// 支付失败
		return s.failPayment(webhook.Event.Data.ID)

	case "charge:delayed":
		// 支付延迟 (区块确认中)
		logger.Info("⏳ 支付延迟: %s", webhook.Event.Data.ID)

	case "charge:pending":
		// 支付待处理
		logger.Info("⏳ 支付待处理: %s", webhook.Event.Data.ID)
	}

	return nil
}

// completePayment 完成支付
func (s *CryptoPaymentService) completePayment(chargeID string) error {
	now := time.Now()

	_, err := s.db.Exec(`
		UPDATE crypto_payments
		SET status = 'completed', completed_at = $1, updated_at = $2
		WHERE charge_id = $3
	`, now, now, chargeID)

	if err != nil {
		return err
	}

	// 获取支付信息
	var payment CryptoPayment
	err = s.db.QueryRow(`
		SELECT user_id, email, plan
		FROM crypto_payments
		WHERE charge_id = $1
	`, chargeID).Scan(&payment.UserID, &payment.Email, &payment.Plan)

	if err != nil {
		return err
	}

	logger.Info("✅ 支付完成: ChargeID=%s, 用户=%s, 套餐=%s", chargeID, payment.UserID, payment.Plan)

	// TODO: 激活订阅
	// billingService.CreateSubscription(payment.UserID, payment.Email, payment.Plan)

	return nil
}

// failPayment 支付失败
func (s *CryptoPaymentService) failPayment(chargeID string) error {
	_, err := s.db.Exec(`
		UPDATE crypto_payments
		SET status = 'failed', updated_at = $1
		WHERE charge_id = $2
	`, time.Now(), chargeID)

	logger.Warn("❌ 支付失败: ChargeID=%s", chargeID)
	return err
}

// ConfirmDirectPayment 确认直接支付 (管理员手动确认)
func (s *CryptoPaymentService) ConfirmDirectPayment(paymentID int, transactionHash string) error {
	now := time.Now()

	_, err := s.db.Exec(`
		UPDATE crypto_payments
		SET status = 'completed', transaction_hash = $1, completed_at = $2, updated_at = $3
		WHERE id = $4
	`, transactionHash, now, now, paymentID)

	if err != nil {
		return err
	}

	// 获取支付信息并激活订阅
	var payment CryptoPayment
	err = s.db.QueryRow(`
		SELECT user_id, email, plan
		FROM crypto_payments
		WHERE id = $1
	`, paymentID).Scan(&payment.UserID, &payment.Email, &payment.Plan)

	if err != nil {
		return err
	}

	logger.Info("✅ 直接支付已确认: ID=%d, TxHash=%s", paymentID, transactionHash)

	// TODO: 激活订阅
	// billingService.CreateSubscription(payment.UserID, payment.Email, payment.Plan)

	return nil
}

// GetPayment 获取支付信息
func (s *CryptoPaymentService) GetPayment(paymentID int) (*CryptoPayment, error) {
	var payment CryptoPayment

	err := s.db.QueryRow(`
		SELECT id, user_id, email, plan, amount, currency, crypto_currency, crypto_amount,
		       payment_method, status, charge_id, payment_address, transaction_hash,
		       expires_at, completed_at, created_at, updated_at
		FROM crypto_payments
		WHERE id = $1
	`, paymentID).Scan(
		&payment.ID, &payment.UserID, &payment.Email, &payment.Plan,
		&payment.Amount, &payment.Currency, &payment.CryptoCurrency, &payment.CryptoAmount,
		&payment.PaymentMethod, &payment.Status, &payment.ChargeID, &payment.PaymentAddress,
		&payment.TransactionHash, &payment.ExpiresAt, &payment.CompletedAt,
		&payment.CreatedAt, &payment.UpdatedAt,
	)

	if err != nil {
		return nil, err
	}

	return &payment, nil
}

// ListUserPayments 列出用户的所有支付
func (s *CryptoPaymentService) ListUserPayments(userID string) ([]*CryptoPayment, error) {
	rows, err := s.db.Query(`
		SELECT id, user_id, email, plan, amount, currency, crypto_currency, crypto_amount,
		       payment_method, status, charge_id, payment_address, transaction_hash,
		       expires_at, completed_at, created_at, updated_at
		FROM crypto_payments
		WHERE user_id = $1
		ORDER BY created_at DESC
	`, userID)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	payments := []*CryptoPayment{}
	for rows.Next() {
		var payment CryptoPayment
		err := rows.Scan(
			&payment.ID, &payment.UserID, &payment.Email, &payment.Plan,
			&payment.Amount, &payment.Currency, &payment.CryptoCurrency, &payment.CryptoAmount,
			&payment.PaymentMethod, &payment.Status, &payment.ChargeID, &payment.PaymentAddress,
			&payment.TransactionHash, &payment.ExpiresAt, &payment.CompletedAt,
			&payment.CreatedAt, &payment.UpdatedAt,
		)
		if err != nil {
			continue
		}
		payments = append(payments, &payment)
	}

	return payments, nil
}

// calculateCryptoAmount 计算加密货币金额
func (s *CryptoPaymentService) calculateCryptoAmount(usdAmount float64, cryptoCurrency string) float64 {
	// 这里简化处理,实际应该调用汇率 API (如 CoinGecko, CoinMarketCap)
	// 示例汇率 (需要实时获取)
	rates := map[string]float64{
		"BTC":  100000.0, // 1 BTC = $100,000
		"ETH":  4000.0,   // 1 ETH = $4,000
		"USDT": 1.0,      // 1 USDT = $1
		"USDC": 1.0,      // 1 USDC = $1
	}

	rate, exists := rates[cryptoCurrency]
	if !exists {
		return 0
	}

	return usdAmount / rate
}

// InitDatabase 初始化数据库表
func (s *CryptoPaymentService) InitDatabase() error {
	schema := `
	CREATE TABLE IF NOT EXISTS crypto_payments (
		id SERIAL PRIMARY KEY,
		user_id VARCHAR(255) NOT NULL,
		email VARCHAR(255) NOT NULL,
		plan VARCHAR(50) NOT NULL,
		amount DECIMAL(10, 2) NOT NULL,
		currency VARCHAR(3) DEFAULT 'USD',
		crypto_currency VARCHAR(10),
		crypto_amount DECIMAL(20, 8),
		payment_method VARCHAR(20) NOT NULL,
		status VARCHAR(20) NOT NULL,
		charge_id VARCHAR(255),
		payment_address TEXT,
		transaction_hash VARCHAR(255),
		expires_at TIMESTAMP,
		completed_at TIMESTAMP,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		INDEX idx_user_id (user_id),
		INDEX idx_status (status),
		INDEX idx_charge_id (charge_id)
	);
	`

	_, err := s.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("初始化数据库失败: %v", err)
	}

	logger.Info("✅ 加密货币支付数据库表初始化成功")
	return nil
}
