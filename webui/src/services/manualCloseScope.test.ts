import { afterEach, expect, it, vi } from 'vitest'

afterEach(() => vi.unstubAllGlobals())

it('manual close facade forwards encoded market/Bot scope without retry', async () => {
  vi.resetModules()
  vi.stubGlobal('window', { location: { origin: 'https://example.test', pathname: '/config' } })
  vi.stubGlobal('localStorage', { getItem: () => 'zh-CN' })
  const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ success_count: 1, fail_count: 0 })))
  vi.stubGlobal('fetch', fetchMock)
  const { closeAllPositions } = await import('./api')
  await closeAllPositions('binance', 'BTCUSDT', 'spot_margin', 'bot/one')
  expect(fetchMock).toHaveBeenCalledExactlyOnceWith('https://example.test/api/trading/close-positions?exchange=binance&symbol=BTCUSDT&market_type=spot_margin&bot_id=bot%2Fone', {
    method: 'POST', credentials: 'include', headers: { 'Content-Type': 'application/json', 'Accept-Language': 'zh-CN' },
  })
})
