import React, { useEffect, useState } from 'react'
import { getPendingOrders, getOrderHistory, PendingOrderInfo } from '../services/api'

interface OrderInfo {
  order_id: number
  client_order_id: string
  symbol: string
  side: string
  price: number
  quantity: number
  status: string
  created_at: string
  updated_at: string
}

type OrderView = 'pending' | 'history'

const Orders: React.FC = () => {
  const [pendingOrders, setPendingOrders] = useState<PendingOrderInfo[]>([])
  const [historyOrders, setHistoryOrders] = useState<OrderInfo[]>([])
  const [view, setView] = useState<OrderView>('pending')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const fetchPendingOrders = async () => {
      try {
        const data = await getPendingOrders()
        setPendingOrders(data.orders)
      } catch (err) {
        console.error('Failed to fetch pending orders:', err)
      }
    }

    const fetchHistoryOrders = async () => {
      try {
        const data = await getOrderHistory()
        setHistoryOrders(data.orders || [])
      } catch (err) {
        console.error('Failed to fetch history orders:', err)
      }
    }

    const fetchData = async () => {
      setLoading(true)
      await Promise.all([fetchPendingOrders(), view === 'history' && fetchHistoryOrders()])
      setError(null)
      setLoading(false)
    }

    fetchData()
    
    // 待成交订单每5秒刷新一次，历史订单每30秒刷新一次
    const interval = setInterval(() => {
      fetchPendingOrders()
      if (view === 'history') {
        fetchHistoryOrders()
      }
    }, view === 'pending' ? 5000 : 30000)

    return () => clearInterval(interval)
  }, [view])

  const formatTime = (timeStr: string) => {
    try {
      return new Date(timeStr).toLocaleString('zh-CN')
    } catch {
      return timeStr
    }
  }

  const getStatusColor = (status: string) => {
    switch (status) {
      case 'PLACED':
        return '#1890ff'
      case 'CONFIRMED':
        return '#52c41a'
      case 'PARTIALLY_FILLED':
        return '#faad14'
      default:
        return '#8c8c8c'
    }
  }

  const getStatusText = (status: string) => {
    switch (status) {
      case 'PLACED':
        return '已下单'
      case 'CONFIRMED':
        return '已确认'
      case 'PARTIALLY_FILLED':
        return '部分成交'
      case 'FILLED':
        return '已完成'
      case 'CANCELED':
        return '已取消'
      default:
        return status
    }
  }

  const getStatusColorForHistory = (status: string) => {
    switch (status) {
      case 'FILLED':
        return '#52c41a'
      case 'CANCELED':
        return '#8c8c8c'
      default:
        return '#1890ff'
    }
  }

  // 计算订单统计
  const todayOrders = historyOrders.filter(order => {
    const orderDate = new Date(order.created_at)
    const today = new Date()
    return orderDate.toDateString() === today.toDateString()
  })

  const successOrders = historyOrders.filter(order => order.status === 'FILLED').length
  const successRate = historyOrders.length > 0 ? (successOrders / historyOrders.length) * 100 : 0

  if (loading && pendingOrders.length === 0 && historyOrders.length === 0) {
    return (
      <div className="orders">
        <h2>订单管理</h2>
        <p>加载中...</p>
      </div>
    )
  }

  if (error) {
    return (
      <div className="orders">
        <h2>订单管理</h2>
        <p style={{ color: 'red' }}>错误: {error}</p>
      </div>
    )
  }

  return (
    <div className="orders">
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '16px' }}>
        <h2>订单管理</h2>
        <div style={{ display: 'flex', gap: '8px' }}>
          <button
            onClick={() => setView('pending')}
            style={{
              padding: '8px 16px',
              border: '1px solid #e8e8e8',
              borderRadius: '4px',
              background: view === 'pending' ? '#1890ff' : 'white',
              color: view === 'pending' ? 'white' : '#333',
              cursor: 'pointer',
            }}
          >
            待成交 ({pendingOrders.length})
          </button>
          <button
            onClick={() => setView('history')}
            style={{
              padding: '8px 16px',
              border: '1px solid #e8e8e8',
              borderRadius: '4px',
              background: view === 'history' ? '#1890ff' : 'white',
              color: view === 'history' ? 'white' : '#333',
              cursor: 'pointer',
            }}
          >
            历史订单 ({historyOrders.length})
          </button>
        </div>
      </div>

      {/* 订单统计卡片 */}
      {view === 'history' && (
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))', gap: '16px', marginBottom: '16px' }}>
          <div style={{ padding: '16px', border: '1px solid #e8e8e8', borderRadius: '4px' }}>
            <div style={{ fontSize: '14px', color: '#8c8c8c', marginBottom: '8px' }}>今日订单数</div>
            <div style={{ fontSize: '24px', fontWeight: 'bold' }}>{todayOrders.length}</div>
          </div>
          <div style={{ padding: '16px', border: '1px solid #e8e8e8', borderRadius: '4px' }}>
            <div style={{ fontSize: '14px', color: '#8c8c8c', marginBottom: '8px' }}>总订单数</div>
            <div style={{ fontSize: '24px', fontWeight: 'bold' }}>{historyOrders.length}</div>
          </div>
          <div style={{ padding: '16px', border: '1px solid #e8e8e8', borderRadius: '4px' }}>
            <div style={{ fontSize: '14px', color: '#8c8c8c', marginBottom: '8px' }}>成功率</div>
            <div style={{ fontSize: '24px', fontWeight: 'bold' }}>{successRate.toFixed(2)}%</div>
          </div>
          <div style={{ padding: '16px', border: '1px solid #e8e8e8', borderRadius: '4px' }}>
            <div style={{ fontSize: '14px', color: '#8c8c8c', marginBottom: '8px' }}>已完成订单</div>
            <div style={{ fontSize: '24px', fontWeight: 'bold', color: '#52c41a' }}>{successOrders}</div>
          </div>
        </div>
      )}

      {/* 待成交订单 */}
      {view === 'pending' && (
        <>
          {pendingOrders.length === 0 ? (
            <p>暂无待成交订单</p>
          ) : (
            <div style={{ overflowX: 'auto' }}>
              <table style={{ width: '100%', borderCollapse: 'collapse', marginTop: '16px' }}>
                <thead>
                  <tr style={{ borderBottom: '2px solid #e8e8e8' }}>
                    <th style={{ padding: '12px', textAlign: 'left' }}>订单ID</th>
                    <th style={{ padding: '12px', textAlign: 'left' }}>方向</th>
                    <th style={{ padding: '12px', textAlign: 'right' }}>价格</th>
                    <th style={{ padding: '12px', textAlign: 'right' }}>数量</th>
                    <th style={{ padding: '12px', textAlign: 'right' }}>已成交</th>
                    <th style={{ padding: '12px', textAlign: 'left' }}>状态</th>
                    <th style={{ padding: '12px', textAlign: 'left' }}>槽位价格</th>
                    <th style={{ padding: '12px', textAlign: 'left' }}>创建时间</th>
                  </tr>
                </thead>
                <tbody>
                  {pendingOrders.map((order) => (
                    <tr key={order.order_id} style={{ borderBottom: '1px solid #f0f0f0' }}>
                      <td style={{ padding: '12px' }}>{order.order_id}</td>
                      <td style={{ padding: '12px', color: order.side === 'BUY' ? '#52c41a' : '#ff4d4f' }}>
                        {order.side === 'BUY' ? '买入' : '卖出'}
                      </td>
                      <td style={{ padding: '12px', textAlign: 'right' }}>{order.price.toFixed(2)}</td>
                      <td style={{ padding: '12px', textAlign: 'right' }}>{order.quantity.toFixed(4)}</td>
                      <td style={{ padding: '12px', textAlign: 'right' }}>{order.filled_quantity.toFixed(4)}</td>
                      <td style={{ padding: '12px', color: getStatusColor(order.status) }}>
                        {getStatusText(order.status)}
                      </td>
                      <td style={{ padding: '12px' }}>{order.slot_price.toFixed(2)}</td>
                      <td style={{ padding: '12px' }}>{formatTime(order.created_at)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </>
      )}

      {/* 历史订单 */}
      {view === 'history' && (
        <>
          {historyOrders.length === 0 ? (
            <p>暂无历史订单</p>
          ) : (
            <div style={{ overflowX: 'auto' }}>
              <table style={{ width: '100%', borderCollapse: 'collapse', marginTop: '16px' }}>
                <thead>
                  <tr style={{ borderBottom: '2px solid #e8e8e8' }}>
                    <th style={{ padding: '12px', textAlign: 'left' }}>订单ID</th>
                    <th style={{ padding: '12px', textAlign: 'left' }}>方向</th>
                    <th style={{ padding: '12px', textAlign: 'right' }}>价格</th>
                    <th style={{ padding: '12px', textAlign: 'right' }}>数量</th>
                    <th style={{ padding: '12px', textAlign: 'left' }}>状态</th>
                    <th style={{ padding: '12px', textAlign: 'left' }}>创建时间</th>
                    <th style={{ padding: '12px', textAlign: 'left' }}>更新时间</th>
                  </tr>
                </thead>
                <tbody>
                  {historyOrders.map((order) => (
                    <tr key={order.order_id} style={{ borderBottom: '1px solid #f0f0f0' }}>
                      <td style={{ padding: '12px' }}>{order.order_id}</td>
                      <td style={{ padding: '12px', color: order.side === 'BUY' ? '#52c41a' : '#ff4d4f' }}>
                        {order.side === 'BUY' ? '买入' : '卖出'}
                      </td>
                      <td style={{ padding: '12px', textAlign: 'right' }}>{order.price.toFixed(2)}</td>
                      <td style={{ padding: '12px', textAlign: 'right' }}>{order.quantity.toFixed(4)}</td>
                      <td style={{ padding: '12px', color: getStatusColorForHistory(order.status) }}>
                        {getStatusText(order.status)}
                      </td>
                      <td style={{ padding: '12px' }}>{formatTime(order.created_at)}</td>
                      <td style={{ padding: '12px' }}>{formatTime(order.updated_at)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </>
      )}
    </div>
  )
}

export default Orders
