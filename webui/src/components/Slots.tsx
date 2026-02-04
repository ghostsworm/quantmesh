import React, { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useSymbol } from '../contexts/SymbolContext'
import { getSlots, getSymbols, SlotInfo } from '../services/api'

const Slots: React.FC = () => {
  const { t } = useTranslation()
  const { selectedExchange, selectedSymbol } = useSymbol()
  const [slots, setSlots] = useState<SlotInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [sortBy, setSortBy] = useState<'price' | 'status'>('price')
  const [filterStatus, setFilterStatus] = useState<string>('all')
  const [symbolDirection, setSymbolDirection] = useState<'LONG' | 'SHORT' | null>(null)

  useEffect(() => {
    // 🔥 修複：切换交易對時立即清空舊數據
    setSlots([])
    setLoading(true)
    
    const fetchSlots = async () => {
      try {
        const data = await getSlots(selectedExchange || undefined, selectedSymbol || undefined)
        setSlots(data.slots || [])
        setError(null)
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to fetch slots')
        console.error('Failed to fetch slots:', err)
      } finally {
        setLoading(false)
      }
    }

    fetchSlots()
    // 每5秒刷新一次
    const interval = setInterval(fetchSlots, 5000)

    return () => {
      clearInterval(interval)
      // 🔥 修複：组件卸載時清空數據
      setSlots([])
    }
  }, [selectedExchange, selectedSymbol])

  // 獲取當前交易對的方向（做多/做空）
  useEffect(() => {
    if (!selectedExchange || !selectedSymbol) {
      setSymbolDirection(null)
      return
    }
    const loadDirection = async () => {
      try {
        const res = await getSymbols()
        const sym = res.symbols?.find(
          s => s.exchange?.toLowerCase() === selectedExchange?.toLowerCase() && s.symbol === selectedSymbol
        )
        setSymbolDirection(sym?.direction === 'SHORT' ? 'SHORT' : 'LONG')
      } catch {
        setSymbolDirection('LONG')
      }
    }
    loadDirection()
  }, [selectedExchange, selectedSymbol])

  const sortedSlots = [...slots].sort((a, b) => {
    if (sortBy === 'price') {
      return b.price - a.price // 從高到低
    }
    return a.position_status.localeCompare(b.position_status)
  })

  const filteredSlots = sortedSlots.filter(slot => {
    if (filterStatus === 'all') return true
    if (filterStatus === 'filled') return slot.position_status === 'FILLED'
    if (filterStatus === 'empty') return slot.position_status === 'EMPTY'
    if (filterStatus === 'locked') return slot.slot_status === 'LOCKED'
    return true
  })

  const getStatusColor = (status: string) => {
    switch (status) {
      case 'FILLED':
        return '#52c41a'
      case 'EMPTY':
        return '#8c8c8c'
      default:
        return '#1890ff'
    }
  }

  const getStatusText = (status: string) => {
    switch (status) {
      case 'FILLED':
        return '有倉'
      case 'EMPTY':
        return '空倉'
      default:
        return status
    }
  }

  const getSlotStatusColor = (status: string) => {
    switch (status) {
      case 'FREE':
        return '#52c41a'
      case 'PENDING':
        return '#faad14'
      case 'LOCKED':
        return '#ff4d4f'
      default:
        return '#8c8c8c'
    }
  }

  const getSlotStatusText = (status: string) => {
    switch (status) {
      case 'FREE':
        return '空闲'
      case 'PENDING':
        return '等待'
      case 'LOCKED':
        return '鎖定'
      default:
        return status
    }
  }

  if (loading && slots.length === 0) {
    return (
      <div className="slots">
        <h2>槽位管理</h2>
        <p>加載中...</p>
      </div>
    )
  }

  if (error) {
    return (
      <div className="slots">
        <h2>槽位管理</h2>
        <p style={{ color: 'red' }}>錯误: {error}</p>
      </div>
    )
  }

  return (
    <div className="slots">
      <h2 style={{ display: 'flex', alignItems: 'center', gap: '8px', flexWrap: 'wrap' }}>
        槽位管理 ({slots.length})
        {symbolDirection != null && (
          <span
            style={{
              fontSize: '12px',
              padding: '2px 8px',
              borderRadius: '4px',
              backgroundColor: symbolDirection === 'SHORT' ? '#ed8936' : '#38a169',
              color: '#fff',
              fontWeight: 500,
            }}
          >
            {symbolDirection === 'SHORT' ? t('configuration.directionShort') : t('configuration.directionLong')}
          </span>
        )}
      </h2>
      
      <div style={{ marginBottom: '16px', display: 'flex', gap: '16px', alignItems: 'center' }}>
        <div>
          <label>排序方式: </label>
          <select value={sortBy} onChange={(e) => setSortBy(e.target.value as 'price' | 'status')}>
            <option value="price">按價格</option>
            <option value="status">按状態</option>
          </select>
        </div>
        <div>
          <label>筛选: </label>
          <select value={filterStatus} onChange={(e) => setFilterStatus(e.target.value)}>
            <option value="all">全部</option>
            <option value="filled">有倉</option>
            <option value="empty">空倉</option>
            <option value="locked">已鎖定</option>
          </select>
        </div>
      </div>

      {filteredSlots.length === 0 ? (
        <p>没有符合条件的槽位</p>
      ) : (
        <div style={{ overflowX: 'auto' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', marginTop: '16px' }}>
            <thead>
              <tr style={{ borderBottom: '2px solid #e8e8e8' }}>
                <th style={{ padding: '12px', textAlign: 'left' }}>價格</th>
                <th style={{ padding: '12px', textAlign: 'left' }}>持倉状態</th>
                <th style={{ padding: '12px', textAlign: 'right' }}>持倉數量</th>
                <th style={{ padding: '12px', textAlign: 'left' }}>槽位状態</th>
                <th style={{ padding: '12px', textAlign: 'left' }}>订單方向</th>
                <th style={{ padding: '12px', textAlign: 'left' }}>订單状態</th>
                <th style={{ padding: '12px', textAlign: 'right' }}>订單價格</th>
                <th style={{ padding: '12px', textAlign: 'right' }}>订單ID</th>
              </tr>
            </thead>
            <tbody>
              {filteredSlots.map((slot) => (
                <tr key={slot.price} style={{ borderBottom: '1px solid #f0f0f0' }}>
                  <td style={{ padding: '12px', fontWeight: 'bold' }}>{slot.price.toFixed(2)}</td>
                  <td style={{ padding: '12px', color: getStatusColor(slot.position_status) }}>
                    {getStatusText(slot.position_status)}
                  </td>
                  <td style={{ padding: '12px', textAlign: 'right' }}>{slot.position_qty.toFixed(4)}</td>
                  <td style={{ padding: '12px', color: getSlotStatusColor(slot.slot_status) }}>
                    {getSlotStatusText(slot.slot_status)}
                  </td>
                  <td style={{ padding: '12px', color: slot.order_side === 'BUY' ? '#52c41a' : '#ff4d4f' }}>
                    {slot.order_side || '-'}
                  </td>
                  <td style={{ padding: '12px' }}>{slot.order_status || '-'}</td>
                  <td style={{ padding: '12px', textAlign: 'right' }}>
                    {slot.order_price > 0 ? slot.order_price.toFixed(2) : '-'}
                  </td>
                  <td style={{ padding: '12px', textAlign: 'right' }}>
                    {slot.order_id > 0 ? slot.order_id : '-'}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}

export default Slots

