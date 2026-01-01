package plugin

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// LicenseInfo 许可证信息
type LicenseInfo struct {
	PluginName    string    `json:"plugin_name"`    // 插件名称
	LicenseKey    string    `json:"license_key"`    // 许可证密钥
	CustomerID    string    `json:"customer_id"`    // 客户ID
	Email         string    `json:"email"`          // 客户邮箱
	Plan          string    `json:"plan"`           // 套餐: starter/professional/enterprise
	ExpiryDate    time.Time `json:"expiry_date"`    // 过期时间
	MaxInstances  int       `json:"max_instances"`  // 最大实例数
	Features      []string  `json:"features"`       // 授权功能列表
	IssuedAt      time.Time `json:"issued_at"`      // 签发时间
	MachineID     string    `json:"machine_id"`     // 机器ID (可选)
	CloudVerify   bool      `json:"cloud_verify"`   // 是否需要云端验证
	Signature     string    `json:"signature"`      // 签名
}

// LicenseStore 许可证存储
type LicenseStore struct {
	licenses map[string]*LicenseInfo
	mu       sync.RWMutex
	filePath string
}

// NewLicenseStore 创建许可证存储
func NewLicenseStore() *LicenseStore {
	homeDir, _ := os.UserHomeDir()
	filePath := filepath.Join(homeDir, ".quantmesh", "licenses.enc")

	store := &LicenseStore{
		licenses: make(map[string]*LicenseInfo),
		filePath: filePath,
	}

	// 尝试加载已保存的许可证
	_ = store.Load()

	return store
}

// Store 存储许可证
func (s *LicenseStore) Store(pluginName, licenseKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 解析许可证
	info, err := ParseLicense(licenseKey)
	if err != nil {
		return fmt.Errorf("解析许可证失败: %v", err)
	}

	info.PluginName = pluginName
	s.licenses[pluginName] = info

	// 持久化到文件
	return s.save()
}

// Get 获取许可证
func (s *LicenseStore) Get(pluginName string) (*LicenseInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	info, exists := s.licenses[pluginName]
	if !exists {
		return nil, fmt.Errorf("插件 %s 的许可证未找到", pluginName)
	}

	return info, nil
}

// Validate 验证许可证
func (s *LicenseStore) Validate(pluginName string) error {
	info, err := s.Get(pluginName)
	if err != nil {
		return err
	}

	// 1. 检查是否过期
	if time.Now().After(info.ExpiryDate) {
		return fmt.Errorf("许可证已过期: %s", info.ExpiryDate.Format("2006-01-02"))
	}

	// 2. 验证签名
	if !verifySignature(info) {
		return errors.New("许可证签名无效")
	}

	// 3. 检查机器ID (如果指定)
	if info.MachineID != "" {
		currentMachineID := getMachineID()
		if info.MachineID != currentMachineID {
			return errors.New("许可证与当前机器不匹配")
		}
	}

	return nil
}

// save 保存许可证到文件 (加密)
func (s *LicenseStore) save() error {
	// 确保目录存在
	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	// 序列化
	data, err := json.Marshal(s.licenses)
	if err != nil {
		return err
	}

	// 加密
	encrypted, err := encrypt(data, getEncryptionKey())
	if err != nil {
		return err
	}

	// 写入文件
	return os.WriteFile(s.filePath, encrypted, 0600)
}

// Load 从文件加载许可证
func (s *LicenseStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 读取文件
	encrypted, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // 文件不存在，不是错误
		}
		return err
	}

	// 解密
	data, err := decrypt(encrypted, getEncryptionKey())
	if err != nil {
		return err
	}

	// 反序列化
	return json.Unmarshal(data, &s.licenses)
}

// ParseLicense 解析许可证字符串
func ParseLicense(licenseKey string) (*LicenseInfo, error) {
	// 许可证格式: BASE64(JSON)
	decoded, err := base64.StdEncoding.DecodeString(licenseKey)
	if err != nil {
		return nil, fmt.Errorf("许可证格式错误: %v", err)
	}

	var info LicenseInfo
	if err := json.Unmarshal(decoded, &info); err != nil {
		return nil, fmt.Errorf("许可证数据错误: %v", err)
	}

	return &info, nil
}

// GenerateLicense 生成许可证 (用于许可证服务器)
func GenerateLicense(
	pluginName string,
	customerID string,
	expiryDate time.Time,
	maxInstances int,
	features []string,
	machineID string,
	secretKey string,
) (string, error) {
	info := &LicenseInfo{
		PluginName:   pluginName,
		CustomerID:   customerID,
		ExpiryDate:   expiryDate,
		MaxInstances: maxInstances,
		Features:     features,
		IssuedAt:     time.Now(),
		MachineID:    machineID,
	}

	// 生成签名
	info.Signature = generateSignature(info, secretKey)

	// 序列化
	data, err := json.Marshal(info)
	if err != nil {
		return "", err
	}

	// Base64编码
	return base64.StdEncoding.EncodeToString(data), nil
}

// generateSignature 生成签名
func generateSignature(info *LicenseInfo, secretKey string) string {
	// 必须与 license-server 的签名算法一致
	data := fmt.Sprintf("%s:%s:%s:%s",
		info.PluginName,
		info.CustomerID,
		info.ExpiryDate.Format(time.RFC3339),
		secretKey,
	)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// verifySignature 验证签名
func verifySignature(info *LicenseInfo) bool {
	// 这里需要使用相同的密钥验证
	// 实际应用中，密钥应该内置在编译后的二进制中
	secretKey := getSecretKey()
	expectedSignature := generateSignature(info, secretKey)
	return info.Signature == expectedSignature
}

// getSecretKey 获取密钥 (应该从安全的地方获取)
func getSecretKey() string {
	// 实际应用中，这个密钥应该:
	// 1. 编译时内置到二进制中
	// 2. 使用代码混淆保护
	// 3. 或从远程服务器验证
	return "quantmesh-secret-key-2025" // 示例密钥
}

// getMachineID 获取机器ID
func getMachineID() string {
	// 简单实现：使用MAC地址
	// 实际应用中应该使用更可靠的方法
	hostname, _ := os.Hostname()
	hash := sha256.Sum256([]byte(hostname))
	return hex.EncodeToString(hash[:8])
}

// getEncryptionKey 获取加密密钥
func getEncryptionKey() []byte {
	// 使用固定密钥 (实际应用中应该更安全)
	// AES-256 需要 32 字节密钥
	key := "quantmesh-encryption-key-2025"
	// 确保是32字节
	keyBytes := []byte(key)
	if len(keyBytes) < 32 {
		// 填充到32字节
		padding := make([]byte, 32-len(keyBytes))
		keyBytes = append(keyBytes, padding...)
	}
	return keyBytes[:32]
}

// encrypt 加密数据
func encrypt(plaintext []byte, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// decrypt 解密数据
func decrypt(ciphertext []byte, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, errors.New("密文太短")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}

	return plaintext, nil
}

// LicenseValidator 许可证验证器
type LicenseValidator struct {
	store          *LicenseStore
	cloudValidator *CloudValidator
}

// NewLicenseValidator 创建许可证验证器
func NewLicenseValidator() *LicenseValidator {
	// 使用本地测试服务器
	return &LicenseValidator{
		store:          NewLicenseStore(),
		cloudValidator: NewCloudValidator("http://127.0.0.1:8000"),
	}
}

// NewLicenseValidatorWithEndpoint 创建许可证验证器(指定云端地址)
func NewLicenseValidatorWithEndpoint(cloudEndpoint string) *LicenseValidator {
	return &LicenseValidator{
		store:          NewLicenseStore(),
		cloudValidator: NewCloudValidator(cloudEndpoint),
	}
}

// ValidatePlugin 验证插件许可证
func (v *LicenseValidator) ValidatePlugin(pluginName string, licenseKey string) error {
	// 1. 解析许可证
	info, err := ParseLicense(licenseKey)
	if err != nil {
		return err
	}

	// 2. 检查插件名称
	if info.PluginName != pluginName {
		return fmt.Errorf("许可证不匹配: 期望 %s, 实际 %s", pluginName, info.PluginName)
	}

	// 3. 检查过期时间
	if time.Now().After(info.ExpiryDate) {
		return fmt.Errorf("许可证已过期: %s", info.ExpiryDate.Format("2006-01-02"))
	}

	// 4. 验证签名
	if !verifySignature(info) {
		return errors.New("许可证签名无效")
	}

	// 5. 检查机器ID
	if info.MachineID != "" {
		currentMachineID := getMachineID()
		if info.MachineID != currentMachineID {
			return errors.New("许可证与当前机器不匹配")
		}
	}

	// 6. 云端验证 (如果需要)
	if info.CloudVerify && v.cloudValidator != nil {
		if err := v.cloudValidator.ValidateWithRetry(licenseKey, 3); err != nil {
			return fmt.Errorf("云端验证失败: %v", err)
		}
	}

	// 7. 存储许可证
	return v.store.Store(pluginName, licenseKey)
}

// CheckFeature 检查功能是否授权
func (v *LicenseValidator) CheckFeature(pluginName, feature string) bool {
	info, err := v.store.Get(pluginName)
	if err != nil {
		return false
	}

	for _, f := range info.Features {
		if f == feature || f == "*" {
			return true
		}
	}
	return false
}

