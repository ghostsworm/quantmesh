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

	// 驗证包含價格整數部分
	if !strings.HasPrefix(id1, "6500050_B_") {
		t.Errorf("订單ID格式錯误: %s", id1)
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
}
