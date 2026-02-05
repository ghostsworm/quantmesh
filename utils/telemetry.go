package utils

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// TelemetryEvent 统计事件结构
type TelemetryEvent struct {
	Event      string    `json:"event"`       // 事件类型: "install" 或 "startup"
	Timestamp  time.Time `json:"timestamp"`   // 时间戳
	Version    string    `json:"version"`     // 版本号
	OS         string    `json:"os"`         // 操作系统
	Arch       string    `json:"arch"`       // 架构
	IP         string    `json:"ip,omitempty"` // IP 地址（可选，由服务端获取）
}

// TelemetryConfig 统计配置
type TelemetryConfig struct {
	Enabled   bool   // 是否启用统计
	Endpoint  string // 统计服务端点
	ProjectID string // 项目 ID
}

var (
	// DefaultTelemetryConfig 默认统计配置
	// 使用 PostHog Cloud（免费层，开源友好）
	// 如果需要自托管，可以修改为自托管地址
	// 注意：ProjectID 优先从环境变量 QUANTMESH_TELEMETRY_PROJECT_ID 读取
	DefaultTelemetryConfig = TelemetryConfig{
		Enabled:   true,
		Endpoint:  "https://us.i.posthog.com/capture/", // 使用 US 区域的 endpoint
		ProjectID: getTelemetryProjectID(),            // 从环境变量读取，如果没有则使用默认值
	}

	// 获取公网 IP 的超时（ip4.dev 返回纯文本 IP）；总耗时 IP(1.5s)+POST(1.5s)≤3s
	telemetryIPFetchTimeout = 1500 * time.Millisecond
	// 发送 PostHog 请求的超时
	telemetryPostTimeout = 1500 * time.Millisecond

	// 实例 ID 相关
	instanceIDOnce sync.Once
	instanceID     string
	instanceIDMu   sync.RWMutex
)

// getTelemetryProjectID 获取 PostHog Project ID
// 优先从环境变量 QUANTMESH_TELEMETRY_PROJECT_ID 读取
// 如果没有设置环境变量，则返回默认值（仅用于开发/演示，生产环境建议使用环境变量）
func getTelemetryProjectID() string {
	if projectID := os.Getenv("QUANTMESH_TELEMETRY_PROJECT_ID"); projectID != "" {
		return projectID
	}
	// 默认值：仅用于开发/演示，生产环境建议通过环境变量配置
	return "phc_kz2U334i5MD8ozz78zvCdN6aRkkx3kYyoU1RSigJOiA"
}

// getOrCreateInstanceID 获取或创建实例 ID
// 实例 ID 存储在 data/instance_id 文件中，用于唯一标识部署实例
// 不包含任何个人信息，只是一个随机生成的 UUID
func getOrCreateInstanceID() string {
	instanceIDOnce.Do(func() {
		instanceIDMu.Lock()
		defer instanceIDMu.Unlock()

		// 尝试从文件读取
		dataDir := "./data"
		instanceIDPath := filepath.Join(dataDir, "instance_id")

		// 确保数据目录存在
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			// 如果无法创建目录，生成临时 ID（不持久化）
			instanceID = generateInstanceID()
			return
		}

		// 尝试读取现有实例 ID
		if data, err := os.ReadFile(instanceIDPath); err == nil {
			id := strings.TrimSpace(string(data))
			if len(id) > 0 {
				instanceID = id
				return
			}
		}

		// 生成新的实例 ID
		instanceID = generateInstanceID()

		// 保存到文件
		if err := os.WriteFile(instanceIDPath, []byte(instanceID), 0600); err != nil {
			// 如果写入失败，仍然使用生成的 ID（只是不会持久化）
		}
	})

	instanceIDMu.RLock()
	defer instanceIDMu.RUnlock()
	return instanceID
}

// generateInstanceID 生成一个新的实例 ID（UUID v4 格式）
func generateInstanceID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// 如果随机数生成失败，使用时间戳 + hostname 作为后备方案
		hostname, _ := os.Hostname()
		return fmt.Sprintf("%x-%x-%x-%x-%x-%s-%d",
			b[0:4], b[4:6], b[6:8], b[8:10], b[10:16],
			hostname, time.Now().UnixNano())
	}
	// UUID v4 格式
	b[6] = (b[6] & 0x0f) | 0x40 // Version 4
	b[8] = (b[8] & 0x3f) | 0x80 // Variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// SendTelemetry 发送统计事件
// 这是一个完全透明的函数，代码可审查
// 只发送最少的信息：事件类型、时间戳、版本、操作系统、架构、hostname
// 注意：IP 地址仅用于本地生成 distinct_id，不会发送到 PostHog
// 总耗时不超过 3 秒（IP 获取 1.5s + POST 1.5s），异步执行不阻塞主程序
func SendTelemetry(event string, version string) error {
	return SendTelemetryWithProperties(event, version, nil)
}

// SendTelemetryWithProperties 发送带自定义属性的统计事件
// properties 可以包含额外的数据，如交易所、币种、API 耗时等
// 注意：不收集 IP 地址，避免地理位置推断
func SendTelemetryWithProperties(event string, version string, properties map[string]interface{}) error {
	// 检查环境变量，允许用户禁用统计
	if os.Getenv("QUANTMESH_DISABLE_TELEMETRY") == "1" {
		return nil
	}

	config := DefaultTelemetryConfig
	if !config.Enabled {
		return nil
	}

	// 如果 Project ID 未配置，跳过
	if config.ProjectID == "YOUR_POSTHOG_PROJECT_ID" {
		return nil
	}

	// 异步发送，不阻塞主程序；总耗时不超过 3 秒
	go func() {
		// 使用实例 ID 作为 distinct_id，不依赖 IP 地址
		instanceID := getOrCreateInstanceID()

		eventData := TelemetryEvent{
			Event:     event,
			Timestamp: time.Now(),
			Version:   version,
			OS:        runtime.GOOS,
			Arch:      runtime.GOARCH,
			// 注意：IP 字段不填充，不会发送到 PostHog
		}

		// 构建 payload，只包含必要信息，不包含 IP 地址
		eventProperties := map[string]interface{}{
			"timestamp":  eventData.Timestamp.Format(time.RFC3339),
			"version":    eventData.Version,
			"os":         eventData.OS,
			"arch":       eventData.Arch,
			"instance_id": instanceID, // 使用实例 ID 而不是 IP
			// 注意：不包含 IP 地址，避免地理位置推断
		}

		// 合并自定义属性
		if properties != nil {
			for k, v := range properties {
				eventProperties[k] = v
			}
		}

		payload := map[string]interface{}{
			"api_key":     config.ProjectID,
			"event":       eventData.Event,
			"distinct_id": instanceID, // 使用实例 ID 作为唯一标识
			"properties":  eventProperties,
		}

		jsonData, err := json.Marshal(payload)
		if err != nil {
			return
		}

		client := &http.Client{Timeout: telemetryPostTimeout}
		req, err := http.NewRequest("POST", config.Endpoint, bytes.NewBuffer(jsonData))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", fmt.Sprintf("QuantMesh/%s", version))

		resp, err := client.Do(req)
		if err != nil {
			return
		}
		defer resp.Body.Close()
	}()

	return nil
}

// SendInstallTelemetry 发送安装统计
func SendInstallTelemetry(version string) {
	_ = SendTelemetry("install", version)
}

// SendStartupTelemetry 发送启动统计
func SendStartupTelemetry(version string) {
	_ = SendTelemetry("startup", version)
}

// getVersion 获取版本号，优先使用传入的版本号，否则从环境变量获取
func getVersion(version string) string {
	if version != "" {
		return version
	}
	// 尝试从环境变量获取版本号
	if v := os.Getenv("QUANTMESH_VERSION"); v != "" {
		return v
	}
	// 默认返回 "unknown"
	return "unknown"
}

// TrackExchangeUsage 追踪交易所使用情况
func TrackExchangeUsage(version string, exchangeName string, symbol string) {
	_ = SendTelemetryWithProperties("exchange_usage", getVersion(version), map[string]interface{}{
		"exchange": exchangeName,
		"symbol":   symbol,
	})
}

// TrackAPILatency 追踪 API 请求/响应耗时
func TrackAPILatency(version string, exchangeName string, apiMethod string, latencyMs int64, success bool) {
	_ = SendTelemetryWithProperties("api_latency", getVersion(version), map[string]interface{}{
		"exchange":   exchangeName,
		"api_method": apiMethod,
		"latency_ms": latencyMs,
		"success":    success,
	})
}

// TrackWebSocketLatency 追踪 WebSocket 延时
func TrackWebSocketLatency(version string, exchangeName string, latencyMs int64, messageType string) {
	_ = SendTelemetryWithProperties("websocket_latency", getVersion(version), map[string]interface{}{
		"exchange":     exchangeName,
		"latency_ms":   latencyMs,
		"message_type": messageType,
	})
}

// TrackTradingActivity 追踪交易活动
func TrackTradingActivity(version string, exchangeName string, symbol string, side string) {
	_ = SendTelemetryWithProperties("trading_activity", getVersion(version), map[string]interface{}{
		"exchange": exchangeName,
		"symbol":   symbol,
		"side":     side, // "buy" or "sell"
	})
}
