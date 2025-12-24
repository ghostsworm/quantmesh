package bitget

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"time"
)

// Signer Bitget API 签名器
type Signer struct {
	apiKey     string
	secretKey  string
	passphrase string
}

// NewSigner 创建签名器
func NewSigner(apiKey, secretKey, passphrase string) *Signer {
	return &Signer{
		apiKey:     apiKey,
		secretKey:  secretKey,
		passphrase: passphrase,
	}
}

// Sign 生成签名
// Bitget 签名规则: Base64(HMAC_SHA256(timestamp + method + requestPath + body, secretKey))
func (s *Signer) Sign(timestamp, method, requestPath, body string) string {
	message := timestamp + method + requestPath + body
	mac := hmac.New(sha256.New, []byte(s.secretKey))
	mac.Write([]byte(message))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// GetTimestamp 获取当前时间戳（毫秒）
func (s *Signer) GetTimestamp() string {
	return fmt.Sprintf("%d", time.Now().UnixMilli())
}

// GetAPIKey 获取 API Key
func (s *Signer) GetAPIKey() string {
	return s.apiKey
}

// GetPassphrase 获取 Passphrase
func (s *Signer) GetPassphrase() string {
	return s.passphrase
}

