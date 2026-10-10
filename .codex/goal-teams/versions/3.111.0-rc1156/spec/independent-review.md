# GT-002 獨立安全評審：啟動掛單終態恢復

## 結論

**結果：發現一項已成立的高風險恢復阻斷；不建議按目前生產路徑直接進入驗收。**目前的 gate 行為是 fail-closed，沒有證據顯示它會錯誤放行；但在一般正成交終態路徑中，即使策略帳與成交帳均已持久化並觸發 bootstrap retry，retry 會因已結算的內存 intent 仍留在 map 而被 journal 初始化拒絕。零成交網格路徑另有缺失的 retry；非網格零成交事件沒有運行時 settle 呼叫。這些是恢復能力退化/永久保持開倉封鎖的問題，不是誤解除 gate。

本審查只讀 `main` 工作樹源碼/測試並執行定向測試；未連接真實帳戶或生產 DB、未下單/撤單，僅新增本審查文件。所有行號以審查時工作樹為準。

## 已成立缺陷

### P1 — 正成交後的生產 bootstrap retry 被已結算內存 intent 阻斷

生產事件路徑為 `symbol_manager.go:1104-1137`：先同步處理 grid 與 routed strategy accounting，之後對正成交終態呼叫 `captureTerminalOrderAndSettleOwnedIntent`；在 `main_execution_fills.go:160-188` 中，完整成交歷史持久化完成後才 settle intent，成功回調隨後於 `symbol_manager.go:1158-1164` 呼叫 `retryExposureBootstrap`。Coordinator 的 retry 執行 `retryRuntimeExposureBootstrapAfterStrategyRecovery`（`symbol_manager.go:1027-1032`），後者再呼叫 `bootstrapRuntimeExposure`（`main_exposure_strategy.go:110-122`）。

但 `bootstrapRuntimeExposure` 每次都會再進 `configureRuntimeIntentJournalWithGridRecovery`（`main_exposure.go:34-42`；`main_intent_journal.go:145-150`）；該函式呼叫 `ConfigureIntentJournal`。`ConfigureIntentJournal` 一開始遇到 `len(oe.intents) != 0` 就回傳 `cannot bind journal after submissions`（`order/intent_journal.go:103-116`）。正常終態結算 `settleIntent` 將 `intent.settled=true` 並耐久保存後直接返回，沒有從 `oe.intents` 刪除（`order/owned_intents.go:234-250`）。因此一般正成交 settle 後，內存 map 仍非空；回調雖有觸發 retry，retry 在重新核對 venue position/open orders 之前就失敗，`runtimeExposureBootstrapBlock` 不會被移除（`main_exposure.go:115-125`）。

這是由同一生產呼叫鏈與具體 guard/狀態更新直接推出的確定缺陷，不是單純「可能沒有 callback」。現有測試未捕捉：`main_exposure_test.go:409-505` 的恢復測試用 `SettleRecoveredIntent` 後再直接呼叫 helper；該恢復方法會刪除 map 項（`order/recovered_intent.go:91-101`），與普通事件路徑的 `SettleIntent` 不同。`main_execution_fills_test.go:148-219,221-285` 驗證 fill durable 後 intent settlement，但沒有經生產 bootstrap coordinator 再跑完整 bootstrap。

### P1 — 網格零成交結算沒有觸發完整啟動核帳；同一 retry guard 亦會阻止簡單補上 callback

`symbol_manager.go:1133` 對零成交事件只呼叫 `settleVerifiedGridZeroFill`；其 goroutine 於 `main_intent_journal.go:102-128` 做 venue 零成交核實及 durable intent settlement，成功後只解除 `grid_zero_fill_settlement:<cid>`，沒有回呼 `retryExposureBootstrap`。而 `settleIntent` 成功同樣保留 settled intent 在 executor map（`order/owned_intents.go:234-250`），所以即使加 callback，bootstrap 再次配置 journal 時仍會被前述 `len(oe.intents)` guard 阻斷，除非修正整體狀態/重載契約。

### P1 — 非網格零成交終態在運行事件路徑沒有結算入口

訂單回呼對所有事件都執行 `ApplyOrderUpdateForStrategyWithAccounting`（`symbol_manager.go:1104-1127`），但零成交只送往 grid 專用 helper；正成交 fill capture 條件是 `ExecutedQty > 0`（`symbol_manager.go:1133-1135`; `main_execution_fills.go:51-52`）。因此目前活著的 runtime 對已路由非網格策略的零成交 CANCELED/EXPIRED/REJECTED 沒有調用 `SettleZeroFillIntent` 或其他 durable settlement，且不會以該事件觸發 bootstrap retry。相關 intent/recovery gate 可持續保留。這是恢復 liveness 缺口；安全結果仍是封鎖，不是錯誤開倉。

## 已存在且應保留的安全措施

- **owner 與事件認領：**symbol manager 先按 symbol 過濾，再要求 `observeOwnedRuntimeOrder` 成功，才把事件送入策略/帳戶路徑（`symbol_manager.go:1047-1057`；`main_intent_journal.go:20-28`）。策略結算再要求 durable owner strategy、client ID/order ID（`main_intent_journal.go:35-72,75-97`）。startup intent scope 驗證 bot/exchange/market/symbol（`order/intent_journal.go:117-124`）。恢復路由只來自 journal（`order/intent_journal.go:393-411`；安裝於 `symbol_manager.go:1580-1584`）。這些檢查降低誤把共享帳戶他人委託歸屬本 Bot 的風險；不等於活動委託已能經濟接管。
- **成交帳先決於 intent settle：**正成交先透過 `PersistOwnedOrderFills` 補齊交易所成交/費用明細，確認完成後才進 strategy/grid intent settlement（`main_execution_fills.go:89-141,146-188`）。交易所終態再由 executor 精確查詢並合併持久化，不單憑 websocket 終態（`order/owned_intents.go:204-250`）。策略更新失敗會阻止 settle，回呼失敗保持開倉 block（`symbol_manager.go:1115-1127,1138-1157`）。
- **先策略狀態、後成交明細的實際事件順序值得明確接受/調整：**事件回呼在啟動 fill capture 前已呼叫 `spm.OnOrderUpdateWithAccounting` 及 strategy accounting（`symbol_manager.go:1104-1127`），之後才持久化 trade/fill/fee（`symbol_manager.go:1133-1137` → `main_execution_fills.go:89-141`）。正成交路徑先封鎖開倉（`symbol_manager.go:1059-1064`），故沒有看到在兩步間重新放行的路徑；但 crash 若落在策略持久化成功、成交費用 ledger 尚未補齊之間，恢復必須證明策略更新冪等、成交明細可重放及最終 reconciliation 能一致，不能把「strategyAccountingVerified」單獨當作所有經濟帳完整。現有審查未證明此崩潰窗口有端到端測試，列為待驗證風險而非已證明帳務錯誤。
- **ExposureBook seed gate：**bootstrap 入口先 block，持有 `BeginPositionSnapshot` 快照/提交協調；要求 intent/legacy/position/owner inventory/open-order 查詢均可證明，且拒絕任何 open orders；只有最後 `book.Seed` 成功才解除自身 block（`main_exposure.go:34-42,44-116,118-125`）。未知/外部委託仍導致 fail-closed。這能避免未核帳時新增風險，但也是目前恢復缺口令開倉長期不可用的直接機制。
- **保護性平倉：**開倉 gate 僅套用 opening request（`order/opening_gate.go:52-64,66-85`）；futures `ReduceOnly` 與依明確 leg 判定的 spot close 不走 opening gate。測試明確覆蓋 gate 阻擋時仍可提交 futures reduce-only close（`order/opening_gate_test.go:45-63,67-84,87-105`），恢復封鎖本身沒有證據會禁掉此保護平倉路徑。仍須由改動後 QA 覆蓋具體恢復 gate + 真正 protective close 接線，而不能只依單元測試推及全 runtime。

## Coordinator、鎖與重入

- `runtimeExposureBootstrapCoordinator.Retry` 在 `c.mu` 持鎖期間同步執行整個 `attempt()`，成功後設 complete；失敗會 unlock 並允許下次 retry（`main_exposure_strategy.go:17-46`）。這保證 retry 串行、只成功一次；測試 16 個並發呼叫且驗證只執行一次成功 attempt（`main_exposure_test.go:23-59`）。
- 未找到 coordinator mutex 的明確遞迴取得/反向鎖序：正成交成功回調在 capture goroutine 中呼叫 Retry；bootstrap 會取得 submission snapshot 協調，之後讀取 journal/venue，沒有已見路徑會在持 executor `intentMu` 時再次呼叫 Retry。據此「已成立死鎖」不成立。
- 仍有鎖活性假設：coordinator mutex 覆蓋可能包含分佈式 snapshot lease、submission drain 及 venue/storage I/O 的整個 bootstrap attempt（`main_exposure_strategy.go:35-45`；`order/shutdown.go:39-61`）。失敗會正常釋放 mutex；若任一底層 I/O 忽略 context、永久阻塞，後續事件的 Retry 會排隊且 recovery coordinator 永不前進。當前只讀證據未證實 adapter 會忽略 context，因此這是待驗證風險，不應寫成已確認死鎖。生產接線測試應以可控阻塞/取消的 adapter 驗證快照 lease 最終釋放及後續 retry 可進入。
- `complete` 只在 attempt nil error 後設置（同上）；失敗可重試。成功之後不再 retry 是預期 one-shot 行為，因 bootstrap gate 已解除且完整 Seed 已成功。

## 策略類型與重啟覆蓋

- 普通 runtime 有 grid、trend、mean_reversion、momentum、martingale、dca、dca_enhanced、combo、spot_short、spot_long、futures_short、futures_long 等註冊分支（`symbol_manager.go:1300-1578`）。正成交事件依 durable route 送到 manager 中指定策略並以 `OrderUpdateAccountingReporter` 回報持久化情況（`strategy/strategy.go:463-516`）。settle helper 對 grid 特判；其他非空 owner route 必須 `strategyAccountingVerified` 才 settle（`main_execution_fills.go:167-186`）。
- 啟動 ExposureBook inventory loader 明確載入 signal 策略、DCA/DCA enhanced、martingale、combo（`main_exposure_strategy.go:48-107`）；其他策略依其 runtime 是否產生 owner inventory 或由共享 grid ledger 記帳而不同。僅依當前 loader 不能推定 `spot_short/spot_long/futures_short/futures_long` 的所有借貸、庫存、對沖腿都進入同一 durable inventory；本次只審查恢復事件與 loader 接線，沒有完成各策略經濟 ledger 的端到端核對。這是架構/QA 必須逐類確認的範圍，不作為已證明錯帳結論。
- 重啟時 `ConfigureIntentJournal` 對 unresolved/prepared/未核實正成交保持 unknown 和 `IntentRecoveryBlock`；zero-fill terminal 也會按精確 CID/order 查 venue，完成後才從恢復集合刪除（`order/intent_journal.go:100-115,128-177,202-228`）。`RecoveredOrderRoutes` 僅提供路由，不代表策略帳已恢復。`SettleRecoveredIntent` 要求 caller 已 durable apply strategy fills，再核對精確 terminal venue order；其後仍保留 `IntentRecoveryBlock`，要完整 ExposureBook bootstrap 成功才有機會開倉（`order/recovered_intent.go:12-16,17-101`）。
- 啟動時若任一 open order 存在，intent bootstrap 及 ExposureBook bootstrap 都 fail closed（`main_intent_journal.go:180-195`; `main_exposure.go:103-116`）。一般 runtime 只看到明確的啟動後 retry 入口：StartAll 成功（`symbol_manager.go:1586-1595`）、正成交 settle 成功（`1158-1164`）、SpotShort reconciliation 成功（`1502-1508`）。無週期 retry。訂單終態本身不應推定所有帳已結算；問題是完整成功後缺乏可靠可執行的重試路徑。

## 測試覆蓋與本輪驗證

已執行並通過：

```text
go test . -run 'Test(RuntimeExposureBootstrapCoordinatorRetriesAndSerializes|RuntimeExposureBootstrapRetriesAfterStrategyRecoverySettlesIntent|RuntimeExposureBootstrapRequiresAuthoritativeEmptyAccount|Captured(Grid|Strategy)FillSettlesOnlyAfterFillHistoryIsDurable)$' -count=1
ok quantmesh 5.764s

go test ./order -run 'Test(SettleZeroFillIntent|ConfigureIntentJournal|RecoveredIntent)' -count=1
ok quantmesh/order 2.461s
```

此結果只確認目前測試通過，並未推翻上述缺陷。缺口：

1. 無測試從 `StartOrderStream` callback 的實際 wiring 驅動正成交終態，等候 durable fee/fill + strategy/grid ledger + intent settle，然後經同一 `runtimeExposureBootstrapCoordinator.Retry` 重新跑 bootstrap。現有 capture tests 直接呼叫 helper；恢復測試直接 `SettleRecoveredIntent` 和 `retryRuntimeExposureBootstrapAfterStrategyRecovery`，沒有保留 normal-settle 的 map 狀態。
2. 沒有 grid zero-fill runtime event → verified zero-fill settlement → bootstrap retry 的因果測試；亦無 non-grid zero-fill 的終態結算測試。
3. 沒有所有已註冊策略各自的 durable accounting/recovery inventory/ExposureBook seed 接線矩陣，尤其含 SpotShort 負債與 spot inventory。
4. 沒有 crash-point 測試覆蓋 strategy durable write 與 fill/fee ledger write 之間、fill ledger 完成與 intent journal settle 之間，以及 settle 成功、bootstrap 失敗後的可重試性。
5. Coordinator 的並發測試證明串行及一次成功，不證明 adapter 永不阻塞/lease 恢復；目前缺可控 context cancellation/阻塞 adapter 的 retry liveness 測試。

## 優先建議

1. 後續 requirement-spec-card 明確加入 GT-002 的 P1 發現：normal `SettleIntent`/`SettleZeroFillIntent` 成功後，內存 intent 的生命週期必須允許同 executor 完整 bootstrap 重試；不能只把成功 callback 接上就宣告修復。
2. 需求分析需決定 settled intent 的 runtime map 清理/安全重載語義，同時保留 durable journal 的冪等核對及防重複事件能力；不要直接放寬 `ConfigureIntentJournal` 的「after submissions」保護。
3. 將非網格與網格零成交分別定義：只有明確 route、策略 accounting reporter 明確確認持久化、fresh exact venue terminal + zero executed quantity 才能 durable settle；其後觸發完整 bootstrap。任何未知/部分成交、owner 缺失、策略未啟用、持久化或查詢錯誤都保持原 gate。
4. QA 先建立實際 production callback wiring fixture，斷言已 settle intent 不在 map/或可安全 reconfigure、所有費用及策略帳 durable、retry seed 成功才解除 `runtimeExposureBootstrapBlock`；另斷言未完成時仍封鎖、reduce-only close 仍可走。
5. 對 coordinator 的 I/O context deadline 與 snapshot lease 釋放做可控阻塞測試；若發現底層 exchange/storage call 不遵守 ctx，再按實證設計超時/避免長持 mutex，不預設為死鎖。

實作完成後，GT-002 需對 `requirement-spec-card.md` 作交叉複審，並在 GT-007 對最終 implementation diff 及 QA 實際測試重新審查；本文件不是最終實作驗收。

## 架構草案復核

復核對象：`.codex/goal-teams/versions/3.111.0-rc1156/spec/requirement-spec-card.md` 與 `architecture-design.md`。本節僅作文件/源碼交叉審查，沒有修改其他文件或程式碼，沒有等待 test-plan。

### 判斷摘要

架構提出「settled journal revision durable CAS 成功後，只刪除該 CID 的 executor 內存項，保留 journal 歷史」方向是合理的，但只能在**確認 CAS 成功、同一 intent/revision 仍為當前狀態、且刪除與並發事件 fencing 具有明確原子邊界**時成立。文件尚未把重複回調、settle 與 venue event 交錯、同綁定 journal refresh 的拒絕副作用、CAS 衝突後如何重讀、以及不同 gate source 的釋放責任定義完整。因此設計目前是可行方向，不足以直接當作已證明安全的實作契約。

### 按 CID 回收是否保留 owner / UNKNOWN fencing

- **已成立的保護基礎：**durable intent payload 帶完整 `IntentScope`/`Request`/strategy owner；scope key 含 Account/Exchange/Market/Symbol/Bot（`execution/intent_journal.go:14-34`、`order/intent_journal.go:281-293`）。settled record 保留在 storage；`ConfigureIntentJournal` 重新載入時驗證 settled state，之後略過而不還原到 live map（`order/intent_journal.go:142-156`）。未知/未結意圖仍載入 map 並設 UNKNOWN，載入完若仍有項目會回傳 recovery required 並保持 `IntentRecoveryBlock`（`order/intent_journal.go:158-177,222-228`）。因此在 durable settled CAS 成功後移除**該一項**，不等於刪除了 owner 歷史；其他 CID 不應被清除。
- **必要前提：**只有 settled JSON 明確滿足 `settled=true, unknown=false, ledgerPending=false, terminal order exists` 才准移除；storage CAS 以 expected revision 更新，rows affected 不為 1 回傳 `ErrIntentJournalConflict`（`storage/execution_intents.go:40-78`）。刪除必須發生在 CAS nil error 之後且仍在同一 `intentMu` 臨界區，寫入失敗/結果不確定時必須保留該 CID、設 UNKNOWN/journal-failure fencing 並不觸發成功 retry。現有 `SettleRecoveredIntent` 已在鎖內檢 revision、寫 settled journal，成功後刪除 CID（`order/recovered_intent.go:84-101`），可作行為參照，但不能直接推論一般 `SettleIntent` 具有相同併發契約。
- **UNKNOWN fencing 邊界：**由明確 unresolved/UNKNOWN 轉成 durable settled 必須以同一筆 intent revision 的條件更新完成；若 venue query 期間收到較新成交、身份衝突或 UNKNOWN 標記，不能用舊查詢覆蓋/刪掉新狀態。架構草案有寫「CAS 衝突不移除」，但尚未明定 settle 開始時記錄的 revision、query 後確認的同一物件/狀態版本，以及 CAS conflict 後的重新載入 API/流程。`SettleRecoveredIntent` 有 revision snapshot + conditional check；普通 `settleIntent` 在 query 前不保存 revision，query/`ObserveOrder` 後只檢查當下 UNKNOWN/ledgerPending/terminal 條件再以目前 revision 寫入（`order/owned_intents.go:188-250`）。這是需實作前定義並用併發測試證明的差異，不據此宣稱已發生錯帳。

### 重複/並發終態與 settle 冪等

- storage CAS 保護跨 executor revision 寫衝突，但不自動提供呼叫層冪等結果。普通 `SettleIntent` 在 live map miss 時會回錯；有 owner-aware 的 `SettleReconciledIntent` 可在 map miss 後讀 durable record 驗證 strategy owner + settled 狀態（`order/owned_intents.go:121-142`; `order/intent_journal.go:43-82`）。事件 callback 的 `settleVerifiedStrategyIntent` 仍呼叫普通 `SettleIntent`（`main_intent_journal.go:31-72`）；grid helper 呼叫 `SettleReconciledIntent`（`main_intent_journal.go:75-97`），有 map-miss durable verification 的基礎，但仍依賴 journal binding/loaded 狀態及 durable record owner 校驗。
- 移除 CID 後新抵達的 duplicate websocket event 會被 `observeOwnedRuntimeOrder` 拒絕，不再進策略會計，符合保守 fencing（`main_intent_journal.go:20-28`; `order/owned_cancellation.go:123-186`）。但是**兩個重複回調已先通過 ObserveOrder、同時進入異步 fill-capture/settlement**的交錯情況，尚無證據證明安全：`runtimeFillCapture` 完成一次後會先清 `running`，再呼叫 `onSuccess`（`main_execution_fills.go:128-141`）；另一 duplicate 可在第一個 settle 刪除 map 前已進入成功 callback。策略 settle 使用普通 `SettleIntent`，在第一個 callback 已刪 CID 時第二個 callback 會報錯，並可能設置目前沒有解除路徑的全域 settlement block；grid settle 有 owner-aware fallback，但 concurrent settle 的結果及 gate 仍需測明。架構稱 settlement 可重入，卻未規定 per-CID in-flight 結果或 already-settled 的 owner-scoped success 語義。須補明確語義與測試，不能只測順序重複事件。
- 另外，`settleIntent` 入口沒有拒絕 `intent.settled`（`order/owned_intents.go:188-197`），回收後普通策略 callback 對 CID 缺失會進錯誤分支；該失敗 handler 會 `Block(strategyIntentSettlementBlock)`（`main_execution_fills.go:176-183`）。因此實作需讓普通事件 settlement 對 durable already-settled 有 owner-scoped、可驗 revision 的冪等成功語義，或在 callback 層讓重複 settle safely join/ignore；不可讓 duplicate 把已完成結算轉成另一個永久阻斷源。grid 的 `SettleReconciledIntent` 可驗 durable settled record，但不可假設其 journal 永遠 loaded，尤其同 scope 尚有其他 pending CID 時。

### 同綁定刷新與其他未結 intent

- **典型 happy path 可行但 API 未鎖定同綁定：**runtime closure 一直把同一個 `intentBackend` 與 `intentScope` 傳給 `bootstrapRuntimeExposure`（`symbol_manager.go:1027-1032`; `main_exposure.go:34-42`），因此在 map 已空時對同一 runtime backend reload 是可推導的預期使用方式。但 `ConfigureIntentJournal` 本身只核對新 scope 與 executor 的 bot/symbol/exchange/market，不核對先前已綁定 journal/backend identity、完整 scope key 是否相同（`order/intent_journal.go:103-125`）。架構應要求 refresh 只能在原 binding 上進行，且不能將 journal 換成另一份同 owner 但缺歷史的 storage；實作需有不依賴 interface 直接 `==`（動態值可能不可比較）的安全 binding identity 策略。
- **已確認的 refresh 拒絕副作用：**`ConfigureIntentJournal` 在檢查 `len(oe.intents) != 0` 前先設 `journalLoaded=false` 並 `Block(IntentRecoveryBlock)`，然後才以非空 map 拒絕（`order/intent_journal.go:103-116`）。架構預期每一 CID durable settle 後均可觸發 coordinator retry；如果同時還有另一未結 CID，bootstrap 會再次呼叫 Configure，按設計應被其他 CID 阻斷。但當前此拒絕會先把 executor 的 `journalLoaded` 改成 false；該未結 CID 後續 `saveJournalIntentLocked` 會因 journal unavailable 失敗（`order/intent_journal.go:274-290`），並轉入 journal-failure/UNKNOWN hold。故「其餘未結 intent 阻斷 bootstrap」這條架構目前會退化成「阻斷後還使剩餘 intent 無法正常 durable settle」。這是已由現有控制流證實的設計/實作交互缺陷。必須在呼叫端只於沒有 unresolved map 項時 retry，或讓同綁定的拒絕/探測不改動既有 loaded binding；兩者需由 Goal Lead 定稿，且不能讓 open order/UNKNOWN 繞過完整 bootstrap。
- 多個 unresolved intents 逐筆 settle 時，前一筆成功後的 bootstrap retry 不應令剩餘 CID 失去事件接收、owner route 或 journal 寫能力。完整 bootstrap 失敗若只是 venue open-order/position/seed 不符，而 journal reload 本身成功，`runtimeExposureBootstrapBlock` 仍保持，Coordinator attempt 返回後可再 retry；但若原因是上述 map guard，則不是可正常恢復的暫時 bootstrap failure。Architecture 中「只清已結算 CID、其他 CID 保持」不足以保證後續 CID 可完成。

### CAS 衝突後的 gate 與重新載入路徑

`saveJournalIntentLocked` 使用 intent revision 做 CAS；寫入錯誤時呼叫端將 intent 留在 map/標 UNKNOWN，`blockJournalFailureLocked` 又將 `journalLoaded=false` 並設定 `IntentJournalFailureBlock`（`order/intent_journal.go:364-368`；settle 寫入失敗處 `order/owned_intents.go:243-248`）。但既有 `ConfigureIntentJournal` 由於非空 map guard 無法在原 executor 上重新載入更新 revision；我未找到另一個 in-place journal reconcile/reload 方法。架構寫「先按既有 durable revision 重讀/恢復」但未指定可執行入口/何時可安全替換內存狀態。此時正確行為是 fail-closed，但 liveness 尚無閉環。需求卡應將其標為需設計的 CAS conflict recovery，不應暗示 coordinator retry 會自行修復。

### Gate 所有權、清除條件與 full-bootstrap retry

- `bootstrapRuntimeExposure` 每次先 block `runtimeExposureBootstrapBlock`，只有完整快照、庫存/委託核對與 `book.Seed` 全成功後才解除同一 source（`main_exposure.go:34-42,103-125`）。bootstrap 錯誤時 defer 只釋放 snapshot 資源，gate 留著；後續 Coordinator retry 失敗可再試，這一 source 的所有權邊界是正確的。
- `ConfigureIntentJournal` 在沒有 unresolved intents 時只解除 `IntentRecoveryBlock`；這與 exposure bootstrap block 分離，因此 journal read 成功本身不會解除 ExposureBook 的 seed gate（`order/intent_journal.go:222-228`; `main_exposure.go:121-125`）。其他 source 原則上也不會被上述 Unblock 刪掉。
- **已確認未閉合的 source：**`strategyIntentSettlementBlock` 在 `settleVerifiedStrategyIntent` 錯誤 defer 及 fill capture settlement error 時被 Block（`main_intent_journal.go:46-50`; `main_execution_fills.go:176-183`），全倉庫沒有對此常量的 Unblock。即使之後 per-CID durable settle 成功、且 `retryRuntimeExposureBootstrapAfterStrategyRecovery` 清除 grid runtime reconciliation holds（`main_exposure_strategy.go:117-122`），此 source 仍令 `OpeningGate.Blocked()` 為 true。這是明確 gate owner/liveness 缺口。它可能是刻意要求人工介入，但架構目前把失敗後 retry 當作一般路徑，未定義此 source 的人工/自動恢復政策。須選擇可證明安全的解除所有權（例如 per-CID/per-operation token 或完整 reconciliation 清單後只解除相應 hold）；不可用 bootstrap 成功時無條件 `Unblock(strategyIntentSettlementBlock)`，那會清掉其他尚未解決失敗的保護。
- Protective close 的 opening-gate 分流仍可保持：`OpeningGate` source 只拒絕 opening request，futures reduce-only 與合法 spot close 不需開倉 admission（`order/opening_gate.go:52-85`，既有回歸見 `order/opening_gate_test.go:45-105`）。新 settlement/global retry gate 不可另行包住或封鎖 close path；架構已寫需測，但此為最終實作/QA 驗收項。

### 草案需補的決策與驗證條件

1. 明定 normal settle 的 per-CID linearization point：在 `intentMu` 下重新確認同一 intent 指標/owner/revision，驗證 no UNKNOWN/no pending ledger/terminal durable CAS 成功，再刪除同一 CID；CAS 失敗絕不刪除。venue 查詢期間有更新時重新核對累計成交、order identity 與 fill-ledger cursor。
2. 明定 settled CID 被回收後，重複 callback 如何透過 owner-scoped durable settled record 回傳冪等成功，而不再重做策略 accounting、不設置不可恢復 gate；測試需要兩個已越過 ObserveOrder 的並行 duplicate。
3. 明定 journal binding immutable；同綁定 refresh 時其他 live/UNKNOWN CID 必須阻斷 bootstrap，但不得先污染 `journalLoaded`，並須允許那些 CID 繼續收到/持久化 reconciliation。CAS revision 衝突時提供專用受控 reload/reconcile 路徑，或清楚列為需 operator action 的永久停止狀態。
4. 明定每一 gate source 的 owner 與 clear proof，尤其 `strategyIntentSettlementBlock` 是否可自動恢復、如何按 CID 清除，以及多 CID 同時失敗時不能互相釋放。完整 ExposureBook bootstrap 成功只可清它直接擁有的 source。
5. 確認 partial/active/foreign order 的 retry 結果：整體 Seed 仍拒絕；同一 terminal fill durable settle 後若還有任何其他未結 CID，剩餘 CID 可繼續 reconcile；只有 intent 集合全數合格、fresh full venue snapshots 一致、`Seed` 成功後才解除 opening block。Protective close 同時保持可用。

**複審狀態：草案需在實作解鎖前修訂/回答上述條件。**此結論不是否定 per-CID 回收方向，而是指出其 owner/fencing 安全性以 durable CAS 和鎖內狀態確認為前提；目前對並行重複、非空同綁定 refresh、CAS 恢復與 gate source 的語義尚未閉合。最終仍需 GT-007 對實作 diff、同一 SHA 測試及 race/生產 callback 接線重審。

### 最新架構修訂複核（同綁定冪等再入）

復核依據為本輪讀到的最新 `architecture-design.md` 決策摘要 2–5、狀態流、失敗表與驗收條款，以及最新 `requirement-spec-card.md` 的「補充 P1」與驗收契約。未執行測試；只讀源碼對照。

#### 結論

修訂方向修正了前版「settle 後刪除 owner map」的主要風險：保留 settled CID 可使重複事件仍能以 durable owner identity 被辨認，也避免把 executor 中的 owner/fence 一併丟掉。對已載入同一 journal 的 settled-only map 做 no-op、對含未結/UNKNOWN map 回傳錯誤且不改 `journalLoaded`，在行為上可以實現；但**目前文件尚不足以讓實作者唯一、無歧義地實作 backend binding identity、失敗副作用及 gate 狀態**。此外，架構文內仍有「remove CID」的舊流程，正成交順序也與需求卡及實際代碼有衝突，故此版不能判為文件契約已完全一致。

#### Binding identity 與 ConfigureIntentJournal 再入

- 文件要求同 `scope/backend` 才 no-op、不同 scope/backend 拒絕；scope identity 可直接用既有 `IntentScope.Key()`（含 Account/Exchange/Market/Symbol/Bot，`execution/intent_journal.go:14-34`）。但 backend 是 `execution.IntentJournal` interface（`execution/intent_journal.go:43-48`），草案沒有定義其穩定 identity：是同一 object instance、storage identity/token、或持久化資料庫/namespace 身份。直接比較 interface 只有在動態值 comparable 時才安全；若採 instance identity，需說明包裝器每次重建會否被視為不同 backend。故「同 scope/backend」概念清楚，binding identity 的可執行判準仍未定。
- **同綁定 settled-only no-op 可落地，但需精確 predicate：**使用既有 map 不 reload、不覆寫 execution state，驗證 scope key 與 journal binding 完全相同；對每個保留項需證明 `Settled || Rejected` 且沒有 `Unknown`、`LedgerPending`、reconciliation/error hold，並確認其 durable write/CAS 已成功。當前 `ConfigureIntentJournal` 是 startup initializer：每次先 `journalLoaded=false`、Block `IntentRecoveryBlock`、非空 map 一律報錯，空 map 才重載（`order/intent_journal.go:103-125`）；所以新行為應明確是一個早於這些副作用的 already-bound 分支，而不是照舊走 reset/reload 後「盡量還原」。
- **含未結項保留 loaded 的規格合理，當前實作不符合：**現行 Configure 在非空 map guard 之前先將 `journalLoaded=false` 並 block recovery source（`order/intent_journal.go:111-115`）；後續 `saveJournalIntentLocked` 會因 loaded=false 拒絕寫入（`274-290`）。新版草案已正確要求未結/UNKNOWN retry 只回可恢復錯誤、不能污染 loaded state；實作需在任何 journalLoaded/gate/map mutation 前判定已存在 binding 與 map 狀態。對 unresolved entry 必須保留 journal pointer、scope、revision、`journalLoaded=true` 及接收後續 event/settle 的能力，Bootstrap 自己的 `runtimeExposureBootstrapBlock` 繼續 hold；若 UNKNOWN 本來有 owner gate，亦不可移除。
- **不同 backend/scope 拒絕的副作用尚未說明：**現行方法在驗證新 scope/backend 前就先改 loaded/gate，再在完成 owner 基本欄位檢查後換 binding（`order/intent_journal.go:111-125`）。新契約應明定錯誤呼叫不得覆寫既有 journal/scope/map/revision，也不得讓 `journalLoaded=false` 使正確 binding 的後續 settle 失效。是否另設 binding-mismatch opening hold，以及該 hold 的 owner/清除條件，需由設計寫明；不能把拒絕呼叫等同於允許舊 binding 無條件繼續或清除此錯誤 source。
- **混合 map 與拒絕條件：**草案說所有項 settled/rejected 才 settled-only no-op；任何未結/UNKNOWN 都 recovery error。必須明定 rejected 是否允許 no-op 的條件（需 durable rejected revision、且不得藏有 UNKNOWN/ledgerPending），以及混合 settled + unresolved 時保留全部 map、只拒絕當次 Seed/retry，不 reload/刪除任何 CID。完整 bootstrap 因活動委託或倉位不匹配失敗，之後同 binding retry 可 no-op journal 再重新做全量快照；不能因 journal no-op 而略過 positions/open-orders/strategy inventory 核對。

#### 文件內部不一致：CID 保留 vs 移除

決策摘要第 3 點、失敗表「settle durable 成功」列，以及段落文字明確要求保留 settled CID；但「預期狀態流」仍有 `durable intent settled CAS succeeds -> remove only this settled CID from executor memory -> serialized full bootstrap retry`。這是舊設計殘留，與新決策直接矛盾。應刪改該箭頭為「同綁定、settled-only journal configuration no-op（map/history 保留）→ full bootstrap」，並確認 retry 前後同一 executor map 不變。否則後端可按任一分支實作，規格不能作為驗收依據。

#### 正成交會計順序：需求卡、架構、代碼三方衝突（未決風險）

- **實際 runtime 順序：策略/grid accounting 在先，成交/費用 fills 持久化在後且非同步。**Order stream callback 先呼叫 `spm.OnOrderUpdateWithAccounting`（`symbol_manager.go:1104`）、`ApplyOrderUpdateForStrategyWithAccounting`（`1115-1127`）與 `multiExecutor.OnOrderUpdate`（`1129-1132`）；之後才啟動 `captureTerminalOrderAndSettleOwnedIntent`（`1134-1138`）。capture goroutine 內 `PersistOwnedOrderFills` 成功後才執行 strategy/grid intent settle（`main_execution_fills.go:89-141,146-188`）。正成交事件在 callback 開始處先設 execution-ledger opening hold（`symbol_manager.go:1059-1064`），所以尚未見到此路徑在 fills 未完成前解除該 hold。
- **文本契約不一致：**最新 requirement-spec-card 的 Doc Capsule 寫「權威 fills/費用持久化 → 策略 accounting → intent settle」；最新 architecture 的接線邊界則寫「先封鎖 → grid/routed strategy accounting → 完整 fill/fee 捕獲及耐久 → intent settle」，與實際 code 一致。故不能說當前代碼已符合需求卡順序。這是產品/風險契約待決，不由本 reviewer 選擇重排。
- **仍待回答的風險：**若程序在策略 runtime state durable 更新後、完整 fills/fee ledger durable 之前崩潰，重啟需證明 strategy state/outbox 的更新可識別、冪等或可補償，並能依成交唯一 ID/cursor 重放費用而不重計數量/PnL；若先讓 fills 落盤而策略 accounting 失敗，也需有 pending settlement/重放閉環。現有 fill-before-intent-settle gate 只證明 intent 不會過早 settle，不能單獨證明兩本帳的 crash consistency。將此標為未決風險並請 Goal Lead/需求分析確認契約，不宣稱錯帳已發生，也不在本評審替使用者決定順序。

#### 更新後的實作驗收條件

1. 在 `ConfigureIntentJournal` 同綁定 reentry 上定義穩定 backend identity 與 full scope equality；同綁定 settled/rejected-only 不改 map、不重載/覆寫 revisions，完整繼續 bootstrap；不同 backend/scope 不換 binding、不污染原 loaded state，錯誤 hold 有明確 owner。
2. 任一未結/UNKNOWN/ledger-pending 項目使該次 bootstrap recovery error，但保留 `journalLoaded=true`、既有 journal pointer/revision/map 與後續 `ObserveOrder`/settle 能力；不要清 UNKNOWN fencing 或觸碰其他 CID。
3. 驗證 settled-only no-op 並不 bypass full snapshot；venue positions/open orders、策略庫存、ExposureBook Seed 任一失敗，只有 bootstrap 自有 gate 保持，後續 same binding retry 可再次完整核對。
4. 對正成交 strategy-first vs fills/fees-first 由需求/Goal Lead 統一需求卡和架構文字；在此決策前將 crash window 列為未解，不以局部通過測試作經濟帳閉環證明。
5. 保留並補足前節已列 gate ownership、重複事件/CAS、UNKNOWN、ReduceOnly protective close 與生產 callback 接線驗收；不跑真實帳戶/生產環境。

**本輪複審狀態：部分通過方向，架構文件仍需收斂後才適合解鎖實作。**「不刪 settled map、同綁定 no-op、unsettled-preserve-loaded」能解決上輪指出的 map 回收問題，且與 owner fencing 相容；但 backend identity/錯誤副作用未具體化、狀態流仍寫刪除 CID、以及正成交經濟帳順序存在三方不一致。後續需重新對修訂文件及實際 implementation diff 做獨立審查。

### 最終架構複核結論

本輪最新設計已修正前述兩項文件問題：預期狀態流不再移除 settled CID，改為同綁定 journal 冪等再入；並明確要求 backend stable identity + canonical scope key，錯 scope/backend 必須在改 `journalLoaded`、journal binding 或 `IntentRecoveryBlock` 前 side-effect-free 拒絕。這比前版足以指引基本實作方向；同綁定 settled-only no-op、unsettled recovery error 並保持 loaded，亦與當前 coordinator 再跑完整 bootstrap 的需求相符。

仍未閉合、不能由本 Reviewer 自行補決定的項目：

1. **Stable backend identity 的具體提供者/範圍：**架構說介面若無穩定身分則先新增明確 identity，但未指定其由 `IntentJournal`、storage adapter 或 executor binding 提供，以及 wrapper 重建後相同持久化 backend 是否仍相同。需在實作解鎖時一併授權必要檔案範圍並固定語義；不能退回 interface `==`。
2. **D-004 仍待使用者答覆：**正成交 runtime 確認為策略/grid accounting 先行、fills/fees 後續異步耐久；策略帳與成交/費用帳先後及 crash recovery 保證仍未定。依架構明示，此順序不作推斷；在 D-004 決定前不能宣稱經濟帳契約已定稿。
3. **既有 gate/CAS 恢復閉環仍需落到設計或實作驗收：**目前 `strategyIntentSettlementBlock` 仍只有 Block、沒有 Unblock；CAS write uncertainty 會令 `journalLoaded=false` 且 unresolved map 阻止一般 Configure reload（本報告前節有證據）。新版架構未明確指定這兩種 recovery hold 的 owner/解除證據及 CAS revision 重讀入口，故仍有永久封鎖風險。這些需在 backend 解鎖前由 Goal Lead 定清，或明確列為本切片 fail-closed 的人工停止邊界。
4. **重複/並發 settlement 的結果語義仍要可測：**保留 map 令重複事件可認 owner，但架構尚未具體說明 durable-settled CID 的重複 callback 如何避免重複策略 accounting、revision 無意義遞增或設定全域 settlement hold；既有 coordinator 串行只序列化 bootstrap，不序列化每個 CID 的 event/accounting。應在 implementation/QA 契約中逐項證明。

**是否可進入實作：目前不建議解鎖完整切片。**binding/no-op/loaded-state 的主架構現在可實作，且錯綁 side-effect-free 與 CID 保留規則已清楚；但 D-004 等待使用者答覆，且 gate recovery/CAS conflict 的責任尚未定義。可先做不依賴 D-004 的 journal binding/no-op 窄實作（前提是 Goal Lead 明確授權相應檔案並納入測試），但不得把整個終態經濟恢復閉環標為 ready 或驗收通過。此結論不替使用者回答 D-004；本輪未改碼、未改其他文件、未跑測試或外部環境。

## 当前工作树实现只读独立审查 Findings

审查范围：当前工作树中 runtime order update coordinator、正成交 fills/fees 与策略账务顺序、zero-fill 终态复查、intent journal CAS 不确定结果恢复。以下判断依据实现代码，不按提交标题推断。未运行测试、未访问真实账户/生产 DB；本节为静态审查，不能替代运行验证。

### [P1] 策略账务失败会把该订单永久留在 coordinator quarantine，且 settlement gate 无代码内恢复路径

- 证据：`main_execution_fills.go:166-172` 在 `account` 阶段任何错误后将 `state.quarantined = true`；`main_execution_fills.go:151-153` 后续同订单 work 只会再次失败，不再重跑 capture/account/settle。除终态成功外，该 state 不会从 `orders` 删除（`main_execution_fills.go:185-218`）；`cancelState` 只在 coordinator context 取消时删除（`233-251`）。
- 失败场景：策略账务发生暂时性存储错误/锁冲突，fills/fees 已经成功持久化；之后同一终态或重放更新到达时仍被 quarantine 拒绝。订单无法在此 coordinator 内重试结算，且队列最终可满。调用方失败闭包继续阻断该订单 gate（`symbol_manager.go:1070-1078`）。
- 影响与建议：这是未结账务的永久性运行态隔离，不是可恢复 retry。应提供明确的 per-order reconcile/retry 状态转换：只在策略账务重读/幂等重放成功后清除 quarantine，并恢复该订单队列；确保读取过程中较新的累计成交不会被旧快照覆盖。单纯重启进程虽会清内存 state，但不能作为恢复协议。

### [P1] intent settlement 失败遗留全局 gate，当前代码没有对应 Unblock

- 证据：正成交 stage 结算失败时 `main_execution_fills.go:370-372` 调用 `gate.Block(strategyIntentSettlementBlock)`，随后 worker 在失败分支继续而不调用 `complete`（`main_execution_fills.go:178-183,211-213`）。`strategyIntentSettlementBlock` 定义于 `main_intent_journal.go:17-18`；全仓 Go 源码中该常量只有上述 Block 使用，没有 Unblock。另一个异步终态 helper 在 `main_execution_fills.go:674-680` 同样只 Block。失败闭包还会留下每事件的 `owned_order_update_unverified:...` gate（`symbol_manager.go:1062,1070-1078`）。
- 失败场景：venue 查询短暂失败、CAS ack 不确定或 journal 短暂不可用，策略账已写入且 fills/fees 已落盘，但 intent settlement 暂时失败；后续成功 retry 即使完成，也只会在 `complete` 中解除该事件 gate（`symbol_manager.go:1080-1082`），不会解除全局 `strategyIntentSettlementBlock`，且 quarantine 情况下甚至无法进入成功 retry。
- 影响与建议：会长期封锁新开仓，形成“安全但悬置”的能力退化。为该 hold 指定唯一 owner 和可审计的解除证据；恢复流程必须重读 durable intent、确认 owner/terminal/fills/策略账，然后成对解除该 gate 及事件 gate。确认保护性平仓/减仓仍走独立路径并补生产接线测试；静态代码不能据此断言平仓已受阻。

### [P1] fills/fees-first 顺序已落入实现，但与待确认的 D-004 契约及跨账 crash recovery 尚未闭合

- 证据：coordinator 严格先执行 `capture`、后执行 `account`、再 `settle`（`main_execution_fills.go:160-182`）。生产接线的 positive-fill capture 调用 `persistOrderFillsBeforeAccounting`（`main_execution_fills.go:315-321`，其同步持久化在 `280-305`）；策略账务位于之后的 `337-353`，intent settlement 更晚（`355-388`）。`symbol_manager.go:1059-1115` 将正成交回调接入此 coordinator。
- 失败场景：fills/fees 已持久化后，进程崩溃或策略账务失败；重启后须能由 durable intent/fill 证据识别“费用账已写、策略账未完成”，并只补策略账、不重复计量。反向窗口也需覆盖：策略账成功后、intent settle CAS 前崩溃时，恢复不得重复计算策略数量/PnL。当前顺序本身并不能证明两账原子性或恢复幂等性。
- 影响与建议：这是实现已选择的顺序，但此前审查记录 D-004 仍等待用户答复，不能视为契约已批准。先由需求负责人确认顺序及 crash-replay 语义；再以稳定 fill/trade identity、策略账幂等键/重放状态和重启测试证明两种窗口。审查中未发现可据以宣称已发生重复入账的证据。

### [P2] zero-fill 双重 venue 查询提供了保守验证，但生产 callback 的重放/并发覆盖尚无对应 coordinator 测试证据

- 已成立保护：`zeroFillOrderStages.capture` 在策略账务前以 order ID、symbol、可选 CID、terminal status 与零 executed qty 复查 venue（`main_execution_fills.go:465-489`）；settle 阶段调用 `SettleZeroFillIntent`（`526-540`），内部再查询并检查精确身份、terminal 和零成交，之后才 CAS 标记 settled（`order/owned_intents.go:191-257`）。启动恢复也对存储的零成交终态重新查询（`order/intent_journal.go:196-201,240-255`）。对应单元测试覆盖迟到成交、错误 FILLED 状态、瞬时 venue 查询失败（`order/intent_journal_test.go:152-220`）。
- 待验证/失败场景：若第一/第二次查询间 venue 状态出现成交，第二次检查会拒绝 settlement，这是正确的 fail-closed 行为；但该路径仍需证明调用方保留 owner/UNKNOWN fencing，并可由后续 reconcile 完成而不永久卡住。当前 `zeroFillOrderStages` coordinator 接线本身未见专门的并发/重启/真实 callback wiring 测试证据；`processOwnedZeroFillTerminal`（`main_execution_fills.go:392-454`）与 `zeroFillOrderStages` 是两套逻辑，需确认生产实际使用的是后者，避免只测试未接线的 helper。
- 建议：增加无外部服务的生产 callback wiring 测试，覆盖两次查询之间迟到成交、重复/乱序终态、策略账成功而 CAS 失败、重启后再验证，以及恢复后 gate 确实按 owner 释放；测试需断言失败后保护性减仓路径仍可调用。

### [P2] CAS 不确定写的同绑定恢复采取了保守 readback/CAS，但冲突分支只能持续 fencing，尚无冲突协调入口

- 证据：`saveJournalIntentLocked` 对写错误先精确 readback，匹配 `expected+1` 的 payload 则确认成功；匹配原 revision/base 时仅重试一次，否则记录 uncertain payload 并返回错误（`order/intent_journal.go:309-365`）。同绑定 `ConfigureIntentJournal` 在 `journalLoaded=false` 且存在 uncertain write 时调用 reconciliation，成功后再检查所有 unresolved intent（`order/intent_journal.go:123-149`）；恢复只接受目标 revision/payload 或原 base，冲突时返回错误（`414-452`）。SQL backend 用 revision 条件 UPDATE 并以受影响行数判冲突（`storage/execution_intents.go:71-109`）。
- 失败场景：另一个 writer 已推进同 CID revision，或 readback pool 持续不可用；reconcile 无法证明本地 uncertain payload 已 durable，也无法安全覆盖较新记录，于是返回冲突/读取错误并保持 journal failure fencing。这避免了盲写覆盖，但当前没有自动合并或 owner-authorized 冲突解决入口。另一个需实测的边界是写连接与独立 `commitProbe` 的可见性/故障组合；实现通过 readback 处理模糊 ack，但静态审查无法证明各支持数据库下的时序行为。
- 建议：保留 fail-closed，不要在不匹配 revision 上强行重试；补可观测的人工/运维 reconcile 流程，要求精确 scope、CID、revision、payload hash 和 venue/ledger 证据，并测试 CAS 成功但响应丢失、CAS 未提交、并发推进、readback 不可用及 SQLite/MySQL 两种后端。该项是恢复能力缺口，不表示 CAS 保护本身失效。

**总体结论：**实现已接上 fills/fees-first → 策略账务 → intent settlement；zero-fill 和 CAS 采用多重核验及保守冲突 fencing。当前阻止闭环的主要问题是 coordinator 账务失败后的不可恢复 quarantine、settlement gate 缺少解除者，以及 D-004/双账崩溃恢复语义仍未定。没有运行测试，因此以上不构成通过实现验收或盈利准备度验收的结论。

## 最新工作树最终只读复核（更正与剩余 Findings）

本节复核当前工作树，优先级高于本文件前述实现复核中已过时的结论；仅为本节追加更正，不改写历史审查记录。用户已确认 D-004 为 fills/fees durable-first，且团队记录当前 root、`./order`、`./execution`、storage 定向测试通过（版本进度记录 `progress.md:56-60`）。我未重新执行测试；不据此宣称全仓、race 或 MySQL 测试通过。

### [P1] settled quantity 检出 late fill 后，较旧 duplicate 事件会错误清除同一 per-order reconciliation gate

- 证据：`runtimeOrderUpdateCoordinator.Submit` 对已 settled CID 比较累计量；高于 settled qty 时走 `failure`，不高于时直接走 `skipped`（`main_execution_fills.go:73-86`）。生产 callback 给同一订单使用稳定 gate key，failure 会 Block，skipped 会无条件 Unblock（`symbol_manager.go:1062-1083,1112-1114`）。
- 失败场景：某已结算订单收到累计成交量高于 durable settled qty 的 late-fill 更新，触发 reconciliation hold；随后旧的、数量不高于已结算值的重复 websocket 更新被当作安全 duplicate，并通过 `skipped` 清掉相同 gate key。late fill 尚未被重新 capture/strategy-account/settle 或显式核账，开仓 gate 却已恢复。
- 建议：把 late-fill/settled-quantity mismatch 记录为独立、sticky 的 reconciliation hold；普通重复/旧事件只能跳过，不能解除它。只有 authoritative venue + fills/fees + owner strategy ledger reconciliation 成功后，才由明确 owner 解除。增加 production-stage/coordinator 回归测试：late fill → stale duplicate → gate 仍阻断；核账成功后才解除。此项是当前明确的安全缺陷。

### [P1] 策略账务失败后的 quarantine 仍没有自动恢复；恢复策略等待用户决策

- 证据：`main_execution_fills.go:183-190` 中 account stage 任意错误都会设置 `state.quarantined`；后续同订单 work 在 `168-170` 只失败返回，不会重试 account/settle。生产阶段遇到 `ApplyOrderUpdateForStrategyWithAccounting` 错误还会 Block `strategy_accounting_unverified`（`main_execution_fills.go:361-369`）。团队记录确认该恢复选择仍等待用户，且指出错误无法区分“未写入”与“已写但确认失败”（`progress.md:62-65`，决策 D-006 于 `tasklist.md:47`）。
- 失败场景：fills/fees 已耐久，策略账务返回错误但底层可能已提交；同订单随后更新被 quarantine 阻止。自动重放可能重复策略入账，永不重放则长期封锁该订单和新开仓。
- 建议：维持当前 fail-closed，不擅自清 quarantine 或重放；等待用户在“具备策略级幂等/提交结果核验后受控重放”与“显式人工核账、授权解锁”之间决策。决策落地后补未提交/已提交模糊结果、重启恢复、同订单新累计量及 gate 归属测试。保护性减仓是否独立可用须由真实调用链测试证明，本审查不推断其被阻断。

### [P2] legacy `strategyIntentSettlementBlock` 在当前生产 symbol callback 中不可达，但旧 helper 仍保留永久 hold 行为

- 生产可达性核对：`symbol_manager.go:1041-1117` 当前订单流使用 coordinator；其正成交 settle stage 直接调用 `SettleReconciledIntent`/`SettleIntent`（`main_execution_fills.go:378-407`），失败走 per-order failure callback。全仓 Go 调用点检索中，`captureTerminalOrderAndSettleOwnedIntent` 只有定义及 `main_execution_fills_test.go` 的测试调用；`settleVerifiedStrategyIntent` 只由该 helper 调用及测试调用。因此 `strategyIntentSettlementBlock` 在现有 production symbol callback 链上没有可达调用点。旧 helper 在 `main_execution_fills.go:712-718` 和 `main_intent_journal.go:35-50` 仍会 Block 且无对称 Unblock；目前是遗留测试/兼容代码风险，不应再描述为当前主生产回调必然留下的全局 settlement hold。
- 建议：删除/隔离遗留 helper，或明确其唯一生产入口和 gate owner/recovery contract；测试避免只验证 legacy helper 而误当成生产路径覆盖。注意生产 account 错误仍有单独的 `strategy_accounting_unverified` hold，属于上一条 quarantine 决策，不与 legacy settlement hold 混为一谈。

### [P2] 定向 callback 测试验证了 stages/coordinator，不等同于真实 `StartOrderStream` symbol callback wiring

- 已有覆盖：`main_order_recovery_callback_test.go:164-230` 直接构造 `ownedPositiveOrderStages` 并调用 coordinator，验证 fills/fees 在策略账前；`233-329` 验证 coordinator 暂时 capture 失败、串行更新及重复终态；`331-359` 测的是 `processOwnedZeroFillTerminal`。团队记录报告对应 root/orders/execution/storage 定向测试通过（`progress.md:56-60`）。
- 缺口：这些测试没有调用 `symbol_manager.go:1041` 注册的真实 `StartOrderStream` callback；测试自行构造 stage、调用 `Submit` 并提供测试 gate closures，因而没有覆盖 callback 中的 symbol/owner filter、`observeOwnedRuntimeOrder`、真实 `gateReason`、failure/complete/skipped 闭包及 late-fill 后 stale duplicate 的组合。zero-fill 测试覆盖的是 helper，而不是生产使用的 `zeroFillOrderStages` callback 链。当前没有足够证据证明真实 wiring 的 gate 清理契约成立。
- 建议：建立可注入 fake stream/runtime seam，让测试触发实际注册 callback；至少覆盖 owner/symbol 拒绝、正成交 durable-first、settled duplicate 与 late fill/stale duplicate、zero-fill 的 `zeroFillOrderStages`、策略账 quarantine、gate 保留/解除及 restart 后路线过滤。没有该测试前，可认可定向阶段测试通过，但不可称真实 symbol callback wiring 已验收。

### 复核中已确认的修正/保护（非 finding）

- **D-004 已解决：**当前实现顺序为 fills/fees durable capture → strategy/grid accounting → terminal intent settle/bootstrap（`main_execution_fills.go:177-199,325-376,378-422`），与用户确认一致。跨库/运行时崩溃窗口仍需幂等恢复证据；这不再是 D-004 未决。
- **owner 与 settled duplicate：**`SettledIntentExecutedQty` 只对仍保留在 executor map 中的 settled owner 返回 durable 累计量（`order/owned_intents.go:105-117`）；`ConfigureIntentJournal` 恢复时保留合法 settled record（`order/intent_journal.go:198-205`），`RecoveredOrderRoutes` 排除 settled/rejected（`order/intent_journal.go:578-595`），因此 owner retention 与“只恢复未结 route”分工清晰。超 settled qty 已能被发现，但上一个 P1 指出其 gate 解除语义尚不安全。
- **CAS retry/readback：**写错误后仅在 exact target revision/payload 已存在时视为成功；读回证明 base revision/payload 未变化时至多安全重试一次，否则保存 uncertain 状态并 fencing（`order/intent_journal.go:333-370,425-455`）。这是保守 CAS 语义；现有测试覆盖安全 retry 与 lost-ack 后重启读回（`order/intent_journal_test.go:594-687`）。本次未发现可据静态代码认定会覆盖并发新 revision 的路径；数据库驱动、MySQL 运行验证和 race 仍不在已声明证据范围内。

**最终结论：**D-004 与 fills/fees-first 已对齐，settled owner retention、route 过滤及 CAS 精确读回/单次安全重试有实现和定向测试支持。仍有一个新的 P1：late-fill mismatch 的 per-order gate 可被旧 duplicate 清掉；策略记账错误 quarantine 的恢复策略继续等待用户选择。legacy 全局 settlement gate 不是当前生产 symbol callback 的可达路径，但遗留 helper 应清理或明确用途。定向测试结果按团队记录采信；真实 `StartOrderStream` callback wiring、race、全仓及 MySQL 均未验收。

### Follow-up：late-fill gate 修正复核

- **P1 late-fill gate：已关闭本轮发现。**当前 `Submit` 在 settled qty 分支中，对不高于已结算累计量的更新直接返回，不再调用 `skipped`（`main_execution_fills.go:73-86`）；超过累计量仍调用 failure 并保留 per-order hold。新增 `TestSettledLateFillHoldCannotBeClearedByOlderDuplicate` 验证“late fill 阻断 → 较旧 duplicate 到达 → hold 仍存在”（`main_order_recovery_callback_test.go:337-360`）。按用户提供的验证结果，该 root 定向测试通过；本次未重跑。结论限于该分支/回归用例，不等于真实 symbol callback wiring 全覆盖。
- **P1 策略 quarantine：仍待用户选择。**account 错误仍会把 coordinator state 标记为 `quarantined`，后续同订单事件不能自动重试（`main_execution_fills.go:183-190,168-170`）；不自行清锁或重放。
- **测试状态更正：**按用户提供结果，late-fill root 定向测试、完整 root 包测试及 race 定向测试通过；完整 root 包耗时 107.269s。身份 wrapper 修正后，capital lock 定向测试重新标记为通过。以上均为用户提供的执行结果，本 reviewer 未运行测试；不外推为全仓、全仓 race 或 MySQL 通过。
- **legacy gate 与 wiring 结论不变：**`strategyIntentSettlementBlock` 仍仅处于 legacy helper 路径，当前生产 `symbol_manager` coordinator 链无调用点；新增 late-fill 测试直接驱动 coordinator 与测试 gate closures，尚未触发注册在 `StartOrderStream` 的完整 symbol callback。因此该 P2 wiring 覆盖缺口仍在。
