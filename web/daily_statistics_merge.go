package web

import "quantmesh/storage"

// collectDailyStatDateKeysInRange 合併 statistics / trades / 資金費 / 交易所已實現 / 日快照 等來源的日期鍵，
// 並限制在 [startDateStr, endDateStr]（YYYY-MM-DD 字串可比較）。
// 避免某日僅有資金費或交易所已實現但無網格成交時，日曆出現「中間缺一塊」。
func collectDailyStatDateKeysInRange(
	startDateStr, endDateStr string,
	statsMap map[string]*storage.Statistics,
	tradesStatsMap map[string]*storage.DailyStatisticsWithTradeCount,
	fundingMap map[string]float64,
	exchangePnLMap map[string]float64,
	snapshotMap map[string]*storage.DailySnapshot,
) map[string]bool {
	allDates := make(map[string]bool)
	for k := range statsMap {
		allDates[k] = true
	}
	for k := range tradesStatsMap {
		allDates[k] = true
	}
	for k := range fundingMap {
		allDates[k] = true
	}
	for k := range exchangePnLMap {
		allDates[k] = true
	}
	for k := range snapshotMap {
		allDates[k] = true
	}
	out := make(map[string]bool)
	for k := range allDates {
		if k >= startDateStr && k <= endDateStr {
			out[k] = true
		}
	}
	return out
}
