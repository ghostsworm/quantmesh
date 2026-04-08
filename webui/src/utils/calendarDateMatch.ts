/**
 * 判斷 YYYY-MM-DD 是否落在指定公曆年月（與後端日曆鍵一致，避免 `new Date('YYYY-MM-DD')` 的時區偏移導致月份過濾錯位）。
 */
export function calendarMonthMatchesDateStr(
  dateStr: string,
  year: number,
  month: number
): boolean {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(dateStr.trim())
  if (!m) return false
  return Number(m[1]) === year && Number(m[2]) === month
}
