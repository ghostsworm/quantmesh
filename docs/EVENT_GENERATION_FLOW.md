# 事件中心：事件如何产生与存储

## 一、事件产生流程

```
业务代码 → eventBus.Publish(event) → 两个订阅者
                                       ├─ EventCenter  → 写入 database (完整字段)
                                       └─ Storage 服务 → 写入 storage (仅 event_type + data)
```

### 1. 事件发布入口（谁在发事件）

以下模块会调用 `eventBus.Publish(&event.Event{...})` 发布事件：

| 模块 | 事件类型示例 | 文件位置 |
|------|-------------|----------|
| **main.go** | `trading_start_failed`, `order_placed`, `order_filled` 等 | main.go:358, 392, 439, 571, 588, 605, 628, 1871, 2024, 2306, 2374 |
| **symbol_manager.go** | WebSocket 断连/重连、API 限流等 | symbol_manager.go:366, 617, 630 |
| **position/super_position_manager.go** | 持仓开平、风控触发等 | super_position_manager.go:862, 1021, 1170, 1216 |
| **strategy/** | 订单成交、止损止盈等 | martingale.go, dca_enhanced.go |
| **position/** | 仓位计划达成、资金分配超限等 | plan_manager.go, allocation_manager.go |
| **智子巡检 (inspector)** | `inspector_report` | main.go:1871 |

### 2. 事件订阅者（谁在收事件）

**EventBus** 有两个订阅者：

1. **EventCenter** (`event/center.go`)
   - 订阅 EventBus
   - 对事件做富化：从 `event.Type` 推导 `severity`、`source`、`title`，并构建 `message`
   - 写入 **database** (Gorm)，表 `events`，字段：`type`, `severity`, `source`, `exchange`, `symbol`, `title`, `message`, `details`, `created_at`

2. **Storage 事件 Worker** (`main.go:1155-1175`)
   - 订阅 EventBus
   - 调用 `storageService.Save(string(e.Type), e.Data)`
   - 最终写入 **storage** 的 `events` 表，字段：`event_type`, `data`, `created_at`

### 3. 关键问题：同一张表，两套写入方式

配置中 `storage.path` 和 `database.dsn` 都指向 `./data/quantmesh.db`，即 **同一 SQLite 文件**。

- **EventCenter** 通过 Gorm 写入：`type`, `severity`, `source`, `title`, `message`, `details`
- **Storage** 通过原始 SQL 写入：`event_type`, `data`

Gorm 的 `EventRecord` 映射到列：`type`, `severity`, `source`, ...  
Storage 写入时只用 `event_type`, `data`，不填充 `type`, `severity`, `source`, `title`, `message`。

因此：
- 由 **EventCenter** 写入的事件：有完整 `type`、`source`、`title`、`message`
- 由 **Storage** 写入的事件：这些字段为空，Web UI 显示为“事件来源/类型/标题/消息”空白，“暂无消息内容”

## 二、线上诊断命令

SSH 到 `facev.app` 后，在 `/opt/quantmesh` 下执行：

```bash
# 1. 查看 events 表结构
sqlite3 data/quantmesh.db ".schema events"

# 2. 查看最近几条事件的原始数据（区分两种写入方式）
sqlite3 data/quantmesh.db "
SELECT id, type, severity, source, title, 
       CASE WHEN message = '' OR message IS NULL THEN '(空)' ELSE substr(message,1,40) END as msg,
       event_type, 
       CASE WHEN data IS NULL OR data = '' THEN '(空)' ELSE substr(data,1,60) END as data_preview,
       created_at 
FROM events 
ORDER BY id DESC 
LIMIT 10;
"

# 3. 统计：哪些事件 type 为空（来自 Storage 写入）
sqlite3 data/quantmesh.db "
SELECT 
  CASE WHEN type IS NULL OR type = '' THEN '来自Storage(空type)' ELSE '来自EventCenter' END as source_type,
  COUNT(*) 
FROM events 
GROUP BY 1;
"

# 4. 若 event_type 列存在，看 Storage 写入的事件类型分布
sqlite3 data/quantmesh.db "
SELECT event_type, COUNT(*) 
FROM events 
WHERE (type IS NULL OR type = '') AND event_type IS NOT NULL 
GROUP BY event_type 
ORDER BY 2 DESC 
LIMIT 20;
"
```

## 三、事件类型与元数据映射

`event/event.go` 中定义了事件类型及对应的 severity、source、title：

- `GetEventSeverity(eventType)` → critical / warning / info
- `GetEventSource(eventType)` → exchange / network / system / strategy / risk / api
- `GetEventTitle(eventType)` → 如「訂單已下單」「WebSocket 断开连接」等

只有通过 **EventCenter** 写入的事件才会自动填充这些字段。

## 四、排查“空字段”事件来源

若事件详情里 `事件来源`、`事件类型`、`事件标题`、`事件消息` 为空：

1. 该条记录很可能是 **Storage** 写入的
2. 在数据库中查看该事件的 `event_type` 和 `data` 列，通常有内容
3. `data` 为 JSON，可解析出原始信息

示例（假设事件 ID 为 6844）：

```bash
sqlite3 data/quantmesh.db "SELECT id, event_type, data, created_at FROM events WHERE id=6844;"
```

## 五、可选修复方向

若希望所有事件在 UI 上都显示完整信息，可以考虑：

1. **停用 Storage 对 events 的写入**：在 Storage 的批量处理逻辑中，对 `order_placed`、`position_opened` 等继续落库，但对通用事件不再写入 `events` 表，只由 EventCenter 写入。
2. **统一由 EventCenter 写入**：确认 EventCenter 已启动（`event_center.enabled: true`），且 `database` 配置正确。
3. **兼容读取旧数据**：在查询事件时，若 `type` 为空但 `event_type` 有值，可从 `event_type` 和 `data` 解析并回填到 `type`、`title`、`message` 等字段再返回给前端。

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
