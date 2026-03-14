# 超大文件拆分检查报告

> 鲁迅说过：代码臃肿如肥宅，拆分如减肥，需循序渐进，不可一刀切。

## 一、现状概览

### Go 超大文件（按行数排序）

| 文件 | 行数 | 建议优先级 |
|------|------|------------|
| `web/api.go` | **8069** | 🔴 最高 |
| `storage/sqlite.go` | **5071** | 🔴 高 |
| `position/super_position_manager.go` | **4689** | 🟡 中 |
| `main.go` | **3611** | 🟡 中 |
| `config/config.go` | **2819** | 🟡 中 |

### 语言文件（i18n）

| 类型 | 行数/文件 | 文件数 |
|------|-----------|--------|
| `webui/src/i18n/locales/*.json` | 3892~4339 | 24 种语言 |
| 顶级 key 数量 | ~80 个模块 | common, botList, botCreate, ... |

---

## 二、Go 文件拆分建议（按成本从低到高）

### 1. `web/api.go`（8069 行）— 成本最低

**现状**：已有 `api_backtest.go`、`api_capital.go`、`api_bots.go` 等拆分，但主文件仍堆积大量 handler。

**建议拆分**（按功能域，与现有 `api_*.go` 模式一致）：

| 新文件 | 建议迁移内容 | 预估行数 |
|--------|--------------|----------|
| `api_status.go` | 系统状态、AITask、statuses、symbols、version、exchanges | ~800 |
| `api_positions.go` | getPositions、getPositionsSummary、getExchangePositionsSummary、getPositionsSummaryAll | ~1200 |
| `api_orders.go` | getOrders、syncOrders、getOrderHistory | ~400 |
| `api_fix.go` | FIX 会话、订单、logon/heartbeat/logout、new/cancel/replace | ~800 |
| `api_statistics.go` | getStatistics、getDailyStatistics、getTradeStatistics | ~600 |
| `api_system.go` | getSystemMetrics、getCurrentSystemMetrics、getDailySystemMetrics | ~150 |
| `api_close_positions.go` | 平仓相关（若尚未在 api_close_positions.go） | 已存在则跳过 |

**操作要点**：
- 共享类型（如 `SystemStatus`、`PositionSummary`）可放 `api_types.go` 或留在主文件
- 共享变量（如 `statusBySymbol`、`aiTaskManager`）需保留在 `api.go` 或抽到 `api_shared.go`
- 路由注册在 `server.go`，handler 只是函数引用，迁移后改 import 即可

---

### 2. `storage/sqlite.go`（5071 行）— 成本中等

**现状**：大量 `migrate*` 函数（约 30+ 个迁移）与核心 CRUD 混在一起。

**建议拆分**：

| 新文件 | 内容 | 预估行数 |
|--------|------|----------|
| `storage/sqlite_migrations.go` | 所有 `migrate*`、`createTables` | ~2500 |
| `storage/sqlite_trades.go` | trades 表相关 CRUD | ~600 |
| `storage/sqlite_orders.go` | orders 表相关 | ~400 |
| `storage/sqlite_*.go` | 其他表按业务域拆分 | 各 ~300 |

**操作要点**：
- `SQLiteStorage` 结构体保留在 `sqlite.go`
- 迁移函数可全部移到 `sqlite_migrations.go`，在 `NewStorage` 中调用
- 同一 package 内可互相调用，无需改接口

---

### 3. `position/super_position_manager.go`（4689 行）— 成本较高

**现状**：96 个方法都在 `SuperPositionManager` 上，是典型的「上帝类」。

**建议**：
- **短期**：按职责拆成多个文件，但保持同一 struct（Go 允许同一类型的方法分散在多个文件）
  - `super_position_manager.go`：核心结构体 + 构造函数
  - `super_position_orders.go`：下单、撤单、批量操作
  - `super_position_slots.go`：槽位管理
  - `super_position_pnl.go`：盈亏计算
- **长期**：考虑用接口 + 组合替代大 struct，但改动大，可后续迭代

---

### 4. `main.go`（3611 行）— 成本中等

**建议**：
- 将 `capitalDataSourceAdapter`、`buildBinanceConfigForBacktest` 等抽到 `main_adapters.go`
- 将各模块初始化逻辑抽到 `main_init.go` 或按模块拆成 `init_*.go`
- 保留 `main()` 和 `Version` 在 `main.go`

---

### 5. `config/config.go`（2819 行）— 成本中等

**建议**：按配置域拆分，如 `config_exchange.go`、`config_strategy.go`、`config_web.go` 等。

---

## 三、语言文件拆分建议

### 方案 A：构建时合并（**成本最低**，推荐）

**思路**：开发时按模块拆成多个 JSON，构建时合并成单文件，运行时无变化。

```
webui/src/i18n/locales/
├── zh-CN/
│   ├── common.json
│   ├── botList.json
│   ├── botCreate.json
│   ├── dashboard.json
│   └── ... (约 80 个)
├── en-US/
│   └── ...
└── merge-locales.js   # 构建时执行，输出 zh-CN.json 等
```

**优点**：
- 不改 `useTranslation()` 调用
- 不改 i18n config
- 仅需新增 merge 脚本 + 调整 `package.json` 的 build 前置步骤

**merge 脚本示例**（可基于现有 `merge-locales.js` 扩展）：

```javascript
// 遍历 locales/zh-CN/*.json，深度合并为一个对象，输出 zh-CN.json
```

---

### 方案 B：i18next namespace 按需加载（成本高）

**思路**：每个模块一个 namespace，`useTranslation(['common', 'botList'])` 按需加载。

**缺点**：
- 需修改所有 `useTranslation()` 调用，补充 namespace
- 需修改 `resourcesToBackend` 的加载逻辑
- 翻译 key 从 `t('botList.title')` 变为 `t('botList:title')` 或传 namespace

**适用**：首屏严格优化、语言包很大的场景，当前 4k 行尚可接受。

---

## 四、执行顺序建议

1. **先做**：`web/api.go` 拆分（收益大、风险小、模式已有）
2. **其次**：语言文件方案 A（构建时合并）
3. **再**：`storage/sqlite.go` 迁移函数拆分
4. **最后**：`position`、`main`、`config` 按需推进

---

## 五、验证清单

- [ ] `go build ./...` 通过
- [ ] `go test ./...` 通过
- [ ] 前端 `yarn build` 通过
- [ ] 语言切换、API 调用与拆分前行为一致
