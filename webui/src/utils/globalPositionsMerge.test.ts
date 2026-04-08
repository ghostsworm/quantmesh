import { describe, expect, it } from 'vitest'
import {
  mergePositionRowsForRefresh,
  positionRowKey,
  type PositionRowData,
} from './globalPositionsMerge'
import type { PendingOrderInfo, PositionSummaryItem } from '../services/api'

function row(
  exchange: string,
  symbol: string,
  extra?: Partial<PositionRowData>
): PositionRowData {
  return {
    exchange,
    symbol,
    market_type: 'futures',
    strategy: 'g',
    total_quantity: 1,
    total_value: 100,
    position_count: 1,
    average_price: 1,
    current_price: 1,
    unrealized_pnl: 0,
    pnl_percentage: 0,
    actual_margin: 0,
    leverage: 1,
    openOrders: [],
    closeOrders: [],
    ordersLoading: false,
    ordersLoaded: false,
    ...extra,
  }
}

describe('positionRowKey', () => {
  it('normalizes casing for stable keys', () => {
    expect(
      positionRowKey({ exchange: 'Binance', symbol: 'BTCUSDT', market_type: 'FUTURES' })
    ).toBe('binance:btcusdt:futures')
    expect(positionRowKey({ exchange: 'binance', symbol: 'btcusdt' })).toBe(
      'binance:btcusdt:futures'
    )
  })
})

describe('mergePositionRowsForRefresh', () => {
  it('preserves order cache when summary is refreshed', () => {
    const o: PendingOrderInfo = {
      order_id: 1,
      client_order_id: '',
      symbol: 'BTCUSDT',
      side: 'BUY',
      price: 0,
      quantity: 0,
      filled_quantity: 0,
      status: 'NEW',
      created_at: '',
      slot_price: 0,
      strategy_name: '',
      strategy_type: '',
    }
    const prev: PositionRowData[] = [
      row('binance', 'BTCUSDT', {
        openOrders: [o],
        closeOrders: [],
        ordersLoaded: true,
        ordersLoading: false,
      }),
    ]
    const incoming: PositionSummaryItem[] = [
      {
        exchange: 'binance',
        symbol: 'btcusdt',
        market_type: 'futures',
        strategy: 'g',
        total_quantity: 1,
        total_value: 200,
        position_count: 1,
        average_price: 1,
        current_price: 2,
        unrealized_pnl: 5,
        pnl_percentage: 1,
        actual_margin: 0,
        leverage: 1,
      },
    ]
    const next = mergePositionRowsForRefresh(prev, incoming)
    expect(next).toHaveLength(1)
    expect(next[0].total_value).toBe(200)
    expect(next[0].unrealized_pnl).toBe(5)
    expect(next[0].ordersLoaded).toBe(true)
    expect(next[0].openOrders).toHaveLength(1)
    expect(next[0].openOrders[0].order_id).toBe(1)
  })

  it('initializes empty order state for new symbols', () => {
    const prev: PositionRowData[] = []
    const incoming: PositionSummaryItem[] = [
      {
        exchange: 'okx',
        symbol: 'ETHUSDT',
        market_type: 'futures',
        strategy: 'g',
        total_quantity: 1,
        total_value: 100,
        position_count: 1,
        average_price: 1,
        current_price: 1,
        unrealized_pnl: 0,
        pnl_percentage: 0,
        actual_margin: 0,
        leverage: 1,
      },
    ]
    const next = mergePositionRowsForRefresh(prev, incoming)
    expect(next[0].openOrders).toEqual([])
    expect(next[0].ordersLoaded).toBe(false)
  })
})
