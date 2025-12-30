package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
	
	"quantmesh/logger"
)

// CloudValidator 云端验证器
type CloudValidator struct {
	apiEndpoint string
	cache       *LicenseCache
	httpClient  *http.Client
}

// LicenseCache License 缓存
type LicenseCache struct {
	entries map[string]*CacheEntry
	mu      sync.RWMutex
}

// CacheEntry 缓存条目
type CacheEntry struct {
	LicenseKey  string
	ValidatedAt time.Time
	ExpiryDate  time.Time
	Valid       bool
}

// NewLicenseCache 创建 License 缓存
func NewLicenseCache() *LicenseCache {
	return &LicenseCache{
		entries: make(map[string]*CacheEntry),
	}
}

// Get 获取缓存
func (c *LicenseCache) Get(licenseKey string) *CacheEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	entry, exists := c.entries[licenseKey]
	if !exists {
		return nil
	}
	
	return entry
}

// Set 设置缓存
func (c *LicenseCache) Set(licenseKey string, expiryDate time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	c.entries[licenseKey] = &CacheEntry{
		LicenseKey:  licenseKey,
		ValidatedAt: time.Now(),
		ExpiryDate:  expiryDate,
		Valid:       true,
	}
}

// Clear 清空缓存
func (c *LicenseCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	c.entries = make(map[string]*CacheEntry)
}

// NewCloudValidator 创建云端验证器
func NewCloudValidator(apiEndpoint string) *CloudValidator {
	if apiEndpoint == "" {
		apiEndpoint = "https://license.quantmesh.io" // 默认 License 服务器地址
	}
	
	return &CloudValidator{
		apiEndpoint: apiEndpoint,
		cache:       NewLicenseCache(),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Validate 验证 License
func (v *CloudValidator) Validate(licenseKey string) error {
	// 1. 检查本地缓存 (24小时有效期)
	if cached := v.cache.Get(licenseKey); cached != nil {
		if time.Since(cached.ValidatedAt) < 24*time.Hour {
			logger.Debug("使用缓存的 License 验证结果")
			return nil // 缓存有效
		}
	}
	
	// 2. 云端验证
	logger.Info("正在进行云端 License 验证...")
	
	reqBody := map[string]interface{}{
		"license_key": licenseKey,
		"machine_id":  getMachineID(),
		"timestamp":   time.Now().Unix(),
	}
	
	reqData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("序列化请求失败: %v", err)
	}
	
	resp, err := v.httpClient.Post(
		v.apiEndpoint+"/api/license/verify",
		"application/json",
		bytes.NewBuffer(reqData),
	)
	
	if err != nil {
		// 网络错误,使用本地缓存 (宽容模式)
		if cached := v.cache.Get(licenseKey); cached != nil {
			logger.Warn("⚠️ 云端验证失败,使用本地缓存: %v", err)
			return nil
		}
		return fmt.Errorf("License 验证失败: %v", err)
	}
	defer resp.Body.Close()
	
	// 3. 解析响应
	var result struct {
		Status  string    `json:"status"`
		Message string    `json:"message"`
		Expiry  time.Time `json:"expiry"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("解析响应失败: %v", err)
	}
	
	if resp.StatusCode != 200 || result.Status != "valid" {
		return errors.New("License 无效或已过期: " + result.Message)
	}
	
	// 4. 更新缓存
	v.cache.Set(licenseKey, result.Expiry)
	logger.Info("✅ License 验证通过,有效期至: %s", result.Expiry.Format("2006-01-02"))
	
	return nil
}

// ValidateWithRetry 带重试的验证
func (v *CloudValidator) ValidateWithRetry(licenseKey string, maxRetries int) error {
	var lastErr error
	
	for i := 0; i < maxRetries; i++ {
		err := v.Validate(licenseKey)
		if err == nil {
			return nil
		}
		
		lastErr = err
		logger.Warn("⚠️ License 验证失败 (尝试 %d/%d): %v", i+1, maxRetries, err)
		
		if i < maxRetries-1 {
			time.Sleep(time.Duration(i+1) * time.Second)
		}
	}
	
	return fmt.Errorf("License 验证失败 (已重试 %d 次): %v", maxRetries, lastErr)
}

// StartHeartbeat 启动心跳检测
func (v *CloudValidator) StartHeartbeat(licenseKey string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	
	logger.Info("🔄 启动 License 心跳检测,间隔: %v", interval)
	
	for range ticker.C {
		if err := v.Validate(licenseKey); err != nil {
			logger.Error("❌ License 心跳检测失败: %v", err)
		} else {
			logger.Debug("✅ License 心跳检测通过")
		}
	}
}

// ClearCache 清空缓存
func (v *CloudValidator) ClearCache() {
	v.cache.Clear()
	logger.Info("✅ License 缓存已清空")
}

