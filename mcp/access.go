package mcp

import (
	"fmt"
	"strings"
)

// 令牌权限。只读令牌看不到也调不了写工具；读写令牌必须带通知方式，
// 写操作成功后按该方式通知。旧的全局令牌用 Legacy 标记，允许在没有通知配置时
// 继续按原来的 mcp_allow_write 行为工作，避免升级后把已接入的 Agent 直接打断。
const (
	AccessReadOnly  = "readonly"
	AccessReadWrite = "readwrite"

	NotifyEmail      = "email"
	NotifyTelegram   = "telegram"
	NotifyWebhook    = "webhook"
	NotifyFeishu     = "feishu"
	NotifyDingTalk   = "dingtalk"
	NotifyWeChatWork = "wechat_work"
	NotifySlack      = "slack"

	ErrTextReadOnly       = "此令牌是只读的，不能执行写操作"
	ErrTextNotifyRequired = "读写令牌未配置通知方式，已拒绝写操作"
)

// NotifySpec 是颁发读写令牌时指定的通知方式。
// Target 的含义由 Channel 决定：邮箱地址、Webhook URL 或 Telegram Chat ID。
// 留空表示使用该渠道已经配好的全局目的地（邮件和通用 Webhook 不允许留空）。
type NotifySpec struct {
	Channel string `json:"channel"`
	Target  string `json:"target,omitempty"`
}

// Access 是一次已通过鉴权的 MCP 请求对应的令牌身份。
type Access struct {
	TokenID    string
	Name       string
	Mode       string
	AllowWrite bool
	Legacy     bool
	Notify     *NotifySpec
}

// WriteCall 是一次已经成功的写工具调用，用于完成后的通知。
type WriteCall struct {
	Tool   string
	Detail string
}

// WriteNotice 是同一个 HTTP 请求里全部成功写操作的汇总。
type WriteNotice struct {
	Access Access
	Calls  []WriteCall
}

// Text 生成发给人看的通知正文。不包含令牌密钥。
func (n WriteNotice) Text() string {
	var b strings.Builder
	name := strings.TrimSpace(n.Access.Name)
	if name == "" {
		name = n.Access.TokenID
	}
	fmt.Fprintf(&b, "QuantMesh MCP 写操作已完成\n令牌：%s\n", name)
	if n.Access.TokenID != "" {
		fmt.Fprintf(&b, "令牌 ID：%s\n", n.Access.TokenID)
	}
	for _, call := range n.Calls {
		detail := strings.TrimSpace(call.Detail)
		if detail == "" {
			detail = "ok"
		}
		fmt.Fprintf(&b, "- %s：%s\n", call.Tool, detail)
	}
	return strings.TrimRight(b.String(), "\n")
}
