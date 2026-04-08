import type { PendingOrderInfo, PositionSummaryItem } from '../services/api'

/** 全局持倉表格行：接口數據 + 懶加載的委託緩存 */
export interface PositionRowData extends PositionSummaryItem {
  openOrders: PendingOrderInfo[]
  closeOrders: PendingOrderInfo[]
  ordersLoading: boolean
  ordersLoaded: boolean
}

/**
 * 穩定行鍵：後端有時返回大小寫不一致的 exchange/symbol，定時刷新必須用同一規則，
 * 否則展開態與委託緩存會對不上。
 */
export function positionRowKey(p: {
  exchange: string
  symbol: string
  market_type?: string
}): string {
  const mt = (p.market_type || 'futures').toLowerCase()
  return `${String(p.exchange).toLowerCase()}:${String(p.symbol).toLowerCase()}:${mt}`
}

/**
 * 定時拉取持倉摘要時合併舊行：保留已展開行懶加載的 open/close 委託與加載狀態，
 * 避免每 10s 刷新把展開面板「洗空」或誤判為未加載。
 */
export function mergePositionRowsForRefresh(
  prev: PositionRowData[],
  incoming: PositionSummaryItem[]
): PositionRowData[] {
  const prevByKey = new Map(prev.map(r => [positionRowKey(r), r]))
  return (incoming || []).map(p => {
    const old = prevByKey.get(positionRowKey(p))
    if (old) {
      return {
        ...p,
        openOrders: old.openOrders,
        closeOrders: old.closeOrders,
        ordersLoading: old.ordersLoading,
        ordersLoaded: old.ordersLoaded,
      }
    }
    return {
      ...p,
      openOrders: [],
      closeOrders: [],
      ordersLoading: false,
      ordersLoaded: false,
    }
  })
}
