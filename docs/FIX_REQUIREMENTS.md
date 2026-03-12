# FIX 协议支持需求与实现状态

## 一、需求背景
为专业机构提供 FIX（Financial Information eXchange）协议接入能力，支持单会话单 Bot 绑定、订单路由与执行回报。

## 二、已完成（3.76.0-rc1 ~ rc3）

### 2.1 持久化与存储
- `fix_session_states` 表：会话状态、序号、bot_id、心跳时间
- `fix_order_links` 表：ClOrdID 与内部订单映射
- Storage 接口：Upsert/Get/List 会话与订单

### 2.2 REST API（Acceptor 侧）
| 方法 | 路径 | 说明 |
|------|------|------|
| GET | /api/fix/sessions | 会话列表 |
| GET | /api/fix/orders | 订单映射列表 |
| POST | /api/fix/sessions/logon | 登录并绑定 bot_id |
| POST | /api/fix/sessions/heartbeat | 心跳续活 |
| POST | /api/fix/orders/new | 新单 |
| POST | /api/fix/orders/cancel | 撤单 |
| POST | /api/fix/orders/replace | 改单 |

### 2.3 会话可靠性
- BotID 持久化到存储，进程重启可恢复
- 心跳超时可配置（`config.fix.heartbeat_timeout_sec`，預設 120 秒）
- reset_seq_num_flg 支持序号重置

### 2.4 可观测
- 审计日志（logon/order/timeout）
- Prometheus 指标：fix_session_logon_total、fix_order_total、fix_session_timeout_total

## 三、待办（按优先级）

### P1 - 管理能力（已完成 3.76.0-rc4）
- [x] POST /api/fix/sessions/logout 主动登出
- [x] Web UI FIX 管理页：会话列表、订单列表、登出操作（`/fix`）

### P2 - 配置化（已完成 3.76.0-rc5）
- [x] 心跳超时可配置（config.fix.heartbeat_timeout_sec，預設 120）
- [x] FIX 开关（config.fix.enabled，預設 true，关闭时所有 FIX API 返回 503）

### P3 - 协议层（可选）
- [ ] FIX Tag=Value 报文解析
- [ ] TCP/Socket Acceptor 监听
- [ ] Initiator 模式（主动连接机构）

### P4 - 高级
- [ ] Gap Fill 序号断档恢复
- [ ] 多 Bot 路由（一主单多子单）

## 四、架构约束
- 单会话单 Bot：每个 FIX 会话绑定一个 bot_id，订单路由到该 Bot 的交易所实例
- 序号策略：登录时可重置，订单/心跳推进 sender/target 序号
