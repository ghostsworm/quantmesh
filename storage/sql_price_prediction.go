package storage

import (
	"database/sql"
	"time"
)

// ========== 価格歴史 / 預测驗证 存儲 ==========

// SavePriceHistory 保存價格历史
func (s *SQLStorage) SavePriceHistory(h *PriceHistory) error {
	_, err := s.db.Exec(`
		INSERT INTO price_history (asset_type, symbol, price, source, recorded_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, h.AssetType, h.Symbol, h.Price, h.Source, h.RecordedAt, h.CreatedAt)
	return err
}

// GetPriceAtTime 獲取指定時间附近的價格（在 tolerance 範圍内取最近的一条）
func (s *SQLStorage) GetPriceAtTime(assetType, symbol string, t time.Time, tolerance time.Duration) (*PriceHistory, error) {
	start := t.Add(-tolerance)
	end := t.Add(tolerance)
	var h PriceHistory
	err := s.db.QueryRow(`
		SELECT id, asset_type, symbol, price, source, recorded_at, created_at
		FROM price_history
		WHERE asset_type = ? AND symbol = ? AND recorded_at >= ? AND recorded_at <= ?
		ORDER BY ABS(strftime('%s', recorded_at) - strftime('%s', ?)) LIMIT 1
	`, assetType, symbol, start, end, t).Scan(&h.ID, &h.AssetType, &h.Symbol, &h.Price, &h.Source, &h.RecordedAt, &h.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &h, nil
}

// GetPriceHistory 查詢價格历史
func (s *SQLStorage) GetPriceHistory(assetType, symbol string, startTime, endTime time.Time, limit int) ([]*PriceHistory, error) {
	if limit <= 0 {
		limit = 1000
	}
	rows, err := s.db.Query(`
		SELECT id, asset_type, symbol, price, source, recorded_at, created_at
		FROM price_history
		WHERE asset_type = ? AND symbol = ? AND recorded_at >= ? AND recorded_at <= ?
		ORDER BY recorded_at ASC LIMIT ?
	`, assetType, symbol, startTime, endTime, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*PriceHistory
	for rows.Next() {
		var h PriceHistory
		var source sql.NullString
		if err := rows.Scan(&h.ID, &h.AssetType, &h.Symbol, &h.Price, &source, &h.RecordedAt, &h.CreatedAt); err != nil {
			return nil, err
		}
		if source.Valid {
			h.Source = source.String
		}
		list = append(list, &h)
	}
	return list, rows.Err()
}

// SavePredictionVerification 保存預测驗证記錄
func (s *SQLStorage) SavePredictionVerification(v *PredictionVerification) error {
	isCorrect := 0
	if v.IsCorrect {
		isCorrect = 1
	}
	_, err := s.db.Exec(`
		INSERT INTO prediction_verification (
			analysis_id, asset_type, symbol, prediction_time, timeframe,
			predicted_direction, predicted_change_pct, predicted_probability,
			actual_price_at_prediction, actual_price_at_verify, actual_direction,
			actual_change_pct, is_correct, verified_at, status
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, v.AnalysisID, v.AssetType, v.Symbol, v.PredictionTime, v.Timeframe,
		v.PredictedDirection, v.PredictedChangePct, v.PredictedProbability,
		v.ActualPriceAtPred, v.ActualPriceAtVerify, v.ActualDirection,
		v.ActualChangePct, isCorrect, v.VerifiedAt, v.Status)
	return err
}

// QueryPredictionVerifications 查詢預测驗证記錄
func (s *SQLStorage) QueryPredictionVerifications(assetType, symbol string, startTime, endTime time.Time, limit, offset int) ([]*PredictionVerification, int64, error) {
	args := []interface{}{startTime, endTime}
	where := "WHERE prediction_time >= ? AND prediction_time <= ?"
	if assetType != "" {
		where += " AND asset_type = ?"
		args = append(args, assetType)
	}
	if symbol != "" {
		where += " AND symbol = ?"
		args = append(args, symbol)
	}

	var total int64
	countArgs := args
	if err := s.db.QueryRow("SELECT COUNT(*) FROM prediction_verification "+where, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, limit, offset)
	rows, err := s.db.Query(`
		SELECT id, analysis_id, asset_type, symbol, prediction_time, timeframe,
			predicted_direction, predicted_change_pct, predicted_probability,
			actual_price_at_prediction, actual_price_at_verify, actual_direction,
			actual_change_pct, is_correct, verified_at, status
		FROM prediction_verification `+where+` ORDER BY prediction_time DESC LIMIT ? OFFSET ?
	`, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var list []*PredictionVerification
	for rows.Next() {
		var v PredictionVerification
		var isCorrect int
		var verifiedAt sql.NullTime
		if err := rows.Scan(&v.ID, &v.AnalysisID, &v.AssetType, &v.Symbol, &v.PredictionTime, &v.Timeframe,
			&v.PredictedDirection, &v.PredictedChangePct, &v.PredictedProbability,
			&v.ActualPriceAtPred, &v.ActualPriceAtVerify, &v.ActualDirection,
			&v.ActualChangePct, &isCorrect, &verifiedAt, &v.Status); err != nil {
			return nil, 0, err
		}
		v.IsCorrect = isCorrect == 1
		if verifiedAt.Valid {
			v.VerifiedAt = verifiedAt.Time
		}
		list = append(list, &v)
	}
	return list, total, rows.Err()
}

// GetPredictionVerificationsByStatus 按状態查詢
func (s *SQLStorage) GetPredictionVerificationsByStatus(status string, limit int) ([]*PredictionVerification, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(`
		SELECT id, analysis_id, asset_type, symbol, prediction_time, timeframe,
			predicted_direction, predicted_change_pct, predicted_probability,
			actual_price_at_prediction, actual_price_at_verify, actual_direction,
			actual_change_pct, is_correct, verified_at, status
		FROM prediction_verification WHERE status = ? ORDER BY prediction_time ASC LIMIT ?
	`, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*PredictionVerification
	for rows.Next() {
		var v PredictionVerification
		var isCorrect int
		var verifiedAt sql.NullTime
		if err := rows.Scan(&v.ID, &v.AnalysisID, &v.AssetType, &v.Symbol, &v.PredictionTime, &v.Timeframe,
			&v.PredictedDirection, &v.PredictedChangePct, &v.PredictedProbability,
			&v.ActualPriceAtPred, &v.ActualPriceAtVerify, &v.ActualDirection,
			&v.ActualChangePct, &isCorrect, &verifiedAt, &v.Status); err != nil {
			return nil, err
		}
		v.IsCorrect = isCorrect == 1
		if verifiedAt.Valid {
			v.VerifiedAt = verifiedAt.Time
		}
		list = append(list, &v)
	}
	return list, rows.Err()
}

// UpdatePredictionVerification 更新預测驗证記錄
func (s *SQLStorage) UpdatePredictionVerification(v *PredictionVerification) error {
	isCorrect := 0
	if v.IsCorrect {
		isCorrect = 1
	}
	_, err := s.db.Exec(`
		UPDATE prediction_verification SET
			actual_price_at_verify = ?, actual_direction = ?, actual_change_pct = ?,
			is_correct = ?, verified_at = ?, status = ?
		WHERE id = ?
	`, v.ActualPriceAtVerify, v.ActualDirection, v.ActualChangePct, isCorrect, v.VerifiedAt, v.Status, v.ID)
	return err
}

// GetPredictionAccuracyStats 獲取預测准确率统计
func (s *SQLStorage) GetPredictionAccuracyStats(assetType string, since time.Time) (total int, correct int, err error) {
	query := "SELECT COUNT(*), SUM(CASE WHEN is_correct = 1 THEN 1 ELSE 0 END) FROM prediction_verification WHERE status = 'verified' AND verified_at >= ?"
	args := []interface{}{since}
	if assetType != "" {
		query += " AND asset_type = ?"
		args = append(args, assetType)
	}
	var sumCorrect sql.NullInt64
	err = s.db.QueryRow(query, args...).Scan(&total, &sumCorrect)
	if err != nil {
		return 0, 0, err
	}
	if sumCorrect.Valid {
		correct = int(sumCorrect.Int64)
	}
	return total, correct, nil
}

// GetPredictionAccuracyStatsByTimeframe 獲取按時间窗口分組的預测准确率统计
func (s *SQLStorage) GetPredictionAccuracyStatsByTimeframe(assetType string, since time.Time) (map[string]struct{ Total, Correct int }, error) {
	query := "SELECT timeframe, COUNT(*), SUM(CASE WHEN is_correct = 1 THEN 1 ELSE 0 END) FROM prediction_verification WHERE status = 'verified' AND verified_at >= ?"
	args := []interface{}{since}
	if assetType != "" {
		query += " AND asset_type = ?"
		args = append(args, assetType)
	}
	query += " GROUP BY timeframe"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stats := make(map[string]struct{ Total, Correct int })
	for rows.Next() {
		var tf string
		var total int
		var correct sql.NullInt64
		if err := rows.Scan(&tf, &total, &correct); err != nil {
			return nil, err
		}
		c := 0
		if correct.Valid {
			c = int(correct.Int64)
		}
		stats[tf] = struct{ Total, Correct int }{Total: total, Correct: c}
	}
	return stats, rows.Err()
}

// GetPredictionDirectionStatsByTimeframe 獲取按時间窗口和方向分組的預测统计
func (s *SQLStorage) GetPredictionDirectionStatsByTimeframe(assetType string, since time.Time) (map[string]map[string]struct{ Total, Correct int }, error) {
	query := "SELECT timeframe, predicted_direction, COUNT(*), SUM(CASE WHEN is_correct = 1 THEN 1 ELSE 0 END) FROM prediction_verification WHERE status = 'verified' AND verified_at >= ?"
	args := []interface{}{since}
	if assetType != "" {
		query += " AND asset_type = ?"
		args = append(args, assetType)
	}
	query += " GROUP BY timeframe, predicted_direction"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// timeframe -> direction -> stats
	stats := make(map[string]map[string]struct{ Total, Correct int })
	for rows.Next() {
		var tf string
		var dir string
		var total int
		var correct sql.NullInt64
		if err := rows.Scan(&tf, &dir, &total, &correct); err != nil {
			return nil, err
		}
		if _, ok := stats[tf]; !ok {
			stats[tf] = make(map[string]struct{ Total, Correct int })
		}
		c := 0
		if correct.Valid {
			c = int(correct.Int64)
		}
		stats[tf][dir] = struct{ Total, Correct int }{Total: total, Correct: c}
	}
	return stats, rows.Err()
}
