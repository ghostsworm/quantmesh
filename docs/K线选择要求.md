# 🕒 K线采样策略指引（Kline Sampling Guide）

## 📘 概要

本文件定义了在执行网格参数优化、回测或实时计算时，  
如何选择适当的 **K线周期（Kline Interval）** 与 **时间跨度（Lookback Range）**。  

选择合适的粒度可以在：
- ⚡ 提升效率的同时  
- ✅ 保证网格策略能准确捕捉波动  
- 🚫 避免误差、过拟合、冗余数据

---

## 🧩 一、核心逻辑

> **K线周期应与网格间距（ΔP）和资产波动率匹配。**

原则公式：

\[
T_{kline} \approx 0.1 \times T_{grid}
\]

即：  
**一根K线的时间跨度 ≈ 一个网格波动周期的 10%–20%**

这确保：
- 回测能捕捉网格成交的动态；
- 不浪费计算；
- 保持指标平滑性。

---

## 📈 二、选择规则（推荐矩阵）

| 资产波动类型 | 日波动幅度 | 网格间距 ΔP | 推荐K线周期 | 推荐回测跨度 | 示例资产 |
|---------------|-------------|--------------|---------------|---------------|-----------|
| 高频（极高） | >8% | 0.1%~0.3% | 1 分钟 (1m) | 7~30 天 | BTC, SOL, DOGE |
| 中高波动 | 3%~8% | 0.3%~0.6% | 5 分钟 (5m) | 30~60 天 | ETH, BNB |
| 中频 | 1%~3% | 0.5%~1.0% | 15~30 分钟 | 60~120 天 | ADA, LTC |
| 低波动 | <1% | 1%~2% | 1 小时 (1h) | 90~180 天 | XAUUSDT, PAXGUSDT |
| 稳定型 | ≈0% | 无法形成网格 | 不建议执行 | - | USDC/USDT 等 |

---

## ⚙️ 三、数据采样建议

1. **初步优化阶段（探索最优区间）**
   - 使用低频数据（1h 或 4h）
   - 可快速搜索全局参数空间；
   - 推荐用于稳定型标的（PAXG、黄金）。

2. **局部精细化阶段**
   - 使用 5min~15min 数据；
   - 聚焦高收益参数区域；
   - 利于捕捉短期震荡。

3. **高频自优化阶段**
   - 针对 BTC/ETH 等高波动标的；
   - 使用 1min 数据；
   - 需注意内存与速度优化。

---

## 🧮 四、自动判断逻辑（供AI系统调用）

AI 可使用以下规则选择合适的粒度：

```python
def choose_kline_interval(symbol, volatility, grid_gap_percent):
    if grid_gap_percent < 0.3 or volatility > 5:
        return "1m", 30  # 高频，回测30天
    elif grid_gap_percent < 0.6 or volatility > 3:
        return "5m", 60
    elif grid_gap_percent < 1.0 or volatility > 1:
        return "15m", 90
    elif grid_gap_percent < 2.0:
        return "1h", 180
    else:
        return "4h", 360

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
