# 使用统计说明（遥测）

## 当前行为

- **默认不会向 PostHog / Sentry 发送事件**：只有用户在 WebUI「全局设置 → 可观测性上报」主动填写 PostHog Project API Key 或 Sentry DSN 并启用后，后端才会把 `logger.Error/Fatal`、HTTP 5xx、panic 作为错误事件上报。
- `utils/telemetry.go` 中的导出函数仍保留为**空实现**，便于旧代码与调用方继续编译；它不会发送产品分析事件。
- **安装脚本** `scripts/install.sh` 中的 `send_install_telemetry` **不再发起**任何网络请求。
- **Web 前端**（`webui/src/services/telemetry.ts`）在应用初始化时可能加载一枚 **1×1 像素**，用于与文档中一致的粗粒度访问量统计；**不**再初始化 PostHog SDK。

## 如何关闭后端可观测性上报

在 WebUI「全局设置 → 可观测性上报」关闭 PostHog / Sentry 开关，或清除对应 Project API Key / DSN。未配置密钥时相关 reporter 是 no-op。

## 如何关闭前端像素

在 `webui/.env.local` 中：

```bash
VITE_DISABLE_TELEMETRY=1
```

或在浏览器控制台 / 应用内将 `localStorage` 项 `QUANTMESH_DISABLE_TELEMETRY` 设为 `1`。

环境变量 `QUANTMESH_DISABLE_TELEMETRY=1` 仍被读取，与历史文档一致；当前后端已无出站遥测，保留该变量不影响行为。

---

## English

- **Nothing is sent to PostHog / Sentry by default.** Backend error reporting starts only after the user explicitly configures and enables a PostHog Project API Key or Sentry DSN in WebUI Global Settings.
- Exported helpers in `utils/telemetry.go` remain **no-ops** for compatibility; they do not send product analytics events.
- The install script’s `send_install_telemetry` **does not** send network traffic.
- The Web UI may still load a **1×1 pixel** on init for coarse usage counts; see `webui/src/services/telemetry.ts`. Disable with `VITE_DISABLE_TELEMETRY=1` in `webui/.env.local` or `localStorage` key `QUANTMESH_DISABLE_TELEMETRY` = `1`.
