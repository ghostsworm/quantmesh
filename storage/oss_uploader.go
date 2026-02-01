package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"quantmesh/logger"
)

// OSSUploader 阿里云 OSS 上传器，用于每日上传审计日志
type OSSUploader struct {
	client        *oss.Client
	bucket        *oss.Bucket
	bucketName    string
	prefix        string
	auditDir      string
	uploadTime    string // "02:00"
	stopCh        chan struct{}
	doneCh        chan struct{}
	mu            sync.Mutex
	lastUploadDay string
}

// OSSConfig OSS 配置
type OSSConfig struct {
	Endpoint        string
	Bucket          string
	AccessKeyID     string
	AccessKeySecret string
	Prefix          string
	UploadTime      string // 每日上传时间，如 "02:00"
	AuditDir        string
}

// NewOSSUploader 创建 OSS 上传器
func NewOSSUploader(cfg OSSConfig) (*OSSUploader, error) {
	if cfg.Endpoint == "" || cfg.Bucket == "" || cfg.AccessKeyID == "" || cfg.AccessKeySecret == "" {
		return nil, fmt.Errorf("OSS 配置不完整：endpoint、bucket、access_key_id、access_key_secret 为必填")
	}
	client, err := oss.New(cfg.Endpoint, cfg.AccessKeyID, cfg.AccessKeySecret)
	if err != nil {
		return nil, fmt.Errorf("创建 OSS 客户端失败: %w", err)
	}
	bucket, err := client.Bucket(cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("获取 Bucket 失败: %w", err)
	}
	if cfg.Prefix == "" {
		cfg.Prefix = "audit/"
	}
	if cfg.UploadTime == "" {
		cfg.UploadTime = "02:00"
	}
	if cfg.AuditDir == "" {
		cfg.AuditDir = "./data/audit"
	}
	return &OSSUploader{
		client:     client,
		bucket:     bucket,
		bucketName: cfg.Bucket,
		prefix:     strings.TrimSuffix(cfg.Prefix, "/"),
		auditDir:   cfg.AuditDir,
		uploadTime: cfg.UploadTime,
		stopCh:     make(chan struct{}),
		doneCh:     make(chan struct{}),
	}, nil
}

// UploadDate 上传指定日期的审计日志文件（格式 2006-01-02）
func (u *OSSUploader) UploadDate(date string) (uploaded int, err error) {
	if u == nil || u.bucket == nil {
		return 0, nil
	}
	// 查找 auditDir 下 audit_trades_<date>.csv 和 audit_trades_<date>.jsonl
	baseName := "audit_trades_" + date
	csvPath := filepath.Join(u.auditDir, baseName+".csv")
	jsonlPath := filepath.Join(u.auditDir, baseName+".jsonl")

	for _, path := range []string{csvPath, jsonlPath} {
		ok, e := u.uploadFile(path, date)
		if e != nil {
			logger.Warn("上传审计日志失败 %s: %v", path, e)
			continue
		}
		if ok {
			uploaded++
		}
	}
	return uploaded, nil
}

func (u *OSSUploader) uploadFile(localPath, date string) (bool, error) {
	fi, err := os.Stat(localPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if fi.IsDir() {
		return false, nil
	}
	var objectKey string
	if u.prefix != "" {
		objectKey = u.prefix + "/" + date + "/" + filepath.Base(localPath)
	} else {
		objectKey = date + "/" + filepath.Base(localPath)
	}
	err = u.bucket.PutObjectFromFile(objectKey, localPath)
	if err != nil {
		return false, err
	}
	logger.Info("✅ 审计日志已上传 OSS: %s -> %s", localPath, objectKey)
	return true, nil
}

// Start 启动每日定时上传
func (u *OSSUploader) Start() {
	if u == nil {
		return
	}
	go u.runScheduler()
	logger.Info("OSS 审计日志每日上传已启动，上传时间: %s", u.uploadTime)
}

// Stop 停止调度
func (u *OSSUploader) Stop() {
	if u == nil {
		return
	}
	close(u.stopCh)
	<-u.doneCh
}

func (u *OSSUploader) runScheduler() {
	defer close(u.doneCh)
	// 解析每日上传时间 "HH:MM"
	parts := strings.Split(u.uploadTime, ":")
	hour, min := 2, 0
	if len(parts) >= 1 {
		fmt.Sscanf(parts[0], "%d", &hour)
	}
	if len(parts) >= 2 {
		fmt.Sscanf(parts[1], "%d", &min)
	}
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-u.stopCh:
			return
		case now := <-ticker.C:
			if now.Hour() == hour && now.Minute() == min {
				// 上传前一天的数据
				yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")
				u.mu.Lock()
				if u.lastUploadDay == yesterday {
					u.mu.Unlock()
					continue
				}
				u.lastUploadDay = yesterday
				u.mu.Unlock()
				n, err := u.UploadDate(yesterday)
				if err != nil {
					logger.Warn("每日审计日志上传失败: %v", err)
				} else if n > 0 {
					logger.Info("每日审计日志上传完成: %s 共 %d 个文件", yesterday, n)
				}
			}
		}
	}
}
