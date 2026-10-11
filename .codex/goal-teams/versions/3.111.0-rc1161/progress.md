# RC1161 实施进度

## 基线

- `210f2ba34daa464bef9cd9858228015277ab0041` 已提交并推送到 GitHub `main`；CI #801 对同一 SHA 总体 success。
- Test job：MySQL 8.0.36 服务健康；前端、Go 全仓、vet、契约脚本和 `--require-mysql` race gate 均成功。race JSON 摘要为 `pass=2373`、`missing_verified_mysql_cases=[]`。
- Build job：Linux amd64 成功；artifact `quantmesh-linux-amd64` 与 race evidence artifact 均由该 SHA 产生。
- 当前开发分支：`fix/order-reconciliation-production-wiring-rc1161`，从已验证 `main` 建立。

## 本轮状态

| 项目 | 状态 | 证据/下一步 |
|---|---|---|
| 生产核账服务注入 | 已接线、局部验收 | `main.go` 在 StorageService 已成功迁移 schema、SymbolManager/BotManager 建成后注入 SQL repository 与只读 evidence；装配失败清空服务。Web listener 先启动，注册前短暂 503 fail-closed |
| SQLite 启动与重启读回 | 本地通过 | `StorageService` 首次迁移后写入 case/audit，Stop 后用同一隔离 SQLite 文件重新创建 StorageService，再由 builder 读回记录 |
| Admin HTTP 与缺 adapter 封锁 | 本地通过（测试分层） | 已持久 case 通过 Admin 测试路由读取；Accounting/Gate 缺失时 reconcile/release 均 503，revision/status/audit 不变。仍缺同一条 main 注入 seam 到真实受保护 HTTP router 的单一端到端测试 |
| 全仓本地检查 | 通过（有明确时序） | 完整 `go test ./... -p 1 -count=1` 与 `go vet ./...` 通过；随后新增 StorageService restart 与 persisted-case HTTP fail-closed 测试，最终 `go test . ./web -run OrderReconciliation -count=1` 及其 `-race` 版本通过。前端 `yarn verify`：56 文件 / 318 测试、Vite build 成功；`make build OUTPUT=/tmp/quantmesh-rc1161` 成功（本地 77 MB binary） |
| MySQL 专项 | 待同提交 CI | 本机未配置 `QUANTMESH_MYSQL_TEST_DSN`；RC1160 同 SHA 的 CI MySQL 8.0.36 已通过，但不能替代 RC1161 同 SHA 验证 |
| 策略账务 adapter | 未完成 | 逐策略 durable cursor、CAS、费用边界与重启恢复需独立盘点 |
| owner-specific Gate bridge | 未完成 | 没有已验证 bridge，不可 release |
| 盈利证据 | 未完成 | 无真实交易与独立账户账单，不得宣称盈利验收 |
| RC1161 提交/远端 CI | 未发生 | 实作与本地验证结束后再决定交付；当前不打 tag、不发布 |

## 独立复核发现

- QA：真实启动 composition 的单一跨层验收仍不完整；当前主装配测试与 Admin HTTP/fail-closed 测试分开，未来应抽出可被 main 与测试共用的启动安装 seam，断言真实路由/持久 case/审计/状态不变。
- 安全复核：Web listener 在 service 注册之前启动，期间 API 暂时 503（fail-closed，但可用性有启动窗口）；旧 schema 迁移的“查列后 ALTER”存在 SQLite/MySQL 多进程并发首次升级竞态，需独立迁移安全处理与并发测试。
- 未发现已接通的生产补账或 Gate 解锁路径；但以上限制未解除前，不将 RC1161 标记为人工核账闭环完成。

## T02 策略账务能力盘点（只读）

独立策略盘点成员按实际生产注册/装配路径核对 14 个策略。所有策略均未实现 `accounting.OrderAccounting` 的逐订单 durable readback、CAS reconcile 及耐久回执，因此当前逐策略结论全部为 `unsupported / fail-closed`。这不代表所有策略完全没有恢复机制：Spot Short、Funding Carry、Funding Perp Spread 等存在专用运行态恢复或 UNKNOWN 封锁，但这些专用状态不等于人工核账订单游标。

主要来源：`symbol_manager.go` 常规策略注册（Grid、Trend、Mean Reversion、Momentum、Martingale、DCA、DCA Enhanced、Combo、Spot Short、Spot Long、Futures Short、Futures Long）；`funding_carry_runtime.go`；`funding_perp_spread_runtime.go`。接口定义在 `accounting/order_reconciliation.go`。全仓生产代码搜索只找到接口与 Web 调用，没有策略 adapter。

费用证据仍存在明确安全边界：`main_order_reconciliation_evidence.go` 会拒绝未能证明的零费用和基础币费用，因为 `exchange.OrderFill`/核账 Fill 合约不能无损表达这些情况。此时维持封锁；后续须增补有资产/数量/精度的费用契约及逐策略测试，而不是放宽校验。
