package web

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"
)

// TestFearGreedAPIResponseFormat 驗證 Alternative.me API 響應格式解析
// API 返回: data 為數組，value/timestamp 為字串
func TestFearGreedAPIResponseFormat(t *testing.T) {
	// 模擬 Alternative.me 實際 API 響應
	apiResponse := `{
		"name": "Fear and Greed Index",
		"data": [
			{
				"value": "12",
				"value_classification": "Extreme Fear",
				"timestamp": "1774051200",
				"time_until_update": "45893"
			}
		],
		"metadata": {"error": null}
	}`

	var result struct {
		Data []struct {
			Value          string `json:"value"`
			Classification string `json:"value_classification"`
			Timestamp      string `json:"timestamp"`
		} `json:"data"`
	}

	if err := json.Unmarshal([]byte(apiResponse), &result); err != nil {
		t.Fatalf("JSON 解析失敗: %v", err)
	}

	if len(result.Data) == 0 {
		t.Fatal("data 數組為空")
	}

	item := result.Data[0]
	value, err := strconv.Atoi(item.Value)
	if err != nil {
		t.Fatalf("value 解析失敗: %v", err)
	}
	timestampSec, err := strconv.ParseInt(item.Timestamp, 10, 64)
	if err != nil {
		t.Fatalf("timestamp 解析失敗: %v", err)
	}

	if value != 12 {
		t.Errorf("value 期望 12, 得到 %d", value)
	}
	if item.Classification != "Extreme Fear" {
		t.Errorf("classification 期望 'Extreme Fear', 得到 %q", item.Classification)
	}
	expectedTime := time.Unix(1774051200, 0)
	if time.Unix(timestampSec, 0) != expectedTime {
		t.Errorf("timestamp 解析錯誤: %v", time.Unix(timestampSec, 0))
	}
}
