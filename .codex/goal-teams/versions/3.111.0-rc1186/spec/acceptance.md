# RC1186 验收标准

## 必须满足

1. 同一 durable source anomaly 被两个独立 SQLite handles 或 MySQL connections 使用不同 operation ID 并发调整，持久结果只能包含一个获胜 adjustment；输家返回冲突或有界、语义明确的可重试错误。
2. 原始 funding ledger fact 及原始/观察证据不可变；获胜 adjustment 为精确十进制、追加式、有 actor/audit/evidence 绑定。
3. 竞争失败方不得新增 adjustment ledger event、receipt/outbox，不得推进 funding source cursor，不得更新 anomaly 为错误状态，不得解除 recovery hold/Gate。
4. SQLite 测试驱动须先真实提交获胜事务，再故意向调用方返回不确定 commit error；`ApplyRecoveryOperation` 必须由独立 `commitProbe` 读回同一 receipt 并返回确认结果，不重复入账。SQLite/MySQL 普通重复 operation 也必须幂等；不同 operation 的重试不可以被误当成幂等成功。MySQL 不宣称其测试注入了 commit 响应丢失。
5. MySQL 测试在 GitHub Actions MySQL 8.0.36 强制 DSN 下实际运行并通过；fixture 必须显式验证本机 loopback、允许的临时 schema 和 destructive opt-in，隔离 schema 清理失败须报告。
6. 专项 race（`-race -count=20`）、storage 套件、带回环权限的 `go test -p=2 ./... -count=1 -timeout=30m`、`go vet ./...`、WebUI typecheck、318 项 Vitest 与 production build 已本地通过。MySQL 专项本机因缺 DSN 而 skip，不算通过；RC1186 同 SHA CI 与 release verification 尚待推送后实际执行。不能把 RC1185 的同 SHA 结果当作 RC1186 验收，也不能把 skipped/pending 写成 passed。
7. 版本、CHANGELOG 和必要的 Product Overview 对齐；提交、推送、CI、构建、发布各状态分开记录。

## 明确不属于验收

- 真实 Binance/账户访问、实盘下单、盈利/回撤验证。
- 自动订单重放、自动解除 Gate/hold，或对旧 funding facts 做原地更新/删除。
- 其他策略的账务垂直切片；后续按序继续，不混入本 RC。
