package gate

import (
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"time"
)

// Signer Gate.io API v4 签名器
type Signer struct {
	apiKey    string
	secretKey string
}

// NewSigner 创建签名器
func NewSigner(apiKey, secretKey string) *Signer {
	return &Signer{
		apiKey:    apiKey,
		secretKey: secretKey,
	}
}

// SignREST 生成 REST API 签名
// Gate.io v4 签名规则:
// 1. 构造待签名字符串: method + "\n" + url_path + "\n" + query_string + "\n" + hex(sha512(body)) + "\n" + timestamp
// 2. 使用 HMAC-SHA512 计算签名
// 3. 返回十六进制字符串
func (s *Signer) SignREST(method, urlPath, queryString, body string, timestamp int64) string {
	// 计算 body 的 SHA512 哈希（即使为空也要计算）
	hasher := sha512.New()
	if body != "" {
		hasher.Write([]byte(body))
	}
	bodyHash := hex.EncodeToString(hasher.Sum(nil))

	// 构造待签名字符串
	message := fmt.Sprintf("%s\n%s\n%s\n%s\n%d",
		method,
		urlPath,
		queryString,
		bodyHash,
		timestamp,
	)

	// HMAC-SHA512 签名
	mac := hmac.New(sha512.New, []byte(s.secretKey))
	mac.Write([]byte(message))
	signature := hex.EncodeToString(mac.Sum(nil))

	return signature
}

// SignWebSocket 生成 WebSocket 签名
// Gate.io WebSocket 签名规则:
// 1. 构造待签名字符串: "channel=" + channel + "&event=" + event + "&time=" + timestamp
// 2. 使用 HMAC-SHA512 计算签名
// 3. 返回十六进制字符串
func (s *Signer) SignWebSocket(channel, event string, timestamp int64) string {
	// 构造待签名字符串
	message := fmt.Sprintf("channel=%s&event=%s&time=%d", channel, event, timestamp)

	// HMAC-SHA512 签名
	mac := hmac.New(sha512.New, []byte(s.secretKey))
	mac.Write([]byte(message))
	signature := hex.EncodeToString(mac.Sum(nil))

	return signature
}

// GetTimestamp 获取当前时间戳（秒）
func (s *Signer) GetTimestamp() int64 {
	return time.Now().Unix()
}

// GetAPIKey 获取 API Key
func (s *Signer) GetAPIKey() string {
	return s.apiKey
}
