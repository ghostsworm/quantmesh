import { describe, it, expect } from 'vitest'
import { isExchangeApiSlotVisuallyEmpty } from './exchangeConfigUi'

describe('isExchangeApiSlotVisuallyEmpty', () => {
  it('returns true when exchanges missing or slot missing', () => {
    expect(isExchangeApiSlotVisuallyEmpty(null, 'binance')).toBe(true)
    expect(isExchangeApiSlotVisuallyEmpty({}, 'binance')).toBe(true)
    expect(isExchangeApiSlotVisuallyEmpty({ exchanges: {} }, 'binance')).toBe(true)
  })

  it('returns false when api_key or secret_key present', () => {
    expect(
      isExchangeApiSlotVisuallyEmpty(
        { exchanges: { binance: { api_key: 'a', secret_key: '' } } },
        'binance'
      )
    ).toBe(false)
    expect(
      isExchangeApiSlotVisuallyEmpty(
        { exchanges: { binance: { api_key: '', secret_key: 's' } } },
        'binance'
      )
    ).toBe(false)
  })

  it('returns false for passphrase exchanges when passphrase set', () => {
    expect(
      isExchangeApiSlotVisuallyEmpty(
        { exchanges: { okx: { passphrase: 'p' } } },
        'okx'
      )
    ).toBe(false)
  })

  it('returns false when testnet or non-zero fee_rate', () => {
    expect(
      isExchangeApiSlotVisuallyEmpty(
        { exchanges: { binance: { testnet: true } } },
        'binance'
      )
    ).toBe(false)
    expect(
      isExchangeApiSlotVisuallyEmpty(
        { exchanges: { binance: { fee_rate: 0.0004 } } },
        'binance'
      )
    ).toBe(false)
  })

  it('returns true when only zero fee_rate and defaults', () => {
    expect(
      isExchangeApiSlotVisuallyEmpty(
        { exchanges: { binance: { fee_rate: 0 } } },
        'binance'
      )
    ).toBe(true)
  })
})
