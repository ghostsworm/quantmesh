# 配置示例（YAML）

运行时主配置权威在数据库 **`app_config`**；本目录下的 YAML 仅作**模板、一次性导入或场景参考**，不必放在仓库根目录。

| 文件 | 说明 |
|------|------|
| `config.example.yaml` | 主模板，迁移：`./quantmesh --migrate-app-config <路径>` |
| `config.example.advanced.yaml` | 高级选项示例 |
| `config.minimal.yaml` | 简化版（亦可由程序生成至本路径） |
| `config-ha-example.yaml` | 高可用示例 |
| `config-hybrid-dual-grid-example.yaml` / `config-hybrid-trend-grid-example.yaml` | 混合策略示例 |
| `config-mysql8-example.yaml` | MySQL 8 示例 |
| `config-volatility-detection-example.yaml` / `config-volatility-pause-example.yaml` | 波动检测/暂停示例 |
| `config.funding-rate-test.yaml` | 资金费率测试 |
| `config.webauthn-fix.yaml` | WebAuthn RPID 等修复场景 |

详见 [配置与数据库设计](../config-database-design.md)。
