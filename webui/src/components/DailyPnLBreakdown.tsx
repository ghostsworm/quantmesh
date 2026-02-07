import React, { useEffect, useState } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useSymbol } from '../contexts/SymbolContext'
import { getDailyPnLBreakdown, type DailyPnLBreakdownResponse } from '../services/api'
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  Tooltip,
  ResponsiveContainer,
  CartesianGrid,
} from 'recharts'

const colorPositive = '#52c41a'
const colorNegative = '#ff4d4f'

const DailyPnLBreakdown: React.FC = () => {
  const { date } = useParams<{ date: string }>()
  const navigate = useNavigate()
  const { t } = useTranslation()
  const { selectedExchange, selectedSymbol } = useSymbol()
  const [data, setData] = useState<DailyPnLBreakdownResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!date) return
    const fetchData = async () => {
      setLoading(true)
      setError(null)
      try {
        const res = await getDailyPnLBreakdown(date, selectedExchange || undefined, selectedSymbol || undefined)
        setData(res)
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to load')
      } finally {
        setLoading(false)
      }
    }
    fetchData()
  }, [date, selectedExchange, selectedSymbol])

  if (!date) {
    return (
      <div style={{ padding: '24px' }}>
        <p>{t('common.error')}</p>
        <button type="button" onClick={() => navigate('/statistics')} style={{ marginTop: '12px' }}>
          {t('dailyBreakdown.back')}
        </button>
      </div>
    )
  }

  if (loading && !data) {
    return (
      <div style={{ padding: '24px' }}>
        <p>{t('common.loading')}</p>
      </div>
    )
  }

  if (error) {
    return (
      <div style={{ padding: '24px' }}>
        <p style={{ color: colorNegative }}>{error}</p>
        <button type="button" onClick={() => navigate('/statistics')} style={{ marginTop: '12px' }}>
          {t('dailyBreakdown.back')}
        </button>
      </div>
    )
  }

  const s = data?.summary
  if (!s) {
    return (
      <div style={{ padding: '24px' }}>
        <button type="button" onClick={() => navigate('/statistics')}>
          {t('dailyBreakdown.back')}
        </button>
        <p style={{ marginTop: '16px' }}>{t('dailyBreakdown.noData')}</p>
      </div>
    )
  }

  const formatNum = (v: number) => (v >= 0 ? `+${v.toFixed(2)}` : v.toFixed(2))
  const colorOf = (v: number) => (v >= 0 ? colorPositive : colorNegative)

  const hourlyChartData = (data.hourly_equity || []).map((p) => ({
    time: new Date(p.timestamp * 1000).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' }),
    equity: p.equity,
  }))

  return (
    <div style={{ padding: '24px', maxWidth: '1200px', margin: '0 auto' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: '12px', marginBottom: '24px' }}>
        <button
          type="button"
          onClick={() => navigate('/statistics')}
          style={{
            padding: '8px 16px',
            border: '1px solid #d9d9d9',
            borderRadius: '4px',
            cursor: 'pointer',
            background: '#fff',
          }}
        >
          {t('dailyBreakdown.back')}
        </button>
        <h2 style={{ margin: 0, fontSize: '20px' }}>
          {t('dailyBreakdown.title')} — {date}
        </h2>
      </div>

      {/* Section 1: Core Data */}
      <section style={{ marginBottom: '32px' }}>
        <h3 style={{ marginBottom: '16px', fontSize: '16px', color: '#262626' }}>
          {t('dailyBreakdown.coreData')}
        </h3>
        <div style={{ overflowX: 'auto' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', border: '1px solid #e8e8e8' }}>
            <thead>
              <tr style={{ background: '#fafafa', borderBottom: '2px solid #e8e8e8' }}>
                <th style={{ padding: '12px', textAlign: 'left' }}>{t('dailyBreakdown.buyOrders')}</th>
                <th style={{ padding: '12px', textAlign: 'right' }}>{t('dailyBreakdown.buyQty')}</th>
                <th style={{ padding: '12px', textAlign: 'right' }}>{t('dailyBreakdown.buyValue')}</th>
                <th style={{ padding: '12px', textAlign: 'left' }}>{t('dailyBreakdown.sellOrders')}</th>
                <th style={{ padding: '12px', textAlign: 'right' }}>{t('dailyBreakdown.sellQty')}</th>
                <th style={{ padding: '12px', textAlign: 'right' }}>{t('dailyBreakdown.sellValue')}</th>
                <th style={{ padding: '12px', textAlign: 'right' }}>{t('dailyBreakdown.netCashFlow')}</th>
                <th style={{ padding: '12px', textAlign: 'right' }}>{t('dailyBreakdown.netQtyChange')}</th>
                <th style={{ padding: '12px', textAlign: 'right' }}>{t('dailyBreakdown.startPositionQty')}</th>
                <th style={{ padding: '12px', textAlign: 'right' }}>{t('dailyBreakdown.endPositionQty')}</th>
                <th style={{ padding: '12px', textAlign: 'right' }}>{t('dailyBreakdown.startPositionValue')}</th>
                <th style={{ padding: '12px', textAlign: 'right' }}>{t('dailyBreakdown.endPositionValue')}</th>
                <th style={{ padding: '12px', textAlign: 'right' }}>{t('dailyBreakdown.positionValueChange')}</th>
              </tr>
            </thead>
            <tbody>
              <tr style={{ borderBottom: '1px solid #f0f0f0' }}>
                <td style={{ padding: '12px' }}>{s.total_buy_orders}</td>
                <td style={{ padding: '12px', textAlign: 'right' }}>{s.total_buy_qty.toFixed(4)}</td>
                <td style={{ padding: '12px', textAlign: 'right' }}>{s.total_buy_value.toFixed(2)}</td>
                <td style={{ padding: '12px' }}>{s.total_sell_orders}</td>
                <td style={{ padding: '12px', textAlign: 'right' }}>{s.total_sell_qty.toFixed(4)}</td>
                <td style={{ padding: '12px', textAlign: 'right' }}>{s.total_sell_value.toFixed(2)}</td>
                <td style={{ padding: '12px', textAlign: 'right', color: colorOf(s.net_cash_flow) }}>
                  {formatNum(s.net_cash_flow)}
                </td>
                <td style={{ padding: '12px', textAlign: 'right' }}>{s.net_qty_change.toFixed(4)}</td>
                <td style={{ padding: '12px', textAlign: 'right' }}>{s.start_position_qty.toFixed(4)}</td>
                <td style={{ padding: '12px', textAlign: 'right' }}>{s.end_position_qty.toFixed(4)}</td>
                <td style={{ padding: '12px', textAlign: 'right' }}>{s.start_position_value.toFixed(2)}</td>
                <td style={{ padding: '12px', textAlign: 'right' }}>{s.end_position_value.toFixed(2)}</td>
                <td style={{ padding: '12px', textAlign: 'right', color: colorOf(s.position_value_change) }}>
                  {formatNum(s.position_value_change)}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      {/* Section 2: Calculation Steps */}
      <section style={{ marginBottom: '32px' }}>
        <h3 style={{ marginBottom: '16px', fontSize: '16px', color: '#262626' }}>
          {t('dailyBreakdown.calculationSteps')}
        </h3>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(240px, 1fr))', gap: '16px' }}>
          <div style={{ padding: '16px', border: '1px solid #e8e8e8', borderRadius: '8px', background: '#fafafa' }}>
            <div style={{ fontSize: '12px', color: '#8c8c8c', marginBottom: '8px' }}>1. {t('dailyBreakdown.stepCashFlow')}</div>
            <div style={{ fontSize: '14px' }}>
              {t('dailyBreakdown.sellRevenue')} − {t('dailyBreakdown.buyCost')} = {t('dailyBreakdown.netCashFlow')}
            </div>
            <div style={{ fontSize: '18px', fontWeight: 'bold', color: colorOf(s.net_cash_flow), marginTop: '8px' }}>
              {formatNum(s.net_cash_flow)}
            </div>
          </div>
          <div style={{ padding: '16px', border: '1px solid #e8e8e8', borderRadius: '8px', background: '#fafafa' }}>
            <div style={{ fontSize: '12px', color: '#8c8c8c', marginBottom: '8px' }}>2. {t('dailyBreakdown.stepPositionChange')}</div>
            <div style={{ fontSize: '14px' }}>
              {t('dailyBreakdown.endValue')} − {t('dailyBreakdown.startValue')}
            </div>
            <div style={{ fontSize: '18px', fontWeight: 'bold', color: colorOf(s.position_value_change), marginTop: '8px' }}>
              {formatNum(s.position_value_change)}
            </div>
          </div>
          <div style={{ padding: '16px', border: '1px solid #e8e8e8', borderRadius: '8px', background: '#fafafa' }}>
            <div style={{ fontSize: '12px', color: '#8c8c8c', marginBottom: '8px' }}>3. {t('dailyBreakdown.stepNetTradingPnL')}</div>
            <div style={{ fontSize: '14px' }}>
              {t('dailyBreakdown.cashFlow')} + {t('dailyBreakdown.positionChange')}
            </div>
            <div style={{ fontSize: '18px', fontWeight: 'bold', color: colorOf(s.net_trading_pnl), marginTop: '8px' }}>
              {formatNum(s.net_trading_pnl)}
            </div>
          </div>
          <div style={{ padding: '16px', border: '1px solid #e8e8e8', borderRadius: '8px', background: '#fafafa' }}>
            <div style={{ fontSize: '12px', color: '#8c8c8c', marginBottom: '8px' }}>4. {t('dailyBreakdown.stepFeeCalculation')}</div>
            <div style={{ fontSize: '14px' }}>{t('dailyBreakdown.totalFee')}</div>
            <div style={{ fontSize: '18px', fontWeight: 'bold', color: colorNegative, marginTop: '8px' }}>
              −{s.total_fee.toFixed(2)}
            </div>
          </div>
        </div>
      </section>

      {/* Section 3: Final Equation */}
      <section style={{ marginBottom: '32px' }}>
        <h3 style={{ marginBottom: '16px', fontSize: '16px', color: '#262626' }}>
          {t('dailyBreakdown.finalEquation')}
        </h3>
        <div
          style={{
            padding: '20px',
            border: '2px solid #e8e8e8',
            borderRadius: '8px',
            background: '#fafafa',
            fontSize: '15px',
          }}
        >
          {t('dailyBreakdown.netTradingPnL')} + {t('dailyBreakdown.fees')} + {t('dailyBreakdown.funding')} ={' '}
          <span style={{ fontWeight: 'bold', color: colorOf(s.net_trading_pnl - s.total_fee + s.funding_fee) }}>
            {formatNum(s.net_trading_pnl - s.total_fee + s.funding_fee)}
          </span>
          <div style={{ marginTop: '12px', fontSize: '13px', color: '#8c8c8c' }}>
            {formatNum(s.net_trading_pnl)} − {s.total_fee.toFixed(2)} + {formatNum(s.funding_fee)}
          </div>
        </div>
      </section>

      {/* Section 4: Intraday Equity Curve */}
      {hourlyChartData.length > 0 && (
        <section style={{ marginBottom: '32px' }}>
          <h3 style={{ marginBottom: '16px', fontSize: '16px', color: '#262626' }}>
            {t('dailyBreakdown.intradayEquityCurve')}
          </h3>
          <div style={{ height: '300px', width: '100%' }}>
            <ResponsiveContainer width="100%" height="100%">
              <LineChart data={hourlyChartData} margin={{ top: 8, right: 8, left: 8, bottom: 8 }}>
                <CartesianGrid strokeDasharray="3 3" stroke="#f0f0f0" />
                <XAxis dataKey="time" tick={{ fontSize: 12 }} />
                <YAxis tick={{ fontSize: 12 }} tickFormatter={(v) => v.toFixed(0)} />
                <Tooltip formatter={(v: number) => [v.toFixed(2), t('dailyBreakdown.equity')]} />
                <Line type="monotone" dataKey="equity" stroke="#1890ff" strokeWidth={2} dot={false} />
              </LineChart>
            </ResponsiveContainer>
          </div>
        </section>
      )}

      {/* Top trades (optional compact list) */}
      {data.top_trades && data.top_trades.length > 0 && (
        <section>
          <h3 style={{ marginBottom: '12px', fontSize: '16px', color: '#262626' }}>
            {t('dailyBreakdown.topTrades')}
          </h3>
          <div style={{ overflowX: 'auto' }}>
            <table style={{ width: '100%', borderCollapse: 'collapse', border: '1px solid #e8e8e8' }}>
              <thead>
                <tr style={{ background: '#fafafa' }}>
                  <th style={{ padding: '8px', textAlign: 'left' }}>{t('dailyBreakdown.sellOrderId')}</th>
                  <th style={{ padding: '8px', textAlign: 'right' }}>{t('dailyBreakdown.buyPrice')}</th>
                  <th style={{ padding: '8px', textAlign: 'right' }}>{t('dailyBreakdown.sellPrice')}</th>
                  <th style={{ padding: '8px', textAlign: 'right' }}>{t('dailyBreakdown.quantity')}</th>
                  <th style={{ padding: '8px', textAlign: 'right' }}>{t('dailyBreakdown.pnl')}</th>
                </tr>
              </thead>
              <tbody>
                {data.top_trades.slice(0, 10).map((tr, i) => (
                  <tr key={i} style={{ borderBottom: '1px solid #f0f0f0' }}>
                    <td style={{ padding: '8px' }}>{tr.sell_order_id}</td>
                    <td style={{ padding: '8px', textAlign: 'right' }}>{tr.buy_price.toFixed(2)}</td>
                    <td style={{ padding: '8px', textAlign: 'right' }}>{tr.sell_price.toFixed(2)}</td>
                    <td style={{ padding: '8px', textAlign: 'right' }}>{tr.quantity.toFixed(4)}</td>
                    <td style={{ padding: '8px', textAlign: 'right', color: colorOf(tr.pnl) }}>
                      {formatNum(tr.pnl)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      )}
    </div>
  )
}

export default DailyPnLBreakdown
