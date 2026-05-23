# QuantMesh 配置迁移指南

本指南用于把旧站点的完整配置迁移到新站点，包括当前运行时配置、数据库中的 `app_config` 和全部 `bot_configs`。

## 迁移范围

迁移包包含：

- `runtime_config`：当前运行时主配置 JSON。
- `runtime_config_yaml`：当前运行时主配置 YAML 视图，便于人工审查。
- `database.app_config`：主库 `app_config` 原始 JSON 文档。
- `database.bot_configs`：主库 `bot_configs` 全量 Bot 配置文档。

迁移包是完整搬站用途，包含未脱敏密钥、交易所凭据、AI key 等敏感配置。必须按密钥文件管理，不要提交到 Git，也不要公开分享。

## 导出旧站配置

登录旧站后调用：

```bash
curl -b cookie.txt \
  -o quantmesh_config_migration.json \
  http://OLD_SITE/api/export/config-migration
```

也可以在浏览器中访问：

```text
/api/export/config-migration
```

下载得到的 JSON 文件就是迁移包。

## 导入新站配置

先确保新站已经完成基础部署，并且主库可用。登录新站后调用：

```bash
curl -b cookie.txt \
  -H "Content-Type: application/json" \
  --data-binary @quantmesh_config_migration.json \
  http://NEW_SITE/api/config/import-migration
```

导入会执行：

- 将迁移包中的 `database.app_config.content` 写入新站主库 `app_config`。
- 将迁移包中的 `database.bot_configs` 逐条写入新站主库 `bot_configs`。
- 同步更新新站运行时配置。
- 如果本地 Bot 文件配置管理器存在，同时写入 `bots/{bot_id}/config.yaml` 作为后备。

## 导入后检查

建议依次检查：

```text
/api/config/json
/api/bots
/api/version
```

如果导入响应中的 `requires_restart` 为 `true`，建议重启新站服务，让所有依赖启动期配置的模块重新初始化。

## 回滚建议

导入前可先在新站导出一次迁移包作为备份：

```bash
curl -b cookie.txt \
  -o quantmesh_config_migration_before_import.json \
  http://NEW_SITE/api/export/config-migration
```

如需回滚，将该备份包重新导入即可。
