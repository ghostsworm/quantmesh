# Risk Control Guide

This guide describes QuantMesh's risk control system: proactive market risk monitoring, account safety checks, reconciliation, and order cleanup.

## Components

| Component | Config section | Purpose |
|-----------|-----------------|---------|
| RiskMonitor | `risk_control` | K-line volume anomaly detection; pauses trading when triggered |
| Account safety | Startup / config | Balance, leverage, and position safety checks before trading |
| Reconciler | `trading.reconcile_interval` | Syncs local state with exchange (orders, positions) |
| Order cleaner | `trading.order_cleanup_*` | Removes stale or excess orders |
| Depth monitor | `risk_control.depth_monitor` | Orderbook depth monitoring (optional) |

---

## 1. Proactive Risk Monitor (K-Line Volume)

### Role

Watches K-line volume for monitored symbols. If current volume is significantly above the recent average (e.g. spike or anomaly), trading can be paused until the market is considered normal again.

### Configuration

```yaml
risk_control:
  enabled: true
  monitor_symbols:
    - BTCUSDT
    - ETHUSDT
    - SOLUSDT
  interval: 1m              # K-line interval
  volume_multiplier: 3       # Trigger when volume > average * this (e.g. 3x)
  average_window: 20         # Number of closed K-bars for average
  recovery_threshold: 3      # Min number of symbols "healthy" to recover (out of len(monitor_symbols))
  max_leverage: 3             # Max allowed leverage (0 = no limit)
  depth_monitor:
    enabled: false
    check_interval: 5
    depth_levels: 10
    drop_threshold: 0.5
    recovery_threshold: 0.7
    min_depth_usdt: 10000
```

### Behavior

- **Trigger**: For any monitored symbol, when the current (or last closed) bar's volume is greater than `average_window` average volume × `volume_multiplier`, that symbol is marked unhealthy.
- **Pause**: If fewer than `recovery_threshold` symbols are healthy, the risk monitor is in "triggered" state and trading is paused (no new orders).
- **Recovery**: When at least `recovery_threshold` symbols are healthy again, the trigger is cleared and trading can resume.

### Integration

- Implemented in [`safety/risk_monitor.go`](../../safety/risk_monitor.go).
- Can be combined with news-driven risk (e.g. NewsMonitor) when configured.

### API

- `GET /api/risk/status` – Current risk (triggered/recovered) and message.
- `GET /api/risk/monitor` – Risk monitor data (e.g. per-symbol health).
- `GET /api/risk/history` – History of risk checks.

---

## 2. Account Safety (Startup)

Before trading starts, the system can check:

- Account balance (sufficient for the strategy).
- Leverage (e.g. not above `max_leverage`).
- Position safety (e.g. minimum number of positions or similar rules).

Implemented in [`safety/safety.go`](../../safety/safety.go). If the account already has an open position, some checks may be skipped (user is assumed to be aware of risk).

---

## 3. Reconciliation

### Role

Periodically aligns local orders and positions with the exchange so that local state does not drift (e.g. after manual cancels or external trades).

### Configuration

```yaml
trading:
  reconcile_interval: 60   # Seconds between reconciliation runs
```

### Behavior

- Fetches open orders and positions from the exchange.
- Updates local state; cancels or adjusts local view as needed.
- Helps keep grid slots and order counts correct.

### API

- `GET /api/reconciliation/status` – Current reconciliation status.
- `GET /api/reconciliation/history` – Reconciliation history.
- `GET /api/reconciliation/aggregated` – Aggregated reconciliation data.

---

## 4. Order Cleanup

### Role

Prevents too many open orders (e.g. old or redundant orders) by cancelling in batches when the count exceeds a threshold.

### Configuration

```yaml
trading:
  order_cleanup_threshold: 50   # Max open orders before cleanup
  cleanup_batch_size: 20         # How many to cancel per run
```

`timing.order_cleanup_interval` controls how often the cleanup job runs (default 60 seconds).

### Behavior

- If open orders for the symbol (or globally) exceed `order_cleanup_threshold`, the system cancels up to `cleanup_batch_size` orders (oldest or by strategy policy).
- Implemented in [`safety/order_cleaner.go`](../../safety/order_cleaner.go).

---

## 5. Depth Monitor (Optional)

Under `risk_control.depth_monitor` you can enable orderbook depth checks:

- If depth (e.g. sum of top N levels in USDT) drops below a threshold, risk can be triggered.
- When depth recovers above `recovery_threshold`, risk is cleared.

Use this when you want to pause or limit trading in thin book conditions.

---

## 6. Multi-Symbol and Recovery

- **monitor_symbols**: All listed symbols are checked every K-line update. Any symbol can contribute to "unhealthy" count.
- **recovery_threshold**: Number of symbols that must be healthy to clear the trigger. Example: 5 symbols, threshold 3 → need at least 3 healthy to recover.
- **News-driven risk**: If NewsMonitor (or similar) is enabled, risk can also be triggered by news/AI signals; see news and AI docs for configuration.

---

## Summary

| Goal | Config / component |
|------|---------------------|
| Pause on volume spike | `risk_control` + RiskMonitor |
| Limit leverage | `risk_control.max_leverage`, account safety |
| Keep state in sync with exchange | `trading.reconcile_interval`, Reconciler |
| Cap open orders | `order_cleanup_threshold`, `cleanup_batch_size`, Order cleaner |
| Pause on thin orderbook | `risk_control.depth_monitor` |

For config redundancy and migration, see [CONFIGURATION_REDUNDANCY_AND_MIGRATION.md](CONFIGURATION_REDUNDANCY_AND_MIGRATION.md). For full config reference, see [CONFIGURATION_GUIDE.md](../../CONFIGURATION_GUIDE.md).
