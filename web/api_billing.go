package web

import (
	"github.com/gin-gonic/gin"

	"quantmesh/saas"
)

var (
	billingService *saas.BillingService
)

// SetBillingService 设置计费服务
func SetBillingService(bs *saas.BillingService) {
	billingService = bs
}

// createSubscriptionHandler 创建订阅
// POST /api/billing/subscriptions/create
func createSubscriptionHandler(c *gin.Context) {
	var req struct {
		Plan  string `json:"plan" binding:"required"`
		Email string `json:"email" binding:"required"`
	}

	if err := c.BindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "无效的请求参数"})
		return
	}

	// 获取用户ID
	userID := c.GetString("user_id")
	if userID == "" {
		userID = "demo_user"
	}

	// 创建订阅
	subscription, err := billingService.CreateSubscription(userID, req.Email, req.Plan)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{
		"subscription": subscription,
		"message":      "订阅创建成功",
	})
}

// getSubscriptionHandler 获取订阅信息
// GET /api/billing/subscriptions
func getSubscriptionHandler(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		userID = "demo_user"
	}

	subscription, err := billingService.GetSubscription(userID)
	if err != nil {
		c.JSON(404, gin.H{"error": "未找到订阅"})
		return
	}

	c.JSON(200, gin.H{
		"subscription": subscription,
	})
}

// updateSubscriptionPlanHandler 更新订阅套餐
// POST /api/billing/subscriptions/update-plan
func updateSubscriptionPlanHandler(c *gin.Context) {
	var req struct {
		NewPlan string `json:"new_plan" binding:"required"`
	}

	if err := c.BindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "无效的请求参数"})
		return
	}

	userID := c.GetString("user_id")
	if userID == "" {
		userID = "demo_user"
	}

	if err := billingService.UpdateSubscriptionPlan(userID, req.NewPlan); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"message": "订阅套餐已更新"})
}

// cancelSubscriptionHandler 取消订阅
// POST /api/billing/subscriptions/cancel
func cancelSubscriptionHandler(c *gin.Context) {
	var req struct {
		Immediately bool `json:"immediately"`
	}

	c.BindJSON(&req)

	userID := c.GetString("user_id")
	if userID == "" {
		userID = "demo_user"
	}

	if err := billingService.CancelSubscription(userID, req.Immediately); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"message": "订阅已取消"})
}

// getPlansHandler 获取所有套餐信息
// GET /api/billing/plans
func getPlansHandler(c *gin.Context) {
	plans := []map[string]interface{}{
		{
			"id":          "starter",
			"name":        "个人版",
			"price":       49.00,
			"currency":    "USD",
			"interval":    "month",
			"cpu":         1.0,
			"memory":      1024,
			"storage":     10240,
			"symbols":     1,
			"strategies":  []string{"grid"},
			"support":     "email",
			"description": "适合个人交易者、小额资金",
		},
		{
			"id":          "professional",
			"name":        "专业版",
			"price":       199.00,
			"currency":    "USD",
			"interval":    "month",
			"cpu":         2.0,
			"memory":      2048,
			"storage":     51200,
			"symbols":     5,
			"strategies":  []string{"grid", "momentum", "mean_reversion", "ai"},
			"support":     "email+telegram",
			"description": "适合专业交易者、中等资金",
		},
		{
			"id":          "enterprise",
			"name":        "企业版",
			"price":       999.00,
			"currency":    "USD",
			"interval":    "month",
			"cpu":         4.0,
			"memory":      8192,
			"storage":     204800,
			"symbols":     -1, // 无限
			"strategies":  []string{"all", "custom"},
			"support":     "24x7",
			"description": "适合机构、大资金、团队",
		},
	}

	c.JSON(200, gin.H{
		"plans": plans,
	})
}

// stripeWebhookHandler Stripe Webhook 处理
// POST /api/billing/webhook/stripe
func stripeWebhookHandler(c *gin.Context) {
	// 这里应该验证 Stripe 签名
	// 然后处理各种事件:
	// - customer.subscription.created
	// - customer.subscription.updated
	// - customer.subscription.deleted
	// - invoice.paid
	// - invoice.payment_failed

	c.JSON(200, gin.H{"received": true})
}
