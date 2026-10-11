# RC1160 实施进度

## 当前状态

- 基线：`main` / `cc40271a` / `v3.111.0-rc1157`。工作区原有未提交材料均保留；本轮未提交、推送、打 tag 或发布。
- 本地版本已更新到 `3.111.0-rc1160`，变更含人工核账审计读取及 UI 刷新、MySQL 自动提现 claim 时间改为 UTC。
- MySQL 8.0.46：使用独立 `quantmesh_audit_rc1159` schema，`go test ./storage -count=1` 通过；claim 回收及 runtime-state CAS 的受限用例通过。
- Web 定向权限/路由 smoke 测试通过；前端全套 56 个测试文件、318 项通过，typecheck 通过；`go vet ./web ./storage` 通过。
- `go test -p 1 ./... -count=1` 使用仓库认可的回环 TCP `quantmesh_audit_*` DSN 串行通过全仓。此前错误 DSN 下的根包和 storage 失败已被正确配置重跑排除；storage 全包单独运行也通过。
- MySQL 曾暴露 claim 时间区源缺陷：主机 `SYSTEM` 时区为 UTC+8，旧实现写入本地 `CURRENT_TIMESTAMP`，回收使用 UTC cutoff。改为原生 UTC 时间参数后，`TestMySQLProfitWithdrawRules` 与 `TestMySQLStrategyRuntimeStateConditionalWritePreservesNewEvidence` 及全套 storage/全仓测试通过。

## 仍未闭合

- 所有生产策略的 durable cursor/CAS、费用完整性及人工幂等 reconcile adapter 尚未实现/接入。
- 核账服务启动装配、owner-scoped durable gate bridge 尚未完成；实际安全解锁仍不可用，故障保持封锁。
- 盈利与权益尚缺可独立复算的真实交易/账户账单证据；策略收益归因和导出过滤仍未验收。
- 同一最终提交的 GitHub CI、发布、真实环境验证均未发生；不宣称实盘或盈利已验收。
