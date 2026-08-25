package storage

import (
	"database/sql"
	"fmt"
	"time"

	"quantmesh/logger"
)

// ========== AI prompt 模板 / Basis 价差 / News 分析 / Inspection 报告 存儲 ==========

// GetAIPromptTemplate 獲取AI提示词模板
func (s *SQLStorage) GetAIPromptTemplate(module string) (*AIPromptTemplate, error) {
	var template AIPromptTemplate
	err := s.db.QueryRow(
		"SELECT id, module, template, system_prompt, updated_at FROM ai_prompts WHERE module = ?",
		module,
	).Scan(&template.ID, &template.Module, &template.Template, &template.SystemPrompt, &template.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, nil // 不存在，返回nil
	}
	if err != nil {
		return nil, err
	}
	return &template, nil
}

// SetAIPromptTemplate 設置AI提示词模板
func (s *SQLStorage) SetAIPromptTemplate(template *AIPromptTemplate) error {
	_, err := s.db.Exec(
		`INSERT INTO ai_prompts (module, template, system_prompt, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(module) DO UPDATE SET
		 template = excluded.template,
		 system_prompt = excluded.system_prompt,
		 updated_at = excluded.updated_at`,
		template.Module, template.Template, template.SystemPrompt, time.Now(),
	)
	return err
}

// GetAllAIPromptTemplates 獲取所有AI提示词模板
func (s *SQLStorage) GetAllAIPromptTemplates() ([]*AIPromptTemplate, error) {
	rows, err := s.db.Query(
		"SELECT id, module, template, system_prompt, updated_at FROM ai_prompts ORDER BY module",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var templates []*AIPromptTemplate
	for rows.Next() {
		var template AIPromptTemplate
		err := rows.Scan(&template.ID, &template.Module, &template.Template, &template.SystemPrompt, &template.UpdatedAt)
		if err != nil {
			return nil, err
		}
		templates = append(templates, &template)
	}

	return templates, rows.Err()
}

// SaveBasisData 保存價差數據
func (s *SQLStorage) SaveBasisData(data *BasisData) error {
	_, err := s.db.Exec(`
		INSERT INTO basis_data (symbol, exchange, spot_price, futures_price, basis, basis_percent, funding_rate, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, data.Symbol, data.Exchange, data.SpotPrice, data.FuturesPrice, data.Basis, data.BasisPercent, data.FundingRate, data.Timestamp)
	return err
}

// GetLatestBasis 獲取最新價差數據
func (s *SQLStorage) GetLatestBasis(symbol, exchange string) (*BasisData, error) {
	var data BasisData
	err := s.db.QueryRow(`
		SELECT symbol, exchange, spot_price, futures_price, basis, basis_percent, funding_rate, timestamp
		FROM basis_data
		WHERE symbol = ? AND exchange = ?
		ORDER BY timestamp DESC
		LIMIT 1
	`, symbol, exchange).Scan(
		&data.Symbol, &data.Exchange, &data.SpotPrice, &data.FuturesPrice,
		&data.Basis, &data.BasisPercent, &data.FundingRate, &data.Timestamp,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &data, nil
}

// GetBasisHistory 獲取價差历史數據
func (s *SQLStorage) GetBasisHistory(symbol, exchange string, limit int) ([]*BasisData, error) {
	// 限制最大返回數量，防止記憶體占用過大
	maxLimit := 10000 // 最多返回1万条價差記錄
	if limit <= 0 {
		limit = 100 // 預設 100条
	}
	if limit > maxLimit {
		limit = maxLimit
		logger.Warn("⚠️ 價差历史查詢 limit 超過限制 (%d)，已限制為 %d", limit, maxLimit)
	}

	rows, err := s.db.Query(`
		SELECT symbol, exchange, spot_price, futures_price, basis, basis_percent, funding_rate, timestamp
		FROM basis_data
		WHERE symbol = ? AND exchange = ?
		ORDER BY timestamp DESC
		LIMIT ?
	`, symbol, exchange, limit)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*BasisData
	for rows.Next() {
		var data BasisData
		err := rows.Scan(
			&data.Symbol, &data.Exchange, &data.SpotPrice, &data.FuturesPrice,
			&data.Basis, &data.BasisPercent, &data.FundingRate, &data.Timestamp,
		)
		if err != nil {
			return nil, err
		}
		result = append(result, &data)
	}

	return result, rows.Err()
}

// GetBasisStatistics 獲取價差统计數據
func (s *SQLStorage) GetBasisStatistics(symbol, exchange string, hours int) (*BasisStats, error) {
	if hours <= 0 {
		hours = 24
	}

	cutoffTime := time.Now().UTC().Add(-time.Duration(hours) * time.Hour)

	rows, err := s.db.Query(`
		SELECT basis_percent
		FROM basis_data
		WHERE symbol = ? AND exchange = ? AND timestamp >= ?
		ORDER BY timestamp DESC
	`, symbol, exchange, cutoffTime)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var values []float64
	for rows.Next() {
		var value float64
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍歷统计數據失败: %w", err)
	}

	if len(values) == 0 {
		return nil, fmt.Errorf("没有找到數據")
	}

	// 计算统计數據
	var sum, max, min float64
	max = values[0]
	min = values[0]

	for _, v := range values {
		sum += v
		if v > max {
			max = v
		}
		if v < min {
			min = v
		}
	}

	avg := sum / float64(len(values))

	// 计算標准差
	var variance float64
	for _, v := range values {
		diff := v - avg
		variance += diff * diff
	}
	variance /= float64(len(values))
	stdDev := 0.0
	if variance > 0 {
		// 简化的平方根计算
		stdDev = variance
		for i := 0; i < 10; i++ {
			stdDev = (stdDev + variance/stdDev) / 2
		}
	}

	return &BasisStats{
		Symbol:     symbol,
		Exchange:   exchange,
		AvgBasis:   avg,
		MaxBasis:   max,
		MinBasis:   min,
		StdDev:     stdDev,
		DataPoints: len(values),
		Hours:      hours,
	}, nil
}

// SaveNewsAnalysisHistory 保存新聞分析历史（成功后填充 history.ID）
func (s *SQLStorage) SaveNewsAnalysisHistory(history *NewsAnalysisHistory) error {
	result, err := s.db.Exec(`
		INSERT INTO news_analysis_history (analysis_time, symbol, current_price, assessment, recent_news_summary, gemini_prompt, gemini_response, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, history.AnalysisTime, history.Symbol, history.CurrentPrice, history.Assessment, history.RecentNewsSummary, history.GeminiPrompt, history.GeminiResponse, history.CreatedAt)
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err == nil {
		history.ID = id
	}
	return nil
}

// SaveInspectionReport 保存智子巡檢報告
func (s *SQLStorage) SaveInspectionReport(report *InspectionReport) error {
	if report == nil {
		return nil
	}
	if report.GeneratedAt.IsZero() {
		report.GeneratedAt = time.Now()
	}
	result, err := s.db.Exec(`
		INSERT INTO inspection_reports (report_type, title, body, snapshot_json, analysis_json, event_type, event_data_json, generated_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, report.ReportType, report.Title, report.Body, report.SnapshotJSON, report.AnalysisJSON, report.EventType, report.EventDataJSON, report.GeneratedAt, report.CreatedAt)
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err == nil {
		report.ID = id
	}
	return nil
}

// QueryNewsAnalysisHistory 查詢新聞分析历史（分页，回傳 total 總數）
func (s *SQLStorage) QueryNewsAnalysisHistory(symbol string, startTime, endTime time.Time, limit, offset int) ([]*NewsAnalysisHistory, int64, error) {
	args := []interface{}{startTime, endTime}
	whereSymbol := ""
	if symbol != "" {
		whereSymbol = " AND symbol = ?"
		args = append(args, symbol)
	}

	// 查總數
	var total int64
	countSQL := "SELECT COUNT(*) FROM news_analysis_history WHERE analysis_time >= ? AND analysis_time <= ?" + whereSymbol
	if err := s.db.QueryRow(countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// 分页查詢
	args = append(args, limit, offset)
	query := `
		SELECT id, analysis_time, symbol, current_price, assessment, recent_news_summary, gemini_prompt, gemini_response, created_at
		FROM news_analysis_history
		WHERE analysis_time >= ? AND analysis_time <= ?` + whereSymbol + `
		ORDER BY analysis_time DESC
		LIMIT ? OFFSET ?`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var list []*NewsAnalysisHistory
	for rows.Next() {
		var h NewsAnalysisHistory
		var prompt, response sql.NullString
		if err := rows.Scan(&h.ID, &h.AnalysisTime, &h.Symbol, &h.CurrentPrice, &h.Assessment, &h.RecentNewsSummary, &prompt, &response, &h.CreatedAt); err != nil {
			return nil, 0, err
		}
		if prompt.Valid {
			h.GeminiPrompt = prompt.String
		}
		if response.Valid {
			h.GeminiResponse = response.String
		}
		list = append(list, &h)
	}
	return list, total, rows.Err()
}

// GetLatestNewsAnalysisHistory 獲取指定币种最新分析記錄（symbol 為空時返回任意币种最新）
func (s *SQLStorage) GetLatestNewsAnalysisHistory(symbol string) (*NewsAnalysisHistory, error) {
	var query string
	var args []interface{}
	if symbol != "" {
		query = `
			SELECT id, analysis_time, symbol, current_price, assessment, recent_news_summary, gemini_prompt, gemini_response, created_at
			FROM news_analysis_history
			WHERE symbol = ?
			ORDER BY analysis_time DESC
			LIMIT 1`
		args = []interface{}{symbol}
	} else {
		query = `
			SELECT id, analysis_time, symbol, current_price, assessment, recent_news_summary, gemini_prompt, gemini_response, created_at
			FROM news_analysis_history
			ORDER BY analysis_time DESC
			LIMIT 1`
	}
	var h NewsAnalysisHistory
	var prompt, response sql.NullString
	var err error
	if len(args) > 0 {
		err = s.db.QueryRow(query, args...).Scan(&h.ID, &h.AnalysisTime, &h.Symbol, &h.CurrentPrice, &h.Assessment, &h.RecentNewsSummary, &prompt, &response, &h.CreatedAt)
	} else {
		err = s.db.QueryRow(query).Scan(&h.ID, &h.AnalysisTime, &h.Symbol, &h.CurrentPrice, &h.Assessment, &h.RecentNewsSummary, &prompt, &response, &h.CreatedAt)
	}
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if prompt.Valid {
		h.GeminiPrompt = prompt.String
	}
	if response.Valid {
		h.GeminiResponse = response.String
	}
	return &h, nil
}

// GetNewsAnalysisHistoryByID 按 ID 獲取分析記錄
func (s *SQLStorage) GetNewsAnalysisHistoryByID(id int64) (*NewsAnalysisHistory, error) {
	var h NewsAnalysisHistory
	var prompt, response sql.NullString
	err := s.db.QueryRow(`
		SELECT id, analysis_time, symbol, current_price, assessment, recent_news_summary, gemini_prompt, gemini_response, created_at
		FROM news_analysis_history WHERE id = ?
	`, id).Scan(&h.ID, &h.AnalysisTime, &h.Symbol, &h.CurrentPrice, &h.Assessment, &h.RecentNewsSummary, &prompt, &response, &h.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if prompt.Valid {
		h.GeminiPrompt = prompt.String
	}
	if response.Valid {
		h.GeminiResponse = response.String
	}
	return &h, nil
}

// CleanupNewsAnalysisHistory 清理指定時间之前的記錄
func (s *SQLStorage) CleanupNewsAnalysisHistory(beforeTime time.Time) error {
	_, err := s.db.Exec(`DELETE FROM news_analysis_history WHERE analysis_time < ?`, beforeTime)
	return err
}
