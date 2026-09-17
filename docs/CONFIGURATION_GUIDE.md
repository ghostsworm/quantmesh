# 配置指南

本文档说明如何配置 QuantMesh，包括单机模式和多实例模式。

## 配置模式

### 单机模式（默认）

**特点：**
- ✅ 简单易用，开箱即用
- ✅ 无需额外服务（Redis、PostgreSQL）
- ✅ 使用 SQLite 本地数据库
- ✅ 不启用分布式锁（零开销）
- ✅ 适合开发环境和小规模交易

**配置示例：**

```yaml
# 实例配置（单机模式，可省略）
instance:
    id: "default-instance"
    index: 0
    total: 1

# 数据库配置（单机模式）
database:
    type: "sqlite"                    # 使用 SQLite
    dsn: "./data/quantmesh.db"        # 本地文件
    max_open_conns: 100
    max_idle_conns: 10
    conn_max_lifetime: 3600
    log_level: "error"

# 分布式锁配置（单机模式）
distributed_lock:
    enabled: false                    # 不启用分布式锁
```

### 多实例模式（高可用）

**特点：**
- ✅ 高可用（99.9%+）
- ✅ 水平扩展，性能提升
- ✅ 使用 PostgreSQL/MySQL 共享数据库
- ✅ 使用 Redis 分布式锁
- ✅ 适合生产环境和大规模交易

**配置示例：**

```yaml
# 实例配置（多实例模式）
instance:
    id: "instance-1"                  # 每个实例唯一
    index: 0                          # 实例索引（0, 1, 2...）
    total: 3                          # 总实例数

# 数据库配置（多实例模式）
database:
    type: "postgres"                  # 使用 PostgreSQL
    dsn: "host=localhost user=quantmesh password=secret dbname=quantmesh port=5432 sslmode=disable"
    max_open_conns: 100
    max_idle_conns: 10
    conn_max_lifetime: 3600
    log_level: "error"

# 分布式锁配置（多实例模式）
distributed_lock:
    enabled: true                     # 启用分布式锁
    type: "redis"
    prefix: "quantmesh:lock:"
    default_ttl: 5
    redis:
        addr: "localhost:6379"
        password: ""
        db: 0
        pool_size: 10
```

## 配置项说明

### 实例配置 (instance)

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `id` | string | "default-instance" | 实例唯一标识 |
| `index` | int | 0 | 实例索引，用于交易对分配 |
| `total` | int | 1 | 总实例数 |

### 数据库配置 (database)

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `type` | string | "sqlite" | 数据库类型：sqlite, postgres, mysql |
| `dsn` | string | "./data/quantmesh.db" | 数据源名称 |
| `max_open_conns` | int | 100 | 最大打开连接数 |
| `max_idle_conns` | int | 10 | 最大空闲连接数 |
| `conn_max_lifetime` | int | 3600 | 连接最大生命周期（秒） |
| `log_level` | string | "error" | 日志级别：silent, error, warn, info |

#### DSN 格式

**SQLite:**
```yaml
dsn: "./data/quantmesh.db"
```

**PostgreSQL:**
```yaml
dsn: "host=localhost user=quantmesh password=secret dbname=quantmesh port=5432 sslmode=disable"
```

**MySQL:**
```yaml
dsn: "quantmesh:secret@tcp(localhost:3306)/quantmesh?charset=utf8mb4&parseTime=True&loc=Local"
```

### 分布式锁配置 (distributed_lock)

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `enabled` | bool | false | 是否启用分布式锁 |
| `type` | string | "redis" | 锁类型：redis, etcd, database |
| `prefix` | string | "quantmesh:lock:" | 锁键前缀 |
| `default_ttl` | int | 5 | 默认锁过期时间（秒） |

#### Redis 配置

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `addr` | string | "localhost:6379" | Redis 地址 |
| `password` | string | "" | Redis 密码 |
| `db` | int | 0 | Redis 数据库 |
| `pool_size` | int | 10 | 连接池大小 |

### 交易對進階配置（網格 3.53+）

以下為每個交易對（`trading.symbols[]`）可選的進階參數，用於價格範圍、觸發價、網格模式、上移/下移與終止行為；Web 配置頁「交易對參數」與「網格風控」區塊可編輯。

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `price_low` | float64 | 0 | 網格價格下限（0=不限制）；軟限制：超出時暫停新開倉，保留平倉單 |
| `price_high` | float64 | 0 | 網格價格上限（0=不限制） |
| `trigger_price` | float64 | 0 | 觸發價格（0=立即啟動）；做多：當前價≤觸發價才啟動；做空：當前價≥觸發價才啟動 |
| `grid_mode` | string | "arithmetic" | 網格模式：`arithmetic` 等差、`geometric` 等比（等比時 `price_interval` 為比例，如 0.005=0.5%） |
| `grid_shift_step` | float64 | 同 price_interval | 網格上移/下移步長（供 API 或按鈕使用） |
| `close_on_stop` | bool | false | Bot 停止時是否平倉（總開關）。未配置 `close_on_stop_config` 時撤銷開倉單並按槽位全平（與以往一致） |
| `close_on_stop_config` | object | 未配置 | 僅在 `close_on_stop: true` 時生效：`method`（`market`/`limit`，配置本段時必填）、`price_offset`（限價偏移百分比）、`timeout_sec`、`auto_retry`、`max_retries`、`quantity_ratio`（0 或 1=全倉）。停止時先撤銷本交易對所有掛單，再按實際持倉下 reduce-only 平倉單；下單失敗且為全倉時回退按槽位全平，部分平倉失敗只記錄錯誤。限價單的超時處理在後台進行，不阻塞停止：到 `timeout_sec` 仍未成交且 `auto_retry: true` 時撤單並按剩餘數量改市價單（最多一次）；未開 `auto_retry` 的限價單超時後會留在交易所。`direction: BOTH` 仍按槽位腿別全平（`method` 不適用，也不允許部分平倉） |
| `slot_filter` | object | 未配置 | 槽位過濾，Bot 啟動時在首輪掛單前應用，運行中仍可通過 `/api/bots/:id/slot-filter` 修改（修改不寫回配置）。`rules[]`：`type`（`exclude`/`include`）、`prices`（精確價位）或 `min_price`+`max_price`（兩端必須同時設置）、`reason` |
| `auto_rebuild` | object | 關閉 | 網格自動重建：價格偏離錨點超過 `price_deviation_layers` 層，或開倉單中 `expired_order_ratio` 比例超過 `order_expire_minutes` 未成交時，撤銷開倉單並把錨點移到現價。其他參數：`check_interval_minutes`、`max_rebuilds_per_hour`、`min_rebuild_interval`（分鐘）；填 0 使用默認值（5/5/30/0.7/2/15）。`rebuild_mode` 只接受 `smart`/`always`，目前兩者行為相同；`require_trend_confirm` 尚未實現，設為 true 會拒絕啟動。非網格多策略模式下不啟動；Bot 停止時隨之停止 |
| `grid_risk_control` | object | — | 網格風控：enabled、stop_loss_ratio、stop_loss_basis（`position` 預設=浮虧/持倉名義價值；`equity`=浮虧/帳戶權益，權益緩存約 10s 後台刷新，暫無數據時回退 position）、take_profit_trigger_ratio、trailing_take_profit_ratio、max_grid_layers、max_open_orders_at_cap、trend_filter_enabled；詳見 [GRID_STRATEGY_ADVANCED_FEATURES.md](GRID_STRATEGY_ADVANCED_FEATURES.md) 與 [RISK_CONTROL_GUIDE.md](RISK_CONTROL_GUIDE.md) |

> `close_on_stop_config`、`slot_filter`、`auto_rebuild` 在 Bot 配置文件（`bots/*.yaml` / `bot_configs`）中分別位於 `advanced.close_on_stop_config`、`advanced.slot_filter`、`grid.auto_rebuild`。三者在 Bot 啟動時校驗，非法時該 Bot 拒絕啟動並給出字段名；運行中熱更新配置不會重新應用這三項，需重啟 Bot。

示例（單一交易對含進階參數與網格風控）：

```yaml
trading:
  symbols:
    - exchange: binance
      symbol: ETHUSDT
      price_interval: 2
      order_quantity: 30
      price_low: 2000
      price_high: 4000
      trigger_price: 3500
      grid_mode: geometric
      grid_shift_step: 2
      close_on_stop: true
      close_on_stop_config:
        method: limit
        price_offset: -0.1
        timeout_sec: 30
        auto_retry: true
        max_retries: 2
      slot_filter:
        rules:
          - type: exclude
            min_price: 3000
            max_price: 3100
            reason: 避開密集成交區
      auto_rebuild:
        enabled: true
        check_interval_minutes: 5
        price_deviation_layers: 5
      grid_risk_control:
        enabled: true
        stop_loss_ratio: 0.1
        take_profit_trigger_ratio: 0.08
        trailing_take_profit_ratio: 0.03
        max_grid_layers: 20
        max_open_orders_at_cap: 0
        trend_filter_enabled: true
```

### 手續費感知利差與 PostOnly 重定價（全局 `trading` 段）

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `fee_aware_spread.enabled` | bool | true（未配置即啟用） | 平倉利差取 `max(profit_spread, 開倉基準價 × (2×費率 + safety_margin_ratio))`。網格單一律 PostOnly，按 maker 費率計算；費率在 Bot 啟動時從交易所接口拉取（受 `timing.skip_exchange_fee_on_bot_start` 控制，目前支持 Binance/Bitget 合約），失敗或現貨時回退 `exchanges.<name>.fee_rate`（maker 保守取同值），並按 `timing.fee_rate_refresh_minutes` 定期刷新。配置利差低於下界時自動抬高，每個 Bot 只告警一次 |
| `fee_aware_spread.safety_margin_ratio` | float64 | 0.0002 | 安全邊際（價格比例，0.0002=0.02%）；<=0 使用預設值 |
| `post_only_reprice_max_attempts` | int | 3 | PostOnly 被拒（如 Binance -5022）時往遠離盤口方向移一個 tick 重掛的最大次數；超過後本輪放棄，下一輪重新計算。**永不降級為 GTC 吃單**。平倉單被交易所撤銷/過期後，下次掛單價至少在現價外 `(1+連續被拒次數)` 個 tick（封頂此值） |

```yaml
trading:
  fee_aware_spread:
    enabled: true
    safety_margin_ratio: 0.0002
  post_only_reprice_max_attempts: 3
```

> 行為說明：`AdjustOrders` 價格推送去抖——價格在 0.1×網格間距的分桶內移動、且無訂單/成交事件時跳過全量重算（至少每 1s 全量一次）；網格風控（止損、回撤止盈、關閉條件）仍每個 tick 檢查。下單路徑的帳戶信息緩存 5s，保證金不足時立即失效。全平倉（止損/熔斷）優先按盤口可成交價下 reduce-only 限價單（覆蓋持倉數量所需的買盤/賣盤檔位，讓價不超過現價±1%），無盤口數據時回退現價±1%。

### K 線 regime 過濾、自適應間隔、邊界冻结、庫存偏斜、資金費定價（全部默認關閉）

設計見 `docs/decisions/2026-09-17-kline-regime-filter.md` 與 `docs/decisions/2026-09-17-grid-regime-wiring.md`。默認讀取全局 `trading.*` / `funding_rate.*`，所有 Bot 共用；單個 Bot 可用 `trading_overrides` 覆蓋，見下文「按 Bot 覆蓋」。

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `trading.regime_filter.enabled` | bool | false | 基於已收盤 K 線（ADX + EMA 斜率，滯回 + 駐留確認）識別 range / trend_up / trend_down。逆勢腿開倉窗口 ×0.5（向下取整，至少 1 檔）、間隔 ×1.5（量化到 base 整數倍）；順勢腿冻结邊界（LONG 冻结上沿、SHORT 冻结下沿，只保留平倉單，不追價重建倉）；range 滿鋪。**數據未就緒或過期（Unknown）時保持原有行為**。啟用後舊 `grid_risk_control.trend_filter_enabled`（50ms tick 均線）不再參與判斷，資金費-趨勢聯動改用 regime 趨勢 |
| `trading.regime_filter.kline_interval` 等 | — | 見 ADR 默認值表 | `kline_interval`(1h)、`adx_period`(14)、`adx_enter_threshold`/`adx_exit_threshold`(25/20)、`ema_period`(50)、`ema_slope_lookback`(5)、`ema_slope_min_atr`(0.05)、`min_dwell_bars`(3)、`atr_period`(14)、`atr_percentile_lookback`(100)、`bootstrap_bars`(500)、`poll_interval_seconds`(0=自動)、`stale_multiplier`(2)。配置非法時 Bot 拒絕啟動。以上參數同樣作用於下面兩項所需的 K 線檢測器 |
| `trading.adaptive_interval.enabled` | bool | false | `interval = QuantizeInterval(Next(ATR) × regime 間隔倍數, base)`，base = 啟動時的 `price_interval`；只在新收盤 K 線或狀態變化時計算（檢查周期 30s）。新間隔恆為 base 整數倍，槽位仍對齊錨點。`profit_spread > 0` 時按同倍數縮放。等比網格/三級火箭網格不支持（跳過並告警）；與 `dynamic_adjustment.price_interval` 同時啟用時本項自動停用。外部熱更新 `price_interval` 後以新值為基準 |
| `trading.adaptive_interval.atr_multiplier` | float64 | 0.5 | k：目標間隔 = k × ATR |
| `trading.adaptive_interval.min_interval` / `max_interval` | float64 | 0 → base / 8×base | 價格單位 |
| `trading.adaptive_interval.change_threshold_ratio` | float64 | 0.25 | 相對變化不足時不切換 |
| `trading.upper_bound_freeze.enabled` | bool | false | ATR 自動邊界：做多腿自動 `price_high = EMA + k×ATR`，做空腿自動 `price_low = EMA − k×ATR`，與手動 `price_high`/`price_low` 取更嚴格者，同為軟限制（越界暫停開倉、槽位裁剪、平倉照常）。快照過期時不生效。不依賴 `regime_filter.enabled` |
| `trading.upper_bound_freeze.atr_multiplier` | float64 | 3 | k |
| `trading.inventory_skew.enabled` | bool | false | `inv = 已成交層數 / 最大層數`（最大層數依次取 `bot_risk_control.max_position_layers`、`open_position_control.max_position_layers`、啟用時的 `grid_risk_control.max_grid_layers`；均未配置則不生效並告警一次）。開倉窗口 ×(1−inv×s)（≤0 停止開倉），開倉價再遠離現價 inv×s×間隔，平倉利差 ×(1−0.5×inv×s) 但不低於手續費下界。BOTH 模式按多/空腿分別計算 |
| `trading.inventory_skew.strength` | float64 | 0.5 | s，範圍 0..1（配置 >1 校驗失敗；<=0 使用默認值） |
| `funding_rate.pricing_enabled` | bool | false | 付費一側（LONG 遇正費率 / SHORT 遇負費率）開倉價遠離、平倉價外移 `|rate| × price × 距結算小時 / 8`，封頂 0.5×間隔；開倉總外移（含庫存偏斜）封頂 0.9×間隔。收費一側不移動。需 `funding_rate.enabled`（僅合約）。下次結算時間未知時按 8 小時計（監控器按 UTC 00/08/16 估算） |
| `funding_rate.pre_settlement_pause_minutes` | int | 0 | >0 時結算前 N 分鐘暫停付費一側的新開倉 |

```yaml
trading:
  regime_filter:
    enabled: true
    kline_interval: 1h
  adaptive_interval:
    enabled: true
    atr_multiplier: 0.5
  upper_bound_freeze:
    enabled: false
    atr_multiplier: 3
  inventory_skew:
    enabled: true
    strength: 0.5
  open_position_control:
    max_position_layers: 8
funding_rate:
  enabled: true
  pricing_enabled: true
  pre_settlement_pause_minutes: 10
```

> 提示：以上參數尚無與實盤同構的回測可驗證（見審計第五節第 9 條），建議先小倉位觀察。

### 按 Bot 覆蓋（`trading_overrides`）

網格 Bot 可在自身配置里寫 `trading_overrides`，覆蓋上面兩節的全局配置。**Bot 寫了的項用 Bot 的值，沒寫的項沿用全局。**

| 覆蓋項 | 對應全局配置 | 合併方式 |
|--------|--------------|----------|
| `fee_aware_spread` | `trading.fee_aware_spread` | 按字段：`enabled` 寫了就覆蓋；`safety_margin_ratio` >0 才覆蓋 |
| `post_only_reprice_max_attempts` | `trading.post_only_reprice_max_attempts` | 寫了就覆蓋（寫 0 表示用預設值 3） |
| `regime_filter` | `trading.regime_filter` | **整段替換**：沒寫的字段取默認值，不取全局值 |
| `adaptive_interval` | `trading.adaptive_interval` | 整段替換 |
| `upper_bound_freeze` | `trading.upper_bound_freeze` | 整段替換 |
| `inventory_skew` | `trading.inventory_skew` | 整段替換 |
| `funding_rate.pricing_enabled` / `funding_rate.pre_settlement_pause_minutes` | 同名全局 `funding_rate.*` | 按字段：寫了就覆蓋（可寫 `false` / `0` 關閉全局已開的功能） |

`grid_risk_control.stop_loss_basis` 不需要放在 `trading_overrides` 里：Bot 自己的 `grid_risk_control.stop_loss_basis` 為空時沿用全局值，寫了就用 Bot 的值。

```yaml
trading:
  inventory_skew:
    enabled: true
    strength: 0.5
funding_rate:
  enabled: true
  pricing_enabled: true

bots:
  - id: eth-grid
    exchange: binance
    symbol: ETHUSDT
    # ...其他 Bot 字段
    trading_overrides:
      inventory_skew:          # 整段替換：本 Bot 強度 0.8
        enabled: true
        strength: 0.8
      regime_filter:           # 全局未開，只給本 Bot 開
        enabled: true
        kline_interval: 4h
      fee_aware_spread:
        safety_margin_ratio: 0.0005
      funding_rate:
        pricing_enabled: false # 本 Bot 關閉資金費定價
        pre_settlement_pause_minutes: 10
```

Web API：`POST /api/bots/create`、`PUT /api/bots/:id/strategy`（整段替換 Bot 上原有的 `trading_overrides`）、`PUT /api/bots/:id/config-file`（`BotConfigFile` 頂層字段）都接受 JSON 字段 `trading_overrides`，結構同上。也可以在 Web 的 YAML 配置編輯器里直接改 `bots[].trading_overrides`。Bot 創建/編輯表單暫無對應輸入項。

注意：

- 通過上述 Bot API 保存時先按「全局 + 覆蓋」合併再校驗，非法（如 `inventory_skew.strength` > 1、`adaptive_interval.min_interval` > `max_interval`）返回 400。YAML 編輯器保存整份配置時不校驗單個 Bot 的覆蓋（避免一個 Bot 寫錯導致整份配置無法加載）。Bot 啟動時一律再校驗一次（含 `regime_filter.kline_interval` 等 K 線參數），非法時只有這個 Bot 拒絕啟動並報出錯誤，其他 Bot 不受影響。
- 修改覆蓋後需要**重啟該 Bot** 才生效（運行中熱更新只同步間隔、數量、窗口等參數）。
- `funding_rate.enabled`（資金費監控總開關）不能按 Bot 覆蓋，資金費定價仍需全局開啟。
- 只作用於網格類 Bot；`funding_carry`、`funding_perp_spread` 不使用這些配置。

## 配置模板

### 模板 1: 单机开发环境

```yaml
# config-dev.yaml
app:
    current_exchange: binance

exchanges:
    binance:
        api_key: "YOUR_API_KEY"
        secret_key: "YOUR_SECRET_KEY"
        testnet: true

trading:
    symbols:
        - exchange: binance
          symbol: ETHUSDT
          price_interval: 2
          order_quantity: 30

system:
    log_level: "DEBUG"
    timezone: "Asia/Shanghai"
    cancel_on_exit: true

# 单机模式（默认配置）
instance:
    id: "dev-instance"
    index: 0
    total: 1

database:
    type: "sqlite"
    dsn: "./data/quantmesh.db"

distributed_lock:
    enabled: false
```

### 模板 2: 生产环境（单实例）

```yaml
# config-prod-single.yaml
app:
    current_exchange: binance

exchanges:
    binance:
        api_key: "YOUR_API_KEY"
        secret_key: "YOUR_SECRET_KEY"
        testnet: false

trading:
    symbols:
        - exchange: binance
          symbol: ETHUSDT
          price_interval: 2
          order_quantity: 200
        - exchange: binance
          symbol: BTCUSDT
          price_interval: 50
          order_quantity: 0.001

system:
    log_level: "INFO"
    timezone: "Asia/Shanghai"
    cancel_on_exit: true

# 单机模式
instance:
    id: "prod-instance"
    index: 0
    total: 1

database:
    type: "sqlite"
    dsn: "./data/quantmesh.db"

distributed_lock:
    enabled: false
```

### 模板 3: 生产环境（多实例 - 实例 1）

```yaml
# config-prod-instance1.yaml
app:
    current_exchange: binance

exchanges:
    binance:
        api_key: "YOUR_API_KEY"
        secret_key: "YOUR_SECRET_KEY"
        testnet: false

trading:
    symbols:
        - exchange: binance
          symbol: ETHUSDT
          price_interval: 2
          order_quantity: 200
        - exchange: binance
          symbol: BTCUSDT
          price_interval: 50
          order_quantity: 0.001

system:
    log_level: "INFO"
    timezone: "Asia/Shanghai"
    cancel_on_exit: true

# 多实例模式 - 实例 1
instance:
    id: "prod-instance-1"
    index: 0
    total: 3

database:
    type: "postgres"
    dsn: "host=postgres user=quantmesh password=secret dbname=quantmesh port=5432 sslmode=disable"
    max_open_conns: 100
    max_idle_conns: 10

distributed_lock:
    enabled: true
    type: "redis"
    prefix: "quantmesh:lock:"
    default_ttl: 5
    redis:
        addr: "redis:6379"
        password: ""
        db: 0
        pool_size: 10
```

### 模板 4: 生产环境（多实例 - 实例 2）

```yaml
# config-prod-instance2.yaml
app:
    current_exchange: binance

exchanges:
    binance:
        api_key: "YOUR_API_KEY"
        secret_key: "YOUR_SECRET_KEY"
        testnet: false

trading:
    symbols:
        - exchange: binance
          symbol: BNBUSDT
          price_interval: 1
          order_quantity: 100
        - exchange: binance
          symbol: SOLUSDT
          price_interval: 0.5
          order_quantity: 50

system:
    log_level: "INFO"
    timezone: "Asia/Shanghai"
    cancel_on_exit: true

# 多实例模式 - 实例 2
instance:
    id: "prod-instance-2"
    index: 1
    total: 3

database:
    type: "postgres"
    dsn: "host=postgres user=quantmesh password=secret dbname=quantmesh port=5432 sslmode=disable"
    max_open_conns: 100
    max_idle_conns: 10

distributed_lock:
    enabled: true
    type: "redis"
    prefix: "quantmesh:lock:"
    default_ttl: 5
    redis:
        addr: "redis:6379"
        password: ""
        db: 0
        pool_size: 10
```

## 配置验证

### 验证单机模式

```bash
# 启动应用
./quantmesh --config=config-dev.yaml

# 查看日志，应该看到：
# ✅ 分布式锁未启用（单机模式）
# ✅ 数据库已初始化 (类型: sqlite)
```

### 验证多实例模式

```bash
# 启动实例 1
./quantmesh --config=config-prod-instance1.yaml &

# 启动实例 2
./quantmesh --config=config-prod-instance2.yaml &

# 查看日志，应该看到：
# ✅ 分布式锁已启用 (类型: redis, 实例: prod-instance-1)
# ✅ 数据库已初始化 (类型: postgres)
```

## 配置迁移

### 从单机迁移到多实例

**步骤 1: 部署 Redis**
```bash
docker run -d --name redis -p 6379:6379 redis:7-alpine
```

**步骤 2: 部署 PostgreSQL**
```bash
docker run -d --name postgres \
  -e POSTGRES_USER=quantmesh \
  -e POSTGRES_PASSWORD=secret \
  -e POSTGRES_DB=quantmesh \
  -p 5432:5432 postgres:15-alpine
```

**步骤 3: 迁移数据**
```bash
# 使用 pgloader 迁移 SQLite 到 PostgreSQL
pgloader data/quantmesh.db postgresql://quantmesh:secret@localhost/quantmesh
```

**步骤 4: 更新配置**
```yaml
# 启用分布式锁
distributed_lock:
    enabled: true

# 切换到 PostgreSQL
database:
    type: "postgres"
    dsn: "host=localhost user=quantmesh password=secret dbname=quantmesh"
```

**步骤 5: 重启应用**
```bash
# 停止旧实例
pkill quantmesh

# 启动新实例
./quantmesh --config=config-prod-instance1.yaml &
./quantmesh --config=config-prod-instance2.yaml &
```

## 常见问题

### Q1: 单机模式下是否需要配置 distributed_lock？

**A:** 不需要。单机模式下 `distributed_lock.enabled` 默认为 `false`，系统会使用零开销的 `NopLock`。

### Q2: 可以在单机模式下使用 PostgreSQL 吗？

**A:** 可以。数据库类型和分布式锁是独立的。单机模式下也可以使用 PostgreSQL，只是没有必要。

### Q3: 多实例模式下必须使用 Redis 吗？

**A:** 目前是的。未来会支持 etcd 和数据库锁。

### Q4: 如何验证配置是否正确？

**A:** 启动应用后查看日志：
- 单机模式：`ℹ️ 分布式锁未启用（单机模式）`
- 多实例模式：`✅ 分布式锁已启用 (类型: redis, 实例: xxx)`

### Q5: 配置错误会怎样？

**A:** 应用会在启动时检测并报错，不会启动。例如：
- Redis 连接失败：`❌ 初始化分布式锁失败: dial tcp: connection refused`
- 数据库连接失败：`⚠️ 初始化数据库失败: connection refused`

## 性能建议

### 单机模式

```yaml
database:
    type: "sqlite"
    max_open_conns: 1      # SQLite 只支持单连接写入
    max_idle_conns: 1
```

### 多实例模式（3 实例）

```yaml
database:
    type: "postgres"
    max_open_conns: 100    # 3实例 × 30并发 + 10余量
    max_idle_conns: 10     # 10% 的最大连接数
    conn_max_lifetime: 1800

distributed_lock:
    default_ttl: 5         # 5秒足够大部分操作
    redis:
        pool_size: 10      # 每实例10个连接
```

## 安全建议

1. **生产环境使用强密码**
   ```yaml
   database:
       dsn: "...password=STRONG_PASSWORD..."
   
   distributed_lock:
       redis:
           password: "STRONG_PASSWORD"
   ```

2. **限制数据库访问**
   ```bash
   # 只允许内网访问
   ufw allow from 10.0.0.0/8 to any port 5432
   ufw allow from 10.0.0.0/8 to any port 6379
   ```

3. **使用 SSL/TLS**
   ```yaml
   database:
       dsn: "...sslmode=require"
   ```

4. **定期备份**
   ```bash
   # 添加到 crontab
   0 */6 * * * /opt/quantmesh/scripts/backup.sh
   ```

## 参考文档

- [高可用架构设计](HIGH_AVAILABILITY.md)
- [快速开始指南](HA_QUICKSTART.md)
- [多实例解决方案](MULTI_INSTANCE_SOLUTION.md)
- [AI 命名上游（多 profile 设计）](AI_UPSTREAM_PROFILES.md) · [English](i18n/en/AI_UPSTREAM_PROFILES.md)

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
