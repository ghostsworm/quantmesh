# Contributing to QuantMesh

感謝您考慮為 QuantMesh 做出貢獻。以下為提交 Issue、PR 與程式風格的簡要說明。

## 如何貢獻

- **Star / Watch**：若專案對您有幫助，歡迎 Star 或 Watch 以關注更新。
- **回報問題**：請透過 [GitHub Issues](https://github.com/ghostsworm/quantmesh/issues) 回報 Bug 或建議，並盡量提供重現步驟與環境資訊。
- **功能討論**：較大的功能建議可先在 [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions) 討論。
- **程式碼貢獻**：歡迎提交 Pull Request，請先閱讀下方 PR 流程與程式風格。

## Pull Request 流程

1. **Fork** 本倉庫並在您自己的 fork 上建立分支（建議分支名：`feature/xxx` 或 `fix/xxx`）。
2. 在分支上完成修改，並確保：
   - 通過現有測試：`go test ./...`
   - 通過 linter：`golangci-lint run`（若專案有配置）。
3. 提交時請使用**繁體中文或英文**撰寫 commit message，簡潔描述變更內容。
4. 在 GitHub 上對 `ghostsworm/quantmesh` 的預設分支發起 **Pull Request**，並在描述中說明變更目的與範圍。
5. 維護者會進行審閱；若有修改建議，請在對應 PR 內回覆並更新程式碼。

## 程式與註解風格

- **註解**：僅使用繁體中文或英文，請勿使用簡體中文。
- **程式風格**：遵循 Go 慣例與本專案既有風格；格式化請使用 `gofmt` 或 `go fmt`。
- **新功能**：若涉及配置或對外行為變更，請同步更新 README 或 `docs/` 下相關文件。

## 授權

依 AGPL-3.0，您對本專案之貢獻將以相同 AGPL-3.0 授權發布。

---

如有疑問，可透過 [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions) 或 contact@quantmesh.io 聯繫。

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
