import { afterEach, describe, expect, it, vi } from 'vitest'

vi.mock('./api', () => ({ fetchWithAuth: vi.fn() }))
afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

describe('daily latest request', () => {
  it('asks the server for the latest closed day without sending an account scope', async () => {
    vi.stubGlobal('window', { location: { origin: 'https://example.test' } })
    const { getDailyLatest } = await import('./profit')
    const { fetchWithAuth } = await import('./api')
    vi.mocked(fetchWithAuth).mockResolvedValueOnce({ status: 'unpublished', date: '2026-11-11', exchangeId: 'binance' })
    await getDailyLatest('binance')
    const url = String(vi.mocked(fetchWithAuth).mock.calls[0][0])
    expect(url).toBe('https://example.test/api/profit/daily-reconciliation/latest?exchange_id=binance')
    expect(url).not.toContain('account_scope')
  })
})
