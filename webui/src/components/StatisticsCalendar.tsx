import React from 'react'

interface DailyStatistics {
  date: string
  total_trades: number
  total_volume: number
  total_pnl: number
  win_rate: number
  winning_trades?: number
  losing_trades?: number
}

interface StatisticsCalendarProps {
  year: number
  month: number
  dailyStats: DailyStatistics[]
}

const StatisticsCalendar: React.FC<StatisticsCalendarProps> = ({ year, month, dailyStats }) => {
  // 创建日期到统计数据的映射
  const statsMap = new Map<string, DailyStatistics>()
  dailyStats.forEach(stat => {
    statsMap.set(stat.date, stat)
  })

  // 获取月份的第一天和最后一天
  const firstDay = new Date(year, month - 1, 1)
  const lastDay = new Date(year, month, 0)
  const daysInMonth = lastDay.getDate()
  const startDayOfWeek = firstDay.getDay() // 0 = 周日, 6 = 周六

  // 星期标题
  const weekDays = ['日', '一', '二', '三', '四', '五', '六']

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

  // 格式化日期为 YYYY-MM-DD
  const formatDate = (date: Date): string => {
    const y = date.getFullYear()
    const m = String(date.getMonth() + 1).padStart(2, '0')
    const d = String(date.getDate()).padStart(2, '0')
    return `${y}-${m}-${d}`
  }

  // 获取某天的统计数据
  const getDayStats = (date: Date | null): DailyStatistics | null => {
    if (!date) return null
    const dateStr = formatDate(date)
    return statsMap.get(dateStr) || null
  }

  return (
    <div style={{ marginTop: '24px' }}>
      <div style={{ 
        display: 'grid', 
        gridTemplateColumns: 'repeat(7, 1fr)', 
        gap: '8px',
        border: '1px solid #e8e8e8',
        borderRadius: '4px',
        padding: '16px',
        backgroundColor: '#fafafa'
      }}>
        {/* 星期标题 */}
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
          const isToday = date && formatDate(date) === new Date().toISOString().split('T')[0]
          
          return (
            <div
              key={index}
              style={{
                minHeight: '100px',
                padding: '8px',
                border: '1px solid #e8e8e8',
                borderRadius: '4px',
                backgroundColor: date ? '#fff' : 'transparent',
                display: 'flex',
                flexDirection: 'column',
                cursor: date ? 'pointer' : 'default',
                position: 'relative',
                ...(isToday ? {
                  borderColor: '#1890ff',
                  borderWidth: '2px'
                } : {})
              }}
            >
              {date ? (
                <>
                  {/* 日期数字 */}
                  <div style={{
                    fontSize: '14px',
                    fontWeight: 'bold',
                    marginBottom: '4px',
                    color: isToday ? '#1890ff' : '#262626'
                  }}>
                    {date.getDate()}
                  </div>

                  {/* 统计数据 */}
                  {stats ? (
                    <div style={{ flex: 1, fontSize: '11px', lineHeight: '1.4' }}>
                      <div style={{
                        color: stats.total_pnl >= 0 ? '#52c41a' : '#ff4d4f',
                        fontWeight: 'bold',
                        marginBottom: '2px'
                      }}>
                        {stats.total_pnl >= 0 ? '+' : ''}{stats.total_pnl.toFixed(2)}
                      </div>
                      <div style={{ color: '#8c8c8c', marginBottom: '2px' }}>
                        {(stats.win_rate * 100).toFixed(1)}%
                      </div>
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
                      无数据
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

