# RC1187 決策記錄

## RC1186 舊格式 pending outbox 的 ExpectedRuntimeRevision

- 使用者確認：舊格式 pending outbox 缺少 `ExpectedRuntimeRevision` 的投影哈希綁定時，採 fail-closed；禁止自動投影/重放，轉人工核帳。
- 原因：雖可保留舊 payload hash 的讀取與稽核相容性，但不能事後證明外層 runtime revision 未被單獨改寫。讓這類資料自動進入 CAS 會把未驗證的版本值當作原始授權。
- 實作要求：新寫入將 revision 加入 `ProjectionHash` 覆蓋的 payload；receipt/outbox 與 takeover candidate/pending 檢查必須確認 payload 內外一致。缺欄位即回報需人工核帳，不清除 hold、不執行 runtime CAS、不成功重放。
- 驗收要求：storage 直接測試舊格式 pair 拒絕但 hash 可讀；storage 持久化 metadata 改寫無副作用；FundingCarry 生產 projector 在 CAS 前拒絕改寫；target+2 takeover 與 response-loss 路徑不繞過此檢查。
