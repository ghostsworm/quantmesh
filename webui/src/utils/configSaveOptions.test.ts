import { describe, it, expect } from 'vitest'
import { hasTradingParamChanges } from './configSaveOptions'
import type { ConfigDiff } from '../services/config'

describe('hasTradingParamChanges', () => {
  it('returns true when trading.symbols changed', () => {
    const diff: ConfigDiff = {
      changes: [{ path: 'trading.symbols.0.price_interval', type: 'modified', old_value: 10, new_value: 20, requires_restart: false }],
      requires_restart: false,
    }
    expect(hasTradingParamChanges(diff)).toBe(true)
  })

  it('returns true when trading.direction changed', () => {
    const diff: ConfigDiff = {
      changes: [{ path: 'trading.direction', type: 'modified', old_value: 'LONG', new_value: 'SHORT', requires_restart: false }],
      requires_restart: false,
    }
    expect(hasTradingParamChanges(diff)).toBe(true)
  })

  it('returns false when only exchanges.api_key changed', () => {
    const diff: ConfigDiff = {
      changes: [{ path: 'exchanges.binance.api_key', type: 'modified', old_value: 'xxx', new_value: 'yyy', requires_restart: false }],
      requires_restart: false,
    }
    expect(hasTradingParamChanges(diff)).toBe(false)
  })

  it('returns false when only app.current_exchange changed', () => {
    const diff: ConfigDiff = {
      changes: [{ path: 'app.current_exchange', type: 'modified', old_value: 'binance', new_value: 'okx', requires_restart: false }],
      requires_restart: false,
    }
    expect(hasTradingParamChanges(diff)).toBe(false)
  })

  it('returns false when only ai.enabled changed', () => {
    const diff: ConfigDiff = {
      changes: [{ path: 'ai.enabled', type: 'modified', old_value: false, new_value: true, requires_restart: false }],
      requires_restart: false,
    }
    expect(hasTradingParamChanges(diff)).toBe(false)
  })

  it('returns true when both exchange and trading changed', () => {
    const diff: ConfigDiff = {
      changes: [
        { path: 'exchanges.binance.api_key', type: 'modified', old_value: 'a', new_value: 'b', requires_restart: false },
        { path: 'trading.symbols.0.order_quantity', type: 'modified', old_value: 0.001, new_value: 0.002, requires_restart: false },
      ],
      requires_restart: false,
    }
    expect(hasTradingParamChanges(diff)).toBe(true)
  })

  it('returns false when changes array is empty', () => {
    const diff: ConfigDiff = { changes: [], requires_restart: false }
    expect(hasTradingParamChanges(diff)).toBe(false)
  })
})
