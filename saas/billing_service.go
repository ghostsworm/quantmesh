package saas

import (
	"database/sql"
	"fmt"
	"time"
	
	"quantmesh/logger"
)

// BillingService 计费服务
type BillingService struct {
	db           *sql.DB
	stripeAPIKey string
}

// Subscription 订阅信息
type Subscription struct {
	ID                   int       `json:"id"`
	UserID               string    `json:"user_id"`
	Email                string    `json:"email"`
	Plan                 string    `json:"plan"`
	Status               string    `json:"status"` // active/cancelled/expired
	StripeSubscriptionID string    `json:"stripe_subscription_id"`
	StripeCustomerID     string    `json:"stripe_customer_id"`
	CurrentPeriodStart   time.Time `json:"current_period_start"`
	CurrentPeriodEnd     time.Time `json:"current_period_end"`
	CancelAtPeriodEnd    bool      `json:"cancel_at_period_end"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

// NewBillingService 创建计费服务
func NewBillingService(db *sql.DB, stripeAPIKey string) *BillingService {
	return &BillingService{
		db:           db,
		stripeAPIKey: stripeAPIKey,
	}
}

// CreateSubscription 创建订阅
func (s *BillingService) CreateSubscription(userID, email, plan string) (*Subscription, error) {
	// 1. 检查用户是否已有订阅
	existing, err := s.GetSubscription(userID)
	if err == nil && existing != nil {
		return nil, fmt.Errorf("用户已有订阅")
	}
	
	// 2. 创建 Stripe 客户 (这里简化,实际应该调用 Stripe API)
	stripeCustomerID := fmt.Sprintf("cus_%s_%d", userID, time.Now().Unix())
	
	// 3. 创建 Stripe 订阅 (这里简化,实际应该调用 Stripe API)
	stripeSubscriptionID := fmt.Sprintf("sub_%s_%d", userID, time.Now().Unix())
	
	// 4. 保存到数据库
	now := time.Now()
	periodEnd := now.AddDate(0, 1, 0) // 一个月后
	
	var subID int
	err = s.db.QueryRow(`
		INSERT INTO subscriptions (
			user_id, email, plan, status, 
			stripe_subscription_id, stripe_customer_id,
			current_period_start, current_period_end,
			cancel_at_period_end, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id
	`, userID, email, plan, "active",
		stripeSubscriptionID, stripeCustomerID,
		now, periodEnd, false, now, now,
	).Scan(&subID)
	
	if err != nil {
		return nil, fmt.Errorf("创建订阅失败: %v", err)
	}
	
	logger.Info("✅ 订阅创建成功: 用户=%s, 套餐=%s", userID, plan)
	
	return &Subscription{
		ID:                   subID,
		UserID:               userID,
		Email:                email,
		Plan:                 plan,
		Status:               "active",
		StripeSubscriptionID: stripeSubscriptionID,
		StripeCustomerID:     stripeCustomerID,
		CurrentPeriodStart:   now,
		CurrentPeriodEnd:     periodEnd,
		CancelAtPeriodEnd:    false,
		CreatedAt:            now,
		UpdatedAt:            now,
	}, nil
}

// GetSubscription 获取订阅
func (s *BillingService) GetSubscription(userID string) (*Subscription, error) {
	var sub Subscription
	
	err := s.db.QueryRow(`
		SELECT id, user_id, email, plan, status,
		       stripe_subscription_id, stripe_customer_id,
		       current_period_start, current_period_end,
		       cancel_at_period_end, created_at, updated_at
		FROM subscriptions
		WHERE user_id = $1 AND status = 'active'
		ORDER BY created_at DESC
		LIMIT 1
	`, userID).Scan(
		&sub.ID,
		&sub.UserID,
		&sub.Email,
		&sub.Plan,
		&sub.Status,
		&sub.StripeSubscriptionID,
		&sub.StripeCustomerID,
		&sub.CurrentPeriodStart,
		&sub.CurrentPeriodEnd,
		&sub.CancelAtPeriodEnd,
		&sub.CreatedAt,
		&sub.UpdatedAt,
	)
	
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("未找到订阅")
	} else if err != nil {
		return nil, err
	}
	
	return &sub, nil
}

// UpdateSubscriptionPlan 更新订阅套餐
func (s *BillingService) UpdateSubscriptionPlan(userID, newPlan string) error {
	// 1. 获取当前订阅
	sub, err := s.GetSubscription(userID)
	if err != nil {
		return err
	}
	
	// 2. 更新 Stripe 订阅 (这里简化,实际应该调用 Stripe API)
	
	// 3. 更新数据库
	_, err = s.db.Exec(`
		UPDATE subscriptions
		SET plan = $1, updated_at = $2
		WHERE id = $3
	`, newPlan, time.Now(), sub.ID)
	
	if err != nil {
		return fmt.Errorf("更新订阅失败: %v", err)
	}
	
	logger.Info("✅ 订阅已更新: 用户=%s, 新套餐=%s", userID, newPlan)
	return nil
}

// CancelSubscription 取消订阅
func (s *BillingService) CancelSubscription(userID string, immediately bool) error {
	// 1. 获取当前订阅
	sub, err := s.GetSubscription(userID)
	if err != nil {
		return err
	}
	
	// 2. 取消 Stripe 订阅 (这里简化,实际应该调用 Stripe API)
	
	// 3. 更新数据库
	if immediately {
		// 立即取消
		_, err = s.db.Exec(`
			UPDATE subscriptions
			SET status = 'cancelled', updated_at = $1
			WHERE id = $2
		`, time.Now(), sub.ID)
	} else {
		// 周期结束时取消
		_, err = s.db.Exec(`
			UPDATE subscriptions
			SET cancel_at_period_end = true, updated_at = $1
			WHERE id = $2
		`, time.Now(), sub.ID)
	}
	
	if err != nil {
		return fmt.Errorf("取消订阅失败: %v", err)
	}
	
	logger.Info("✅ 订阅已取消: 用户=%s, 立即取消=%v", userID, immediately)
	return nil
}

// RenewSubscription 续订
func (s *BillingService) RenewSubscription(userID string) error {
	// 1. 获取当前订阅
	sub, err := s.GetSubscription(userID)
	if err != nil {
		return err
	}
	
	// 2. 处理 Stripe 支付 (这里简化,实际应该调用 Stripe API)
	
	// 3. 更新周期
	newPeriodStart := sub.CurrentPeriodEnd
	newPeriodEnd := newPeriodStart.AddDate(0, 1, 0)
	
	_, err = s.db.Exec(`
		UPDATE subscriptions
		SET current_period_start = $1,
		    current_period_end = $2,
		    cancel_at_period_end = false,
		    updated_at = $3
		WHERE id = $4
	`, newPeriodStart, newPeriodEnd, time.Now(), sub.ID)
	
	if err != nil {
		return fmt.Errorf("续订失败: %v", err)
	}
	
	logger.Info("✅ 订阅已续订: 用户=%s, 新周期=%s", userID, newPeriodEnd.Format("2006-01-02"))
	return nil
}

// GetPriceID 获取套餐价格ID
func (s *BillingService) GetPriceID(plan string) string {
	prices := map[string]string{
		"starter":      "price_starter_monthly",
		"professional": "price_professional_monthly",
		"enterprise":   "price_enterprise_monthly",
	}
	
	if priceID, exists := prices[plan]; exists {
		return priceID
	}
	
	return ""
}

// GetPlanPrice 获取套餐价格
func (s *BillingService) GetPlanPrice(plan string) float64 {
	prices := map[string]float64{
		"starter":      49.00,
		"professional": 199.00,
		"enterprise":   999.00,
	}
	
	if price, exists := prices[plan]; exists {
		return price
	}
	
	return 0.0
}

// InitDatabase 初始化数据库表
func (s *BillingService) InitDatabase() error {
	schema := `
	CREATE TABLE IF NOT EXISTS subscriptions (
		id SERIAL PRIMARY KEY,
		user_id VARCHAR(255) NOT NULL,
		email VARCHAR(255) NOT NULL,
		plan VARCHAR(50) NOT NULL,
		status VARCHAR(20) NOT NULL,
		stripe_subscription_id VARCHAR(255),
		stripe_customer_id VARCHAR(255),
		current_period_start TIMESTAMP NOT NULL,
		current_period_end TIMESTAMP NOT NULL,
		cancel_at_period_end BOOLEAN DEFAULT FALSE,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		INDEX idx_user_id (user_id),
		INDEX idx_status (status)
	);
	
	CREATE TABLE IF NOT EXISTS invoices (
		id SERIAL PRIMARY KEY,
		subscription_id INT NOT NULL,
		user_id VARCHAR(255) NOT NULL,
		amount DECIMAL(10, 2) NOT NULL,
		currency VARCHAR(3) DEFAULT 'USD',
		status VARCHAR(20) NOT NULL,
		stripe_invoice_id VARCHAR(255),
		paid_at TIMESTAMP,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (subscription_id) REFERENCES subscriptions(id),
		INDEX idx_user_id (user_id),
		INDEX idx_status (status)
	);
	`
	
	_, err := s.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("初始化数据库失败: %v", err)
	}
	
	logger.Info("✅ 计费数据库表初始化成功")
	return nil
}

