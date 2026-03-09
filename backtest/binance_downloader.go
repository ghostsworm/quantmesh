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

// BinanceDownloader 幣安歷史數據下載器
// 從 https://data.binance.vision/ 下載歷史K線、資金費率和 aggTrade 數據
type BinanceDownloader struct {
	DataDir    string
	Symbol     string
	Interval   string // 1m, 5m, 15m, 1h, 4h, 1d (僅 K線使用)
	HTTPClient *http.Client
}

// NewBinanceDownloader 創建幣安數據下載器
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
	binanceBaseURL   = "https://data.binance.vision/data/futures/um/monthly"
	binanceDailyURL  = "https://data.binance.vision/data/futures/um/daily"
)

// getKlinesURL 獲取K線數據URL
func (d *BinanceDownloader) getKlinesURL(year, month int) string {
	return fmt.Sprintf("%s/klines/%s/%s/%s-%s-%d-%02d.zip",
		binanceBaseURL, d.Symbol, d.Interval, d.Symbol, d.Interval, year, month)
}

// getFundingRateURL 獲取資金費率數據URL
func (d *BinanceDownloader) getFundingRateURL(year, month int) string {
	return fmt.Sprintf("%s/fundingRate/%s/%s-fundingRate-%d-%02d.zip",
		binanceBaseURL, d.Symbol, d.Symbol, year, month)
}

// downloadFile 下載檔案
func (d *BinanceDownloader) downloadFile(url, destPath string) error {
	logger.Info("Downloading: %s", url)

	// 創建目錄
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// 檢查檔案是否已存在
	if _, err := os.Stat(destPath); err == nil {
		logger.Info("File already exists: %s", destPath)
		return nil
	}

	// 發起HTTP請求
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

	// 創建目標檔案
	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer out.Close()

	// 寫入檔案
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	logger.Info("Downloaded successfully: %s", destPath)
	return nil
}

// extractZip 解壓zip檔案
func (d *BinanceDownloader) extractZip(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("failed to open zip: %w", err)
	}
	defer r.Close()

	// 創建目標目錄
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// 解壓每個檔案
	for _, f := range r.File {
		filePath := filepath.Join(destDir, f.Name)

		// 創建檔案
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

// DownloadMonthKlines 下載指定月份的K線數據
func (d *BinanceDownloader) DownloadMonthKlines(year, month int) error {
	url := d.getKlinesURL(year, month)
	zipPath := filepath.Join(d.DataDir, "klines", d.Symbol, d.Interval, fmt.Sprintf("%s-%s-%d-%02d.zip", d.Symbol, d.Interval, year, month))

	if err := d.downloadFile(url, zipPath); err != nil {
		return err
	}

	// 解壓檔案
	destDir := filepath.Join(d.DataDir, "klines", d.Symbol, d.Interval)
	if err := d.extractZip(zipPath, destDir); err != nil {
		return err
	}

	return nil
}

// DownloadMonthFundingRate 下載指定月份的資金費率數據
func (d *BinanceDownloader) DownloadMonthFundingRate(year, month int) error {
	url := d.getFundingRateURL(year, month)
	zipPath := filepath.Join(d.DataDir, "funding_rate", d.Symbol, fmt.Sprintf("%s-fundingRate-%d-%02d.zip", d.Symbol, year, month))

	if err := d.downloadFile(url, zipPath); err != nil {
		return err
	}

	// 解壓檔案
	destDir := filepath.Join(d.DataDir, "funding_rate", d.Symbol)
	if err := d.extractZip(zipPath, destDir); err != nil {
		return err
	}

	return nil
}

// DownloadRange 下載指定時間範圍的數據
func (d *BinanceDownloader) DownloadRange(start, end time.Time) error {
	// K線數據
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

	// 資金費率數據
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

// CheckAvailability 檢查數據是否可用
func (d *BinanceDownloader) CheckAvailability() (klinesAvailable, fundingAvailable map[string]bool, err error) {
	klinesAvailable = make(map[string]bool)
	fundingAvailable = make(map[string]bool)

	// 檢查當前月份和上个月
	now := time.Now().UTC()
	monthsToCheck := []time.Time{
		now,
		now.AddDate(0, -1, 0),
	}

	for _, t := range monthsToCheck {
		year := t.Year()
		month := t.Month()
		key := fmt.Sprintf("%d-%02d", year, month)

		// 檢查K線
		url := d.getKlinesURL(year, int(month))
		if resp, err := d.HTTPClient.Head(url); err == nil {
			klinesAvailable[key] = resp.StatusCode == http.StatusOK
			resp.Body.Close()
		}

		// 檢查資金費率
		url = d.getFundingRateURL(year, int(month))
		if resp, err := d.HTTPClient.Head(url); err == nil {
			fundingAvailable[key] = resp.StatusCode == http.StatusOK
			resp.Body.Close()
		}
	}

	return klinesAvailable, fundingAvailable, nil
}

// GetLatestDataTime 獲取最新數據的時間
func (d *BinanceDownloader) GetLatestDataTime() (klinesTime, fundingTime time.Time, err error) {
	// 嘗試獲取當前月份的數據
	now := time.Now().UTC()
	year := now.Year()
	month := int(now.Month())

	// 檢查K線
	url := d.getKlinesURL(year, month)
	if resp, err := d.HTTPClient.Head(url); err == nil {
		if resp.StatusCode == http.StatusOK {
			klinesTime = now
		}
		resp.Body.Close()
	}

	// 檢查資金費率
	url = d.getFundingRateURL(year, month)
	if resp, err := d.HTTPClient.Head(url); err == nil {
		if resp.StatusCode == http.StatusOK {
			fundingTime = now
		}
		resp.Body.Close()
	}

	return klinesTime, fundingTime, nil
}

// ParseTimeRange 解析時間範圍字符串
func ParseTimeRange(startStr, endStr string) (start, end time.Time, err error) {
	// 支援格式：YYYY-MM, YYYY-MM-DD
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
		// 預設开始時間：3个月前
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

// DataInfo 數據信息
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

// GetDataInfo 獲取數據信息
func (d *BinanceDownloader) GetDataInfo() (*DataInfo, error) {
	info := &DataInfo{
		Symbol:   d.Symbol,
		Interval: d.Interval,
	}

	// 扫描K線檔案
	klinesDir := filepath.Join(d.DataDir, "klines", d.Symbol, d.Interval)
	if files, err := filepath.Glob(filepath.Join(klinesDir, "*.csv")); err == nil {
		info.KlinesFiles = files
		for _, file := range files {
			// 解析檔案名獲取時間
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

			// 獲取檔案大小
			if fi, err := os.Stat(file); err == nil {
				info.KlinesSizeMB += float64(fi.Size()) / (1024 * 1024)
			}
		}
	}

	// 扫描資金費率檔案
	fundingDir := filepath.Join(d.DataDir, "funding_rate", d.Symbol)
	if files, err := filepath.Glob(filepath.Join(fundingDir, "*.csv")); err == nil {
		info.FundingFiles = files
		for _, file := range files {
			// 解析檔案名獲取時間
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

			// 獲取檔案大小
			if fi, err := os.Stat(file); err == nil {
				info.FundingSizeMB += float64(fi.Size()) / (1024 * 1024)
			}
		}
	}

	return info, nil
}

// ========== aggTrade 數據下載 ==========

// getAggTradesURL 獲取聚合交易數據URL (按天)
func (d *BinanceDownloader) getAggTradesURL(date time.Time) string {
	return fmt.Sprintf("%s/aggTrades/%s/%s-aggTrades-%s.zip",
		binanceDailyURL, d.Symbol, d.Symbol, date.Format("2006-01-02"))
}

// DownloadDayAggTrades 下載指定日期的聚合交易數據
func (d *BinanceDownloader) DownloadDayAggTrades(date time.Time) error {
	url := d.getAggTradesURL(date)
	dateStr := date.Format("2006-01-02")
	zipPath := filepath.Join(d.DataDir, "aggtrades", d.Symbol, fmt.Sprintf("%s-aggTrades-%s.zip", d.Symbol, dateStr))

	logger.Info("Downloading aggTrades for %s on %s", d.Symbol, dateStr)

	if err := d.downloadFile(url, zipPath); err != nil {
		return err
	}

	// 解壓檔案
	destDir := filepath.Join(d.DataDir, "aggtrades", d.Symbol)
	if err := d.extractZip(zipPath, destDir); err != nil {
		return err
	}

	return nil
}

// DownloadRangeAggTrades 下載指定時間範圍的聚合交易數據
func (d *BinanceDownloader) DownloadRangeAggTrades(start, end time.Time) error {
	logger.Info("Downloading aggTrades data for %s from %s to %s", d.Symbol, start.Format("2006-01-02"), end.Format("2006-01-02"))

	for date := start; !date.After(end); date = date.AddDate(0, 0, 1) {
		if err := d.DownloadDayAggTrades(date); err != nil {
			logger.Warn("Failed to download aggTrades %s: %v", date.Format("2006-01-02"), err)
		}
	}

	return nil
}

// CheckAggTradesAvailability 檢查指定日期的 aggTrades 數據是否可用
func (d *BinanceDownloader) CheckAggTradesAvailability(date time.Time) bool {
	url := d.getAggTradesURL(date)
	resp, err := d.HTTPClient.Head(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// GetAggTradesInfo 獲取 aggTrades 數據信息
type AggTradesInfo struct {
	Symbol        string
	Files         []string
	EarliestDate  time.Time
	LatestDate    time.Time
	TotalSizeMB   float64
	TotalTrades   int64
	DateRange     string // 覆蓋的日期範圍
}

// GetAggTradesInfo 獲取 aggTrades 數據統計信息
func (d *BinanceDownloader) GetAggTradesInfo() (*AggTradesInfo, error) {
	info := &AggTradesInfo{
		Symbol: d.Symbol,
	}

	// 扫描 aggTrades 檔案
	aggTradesDir := filepath.Join(d.DataDir, "aggtrades", d.Symbol)
	if files, err := filepath.Glob(filepath.Join(aggTradesDir, "*.csv")); err == nil {
		info.Files = files

		for _, file := range files {
			// 解析檔案名獲取日期
			base := filepath.Base(file)
			// 格式: BTCUSDT-aggTrades-2024-01-01.csv
			parts := strings.Split(base, "-")
			if len(parts) >= 4 {
				dateStr := fmt.Sprintf("%s-%s-%s", parts[2], parts[3], strings.TrimSuffix(parts[4], ".csv"))
				if fileTime, err := time.ParseInLocation("2006-01-02", dateStr, time.UTC); err == nil {
					if info.EarliestDate.IsZero() || fileTime.Before(info.EarliestDate) {
						info.EarliestDate = fileTime
					}
					if fileTime.After(info.LatestDate) {
						info.LatestDate = fileTime
					}
				}
			}

			// 獲取檔案大小
			if fi, err := os.Stat(file); err == nil {
				info.TotalSizeMB += float64(fi.Size()) / (1024 * 1024)
			}
		}

		// 計算日期範圍
		if !info.EarliestDate.IsZero() && !info.LatestDate.IsZero() {
			info.DateRange = fmt.Sprintf("%s ~ %s",
				info.EarliestDate.Format("2006-01-02"),
				info.LatestDate.Format("2006-01-02"))
		}

		// 統計總成交筆數（快速估算：每行約100字節）
		if info.TotalSizeMB > 0 {
			// 假設每筆交易平均 100 字節
			info.TotalTrades = int64(info.TotalSizeMB * 1024 * 1024 / 100)
		}
	}

	return info, nil
}
