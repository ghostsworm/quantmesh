# K 线文件统一管理功能说明

## 功能概述

成功实现了 K 线文件的统一管理，解决了之前数据存储割裂的问题：

### ✅ 统一存储方案
- **统一目录**: 所有 K 线文件统一存放在 `./data/kline`
- **统一元信息**: 通过数据库表 `kline_files` 记录文件状态、时间范围、是否带深度等
- **状态管理**: 区分 `collecting`（采集中）、`completed`（已完成）、`error`（出错）三种状态

### 🔧 主要改进

1. **数据源整合**
   - KlineCollector 采集的实时数据（按日拆分）
   - 回测时从交易所拉取的缓存数据（按时间段）
   - 手动导入的数据文件

2. **智能状态管理**
   - 采集中的文件（当天）: `status=collecting`，不可用于回测
   - 已完成的文件: `status=completed`，可用于回测和参数优化
   - 自动检测和更新文件状态（每小时检查）

3. **向后兼容**
   - 保持现有文件格式和命名规则
   - 回测缓存自动从 `backtest/cache` 迁移到统一目录
   - 现有 API 功能不受影响

## 新增功能

### 📊 数据库表: `kline_files`
```sql
CREATE TABLE kline_files (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    filename TEXT UNIQUE NOT NULL,           -- 文件名（不含路径）
    exchange TEXT NOT NULL,                  -- 交易所 (binance, bitget)
    symbol TEXT NOT NULL,                    -- 交易对 (BTCUSDT)
    interval TEXT NOT NULL,                  -- K线周期 (tick, 1m, 1h, 1d)
    start_time TIMESTAMP NOT NULL,           -- 数据开始时间
    end_time TIMESTAMP,                      -- 数据结束时间（采集中为 NULL）
    status TEXT NOT NULL DEFAULT 'collecting', -- collecting | completed | error
    has_depth INTEGER NOT NULL DEFAULT 0,    -- 是否带深度数据 (0/1)
    candle_count INTEGER DEFAULT 0,          -- K线条数
    file_size INTEGER DEFAULT 0,             -- 文件大小（字节）
    source TEXT NOT NULL,                    -- 数据来源: collector | backtest_cache | manual
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

### 🌐 新 API: `/api/kline-files/available`
返回仅 `status=completed` 的可用文件，支持按交易所、交易对、周期过滤：

```json
{
  "success": true,
  "files": [
    {
      "id": 1,
      "filename": "binance_BTCUSDT_1m_2026-01-01_2026-01-31.csv",
      "exchange": "binance",
      "symbol": "BTCUSDT", 
      "interval": "1m",
      "time_range": "2026-01-01 ~ 2026-01-31",
      "has_depth": false,
      "candle_count": 44640,
      "source": "backtest_cache"
    }
  ]
}
```

### 🎯 前端改进
- 回测页面的 K 线文件选择现在仅显示已完成的文件
- 显示文件时间范围、K 线数量、是否带深度、数据来源等详细信息
- 采集中的文件会被过滤，避免用户误选导致回测失败

## 自动化流程

1. **KlineCollector 采集时**:
   - 写入文件 → 记录到数据库（`status=collecting`）
   - 每小时检查昨天的文件 → 更新状态为 `completed`

2. **回测缓存生成时**:
   - 从交易所拉取数据 → 保存到统一目录 → 迁移脚本会扫描并录入数据库

3. **文件迁移**:
   - 启动时自动运行迁移脚本
   - 扫描现有文件并导入 `kline_files` 表
   - 将 `backtest/cache` 文件移动到统一目录

## 使用场景

### 回测选择文件
用户现在可以：
1. 选择"K线文件"数据源
2. 从已完成的文件列表中选择（显示详细信息）
3. 系统自动校验文件状态，采集中的文件会提示"暂不可用"

### 参数优化
参数优化功能也将获得相同的文件选择能力（后续可扩展）

## 技术细节

### 文件命名规则
- **采集中**: `{interval}_{exchange}_{symbol}_{date}.csv`（如 `1m_binance_BTCUSDT_20260203.csv`）
- **已完成**: 保持原名或重命名为 `{exchange}_{symbol}_{interval}_{start}_{end}.csv`

### 缓存键统一
- 修复了 `getCacheStatus` 和 `GetHistoricalData` 缓存键不一致的问题
- 统一使用格式: `{exchange}_{symbol}_{interval}_{start_date}_{end_date}`

### 状态监控
- 每小时自动检查并更新过期文件状态
- 文件完整性分析（列数、K线数估算、文件大小）

这个统一管理方案彻底解决了之前 K 线数据割裂的问题，为回测和优化提供了可靠的数据源管理机制。