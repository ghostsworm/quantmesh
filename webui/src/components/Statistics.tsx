import React, { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import {
  Button,
  Modal,
  ModalOverlay,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalCloseButton,
  useDisclosure,
  VStack,
  Text,
  SimpleGrid,
  Alert,
  AlertIcon,
  AlertDescription,
} from '@chakra-ui/react'
import { useSymbol } from '../contexts/SymbolContext'
import { useBot } from '../contexts/BotContext'
import { getStatistics, getDailyStatistics, getPnLByTimeRange, getExchangePnLDiagnosis, type ExchangePnLDiagnosisResponse } from '../services/api'
import StatisticsCalendar from './StatisticsCalendar'
import DailyCumulativePnLChart from './DailyCumulativePnLChart'
import { buildDailyEquityChartPoints, filterDailyStatsByRecentDays } from '../utils/dailyEquityChartData'
import { calendarMonthMatchesDateStr } from '../utils/calendarDateMatch'

interface StatisticsData {
  total_trades: number
  total_volume: number
  total_pnl: number // 淨利潤（已扣手續費）
  gross_pnl?: number // 毛利（未扣手續費）
  total_fee?: number // 手續費合計
  win_rate: number
  exchange_pnl?: number // 交易所已實現盈虧合計
  unrealized_pnl?: number // 待實現盈虧（當前持倉×當前價格）
}

interface DailyStatistics {
  date: string
  total_trades: number
  total_volume: number
  total_pnl: number // 當日淨利潤（已扣手續費）
  gross_pnl?: number // 當日毛利
  total_fee?: number // 當日手續費
  funding_fee?: number // 當日資金費用（正=收入，負=支出）
  win_rate: number
  winning_trades?: number
  losing_trades?: number
  volume_profit?: number   // 盈利交易量（pnl>0 的交易）
  volume_stop_loss?: number // 止損交易量（pnl<=0 的交易）
  open_price?: number      // 當日开盘價
  close_price?: number     // 當日收盘價
  price_change?: number    // 價格變化（收盘價-开盘價）
  price_change_pct?: number // 價格變化百分比
  cumulative_pnl?: number  // 累计盈亏
  unrealized_pnl?: number  // 當日收盤未實現盈虧
  book_value_pnl?: number  // 賬面盈虧 = 已平倉 + 未實現
  intraday_max_drawdown?: number
  intraday_max_drawdown_pct?: number
  exchange_pnl?: number // 當日交易所已實現盈虧
  /** 交易所 API 帳戶權益（USDT） */
  account_equity?: number
}

interface PnLBySymbol {
  symbol: string
  total_pnl: number
  total_trades: number
  total_volume: number
  win_rate: number
  unrealized_pnl?: number
}

const Statistics: React.FC = () => {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { botId } = useBot()
  const { selectedExchange, selectedSymbol, selectedMarketType } = useSymbol()
  const [stats, setStats] = useState<StatisticsData | null>(null)
  const [dailyStats, setDailyStats] = useState<DailyStatistics[]>([])
  const [maxDrawdown, setMaxDrawdown] = useState<number>(0)
  const [maxDrawdownPct, setMaxDrawdownPct] = useState<number>(0)
  const [pnlByTimeRange, setPnlByTimeRange] = useState<PnLBySymbol[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [days, setDays] = useState(30)
  const [startDate, setStartDate] = useState<string>(new Date(Date.now() - 30 * 24 * 60 * 60 * 1000).toISOString().split('T')[0])
  const [endDate, setEndDate] = useState<string>(new Date().toISOString().split('T')[0])
  const [currentMonth, setCurrentMonth] = useState(new Date().getMonth() + 1)
  const [currentYear, setCurrentYear] = useState(new Date().getFullYear())
  const { isOpen: isDiagnosisOpen, onOpen: onDiagnosisOpen, onClose: onDiagnosisClose } = useDisclosure()
  const [diagnosisData, setDiagnosisData] = useState<ExchangePnLDiagnosisResponse | null>(null)
  const [diagnosisLoading, setDiagnosisLoading] = useState(false)
  const [dailyMarketType, setDailyMarketType] = useState<string | undefined>(undefined)

  const handleOpenDiagnosis = async () => {
    onDiagnosisOpen()
    setDiagnosisLoading(true)
    setDiagnosisData(null)
    try {
      const data = await getExchangePnLDiagnosis(
        selectedExchange || undefined,
        selectedSymbol || undefined,
        undefined,
        undefined,
        selectedExchange && selectedSymbol ? (selectedMarketType ?? 'futures') : undefined
      )
      setDiagnosisData(data)
    } catch (err) {
      console.error('Diagnosis failed:', err)
    } finally {
      setDiagnosisLoading(false)
    }
  }

  useEffect(() => {
    const fetchData = async () => {
      try {
        setLoading(true)
        // 查詢365天的历史數據，确保显示所有交易記錄
        const mt = selectedExchange && selectedSymbol ? (selectedMarketType ?? 'futures') : undefined
        const bid = botId || undefined
        const [statsData, dailyData] = await Promise.all([
          getStatistics(selectedExchange || undefined, selectedSymbol || undefined, mt, bid),
          getDailyStatistics(selectedExchange || undefined, selectedSymbol || undefined, 365, mt, bid).catch(() => ({
            statistics: [],
            max_drawdown: 0,
            max_drawdown_pct: 0,
          })),
        ])
        setStats(statsData)
        setDailyStats(dailyData.statistics || [])
        setDailyMarketType(dailyData.market_type)
        setMaxDrawdown(dailyData.max_drawdown || 0)
        setMaxDrawdownPct(dailyData.max_drawdown_pct || 0)
        setError(null)
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to fetch statistics')
        console.error('Failed to fetch statistics:', err)
      } finally {
        setLoading(false)
      }
    }

    fetchData()
    // 每30秒刷新一次
    const interval = setInterval(fetchData, 30000)

    return () => clearInterval(interval)
  }, [selectedExchange, selectedSymbol, selectedMarketType, botId])

  const filteredDailyStats = useMemo(
    () => filterDailyStatsByRecentDays(dailyStats, days),
    [dailyStats, days]
  )

  const dailyEquityChartPoints = useMemo(
    () => buildDailyEquityChartPoints(filteredDailyStats),
    [filteredDailyStats]
  )

  // 獲取按時间区间的盈亏數據
  useEffect(() => {
    const loadPnLByTimeRange = async () => {
      try {
        const startTime = new Date(startDate).toISOString()
        const endTime = new Date(endDate + 'T23:59:59').toISOString()
        const data = await getPnLByTimeRange(startTime, endTime)
        setPnlByTimeRange(data.pnl_by_symbol || [])
      } catch (err) {
        console.error('Failed to fetch PnL by time range:', err)
      }
    }

    loadPnLByTimeRange()
  }, [startDate, endDate])

  if (loading && !stats) {
    return (
      <div className="statistics">
        <h2>{t('statistics.title')}</h2>
        <p>{t('statistics.loading')}</p>
      </div>
    )
  }

  if (error) {
    return (
      <div className="statistics">
        <h2>{t('statistics.title')}</h2>
        <p style={{ color: 'red' }}>{t('statistics.error')}: {error}</p>
      </div>
    )
  }

  const isGlobalMode = !botId
  const modeLabel = isGlobalMode
    ? t('statistics.modeGlobal')
    : t('statistics.modeSingleBot', { symbol: selectedSymbol || '' })

  return (
    <div className="statistics">
      <div style={{ marginBottom: '16px', padding: '12px 16px', background: '#f5f5f5', borderRadius: '8px', fontSize: '14px', color: '#595959', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <span>{modeLabel}</span>
        <Button size="sm" variant="outline" colorScheme="blue" onClick={handleOpenDiagnosis}>
          {t('statistics.pnlDiagnosis')}
        </Button>
      </div>
      <h2>{t('statistics.title')}</h2>

      {botId && (
        <Alert status="info" variant="left-accent" borderRadius="md" mb={4} maxW="960px">
          <AlertIcon />
          <AlertDescription fontSize="sm">{t('statistics.botScopeNotice')}</AlertDescription>
        </Alert>
      )}

      {stats && (
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(160px, 1fr))', gap: '12px', marginTop: '16px', maxWidth: '900px' }}>
          <div style={{ padding: '16px', border: '1px solid #e8e8e8', borderRadius: '4px' }}>
            <div style={{ fontSize: '14px', color: '#8c8c8c', marginBottom: '8px' }}>{t('statistics.totalTrades')}</div>
            <div style={{ fontSize: '24px', fontWeight: 'bold' }}>{stats.total_trades}</div>
          </div>
          <div style={{ padding: '16px', border: '1px solid #e8e8e8', borderRadius: '4px' }}>
            <div style={{ fontSize: '14px', color: '#8c8c8c', marginBottom: '8px' }}>{t('statistics.totalVolume')}</div>
            <div style={{ fontSize: '24px', fontWeight: 'bold' }}>{stats.total_volume.toFixed(4)}</div>
          </div>
          <div style={{ padding: '16px', border: '1px solid #e8e8e8', borderRadius: '4px' }}>
            <div style={{ fontSize: '14px', color: '#8c8c8c', marginBottom: '8px' }}>{t('statistics.netPnL')}</div>
            <div style={{ fontSize: '24px', fontWeight: 'bold', color: stats.total_pnl >= 0 ? '#52c41a' : '#ff4d4f' }}>
              {stats.total_pnl >= 0 ? '+' : ''}{stats.total_pnl.toFixed(2)}
            </div>
          </div>
          {(stats.gross_pnl !== undefined || stats.total_fee !== undefined) && (
            <>
              <div style={{ padding: '16px', border: '1px solid #e8e8e8', borderRadius: '4px' }}>
                <div style={{ fontSize: '14px', color: '#8c8c8c', marginBottom: '8px' }}>{t('statistics.grossPnL')}</div>
                <div style={{ fontSize: '20px', fontWeight: 'bold', color: (stats.gross_pnl ?? 0) >= 0 ? '#52c41a' : '#ff4d4f' }}>
                  {(stats.gross_pnl ?? 0) >= 0 ? '+' : ''}{(stats.gross_pnl ?? 0).toFixed(2)}
                </div>
              </div>
              <div style={{ padding: '16px', border: '1px solid #e8e8e8', borderRadius: '4px' }}>
                <div style={{ fontSize: '14px', color: '#8c8c8c', marginBottom: '8px' }}>{t('statistics.totalFee')}</div>
                <div style={{ fontSize: '20px', fontWeight: 'bold', color: '#fa8c16' }}>
                  -{(stats.total_fee ?? 0).toFixed(2)}
                </div>
              </div>
            </>
          )}
          {stats.exchange_pnl !== undefined && stats.exchange_pnl !== 0 && (
            <div style={{ padding: '16px', border: '1px solid #e8e8e8', borderRadius: '4px' }} title={t('statistics.exchangePnlTooltip')}>
              <div style={{ fontSize: '14px', color: '#8c8c8c', marginBottom: '8px' }}>{t('statistics.exchangePnl')}</div>
              <div style={{ fontSize: '20px', fontWeight: 'bold', color: stats.exchange_pnl >= 0 ? '#52c41a' : '#ff4d4f' }}>
                {stats.exchange_pnl >= 0 ? '+' : ''}{stats.exchange_pnl.toFixed(2)}
              </div>
            </div>
          )}
          {(stats.unrealized_pnl !== undefined && Math.abs(stats.unrealized_pnl) > 0.001) && (
            <div style={{ padding: '16px', border: '1px solid #e8e8e8', borderRadius: '4px' }} title={t('statistics.unrealizedPnLTooltip')}>
              <div style={{ fontSize: '14px', color: '#8c8c8c', marginBottom: '8px' }}>{t('statistics.unrealizedPnL')}</div>
              <div style={{ fontSize: '20px', fontWeight: 'bold', color: stats.unrealized_pnl >= 0 ? '#95de64' : '#ff7875', fontStyle: 'italic' }}>
                {stats.unrealized_pnl >= 0 ? '+' : ''}{stats.unrealized_pnl.toFixed(2)}
              </div>
            </div>
          )}
          <div style={{ padding: '16px', border: '1px solid #e8e8e8', borderRadius: '4px' }}>
            <div style={{ fontSize: '14px', color: '#8c8c8c', marginBottom: '8px' }}>{t('statistics.winRate')}</div>
            <div style={{ fontSize: '24px', fontWeight: 'bold' }}>{(stats.win_rate * 100).toFixed(2)}%</div>
          </div>
          <div style={{ padding: '16px', border: '1px solid #e8e8e8', borderRadius: '4px', background: maxDrawdownPct > 10 ? '#fff2f0' : '#f6ffed' }}>
            <div style={{ fontSize: '14px', color: '#8c8c8c', marginBottom: '8px' }}>{t('statistics.maxDrawdown')}</div>
            <div style={{ fontSize: '24px', fontWeight: 'bold', color: maxDrawdownPct > 10 ? '#ff4d4f' : '#52c41a' }}>
              {maxDrawdownPct.toFixed(2)}%
            </div>
            <div style={{ fontSize: '12px', color: '#8c8c8c', marginTop: '4px' }}>
              -{maxDrawdown.toFixed(2)} USDT
            </div>
          </div>
        </div>
      )}

      <div style={{ marginTop: '32px', maxWidth: '960px' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '12px', flexWrap: 'wrap', gap: '12px' }}>
          <h3 style={{ margin: 0, display: 'flex', alignItems: 'center', gap: '10px', flexWrap: 'wrap' }}>
            {t('statistics.dailyEquityCurveTitle')}
            {dailyMarketType ? (
              <span
                style={{
                  fontSize: '12px',
                  fontWeight: 500,
                  color: '#595959',
                  padding: '2px 8px',
                  borderRadius: 4,
                  background: '#f0f0f0',
                }}
              >
                {dailyMarketType === 'spot' ? t('statistics.marketTypeSpot') : t('statistics.marketTypeFutures')}
              </span>
            ) : null}
          </h3>
          <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
            <label style={{ fontSize: '14px', color: '#595959' }}>
              {t('statistics.dailyEquityCurveRange')}
              <select value={days} onChange={(e) => setDays(Number(e.target.value))} style={{ marginLeft: '8px', padding: '8px' }}>
                <option value={7}>{t('statistics.last7d')}</option>
                <option value={30}>{t('statistics.last30d')}</option>
                <option value={90}>{t('statistics.last90d')}</option>
                <option value={365}>{t('statistics.last365d')}</option>
              </select>
            </label>
          </div>
        </div>
        <p style={{ fontSize: '13px', color: '#8c8c8c', marginBottom: '16px', lineHeight: 1.5 }}>
          {t('statistics.dailyEquityCurveHint')}
        </p>
        <DailyCumulativePnLChart data={dailyEquityChartPoints} />
      </div>

      <div style={{ marginTop: '32px', maxWidth: '900px' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '16px' }}>
          <h3>{t('statistics.calendarView')}</h3>
          <div style={{ display: 'flex', gap: '12px', alignItems: 'center' }}>
            <button
              onClick={() => {
                if (currentMonth === 1) {
                  setCurrentMonth(12)
                  setCurrentYear(currentYear - 1)
                } else {
                  setCurrentMonth(currentMonth - 1)
                }
              }}
              style={{ padding: '6px 12px', border: '1px solid #d9d9d9', borderRadius: '4px', cursor: 'pointer' }}
            >
              {t('statistics.prevMonth')}
            </button>
            <span style={{ minWidth: '120px', textAlign: 'center' }}>
              {t('statistics.yearMonth', { year: currentYear, month: currentMonth })}
            </span>
            <button
              onClick={() => {
                if (currentMonth === 12) {
                  setCurrentMonth(1)
                  setCurrentYear(currentYear + 1)
                } else {
                  setCurrentMonth(currentMonth + 1)
                }
              }}
              style={{ padding: '6px 12px', border: '1px solid #d9d9d9', borderRadius: '4px', cursor: 'pointer' }}
            >
              {t('statistics.nextMonth')}
            </button>
          </div>
        </div>
        
        {/* 日历组件 */}
        <StatisticsCalendar 
          year={currentYear}
          month={currentMonth}
          dailyStats={dailyStats.filter(stat =>
            calendarMonthMatchesDateStr(stat.date, currentYear, currentMonth)
          )}
          onDayClick={(date) => {
            if (botId) navigate(`/bots/${botId}/statistics/daily/${date}`)
            else navigate(`/statistics/daily/${date}`)
          }}
        />
      </div>

      <div style={{ marginTop: '32px' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '16px' }}>
          <h3>{t('statistics.dailyStats')}</h3>
          <span style={{ fontSize: '13px', color: '#8c8c8c' }}>{t('statistics.dailyStatsRangeHint')}</span>
        </div>
        {filteredDailyStats.length > 0 ? (
          <div style={{ overflowX: 'auto' }}>
            <table style={{ width: '100%', borderCollapse: 'collapse' }}>
              <thead>
                <tr style={{ borderBottom: '2px solid #e8e8e8' }}>
                  <th style={{ padding: '12px', textAlign: 'left' }}>{t('statistics.date')}</th>
                  <th style={{ padding: '12px', textAlign: 'right' }}>{t('statistics.trades')}</th>
                  <th style={{ padding: '12px', textAlign: 'right' }} title={t('statistics.volumeTooltip')}>{t('statistics.volume')}</th>
                  <th style={{ padding: '12px', textAlign: 'right' }}>{t('statistics.volumeProfit')}</th>
                  <th style={{ padding: '12px', textAlign: 'right' }}>{t('statistics.volumeStopLoss')}</th>
                  <th style={{ padding: '12px', textAlign: 'right' }}>{t('statistics.pnl')}</th>
                  <th style={{ padding: '12px', textAlign: 'right' }} title={t('statistics.exchangePnlTooltip')}>{t('statistics.exchangePnl')}</th>
                  {(filteredDailyStats.some(s => s.gross_pnl !== undefined) || filteredDailyStats.some(s => s.total_fee !== undefined)) && (
                    <>
                      <th style={{ padding: '12px', textAlign: 'right' }}>{t('statistics.grossPnL')}</th>
                      <th style={{ padding: '12px', textAlign: 'right' }}>{t('statistics.totalFee')}</th>
                    </>
                  )}
                  <th style={{ padding: '12px', textAlign: 'right' }}>{t('statistics.fundingFee')}</th>
                  <th style={{ padding: '12px', textAlign: 'right' }}>{t('statistics.unrealizedPnL')}</th>
                  <th style={{ padding: '12px', textAlign: 'right' }} title={t('statistics.bookValuePnL')}>{t('statistics.bookValuePnL')}</th>
                  {filteredDailyStats.some((s) => s.account_equity !== undefined && s.account_equity !== null) ? (
                    <th style={{ padding: '12px', textAlign: 'right' }} title={t('statistics.accountEquityTooltip')}>
                      {t('statistics.accountEquity')}
                    </th>
                  ) : null}
                  <th style={{ padding: '12px', textAlign: 'right' }}>{t('statistics.cumulativePnL')}</th>
                  <th style={{ padding: '12px', textAlign: 'right' }}>{t('statistics.winRate')}</th>
                  <th style={{ padding: '12px', textAlign: 'right' }}>{t('statistics.intradayDrawdown')}</th>
                  <th style={{ padding: '12px', textAlign: 'right' }}>{t('statistics.winLoss')}</th>
                  <th style={{ padding: '12px', textAlign: 'right' }}>{t('statistics.openClose')}</th>
                  <th style={{ padding: '12px', textAlign: 'right' }}>{t('statistics.priceChange')}</th>
                </tr>
              </thead>
              <tbody>
                {filteredDailyStats.map((stat, index) => (
                  <tr key={index} style={{ borderBottom: '1px solid #f0f0f0' }}>
                    <td style={{ padding: '12px' }}>{new Date(stat.date).toLocaleDateString('zh-CN')}</td>
                    <td style={{ padding: '12px', textAlign: 'right' }}>{stat.total_trades}</td>
                    <td style={{ padding: '12px', textAlign: 'right' }} title={t('statistics.volumeTooltip')}>{stat.total_volume.toFixed(4)}</td>
                    <td style={{ padding: '12px', textAlign: 'right', color: '#52c41a' }}>
                      {stat.volume_profit !== undefined ? stat.volume_profit.toFixed(4) : '-'}
                    </td>
                    <td style={{ padding: '12px', textAlign: 'right', color: '#ff4d4f' }}>
                      {stat.volume_stop_loss !== undefined ? stat.volume_stop_loss.toFixed(4) : '-'}
                    </td>
                    <td style={{ padding: '12px', textAlign: 'right', color: stat.total_pnl >= 0 ? '#52c41a' : '#ff4d4f' }}>
                      {stat.total_pnl >= 0 ? '+' : ''}{stat.total_pnl.toFixed(2)}
                    </td>
                    <td style={{ padding: '12px', textAlign: 'right', color: (stat.exchange_pnl ?? 0) >= 0 ? '#1890ff' : '#ff4d4f', fontSize: '13px' }}>
                      {stat.exchange_pnl !== undefined && stat.exchange_pnl !== 0
                        ? (stat.exchange_pnl >= 0 ? '+' : '') + stat.exchange_pnl.toFixed(2)
                        : '-'}
                    </td>
                    {(stat.gross_pnl !== undefined || stat.total_fee !== undefined) && (
                      <>
                        <td style={{ padding: '12px', textAlign: 'right', color: (stat.gross_pnl ?? 0) >= 0 ? '#52c41a' : '#ff4d4f' }}>
                          {(stat.gross_pnl ?? 0) >= 0 ? '+' : ''}{(stat.gross_pnl ?? 0).toFixed(2)}
                        </td>
                        <td style={{ padding: '12px', textAlign: 'right', color: '#fa8c16' }}>
                          -{(stat.total_fee ?? 0).toFixed(2)}
                        </td>
                      </>
                    )}
                    <td style={{ padding: '12px', textAlign: 'right', color: (stat.funding_fee ?? 0) >= 0 ? '#52c41a' : '#fa8c16' }}>
                      {stat.funding_fee !== undefined && stat.funding_fee !== 0
                        ? (stat.funding_fee >= 0 ? '+' : '') + stat.funding_fee.toFixed(2)
                        : '-'}
                    </td>
                    <td style={{ padding: '12px', textAlign: 'right', color: (stat.unrealized_pnl ?? 0) >= 0 ? '#95de64' : '#ff7875', fontStyle: 'italic' }}>
                      {stat.unrealized_pnl !== undefined && stat.unrealized_pnl !== 0
                        ? (stat.unrealized_pnl >= 0 ? '+' : '') + stat.unrealized_pnl.toFixed(2)
                        : '-'}
                    </td>
                    <td style={{ padding: '12px', textAlign: 'right', color: ((stat.book_value_pnl ?? stat.total_pnl) >= 0 ? '#389e0d' : '#cf1322'), fontWeight: 600 }}>
                      {(stat.book_value_pnl ?? stat.total_pnl) >= 0 ? '+' : ''}{(stat.book_value_pnl ?? stat.total_pnl).toFixed(2)}
                    </td>
                    {filteredDailyStats.some((s) => s.account_equity !== undefined && s.account_equity !== null) ? (
                      <td style={{ padding: '12px', textAlign: 'right', fontWeight: 600, color: '#1890ff' }}>
                        {stat.account_equity !== undefined && stat.account_equity !== null
                          ? stat.account_equity.toFixed(2)
                          : '—'}
                      </td>
                    ) : null}
                    <td style={{ padding: '12px', textAlign: 'right', color: (stat.cumulative_pnl || 0) >= 0 ? '#52c41a' : '#ff4d4f' }}>
                      {(stat.cumulative_pnl || 0) >= 0 ? '+' : ''}{(stat.cumulative_pnl || 0).toFixed(2)}
                    </td>
                    <td style={{ padding: '12px', textAlign: 'right' }}>{(stat.win_rate * 100).toFixed(2)}%</td>
                    <td style={{ padding: '12px', textAlign: 'right', fontSize: '12px', color: '#8c8c8c' }}>
                      {stat.intraday_max_drawdown_pct !== undefined && stat.intraday_max_drawdown_pct > 0
                        ? `-${stat.intraday_max_drawdown_pct.toFixed(1)}%`
                        : '-'}
                    </td>
                    <td style={{ padding: '12px', textAlign: 'right', fontSize: '12px', color: '#8c8c8c' }}>
                      {stat.winning_trades !== undefined && stat.losing_trades !== undefined ? (
                        <>
                          <span style={{ color: '#52c41a' }}>{stat.winning_trades}</span>
                          {' / '}
                          <span style={{ color: '#ff4d4f' }}>{stat.losing_trades}</span>
                        </>
                      ) : '-'}
                    </td>
                    <td style={{ padding: '12px', textAlign: 'right', fontSize: '12px' }}>
                      {stat.open_price !== undefined && stat.close_price !== undefined ? (
                        <span title={t('statistics.openCloseTooltip', { open: stat.open_price.toFixed(2), close: stat.close_price.toFixed(2) })}>
                          {stat.open_price.toFixed(0)} / {stat.close_price.toFixed(0)}
                        </span>
                      ) : '-'}
                    </td>
                    <td style={{ padding: '12px', textAlign: 'right', color: (stat.price_change_pct || 0) >= 0 ? '#52c41a' : '#ff4d4f' }}>
                      {stat.price_change_pct !== undefined ? (
                        <>
                          {(stat.price_change_pct >= 0 ? '+' : '')}{stat.price_change_pct.toFixed(2)}%
                        </>
                      ) : '-'}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <div style={{ padding: '32px', textAlign: 'center', color: '#8c8c8c' }}>{t('statistics.noStats')}</div>
        )}
      </div>

      <div style={{ marginTop: '32px' }}>
        <h3>{t('statistics.pnlByTimeRange')}</h3>
        <div style={{ display: 'flex', gap: '12px', alignItems: 'center', marginBottom: '16px' }}>
          <label>
            {t('statistics.startDate')}:
            <input
              type="date"
              value={startDate}
              onChange={(e) => setStartDate(e.target.value)}
              style={{ marginLeft: '8px', padding: '6px' }}
            />
          </label>
          <label>
            {t('statistics.endDate')}:
            <input
              type="date"
              value={endDate}
              onChange={(e) => setEndDate(e.target.value)}
              style={{ marginLeft: '8px', padding: '6px' }}
            />
          </label>
        </div>

        {pnlByTimeRange.length > 0 ? (
          <div style={{ overflowX: 'auto' }}>
            <table style={{ width: '100%', borderCollapse: 'collapse' }}>
              <thead>
                <tr style={{ borderBottom: '2px solid #e8e8e8' }}>
                  <th style={{ padding: '12px', textAlign: 'left' }}>{t('statistics.symbolPair')}</th>
                  <th style={{ padding: '12px', textAlign: 'right' }}>{t('statistics.trades')}</th>
                  <th style={{ padding: '12px', textAlign: 'right' }}>{t('statistics.volume')}</th>
                  <th style={{ padding: '12px', textAlign: 'right' }}>{t('statistics.pnl')}</th>
                  <th style={{ padding: '12px', textAlign: 'right' }}>{t('statistics.unrealizedPnL')}</th>
                  <th style={{ padding: '12px', textAlign: 'right' }}>{t('statistics.winRate')}</th>
                </tr>
              </thead>
              <tbody>
                {pnlByTimeRange.map((item, index) => (
                  <tr key={index} style={{ borderBottom: '1px solid #f0f0f0' }}>
                    <td style={{ padding: '12px' }}>{item.symbol}</td>
                    <td style={{ padding: '12px', textAlign: 'right' }}>{item.total_trades}</td>
                    <td style={{ padding: '12px', textAlign: 'right' }}>{item.total_volume.toFixed(4)}</td>
                    <td style={{ padding: '12px', textAlign: 'right', color: item.total_pnl >= 0 ? '#52c41a' : '#ff4d4f' }}>
                      {item.total_pnl >= 0 ? '+' : ''}{item.total_pnl.toFixed(2)}
                    </td>
                    <td style={{ padding: '12px', textAlign: 'right', color: (item.unrealized_pnl ?? 0) >= 0 ? '#95de64' : '#ff7875', fontStyle: 'italic' }}>
                      {item.unrealized_pnl !== undefined && item.unrealized_pnl !== 0
                        ? (item.unrealized_pnl >= 0 ? '+' : '') + item.unrealized_pnl.toFixed(2)
                        : '-'}
                    </td>
                    <td style={{ padding: '12px', textAlign: 'right' }}>{(item.win_rate * 100).toFixed(2)}%</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <div style={{ padding: '32px', textAlign: 'center', color: '#8c8c8c' }}>{t('statistics.noDataInRange')}</div>
        )}
      </div>

      {/* 网格 vs 交易所盈亏诊断 Modal */}
      <Modal isOpen={isDiagnosisOpen} onClose={onDiagnosisClose} size="lg" scrollBehavior="inside">
        <ModalOverlay />
        <ModalContent>
          <ModalHeader>{t('statistics.pnlDiagnosisTitle')}</ModalHeader>
          <ModalCloseButton />
          <ModalBody pb={6}>
            {diagnosisLoading ? (
              <Text>{t('statistics.loading')}</Text>
            ) : diagnosisData?.error ? (
              <Alert status="error" borderRadius="md">
                <AlertIcon />
                <Text fontSize="sm">{String(diagnosisData.error)}</Text>
              </Alert>
            ) : diagnosisData?.pnl_comparison ? (
              <VStack align="stretch" spacing={4}>
                <SimpleGrid columns={2} spacing={4}>
                  <div style={{ padding: '12px', border: '1px solid #e8e8e8', borderRadius: '8px' }}>
                    <Text fontSize="sm" color="gray.600">{t('statistics.diagnosisGridPnL')}</Text>
                    <Text fontSize="xl" fontWeight="bold" color={diagnosisData.pnl_comparison.grid_pnl >= 0 ? 'green.500' : 'red.500'}>
                      {diagnosisData.pnl_comparison.grid_pnl >= 0 ? '+' : ''}{diagnosisData.pnl_comparison.grid_pnl.toFixed(2)}
                    </Text>
                  </div>
                  <div style={{ padding: '12px', border: '1px solid #e8e8e8', borderRadius: '8px' }}>
                    <Text fontSize="sm" color="gray.600">{t('statistics.diagnosisExchangePnL')}</Text>
                    <Text fontSize="xl" fontWeight="bold" color={diagnosisData.pnl_comparison.exchange_pnl >= 0 ? 'green.500' : 'red.500'}>
                      {diagnosisData.pnl_comparison.exchange_pnl >= 0 ? '+' : ''}{diagnosisData.pnl_comparison.exchange_pnl.toFixed(2)}
                    </Text>
                  </div>
                </SimpleGrid>
                <div style={{ padding: '12px', border: '1px solid #e8e8e8', borderRadius: '8px' }}>
                  <Text fontSize="sm" color="gray.600">{t('statistics.diagnosisDiscrepancy')}</Text>
                  <Text fontSize="lg" fontWeight="bold">
                    {diagnosisData.pnl_comparison.discrepancy >= 0 ? '+' : ''}{diagnosisData.pnl_comparison.discrepancy.toFixed(2)}
                  </Text>
                </div>
                {diagnosisData.pnl_comparison.discrepancy_explanation && (
                  <Alert status="info" borderRadius="md">
                    <AlertIcon />
                    <Text fontSize="sm">{diagnosisData.pnl_comparison.discrepancy_explanation}</Text>
                  </Alert>
                )}
                <SimpleGrid columns={2} spacing={2} fontSize="sm" color="gray.600">
                  <Text>{t('statistics.diagnosisOrdersWithPnL')}: {diagnosisData.pnl_comparison.orders_with_realized_pnl}</Text>
                  <Text>{t('statistics.diagnosisSellOrdersMissingPnL')}: {diagnosisData.pnl_comparison.sell_orders_missing_pnl}</Text>
                </SimpleGrid>
                <Text fontSize="xs" color="gray.500" fontStyle="italic">{diagnosisData.note}</Text>
              </VStack>
            ) : (
              <Text color="gray.500">{t('statistics.diagnosisNoData')}</Text>
            )}
          </ModalBody>
        </ModalContent>
      </Modal>
    </div>
  )
}

export default Statistics
