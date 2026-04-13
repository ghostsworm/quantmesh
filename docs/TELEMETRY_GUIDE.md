# 使用统计说明（遥测）

## 当前行为

- **PostHog 及同类第三方产品分析已从本仓库移除**：`utils/telemetry.go` 中的导出函数保留为**空实现**，便于旧代码与调用方继续编译；**不会**向外部 URL 发送 JSON 事件。
- **安装脚本** `scripts/install.sh` 中的 `send_install_telemetry` **不再发起**任何网络请求。
- **Web 前端**（`webui/src/services/telemetry.ts`）在应用初始化时可能加载一枚 **1×1 像素**，用于与文档中一致的粗粒度访问量统计；**不**再初始化 PostHog SDK。

## 如何关闭前端像素

在 `webui/.env.local` 中：

```bash
VITE_DISABLE_TELEMETRY=1
```

或在浏览器控制台 / 应用内将 `localStorage` 项 `QUANTMESH_DISABLE_TELEMETRY` 设为 `1`。

环境变量 `QUANTMESH_DISABLE_TELEMETRY=1` 仍被读取，与历史文档一致；当前后端已无出站遥测，保留该变量不影响行为。

---

## English

- **PostHog and similar third-party product analytics have been removed.** Exported helpers in `utils/telemetry.go` are **no-ops** for compatibility; nothing is POSTed to external analytics endpoints.
- The install script’s `send_install_telemetry` **does not** send network traffic.
- The Web UI may still load a **1×1 pixel** on init for coarse usage counts; see `webui/src/services/telemetry.ts`. Disable with `VITE_DISABLE_TELEMETRY=1` in `webui/.env.local` or `localStorage` key `QUANTMESH_DISABLE_TELEMETRY` = `1`.
