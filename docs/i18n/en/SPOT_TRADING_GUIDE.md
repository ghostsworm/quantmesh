# Spot Trading Guide

QuantMesh supports both **futures** (U-margined contracts) and **spot** markets. This guide explains how to use spot trading.

## Spot vs Futures

| Aspect | Spot | Futures |
|--------|------|---------|
| Leverage | None (1x) | Configurable (e.g. 1x–20x) |
| Short selling | No (sell only what you hold) | Yes |
| Funding rate | N/A | Yes (periodic funding) |
| ReduceOnly | N/A | Supported on futures |
| Typical use | Buy and hold, grid buy-low/sell-high | Directional and grid with leverage |

## Enabling Spot for a Symbol

Set `market_type` to `spot` in the symbol configuration. Multi-symbol config uses `trading.symbols[].market_type`.

### Example: Single symbol (legacy-style)

If you use the legacy single-pair config, set:

```yaml
trading:
  symbol: BTCUSDT
  market_type: spot   # Use spot market (default is futures)
  price_interval: 150
  order_quantity: 150
  # ...
```

### Example: Multi-symbol (recommended)

```yaml
trading:
  symbols:
    - enabled: true
      exchange: binance
      symbol: BTCUSDT
      market_type: spot    # Spot market for this pair
      total_allocated_capital: 5000
      strategies:
        - type: grid
          weight: 1.0
          config: {}
      price_interval: 150
      order_quantity: 150
      # ...
    - enabled: true
      exchange: binance
      symbol: ETHUSDT
      market_type: futures # Futures for this pair
      # ...
```

- Default is `futures` when `market_type` is omitted (backward compatible).
- Valid values: `spot`, `futures`.

## Supported Exchanges

Spot is supported wherever the exchange adapter implements spot APIs (e.g. Binance Spot, Gate Spot). The same adapter may support both spot and futures depending on symbol and endpoint.

- **Binance**: Spot and USDT-M futures; set `market_type` per symbol.
- **Gate, OKX, Bybit, etc.**: Use the adapter's spot/futures support; configure `market_type` accordingly.

Check each exchange's adapter and docs for symbol format (e.g. `BTCUSDT` for spot vs futures).

## Important Notes for Spot

1. **No leverage**: Spot is always 1x. `leverage` in exchange config is ignored for spot symbols.
2. **No ReduceOnly**: Spot orders are normal buy/sell; ReduceOnly is a futures concept and is not used for spot.
3. **Position meaning**: "Position" for spot is your net base-asset balance (buys minus sells), not a separate contract position.
4. **Funding**: Funding rate and funding-based features (e.g. P1 trend sync) apply only to futures symbols; they are skipped for spot.

## Strategy Compatibility

- **Grid**: Works on spot (buy low, sell high; no short side).
- **DCA / Martingale / Mean reversion / Momentum / Trend following**: Generally support both spot and futures; behavior may differ (e.g. no shorting on spot).

## Configuration Checklist

- [ ] Set `market_type: spot` for each spot symbol in `trading.symbols` (or in legacy `trading` if single-pair).
- [ ] Use an exchange that supports spot for that symbol.
- [ ] Do not rely on funding-rate or ReduceOnly logic for spot symbols.
- [ ] Ensure `price_interval` and `order_quantity` suit spot liquidity and fees.

For multi-symbol and validation details, see [CONFIGURATION_GUIDE.md](../../CONFIGURATION_GUIDE.md) and [CONFIGURATION_REDUNDANCY_AND_MIGRATION.md](../../CONFIGURATION_REDUNDANCY_AND_MIGRATION.md).

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
