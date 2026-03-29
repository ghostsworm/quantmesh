# Configuration Redundancy and Migration

This document describes redundant configuration items and how the system handles them. Use it to simplify your `config.yaml` and avoid confusion.

## 1. Single vs Multi-Symbol (trading.symbol vs trading.symbols)

**Redundancy:** The config supports both legacy single-pair fields (`trading.symbol`, `trading.price_interval`, etc.) and the new multi-pair list (`trading.symbols`).

**Behavior:**
- If `trading.symbols` is non-empty, it takes precedence. The first symbol's parameters are also written back to the legacy fields for backward compatibility.
- If `trading.symbols` is empty, the loader builds a single-element list from `trading.symbol` and the other legacy fields.

**Recommendation:** Prefer configuring only `trading.symbols`. Omit or remove duplicate legacy fields for each pair; per-symbol defaults fall back to the top-level `trading.*` values.

**Example (preferred):**

```yaml
trading:
  symbols:
    - enabled: true
      exchange: binance
      symbol: BTCUSDT
      total_allocated_capital: 5000
      strategies: []
      # price_interval, order_quantity, etc. optional here; inherit from below or defaults
  # Legacy fields below are optional when using symbols; used as defaults for symbols
  price_interval: 150
  order_quantity: 150
```

## 2. Database vs Storage (SQLite path)

**Redundancy:** Two blocks define persistence: `database` (GORM/application DB) and `storage` (event/trade log writer). Both can point to a SQLite file.

**Behavior:**
- When both use SQLite, the loader **unifies** the path: `storage.path` is set to `database.dsn` at load time so only one file is used.
- `database.dsn` is the single source of truth for the SQLite file path.

**Recommendation:** For SQLite, set only `database.dsn`. Leave `storage.path` unset or ensure it matches `database.dsn`; the loader will align them.

**Example:**

```yaml
database:
  type: sqlite
  dsn: ./data/quantmesh.db

storage:
  enabled: true
  type: sqlite
  # path can be omitted; will be set to database.dsn
```

## 3. Log level (system vs database)

**Redundancy:** Log level can be set in two places: `system.log_level` (application logs) and `database.log_level` (GORM logs).

**Behavior:** They control different loggers. No automatic unification.

**Recommendation:** Keep both if you need different verbosity for app vs DB. For minimal config, set `system.log_level` and leave `database.log_level` at default (`error`).

## 4. Notifications (top-level vs per-channel enabled)

**Redundancy:** `notifications.enabled` is a global switch; each channel (e.g. `notifications.telegram.enabled`) also has its own `enabled` flag.

**Behavior:** A channel only runs when both the global and its own `enabled` are true.

**Recommendation:** Set `notifications.enabled: true` when using any channel, and set `enabled: true` only for the channels you use. Leave others `enabled: false` or omit.

## 5. Summary

| Item              | Redundant with     | Resolution / recommendation                          |
|-------------------|--------------------|------------------------------------------------------|
| trading.symbol    | trading.symbols    | Prefer `symbols`; legacy fields used as defaults     |
| storage.path      | database.dsn       | Unified to `database.dsn` when both use SQLite      |
| system.log_level  | database.log_level | Different scopes; set both only if needed           |
| notifications.*   | Per-channel enabled| Global + per-channel; enable only what you use      |

After applying these, you can trim redundant keys from `config.yaml` and rely on defaults and the loader behavior above.

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
