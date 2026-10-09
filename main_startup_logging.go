package main

import "quantmesh/logger"

func logStartupBotFailure(botID string, _ error) {
	logger.Error("❌ [%s] 啟动失败；底层診斷未輸出至通用日志", botID)
}
