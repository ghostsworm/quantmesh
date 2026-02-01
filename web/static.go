package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed dist/*
var staticFiles embed.FS

// GetStaticFS 獲取静態文件系统
func GetStaticFS() http.FileSystem {
	distFS, err := fs.Sub(staticFiles, "dist")
	if err != nil {
		// 如果 dist 目錄不存在，返回空文件系统
		return http.FS(staticFiles)
	}
	return http.FS(distFS)
}

// GetAssetsFS 獲取 assets 目錄的文件系统（用於提供 CSS、JS 等静態资源）
func GetAssetsFS() http.FileSystem {
	assetsFS, err := fs.Sub(staticFiles, "dist/assets")
	if err != nil {
		// 如果 assets 目錄不存在，尝試從 dist 目錄獲取
		distFS, err2 := fs.Sub(staticFiles, "dist")
		if err2 != nil {
			return http.FS(staticFiles)
		}
		return http.FS(distFS)
	}
	return http.FS(assetsFS)
}

// GetIconsFS 獲取 icons 目錄的文件系统（用於提供 PWA 图標）
func GetIconsFS() http.FileSystem {
	iconsFS, err := fs.Sub(staticFiles, "dist/icons")
	if err != nil {
		return nil
	}
	return http.FS(iconsFS)
}
