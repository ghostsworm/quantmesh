import React from 'react'
import { useTranslation } from 'react-i18next'

interface DailyStatistics {
  date: string
  total_trades: number
  total_volume: number
  total_pnl: number
  funding_fee?: number // 資金費用（正=收入，負=支出）
  win_rate: number
  winning_trades?: number
  losing_trades?: number
  unrealized_pnl?: number  // 當日收盤未實現盈虧
  book_value_pnl?: number  // 賬面盈虧 = 已平倉 + 未實現
  intraday_max_drawdown?: number
  intraday_max_drawdown_pct?: number
  exchange_pnl?: number // 當日交易所已實現盈虧
}

interface StatisticsCalendarProps {
  year: number
  month: number
  dailyStats: DailyStatistics[]
  onDayClick?: (date: string) => void
}

const StatisticsCalendar: React.FC<StatisticsCalendarProps> = ({ year, month, dailyStats, onDayClick }) => {
  const { t } = useTranslation()
  const statsMap = new Map<string, DailyStatistics>()
  dailyStats.forEach(stat => {
    statsMap.set(stat.date, stat)
  })

  // 獲取月份的第一天和最后一天
  const firstDay = new Date(year, month - 1, 1)
  const lastDay = new Date(year, month, 0)
  const daysInMonth = lastDay.getDate()
  const startDayOfWeek = firstDay.getDay() // 0 = 周日, 6 = 周六

  const weekDays = [
    t('statistics.weekday0'), t('statistics.weekday1'), t('statistics.weekday2'),
    t('statistics.weekday3'), t('statistics.weekday4'), t('statistics.weekday5'),
    t('statistics.weekday6')
  ]

  // 生成日期格子
  const calendarDays: (Date | null)[] = []
  
  // 填充月初的空格
  for (let i = 0; i < startDayOfWeek; i++) {
    calendarDays.push(null)
  }
  
  // 填充日期
  for (let day = 1; day <= daysInMonth; day++) {
    calendarDays.push(new Date(year, month - 1, day))
  }

  // 格式化日期為 YYYY-MM-DD
  const formatDate = (date: Date): string => {
    const y = date.getFullYear()
    const m = String(date.getMonth() + 1).padStart(2, '0')
    const d = String(date.getDate()).padStart(2, '0')
    return `${y}-${m}-${d}`
  }

  // 獲取某天的统计數據
  const getDayStats = (date: Date | null): DailyStatistics | null => {
    if (!date) return null
    const dateStr = formatDate(date)
    return statsMap.get(dateStr) || null
  }

  // 今日日期字串（使用本地時區，與日曆格子一致，避免 UTC 導致焦點錯位）
  const todayStr = (() => {
    const now = new Date()
    return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`
  })()

  return (
    <div style={{ marginTop: '24px' }}>
      <div style={{ 
        display: 'grid', 
        gridTemplateColumns: 'repeat(7, 1fr)', 
        gap: '4px',
        border: '1px solid #e8e8e8',
        borderRadius: '4px',
        padding: '12px',
        backgroundColor: '#fafafa',
        maxWidth: '900px'
      }}>
        {/* 星期標题 */}
        {weekDays.map((day, index) => (
          <div
            key={index}
            style={{
              padding: '8px',
              textAlign: 'center',
              fontWeight: 'bold',
              color: '#595959',
              fontSize: '14px'
            }}
          >
            {day}
          </div>
        ))}

        {/* 日期格子 */}
        {calendarDays.map((date, index) => {
          const stats = getDayStats(date)
          const isToday = date ? formatDate(date) === todayStr : false
          const dateStr = date ? formatDate(date) : ''
          const hasData = !!stats
          const isClickable = hasData && onDayClick

          return (
            <div
              key={index}
              role={isClickable ? 'button' : undefined}
              tabIndex={isClickable ? 0 : undefined}
              onClick={isClickable ? () => onDayClick(dateStr) : undefined}
              onKeyDown={isClickable ? (e) => { if (e.key === 'Enter' || e.key === ' ') onDayClick(dateStr) } : undefined}
              style={{
                minHeight: '85px',
                padding: '6px',
                border: '1px solid #e8e8e8',
                borderRadius: '4px',
                backgroundColor: date ? '#fff' : 'transparent',
                display: 'flex',
                flexDirection: 'column',
                cursor: isClickable ? 'pointer' : (date ? 'default' : 'default'),
                position: 'relative',
                ...(isToday ? {
                  borderColor: '#1890ff',
                  borderWidth: '2px'
                } : {}),
                ...(isClickable ? {
                  transition: 'background-color 0.2s, box-shadow 0.2s'
                } : {})
              }}
              onMouseEnter={isClickable ? (e) => {
                e.currentTarget.style.backgroundColor = '#f0f7ff'
                e.currentTarget.style.boxShadow = '0 1px 4px rgba(0,0,0,0.08)'
              } : undefined}
              onMouseLeave={isClickable ? (e) => {
                e.currentTarget.style.backgroundColor = '#fff'
                e.currentTarget.style.boxShadow = 'none'
              } : undefined}
            >
              {date ? (
                <>
                  {/* 日期數字 */}
                  <div style={{
                    fontSize: '14px',
                    fontWeight: 'bold',
                    marginBottom: '4px',
                    color: isToday ? '#1890ff' : '#262626'
                  }}>
                    {date.getDate()}
                  </div>

                  {/* 统计數據 */}
                  {stats ? (
                    <div style={{ flex: 1, fontSize: '11px', lineHeight: '1.4' }}>
                      <div style={{
                        color: stats.total_pnl >= 0 ? '#52c41a' : '#ff4d4f',
                        fontWeight: 'bold',
                        marginBottom: '2px'
                      }} title={t('statistics.pnl')}>
                        {stats.total_pnl >= 0 ? '+' : ''}{stats.total_pnl.toFixed(2)}
                      </div>
                      {stats.exchange_pnl !== undefined && stats.exchange_pnl !== 0 && (
                        <div style={{
                          color: stats.exchange_pnl >= 0 ? '#1890ff' : '#ff4d4f',
                          fontSize: '10px',
                          marginBottom: '2px'
                        }} title={t('statistics.exchangePnlTooltip')}>
                          {t('statistics.exchangePnlShort')}: {(stats.exchange_pnl >= 0 ? '+' : '') + stats.exchange_pnl.toFixed(2)}
                        </div>
                      )}
                      {stats.unrealized_pnl !== undefined && stats.unrealized_pnl !== 0 && (
                        <div style={{
                          color: stats.unrealized_pnl >= 0 ? '#95de64' : '#ff7875',
                          fontSize: '10px',
                          marginBottom: '2px',
                          fontStyle: 'italic'
                        }} title={t('statistics.unrealizedPnL')}>
                          {t('statistics.unrealizedShort')}: {(stats.unrealized_pnl >= 0 ? '+' : '') + stats.unrealized_pnl.toFixed(2)}
                        </div>
                      )}
                      {stats.unrealized_pnl !== undefined && Math.abs(stats.unrealized_pnl) > 0.001 && stats.book_value_pnl !== undefined && (
                        <div style={{
                          color: stats.book_value_pnl >= 0 ? '#389e0d' : '#cf1322',
                          fontSize: '10px',
                          fontWeight: '600',
                          marginBottom: '2px'
                        }} title={t('statistics.bookValuePnL')}>
                          {t('statistics.bookValueShort')}: {(stats.book_value_pnl >= 0 ? '+' : '') + stats.book_value_pnl.toFixed(2)}
                        </div>
                      )}
                      {stats.funding_fee !== undefined && stats.funding_fee !== 0 && (
                        <div style={{
                          color: stats.funding_fee >= 0 ? '#52c41a' : '#fa8c16',
                          fontSize: '10px',
                          marginBottom: '2px'
                        }} title={t('statistics.fundingFee')}>
                          {t('statistics.fundingFeeShort')}: {stats.funding_fee >= 0 ? '+' : ''}{stats.funding_fee.toFixed(2)}
                        </div>
                      )}
                      <div style={{ color: '#8c8c8c', marginBottom: '2px' }}>
                        {(stats.win_rate * 100).toFixed(1)}%
                      </div>
                      {stats.intraday_max_drawdown_pct !== undefined && stats.intraday_max_drawdown_pct > 0 && (
                        <div style={{ color: '#8c8c8c', fontSize: '10px' }}>
                          {t('statistics.drawdown')} -{stats.intraday_max_drawdown_pct.toFixed(1)}%
                        </div>
                      )}
                      {stats.winning_trades !== undefined && stats.losing_trades !== undefined && (
                        <div style={{ color: '#8c8c8c', fontSize: '10px' }}>
                          <span style={{ color: '#52c41a' }}>{stats.winning_trades}</span>
                          {' / '}
                          <span style={{ color: '#ff4d4f' }}>{stats.losing_trades}</span>
                        </div>
                      )}
                    </div>
                  ) : (
                    <div style={{ flex: 1, fontSize: '11px', color: '#bfbfbf' }}>
                      {t('statistics.noData')}
                    </div>
                  )}
                </>
              ) : null}
            </div>
          )
        })}
      </div>
    </div>
  )
}

export default StatisticsCalendar

