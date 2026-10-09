import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

beforeEach(() => {
  vi.resetModules()
  vi.stubGlobal('window', { location: { origin: 'https://example.test', pathname: '/bots', replace: vi.fn() } })
  vi.stubGlobal('localStorage', { getItem: vi.fn(() => 'en-US') })
})
afterEach(() => vi.unstubAllGlobals())

describe('API module boundaries', () => {
  it('keeps the public facade and shared authenticated transport identical', async () => {
    const api = await import('./api')
    const bot = await import('./botApi')
    const transport = await import('./apiTransport')
    expect(api.fetchWithAuth).toBe(transport.fetchWithAuth)
    expect(api.getBotPositionStatus).toBe(bot.getBotPositionStatus)
    expect(api.updateBotRiskControl).toBe(bot.updateBotRiskControl)
    const fetch = vi.fn(async () => new Response(JSON.stringify({ should_stop_opening: true }), { status: 200 }))
    vi.stubGlobal('fetch', fetch)
    expect(await api.getBotPositionStatus('bot/one')).toEqual({ should_stop_opening: true })
    expect(fetch).toHaveBeenCalledWith('https://example.test/api/v2/bots/bot%2Fone/position-status', expect.objectContaining({
      credentials: 'include', headers: expect.objectContaining({ 'Accept-Language': 'en-US' }),
    }))
  })
  it('retains structured errors and authentication handling', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ error: 'denied', code: 'blocked', error_key: 'risk.denied' }), { status: 401 })))
    const { fetchWithAuth } = await import('./apiTransport')
    await expect(fetchWithAuth('/api/test')).rejects.toMatchObject({ status: 401, code: 'blocked', errorKey: 'risk.denied' })
    expect(window.location.replace).toHaveBeenCalledWith('/login')
  })

  it('routes retired-account reset reads and writes through authenticated same-origin transport', async () => {
    const api = await import('./api')
    const fetch = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({ accounts: [] }), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ history: [] }), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ operation: { id: 'op-1' }, status: 'completed' }), { status: 200 }))
    vi.stubGlobal('fetch', fetch)

    expect(await api.getRetiredEquityAccounts()).toEqual({ accounts: [] })
    expect(await api.getRetiredEquityResetHistory()).toEqual({ history: [] })
    await api.resetRetiredEquityAccounts()

    expect(fetch).toHaveBeenNthCalledWith(1, 'https://example.test/api/capital/retired-equity-accounts', expect.objectContaining({
      credentials: 'include', headers: expect.objectContaining({ 'Accept-Language': 'en-US' }),
    }))
    expect(fetch).toHaveBeenNthCalledWith(2, 'https://example.test/api/capital/retired-equity-accounts/reset-history', expect.objectContaining({
      credentials: 'include', headers: expect.objectContaining({ 'Accept-Language': 'en-US' }),
    }))
    expect(fetch).toHaveBeenNthCalledWith(3, 'https://example.test/api/capital/retired-equity-accounts/reset', expect.objectContaining({
      method: 'POST', body: '{}', credentials: 'include',
    }))
  })
})
