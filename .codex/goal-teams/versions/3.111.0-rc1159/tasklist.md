# RC1159 任务清单

| ID | 负责人 | 状态 | 锁定范围 | 完成条件 | 验证/文档 |
|---|---|---|---|---|---|
| T01 | Goal Lead | completed | 只读当前 Git 状态 | 确认 main/HEAD、既有未提交内容、版本与 CI 失败根因 | 本会话命令记录 |
| T02 | 策略-账务核验 | completed_audit_only | 只读生产策略与调用链 | 已列出注册策略、缺失生产 adapter、成交费用字段不足和 owner-scoped gate 障碍；实施仍是 T05 | 独立策略审查已写入 `progress.md` 与 `spec/profit-evidence.md` |
| T03 | Goal Lead | completed_local | `storage/sql_bot_state.go` 与测试 | MySQL BotState 时间 round-trip 在本机 MySQL 8.0.46 专用新库通过；SQLite 回归通过；仍需同一已提交源码在 CI MySQL 8.0.36 复验 | `go test ./storage`（强制 DSN）通过 |
| T04 | 后端-审计解锁接线 | partial | 核账相关后端与持久层 | 已增加管理员 audit read endpoint；quarantine/case 与 owner-scoped durable Gate 生产装配及重启/崩溃状态机仍未完成 | API/DB 定向测试通过；需独立审查 |
| T05 | 策略-账务核验 | pending | 策略账本及专属适配器 | 逐策略建立可审计的读回与幂等缺口补账；不支持者持续封锁 | 策略 race 与重启测试 |
| T06 | 前端-核账操作台 | partial | 核账 UI/API service/locales | 已展示服务端耐久审计历史并完成英/简中文案；生产 API/策略/gate 尚不可用时必须保持失败说明，不能展示虚假成功 | 定向 service/UI/locale tests 通过，typecheck 通过 |
| T07 | 评审-审计盈利证据 | completed_audit_only | 只读财务路径 | 已指出字段/调用链缺口，定义证据报告契约并标记真实数据依赖；计算/导出实现仍待后续切片 | `spec/profit-evidence.md` |
| T08 | 验证-跨层验收 | partial | 测试文件及测试计划 | 当前 admin audit/DB failure/UI 与 MySQL claim 路径已覆盖；策略生产接线、owner/gate/fees/UNKNOWN 全链路仍缺 | RC1160 本地全仓 Go、MySQL、前端记录；须同一源码指纹 CI |
| T09 | 评审-安全复核 | partial | 只读最终 diff | 初次 Review 的审计缓存刷新与路由/错误分支覆盖已修；最终 diff 需独立复核，且全策略闭环仍未实现 | 初次 reviewer 结论待针对 RC1160 复核 |
| T10 | Goal Lead | pending | 最终文档与版本文件 | Changelog/概览/API 版本同步；CI、CD、push/release 状态有证据 | 最终状态矩阵 |

T04/T05/T06 可并行，但跨层成功路径必须等待策略 contract；T08 必须独立于实现成员。所有完成状态均绑定同一源码指纹。
