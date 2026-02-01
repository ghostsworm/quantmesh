# 多語系翻譯維護 (i18n Locales)

本專案有兩處多語系資源：後端 TOML（日誌/錯誤訊息）與前端 WebUI JSON。僅 zh-CN、en-US 為完整基準；其他語系需定期補齊與優化。

## 後端 i18n（Go）

- **路徑**: `i18n/locales/*.toml`
- **語系**: zh-CN、en-US、zh-TW
- **用途**: 日誌、錯誤訊息、首次設置嚮導等
- **維護**: 新增 key 時請同步更新三份 toml；zh-TW 使用繁體用詞（儲存/資料/模組/載入/登入等）。

## 前端 WebUI i18n（React）

- **路徑**: `webui/src/i18n/locales/*.json`
- **語系**: zh-CN、en-US、zh-TW、ar-SA、de-DE、es-ES、fr-FR、hi-IN、id-ID、it-IT、ko-KR、nl-NL、pt-BR、ru-RU、tr-TR、vi-VN
- **基準**: `en-US.json` 為鍵值全集參考；`zh-CN.json` 為簡體中文完整版。

### 1. 補齊缺失鍵（以英文為 fallback）

其他語系若缺少鍵，介面會顯示 key 或空白。可用合併腳本從 en-US 補齊缺失鍵（缺譯處暫時顯示英文）：

```bash
cd webui
node scripts/merge-locales.js
```

執行後會更新除 `en-US.json`、`zh-CN.json` 外的所有 `locales/*.json`，僅補缺失鍵，不覆蓋既有翻譯。

### 2. 繁體中文 zh-TW（本地，無需 API）

由簡體 zh-CN 經 OpenCC 轉為繁體，無需 API Key：

```bash
cd webui
node scripts/zh-cn-to-zh-tw.js
```

會覆寫 `locales/zh-TW.json`。

### 3. 使用 Gemini 批量翻譯其他語系（可選）

若已配置 `GEMINI_API_KEY`（環境變數或專案根目錄 / webui 目錄下的 `.env`），可對除 en-US、zh-CN 外的語系做整份翻譯覆寫：

```bash
cd webui
node scripts/translate-locales.js
```

僅翻譯部分語系：

```bash
node scripts/translate-locales.js fr-FR de-DE ja-JP
```

注意：

- 此腳本會用 Gemini 將 `en-US.json` 整份翻譯後寫入對應語系檔案，會覆蓋該檔案現有內容，建議先備份或提交後再執行。
- 若出現 `User location is not supported for the API use`，表示目前網路地區不在 Gemini 支援範圍，請在本機以 VPN 連到支援地區（例如美國）後再執行上述指令。

## 建議流程

1. 在 `en-US.json`（或 `zh-CN.json`）新增或修改鍵值。
2. 執行 `node webui/scripts/merge-locales.js`，讓其他語系補齊鍵（缺譯顯示英文）。
3. 繁體中文：執行 `node webui/scripts/zh-cn-to-zh-tw.js` 由 zh-CN 生成 zh-TW。
4. 其他語系：在本機執行 `node webui/scripts/translate-locales.js`（需 `GEMINI_API_KEY`；若遇地區限制請用 VPN）；或手動翻譯/校對重點語系。
