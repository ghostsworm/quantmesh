# 事件中心实现总结

## 实现概述

本次实现为 QuantMesh 系统添加了一个完整的事件中心功能，用于统一管理和展示所有系统事件，包括网络中断、API错误、价格波动等突发事件。

## 主要功能

### 1. 事件类型系统

实现了丰富的事件类型分类：

- **订单相关事件**: 订单下单、成交、取消、失败
- **持仓相关事件**: 持仓开仓、平仓
- **风控相关事件**: 风控触发/恢复、止损/止盈、保证金不足
- **网络相关事件**: WebSocket 断连/重连、API 请求失败、连接超时
- **API 错误事件**: API 限流(429)、服务器错误(5xx)、认证失败
- **价格波动事件**: 价格大幅波动、价格异常
- **系统资源事件**: CPU/内存/磁盘告警
- **系统状态事件**: 系统启动/停止、错误

### 2. 严重程度分级

事件按严重程度分为三级：

- **Critical (严重)**: 影响交易的关键问题，保留1年或100万条
- **Warning (警告)**: 需要关注的告警，保留3个月或50万条
- **Info (信息)**: 一般性信息事件，保留1个月或30万条

### 3. 智能清理机制

- 按严重程度采用不同的保留策略
- 同时支持按时间和按数量保留
- 定期自动清理（默认每24小时）
- 确保重要事件长期保留，一般事件及时清理

### 4. 前端展示

- 事件列表展示（按时间倒序）
- 按事件类型、严重程度、来源筛选
- 实时统计卡片（总数、24小时数、各级别数量）
- 事件详情弹窗（完整信息、JSON格式化）
- 自动刷新（30秒）

### 5. 通知集成

- 复用现有通知系统（Telegram、Webhook、Email）
- Critical 级别事件总是通知
- Warning 级别重要事件通知（API限流、价格波动等）
- Info 级别通常不通知

## 文件清单

### 后端文件

1. **event/event.go** - 事件类型定义和严重程度分级
   - 新增30+事件类型
   - 严重程度自动判断
   - 事件来源和标题辅助函数

2. **event/center.go** - 事件中心核心模块 (新建)
   - 事件订阅和处理
   - 智能消息构建
   - 通知触发逻辑
   - 自动清理任务

3. **database/interface.go** - 数据模型扩展
   - EventRecord 事件记录表
   - EventStats 事件统计结构
   - EventFilter 查询过滤器

4. **database/gorm.go** - 数据库实现
   - SaveEvent/GetEvents/GetEventByID
   - GetEventStats 统计查询
   - CleanupOldEvents 清理方法

5. **config/config.go** - 配置扩展
   - EventCenter 配置结构
   - 保留策略配置
   - 默认值设置

6. **web/api_events.go** - Web API 接口 (新建)
   - GET /api/events - 获取事件列表
   - GET /api/events/:id - 获取事件详情
   - GET /api/events/stats - 获取事件统计

7. **web/server.go** - 路由注册
   - 注册事件中心路由

8. **main.go** - 主程序集成
   - 初始化事件中心
   - 设置事件提供者

9. **event/center_test.go** - 单元测试 (新建)
   - 配置测试
   - 严重程度测试
   - 事件来源测试
   - 事件标题测试

### 前端文件

1. **webui/src/components/EventCenter.tsx** - 事件中心页面 (新建)
   - 事件列表展示
   - 统计卡片
   - 类型筛选
   - 自动刷新

2. **webui/src/components/EventDetailModal.tsx** - 详情弹窗 (新建)
   - 完整事件信息
   - JSON格式化展示

3. **webui/src/services/api.ts** - API 服务集成
   - getEvents/getEventDetail/getEventStats

4. **webui/src/App.tsx** - 路由配置
   - /events 路由

5. **webui/src/components/Sidebar.tsx** - 导航菜单
   - 事件中心菜单项

6. **webui/src/i18n/locales/zh-CN.json** - 中文翻译
7. **webui/src/i18n/locales/en-US.json** - 英文翻译

### 配置文件

1. **config.example.yaml** - 示例配置
   - event_center 配置段
   - 保留策略示例

## 配置说明

```yaml
event_center:
  enabled: true  # 是否启用事件中心
  price_volatility_threshold: 5.0  # 价格波动阈值（百分比）
  monitored_symbols:  # 监控的交易对
    - BTCUSDT
    - ETHUSDT
  
  retention:
    # 按时间保留
    critical_days: 365  # 1年
    warning_days: 90    # 3个月
    info_days: 30       # 1个月
    
    # 按数量保留
    critical_max_count: 1000000  # 100万条
    warning_max_count: 500000    # 50万条
    info_max_count: 300000       # 30万条
  
  cleanup_interval: 24  # 清理间隔（小时）
```

## API 接口

### 1. 获取事件列表

```http
GET /api/events?type=xxx&severity=xxx&source=xxx&limit=100&offset=0
```

**查询参数:**
- type: 事件类型
- severity: 严重程度 (critical/warning/info)
- source: 事件源 (exchange/network/system/api/risk/strategy)
- exchange: 交易所
- symbol: 交易对
- start_time: 开始时间 (RFC3339)
- end_time: 结束时间 (RFC3339)
- limit: 限制数量
- offset: 偏移量

**响应:**
```json
{
  "events": [...],
  "count": 100
}
```

### 2. 获取事件详情

```http
GET /api/events/:id
```

### 3. 获取事件统计

```http
GET /api/events/stats
```

**响应:**
```json
{
  "total_count": 1234,
  "critical_count": 56,
  "warning_count": 234,
  "info_count": 944,
  "last_24_hours_count": 123,
  "count_by_type": {...},
  "count_by_source": {...}
}
```

## 测试结果

### 后端测试

```bash
cd /Users/rocky/Sites/btc/quantmesh-opensource
go test -v ./event/...
```

**测试结果:** ✅ 全部通过
- TestEventCenterBasic: 配置创建测试
- TestEventSeverity: 严重程度判断测试
- TestEventSource: 事件来源判断测试
- TestEventTitle: 事件标题获取测试

### 编译测试

```bash
go build -o /tmp/quantmesh-test
```

**编译结果:** ✅ 成功

## 使用流程

### 1. 启动系统

系统启动时会自动：
- 初始化事件中心
- 创建 EventRecord 数据表
- 启动事件处理协程
- 启动清理任务

### 2. 事件自动采集

系统运行时会自动采集各类事件：
- 订单、持仓变化
- 网络连接状态
- API 调用错误
- 价格波动
- 系统资源状态

### 3. 查看事件

访问前端页面：
1. 点击侧边栏"事件中心"菜单
2. 查看事件列表和统计
3. 使用筛选功能过滤事件
4. 点击事件查看详情

### 4. 通知接收

重要事件会通过配置的通知渠道发送：
- Telegram Bot
- Webhook
- Email

## 扩展建议

未来可以考虑的增强功能：

1. **实时 WebSocket 推送**: 新事件实时推送到前端
2. **事件搜索**: 支持关键词搜索事件
3. **事件导出**: 导出事件记录为CSV/JSON
4. **事件趋势图**: 展示事件发生趋势
5. **自定义告警规则**: 用户自定义监听规则
6. **事件关联分析**: 分析事件之间的关联关系
7. **智能降噪**: 相似事件合并，减少噪音

## 注意事项

1. **数据库性能**: 事件表可能增长很快，建议定期监控数据库性能
2. **磁盘空间**: 确保有足够的磁盘空间存储事件数据
3. **清理策略**: 根据实际情况调整保留策略参数
4. **通知频率**: 避免过于频繁的通知，建议使用冷却机制

## 完成时间

2025年1月7日

## 作者

AI Assistant (Claude Sonnet 4.5)

