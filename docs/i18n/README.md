# 文檔多語系 (docs i18n)

本目錄存放文檔的英文與其他語言版本。倉庫根目錄 [README.md](../../README.md) 為**簡體中文**主說明；繁體版見同目錄 `README.zh-TW.md`。

## 目錄結構

| 目錄 | 語言 | 說明 |
|------|------|------|
| `docs/` | 繁體中文（預設） | 主文檔目錄，新文檔請優先以繁體中文撰寫 |
| `docs/i18n/en/` | English | 英文版文檔 |
| `docs/i18n/zh-TW/` | 繁體中文 | 與 `docs/` 對應的繁體中文副本，便於多語系站點或打包 |

## 既有 i18n 檔案（README）

- `../../README.md`：簡體中文 README（預設）
- `README.zh-TW.md`：繁體中文 README
- `README.zh-Hans.md`：簡體中文副本（與根目錄內容可能不同步時請以根目錄為準）
- `README.en.md`：英文 README
- `README.es.md`、`README.fr.md`、`README.pt.md`：其他語系

## 簡繁轉換

專案內腳本 `scripts/s2t_comments.py` 可將簡體中文轉為繁體中文，目前僅處理程式碼檔案（`.go`、`.ts`、`.tsx`、`.js`、`.css`），不處理 `.md`。

```bash
# 註釋與字串的簡繁轉換（.go, .ts, .tsx 等）
python3 scripts/s2t_comments.py
```

若需將**簡體中文 Markdown 文檔**轉成繁體，可手動撰寫或另寫腳本對 `.md` 套用相同用詞對照；`s2t_comments.py` 內的 `S2T` 對照表可複用。

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
