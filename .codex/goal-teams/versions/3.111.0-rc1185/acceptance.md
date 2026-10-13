# RC1185 验收记录

| 项目 | 状态 | 证据/边界 |
|---|---|---|
| late/changed/deleted/摘要异常及原始/观察事实差额 | 通过 | 精确十进制分类；原始事件保持不可变；仅摘要异常不能伪造行差额 |
| durable capture、owner fencing、幂等和 held Gate | 通过（SQLite） | 捕获与当前代次、episode revision、cursor、hold 同事务校验；MySQL 实际运行未验收 |
| 管理员追加 adjustment event + receipt/outbox/CAS | 通过（生产 HTTP E2E） | 首次 projection 失败后同 operation retry；原 funding 行保留且 adjustment 仅写入一次 |
| 完整回扫核验 adjustment 后安全推进 cursor | 通过（生产 HTTP E2E） | 调整后仅完整双扫描匹配才与 cursor 原子推进并标记 verified；真实 Binance 来源未访问 |
| response-loss、重启、多进程并发、MySQL | 未完成 | SQLite 基础覆盖；尚缺独立 OS 进程、多进程竞态和真实 MySQL DSN 验收 |
| 全仓 Go、vet、WebUI 与目标 race 验证 | 通过 | `go test -p=2 ./... -count=1 -timeout=30m`（获准 loopback 环境）、`go vet ./...`、WebUI typecheck、56 files/318 Vitest tests、production build、生产 HTTP + storage anomaly race 均通过 |
| MySQL 实际迁移、独立多进程及同 SHA 远端 CI | 未完成 | 没有配置 MySQL DSN；未提交/推送，因此无远端同 SHA 结果 |

未提交、未推送、未发布；未访问真实账户或下单。
