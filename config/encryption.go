package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/pbkdf2"
)

const (
	// Legacy provider-shaped prefixes identify ciphertext emitted by the prior format.
	// New ciphertext always uses VersionedEncryptionPrefix.
	EncryptionPrefixAWS       = "AKIA" // AWS Access Key 格式
	EncryptionPrefixAliyun    = "LTAI" // Aliyun Access Key 格式
	EncryptionPrefixTencent   = "AKID" // Tencent Cloud Access Key 格式
	EncryptionPrefixGoogle    = "GOOG" // Google Cloud 格式
	VersionedEncryptionPrefix = "QME1:"
	// EncryptionPrefix is retained for reading legacy ciphertext only.
	EncryptionPrefix = EncryptionPrefixAWS

	// MasterKeyEnvVar 主密钥环境变量名
	MasterKeyEnvVar = "QUANTMESH_MASTER_KEY"
	// DefaultMasterKeyPath 默认主密钥文件路径
	DefaultMasterKeyPath = "./data/master.key"
	// MasterKeyFilePerm 主密钥文件权限（仅所有者可读写）
	MasterKeyFilePerm = 0600
	// PBKDF2Iterations PBKDF2迭代次数
	PBKDF2Iterations = 100000
	// SaltSize 盐值大小（字节）
	SaltSize = 16
	// NonceSize GCM nonce大小（字节）
	NonceSize = 12
	// EncryptedKeyLength 加密后密钥的总长度（前缀4字节 + base64编码的数据）
	// AWS Access Key 格式：AKIA + 16个字符 = 20个字符
	// 我们使用 AKIA + base64编码的加密数据（约44-48个字符），总长度约48-52字符
	// 这看起来像是一个有效的AWS密钥格式

	// ConfigHMACKeyEnvVar 配置文件HMAC密钥环境变量名
	ConfigHMACKeyEnvVar = "QUANTMESH_CONFIG_HMAC_KEY"
	// DefaultConfigHMACKeyPath 默认配置文件HMAC密钥文件路径
	DefaultConfigHMACKeyPath = "./data/config.hmac.key"
)

// EncryptAPIKey 加密API密钥
// 使用AES-256-GCM及无损 Base64URL 编码，输出带版本标记的密文。
func EncryptAPIKey(key string, masterKey []byte) (string, error) {
	if len(key) == 0 {
		return "", nil
	}
	if len(masterKey) < 32 {
		return "", fmt.Errorf("master key must contain at least 32 bytes")
	}

	// 生成随机盐值
	salt := make([]byte, SaltSize)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("生成盐值失败: %w", err)
	}

	// 使用PBKDF2从主密钥派生加密密钥
	derivedKey := pbkdf2.Key(masterKey, salt, PBKDF2Iterations, 32, sha256.New)

	// 创建AES cipher
	block, err := aes.NewCipher(derivedKey)
	if err != nil {
		return "", fmt.Errorf("创建AES cipher失败: %w", err)
	}

	// 创建GCM
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("创建GCM失败: %w", err)
	}

	// 生成nonce
	nonce := make([]byte, NonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("生成nonce失败: %w", err)
	}

	// 加密
	plaintext := []byte(key)
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	// 组合：salt + nonce + ciphertext
	combined := append(salt, append(nonce, ciphertext...)...)

	encoded := base64.RawURLEncoding.EncodeToString(combined)
	return VersionedEncryptionPrefix + encoded, nil
}

// DecryptAPIKey 解密API密钥
// 新密文使用 QME1 标记；旧的 provider-shaped 格式仅为兼容读取。
func DecryptAPIKey(encrypted string, masterKey []byte) (string, error) {
	if len(encrypted) == 0 {
		return "", nil
	}

	if strings.HasPrefix(encrypted, VersionedEncryptionPrefix) {
		combined, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(encrypted, VersionedEncryptionPrefix))
		if err != nil {
			return "", fmt.Errorf("decode versioned encrypted credential: %w", err)
		}
		return decryptCredentialPayload(combined, masterKey)
	}
	if !IsEncrypted(encrypted) {
		return encrypted, nil
	}

	// Legacy ciphertext used a lossy provider-shaped alphabet. Keep reading
	// those values for compatibility, but all new ciphertext uses QME1.
	var encoded string
	if len(encrypted) >= len(EncryptionPrefixAWS) && encrypted[:len(EncryptionPrefixAWS)] == EncryptionPrefixAWS {
		encoded = encrypted[len(EncryptionPrefixAWS):]
	} else if len(encrypted) >= len(EncryptionPrefixAliyun) && encrypted[:len(EncryptionPrefixAliyun)] == EncryptionPrefixAliyun {
		encoded = encrypted[len(EncryptionPrefixAliyun):]
	} else if len(encrypted) >= len(EncryptionPrefixTencent) && encrypted[:len(EncryptionPrefixTencent)] == EncryptionPrefixTencent {
		encoded = encrypted[len(EncryptionPrefixTencent):]
	} else if len(encrypted) >= len(EncryptionPrefixGoogle) && encrypted[:len(EncryptionPrefixGoogle)] == EncryptionPrefixGoogle {
		encoded = encrypted[len(EncryptionPrefixGoogle):]
	} else {
		// 没有加密前缀，返回原字符串（向后兼容）
		return encrypted, nil
	}

	// 将AWS格式转换回base64 URL格式（反转convertToAWSFormat）
	normalized := convertFromAWSFormat(encoded)

	// Base64 URL解码（无填充）
	combined, err := base64.URLEncoding.WithPadding(base64.NoPadding).DecodeString(normalized)
	if err != nil {
		// 如果URL解码失败，尝试添加填充后解码
		// base64 URL编码可能需要填充
		for len(normalized)%4 != 0 {
			normalized += "="
		}
		combined, err = base64.URLEncoding.DecodeString(normalized)
		if err != nil {
			return "", fmt.Errorf("Base64解码失败: %w", err)
		}
	}

	return decryptCredentialPayload(combined, masterKey)
}

func decryptCredentialPayload(combined, masterKey []byte) (string, error) {
	if len(masterKey) < 32 {
		return "", fmt.Errorf("master key must contain at least 32 bytes")
	}
	// 检查长度
	if len(combined) < SaltSize+NonceSize {
		return "", fmt.Errorf("加密数据长度不足")
	}

	// 提取salt、nonce和ciphertext
	salt := combined[:SaltSize]
	nonce := combined[SaltSize : SaltSize+NonceSize]
	ciphertext := combined[SaltSize+NonceSize:]

	// 使用PBKDF2从主密钥派生解密密钥
	derivedKey := pbkdf2.Key(masterKey, salt, PBKDF2Iterations, 32, sha256.New)

	// 创建AES cipher
	block, err := aes.NewCipher(derivedKey)
	if err != nil {
		return "", fmt.Errorf("创建AES cipher失败: %w", err)
	}

	// 创建GCM
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("创建GCM失败: %w", err)
	}

	// 解密
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("解密失败: %w", err)
	}

	return string(plaintext), nil
}

// LoadMasterKey 加载主密钥（如果存在）
// 优先从环境变量加载，其次从文件加载，如果都不存在则返回 nil
// 不会自动生成新密钥，用于配置加载时的自动解密
func LoadMasterKey(keyPath string) ([]byte, error) {
	// 1. 优先从环境变量加载
	if envKey := os.Getenv(MasterKeyEnvVar); envKey != "" {
		// 环境变量中的密钥可以是base64编码的，也可以是原始密钥
		// 尝试解码，如果失败则使用原始值
		decoded, err := base64.StdEncoding.DecodeString(envKey)
		if err == nil && len(decoded) >= 32 {
			return decoded, nil
		}
		// 如果不是base64编码，使用原始值（需要至少32字节）
		if len(envKey) >= 32 {
			return []byte(envKey)[:32], nil
		}
		return nil, fmt.Errorf("环境变量 %s 的密钥长度不足（至少需要32字节）", MasterKeyEnvVar)
	}

	// 2. 从文件加载
	if keyPath == "" {
		keyPath = DefaultMasterKeyPath
	}

	// 尝试读取文件
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		// 文件不存在，返回 nil（不生成新密钥）
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取密钥文件失败: %w", err)
	}

	// 文件存在，解码base64
	decoded, err := base64.StdEncoding.DecodeString(string(keyData))
	if err != nil {
		return nil, fmt.Errorf("解码密钥文件失败: %w", err)
	}
	if len(decoded) < 32 {
		return nil, fmt.Errorf("密钥文件中的密钥长度不足（至少需要32字节）")
	}
	return decoded[:32], nil
}

// LoadOrGenerateMasterKey 加载或生成主密钥
// 优先从环境变量加载，其次从文件加载，如果都不存在则生成新密钥
func LoadOrGenerateMasterKey(keyPath string) ([]byte, error) {
	// 1. 优先从环境变量加载
	if envKey := os.Getenv(MasterKeyEnvVar); envKey != "" {
		// 环境变量中的密钥可以是base64编码的，也可以是原始密钥
		// 尝试解码，如果失败则使用原始值
		decoded, err := base64.StdEncoding.DecodeString(envKey)
		if err == nil && len(decoded) >= 32 {
			return decoded, nil
		}
		// 如果不是base64编码，使用原始值（需要至少32字节）
		if len(envKey) >= 32 {
			return []byte(envKey)[:32], nil
		}
		return nil, fmt.Errorf("环境变量 %s 的密钥长度不足（至少需要32字节）", MasterKeyEnvVar)
	}

	// 2. 从文件加载
	if keyPath == "" {
		keyPath = DefaultMasterKeyPath
	}

	// 确保目录存在
	dir := filepath.Dir(keyPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("创建密钥目录失败: %w", err)
	}

	// 尝试读取文件
	keyData, err := os.ReadFile(keyPath)
	if err == nil {
		// 文件存在，解码base64
		decoded, err := base64.StdEncoding.DecodeString(string(keyData))
		if err != nil {
			return nil, fmt.Errorf("解码密钥文件失败: %w", err)
		}
		if len(decoded) < 32 {
			return nil, fmt.Errorf("密钥文件中的密钥长度不足（至少需要32字节）")
		}
		return decoded[:32], nil
	}

	// 3. 文件不存在，生成新密钥
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("读取密钥文件失败: %w", err)
	}

	// 生成32字节随机密钥
	newKey := make([]byte, 32)
	if _, err := rand.Read(newKey); err != nil {
		return nil, fmt.Errorf("生成密钥失败: %w", err)
	}

	// 保存到文件（base64编码）
	encoded := base64.StdEncoding.EncodeToString(newKey)
	if err := os.WriteFile(keyPath, []byte(encoded), MasterKeyFilePerm); err != nil {
		return nil, fmt.Errorf("保存密钥文件失败: %w", err)
	}

	return newKey, nil
}

// convertFromAWSFormat 将AWS格式转换回base64 URL格式（反转convertToAWSFormat）
func convertFromAWSFormat(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z':
			// A-Z可能是：
			// 1. 原始A-Z -> 保持原样
			// 2. 从a-z转换来的大写字母 -> 需要转换回小写
			// 3. 从-转换来的X -> 需要转换回-
			// 4. 从_转换来的Y -> 需要转换回_
			// 由于无法完全区分，我们使用启发式方法：
			// - X和Y在base64中较少见，假设是从-和_转换来的
			// - 其他A-Z保持原样（因为base64本身就有A-Z）
			if c == 'X' {
				result[i] = '-' // X -> -
			} else if c == 'Y' {
				result[i] = '_' // Y -> _
			} else {
				result[i] = c // A-Z保持原样
			}
		case c >= '0' && c <= '9':
			result[i] = c // 0-9保持不变
		default:
			result[i] = 'A' // 默认
		}
	}
	return string(result)
}

// IsEncrypted 检查字符串是否已加密
// 检查是否以已知的加密前缀开头
func IsEncrypted(s string) bool {
	if strings.HasPrefix(s, VersionedEncryptionPrefix) {
		return true
	}
	// Real provider API key IDs often use these prefixes. The legacy encrypted
	// format is substantially longer than those IDs, so avoid classifying a
	// normal short key as ciphertext.
	if len(s) < 40 {
		return false
	}
	prefix := s[:4]
	return prefix == EncryptionPrefixAWS ||
		prefix == EncryptionPrefixAliyun ||
		prefix == EncryptionPrefixTencent ||
		prefix == EncryptionPrefixGoogle
}
