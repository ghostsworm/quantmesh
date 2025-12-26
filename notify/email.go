package notify

import (
	"fmt"
	"net/smtp"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/logger"
)

// EmailNotifier 邮件通知器
type EmailNotifier struct {
	provider string
	smtp     *SMTPProvider
	resend   *ResendProvider
	mailgun  *MailgunProvider
	from     string
	to       string
	subject  string
}

// NewEmailNotifier 创建邮件通知器
func NewEmailNotifier(cfg *config.Config) (*EmailNotifier, error) {
	if !cfg.Notifications.Email.Enabled {
		return nil, fmt.Errorf("邮件通知未启用")
	}

	if cfg.Notifications.Email.From == "" || cfg.Notifications.Email.To == "" {
		return nil, fmt.Errorf("邮件 From 或 To 未配置")
	}

	en := &EmailNotifier{
		provider: cfg.Notifications.Email.Provider,
		from:     cfg.Notifications.Email.From,
		to:       cfg.Notifications.Email.To,
		subject:  cfg.Notifications.Email.Subject,
	}

	// 根据 provider 初始化对应的邮件服务
	switch cfg.Notifications.Email.Provider {
	case "smtp":
		if cfg.Notifications.Email.SMTP.Host == "" {
			return nil, fmt.Errorf("SMTP Host 未配置")
		}
		en.smtp = NewSMTPProvider(cfg)
	case "resend":
		if cfg.Notifications.Email.Resend.APIKey == "" {
			return nil, fmt.Errorf("Resend APIKey 未配置")
		}
		en.resend = NewResendProvider(cfg)
	case "mailgun":
		if cfg.Notifications.Email.Mailgun.APIKey == "" || cfg.Notifications.Email.Mailgun.Domain == "" {
			return nil, fmt.Errorf("Mailgun APIKey 或 Domain 未配置")
		}
		en.mailgun = NewMailgunProvider(cfg)
	default:
		return nil, fmt.Errorf("不支持的邮件服务商: %s", cfg.Notifications.Email.Provider)
	}

	return en, nil
}

// Name 返回通知器名称
func (en *EmailNotifier) Name() string {
	return fmt.Sprintf("Email (%s)", en.provider)
}

// Send 发送通知
func (en *EmailNotifier) Send(evt *event.Event) error {
	message := formatEmailMessage(evt)
	subject := en.subject
	if subject == "" {
		subject = fmt.Sprintf("QuantMesh 通知: %s", string(evt.Type))
	}

	switch en.provider {
	case "smtp":
		return en.smtp.Send(en.from, en.to, subject, message)
	case "resend":
		return en.resend.Send(en.from, en.to, subject, message)
	case "mailgun":
		return en.mailgun.Send(en.from, en.to, subject, message)
	default:
		return fmt.Errorf("不支持的邮件服务商: %s", en.provider)
	}
}

// formatEmailMessage 格式化邮件消息
func formatEmailMessage(evt *event.Event) string {
	var title string
	switch evt.Type {
	case event.EventTypeOrderPlaced:
		title = "订单已下单"
	case event.EventTypeOrderFilled:
		title = "订单已成交"
	case event.EventTypeOrderCanceled:
		title = "订单已取消"
	case event.EventTypeRiskTriggered:
		title = "风控触发"
	case event.EventTypeRiskRecovered:
		title = "风控解除"
	case event.EventTypeStopLoss:
		title = "止损触发"
	case event.EventTypeTakeProfit:
		title = "止盈触发"
	case event.EventTypeError:
		title = "系统错误"
	case event.EventTypeSystemStart:
		title = "系统启动"
	case event.EventTypeSystemStop:
		title = "系统停止"
	default:
		title = "系统通知"
	}

	message := fmt.Sprintf("%s\n\n", title)
	message += fmt.Sprintf("时间: %s\n\n", evt.Timestamp.Format("2006-01-02 15:04:05"))

	// 添加事件数据
	if evt.Data != nil {
		message += "详细信息:\n"
		for key, value := range evt.Data {
			message += fmt.Sprintf("  %s: %v\n", key, value)
		}
	}

	return message
}

// SMTPProvider SMTP 邮件提供者
type SMTPProvider struct {
	host     string
	port     int
	username string
	password string
}

// NewSMTPProvider 创建 SMTP 提供者
func NewSMTPProvider(cfg *config.Config) *SMTPProvider {
	return &SMTPProvider{
		host:     cfg.Notifications.Email.SMTP.Host,
		port:     cfg.Notifications.Email.SMTP.Port,
		username: cfg.Notifications.Email.SMTP.Username,
		password: cfg.Notifications.Email.SMTP.Password,
	}
}

// Send 发送邮件
func (sp *SMTPProvider) Send(from, to, subject, body string) error {
	if sp.port <= 0 {
		sp.port = 587 // 默认端口
	}

	addr := fmt.Sprintf("%s:%d", sp.host, sp.port)
	auth := smtp.PlainAuth("", sp.username, sp.password, sp.host)

	msg := []byte(fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\n\r\n%s\r\n",
		from, to, subject, body))

	return smtp.SendMail(addr, auth, from, []string{to}, msg)
}

// ResendProvider Resend 邮件提供者
type ResendProvider struct {
	apiKey string
}

// NewResendProvider 创建 Resend 提供者
func NewResendProvider(cfg *config.Config) *ResendProvider {
	return &ResendProvider{
		apiKey: cfg.Notifications.Email.Resend.APIKey,
	}
}

// Send 发送邮件（使用 Resend API）
func (rp *ResendProvider) Send(from, to, subject, body string) error {
	// 注意：这里需要安装 resend-go 库
	// 为了简化，这里先返回一个错误提示
	// 实际实现需要使用 github.com/resend/resend-go/v2
	logger.Warn("⚠️ Resend 邮件服务需要安装 resend-go 库，当前使用占位实现")
	return fmt.Errorf("Resend 邮件服务暂未实现，请安装 github.com/resend/resend-go/v2")
}

// MailgunProvider Mailgun 邮件提供者
type MailgunProvider struct {
	apiKey string
	domain string
}

// NewMailgunProvider 创建 Mailgun 提供者
func NewMailgunProvider(cfg *config.Config) *MailgunProvider {
	return &MailgunProvider{
		apiKey: cfg.Notifications.Email.Mailgun.APIKey,
		domain: cfg.Notifications.Email.Mailgun.Domain,
	}
}

// Send 发送邮件（使用 Mailgun API）
func (mp *MailgunProvider) Send(from, to, subject, body string) error {
	// 注意：这里需要安装 mailgun-go 库
	// 为了简化，这里先返回一个错误提示
	// 实际实现需要使用 github.com/mailgun/mailgun-go/v4
	logger.Warn("⚠️ Mailgun 邮件服务需要安装 mailgun-go 库，当前使用占位实现")
	return fmt.Errorf("Mailgun 邮件服务暂未实现，请安装 github.com/mailgun/mailgun-go/v4")
}

