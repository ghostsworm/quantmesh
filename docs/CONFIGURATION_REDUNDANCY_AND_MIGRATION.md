# 配置冗餘與遷移

本文件說明冗餘配置項與系統處理方式，供精簡 `config.yaml` 與避免混淆。

## 1. 單一與多交易對（trading.symbol vs trading.symbols）

**冗餘**：配置同時支援舊版單一交易對欄位（`trading.symbol`、`trading.price_interval` 等）與新版多交易對列表（`trading.symbols`）。

**行為**：
- 若 `trading.symbols` 非空，以它為準；並會將第一個交易對的參數寫回舊欄位以相容舊程式。
- 若 `trading.symbols` 為空，載入器會依 `trading.symbol` 與其他舊欄位組出單一元素的列表。

**建議**：優先只配置 `trading.symbols`。各交易對可省略重複的舊欄位；未填時會沿用頂層 `trading.*` 的預設。

**範例（建議寫法）**：

```yaml
trading:
  symbols:
    - enabled: true
      exchange: binance
      symbol: BTCUSDT
      total_allocated_capital: 5000
      strategies: []
      # price_interval、order_quantity 等可在此省略，沿用下方或預設
  # 下方舊欄位在使用 symbols 時可選，作為 symbols 的預設
  price_interval: 150
  order_quantity: 150
```

## 2. 資料庫與存儲（SQLite 路徑）

**冗餘**：`database`（GORM／應用資料庫）與 `storage`（事件／成交寫入）兩區塊皆可指向 SQLite 檔案。

**行為**：
- 當兩者皆為 SQLite 時，載入時會**統一**路徑：將 `storage.path` 設為 `database.dsn`，只使用一個檔案。
- `database.dsn` 為 SQLite 路徑的單一真相來源。

**建議**：使用 SQLite 時只設定 `database.dsn`。可省略 `storage.path` 或令其與 `database.dsn` 一致；載入器會對齊。

**範例**：

```yaml
database:
  type: sqlite
  dsn: ./data/quantmesh.db

storage:
  enabled: true
  type: sqlite
  # path 可省略；會設為 database.dsn
```

## 3. 日誌級別（system vs database）

**冗餘**：日誌級別可於兩處設定：`system.log_level`（應用日誌）與 `database.log_level`（GORM 日誌）。

**行為**：兩者控制不同紀錄器，不會自動統一。

**建議**：若需要應用與資料庫不同詳細度可兩者都設。精簡配置可只設 `system.log_level`，`database.log_level` 維持預設（`error`）。

## 4. 通知（頂層與各頻道 enabled）

**冗餘**：`notifications.enabled` 為總開關；各頻道（如 `notifications.telegram.enabled`）也有自己的 `enabled`。

**行為**：僅當總開關與該頻道皆為 true 時，該頻道才會啟用。

**建議**：使用任一頻道時設 `notifications.enabled: true`，僅對要使用的頻道設 `enabled: true`，其餘可 `enabled: false` 或省略。

## 5. 摘要

| 項目 | 冗餘對象 | 處理／建議 |
|-------------------|--------------------|------------------------------------------------------|
| trading.symbol | trading.symbols | 優先使用 `symbols`；舊欄位作為預設 |
| storage.path | database.dsn | 兩者皆為 SQLite 時統一為 `database.dsn` |
| system.log_level | database.log_level | 用途不同；僅必要時兩者都設 |
| notifications.* | 各頻道 enabled | 總開關＋各頻道；只啟用需要的頻道 |

依上述方式可刪減 `config.yaml` 中的冗餘鍵，並依賴預設值與載入器行為。

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
