# 3.111.0-rc1156 恢復架構設計

狀態：使用者於 2026-10-10 確認完整跨層實施分工及正成交/部分成交 fills/fees-first。此文件作為 GT-005/006 實施基線；backend identity、CAS 不確定寫入恢復、每訂單累計事件序列化的具體機制仍須由代碼與獨立測試證明，不得以設計文字替代證據。

## 決策摘要

1. **不接管活動單**：啟動快照中任何活動委託（無論是否能證明 owner）仍阻止 ExposureBook Seed。不得推算尚未成交餘量的策略風險預留；不自動撤單。這是既有 fail-closed 安全邊界，不是本切片要放寬的能力。
2. **normal bootstrap 對同一已載入 journal 必須冪等**：若 executor 已在相同 backend identity + canonical scope 綁定並載入 journal，再次進完整 bootstrap 時不能重置 `journalLoaded` 或重新從 journal 覆蓋執行期狀態。所有記憶體 intent 均已 durable settled/rejected 時，同綁定配置可安全 no-op；仍有任何未結/UNKNOWN/ledgerPending intent 時回傳可重試的 recovery error，但不能污染 loaded state 或阻斷後續事件結算。不同 scope/backend 仍 side-effect-free 拒絕重綁。
3. **保留即時 owner map 與 durable history**：不要在 settle 後刪除 intent；保留 owner/CID 與 terminal 狀態可令重複事件辨識及避免到達後報「未認領」。settled journal record、成交/費用、scope、order/trade IDs 和策略帳全部保留。重啟時 loader 仍驗證 settled record 再略過，不把它恢復成未結 intent。
4. **完整 retry 仍是唯一 Seed 路徑**：正成交與網格/非網格零成交成功結算均呼叫既有 `runtimeExposureBootstrapCoordinator.Retry`；重新做完整策略 inventory、venue position/open-order、提交快照協調及 ExposureBook Seed。不得直接清 gate 或局部 Seed。
5. **不放寬不同綁定/未結 intent 防護**：不同 scope/backend 不能重綁；同綁定再進 bootstrap 遇到任何未結、UNKNOWN intent 時必須拒絕 Seed，但保持 journal 可用以接收後續終態並完成 settlement。不可藉清空 map、刪除 intent 或跳過未知狀態通過。

## 預期狀態流

```text
owned intent observed
  -> (for each positive cumulative fill update) complete unique fill/fee ledger persisted
  -> route + strategy durable accounting using the monotonic accepted update
  -> exact venue order re-query; terminal quantity/status validated
  -> durable intent settled CAS succeeds
  -> same-binding journal bootstrap re-entry (no reset of loaded runtime state)
  -> serialized full bootstrap retry
  -> re-read intent history + positions + all open orders + owner inventory
  -> seed ExposureBook and release only bootstrap-owned hold
```

任一步失敗則不執行其後步驟；特別是 durable settle 成功但完整 bootstrap 因其他未結 intent/活動委託失敗時，已載入 journal 仍須保持可用，允許其餘事件繼續被核驗/結算；不能以「rebind journal」方式重置 loaded flag，亦不得撤銷 durable settle 或清理其他 CID。等所有 intents settle 後再完整 bootstrap；若仍有活動委託則繼續 fail-closed，直至活動單終態並完整核帳。

## 接線邊界

- 成交事件先封鎖新開倉。依使用者確認，對每個 `executedQty > 0` 的累計更新先完整捕獲並耐久持久化唯一成交及費用/盈虧，再依單調接納的最新訂單狀態執行 grid/routed strategy durable accounting；只有終態且兩類帳均確認 durable 才 settle intent、全量 bootstrap。不得讓較早/較低累計量 callback 越過較晚更新先寫策略帳；需 per-order 序列化/合併或其他經測試證明等價的機制，且不可長時間鎖住共享事件循環。
- 對已路由非 grid 的零成交終態，需新增通用 settlement 協調器：先由 strategy accounting reporter 確認 durable state，再驗證精確 owner/strategy/CID，呼叫 zero-fill venue re-query settlement；不可把 strategy 未啟動、route 空值或 `accounted=false` 當成功。
- Grid 零成交保留已存在的 grid slot durable accounting 要求；settle 成功後執行完整 bootstrap retry。非同步任務須攜帶可取消 context/有限 deadline，錯誤要記錄並維持可觀測 gate。
- 重複/亂序事件不得造成重複 fill、雙重結算或清除不同來源 gate。Settlement 和 retry 必須可重入；coordinator 繼續串行化 bootstrap。
- 保護性減倉/平倉路徑不可因新增 recovery hold 而被擋；沿用現有 OpeningGate 對開倉 request 的區分，新增接線測試直接驗證。

## 失敗、重試與可觀測性

| 情況 | 即時 map / durable journal | gate / bootstrap |
|---|---|---|
| venue 查詢失敗、nil、錯 ID/symbol/CID、非終態 | 不移除 map；不寫 settled | 保持現有 recovery/settlement hold；不 retry 成功路徑、不 Seed |
| zero-fill 查得非零累計量或 filled status | 不移除；不得走 zero-fill | 留 gate，需走完整成交恢復，不可偽裝成零成交 |
| strategy/fill/fee 落盤失敗或資料不足 | 不 settle；保留原 intent | 保持對應 accounting gate 與 bootstrap hold |
| settled journal CAS/持久化錯誤或結果不確定 | 不移除 map；保留 UNKNOWN/journal failure block | 不觸發可放行的 retry；先依既有 durable revision 重讀/恢復 |
| settle durable 成功 | 保留此 settled CID 與 owner map；journal record 保留 | 呼叫 coordinator 完整 retry；同綁定已載入 journal 可冪等核驗 |
| 完整 retry 發現其他活動單/未知單/不一致倉位 | 不修改這些 intent | Seed 拒絕且 gate 保持；無自動撤單 |
| bootstrap 暫時性錯誤 | 已 settle CID 不重新造回未結狀態 | coordinator 釋鎖後允許後續安全 retry；錯誤需帶 owner/scope/order context（不可含憑據） |

同綁定判定必須包含穩定 backend 身分及 canonical scope key，不能只比 scope 字串。不同 backend/scope 請求需在任何改動 `journalLoaded`、journal pointer/scope 或 IntentRecoveryBlock 之前 side-effect-free 拒絕，避免舊 executor 因一次錯誤配置而失效。若 backend 介面沒有穩定身分，先新增明確 binding identity，不用可能 panic 的 interface equality 猜測。

實作需確認 `ConfigureIntentJournal` 同綁定冪等路徑與並行 `ObserveOrder`/重複 settle 的鎖與 revision 邊界。未結 map 存在時的重試必須回傳錯誤但不能將 `journalLoaded=false`；同綁定 no-op 不得繞過 owner/scope/backend 核驗。不可長時間持有 `intentMu` 跨 exchange I/O；如需 per-CID in-flight fencing，須以明確狀態及取消測試設計，避免第二個事件盲目重寫 revision。

### 正成交 ledger-first 的執行活性

- 成交明細查詢/費用持久化若在交易所 API 緩慢時延遲策略帳回寫，可能令策略內部倉位更新與保護性退出較慢。實作應先以 per-order 有界工作協調隔離 IO，不阻塞全局訂單 stream；但不能先呼叫策略經濟帳，也不能在 capture 未成功時暫時放行新開倉。
- 每個正累計量事件需納入單調 target：更高累計量到達時擴大待捕獲 target；較舊事件不可令策略帳回退。只有確認對應累計成交/費用完整耐久後才應用相應策略更新；terminal settlement 等待 terminal target 與策略帳皆完成。venue/store failure 保持 admission block 並維持可重試恢復狀態。

## 開發鎖定與驗收

- 後端只修改 tasklist 鎖定的 runtime journal/order callback 範圍；本 P1 要求解決同綁定 journal retry，因此 Goal Lead 可在測試計畫就緒後，逐檔授權 `order/intent_journal.go`；不得刪除 map 項或改 `order/owned_intents.go` 結算語義，除非獨立證據證明必要並重新審查。
- QA 測試計畫須先證明能在無真實帳戶下實際驅動 production callback；若無法，先由 Goal Lead 調整 fixture 設計，不可用單純 helper 測試冒充端到端。
- 必測：normal positive settle 後同 executor 同綁定重入 bootstrap；有其他未結 intent 時 retry 失敗但 journalLoaded 保持且後續終態仍可結算；全部 settle 後成功重試；不同 backend/scope 拒絕；grid/non-grid zero-fill settle 與 retry；CAS/venue/策略/fill ledger/庫存/Seed 失敗；重複與亂序；重啟；並行 retry/settle；UNKNOWN/活動/外部單仍封鎖；recovery gate 下 ReduceOnly close 仍可執行。
- 必測：normal positive settle 後同 executor 同綁定重入 bootstrap；有其他未結 intent 時 retry 失敗但 journalLoaded 保持且後續終態仍可結算；全部 settle 後成功重試；不同 backend/scope 拒絕；grid/non-grid zero-fill settle 與 retry；CAS/venue/策略/fill ledger/庫存/Seed 失敗；重複與亂序；重啟；並行 retry/settle；UNKNOWN/活動/外部單仍封鎖；recovery gate 下 ReduceOnly close 仍可執行。
- 每個正累計成交更新的 fill/fee 持久化必須先於策略經濟帳變更；終態 intent settle 又必須晚於完整累計 fills、費用及策略 durable accounting。測試需證明這個順序及 capture 延遲/失敗時保護性減倉能力未遭不安全退化。
- 同一工作樹 SHA 上跑目標包測試與 race 測試，核對 `main.go`、`webui/package.json` 版本一致後升至 `3.111.0-rc1156`，同步 CHANGELOG、product overview、審計/驗收記錄。通過本地測試不等於已提交、已推送、已發布、真實實盤或盈利驗收。
