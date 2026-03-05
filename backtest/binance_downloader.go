package backtest

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"quantmesh/logger"
)

// BinanceDownloader 币安历史数据下载器
// 从 https://data.binance.vision/ 下载历史K线和资金费率数据
type BinanceDownloader struct {
	DataDir    string
	Symbol     string
	Interval   string // 1m, 5m, 15m, 1h, 4h, 1d
	HTTPClient *http.Client
}

// NewBinanceDownloader 创建币安数据下载器
func NewBinanceDownloader(dataDir, symbol, interval string) *BinanceDownloader {
	return &BinanceDownloader{
		DataDir:  dataDir,
		Symbol:   strings.ToUpper(symbol),
		Interval: interval,
		HTTPClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

const (
	binanceBaseURL = "https://data.binance.vision/data/futures/um/monthly"
)

// getKlinesURL 获取K线数据URL
func (d *BinanceDownloader) getKlinesURL(year, month int) string {
	return fmt.Sprintf("%s/klines/%s/%s/%s-%s-%d-%02d.zip",
		binanceBaseURL, d.Symbol, d.Interval, d.Symbol, d.Interval, year, month)
}

// getFundingRateURL 获取资金费率数据URL
func (d *BinanceDownloader) getFundingRateURL(year, month int) string {
	return fmt.Sprintf("%s/fundingRate/%s/%s-fundingRate-%d-%02d.zip",
		binanceBaseURL, d.Symbol, d.Symbol, year, month)
}

// downloadFile 下载文件
func (d *BinanceDownloader) downloadFile(url, destPath string) error {
	logger.Info("Downloading: %s", url)

	// 创建目录
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// 检查文件是否已存在
	if _, err := os.Stat(destPath); err == nil {
		logger.Info("File already exists: %s", destPath)
		return nil
	}

	// 发起HTTP请求
	resp, err := d.HTTPClient.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("file not found (404): %s", url)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// 创建目标文件
	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer out.Close()

	// 写入文件
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	logger.Info("Downloaded successfully: %s", destPath)
	return nil
}

// extractZip 解压zip文件
func (d *BinanceDownloader) extractZip(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("failed to open zip: %w", err)
	}
	defer r.Close()

	// 创建目标目录
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// 解压每个文件
	for _, f := range r.File {
		filePath := filepath.Join(destDir, f.Name)

		// 创建文件
		outFile, err := os.Create(filePath)
		if err != nil {
			return fmt.Errorf("failed to create file: %w", err)
		}

		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return fmt.Errorf("failed to open file in zip: %w", err)
		}

		_, err = io.Copy(outFile, rc)
		rc.Close()
		outFile.Close()

		if err != nil {
			return fmt.Errorf("failed to extract file: %w", err)
		}
	}

	logger.Info("Extracted: %s -> %s", zipPath, destDir)
	return nil
}

// DownloadMonthKlines 下载指定月份的K线数据
func (d *BinanceDownloader) DownloadMonthKlines(year, month int) error {
	url := d.getKlinesURL(year, month)
	zipPath := filepath.Join(d.DataDir, "klines", d.Symbol, d.Interval, fmt.Sprintf("%s-%s-%d-%02d.zip", d.Symbol, d.Interval, year, month))

	if err := d.downloadFile(url, zipPath); err != nil {
		return err
	}

	// 解压文件
	destDir := filepath.Join(d.DataDir, "klines", d.Symbol, d.Interval)
	if err := d.extractZip(zipPath, destDir); err != nil {
		return err
	}

	return nil
}

// DownloadMonthFundingRate 下载指定月份的资金费率数据
func (d *BinanceDownloader) DownloadMonthFundingRate(year, month int) error {
	url := d.getFundingRateURL(year, month)
	zipPath := filepath.Join(d.DataDir, "funding_rate", d.Symbol, fmt.Sprintf("%s-fundingRate-%d-%02d.zip", d.Symbol, year, month))

	if err := d.downloadFile(url, zipPath); err != nil {
		return err
	}

	// 解压文件
	destDir := filepath.Join(d.DataDir, "funding_rate", d.Symbol)
	if err := d.extractZip(zipPath, destDir); err != nil {
		return err
	}

	return nil
}

// DownloadRange 下载指定时间范围的数据
func (d *BinanceDownloader) DownloadRange(start, end time.Time) error {
	// K线数据
	logger.Info("Downloading K-line data for %s from %s to %s", d.Symbol, start.Format("2006-01"), end.Format("2006-01"))
	for y := start.Year(); y <= end.Year(); y++ {
		for m := 1; m <= 12; m++ {
			if y == start.Year() && m < int(start.Month()) {
				continue
			}
			if y == end.Year() && m > int(end.Month()) {
				break
			}
			if err := d.DownloadMonthKlines(y, m); err != nil {
				logger.Warn("Failed to download K-line %d-%02d: %v", y, m, err)
			}
		}
	}

	// 资金费率数据
	logger.Info("Downloading funding rate data for %s from %s to %s", d.Symbol, start.Format("2006-01"), end.Format("2006-01"))
	for y := start.Year(); y <= end.Year(); y++ {
		for m := 1; m <= 12; m++ {
			if y == start.Year() && m < int(start.Month()) {
				continue
			}
			if y == end.Year() && m > int(end.Month()) {
				break
			}
			if err := d.DownloadMonthFundingRate(y, m); err != nil {
				logger.Warn("Failed to download funding rate %d-%02d: %v", y, m, err)
			}
		}
	}

	return nil
}

// CheckAvailability 检查数据是否可用
func (d *BinanceDownloader) CheckAvailability() (klinesAvailable, fundingAvailable map[string]bool, err error) {
	klinesAvailable = make(map[string]bool)
	fundingAvailable = make(map[string]bool)

	// 检查当前月份和上个月
	now := time.Now().UTC()
	monthsToCheck := []time.Time{
		now,
		now.AddDate(0, -1, 0),
	}

	for _, t := range monthsToCheck {
		year := t.Year()
		month := t.Month()
		key := fmt.Sprintf("%d-%02d", year, month)

		// 检查K线
		url := d.getKlinesURL(year, int(month))
		if resp, err := d.HTTPClient.Head(url); err == nil {
			klinesAvailable[key] = resp.StatusCode == http.StatusOK
			resp.Body.Close()
		}

		// 检查资金费率
		url = d.getFundingRateURL(year, int(month))
		if resp, err := d.HTTPClient.Head(url); err == nil {
			fundingAvailable[key] = resp.StatusCode == http.StatusOK
			resp.Body.Close()
		}
	}

	return klinesAvailable, fundingAvailable, nil
}

// GetLatestDataTime 获取最新数据的时间
func (d *BinanceDownloader) GetLatestDataTime() (klinesTime, fundingTime time.Time, err error) {
	// 尝试获取当前月份的数据
	now := time.Now().UTC()
	year := now.Year()
	month := int(now.Month())

	// 检查K线
	url := d.getKlinesURL(year, month)
	if resp, err := d.HTTPClient.Head(url); err == nil {
		if resp.StatusCode == http.StatusOK {
			klinesTime = now
		}
		resp.Body.Close()
	}

	// 检查资金费率
	url = d.getFundingRateURL(year, month)
	if resp, err := d.HTTPClient.Head(url); err == nil {
		if resp.StatusCode == http.StatusOK {
			fundingTime = now
		}
		resp.Body.Close()
	}

	return klinesTime, fundingTime, nil
}

// ParseTimeRange 解析时间范围字符串
func ParseTimeRange(startStr, endStr string) (start, end time.Time, err error) {
	// 支持格式：YYYY-MM, YYYY-MM-DD
	if startStr != "" {
		if len(startStr) == 7 { // YYYY-MM
			start, err = time.Parse("2006-01", startStr)
		} else {
			start, err = time.Parse("2006-01-02", startStr)
		}
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid start time format: %w", err)
		}
	} else {
		// 默认开始时间：3个月前
		start = time.Now().AddDate(0, -3, 0)
	}

	if endStr != "" {
		if len(endStr) == 7 { // YYYY-MM
			end, err = time.Parse("2006-01", endStr)
			// 设置为月末
			if err == nil {
				end = end.AddDate(0, 1, 0).Add(-time.Second)
			}
		} else {
			end, err = time.Parse("2006-01-02", endStr)
		}
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid end time format: %w", err)
		}
	} else {
		end = time.Now()
	}

	return start, end, nil
}

// DataInfo 数据信息
type DataInfo struct {
	Symbol           string
	Interval         string
	KlinesFiles      []string
	FundingFiles     []string
	EarliestKline    time.Time
	LatestKline      time.Time
	EarliestFunding  time.Time
	LatestFunding    time.Time
	KlinesSizeMB     float64
	FundingSizeMB    float64
}

// GetDataInfo 获取数据信息
func (d *BinanceDownloader) GetDataInfo() (*DataInfo, error) {
	info := &DataInfo{
		Symbol:   d.Symbol,
		Interval: d.Interval,
	}

	// 扫描K线文件
	klinesDir := filepath.Join(d.DataDir, "klines", d.Symbol, d.Interval)
	if files, err := filepath.Glob(filepath.Join(klinesDir, "*.csv")); err == nil {
		info.KlinesFiles = files
		for _, file := range files {
			// 解析文件名获取时间
			base := filepath.Base(file)
			parts := strings.Split(base, "-")
			if len(parts) >= 4 {
				year, _ := strconv.Atoi(parts[2])
				month, _ := strconv.Atoi(parts[3][:2])
				fileTime := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)

				if info.EarliestKline.IsZero() || fileTime.Before(info.EarliestKline) {
					info.EarliestKline = fileTime
				}
				if fileTime.After(info.LatestKline) {
					info.LatestKline = fileTime
				}
			}

			// 获取文件大小
			if fi, err := os.Stat(file); err == nil {
				info.KlinesSizeMB += float64(fi.Size()) / (1024 * 1024)
			}
		}
	}

	// 扫描资金费率文件
	fundingDir := filepath.Join(d.DataDir, "funding_rate", d.Symbol)
	if files, err := filepath.Glob(filepath.Join(fundingDir, "*.csv")); err == nil {
		info.FundingFiles = files
		for _, file := range files {
			// 解析文件名获取时间
			base := filepath.Base(file)
			parts := strings.Split(base, "-")
			if len(parts) >= 4 {
				year, _ := strconv.Atoi(parts[2])
				month, _ := strconv.Atoi(parts[3][:2])
				fileTime := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)

				if info.EarliestFunding.IsZero() || fileTime.Before(info.EarliestFunding) {
					info.EarliestFunding = fileTime
				}
				if fileTime.After(info.LatestFunding) {
					info.LatestFunding = fileTime
				}
			}

			// 获取文件大小
			if fi, err := os.Stat(file); err == nil {
				info.FundingSizeMB += float64(fi.Size()) / (1024 * 1024)
			}
		}
	}

	return info, nil
}
