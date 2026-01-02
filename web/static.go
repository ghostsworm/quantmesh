package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed dist/*
var staticFiles embed.FS

// GetStaticFS 获取静态文件系统
func GetStaticFS() http.FileSystem {
	distFS, err := fs.Sub(staticFiles, "dist")
	if err != nil {
		// 如果 dist 目录不存在，返回空文件系统
		return http.FS(staticFiles)
	}
	return http.FS(distFS)
}

// GetAssetsFS 获取 assets 目录的文件系统（用于提供 CSS、JS 等静态资源）
func GetAssetsFS() http.FileSystem {
	assetsFS, err := fs.Sub(staticFiles, "dist/assets")
	if err != nil {
		// 如果 assets 目录不存在，尝试从 dist 目录获取
		distFS, err2 := fs.Sub(staticFiles, "dist")
		if err2 != nil {
			return http.FS(staticFiles)
		}
		return http.FS(distFS)
	}
	return http.FS(assetsFS)
}
