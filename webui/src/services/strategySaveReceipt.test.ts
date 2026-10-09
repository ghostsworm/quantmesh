import { afterEach, describe, expect, it, vi } from 'vitest'
import { withStrategySave } from '../i18n/strategySave'

afterEach(() => vi.unstubAllGlobals())

async function fixture(status = 200, override: Record<string, unknown> = {}) {
  vi.resetModules()
  vi.stubGlobal('window', { location: { origin: 'https://example.test', pathname: '/bots', replace: vi.fn() } })
  vi.stubGlobal('localStorage', { getItem: () => 'zh-CN' })
  const body = {
    bot_id: 'bot/1', config_saved: true, ok: true,
    runtime_update: { verified: true, applied: ['bot/1'], not_running: [], failed: {} },
    ...override,
  }
  const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status }))
  vi.stubGlobal('fetch', fetchMock)
  const api = await import('./api')
  const helper = await import('./strategySaveReceipt')
  return { api, helper, body, fetchMock }
}

describe('actual strategy save API receipt', () => {
  it('uses authenticated same-origin PUT and only verified target application succeeds', async () => {
    const { api, fetchMock, helper } = await fixture()
    const receipt = await api.updateBotStrategy('bot/1', {})
    expect(receipt).toEqual({ saved: true, state: 'applied' })
    expect(helper.strategyReceiptFeedback(receipt).status).toBe('success')
    expect(fetchMock).toHaveBeenCalledExactlyOnceWith('https://example.test/api/bots/bot%2F1/strategy', {
      method: 'PUT', body: '{}', credentials: 'include',
      headers: { 'Content-Type': 'application/json', 'Accept-Language': 'zh-CN' },
    })
  })

  it('consumes only a same-bot saved application conflict without retry or raw provider text', async () => {
    const { api, fetchMock, helper } = await fixture(409, {
      ok: false, error: 'runtime_configuration_apply_failed',
      runtime_update: { verified: true, applied: [], not_running: [], failed: { 'bot/1': 'private provider details' } },
    })
    const receipt = await api.updateBotStrategy('bot/1', {})
    expect(receipt).toEqual({ saved: true, state: 'failed' })
    expect(helper.strategyReceiptFeedback(receipt)).toEqual({ title: 'strategySave.failed', status: 'warning' })
    expect(JSON.stringify(receipt)).not.toContain('private')
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it.each([
    { bot_id: 'different' }, { config_saved: false }, { error: 'bot_configuration_changed' },
  ])('does not resolve unrelated/rejected conflicts: %j', async override => {
    const { api } = await fixture(409, { error: 'runtime_configuration_apply_failed', ...override })
    await expect(api.updateBotStrategy('bot/1', {})).rejects.toMatchObject({ status: 409 })
  })

  it.each([
    { verified: false, applied: ['bot/1'], not_running: [], failed: {} },
    { verified: true, applied: ['other'], not_running: [], failed: {} },
    { verified: true, applied: ['bot/1'], not_running: ['bot/1'], failed: {} },
    { verified: true, applied: ['bot/1', 'bot/1'], not_running: [], failed: {} },
    { verified: true, applied: ['bot/1'], not_running: [], failed: null },
    { verified: true, applied: [null], not_running: [], failed: {} },
    null,
  ])('malformed or insufficient runtime evidence never succeeds: %j', async report => {
    const { api, helper } = await fixture(200, { runtime_update: report })
    const receipt = await api.updateBotStrategy('bot/1', {})
    expect(receipt).toEqual({ saved: true, state: 'unverified' })
    expect(helper.strategyReceiptFeedback(receipt).status).toBe('warning')
  })

  it('stopped target is saved, not successfully applied', async () => {
    const { api } = await fixture(200, { runtime_update: { verified: true, applied: [], not_running: ['bot/1'], failed: {} } })
    expect(await api.updateBotStrategy('bot/1', {})).toEqual({ saved: true, state: 'notRunning' })
  })

  it('legacy 200 success does not prove persistence or runtime application', async () => {
    const { api } = await fixture(200, { config_saved: undefined, runtime_update: undefined })
    expect(await api.updateBotStrategy('bot/1', {})).toEqual({ saved: false, state: 'unverified' })
  })

  it('network failure stays uncertain and is not retried', async () => {
    const { api, fetchMock } = await fixture()
    fetchMock.mockRejectedValue(new TypeError('connection interrupted'))
    await expect(api.updateBotStrategy('bot/1', {})).rejects.toThrow('connection interrupted')
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it.each(['zh-CN', 'zh-TW', 'en-US', 'ja-JP'])('all feedback keys resolve through localized resources: %s', async lang => {
    const { helper } = await fixture()
    const resource = withStrategySave(lang, {}).strategySave
    for (const state of ['applied', 'failed', 'notRunning', 'unverified'] as const) {
      const key = helper.strategyReceiptFeedback({ saved: true, state }).title.split('.')[1]
      expect(resource[key as keyof typeof resource]).toBeTruthy()
    }
    expect(resource.unknownSave).toBeTruthy()
  })
})
