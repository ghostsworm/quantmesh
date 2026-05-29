package mcp

// RegisterAllTools 注册所有当前 providers 支持的工具。
// 写工具仅在 allowWrite=true 时注册。
//
// 调用方：main 包初始化阶段。
func RegisterAllTools(s *Server, p Providers, allowWrite bool) {
	RegisterMetaTools(s, p)
	RegisterPositionTools(s, p)
	RegisterBotTools(s, p)
	RegisterPnLTools(s, p)
	RegisterLogTools(s, p)
	RegisterBacktestTools(s, p)
	RegisterSettingsTools(s, p)
	RegisterFundingTools(s, p)
	RegisterOrderTools(s, p)
	if allowWrite {
		RegisterWriteTools(s, p)
	}
}
