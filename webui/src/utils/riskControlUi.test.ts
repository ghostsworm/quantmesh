import { describe, it, expect } from 'vitest'
import { parseMonitorSymbolsInput } from './riskControlUi'

describe('parseMonitorSymbolsInput', () => {
  it('splits comma and Chinese comma', () => {
    expect(parseMonitorSymbolsInput('BTCUSDT，ETHUSDT')).toEqual(['BTCUSDT', 'ETHUSDT'])
  })

  it('trims and uppercases', () => {
    expect(parseMonitorSymbolsInput(' btcusdt , solusdt ')).toEqual(['BTCUSDT', 'SOLUSDT'])
  })

  it('handles newlines', () => {
    expect(parseMonitorSymbolsInput('BTCUSDT\nETHUSDT')).toEqual(['BTCUSDT', 'ETHUSDT'])
  })

  it('returns empty for empty input', () => {
    expect(parseMonitorSymbolsInput('   ')).toEqual([])
  })
})
