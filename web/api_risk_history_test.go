package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"quantmesh/storage"
)

type testStorageProvider struct {
	st storage.Storage
}

func (p *testStorageProvider) GetStorage() storage.Storage {
	return p.st
}

func TestGetRiskCheckHistoryWithBotIDFilter(t *testing.T) {
	gin.SetMode(gin.TestMode)

	dbPath := "./test_api_risk_history.db"
	defer os.Remove(dbPath)
	defer os.Remove(dbPath + "-shm")
	defer os.Remove(dbPath + "-wal")

	st, err := storage.NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("创建存储失败: %v", err)
	}
	defer st.Close()

	now := time.Now().UTC()
	if err := st.SaveRiskCheck(&storage.RiskCheckRecord{
		CheckTime:  now,
		BotID:      "binance:BTCUSDT:futures",
		Exchange:   "binance",
		MarketType: "futures",
		Symbol:     "BTCUSDT",
		IsHealthy:  false,
		Reason:     "panic",
	}); err != nil {
		t.Fatalf("保存 BTC 风控记录失败: %v", err)
	}
	if err := st.SaveRiskCheck(&storage.RiskCheckRecord{
		CheckTime:  now.Add(30 * time.Second),
		BotID:      "binance:ETHUSDT:futures",
		Exchange:   "binance",
		MarketType: "futures",
		Symbol:     "ETHUSDT",
		IsHealthy:  true,
		Reason:     "ok",
	}); err != nil {
		t.Fatalf("保存 ETH 风控记录失败: %v", err)
	}

	origStorageProvider := storageServiceProvider
	SetStorageServiceProvider(&testStorageProvider{st: st})
	t.Cleanup(func() { SetStorageServiceProvider(origStorageProvider) })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodGet,
		"/api/risk/history?bot_id=binance:BTCUSDT:futures&start_time="+now.Add(-time.Hour).Format(time.RFC3339)+"&end_time="+now.Add(time.Hour).Format(time.RFC3339),
		nil,
	)
	c.Request = req

	getRiskCheckHistory(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		History []struct {
			Symbols []struct {
				Symbol string `json:"symbol"`
			} `json:"symbols"`
		} `json:"history"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}

	foundBTC := false
	foundETH := false
	for _, h := range resp.History {
		for _, s := range h.Symbols {
			if s.Symbol == "BTCUSDT" {
				foundBTC = true
			}
			if s.Symbol == "ETHUSDT" {
				foundETH = true
			}
		}
	}
	if !foundBTC {
		t.Fatalf("按 bot_id 过滤后应返回 BTCUSDT")
	}
	if foundETH {
		t.Fatalf("按 bot_id 过滤后不应返回 ETHUSDT")
	}
}

