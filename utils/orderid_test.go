package utils

import (
	"strings"
	"testing"
)

func TestGenerateOrderID(t *testing.T) {
	price := 65000.5
	side := "BUY"
	decimals := 2

	id1 := GenerateOrderID(price, side, decimals)
	if id1 == "" {
		t.Fatal("生成的订單ID不能為空")
	}

	parsedPrice, parsedSide, _, valid := ParseOrderID(id1, decimals)
	if !valid {
		t.Fatalf("生成的订單ID應可解析: %s", id1)
	}
	if parsedPrice != price {
		t.Errorf("解析價格错误: 期望 %.2f, 得到 %.2f", price, parsedPrice)
	}
	if parsedSide != side {
		t.Errorf("解析方向错误: 期望 %s, 得到 %s", side, parsedSide)
	}

	// 驗证唯一性（连续調用）
	id2 := GenerateOrderID(price, side, decimals)
	if id1 == id2 {
		t.Errorf("生成的订單ID不唯一: %s == %s", id1, id2)
	}
}

func TestParseOrderID(t *testing.T) {
	price := 1234.56
	side := "SELL"
	decimals := 2

	clientOID := GenerateOrderID(price, side, decimals)
	parsedPrice, parsedSide, timestamp, valid := ParseOrderID(clientOID, decimals)

	if !valid {
		t.Fatal("解析订單ID失败")
	}

	if parsedPrice != price {
		t.Errorf("價格解析錯误: 期望 %.2f, 得到 %.2f", price, parsedPrice)
	}

	if parsedSide != side {
		t.Errorf("方向解析錯误: 期望 %s, 得到 %s", side, parsedSide)
	}

	if timestamp == 0 {
		t.Error("時间戳解析錯误: 得到 0")
	}
}

func TestGenerateOrderIDWithSource(t *testing.T) {
	price := 65000.5
	side := "SELL"
	decimals := 2

	// 正常單
	idNormal := GenerateOrderIDWithSource(price, side, decimals, "")
	if strings.HasSuffix(idNormal, "_SL") {
		t.Errorf("正常單不應有 _SL 後綴: %s", idNormal)
	}

	// 止損單
	idSL := GenerateOrderIDWithSource(price, side, decimals, "stop_loss")
	if !strings.HasSuffix(idSL, "_SL") {
		t.Errorf("止損單應有 _SL 後綴: %s", idSL)
	}
	// 驗證帶 _SL 的 ID 可直接解析（ParseOrderID 會自動剝離）
	parsedPrice, _, _, valid := ParseOrderID(idSL, decimals)
	if !valid || parsedPrice != price {
		t.Errorf("止損單 _SL 後綴應可解析: %s", idSL)
	}
}

func TestGenerateOrderIDOKXRoundTrip(t *testing.T) {
	price := 69538.4
	side := "BUY"
	decimals := 1
	id := GenerateOrderIDOKX(price, side, decimals)
	for _, c := range id {
		if (c < '0' || c > '9') && c != 'B' && c != 'S' {
			t.Fatalf("OKX clOrdId 含非法字符 %q: %s", c, id)
		}
	}
	if len(id) > 32 {
		t.Fatalf("OKX clOrdId 長度 %d > 32: %s", len(id), id)
	}
	p, s, _, ok := ParseOrderID(id, decimals)
	if !ok || p != price || s != side {
		t.Fatalf("OKX ID 往返失败: id=%s ok=%v p=%v s=%v want p=%v s=%v", id, ok, p, s, price, side)
	}
	idSL := GenerateOrderIDWithSourceOKX(price, "SELL", decimals, "stop_loss")
	if !strings.HasSuffix(idSL, "SL") || strings.Contains(idSL, "_") {
		t.Fatalf("OKX 止損 ID 應以 SL 結尾且無下劃線: %s", idSL)
	}
	if ParseOrderSource(idSL) != "stop_loss" {
		t.Fatalf("ParseOrderSource OKX 止損: %s", idSL)
	}
	p2, _, _, ok2 := ParseOrderID(idSL, decimals)
	if !ok2 || p2 != price {
		t.Fatalf("ParseOrderID OKX 止損: %v", idSL)
	}
}

func TestParseOrderSource(t *testing.T) {
	tests := []struct {
		clientOrderID string
		want          string
	}{
		{"65000_S_1702468800123", "normal"},
		{"65000_S_1702468800123_SL", "stop_loss"},
		{"x-zdfVM8vY65000_S_1702468800123_SL", "stop_loss"},
		{"65000_B_1702468800001", "normal"},
		{"6953840B1776004528564SL", "stop_loss"},
		{"6953840B1776004528564", "normal"},
	}
	for _, tt := range tests {
		got := ParseOrderSource(tt.clientOrderID)
		if got != tt.want {
			t.Errorf("ParseOrderSource(%q) = %q, want %q", tt.clientOrderID, got, tt.want)
		}
	}
}

func TestParseOrderIDWithSLSuffix(t *testing.T) {
	price := 1234.56
	side := "SELL"
	decimals := 2

	clientOID := GenerateOrderIDWithSource(price, side, decimals, "stop_loss")
	parsedPrice, parsedSide, _, valid := ParseOrderID(clientOID, decimals)

	if !valid {
		t.Fatal("帶 _SL 後綴的訂單ID應可解析")
	}
	if parsedPrice != price {
		t.Errorf("價格解析錯誤: 期望 %.2f, 得到 %.2f", price, parsedPrice)
	}
	if parsedSide != side {
		t.Errorf("方向解析錯誤: 期望 %s, 得到 %s", side, parsedSide)
	}
}

func TestBrokerPrefix(t *testing.T) {
	clientOID := "12345_B_1700000000001"

	// 测試币安前缀
	binanceID := AddBrokerPrefix("binance", clientOID)
	if !strings.HasPrefix(binanceID, "x-zdfVM8vY") {
		t.Errorf("币安前缀添加失败: %s", binanceID)
	}
	if len(binanceID) > 36 {
		t.Errorf("币安订單ID超长: %d", len(binanceID))
	}

	removedBinance := RemoveBrokerPrefix("binance", binanceID)
	if removedBinance != clientOID {
		t.Errorf("币安前缀移除失败: 期望 %s, 得到 %s", clientOID, removedBinance)
	}

	// 测試 Gate.io 前缀
	gateID := AddBrokerPrefix("gate", clientOID)
	if !strings.HasPrefix(gateID, "t-") {
		t.Errorf("Gate.io前缀添加失败: %s", gateID)
	}
	if len(gateID) > 30 {
		t.Errorf("Gate.io订單ID超长: %d", len(gateID))
	}

	removedGate := RemoveBrokerPrefix("gate", gateID)
	if removedGate != clientOID {
		t.Errorf("Gate.io前缀移除失败: 期望 %s, 得到 %s", clientOID, removedGate)
	}

	// 止損單帶 _SL 後綴，幣安 36 字符限制
	slOID := "65000_S_1702468800123_SL"
	binanceSL := AddBrokerPrefix("binance", slOID)
	if len(binanceSL) > 36 {
		t.Errorf("止損單加幣安前綴超长: %d > 36, %s", len(binanceSL), binanceSL)
	}
}

func TestParseCompactOrderID(t *testing.T) {
	// 使用大價格與高精度觸發緊湊格式
	price := 123456.789012
	decimals := 6
	clientOID := GenerateOrderID(price, "BUY", decimals)
	parsedPrice, parsedSide, ts, valid := ParseOrderID(clientOID, decimals)
	if !valid {
		t.Fatalf("緊湊格式應可解析: %s", clientOID)
	}
	if parsedPrice != price {
		t.Fatalf("緊湊格式價格不一致: want=%.6f got=%.6f id=%s", price, parsedPrice, clientOID)
	}
	if parsedSide != "BUY" {
		t.Fatalf("緊湊格式方向不一致: %s", parsedSide)
	}
	if ts <= 0 {
		t.Fatalf("緊湊格式時間戳無效: %d", ts)
	}
}
