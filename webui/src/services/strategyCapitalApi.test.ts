import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

beforeEach(() => {
  vi.resetModules()
  vi.stubGlobal('window', { location: { origin: 'https://example.test', pathname: '/bots', replace: vi.fn() } })
  vi.stubGlobal('localStorage', { getItem: vi.fn(() => 'zh-CN') })
})
afterEach(() => vi.unstubAllGlobals())

const conflict = (released: unknown, total: number) => ({
  success: false, error: 'capital_release_unverified', released, total_released: total,
  partial: total > 0, requires_reconciliation: true, message: 'internal details must not drive UI copy',
})
const respond = (body: unknown, status = 409) => {
  const fetch = vi.fn(async () => new Response(JSON.stringify(body), { status }))
  vi.stubGlobal('fetch', fetch)
  return fetch
}

describe('strategy capital release transport contract', () => {
  it('preserves single-strategy partial release and authentication through the public facade', async () => {
    const fetch = respond(conflict(200, 200))
    const api = await import('./api')
    const capital = await import('./strategyCapitalApi')
    expect(api.releaseStrategyCapital).toBe(capital.releaseStrategyCapital)
    await expect(api.releaseStrategyCapital('dca/one')).resolves.toMatchObject({ success: false, partial: true, released: 200, total_released: 200 })
    expect(fetch).toHaveBeenCalledWith('https://example.test/api/strategies/dca%2Fone/release-capital', expect.objectContaining({
      method: 'POST', credentials: 'include', headers: expect.objectContaining({ 'Accept-Language': 'zh-CN' }),
    }))
  })
  it('retains per-strategy amounts on a partial all-strategy release', async () => {
    respond(conflict({ dca: 200, momentum: 10 }, 210))
    const { releaseAllStrategiesCapital } = await import('./strategyCapitalApi')
    await expect(releaseAllStrategiesCapital()).resolves.toMatchObject({ success: false, partial: true, released: { dca: 200, momentum: 10 }, total_released: 210 })
  })
  it('accepts explicit zero-release refusal without treating it as success', async () => {
    respond(conflict(0, 0))
    const { releaseStrategyCapital } = await import('./strategyCapitalApi')
    await expect(releaseStrategyCapital('dca')).resolves.toMatchObject({ success: false, partial: false, total_released: 0 })
  })
  it('keeps successful legacy wire shapes compatible', async () => {
    respond({ success: true, released: 200, strategy: 'dca' }, 200)
    const { releaseStrategyCapital, releaseAllStrategiesCapital } = await import('./strategyCapitalApi')
    await expect(releaseStrategyCapital('dca')).resolves.toMatchObject({ success: true, released: 200, total_released: 200 })
    respond({ success: true, released: { dca: 200 }, total_released: 200 }, 200)
    await expect(releaseAllStrategiesCapital()).resolves.toMatchObject({ success: true, total_released: 200 })
  })
  it.each([
    conflict({ dca: -1 }, -1), conflict({ dca: 200 }, 0), conflict({ dca: '200' }, 200),
    { ...conflict({ dca: 200 }, 200), partial: false },
    { ...conflict({ dca: 0 }, 0), requires_reconciliation: false },
    { ...conflict({ dca: 0 }, 0), released: null },
  ])('rejects invalid amount/partial contracts instead of inventing certainty', async (body) => {
    respond(body)
    const { releaseAllStrategiesCapital } = await import('./strategyCapitalApi')
    await expect(releaseAllStrategiesCapital()).rejects.toThrow('capital_release_contract_invalid')
  })
  it('keeps non-contract server errors and network interruption uncertain', async () => {
    respond({ success: false, error: 'capital_release_result_unverified' }, 500)
    const { releaseStrategyCapital } = await import('./strategyCapitalApi')
    await expect(releaseStrategyCapital('dca')).rejects.toMatchObject({ status: 500 })
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('network interrupted')))
    await expect(releaseStrategyCapital('dca')).rejects.toThrow('network interrupted')
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new DOMException('request cancelled', 'AbortError')))
    await expect(releaseStrategyCapital('dca')).rejects.toMatchObject({ name: 'AbortError' })
  })
  it('never reports a conflict claiming success as a successful release', async () => {
    respond({ ...conflict(200, 200), success: true })
    const { releaseStrategyCapital } = await import('./strategyCapitalApi')
    await expect(releaseStrategyCapital('dca')).rejects.toMatchObject({ status: 409 })
  })
})
