# RC1186 团队目标包

- 目标：闭合 RC1185 funding source anomaly append-only 人工调整的跨 storage owner 并发安全验收。
- 版本：`3.111.0-rc1186`（bugfix，仅递增 rc）。
- 当前证据：同 operation 双句柄并发已有覆盖；同一 anomaly 的不同 operation 竞争尚无直接测试；MySQL 强制 DSN 的全量发布验证已通过，但尚无这条具体 anomaly 调整竞争测试。
- 成功标准：SQLite 独立 handles 和 MySQL 独立 connections 对同一 anomaly 使用不同 operation 并发时，最多一个 adjustment 落账；原始事件不变；失败操作不能产生重复 event/receipt/outbox 投影、推进 cursor 或清除 hold；重试结果与持久化状态一致。
- 允许范围：FundingRecovery anomaly adjustment 存储事务/CAS 及针对性测试、版本/changelog/验收记录。
- 禁止范围：真实账户、真实下单、自动重放、自动解锁、改写/删除历史事实、提交未跟踪历史目录或无关用户改动。
- 必跑验证：SQLite 双句柄并发；MySQL 8 双连接真实竞争；相关 storage/race；Go 全套、vet、前端门禁和同一提交 CI/CD。
- 独立验证：测试和代码均由非作者审查；结果按未提交/已提交/已推送/CI/构建/发布分开记载。
- 停止条件：任何可能双记账、历史覆盖、Gate/hold 绕过、账户归属不匹配，或 MySQL fixture 不满足明确隔离要求。
