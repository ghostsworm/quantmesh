package utils

import (
	"time"
)

var (
	// GlobalLocation 全局配置的时区
	GlobalLocation *time.Location
)

func init() {
	// 默认加载东8区时区
	SetLocation("Asia/Shanghai")
}

// SetLocation 设置全局时区
func SetLocation(name string) error {
	loc, err := time.LoadLocation(name)
	if err != nil {
		// 如果加载失败，尝试常见的时区格式
		if name == "UTC+8" || name == "Asia/Shanghai" {
			GlobalLocation = time.FixedZone("UTC+8", 8*60*60)
			return nil
		}
		// 如果还是失败，保留原有时区或默认值
		if GlobalLocation == nil {
			GlobalLocation = time.Local
		}
		return err
	}
	GlobalLocation = loc
	return nil
}

// ToConfiguredTimezone 将时间转换为配置的时区
func ToConfiguredTimezone(t time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	return t.In(GlobalLocation)
}

// ToUTC8 将UTC时间转换为东8区时间 (保留兼容性，现在根据配置转换)
func ToUTC8(t time.Time) time.Time {
	return ToConfiguredTimezone(t)
}

// ToUTC 将时间转换为UTC时间
func ToUTC(t time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	// 转换为UTC时区
	return t.UTC()
}

// NowUTC 获取当前UTC时间
func NowUTC() time.Time {
	return time.Now().UTC()
}

// NowConfiguredTimezone 获取当前配置时区的时间
func NowConfiguredTimezone() time.Time {
	return time.Now().In(GlobalLocation)
}

// NowUTC8 获取当前东8区时间 (保留兼容性，现在根据配置获取)
func NowUTC8() time.Time {
	return NowConfiguredTimezone()
}
