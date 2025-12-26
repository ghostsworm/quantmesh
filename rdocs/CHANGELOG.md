# 更新日志 (Changelog)

本文档记录 OpenSQT Market Maker 项目的所有重要功能变更、算法调整和版本更新。

## 版本规范

- 每次功能变更或算法调整都会打一个 Git Tag
- 版本号格式：`v{major}.{minor}.{patch}` (例如：v1.0.0)
- 每个版本记录包含：版本号、Git Tag、更新时间、变更内容

---

## [未发布] - Unreleased

### 新增
- 创建 CHANGELOG.md 文件，建立版本管理规范
- 添加 ReduceOnly 订单错误自动处理机制
- 新增 `BatchPlaceOrdersWithDetails()` 方法，返回详细的订单执行结果
- 新增 `isReduceOnlyError()` 错误检测函数
- 添加存储服务停止状态检查机制
- 添加 SQLite 数据库关闭状态标记
- **新增个人资料页面**，支持修改密码和管理 WebAuthn 凭证
- 新增修改密码 API (`POST /api/auth/password/change`)
- **引入 Tailwind CSS 现代化 UI 框架**，提升前端开发效率和界面美观度
- **新增亏损率显示功能**，在持仓概览页面显示相对于持仓成本的盈亏百分比
- **新增K线图页面**，支持查看当前交易币种的K线数据和成交量，支持时间周期切换（1m/5m/15m/30m/1h/4h/1d），使用 lightweight-charts 库渲染专业级K线图表
- 新增K线数据API (`GET /api/klines`)，支持查询历史K线数据

### 修复
- 修复 ReduceOnly 订单被拒绝时持续重试的问题（币安 API 错误码 -2022）
- 修复本地槽位持仓状态与交易所实际持仓不同步的问题
- **修复退出时数据库写入失败的问题**（`sql: database is closed` 错误）
- 修复首次设置密码后未自动登录的问题
- 修复注册指纹时提示"未登录"的问题
- **修复首次登录设置密码后反复要求设置密码的问题**
- 修复日志页面缺少实时订阅函数导致 `/logs` 页面报错的问题
- **修复 session_id Cookie 因 Base64 填充符导致会话查找失败的问题**
- **修复前端命名遮蔽导致设置密码请求未发送的问题**
- **实现 WebAuthn 注册完成功能**
- **修复前端密码设置请求未发送的问题（state setter 覆盖了 API 方法）**
- **修复会话 ID 在 Cookie 中被转义导致无法识别的问题（去除 Base64 填充）**

### 变更
- 改进订单执行器错误处理逻辑，ReduceOnly 错误不再重试
- 增强仓位管理器自动修复能力，检测到 ReduceOnly 错误时自动清空槽位状态
- 优化批量下单接口，支持返回 ReduceOnly 错误详情
- **优化系统退出流程**，调整组件关闭顺序，确保数据完整性
- 改进存储服务关闭逻辑，防止在数据库关闭后继续写入
- 首次设置密码后自动创建会话并登录
- **优化首次设置流程**，使用 sessionStorage 跟踪设置状态，确保密码设置后能继续 WebAuthn 注册
- **将会话 Cookie 的 SameSite 模式从 Strict 改为 Lax**，提高同站请求的兼容性
- **前端 API 基址改为同源绝对地址**，避免代理/扩展劫持相对路径导致设置密码请求未发送

### 技术细节
- 修改文件：
  - `order/executor_adapter.go`: 添加 ReduceOnly 错误检测和处理
  - `position/super_position_manager.go`: 自动清空无效持仓槽位
  - `strategy/multi_strategy_executor.go`: 支持详细错误结果传递
  - `strategy/executor_adapter.go`: 适配新接口
  - `main.go`: 优化退出流程，调整组件关闭顺序
  - `storage/storage.go`: 添加停止状态检查，改进 Stop/Save/batchSave 方法
  - `storage/sqlite.go`: 添加关闭状态标记，防止重复关闭
  - `web/api_auth.go`: 设置密码后自动创建会话
  - `web/session_manager.go`: 将 Cookie SameSite 模式改为 Lax，添加延迟确保 Cookie 处理
  - `web/session_manager.go`: SessionID 使用 RawURLEncoding（无 '=' 填充），避免 Cookie 转义导致会话查找失败
  - `webui/src/components/FirstTimeSetup.tsx`: 使用 sessionStorage 跟踪设置流程状态，添加延迟确保 Cookie 被浏览器处理，修复密码 state setter 覆盖 API 方法的问题
  - `webui/src/App.tsx`: 改进路由逻辑，支持首次设置流程中的状态跟踪
  - `webui/src/services/auth.ts`: API 基址改为同源绝对地址，禁用缓存，确保设置密码请求必发出；改进错误处理，非 2xx 响应会抛出详细错误
  - `webui/src/services/api.ts`: API 基址改为同源绝对地址，避免代理/扩展对相对路径的劫持
  - `webui/src/services/api.ts`: 新增日志 WebSocket 订阅函数 `subscribeLogs`，用于实时接收日志流
  - `webui/src/components/Logs.tsx`: 引入订阅函数，恢复日志页面实时显示能力
  - `webui/src/components/FirstTimeSetup.tsx`: 修复本地 state setter 遮蔽 API 函数的问题，改进错误处理逻辑
  - `web/session_manager.go`: 改用 RawURLEncoding 生成 sessionID，避免 Base64 填充符在 Cookie 中被转义
  - `web/api_webauthn.go`: 实现 `finishWebAuthnRegistration` 函数，完成 WebAuthn 注册流程
  - `webui/src/components/Profile.tsx`: 新增个人资料页面组件，支持修改密码和管理 WebAuthn 凭证
  - `webui/src/components/Profile.css`: 个人资料页面样式
  - `webui/src/App.tsx`: 添加个人资料路由和导航链接
  - `web/api_auth.go`: 新增 `changePassword` 函数，实现修改密码功能
  - `web/server.go`: 添加修改密码路由
  - `webui/src/services/auth.ts`: 新增 `changePassword` 函数
  - `webui/package.json`: 添加 Tailwind CSS、PostCSS 和 Autoprefixer 依赖
  - `webui/tailwind.config.js`: 创建 Tailwind CSS 配置文件，配置内容扫描路径
  - `webui/postcss.config.js`: 创建 PostCSS 配置文件，集成 Tailwind 和 Autoprefixer
  - `webui/src/index.css`: 添加 Tailwind CSS 指令（@tailwind base/components/utilities），保留现有基础样式
- 新增文档：
  - `rdocs/ReduceOnly错误处理说明.md`
  - `rdocs/退出流程优化说明.md`

### 退出流程优化详情
1. **新的关闭顺序**：
   - 第一优先级：撤销所有订单
   - 第二优先级：优雅停止各个组件（价格监控、订单流、风控等）
   - 第三优先级：取消 context（停止事件处理协程）
   - 等待 500ms 让事件队列处理完毕
   - 第四优先级：停止存储服务（关闭数据库）
   - 等待 200ms 让最后的写入完成

2. **存储服务改进**：
   - 添加 `stopped` 状态标记，防止在停止后接受新事件
   - `Stop()` 方法先取消 context，等待事件处理完，再关闭数据库
   - `Save()` 方法检查服务状态，停止后直接返回
   - `batchSave()` 方法检测数据库关闭错误并优雅处理

3. **数据完整性保证**：
   - 确保所有事件都被正确保存到数据库
   - 防止数据库关闭后继续写入导致的错误
   - 优雅处理关闭过程中的异常情况

---

## 版本历史

### 示例格式

```markdown
## [v1.0.0] - 2025-12-26

**Git Tag:** `v1.0.0`  
**发布时间:** 2025年12月26日

### 新增 (Added)
- 新功能描述

### 变更 (Changed)
- 功能调整描述
- 算法优化描述

### 修复 (Fixed)
- Bug 修复描述

### 移除 (Removed)
- 移除的功能描述

### 安全 (Security)
- 安全相关更新
```

---

## 变更类型说明

- **新增 (Added)**: 新增的功能
- **变更 (Changed)**: 对现有功能的变更或算法调整
- **弃用 (Deprecated)**: 即将移除的功能
- **移除 (Removed)**: 已移除的功能
- **修复 (Fixed)**: Bug 修复
- **安全 (Security)**: 安全相关的修复或更新

---

## 注意事项

1. 每次发布新版本前，将 `[未发布]` 部分的内容移动到新版本记录中
2. 确保每个版本都有对应的 Git Tag
3. 记录时间格式：YYYY年MM月DD日
4. 重要的算法调整需要详细说明调整原因和预期效果
5. 破坏性变更需要特别标注 **[BREAKING CHANGE]**

