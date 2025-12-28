import React, { useEffect, useState } from 'react'
import { useSymbol } from '../contexts/SymbolContext'
import { getSlots, SlotInfo } from '../services/api'

const Slots: React.FC = () => {
  const { selectedExchange, selectedSymbol } = useSymbol()
  const [slots, setSlots] = useState<SlotInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [sortBy, setSortBy] = useState<'price' | 'status'>('price')
  const [filterStatus, setFilterStatus] = useState<string>('all')

  useEffect(() => {
    // 🔥 修复：切换交易对时立即清空旧数据
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
      // 🔥 修复：组件卸载时清空数据
      setSlots([])
    }
  }, [selectedExchange, selectedSymbol])

  const sortedSlots = [...slots].sort((a, b) => {
    if (sortBy === 'price') {
      return b.price - a.price // 从高到低
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
        return '有仓'
      case 'EMPTY':
        return '空仓'
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
        return '锁定'
      default:
        return status
    }
  }

  if (loading && slots.length === 0) {
    return (
      <div className="slots">
        <h2>槽位管理</h2>
        <p>加载中...</p>
      </div>
    )
  }

  if (error) {
    return (
      <div className="slots">
        <h2>槽位管理</h2>
        <p style={{ color: 'red' }}>错误: {error}</p>
      </div>
    )
  }

  return (
    <div className="slots">
      <h2>槽位管理 ({slots.length})</h2>
      
      <div style={{ marginBottom: '16px', display: 'flex', gap: '16px', alignItems: 'center' }}>
        <div>
          <label>排序方式: </label>
          <select value={sortBy} onChange={(e) => setSortBy(e.target.value as 'price' | 'status')}>
            <option value="price">按价格</option>
            <option value="status">按状态</option>
          </select>
        </div>
        <div>
          <label>筛选: </label>
          <select value={filterStatus} onChange={(e) => setFilterStatus(e.target.value)}>
            <option value="all">全部</option>
            <option value="filled">有仓</option>
            <option value="empty">空仓</option>
            <option value="locked">已锁定</option>
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
                <th style={{ padding: '12px', textAlign: 'left' }}>价格</th>
                <th style={{ padding: '12px', textAlign: 'left' }}>持仓状态</th>
                <th style={{ padding: '12px', textAlign: 'right' }}>持仓数量</th>
                <th style={{ padding: '12px', textAlign: 'left' }}>槽位状态</th>
                <th style={{ padding: '12px', textAlign: 'left' }}>订单方向</th>
                <th style={{ padding: '12px', textAlign: 'left' }}>订单状态</th>
                <th style={{ padding: '12px', textAlign: 'right' }}>订单价格</th>
                <th style={{ padding: '12px', textAlign: 'right' }}>订单ID</th>
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

