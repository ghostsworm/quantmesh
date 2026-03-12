package utils

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"
)

// OrderIDGenerator 订單ID生成器
// 生成紧凑的 ClientOrderID（優先舊格式，超長時切換緊湊格式）
type OrderIDGenerator struct {
	mu       sync.Mutex
	lastSec  int64
	sequence int
}

var globalIDGen = &OrderIDGenerator{}

// GenerateOrderID 生成紧凑的订單ID
// 格式: {price_int}_{side}_{timestamp}{seq}
//
// 参數:
//
//	price: 订單價格
//	side: 订單方向 (BUY/SELL)
//	priceDecimals: 價格精度
//
// 返回值示例:
//
//	65000_B_1702468800001  (價格65000，買單，约18字符)
//	950_S_1702468800123    (價格0.950，賣單，约16字符)
//
// 注意: 舊格式在極端價格位數下可能超長，函數會自動回退到緊湊格式
func GenerateOrderID(price float64, side string, priceDecimals int) string {
	globalIDGen.mu.Lock()
	defer globalIDGen.mu.Unlock()

	// 1. 將價格轉為整數字符串（避免浮点數）
	multiplier := math.Pow(10, float64(priceDecimals))
	priceInt := int64(math.Round(price * multiplier))

	// 2. 方向编碼（單字符）
	sideCode := "B"
	if side == "SELL" {
		sideCode = "S"
	}

	// 3. 生成紧凑的時间戳 + 序列号
	now := time.Now()
	currentSec := now.Unix()

	// 重置序列号（每秒重置）
	if currentSec != globalIDGen.lastSec {
		globalIDGen.lastSec = currentSec
		globalIDGen.sequence = 0
	}

	globalIDGen.sequence++

	// 時间戳(10位) + 序列号(3位) = 13字符
	timestampSeq := fmt.Sprintf("%d%03d", currentSec, globalIDGen.sequence)

	// 優先使用可讀的舊格式
	legacy := fmt.Sprintf("%d_%s_%s", priceInt, sideCode, timestampSeq)
	if len(legacy) <= 18 {
		return legacy
	}

	// 超長時使用緊湊格式: c{price36}_{side}_{ts36}{seq36}
	priceB36 := strings.ToLower(strconv.FormatInt(priceInt, 36))
	tsB36 := strings.ToLower(strconv.FormatInt(currentSec, 36))
	seqB36 := strings.ToLower(strconv.FormatInt(int64(globalIDGen.sequence), 36))
	if len(seqB36) < 2 {
		seqB36 = strings.Repeat("0", 2-len(seqB36)) + seqB36
	}
	if len(seqB36) > 2 {
		seqB36 = seqB36[len(seqB36)-2:]
	}
	return fmt.Sprintf("c%s_%s_%s%s", priceB36, sideCode, tsB36, seqB36)
}

// GenerateOrderIDWithSource 生成带訂單來源標識的 ClientOrderID
// 當 orderSource 為 "stop_loss" 時，在末尾追加 _SL 後綴，便於從交易所返回的訂單中解析訂單來源
// 格式: {price}_{side}_{timestamp}{seq} 或 {price}_{side}_{timestamp}{seq}_SL
// 交易所（如幣安 newClientOrderId）會原樣存儲並返回此 ID，可從訂單歷史/WebSocket 中解析出 order_source
func GenerateOrderIDWithSource(price float64, side string, priceDecimals int, orderSource string) string {
	base := GenerateOrderID(price, side, priceDecimals)
	if orderSource == "stop_loss" {
		return base + "_SL"
	}
	return base
}

// ParseOrderSource 從 ClientOrderID 解析訂單來源
// 支持帶交易所前綴的 ID（如 x-zdfVM8vY65000_S_xxx_SL），_SL 後綴表示止損平倉
// 返回 "stop_loss" 或 "normal"
func ParseOrderSource(clientOrderID string) string {
	if strings.HasSuffix(clientOrderID, "_SL") {
		return "stop_loss"
	}
	return "normal"
}

// ParseOrderID 解析紧凑的订單ID
// 支持帶 _SL 後綴的止損單格式，解析前會自動剝離
// 返回: price, side, timestamp, valid
func ParseOrderID(clientOrderID string, priceDecimals int) (float64, string, int64, bool) {
	// 剝離 _SL 後綴（止損單），保持向後兼容
	baseID := strings.TrimSuffix(clientOrderID, "_SL")
	parts := strings.Split(baseID, "_")
	if len(parts) != 3 {
		return 0, "", 0, false
	}

	var (
		priceInt  int64
		timestamp int64
		err       error
	)

	// 1. 解析價格整數（兼容舊格式與緊湊格式）
	if strings.HasPrefix(parts[0], "c") {
		priceInt, err = strconv.ParseInt(parts[0][1:], 36, 64)
		if err != nil {
			return 0, "", 0, false
		}
	} else {
		priceInt, err = strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return 0, "", 0, false
		}
	}

	// 还原為浮点數價格
	multiplier := math.Pow(10, float64(priceDecimals))
	price := float64(priceInt) / multiplier

	// 2. 解析方向
	sideCode := parts[1]
	side := "BUY"
	if sideCode == "S" {
		side = "SELL"
	}

	// 3. 解析時间戳
	timestampSeq := parts[2]
	if strings.HasPrefix(parts[0], "c") {
		if len(timestampSeq) < 3 { // 至少 ts1 + seq2
			return 0, "", 0, false
		}
		tsPart := timestampSeq[:len(timestampSeq)-2]
		timestamp, err = strconv.ParseInt(tsPart, 36, 64)
		if err != nil {
			return 0, "", 0, false
		}
	} else {
		if len(timestampSeq) < 10 {
			return 0, "", 0, false
		}
		timestamp, err = strconv.ParseInt(timestampSeq[:10], 10, 64)
		if err != nil {
			return 0, "", 0, false
		}
	}

	return price, side, timestamp, true
}

func compactLegacyOrderID(clientOrderID string) (string, bool) {
	baseID := strings.TrimSuffix(clientOrderID, "_SL")
	isStopLoss := strings.HasSuffix(clientOrderID, "_SL")
	parts := strings.Split(baseID, "_")
	if len(parts) != 3 {
		return "", false
	}
	priceInt, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return "", false
	}
	tsPart := parts[2]
	if len(tsPart) < 10 {
		return "", false
	}
	sec, err := strconv.ParseInt(tsPart[:10], 10, 64)
	if err != nil {
		return "", false
	}
	seqVal := int64(0)
	if len(tsPart) > 10 {
		if v, e := strconv.ParseInt(tsPart[10:], 10, 64); e == nil {
			seqVal = v
		}
	}
	seqB36 := strings.ToLower(strconv.FormatInt(seqVal, 36))
	if len(seqB36) < 2 {
		seqB36 = strings.Repeat("0", 2-len(seqB36)) + seqB36
	}
	if len(seqB36) > 2 {
		seqB36 = seqB36[len(seqB36)-2:]
	}
	compact := fmt.Sprintf("c%s_%s_%s%s",
		strings.ToLower(strconv.FormatInt(priceInt, 36)),
		parts[1],
		strings.ToLower(strconv.FormatInt(sec, 36)),
		seqB36,
	)
	if isStopLoss {
		compact += "_SL"
	}
	return compact, true
}

// AddBrokerPrefix 為不同交易所添加返佣前缀
//
// 交易所限制:
//   - Binance: 36字符限制，返佣前缀 "x-zdfVM8vY" (10字符)
//   - Gate.io: 30字符限制，返佣前缀 "t-" (2字符)
func AddBrokerPrefix(exchange, clientOrderID string) string {
	switch exchange {
	case "binance":
		// 币安返佣前缀: x-zdfVM8vY (10字符)
		prefix := "x-zdfVM8vY"
		result := prefix + clientOrderID

		// 长度检查（币安限制36字符）
		if len(result) > 36 {
			if compact, ok := compactLegacyOrderID(clientOrderID); ok {
				result = prefix + compact
			}
		}
		if len(result) > 36 {
			maxIDLen := 36 - len(prefix)
			if maxIDLen > 0 {
				result = prefix + clientOrderID[:maxIDLen]
			} else {
				result = prefix
			}
		}
		return result

	case "gate":
		// Gate.io 返佣前缀: t- (2字符)
		prefix := "t-"
		result := prefix + clientOrderID

		// 长度检查（Gate.io 限制30字符）
		if len(result) > 30 {
			if compact, ok := compactLegacyOrderID(clientOrderID); ok {
				result = prefix + compact
			}
		}
		if len(result) > 30 {
			maxIDLen := 30 - len(prefix)
			if maxIDLen > 0 {
				result = prefix + clientOrderID[:maxIDLen]
			} else {
				result = prefix
			}
		}
		return result

	default:
		return clientOrderID
	}
}

// RemoveBrokerPrefix 移除交易所返佣前缀
func RemoveBrokerPrefix(exchange, clientOrderID string) string {
	switch exchange {
	case "binance":
		prefix := "x-zdfVM8vY"
		if strings.HasPrefix(clientOrderID, prefix) {
			return clientOrderID[len(prefix):]
		}
		return clientOrderID

	case "gate":
		if strings.HasPrefix(clientOrderID, "t-") {
			return clientOrderID[2:]
		}
		return clientOrderID

	default:
		return clientOrderID
	}
}
