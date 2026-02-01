# API Reference

All HTTP APIs are under the `/api` prefix. Successful JSON responses use `200` unless noted. The server adds `X-App-Version` to every API response for debugging.

**Base URL**: `http://<host>:<port>/api` (default port from `web.port`, e.g. 28888)

**Authentication**: Most business APIs require authentication (session cookie or API key when configured). Public endpoints: `/api/version`, `/api/auth/*` (login/setup), `/api/setup/*`.

---

## Version

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/version` | No | Returns `{ "version": "<semver>" }`. Response header: `X-App-Version`. |

---

## Auth

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/auth/status` | No | Current auth status |
| POST | `/api/auth/password/set` | No | Set initial password (setup) |
| POST | `/api/auth/password/verify` | No | Verify password (login) |
| POST | `/api/auth/logout` | No | Logout |
| POST | `/api/auth/password/change` | Yes | Change password |

---

## Setup

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/setup/status` | No | Setup completion status |
| POST | `/api/setup/init` | No | Initialize setup |
| POST | `/api/setup/exchange-symbols` | No | Get exchange symbols |

---

## WebAuthn

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| POST | `/api/webauthn/register/begin` | Yes | Start WebAuthn registration |
| POST | `/api/webauthn/register/finish` | Yes | Finish WebAuthn registration |
| POST | `/api/webauthn/login/begin` | No | Start WebAuthn login |
| POST | `/api/webauthn/login/finish` | No | Finish WebAuthn login |
| GET | `/api/webauthn/credentials` | Yes | List WebAuthn credentials |
| POST | `/api/webauthn/credentials/delete` | Yes | Delete a credential |

---

## Status & Trading

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/status` | Yes | Single instance status |
| GET | `/api/statuses` | Yes | Multi-instance statuses |
| GET | `/api/symbols` | Yes | Symbol list |
| GET | `/api/exchanges` | Yes | Exchange list |
| GET | `/api/positions` | Yes | Positions |
| GET | `/api/positions/summary` | Yes | Positions summary |
| GET | `/api/orders` | Yes | Orders |
| GET | `/api/orders/history` | Yes | Order history |
| GET | `/api/orders/pending` | Yes | Pending orders |
| POST | `/api/trading/start` | Yes | Start trading |
| POST | `/api/trading/stop` | Yes | Stop trading |
| POST | `/api/trading/close-positions` | Yes | Close all positions |

---

## Statistics

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/statistics` | Yes | Statistics |
| GET | `/api/statistics/daily` | Yes | Daily statistics |
| GET | `/api/statistics/trades` | Yes | Trade statistics |
| GET | `/api/statistics/pnl/symbol` | Yes | PnL by symbol |
| GET | `/api/statistics/pnl/time-range` | Yes | PnL by time range |
| GET | `/api/statistics/pnl/exchange` | Yes | PnL by exchange |
| GET | `/api/statistics/pnl/diagnosis` | Yes | Exchange PnL diagnosis |
| GET | `/api/statistics/anomalous-trades` | Yes | Anomalous trades |

---

## Allocation & Plans

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/allocation/status` | Yes | Allocation status |
| GET | `/api/allocation/status/:exchange/:symbol` | Yes | Allocation by exchange/symbol |
| GET | `/api/position-plans/check` | Yes | Position plan check |
| GET | `/api/position-plans` | Yes | List position plans |
| GET | `/api/position-plans/:id` | Yes | Get position plan |
| POST | `/api/position-plans` | Yes | Create position plan |
| PUT | `/api/position-plans/:id` | Yes | Update position plan |
| DELETE | `/api/position-plans/:id` | Yes | Cancel position plan |

---

## Reconciliation & Risk

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/reconciliation/status` | Yes | Reconciliation status |
| GET | `/api/reconciliation/history` | Yes | Reconciliation history |
| GET | `/api/reconciliation/aggregated` | Yes | Aggregated reconciliation |
| GET | `/api/risk/status` | Yes | Risk status |
| GET | `/api/risk/monitor` | Yes | Risk monitor data |
| GET | `/api/risk/history` | Yes | Risk check history |
| GET | `/api/risk/newbie-check` | Yes | Newbie risk check |
| POST | `/api/risk/newbie-check/apply` | Yes | Apply newbie security config |

---

## News & Predictions

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/news/analysis` | Yes | News analysis |
| GET | `/api/news/predictions` | Yes | News predictions |
| POST | `/api/news/analyze` | Yes | Trigger news analysis |
| GET | `/api/news/collected` | Yes | Collected news |
| GET | `/api/news/keywords` | Yes | News keywords |
| PUT | `/api/news/keywords` | Yes | Update news keywords |
| GET | `/api/news/history` | Yes | News history |
| GET | `/api/news/history/:id` | Yes | News history by ID |
| GET | `/api/predictions/accuracy` | Yes | Predictions accuracy |
| GET | `/api/predictions/history` | Yes | Predictions history |

---

## Config

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/config` | Yes | Get config |
| GET | `/api/config/json` | Yes | Get config as JSON |
| POST | `/api/config/validate` | Yes | Validate config |
| POST | `/api/config/validate-yaml` | Yes | Validate YAML config |
| POST | `/api/config/preview` | Yes | Preview config |
| POST | `/api/config/update` | Yes | Update config |
| POST | `/api/config/update-yaml` | Yes | Update config from YAML |
| GET | `/api/config/backups` | Yes | List backups |
| POST | `/api/config/restore/:backup_id` | Yes | Restore backup |
| DELETE | `/api/config/backup/:backup_id` | Yes | Delete backup |
| GET | `/api/config/history` | Yes | Config history list |
| GET | `/api/config/history/:version` | Yes | Config at version |
| POST | `/api/config/history/:version/restore` | Yes | Restore config version |
| POST | `/api/config/history/diff` | Yes | Diff config versions |

---

## Export

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/export/config` | Yes | Export config |
| GET | `/api/export/config/history/:version` | Yes | Export config version |
| GET | `/api/export/trades` | Yes | Export trades |
| GET | `/api/export/orders` | Yes | Export orders |
| GET | `/api/export/positions` | Yes | Export positions |
| GET | `/api/export/statistics` | Yes | Export statistics |
| GET | `/api/export/reconciliation` | Yes | Export reconciliation |
| GET | `/api/export/risk-checks` | Yes | Export risk checks |
| GET | `/api/export/system-metrics` | Yes | Export system metrics |
| GET | `/api/export/logs` | Yes | Export logs |
| GET | `/api/export/audit-logs` | Yes | Export audit logs |
| GET | `/api/export/all` | Yes | Export all |

---

## Backtest

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| POST | `/api/backtest/run` | Yes | Run backtest |
| GET | `/api/backtest/strategies` | Yes | Backtest strategies |
| GET | `/api/backtest/presets/:symbol` | Yes | Preset for symbol |
| POST | `/api/backtest/cache/generate` | Yes | Generate cache |
| GET | `/api/backtest/cache/status` | Yes | Cache status |
| GET | `/api/backtest/cache/stats` | Yes | Cache stats |
| GET | `/api/backtest/cache/list` | Yes | List cache |
| DELETE | `/api/backtest/cache/:key` | Yes | Delete cache entry |
| DELETE | `/api/backtest/cache` | Yes | Clear cache |
| POST | `/api/backtest/tasks` | Yes | Create backtest task |
| GET | `/api/backtest/tasks` | Yes | List backtest tasks |
| GET | `/api/backtest/tasks/:id` | Yes | Get task |
| GET | `/api/backtest/tasks/:id/result` | Yes | Task result |
| GET | `/api/backtest/tasks/:id/report` | Yes | Task report |
| DELETE | `/api/backtest/tasks/:id` | Yes | Delete task |

---

## Optimizer

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/optimizer/price` | Yes | Optimizer price |
| POST | `/api/optimizer/run` | Yes | Run optimizer |
| GET | `/api/optimizer/status/:id` | Yes | Optimizer status |
| GET | `/api/optimizer/result/:id` | Yes | Optimizer result |
| POST | `/api/optimizer/stop/:id` | Yes | Stop optimizer |

---

## Payment (Crypto)

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/payment/crypto/currencies` | Yes | Supported crypto currencies |
| POST | `/api/payment/crypto/coinbase/create` | Yes | Create Coinbase payment |
| POST | `/api/payment/crypto/direct/create` | Yes | Create direct payment |
| GET | `/api/payment/crypto/list` | Yes | List user payments |
| GET | `/api/payment/crypto/:id` | Yes | Payment status |
| POST | `/api/payment/crypto/:id/submit-tx` | Yes | Submit tx hash |
| POST | `/api/payment/crypto/:id/confirm` | Yes | Confirm direct payment (admin) |

---

## System & Logs

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/system/metrics` | Yes | System metrics |
| GET | `/api/system/metrics/current` | Yes | Current system metrics |
| GET | `/api/system/metrics/daily` | Yes | Daily system metrics |
| GET | `/api/logs` | Yes | Logs |
| POST | `/api/logs/clean` | Yes | Clean logs |
| GET | `/api/logs/stats` | Yes | Log stats |
| POST | `/api/logs/vacuum` | Yes | Vacuum logs |

---

## Slots, Strategies, K-Lines, Funding

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/slots` | Yes | Slot data |
| GET | `/api/strategies/allocation` | Yes | Strategy allocation |
| GET | `/api/klines` | Yes | K-line data |
| GET | `/api/funding/current` | Yes | Current funding rate |
| GET | `/api/funding/history` | Yes | Funding rate history |

---

## AI

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/ai/status` | Yes | AI analysis status |
| GET | `/api/ai/analysis/market` | Yes | Market analysis |
| GET | `/api/ai/analysis/parameter` | Yes | Parameter optimization |
| GET | `/api/ai/analysis/risk` | Yes | Risk analysis |
| GET | `/api/ai/analysis/sentiment` | Yes | Sentiment analysis |
| GET | `/api/ai/analysis/polymarket` | Yes | Polymarket signal |
| POST | `/api/ai/analysis/trigger/:module` | Yes | Trigger AI module |
| GET | `/api/ai/prompts` | Yes | AI prompts |
| POST | `/api/ai/prompts` | Yes | Update AI prompt |
| POST | `/api/ai/generate-config` | Yes | Generate AI config |
| GET | `/api/ai/task/:task_id` | Yes | AI task status |
| GET | `/api/ai/tasks` | Yes | AI tasks |
| GET | `/api/ai/tasks/stats` | Yes | AI task stats |
| POST | `/api/ai/apply-config` | Yes | Apply AI config |

---

## Basis (Spread Monitor)

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/basis/current` | Yes | Current basis |
| GET | `/api/basis/history` | Yes | Basis history |
| GET | `/api/basis/statistics` | Yes | Basis statistics |

---

## Market Intelligence, Permissions, Audit

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/market-intelligence` | Yes | Market intelligence |
| GET | `/api/permissions/check` | Yes | API permissions check |
| GET | `/api/audit/logs` | Yes | Audit logs |

---

## Strategies (Management)

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/strategies` | Yes | List strategies |
| GET | `/api/strategies/types` | Yes | Strategy types |
| GET | `/api/strategies/configs` | Yes | Strategy configs |
| GET | `/api/strategies/enabled` | Yes | Enabled strategies |
| POST | `/api/strategies/batch-update` | Yes | Batch update strategies |
| GET | `/api/strategies/:id` | Yes | Strategy detail |
| POST | `/api/strategies/:id/enable` | Yes | Enable strategy |
| POST | `/api/strategies/:id/disable` | Yes | Disable strategy |
| GET | `/api/strategies/:id/license` | Yes | Strategy license |
| PUT | `/api/strategies/:id/config` | Yes | Update strategy config |
| POST | `/api/strategies/:id/purchase` | Yes | Purchase strategy |

---

## Profit

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/profit/summary` | Yes | Profit summary |
| GET | `/api/profit/by-strategy` | Yes | Profits by strategy |
| GET | `/api/profit/by-strategy/:id` | Yes | Strategy profit detail |
| GET | `/api/profit/withdraw-rules` | Yes | Withdraw rules |
| PUT | `/api/profit/withdraw-rules` | Yes | Update withdraw rules |
| POST | `/api/profit/withdraw-rules/upsert` | Yes | Upsert withdraw rule |
| DELETE | `/api/profit/withdraw-rules/:id` | Yes | Delete withdraw rule |
| POST | `/api/profit/withdraw` | Yes | Withdraw profit |
| GET | `/api/profit/history` | Yes | Withdraw history |
| GET | `/api/profit/trend` | Yes | Profit trend |
| POST | `/api/profit/withdraw/estimate` | Yes | Estimate withdraw fee |
| POST | `/api/profit/withdraw/:id/cancel` | Yes | Cancel withdraw |
| GET | `/api/profit/withdraw/:id` | Yes | Withdraw detail |

---

## Capital

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/capital/overview` | Yes | Capital overview |
| GET | `/api/capital/allocation` | Yes | Capital allocation |
| PUT | `/api/capital/allocation` | Yes | Update allocation |
| GET | `/api/capital/allocation/:id` | Yes | Strategy capital detail |
| PUT | `/api/capital/allocation/:id` | Yes | Update strategy capital |
| POST | `/api/capital/allocation/:id/lock` | Yes | Lock strategy capital |
| POST | `/api/capital/rebalance` | Yes | Rebalance capital |
| GET | `/api/capital/history` | Yes | Capital history |
| PUT | `/api/capital/reserve` | Yes | Set reserve capital |

---

## Webhooks (no auth; signature verified)

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/billing/webhook/stripe` | Stripe webhook |
| POST | `/api/payment/crypto/webhook/coinbase` | Coinbase webhook |

---

## WebSocket

| Path | Description |
|------|-------------|
| GET | `/ws` – WebSocket connection for real-time updates (e.g. prices, orders). |

---

## Other Endpoints

- **Prometheus**: `GET /metrics` (no `/api`; no auth).
- **pprof**: Under `/debug/pprof/*` when `web.pprof.enabled` is true; may require auth and/or IP allowlist.

---

## Errors

- `401`: Unauthorized (missing or invalid auth).
- `403`: Forbidden (e.g. pprof IP not allowed).
- `404`: Not found.
- `500`: Server error; check response body and logs.

Response body for errors is typically JSON, e.g. `{ "error": "message" }`.
