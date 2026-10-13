# RC1187 驗收標準

## 必須滿足

0. Storage、FundingCarry takeover/retry/projection、income replay 與 Web API durable readback 全部使用同一 exported semantic/legacy hash verifier；不得殘留對 `ProjectionPayload` 的原始 SHA-only 校驗。
0a. `MarkRecoveryProjected` 要拒絕與 durable outbox 不同的 `ExpectedRuntimeRevision`；receipt-only hash/revision mismatch 亦拒絕，episode、receipt、outbox、cursor、ledger 與 hold 均不變。
0b. FundingCarry runtime projection CAS 前及所有 pending/completed response-loss replay 成功回傳前，必須核對 receipt/outbox/episode 的身份、revision、狀態與 hash 錨點；receipt-only mismatch 不得先寫 runtime。
0c. projected receipt `target+2` 只能由檢查當前 episode 完整前態、精確 owner-takeover 記錄及該 takeover 操作 receipt 的 income 專用流程接受；須按授權欄位、owner generation、scope 重算並匹配 takeover request hash，並要求 record/receipt 審計時間非零。其他 replay 不得僅憑 revision 數字接受。completed takeover recovery 必須驗證候選舊 outbox 的對應 receipt。
0d. 新寫入 outbox 的 `ExpectedRuntimeRevision` 必須位於 `ProjectionHash` 覆蓋的完整 projection 中，並在 receipt/outbox 配對及 takeover candidate/pending 校驗時與外層 CAS 元數據相等；持久化層若只改寫外層 revision，生產 projector 必須在 runtime CAS 前拒絕且無副作用。歷史 payload 若沒有該綁定字段，仍可讀取/驗證舊 payload hash，但自動投影與成功重放必須 fail-closed，要求人工核帳。
0b. 透過 FundingCarry 生產 recovery/retry 路徑驗證新的 semantic payload 投影及舊 raw-byte hash pending payload 恢復；不得只直接測 Storage helper。

1. MySQL 原生 JSON round-trip 導致的單純序列化正規化不會讓有效 `RecoveryOutbox` payload hash 失配。
2. 正規化/重序列化必須涵蓋完整投影（episode、children、events、source_cursor），或對不支援字段明確拒絕；不得靜默丟棄未知字段。
3. episode、children、events、source cursor 等有意義內容任一被改而可信 hash anchor 不變時必須拒絕；確認 receipt/outbox hash 的錨定關係，並明確 SHA-256 的威脅模型。不得聲稱普通 hash 可防止同時竄改 payload 與所有 hash 副本的資料庫寫入者。
3a. 對 payload 新增未知字段並重算為匹配的 semantic hash，仍必須拒絕，不能由 typed unmarshal 靜默丟棄。
4. canonical JSON 測試必須證明不經 float64 的精確數字邊界、等價數值表示、object key order/whitespace、數組順序、duplicate key/多值拒絕，以及 amount/time 字串字面語義。
5. RC1186/先前版本 pending outbox 的 payload hash 仍可供稽核讀取，無需清空或重建數據；因舊格式未將 `ExpectedRuntimeRevision` 綁入投影哈希，舊記錄禁止自動投影/重放並轉人工核帳。新寫入格式才允許自動恢復。
6. MySQL seed/cursor projection、異常調整競爭與 retry 都通過；只有一個 adjustment 勝者，loser 不產生 receipt/outbox/ledger、推進 cursor 或釋放 hold。
7. 本地 SQLite/storage/全套 Go、race、`go vet ./...`、前端 typecheck/tests/build 通過。
8. 推送後同 SHA MySQL 8.0.36 專項與完整 CI/race/build 均成功；CI 失敗須修復新提交並重跑，不能復用舊 SHA 結果。
9. RC1188 的 MySQL fixtures 必須把 projection payload 寫成 JSON object、把 64 位 projection hash 寫成 JSON string；RC1189 的歷史哈希 helper 必須把 `ExpectedRuntimeRevision` 納入哈希形狀，並由不依賴 MySQL 的單測證明該字段變更會改變哈希。`main.go`、`webui/package.json`、Changelog、必要 Product Overview 版本一致為 `3.111.0-rc1189`。只有修復後新 SHA 的 MySQL 與完整 CI 全綠才可 tag/release。

## 不屬於驗收

- 真實 Binance 賬戶、實盤下單、真實盈虧或回撤驗證。
- 自動釋放 Gate/hold、跳過 receipt/outbox 或忽略哈希錯誤。
- 對 `payload_json` 做會破壞舊 pending 記錄的格式改寫。
