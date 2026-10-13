# RC1187 測試計畫

## RC1190 SQLite 迁移并发连接获取

| 层级 | 测试 | 关键断言 | 执行环境 |
|---|---|---|---|
| 连接获取单测 | 可控 driver 连续返回 SQLite BUSY 后成功 | 重试到成功；非 BUSY SQLite 错误立即返回；MySQL 不重试；错误链保留 | 本地 Go |
| 并发迁移 | 12 个独立冷连接同时初始化当前/旧 schema，重复多轮 | 全部成功、最终 schema 完整、幂等；无锁泄漏 | 本地 Go/race |
| 取消边界 | 持有 SQLite 独占锁并取消调用方 context | 返回及时且可由 `errors.Is` 识别；释放锁后重试成功 | 本地 Go |
| 完整门禁 | 全仓 Go、vet、WebUI verify、CI | 同一候选提交全绿；记录同 SHA 的远端 MySQL/完整 CI 状态 | 本地及 GitHub Actions |

| 層級 | 測試 | 關鍵斷言 | 執行環境 |
|---|---|---|---|
| Hash 單元 | 原始 projection 與 MySQL JSON normalization 等價 payload | 正常化不改驗證結果；episode/child/event/cursor 語義篡改均改變 hash 並被拒絕 | 本地 Go |
| Canonical JSON 表驅動 | object key/空白、等價數字拼寫、超大整數、數組順序、重複鍵、尾隨 JSON、amount/time string | 使用精確整數/十進位正規化，不經 float64；保留數組順序與字串字面語義，拒絕重複鍵/多值輸入 | 本地 Go |
| MySQL durable outbox | 建立 pending outbox，讀回原生 JSON column 後經生產 projector 標記完成 | RC1186 seed projection 通過、cursor 僅按預期 revision 推進；legacy fixture 將 projection payload 作 JSON object 寫入、hash 作 JSON string 寫入，不對裸 hash 做 JSON CAST | CI MySQL 8.0.36 強制 DSN |
| 舊 pending 重啟 | 使用修復前 MySQL JSON round-trip 生成的舊 envelope/payload | 修復版可讀回與投影；不得要求刪除/重寫既有記錄 | MySQL fixture + SQLite compatibility fixture |
| Anomaly CAS | 同 anomaly 不同 operation 並發調整並重試輸贏雙方 | 單一 adjustment；無輸家副作用；cursor 不被錯推進；hold 維持 held | MySQL 8.0.36 |
| 安全負例 | 修改已讀 payload 語義字段、hash 索引值/owner/revision | 檢查返回錯誤/conflict，durable state 無副作用，Gate/hold 不釋放 | SQLite + MySQL |
| 全量門禁 | `go test -p=2 ./... -count=1 -timeout=30m`、`go test -race` 專項、`go vet ./...`、`yarn --cwd webui verify` | 同一候選提交全綠；推送後記錄完整 SHA/Actions links | 本地與 GitHub Actions |
