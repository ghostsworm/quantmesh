# Goal Teams 計畫：啟動掛單終態恢復閉環

## 目標與狀態

持續修復盈利準備度審查中未閉合的啟動訂單恢復高風險問題。此切片只處理：已確認屬於目前 Bot/帳戶/交易對的恢復意圖，在交易所掛單終態後如何安全完成策略帳、成交帳與 ExposureBook 啟動核帳。不得因掛單終態本身推定經濟帳已結算；不得對未知/外部委託自動撤單或解除封鎖；不得宣稱真實實盤或盈利已驗收。

狀態：使用者於 2026-10-09 確認初始分工，並於 2026-10-10 確認擴展跨層完整實施及正成交/部分成交採 fills/fees durable-first。需求卡、第一輪獨立審查、測試計畫已完成；擴展 backend/executor/storage/runtime 七文件與獨立 QA 測試範圍已確認。後端首輪依窄範圍停止後已收到明確擴展授權，正重新進入實施。

## 環境及版本

| 項目 | 當前證據 | 處理方式 |
|---|---|---|
| 指引 | 根目錄 `AGENTS.md` 存在 | 簡體中文回覆；bugfix 遞增 rc；後端/前端版本一致；同步 CHANGELOG 與產品概覽；commit message 繁體中文 |
| 分支/版本 | `main`，`main.go` 與 `webui/package.json` 均為 `3.111.0-rc1155` | 本切片如實作 bugfix，候選版本 `3.111.0-rc1156`；實作前再核對當前值 |
| 工作區 | 僅見未追蹤 `--help/`、`.codex/` | 視為既有使用者資料；不清理、不覆寫。新增此計畫位於新版本子目錄 |
| Changelog/產品概覽 | `CHANGELOG.md`、`product-overview.md` 存在 | 僅在程式實作後依規則更新；本計畫不改版本號 |
| 遠端狀態 | 先前 `git ls-remote` 因環境禁止連線失敗 | 不宣稱本計畫或任何提交已推送 |

## 目前證據與缺口

| 呼叫鏈位置 | 觀察 | 風險 |
|---|---|---|
| `main_exposure.go:115` | 啟動快照只要仍有任何 open order 就拒絕 Seed ExposureBook | 安全封鎖正確，但沒有恢復動作 |
| `symbol_manager.go` 訂單事件回呼 | 所有已驗證歸屬更新先寫入意圖觀察；正成交終態進成交捕獲與策略結算；零成交僅呼叫網格專用 `settleVerifiedGridZeroFill` | 非網格零成交意圖缺少通用結算路徑；可能永久保留 recovery hold |
| `main_intent_journal.go` | 網格零成交結算在 goroutine 中完成，成功只解除局部 `grid_zero_fill_settlement` 門控 | 成功後沒有通知 ExposureBootstrapCoordinator 重跑 |
| `main_exposure_strategy.go` | Coordinator 只有顯式 Retry；策略啟動後、正成交安全結算後、SpotShort reconciliation 成功後才有重試入口 | 未覆蓋零成交終態的完整生產事件路徑；沒有週期性自動重試 |
| `safety/reconciler.go` | 驗證所有活動委託確切屬於目前執行意圖 | 所有權驗證不等於策略經濟帳結算，也不會完成 runtime exposure bootstrap |
| `main_exposure_test.go` | `TestRuntimeExposureBootstrapRetriesAfterStrategyRecoverySettlesIntent` 直接呼叫 bootstrap helper | 未覆蓋真實訂單流回呼到意圖結算再到 bootstrap 的接線 |

上述是程式碼路徑與測試證據，不代表已驗證任何真實帳戶狀態。針對性現有測試曾通過，但沒有覆蓋缺失接線。

## 相對優先級複核

`docs/audits/2026-10-03-remediation-progress.md` 的 rc1143 主線複核記錄 R01–R04 已有相應風控/保護路徑與定向測試，R13 的本地回環及初始化 fail-closed 路徑也有針對性 race 測試；它同時把 R12/R09「沒有把自有活動委託接入耐久 exposure reservation 並完成策略經濟核帳後的恢復閉環」列為明確未完成項。當前重新執行四項 R13 非 race 測試亦通過。生產反向代理/實際部署仍未驗收，但現有證據支持先推進 R12/R09 恢復切片；不表示其他 R01–R15 已全數完成。

## 恢復安全契約（待獨立需求分析確認）

1. 只有精確匹配當前 journal scope、owner、symbol、client order ID/venue order ID 的訂單更新可進入本 Bot 恢復處理。
2. 非終態、UNKNOWN、身份衝突、累計成交回退/不一致、交易所查詢失敗、策略帳落盤失敗或成交帳證據不足時，維持相關 gate；不得 Seed ExposureBook。
3. 零成交終態需重新向交易所查證終態及零成交，且依策略類型完成持久化結算；事件重複必須冪等。成功後才可請求重新執行完整啟動核帳。
4. 正成交終態沿用既有順序：交易所成交證據 → 成交費用/成交帳持久化 → 對應策略的持倉/資金帳持久化 → 意圖結算 → 重新跑完整啟動核帳。不得把本地 websocket 終態直接當成完整證明。
5. 重試 bootstrap 仍須取得現有提交凍結/快照協調，重查持倉與全部委託、驗證完整恢復庫存並成功 Seed；所有失敗均保留 gate 並可觀測地回報。
6. 未知或非本執行器所有的掛單維持拒絕；本切片不新增自動撤單、手動解鎖或繞過策略帳的機制。

## 團隊規劃表

| 成員 | 子代理/角色 | 任務範圍 | 鎖定範圍 | 交付與完成條件 | 測試/獨立驗證 |
|---|---|---|---|---|---|
| 需求分析-啟動訂單恢復契約 | `goal_requirements_analyst` | 只讀核對網格、非網格、零/部分/全成交、重啟與重複事件狀態轉移 | 不改生產碼、不推測缺少的 venue 證據 | Requirement Specification Card；每個終態列出所需持久化證據與封鎖條件 | 評審-恢復安全審查核對呼叫鏈和不變量 |
| 後端-終態意圖恢復接線 | `goal_backend` | 按確認後契約補通用安全結算/恢復協調接線 | 限 `main_intent_journal.go`、`main_execution_fills.go`、`symbol_manager.go` 及經確認必要的後端文件；不改策略交易決策 | 失敗時維持封鎖；所有成功路徑只能在 durable accounting 後重跑完整 bootstrap | 測試-訂單恢復生產接線 + 評審-恢復安全審查 |
| 測試-訂單恢復生產接線 | `goal_qa` | 獨立編寫/執行事件路徑測試 | 限測試檔；不可修改實作以讓測試通過 | 覆蓋零成交網格/非網格、正成交帳先行、重複事件、重啟、venue/持久化失敗、外部單、並行重試 | 評審-恢復安全審查確認斷言能捕捉錯誤解除 gate |
| 評審-恢復安全審查 | `goal_reviewer` | 只讀審查需求、差異、鎖順序、重試/競態、經濟帳順序 | 不改碼、不代替 QA | 明確列出阻斷問題、回歸和未覆蓋情況；沒有未處理高風險缺陷 | Goal Lead 對照規格與測試結果整合 |

Goal Lead 負責版本同步、`CHANGELOG.md`、`product-overview.md`、索引/任務狀態與最終整合；獨立 QA/Reviewer 必須檢查其各自產物。提交、推送、標籤或發布不在此修復計畫授權範圍內，須遵循使用者後續明確要求。

## SPEC/任務文件就緒度

| 文件 | 狀態 | 確認後動作 |
|---|---|---|
| Requirement Specification Card | 完成，已納入獨立審查發現 | 定義 settled map 生命週期及活動單 fail-closed |
| PRD | 不適用 | 本切片為既有安全契約修復，不新增產品功能或 API |
| Architecture Design | 進行中 | 對比安全刷新與 settled 項移除語義，保留未結 intent fencing |
| tasklist.md | 已建立 | 按依賴順序解鎖，不跨門檻派實作 |
| test-plan.md / acceptance.md | QA test-plan 待產出；acceptance 待同提交驗證後 | 明確驗證真實 callback 生產接線及失敗封鎖 |
| INDEX.md | 已建立 | 索引隨文件狀態更新 |

## 風險、授權及停止條件

| 風險/決策 | 影響 | 停止條件 |
|---|---|---|
| 結算時 venue 狀態不可查或回應不完整 | 無法證明委託終態/零成交 | 保留 gate，不將未知轉成未送出/零成交 |
| 部分成交 | 需要精確成交明細、費用與策略帳證據 | 任一帳本未持久化則不 settle、不 seed |
| 回呼與啟動/重試並行 | 可能重複結算或快照互相交錯 | 必須沿用序列化協調，race 測試不能通過則停止 |
| 任務涉及未知 API 行為或需要真帳戶資料 | 超出隔離測試範圍 | 不連接真實帳戶、不下單；回報缺少的模擬介面/證據 |
| 團隊執行授權 | 新切片計畫已列分工，仍待使用者確認 | 確認前不派子代理、不改生產程式碼 |
| 遠端 GitHub 不可達 | 無法驗證推送結果 | 不宣稱已推送；後續明確獲准推送時需重新讀回遠端 |

## 下一步

先由需求分析與只讀評審梳理契約並更新 SPEC/tasklist；契約固定後由 Goal Lead 核准進入實作，後端與獨立 QA 依鎖定範圍執行，最後由 Reviewer 對同一份差異獨立驗證。版本及變更記錄僅隨實際程式碼修復更新。
