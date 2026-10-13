# RC1186 测试计划

| 层级 | 场景 | 关键断言 | 环境 |
|---|---|---|---|
| SQLite 并发 | 两个独立 `SQLStorage` handles 同时提交相同 anomaly revision、不同 operation ID 和不同 adjustment event | 只一个成功；另一个冲突；原始事实与获胜调整读回唯一；cursor revision 不变；hold 仍 held | 临时独立 SQLite 文件、同步起跑屏障 |
| SQLite ambiguous commit | 注入驱动先真实提交获胜事务、随后返回 commit error，验证 `commitProbe` 从独立连接查回 receipt；之后再正常重复原 operation | commitProbe 返回同 receipt/replay 结果；ledger adjustment 仍恰一条；不增加 anomaly/episode revision | 上述临时数据库 + 独立 commitProbe pool |
| MySQL 8 并发 | 两个独立 DB pools/connections 对同一 durable anomaly 提交不同 operation | 最多一个提交；输家 CAS conflict；不存在双账；cursor/hold 安全 | CI MySQL 8.0.36、受限 DSN、每测试独立 disposable schema |
| MySQL 重试 | 获胜 operation 正常提交后以原 operation 重试，并重试输家 operation | 原 operation 幂等读回；输家仍是 CAS conflict，不得接管或重复记账 | 同上；不把普通 retry 描述为 commit response-loss |
| 负向安全 | 旧 generation、错账户 scope、过期 anomaly revision、缺审计证据、cursor/episode revision 不匹配 | 写入拒绝且 durable state 无变化 | SQLite 与 MySQL 至少覆盖主要冲突边界 |
| 回归门禁 | `go test -race` 专项、`go test ./storage`、Go 全套/vet、frontend 和同 SHA 发布验证 | 所有硬门禁成功，保留完整 run/commit SHA 证据 | 本地 + GitHub Actions |

如果数据库本地不可用，必须保留测试但标记未验收；由强制配置 MySQL 的 CI 提供实际 MySQL 证据。不能以 SQL 字符串/DDL 嵌入测试替代运行时并发验证。
