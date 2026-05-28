package storage

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"quantmesh/utils"
)

// ========== Bot 風控事件 / 风控检查历史 存儲 ==========

// SaveBotRiskControlEvent 保存 Bot 開倉風控暫停/恢復事件
func (s *SQLStorage) SaveBotRiskControlEvent(record *BotRiskControlEventRecord) error {
	if record == nil || strings.TrimSpace(record.BotID) == "" {
		return nil
	}
	et := strings.TrimSpace(record.EventType)
	if et != "paused" && et != "resumed" {
		return fmt.Errorf("invalid event_type: %s", record.EventType)
	}
	t := utils.ToUTC(record.CreatedAt)
	if t.IsZero() {
		t = time.Now().UTC()
	}
	_, err := s.db.Exec(`
		INSERT INTO bot_risk_control_events (bot_id, event_type, reason, source, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, record.BotID, et, record.Reason, record.Source, t)
	return err
}

// QueryBotRiskControlEvents 按 Bot 查詢事件（新到舊）
func (s *SQLStorage) QueryBotRiskControlEvents(botID string, limit, offset int) ([]*BotRiskControlEventRecord, error) {
	if strings.TrimSpace(botID) == "" {
		return nil, fmt.Errorf("bot_id required")
	}
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.Query(`
		SELECT id, bot_id, event_type, reason, source, created_at
		FROM bot_risk_control_events
		WHERE bot_id = ?
		ORDER BY created_at DESC, id DESC
		LIMIT ? OFFSET ?
	`, botID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*BotRiskControlEventRecord
	for rows.Next() {
		var r BotRiskControlEventRecord
		if err := rows.Scan(&r.ID, &r.BotID, &r.EventType, &r.Reason, &r.Source, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &r)
	}
	return out, rows.Err()
}

// CountBotRiskControlEvents 統計某 Bot 事件總數
func (s *SQLStorage) CountBotRiskControlEvents(botID string) (int64, error) {
	if strings.TrimSpace(botID) == "" {
		return 0, fmt.Errorf("bot_id required")
	}
	var n int64
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM bot_risk_control_events WHERE bot_id = ?
	`, botID).Scan(&n)
	return n, err
}

// SaveRiskCheck 保存风控检查記錄
func (s *SQLStorage) SaveRiskCheck(record *RiskCheckRecord) error {
	// 轉换為UTC時间存儲
	checkTime := utils.ToUTC(record.CheckTime)
	_, err := s.db.Exec(`
		INSERT INTO risk_check_history
		(check_time, bot_id, exchange, market_type, symbol, is_healthy, price_deviation, volume_ratio, reason)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, checkTime, record.BotID, record.Exchange, record.MarketType, record.Symbol, record.IsHealthy, record.PriceDeviation, record.VolumeRatio, record.Reason)
	return err
}

// QueryRiskCheckHistory 查詢风控检查历史
func (s *SQLStorage) QueryRiskCheckHistory(startTime, endTime time.Time, limit int, botID string) ([]*RiskCheckHistory, error) {
	// 如果 limit <= 0，默认限制為 200 条，防止前端渲染數據過大導致卡顿
	if limit <= 0 {
		limit = 200
	}
	// 上限限制，避免一次性拉取過多數據占用記憶體/CPU
	if limit > 500 {
		limit = 500
	}

	// 根據時间範圍决定聚合粒度
	timeRange := endTime.Sub(startTime)
	var truncateDuration time.Duration
	if timeRange > 30*24*time.Hour {
		// 超過30天，按小時聚合
		truncateDuration = time.Hour
	} else if timeRange > 7*24*time.Hour {
		// 超過7天，按30分钟聚合
		truncateDuration = 30 * time.Minute
	} else if timeRange > 24*time.Hour {
		// 超過1天，按10分钟聚合
		truncateDuration = 10 * time.Minute
	} else {
		// 1天内，按分钟聚合
		truncateDuration = time.Minute
	}

	// 查詢數據，按時间倒序，限制數量
	query := `
		SELECT check_time, symbol, is_healthy, price_deviation, volume_ratio, reason
		FROM risk_check_history
		WHERE check_time >= ? AND check_time <= ?
	`
	args := []interface{}{startTime, endTime}
	if botID != "" {
		query += ` AND bot_id = ?`
		args = append(args, botID)
	}
	query += `
		ORDER BY check_time DESC
		LIMIT ?
	`
	args = append(args, limit*4) // 多查詢一些，因為后面會聚合，但限制在 4 倍以内防止過大
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("查詢风控检查历史失败: %w", err)
	}
	defer rows.Close()

	// 按检查時间分组
	historyMap := make(map[time.Time]*RiskCheckHistory)

	for rows.Next() {
		var checkTime time.Time
		var symbol string
		var isHealthy int
		var priceDeviation sql.NullFloat64
		var volumeRatio sql.NullFloat64
		var reason sql.NullString

		err := rows.Scan(&checkTime, &symbol, &isHealthy, &priceDeviation, &volumeRatio, &reason)
		if err != nil {
			continue
		}

		// 根據時间範圍聚合時间戳
		checkTimeRounded := checkTime.Truncate(truncateDuration)

		history, exists := historyMap[checkTimeRounded]
		if !exists {
			history = &RiskCheckHistory{
				CheckTime: checkTimeRounded,
				Symbols:   []*RiskCheckSymbol{},
			}
			historyMap[checkTimeRounded] = history
		}

		symbolData := &RiskCheckSymbol{
			Symbol:    symbol,
			IsHealthy: isHealthy == 1,
		}
		if priceDeviation.Valid {
			symbolData.PriceDeviation = priceDeviation.Float64
		}
		if volumeRatio.Valid {
			symbolData.VolumeRatio = volumeRatio.Float64
		}
		if reason.Valid {
			symbolData.Reason = reason.String
		}

		history.Symbols = append(history.Symbols, symbolData)
		if symbolData.IsHealthy {
			history.HealthyCount++
		}
		history.TotalCount++
	}

	// 轉换為切片並排序
	result := make([]*RiskCheckHistory, 0, len(historyMap))
	for _, history := range historyMap {
		result = append(result, history)
	}

	// 按時间排序（升序），使用 sort.Slice 替代 O(n^2) 嵌套循环
	sort.Slice(result, func(i, j int) bool {
		return result[i].CheckTime.Before(result[j].CheckTime)
	})

	// 限制返回數量（取最新的 limit 条）
	if len(result) > limit {
		result = result[len(result)-limit:]
	}

	return result, nil
}

// CleanupRiskCheckHistory 清理指定時间之前的风控检查历史
func (s *SQLStorage) CleanupRiskCheckHistory(beforeTime time.Time) error {
	_, err := s.db.Exec(`
		DELETE FROM risk_check_history
		WHERE check_time < ?
	`, beforeTime)
	return err
}
