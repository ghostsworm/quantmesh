package utils

import (
	"time"
)

var (
	// UTC8Location 东8区时区
	UTC8Location *time.Location
)

func init() {
	var err error
	// 加载东8区时区
	UTC8Location, err = time.LoadLocation("Asia/Shanghai")
	if err != nil {
		// 如果加载失败，使用固定偏移量创建
		UTC8Location = time.FixedZone("UTC+8", 8*60*60)
	}
}

// ToUTC8 将UTC时间转换为东8区时间
// 用于返回给客户端的时间
func ToUTC8(t time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	// 如果时间已经是UTC+8，直接返回
	if t.Location() == UTC8Location {
		return t
	}
	// 转换为UTC+8时区
	return t.In(UTC8Location)
}

// ToUTC 将时间转换为UTC时间
// 用于存储到数据库的时间
func ToUTC(t time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	// 转换为UTC时区
	return t.UTC()
}

// NowUTC 获取当前UTC时间
// 用于存储到数据库
func NowUTC() time.Time {
	return time.Now().UTC()
}

// NowUTC8 获取当前东8区时间
// 用于返回给客户端
func NowUTC8() time.Time {
	return time.Now().In(UTC8Location)
}

