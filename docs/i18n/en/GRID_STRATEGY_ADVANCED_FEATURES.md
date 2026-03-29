# Grid Strategy Advanced Features (P1 / P2)

This guide describes the advanced grid features: **P1 (Funding Rate & Trend Sync)** and **P2 (Orderbook Optimization)**, plus dynamic order quantity adjustment.

## Overview

| Feature | Config key | Description |
|--------|-------------|-------------|
| P1 | `funding_rate.trend_sync_enabled` | Aligns funding-rate bias with trend filter to improve buy/sell timing |
| P2 | `trading.orderbook_optimization` | Tweaks grid prices using orderbook depth to avoid thin liquidity |
| Dynamic | `trading.dynamic_adjustment.order_quantity` | Adjusts per-order amount by trade frequency |

---

## P1: Funding Rate and Trend Sync

### Purpose

When funding is negative and trend is up, buying is relaxed; when funding is high and trend is down, selling is emphasized. This improves entry/exit quality.

### Configuration

```yaml
funding_rate:
  enabled: true
  bias_enabled: true
  trend_sync_enabled: true   # Enable P1: funding + trend sync (default true)
  high_rate_threshold: 0.001
  pause_buy_threshold: 0.0015
```

### Behavior

- **Relax buy**: `buyBias > 1` (negative funding) and trend is up → trend filter is relaxed; `allowedNewBuyOrders` may be increased (capped by bias).
- **Strengthen sell**: `buyBias < 1` (high positive funding) and trend is down → `skipBuying = true` or `allowedNewBuyOrders` reduced to 0.
- **Other cases**: Existing logic unchanged; trend and funding are combined as before.

### Requirements

- `funding_rate.bias_enabled: true`
- Grid risk control with `trend_filter_enabled: true` where trend is used
- `RiskMonitor` and trend detector (e.g. `TrendDetector`) must be wired in `SuperPositionManager`

### Tuning

- Use `high_rate_threshold` and `pause_buy_threshold` to define "high" funding; adjust with your symbol and funding schedule.
- If P1 is too aggressive, set `trend_sync_enabled: false` to fall back to non-synced behavior.

---

## P2: Orderbook Optimization

### Purpose

Adjusts grid order prices using orderbook depth so orders are not placed in "empty" zones, improving fill probability.

### Configuration

```yaml
trading:
  orderbook_optimization:
    enabled: true
    depth_levels: 20           # Orderbook depth to fetch
    min_depth_usdt: 5000       # Below this (sum of N levels) = thin, need adjustment
    lookback_levels: 3          # Check N levels around candidate price
    optimization_interval: 30   # Seconds; 0 = optimize on every AdjustOrders
```

### Behavior

1. Before placing orders, the system fetches orderbook with `depth_levels`.
2. For each candidate price:
   - **Bids**: If total depth (in USDT) of the next `lookback_levels` levels below the price is less than `min_depth_usdt`, the price is shifted slightly down toward a deeper level.
   - **Asks**: If total depth above the price is below `min_depth_usdt`, the price is shifted slightly up.
3. Adjusted prices stay within the grid's buy/sell window and `price_interval`.

### Edge Cases

- If orderbook fetch fails, original prices are used and normal grid logic continues.
- Adjustments are limited so grid structure is preserved.

### Tuning

- Increase `min_depth_usdt` for larger size or illiquid symbols.
- Increase `optimization_interval` to reduce exchange API usage.

---

## Dynamic Order Quantity (P0)

### Purpose

Adjusts per-order amount based on recent trade frequency (e.g. increase when fills are rare, decrease when fills are frequent).

### Configuration

```yaml
trading:
  dynamic_adjustment:
    enabled: true
    order_quantity:
      enabled: true
      min: 50
      max: 300
      frequency_threshold: 2    # Trades per minute threshold
      adjustment_step: 20
      check_interval: 60         # Seconds
```

### Behavior

- The system periodically checks trade frequency; if it is below `frequency_threshold`, order quantity may be increased (up to `max`); if above, decreased (down to `min`), in steps of `adjustment_step`.

### UI

Dynamic adjustment (including order quantity) can be configured from the Web UI under the grid/symbol settings.

---

## Parameter Summary

| Parameter | Section | Default | Notes |
|-----------|---------|---------|--------|
| `trend_sync_enabled` | `funding_rate` | true | P1 on/off |
| `orderbook_optimization.enabled` | `trading` | false | P2 on/off |
| `orderbook_optimization.depth_levels` | `trading` | 20 | Orderbook depth |
| `orderbook_optimization.min_depth_usdt` | `trading` | 5000 | Thin-depth threshold (USDT) |
| `orderbook_optimization.lookback_levels` | `trading` | 3 | Levels around price to check |
| `orderbook_optimization.optimization_interval` | `trading` | 30 | Seconds; 0 = every run |
| `dynamic_adjustment.order_quantity.*` | `trading` | — | P0 dynamic quantity |

For full P1/P2 implementation details, see [GRID_ALPHA_P1_P2_SPEC.md](../../GRID_ALPHA_P1_P2_SPEC.md).

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
