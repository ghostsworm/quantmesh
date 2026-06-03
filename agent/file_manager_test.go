package agent

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFileManagerSaveUploadedFileRoutesByContentType(t *testing.T) {
	t.Parallel()

	fm := NewFileManager(t.TempDir(), "http://localhost:8080")

	cases := []struct {
		name        string
		filename    string
		contentType string
		wantDir     string
	}{
		{name: "image", filename: "avatar.png", contentType: "image/png", wantDir: "images"},
		{name: "video", filename: "clip.mp4", contentType: "video/mp4", wantDir: "videos"},
		{name: "document", filename: "note.txt", contentType: "text/plain", wantDir: "documents"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			header := uploadedFileHeader(t, tc.filename, tc.contentType, []byte("payload"))
			webURL, relPath, err := fm.SaveUploadedFile(header)
			if err != nil {
				t.Fatalf("SaveUploadedFile returned error: %v", err)
			}
			if !strings.Contains(webURL, "/shared/"+tc.wantDir+"/") {
				t.Fatalf("webURL = %q, want directory %q", webURL, tc.wantDir)
			}
			if !strings.HasPrefix(relPath, tc.wantDir+string(os.PathSeparator)) {
				t.Fatalf("relPath = %q, want directory %q", relPath, tc.wantDir)
			}
			data, err := os.ReadFile(filepath.Join(fm.sharedDir, relPath))
			if err != nil {
				t.Fatalf("saved file missing: %v", err)
			}
			if string(data) != "payload" {
				t.Fatalf("saved data = %q", data)
			}
		})
	}
}

func TestFileManagerSaveGeneratedImageAndFiles(t *testing.T) {
	t.Parallel()

	fm := NewFileManager(t.TempDir(), "https://example.test")

	imageCases := []struct {
		filename string
		mimeType string
		wantExt  string
	}{
		{filename: "chart", mimeType: "image/png", wantExt: ".png"},
		{filename: "photo", mimeType: "image/jpeg", wantExt: ".jpg"},
		{filename: "loop", mimeType: "image/gif", wantExt: ".gif"},
		{filename: "preview", mimeType: "image/webp", wantExt: ".webp"},
		{filename: "blob", mimeType: "application/octet-stream", wantExt: ".bin"},
		{filename: "already.svg", mimeType: "image/svg+xml", wantExt: ".svg"},
	}
	for _, tc := range imageCases {
		tc := tc
		t.Run("image_"+tc.wantExt, func(t *testing.T) {
			t.Parallel()
			webURL, relPath, err := fm.SaveGeneratedImage([]byte("img"), tc.filename, tc.mimeType)
			if err != nil {
				t.Fatalf("SaveGeneratedImage returned error: %v", err)
			}
			if !strings.Contains(webURL, "/shared/images/") {
				t.Fatalf("webURL = %q", webURL)
			}
			if filepath.Ext(relPath) != tc.wantExt {
				t.Fatalf("relPath = %q, want extension %q", relPath, tc.wantExt)
			}
		})
	}

	fileCases := []struct {
		fileType string
		wantDir  string
	}{
		{fileType: "video", wantDir: "videos"},
		{fileType: "chart", wantDir: "charts"},
		{fileType: "report", wantDir: "documents"},
	}
	for _, tc := range fileCases {
		tc := tc
		t.Run("file_"+tc.wantDir, func(t *testing.T) {
			t.Parallel()
			webURL, relPath, err := fm.SaveGeneratedFile([]byte("data"), "result", tc.fileType, "application/octet-stream")
			if err != nil {
				t.Fatalf("SaveGeneratedFile returned error: %v", err)
			}
			if !strings.Contains(webURL, "/shared/"+tc.wantDir+"/") {
				t.Fatalf("webURL = %q", webURL)
			}
			if !strings.HasPrefix(relPath, tc.wantDir+string(os.PathSeparator)) || filepath.Ext(relPath) != ".bin" {
				t.Fatalf("relPath = %q", relPath)
			}
		})
	}
}

func TestGetFileExtensionAndCleanupOldFiles(t *testing.T) {
	t.Parallel()

	extensionCases := map[string]string{
		"image/png":       ".png",
		"image/jpeg":      ".jpg",
		"image/jpg":       ".jpg",
		"image/gif":       ".gif",
		"image/webp":      ".webp",
		"video/mp4":       ".mp4",
		"video/webm":      ".webm",
		"application/pdf": ".pdf",
		"text/plain":      ".bin",
	}
	for mimeType, want := range extensionCases {
		if got := GetFileExtension(mimeType); got != want {
			t.Fatalf("GetFileExtension(%q) = %q, want %q", mimeType, got, want)
		}
	}

	root := t.TempDir()
	fm := NewFileManager(root, "")
	oldPath := filepath.Join(root, "documents", "old.txt")
	newPath := filepath.Join(root, "documents", "new.txt")
	if err := os.WriteFile(oldPath, []byte("old"), 0600); err != nil {
		t.Fatalf("write old file: %v", err)
	}
	if err := os.WriteFile(newPath, []byte("new"), 0600); err != nil {
		t.Fatalf("write new file: %v", err)
	}
	oldTime := time.Now().AddDate(0, 0, -10)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes old file: %v", err)
	}

	if err := fm.CleanupOldFiles(7); err != nil {
		t.Fatalf("CleanupOldFiles returned error: %v", err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old file still exists or stat failed unexpectedly: %v", err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("new file should remain: %v", err)
	}
}

func uploadedFileHeader(t *testing.T, filename, contentType string, data []byte) *multipart.FileHeader {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	partHeader := make(textproto.MIMEHeader)
	partHeader.Set("Content-Disposition", `form-data; name="file"; filename="`+filename+`"`)
	partHeader.Set("Content-Type", contentType)
	part, err := writer.CreatePart(partHeader)
	if err != nil {
		t.Fatalf("CreatePart failed: %v", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("write multipart part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if err := req.ParseMultipartForm(int64(body.Len()) + 1024); err != nil {
		t.Fatalf("ParseMultipartForm failed: %v", err)
	}
	files := req.MultipartForm.File["file"]
	if len(files) != 1 {
		t.Fatalf("multipart files length = %d, want 1", len(files))
	}
	return files[0]
}
