package agent

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"time"

	"quantmesh/logger"
)

// FileManager 文件管理器
type FileManager struct {
	sharedDir string // 共享目录路径
	baseURL   string // Web 访问基础 URL
}

// NewFileManager 创建文件管理器
func NewFileManager(sharedDir, baseURL string) *FileManager {
	// 确保共享目录存在
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		logger.Error("Failed to create shared directory: %v", err)
	}

	// 创建子目录
	subdirs := []string{"images", "videos", "documents", "charts"}
	for _, dir := range subdirs {
		if err := os.MkdirAll(filepath.Join(sharedDir, dir), 0755); err != nil {
			logger.Error("Failed to create subdirectory %s: %v", dir, err)
		}
	}

	return &FileManager{
		sharedDir: sharedDir,
		baseURL:   baseURL,
	}
}

// SaveUploadedFile 保存上传的文件
func (fm *FileManager) SaveUploadedFile(fileHeader *multipart.FileHeader) (string, string, error) {
	// 打开上传的文件
	src, err := fileHeader.Open()
	if err != nil {
		return "", "", fmt.Errorf("failed to open uploaded file: %w", err)
	}
	defer src.Close()

	// 生成唯一文件名
	ext := filepath.Ext(fileHeader.Filename)
	hasher := md5.New()
	hasher.Write([]byte(fmt.Sprintf("%d-%s", time.Now().UnixNano(), fileHeader.Filename)))
	uniqueName := hex.EncodeToString(hasher.Sum(nil)) + ext

	// 根据文件类型确定子目录
	var subdir string
	contentType := fileHeader.Header.Get("Content-Type")
	switch {
	case strings.HasPrefix(contentType, "image/"):
		subdir = "images"
	case strings.HasPrefix(contentType, "video/"):
		subdir = "videos"
	default:
		subdir = "documents"
	}

	// 目标路径
	destPath := filepath.Join(fm.sharedDir, subdir, uniqueName)

	// 创建目标文件
	dst, err := os.Create(destPath)
	if err != nil {
		return "", "", fmt.Errorf("failed to create destination file: %w", err)
	}
	defer dst.Close()

	// 复制文件内容
	if _, err := io.Copy(dst, src); err != nil {
		return "", "", fmt.Errorf("failed to copy file: %w", err)
	}

	// 返回 Web URL 和相对路径
	webURL := fmt.Sprintf("%s/shared/%s/%s", fm.baseURL, subdir, uniqueName)
	relPath := filepath.Join(subdir, uniqueName)

	return webURL, relPath, nil
}

// SaveGeneratedImage 保存 AI 生成的图片
func (fm *FileManager) SaveGeneratedImage(data []byte, filename, mimeType string) (string, string, error) {
	// 生成唯一文件名
	ext := filepath.Ext(filename)
	if ext == "" {
		// 根据 MIME 类型推断扩展名
		switch mimeType {
		case "image/png":
			ext = ".png"
		case "image/jpeg":
			ext = ".jpg"
		case "image/gif":
			ext = ".gif"
		case "image/webp":
			ext = ".webp"
		default:
			ext = ".bin"
		}
	}

	hasher := md5.New()
	hasher.Write([]byte(fmt.Sprintf("%d-%s", time.Now().UnixNano(), filename)))
	uniqueName := hex.EncodeToString(hasher.Sum(nil)) + ext

	// 目标路径
	destPath := filepath.Join(fm.sharedDir, "images", uniqueName)

	// 写入文件
	if err := os.WriteFile(destPath, data, 0644); err != nil {
		return "", "", fmt.Errorf("failed to write image file: %w", err)
	}

	// 返回 Web URL 和相对路径
	webURL := fmt.Sprintf("%s/shared/images/%s", fm.baseURL, uniqueName)
	relPath := filepath.Join("images", uniqueName)

	return webURL, relPath, nil
}

// SaveGeneratedFile 保存 AI 生成的其他文件
func (fm *FileManager) SaveGeneratedFile(data []byte, filename, fileType, mimeType string) (string, string, error) {
	// 确定子目录
	var subdir string
	switch fileType {
	case "video":
		subdir = "videos"
	case "chart":
		subdir = "charts"
	default:
		subdir = "documents"
	}

	// 生成唯一文件名
	ext := filepath.Ext(filename)
	if ext == "" {
		ext = ".bin"
	}

	hasher := md5.New()
	hasher.Write([]byte(fmt.Sprintf("%d-%s", time.Now().UnixNano(), filename)))
	uniqueName := hex.EncodeToString(hasher.Sum(nil)) + ext

	// 目标路径
	destPath := filepath.Join(fm.sharedDir, subdir, uniqueName)

	// 写入文件
	if err := os.WriteFile(destPath, data, 0644); err != nil {
		return "", "", fmt.Errorf("failed to write file: %w", err)
	}

	// 返回 Web URL 和相对路径
	webURL := fmt.Sprintf("%s/shared/%s/%s", fm.baseURL, subdir, uniqueName)
	relPath := filepath.Join(subdir, uniqueName)

	return webURL, relPath, nil
}

// GetFileExtension 从 MIME 类型获取文件扩展名
func GetFileExtension(mimeType string) string {
	switch mimeType {
	case "image/png":
		return ".png"
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	case "application/pdf":
		return ".pdf"
	default:
		return ".bin"
	}
}

// CleanupOldFiles 清理超过指定天数的旧文件
func (fm *FileManager) CleanupOldFiles(days int) error {
	cutoffTime := time.Now().AddDate(0, 0, -days)

	return filepath.Walk(fm.sharedDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 跳过目录
		if info.IsDir() {
			return nil
		}

		// 删除旧文件
		if info.ModTime().Before(cutoffTime) {
			logger.Info("Deleting old file: %s", path)
			return os.Remove(path)
		}

		return nil
	})
}
