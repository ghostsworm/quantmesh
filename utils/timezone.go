package utils

import (
	"time"
)

var (
	// GlobalLocation 全局配置的時区
	GlobalLocation *time.Location
)

func init() {
	// 默认加載东8区時区
	SetLocation("Asia/Shanghai")
}

// SetLocation 設置全局時区
func SetLocation(name string) error {
	loc, err := time.LoadLocation(name)
	if err != nil {
		// 如果加載失败，尝試常见的時区格式
		if name == "UTC+8" || name == "Asia/Shanghai" {
			GlobalLocation = time.FixedZone("UTC+8", 8*60*60)
			return nil
		}
		// 如果还是失败，保留原有時区或默认值
		if GlobalLocation == nil {
			GlobalLocation = time.Local
		}
		return err
	}
	GlobalLocation = loc
	return nil
}

// ToConfiguredTimezone 將時间轉换為配置的時区
func ToConfiguredTimezone(t time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	return t.In(GlobalLocation)
}

// ToUTC8 將UTC時间轉换為东8区時间 (保留兼容性，現在根據配置轉换)
func ToUTC8(t time.Time) time.Time {
	return ToConfiguredTimezone(t)
}

// ToUTC 將時间轉换為UTC時间
func ToUTC(t time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	// 轉换為UTC時区
	return t.UTC()
}

// NowUTC 獲取當前UTC時间
func NowUTC() time.Time {
	return time.Now().UTC()
}

// NowConfiguredTimezone 獲取當前配置時区的時间
func NowConfiguredTimezone() time.Time {
	return time.Now().In(GlobalLocation)
}

// NowUTC8 獲取當前东8区時间 (保留兼容性，現在根據配置獲取)
func NowUTC8() time.Time {
	return NowConfiguredTimezone()
}
