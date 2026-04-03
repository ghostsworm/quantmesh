# 分级限额配置指南

> **说明：** 运行时的权威配置在主库 **`app_config`**。下述 YAML 片段适用于 **Web 控制台编辑** 或 **一次性导入 YAML**；若仍使用磁盘路径，仅为运维示例，非强制文件名。

## 快速修改配置

在 Web 或主配置 YAML 中找到 `position_allocation` 部分（示例路径如 `/opt/quantmesh/config.yaml` 仅作参考）：

```yaml
position_allocation:
    enabled: true
    allocations:
        - exchange: binance
          symbol: BTCUSDT
          max_amount_usdt: 5000  # 🔧 正常限额（可修改）
          max_percentage: 100
          tiered_limits:
            enabled: true  # 🔧 是否启用分级限额（true/false）
            emergency_limit: 8000  # 🔧 紧急限额（可修改）
            triggers:
              price_drop_percent: 10   # 🔧 价格下跌触发阈值（%）
              position_layers: 20      # 🔧 持仓层数触发阈值
              unrealized_loss_usd: 500 # 🔧 亏损触发阈值（USDT）
            recovery:
              price_recover_percent: 5  # 🔧 价格恢复阈值（%）
              cooldown_seconds: 300     # 🔧 冷却时间（秒）
            notification:
              on_trigger: true   # 🔧 触发时是否通知
              on_recovery: true  # 🔧 恢复时是否通知
```

## 修改步骤

> **推荐**：优先在 **Web 控制台** 修改并保存（写入 **`app_config`**）。以下「SSH 直接改磁盘 YAML」为运维备选；若只改文件未 **`./quantmesh --migrate-app-config`** 且未在 Web 保存，进程仍可能使用库内旧配置。

### 方法一：SSH 直接修改

```bash
# 1. 连接服务器
ssh root@facev.app

# 2. 备份配置文件
cd /root/quantmesh
cp config.yaml config.yaml.backup.$(date +%Y%m%d_%H%M%S)

# 3. 编辑配置文件
nano config.yaml
# 或使用 vim
vim config.yaml

# 4. 修改完成后，重启服务
systemctl restart quantmesh

# 5. 查看日志确认配置生效
journalctl -u quantmesh -f | grep "资金分配"
```

### 方法二：使用脚本批量修改

创建修改脚本 `/root/quantmesh/update_limits.sh`：

```bash
#!/bin/bash

# 修改配置参数
NORMAL_LIMIT=5000      # 正常限额
EMERGENCY_LIMIT=8000   # 紧急限额
PRICE_DROP=10          # 价格下跌触发阈值
POSITION_LAYERS=20     # 持仓层数触发阈值
LOSS_USD=500           # 亏损触发阈值
PRICE_RECOVER=5        # 价格恢复阈值
COOLDOWN=300           # 冷却时间

cd /root/quantmesh

# 备份
cp config.yaml config.yaml.backup.$(date +%Y%m%d_%H%M%S)

# 使用 Python 修改配置
python3 << EOF
import yaml

with open("config.yaml", "r", encoding="utf-8") as f:
    config = yaml.safe_load(f)

# 修改配置
config["position_allocation"]["allocations"][0]["max_amount_usdt"] = $NORMAL_LIMIT
config["position_allocation"]["allocations"][0]["tiered_limits"]["emergency_limit"] = $EMERGENCY_LIMIT
config["position_allocation"]["allocations"][0]["tiered_limits"]["triggers"]["price_drop_percent"] = $PRICE_DROP
config["position_allocation"]["allocations"][0]["tiered_limits"]["triggers"]["position_layers"] = $POSITION_LAYERS
config["position_allocation"]["allocations"][0]["tiered_limits"]["triggers"]["unrealized_loss_usd"] = $LOSS_USD
config["position_allocation"]["allocations"][0]["tiered_limits"]["recovery"]["price_recover_percent"] = $PRICE_RECOVER
config["position_allocation"]["allocations"][0]["tiered_limits"]["recovery"]["cooldown_seconds"] = $COOLDOWN

with open("config.yaml", "w", encoding="utf-8") as f:
    yaml.dump(config, f, allow_unicode=True, default_flow_style=False, sort_keys=False)

print("✅ 配置已更新")
print(f"正常限额: $NORMAL_LIMIT USDT")
print(f"紧急限额: $EMERGENCY_LIMIT USDT")
print(f"价格下跌触发: $PRICE_DROP%")
print(f"持仓层数触发: $POSITION_LAYERS 层")
print(f"亏损触发: $LOSS_USD USDT")
print(f"价格恢复: $PRICE_RECOVER%")
print(f"冷却时间: $COOLDOWN 秒")
EOF

# 重启服务
systemctl restart quantmesh

echo "✅ 服务已重启，查看日志："
journalctl -u quantmesh --since "10 seconds ago" | grep "资金分配"
```

使用方法：

```bash
# 赋予执行权限
chmod +x /root/quantmesh/update_limits.sh

# 执行脚本
./update_limits.sh
```

## 配置参数说明

### 1. max_amount_usdt（正常限额）

**作用**：日常交易的资金限额

**建议值**：
- 小资金（< 1万）：1000-3000 USDT
- 中等资金（1-5万）：3000-10000 USDT
- 大资金（> 5万）：10000-50000 USDT

**当前值**：5000 USDT

### 2. emergency_limit（紧急限额）

**作用**：市场大跌时自动放宽的限额

**建议值**：正常限额的 1.5-2 倍
- 保守：1.5 倍（如 5000 → 7500）
- 中等：1.6 倍（如 5000 → 8000）
- 激进：2 倍（如 5000 → 10000）

**当前值**：8000 USDT（1.6 倍）

### 3. price_drop_percent（价格下跌触发阈值）

**作用**：价格下跌超过此百分比时触发紧急限额

**建议值**：
- 保守：15-20%（大跌才触发）
- 中等：10-15%（适中）
- 激进：5-10%（小跌就触发）

**当前值**：10%

**示例**：
- 锚点价格 90,000 USDT
- 触发阈值 10%
- 触发价格：90,000 × (1 - 10%) = 81,000 USDT

### 4. position_layers（持仓层数触发阈值）

**作用**：持仓层数达到此值时触发紧急限额

**建议值**（根据价格间隔）：
- 价格间隔 150 USDT：15-20 层
- 价格间隔 100 USDT：20-30 层
- 价格间隔 50 USDT：30-50 层

**当前值**：20 层（价格间隔 150 USDT）

**计算方法**：
- 20 层 × 150 USDT = 3000 USDT 跌幅
- 如果锚点价格 90,000，触发价格约 87,000

### 5. unrealized_loss_usd（亏损触发阈值）

**作用**：未实现亏损超过此值时触发紧急限额

**建议值**：正常限额的 10-20%
- 保守：20%（如 5000 → 1000）
- 中等：10%（如 5000 → 500）
- 激进：5%（如 5000 → 250）

**当前值**：500 USDT（10%）

### 6. price_recover_percent（价格恢复阈值）

**作用**：价格恢复到下跌此百分比以内时才能恢复正常限额

**建议值**：
- 保守：2-3%（价格基本恢复）
- 中等：5%（适中）
- 激进：8-10%（还在下跌也恢复）

**当前值**：5%

**示例**：
- 锚点价格 90,000 USDT
- 恢复阈值 5%
- 恢复价格：90,000 × (1 - 5%) = 85,500 USDT

### 7. cooldown_seconds（冷却时间）

**作用**：触发紧急限额后，至少等待此时间才能恢复正常限额

**建议值**：
- 短期：180-300 秒（3-5 分钟）
- 中期：600-900 秒（10-15 分钟）
- 长期：1800+ 秒（30 分钟+）

**当前值**：300 秒（5 分钟）

**目的**：防止价格频繁波动导致限额频繁切换

## 常用配置方案

### 方案一：保守配置（低风险）

```yaml
max_amount_usdt: 5000
tiered_limits:
  enabled: true
  emergency_limit: 7500  # 仅提升 50%
  triggers:
    price_drop_percent: 15   # 价格下跌 15% 才触发
    position_layers: 25      # 持仓 25 层才触发
    unrealized_loss_usd: 1000 # 亏损 1000 USDT 才触发
  recovery:
    price_recover_percent: 3  # 价格恢复到下跌 3% 以内
    cooldown_seconds: 900     # 冷却 15 分钟
```

### 方案二：中等配置（平衡）⭐ 当前使用

```yaml
max_amount_usdt: 5000
tiered_limits:
  enabled: true
  emergency_limit: 8000  # 提升 60%
  triggers:
    price_drop_percent: 10   # 价格下跌 10% 触发
    position_layers: 20      # 持仓 20 层触发
    unrealized_loss_usd: 500 # 亏损 500 USDT 触发
  recovery:
    price_recover_percent: 5  # 价格恢复到下跌 5% 以内
    cooldown_seconds: 300     # 冷却 5 分钟
```

### 方案三：激进配置（高风险高收益）

```yaml
max_amount_usdt: 5000
tiered_limits:
  enabled: true
  emergency_limit: 10000  # 提升 100%
  triggers:
    price_drop_percent: 5    # 价格下跌 5% 即触发
    position_layers: 15      # 持仓 15 层触发
    unrealized_loss_usd: 300 # 亏损 300 USDT 触发
  recovery:
    price_recover_percent: 8  # 价格恢复到下跌 8% 以内
    cooldown_seconds: 600     # 冷却 10 分钟
```

## 实时监控

### 查看当前限额状态

```bash
# 查看初始化日志
journalctl -u quantmesh | grep "资金分配.*初始化"

# 查看当前限额
journalctl -u quantmesh | grep "限额" | tail -5

# 查看触发记录
journalctl -u quantmesh | grep "触发紧急限额"

# 查看恢复记录
journalctl -u quantmesh | grep "恢复正常限额"
```

### 实时监控限额变化

```bash
# 实时查看资金分配相关日志
journalctl -u quantmesh -f | grep --color=auto "资金分配\|限额\|紧急"
```

## 临时禁用分级限额

如果想临时禁用分级限额功能，只需修改：

```yaml
tiered_limits:
  enabled: false  # 改为 false
```

然后重启服务：

```bash
systemctl restart quantmesh
```

系统将始终使用正常限额（max_amount_usdt）。

## 故障排查

### 问题1：修改配置后没有生效

**解决方法**：
1. 检查 YAML 格式是否正确（缩进必须用空格，不能用 Tab）
2. 确认已重启服务：`systemctl restart quantmesh`
3. 查看启动日志：`journalctl -u quantmesh --since "1 minute ago"`

### 问题2：紧急限额一直触发，无法恢复

**可能原因**：
- 价格还没有恢复到恢复阈值
- 冷却时间还没有到

**解决方法**：
- 调低 `price_recover_percent`（如从 5% 改为 8%）
- 缩短 `cooldown_seconds`（如从 300 改为 180）

### 问题3：想要更灵敏的触发

**解决方法**：
- 降低 `price_drop_percent`（如从 10% 改为 5%）
- 降低 `position_layers`（如从 20 改为 15）
- 降低 `unrealized_loss_usd`（如从 500 改为 300）

## 配置与备份位置（权威）

- **运行时权威**：主库表 **`app_config`**（路径取决于 `database.dsn` / `storage`，常见为 `data/quantmesh.db`）。
- **可选磁盘 YAML**：如 `/root/quantmesh/config.yaml`，仅作导入或人机编辑副本；**非**固定 SSOT 文件名。
- **灾备**：以 **数据库备份** +（可选）**导出的 YAML** 为主；若仍保留 `config.yaml.backup.*` 可一并纳入。

## 相关命令（节选）

```bash
# 若存在导入/编辑用 YAML
cat /root/quantmesh/config.yaml | grep -A 20 "position_allocation"

# 备份磁盘 YAML（若使用）
cp /root/quantmesh/config.yaml /root/quantmesh/config.yaml.backup.$(date +%Y%m%d_%H%M%S)

# 重启服务（Web 保存的配置一般已入库；纯改文件需配合迁移或 Web）
systemctl restart quantmesh
systemctl status quantmesh
journalctl -u quantmesh -f
```

## 注意事项

1. **修改前务必备份**：避免配置错误导致服务无法启动
2. **YAML 格式严格**：缩进必须用空格，不能用 Tab
3. **修改后必须重启**：配置不会自动生效
4. **监控日志**：确认配置已正确加载
5. **逐步调整**：不要一次性改动太大，建议小步调整观察效果

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
