package web

import (
	"context"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"math"
	"net/http"
	"quantmesh/logger"
	"time"
)

// StrategyProvider 策略资金分配提供者接口
type StrategyProvider interface {
	GetCapitalAllocation() map[string]StrategyCapitalInfo
	ReleaseLockedCapital(strategyName string) float64
	ReleaseAllLockedCapital() map[string]float64
}

// VerifiedStrategyCapitalProvider must verify runtime risk before mutation.
// Error results may include partial releases and must not be reported as success.
type VerifiedStrategyCapitalProvider interface {
	ReleaseVerifiedCapital(context.Context, string) (float64, error)
	ReleaseAllVerifiedCapital(context.Context) (map[string]float64, error)
}

var ErrStrategyCapitalVerificationUnavailable = errors.New("strategy capital verification is unavailable")

const strategyCapitalReleaseTimeout = 30 * time.Second

type verifiedStrategyProviderAdapter struct {
	strategyProviderAdapter
	releaseVerified    func(context.Context, string) (float64, error)
	releaseAllVerified func(context.Context) (map[string]float64, error)
}

func NewVerifiedStrategyProviderAdapter(getAllocation func() map[string]StrategyCapitalInfo, release func(context.Context, string) (float64, error), releaseAll func(context.Context) (map[string]float64, error)) StrategyProvider {
	return &verifiedStrategyProviderAdapter{
		strategyProviderAdapter: strategyProviderAdapter{getAllocationFunc: getAllocation},
		releaseVerified:         release, releaseAllVerified: releaseAll,
	}
}

func (a *verifiedStrategyProviderAdapter) ReleaseVerifiedCapital(ctx context.Context, name string) (float64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if a.releaseVerified == nil {
		return 0, ErrStrategyCapitalVerificationUnavailable
	}
	return a.releaseVerified(ctx, name)
}

func (a *verifiedStrategyProviderAdapter) ReleaseAllVerifiedCapital(ctx context.Context) (map[string]float64, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if a.releaseAllVerified == nil {
		return nil, ErrStrategyCapitalVerificationUnavailable
	}
	return a.releaseAllVerified(ctx)
}

func capitalReleaseConflict(c *gin.Context, released interface{}, total float64) {
	c.JSON(http.StatusConflict, gin.H{
		"success": false, "error": "capital_release_unverified",
		"message":  "资金释放未完成，必须先核实持仓、委托和资金账本",
		"released": released, "total_released": total, "partial": total > 0,
		"requires_reconciliation": true,
	})
}

func validCapitalReleaseAmount(amount float64) bool {
	return !math.IsNaN(amount) && !math.IsInf(amount, 0) && amount >= 0
}

func capitalReleaseInvalidResult(c *gin.Context) {
	c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "capital_release_result_unverified", "requires_reconciliation": true})
}

// StrategyCapitalInfo 策略资金信息
type StrategyCapitalInfo struct {
	Allocated float64 `json:"allocated"`  // 分配的资金
	Used      float64 `json:"used"`       // 已使用的资金（保证金）
	Available float64 `json:"available"`  // 可用资金
	Weight    float64 `json:"weight"`     // 权重
	FixedPool float64 `json:"fixed_pool"` // 固定资金池（如果指定）
}

// SetStrategyProvider 設置策略數據提供者
func SetStrategyProvider(provider StrategyProvider) {
	strategyProvider = provider
}

// strategyProviderAdapter 策略提供者适配器
type strategyProviderAdapter struct {
	getAllocationFunc     func() map[string]StrategyCapitalInfo
	releaseCapitalFunc    func(strategyName string) float64
	releaseAllCapitalFunc func() map[string]float64
}

// NewStrategyProviderAdapter 創建策略提供者适配器
func NewStrategyProviderAdapter(
	getAllocationFunc func() map[string]StrategyCapitalInfo,
	releaseCapitalFunc func(strategyName string) float64,
	releaseAllCapitalFunc func() map[string]float64,
) StrategyProvider {
	return &strategyProviderAdapter{
		getAllocationFunc:     getAllocationFunc,
		releaseCapitalFunc:    releaseCapitalFunc,
		releaseAllCapitalFunc: releaseAllCapitalFunc,
	}
}

// GetCapitalAllocation 獲取策略资金分配信息
func (a *strategyProviderAdapter) GetCapitalAllocation() map[string]StrategyCapitalInfo {
	return a.getAllocationFunc()
}

// ReleaseLockedCapital 释放指定策略的锁定资金
func (a *strategyProviderAdapter) ReleaseLockedCapital(strategyName string) float64 {
	if a.releaseCapitalFunc != nil {
		return a.releaseCapitalFunc(strategyName)
	}
	return 0
}

// ReleaseAllLockedCapital 释放所有策略的锁定资金
func (a *strategyProviderAdapter) ReleaseAllLockedCapital() map[string]float64 {
	if a.releaseAllCapitalFunc != nil {
		return a.releaseAllCapitalFunc()
	}
	return map[string]float64{}
}

// getStrategyAllocation 獲取策略资金分配信息
// GET /api/strategies/allocation
func getStrategyAllocation(c *gin.Context) {
	if strategyProvider == nil {
		c.JSON(http.StatusOK, gin.H{"allocation": map[string]interface{}{}})
		return
	}

	allocation := strategyProvider.GetCapitalAllocation()
	c.JSON(http.StatusOK, gin.H{"allocation": allocation})
}

// releaseStrategyCapital 释放策略的锁定资金
// POST /api/strategies/:id/release-capital
func releaseStrategyCapital(c *gin.Context) {
	strategyName := c.Param("id")
	if strategyName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少策略名称"})
		return
	}

	if strategyProvider == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "策略服務未初始化"})
		return
	}

	verified, ok := strategyProvider.(VerifiedStrategyCapitalProvider)
	if !ok {
		capitalReleaseConflict(c, 0, 0)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), strategyCapitalReleaseTimeout)
	defer cancel()
	released, err := verified.ReleaseVerifiedCapital(ctx, strategyName)
	if !validCapitalReleaseAmount(released) {
		capitalReleaseInvalidResult(c)
		return
	}
	if err != nil {
		capitalReleaseConflict(c, released, released)
		return
	}
	logger.Info("💰 [手动释放资金] 策略 %s 已释放锁定资金: %.2f USDT", strategyName, released)

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"message":  fmt.Sprintf("已释放 %.2f USDT 锁定资金", released),
		"released": released,
		"strategy": strategyName,
	})
}

// releaseAllStrategiesCapital 释放所有策略的锁定资金
// POST /api/strategies/release-all-capital
func releaseAllStrategiesCapital(c *gin.Context) {
	if strategyProvider == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "策略服務未初始化"})
		return
	}

	verified, ok := strategyProvider.(VerifiedStrategyCapitalProvider)
	if !ok {
		capitalReleaseConflict(c, map[string]float64{}, 0)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), strategyCapitalReleaseTimeout)
	defer cancel()
	released, err := verified.ReleaseAllVerifiedCapital(ctx)

	totalReleased := 0.0
	for _, amount := range released {
		if !validCapitalReleaseAmount(amount) || !validCapitalReleaseAmount(totalReleased+amount) {
			capitalReleaseInvalidResult(c)
			return
		}
		totalReleased += amount
	}
	if err != nil {
		capitalReleaseConflict(c, released, totalReleased)
		return
	}
	for name, amount := range released {
		logger.Info("💰 [手动释放资金] 策略 %s 已释放锁定资金: %.2f USDT", name, amount)
	}

	c.JSON(http.StatusOK, gin.H{
		"success":        true,
		"message":        fmt.Sprintf("已释放所有策略的锁定资金，總計 %.2f USDT", totalReleased),
		"released":       released,
		"total_released": totalReleased,
	})
}
