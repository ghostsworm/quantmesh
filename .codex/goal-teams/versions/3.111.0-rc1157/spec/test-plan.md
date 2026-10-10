# 测试计划：人工核账与精确解锁

| 类别 | 场景 | 必须断言 |
|---|---|---|
| 权限 | 匿名、普通用户、local-dev 管理员、正常管理员 | 仅正常管理员可读写；其他请求无状态副作用 |
| 身份/归属 | 同一订单 ID 不同 scope/CID/strategy/Bot、重复交易所 ID | 不匹配和歧义均拒绝，原 quarantine 不变 |
| 核账证据 | venue UNKNOWN、fills/fees 缺页、策略账不可读/不匹配、缺少理由/证据 | 不进入已核账态、不释放任何 gate |
| 策略 cursor | 每种生产注册策略：当前 cursor、cursor 落后、等于目标、超前、revision 冲突、损坏或缺失耐久状态 | 只有落后且可精确界定的遗漏允许人工 reconcile；等于目标为只读成功；其余 fail-closed |
| 策略幂等 reconcile | 同一 operation ID 重试、存储成功但响应丢失、调用超时后重启、相同 cursor 重复请求 | 不重放原始 callback、不重复经济记账；可通过 durable operation receipt 和读回证明结果 |
| 策略重启 | 每个策略已持久游标/经济结果保存，实例销毁后重建，再读回及 reconcile | 重建实例的读回结果与持久 revision/cursor 一致；缺快照/解码错误不开放 gate |
| 操作持久化 | SQL 故障、提交回执未知、进程重启 | 审计记录可读；不确定结果通过精确读回幂等恢复；没有证据不开放 |
| 幂等/并发 | 相同 operation key 重放、不同 key 争用相同 order/revision | 只产生一个状态转换；冲突被拒绝 |
| 解锁范围 | 目标订单 quarantine + manual/global/bootstrap 其他源 | 只解除明确 owner 的订单 hold，其他来源仍阻断 |
| 生产接线 | 实际 StartOrderStream callback → quarantine → API reconcile/confirm → runtime gate | 真实注册 callback 可观察；无 helper-only 假阳性 |
| 保护性减仓 | quarantine 时调用 reduction-only close 的实际 runtime 路径 | 明确记录允许/拒绝及其既有风险契约，不回归扩大开仓权限 |
| 前端 | 三语言 locale、必填验证、双步骤确认、失败反馈 | 不存在未翻译硬编码文案；状态由服务端回读；不可盲目重复操作 |

所有测试使用 fake exchange 与隔离 SQLite；如使用 MySQL，明确隔离容器/fixture 和执行结果。禁止真实账户或订单。
