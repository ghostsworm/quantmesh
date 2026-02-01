package exchange

import "context"

// PermissionChecker API 权限检测接口
type PermissionChecker interface {
	// CheckAPIPermissions 检查 API 密钥权限
	CheckAPIPermissions(ctx context.Context) (*APIPermissions, error)
}

// APIPermissions API 权限信息
type APIPermissions struct {
	// 基本权限
	CanTrade    bool `json:"can_trade"`    // 是否可以交易
	CanWithdraw bool `json:"can_withdraw"` // 是否可以提现
	CanTransfer bool `json:"can_transfer"` // 是否可以转账
	CanRead     bool `json:"can_read"`     // 是否可以读取数据

	// IP 限制
	IPRestricted bool     `json:"ip_restricted"` // 是否启用 IP 限制
	AllowedIPs   []string `json:"allowed_ips"`   // 允许的 IP 列表

	// 其他信息
	APIKeyName string `json:"api_key_name"` // API Key 名称/标签
	CreateTime int64  `json:"create_time"`  // 创建时间（Unix 时间戳）

	// 安全评分（0-100，越高越安全）
	SecurityScore int    `json:"security_score"`
	RiskLevel     string `json:"risk_level"` // "low", "medium", "high"
}

// CalculateSecurityScore 计算安全评分
func (p *APIPermissions) CalculateSecurityScore() {
	score := 100

	// 如果有提现权限，扣 50 分
	if p.CanWithdraw {
		score -= 50
	}

	// 如果有转账权限，扣 30 分
	if p.CanTransfer {
		score -= 30
	}

	// 如果没有 IP 限制，扣 20 分
	if !p.IPRestricted {
		score -= 20
	}

	if score < 0 {
		score = 0
	}

	p.SecurityScore = score

	// 设置风险等级
	if score >= 80 {
		p.RiskLevel = "low"
	} else if score >= 50 {
		p.RiskLevel = "medium"
	} else {
		p.RiskLevel = "high"
	}
}

// IsSecure 判断 API 密钥是否安全（用于交易）
func (p *APIPermissions) IsSecure() bool {
	// 不能有提现权限
	if p.CanWithdraw {
		return false
	}

	// 必须有交易权限
	if !p.CanTrade {
		return false
	}

	return true
}

// GetWarnings 获取安全警告列表
func (p *APIPermissions) GetWarnings() []string {
	warnings := []string{}

	if p.CanWithdraw {
		warnings = append(warnings, "⚠️ 危险：API 密钥具有提现权限！强烈建议禁用")
	}

	if p.CanTransfer {
		warnings = append(warnings, "⚠️ 警告：API 密钥具有转账权限，建议禁用")
	}

	if !p.IPRestricted {
		warnings = append(warnings, "💡 建议：启用 IP 白名单限制以提高安全性")
	}

	if !p.CanTrade {
		warnings = append(warnings, "ℹ️ 注意：API 密钥没有交易权限，无法进行做市")
	}

	return warnings
}
