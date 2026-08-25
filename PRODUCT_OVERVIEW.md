# Product Overview

> 最后更新：2026-08-25 | 当前版本：v3.110.0-rc1

## 项目简介

QuantMesh 是一个面向量化交易运行、监控、回测、风控和 AI 辅助排障的 Go + React 一体化系统，支持将前端构建产物嵌入后端二进制部署。

## 核心功能

- Bot 列表、Bot 工作区、交易面板、持仓、订单、策略槽位和配置管理。
- 全局看板、全局持仓、策略市场、收益统计、风险监控、事件中心和运行日志。
- 回测、数据导出、K 线文件管理、资金费率、基差监控、震荡分析和新闻分析。
- 盈利管理、资金管理、FIX 管理、服务状态、首次设置向导和个人资料。
- AI 配置助手：支持 Gemini、OpenAI、Claude/Anthropic、DashScope 北京/新加坡、Kimi 中国站/国际站、DeepSeek 和自定义 OpenAI 兼容中转站生成交易配置建议。
- MCP 服务：通过 `/mcp` 暴露只读/受控写入工具，支持工具能力地图、工具帮助、健康报告和跨域实体搜索。
- 左侧菜单搜索：支持按菜单名称、分组和路径快速命中并跳转。

## 技术栈

- 后端：Go + Gin + SQLite/MySQL/PostgreSQL 存储适配。
- 前端：React + TypeScript + Vite + Chakra UI + i18next。
- 自动化：Go test、Vitest、Ruby 自动化测试脚本。
- 部署：Go embed 打包前端，单二进制运行；也支持通过 SSH/反向代理访问。

## API 概览

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/version` | 返回当前应用版本 |
| GET/POST | `/mcp` | Model Context Protocol endpoint |
| GET | `/api/status` | 服务状态 |
| GET | `/api/bots` | Bot 列表 |
| GET | `/api/orders` | 订单查询 |
| GET | `/api/positions` | 持仓查询 |
| GET | `/api/logs` | 日志查询 |

## 部署

- 前端构建：`cd webui && yarn build`
- 后端测试：`go test ./...`
- 本地运行：`go run main.go`
- 生产部署：构建后将服务二进制复制到服务器，由 systemd 或同类守护进程托管。

## 已知问题 / 待办

- [ ] 继续补齐 MCP 工具的业务工作流示例和更细粒度审计能力。
- [ ] 左侧菜单搜索后续可加入拼音/别名索引，提升中文输入命中率。
- [ ] 多语言资源中部分非中英文 locale 仍依赖英文 fallback。
