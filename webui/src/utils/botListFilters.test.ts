import { describe, it, expect } from 'vitest'
import {
  getUniqueExchangesAndSymbols,
  filterBotsByExchangeAndSymbol,
} from './botListFilters'
import type { BotInfo } from '../services/api'

const mockBots: BotInfo[] = [
  { bot_id: '1', name: 'A', exchange: 'binance', symbol: 'BTCUSDT', market_type: 'futures', running: true },
  { bot_id: '2', name: 'B', exchange: 'binance', symbol: 'ETHUSDT', market_type: 'futures', running: false },
  { bot_id: '3', name: 'C', exchange: 'okx', symbol: 'BTCUSDT', market_type: 'spot', running: true },
]

describe('botListFilters', () => {
  describe('getUniqueExchangesAndSymbols', () => {
    it('returns sorted unique exchanges and symbols', () => {
      const { uniqueExchanges, uniqueSymbols } = getUniqueExchangesAndSymbols(mockBots)
      expect(uniqueExchanges).toEqual(['binance', 'okx'])
      expect(uniqueSymbols).toEqual(['BTCUSDT', 'ETHUSDT'])
    })

    it('returns empty arrays for empty bot list', () => {
      const { uniqueExchanges, uniqueSymbols } = getUniqueExchangesAndSymbols([])
      expect(uniqueExchanges).toEqual([])
      expect(uniqueSymbols).toEqual([])
    })
  })

  describe('filterBotsByExchangeAndSymbol', () => {
    it('returns all when no filter applied', () => {
      const result = filterBotsByExchangeAndSymbol(mockBots, '', '')
      expect(result).toHaveLength(3)
    })

    it('filters by exchange', () => {
      const result = filterBotsByExchangeAndSymbol(mockBots, 'binance', '')
      expect(result).toHaveLength(2)
      expect(result.map((b) => b.bot_id)).toEqual(['1', '2'])
    })

    it('filters by symbol', () => {
      const result = filterBotsByExchangeAndSymbol(mockBots, '', 'BTCUSDT')
      expect(result).toHaveLength(2)
      expect(result.map((b) => b.bot_id)).toEqual(['1', '3'])
    })

    it('filters by both exchange and symbol', () => {
      const result = filterBotsByExchangeAndSymbol(mockBots, 'binance', 'BTCUSDT')
      expect(result).toHaveLength(1)
      expect(result[0].bot_id).toBe('1')
    })

    it('returns empty when no match', () => {
      const result = filterBotsByExchangeAndSymbol(mockBots, 'okx', 'ETHUSDT')
      expect(result).toHaveLength(0)
    })
  })
})
