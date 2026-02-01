#!/bin/bash

# QuantMesh 插件脚手架生成器

set -e

# 检查参数
if [ $# -eq 0 ]; then
    echo "用法: $0 <plugin_name>"
    echo "示例: $0 my_strategy"
    exit 1
fi

PLUGIN_NAME=$1
PLUGIN_DIR="plugins/${PLUGIN_NAME}"

# 创建插件目录
echo "📦 创建插件目录: ${PLUGIN_DIR}"
mkdir -p "${PLUGIN_DIR}"

# 生成插件主文件
cat > "${PLUGIN_DIR}/plugin.go" << EOF
package ${PLUGIN_NAME}

import (
	"context"
	"quantmesh/config"
	"quantmesh/plugin"
	"quantmesh/position"
	"quantmesh/strategy"
)

// Plugin ${PLUGIN_NAME} 插件
type Plugin struct {
	metadata  *plugin.PluginMetadata
	strategy  strategy.Strategy
	validator *plugin.LicenseValidator
}

// NewPlugin 创建插件实例
func NewPlugin() *Plugin {
	return &Plugin{
		metadata: &plugin.PluginMetadata{
			Name:        "${PLUGIN_NAME}",
			Version:     "1.0.0",
			Author:      "Your Name",
			Description: "${PLUGIN_NAME} 策略插件",
			Type:        plugin.PluginTypeStrategy,
			License:     "free", // 或 "commercial"
			RequiresKey: false,  // 商业插件设置为 true
		},
		validator: plugin.NewLicenseValidator(),
	}
}

// GetMetadata 获取插件元数据
func (p *Plugin) GetMetadata() *plugin.PluginMetadata {
	return p.metadata
}

// Initialize 初始化插件
func (p *Plugin) Initialize(cfg *config.Config, params map[string]interface{}) error {
	// TODO: 实现初始化逻辑
	p.strategy = NewStrategy()
	return nil
}

// Validate 验证许可证
func (p *Plugin) Validate(licenseKey string) error {
	if !p.metadata.RequiresKey {
		return nil
	}
	return p.validator.ValidatePlugin(p.metadata.Name, licenseKey)
}

// GetStrategy 获取策略实例
func (p *Plugin) GetStrategy() strategy.Strategy {
	return p.strategy
}

// Close 关闭插件
func (p *Plugin) Close() error {
	if p.strategy != nil {
		return p.strategy.Stop()
	}
	return nil
}

// Strategy ${PLUGIN_NAME} 策略实现
type Strategy struct {
	name     string
	cfg      *config.Config
	executor position.OrderExecutorInterface
	exchange position.IExchange
}

// NewStrategy 创建策略实例
func NewStrategy() *Strategy {
	return &Strategy{
		name: "${PLUGIN_NAME}",
	}
}

func (s *Strategy) Name() string {
	return s.name
}

func (s *Strategy) Initialize(cfg *config.Config, executor position.OrderExecutorInterface, exchange position.IExchange) error {
	s.cfg = cfg
	s.executor = executor
	s.exchange = exchange
	// TODO: 实现策略初始化逻辑
	return nil
}

func (s *Strategy) OnPriceChange(price float64) error {
	// TODO: 实现价格变化处理逻辑
	return nil
}

func (s *Strategy) OnOrderUpdate(update *position.OrderUpdate) error {
	// TODO: 实现订单更新处理逻辑
	return nil
}

func (s *Strategy) GetPositions() []*strategy.Position {
	// TODO: 返回持仓信息
	return nil
}

func (s *Strategy) GetOrders() []*strategy.Order {
	// TODO: 返回订单信息
	return nil
}

func (s *Strategy) GetStatistics() *strategy.StrategyStatistics {
	// TODO: 返回策略统计
	return &strategy.StrategyStatistics{}
}

func (s *Strategy) Start(ctx context.Context) error {
	// TODO: 实现策略启动逻辑
	return nil
}

func (s *Strategy) Stop() error {
	// TODO: 实现策略停止逻辑
	return nil
}
EOF

# 生成测试文件
cat > "${PLUGIN_DIR}/plugin_test.go" << EOF
package ${PLUGIN_NAME}

import (
	"testing"
	"quantmesh/config"
)

func TestPlugin(t *testing.T) {
	plugin := NewPlugin()
	
	// 测试元数据
	metadata := plugin.GetMetadata()
	if metadata.Name != "${PLUGIN_NAME}" {
		t.Errorf("期望插件名称为 ${PLUGIN_NAME}, 实际为 %s", metadata.Name)
	}
	
	// 测试初始化
	cfg := &config.Config{}
	err := plugin.Initialize(cfg, nil)
	if err != nil {
		t.Errorf("插件初始化失败: %v", err)
	}
	
	// 测试策略
	strategy := plugin.GetStrategy()
	if strategy == nil {
		t.Error("策略实例为空")
	}
	
	if strategy.Name() != "${PLUGIN_NAME}" {
		t.Errorf("期望策略名称为 ${PLUGIN_NAME}, 实际为 %s", strategy.Name())
	}
}
EOF

# 生成 README
cat > "${PLUGIN_DIR}/README.md" << EOF
# ${PLUGIN_NAME} 插件

## 描述

${PLUGIN_NAME} 策略插件

## 安装

\`\`\`bash
# 将插件添加到主程序
cd quantmesh_market_maker
go mod edit -replace quantmesh/plugins/${PLUGIN_NAME}=./plugins/${PLUGIN_NAME}
go build
\`\`\`

## 使用

\`\`\`go
import "quantmesh/plugins/${PLUGIN_NAME}"

// 创建插件
plugin := ${PLUGIN_NAME}.NewPlugin()

// 加载插件
loader.LoadStrategyPlugin(
    plugin,
    "", // 许可证密钥 (免费插件留空)
    map[string]interface{}{
        "weight": 1.0,
    },
    strategyManager,
    executor,
    exchange,
)
\`\`\`

## 配置

\`\`\`yaml
plugins:
  plugins:
    - name: "${PLUGIN_NAME}"
      enabled: true
      license_key: ""
      params:
        weight: 1.0
\`\`\`

## 开发

\`\`\`bash
# 运行测试
cd plugins/${PLUGIN_NAME}
go test -v

# 构建
go build
\`\`\`

## 许可证

MIT / Commercial (根据实际情况修改)
EOF

# 生成 go.mod
cat > "${PLUGIN_DIR}/go.mod" << EOF
module quantmesh/plugins/${PLUGIN_NAME}

go 1.21

require quantmesh v0.0.0

replace quantmesh => ../../
EOF

echo "✅ 插件脚手架创建成功!"
echo ""
echo "📁 插件目录: ${PLUGIN_DIR}"
echo ""
echo "下一步:"
echo "1. 编辑 ${PLUGIN_DIR}/plugin.go 实现你的策略逻辑"
echo "2. 运行测试: cd ${PLUGIN_DIR} && go test -v"
echo "3. 在 main.go 中注册插件"
echo ""
echo "如需创建商业插件:"
echo "1. 修改 plugin.go 中的 License 为 'commercial'"
echo "2. 设置 RequiresKey 为 true"
echo "3. 使用 scripts/generate_license.sh 生成许可证"

