import { afterEach, describe, expect, it, vi } from 'vitest'
import { readConfigSaveReceipt, configReceiptFeedback, startAfterVerifiedConfigSave } from './configSaveReceipt'
import { withConfigSave } from '../i18n/configSave'

vi.hoisted(() => {
  vi.stubGlobal('window', { location: { origin: 'https://example.test' } })
})

afterEach(() => vi.unstubAllGlobals())

const appliedBody = {
  ok: true, config_saved: true, requires_restart: false,
  runtime_update: { verified: true, applied: ['target'], failed: {}, not_running: [] },
}

async function fixture(status: number, body: unknown) {
  vi.resetModules()
  vi.stubGlobal('window', { location: { origin: 'https://example.test', pathname: '/config', replace: vi.fn() } })
  vi.stubGlobal('localStorage', { getItem: () => 'zh-TW' })
  const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status }))
  vi.stubGlobal('fetch', fetchMock)
  return { api: await import('./api'), config: await import('./config'), fetchMock }
}

describe('configuration save receipts and automatic start', () => {
  it.each(['api', 'config'] as const)('actual %s JSON facade sends authenticated same-origin request', async facade => {
    const { api, config, fetchMock } = await fixture(200, appliedBody)
    const receipt = facade === 'api' ? await api.updateConfig({}) : await config.updateConfig({} as import('./config').Config)
    expect(receipt).toEqual({ saved: true, state: 'applied', requires_restart: false, applied: ['target'] })
    expect(fetchMock).toHaveBeenCalledExactlyOnceWith('https://example.test/api/config/update', {
      method: 'POST', body: '{}', credentials: 'include',
      headers: { 'Content-Type': 'application/json', 'Accept-Language': 'zh-TW' },
    })
  })

  it('actual YAML facade preserves raw body and language, and does not claim runtime success', async () => {
    const { config, fetchMock } = await fixture(200, { config_saved: true, ok: false, runtime_update_verified: false, requires_restart: false })
    expect(await config.updateConfigYAML('test: true')).toEqual({ saved: true, state: 'unverified', requires_restart: false, applied: [] })
    expect(fetchMock).toHaveBeenCalledExactlyOnceWith('https://example.test/api/config/update-yaml', {
      method: 'POST', body: 'test: true', credentials: 'include',
      headers: { 'Content-Type': 'application/x-yaml', 'Accept-Language': 'zh-TW' },
    })
  })

  it.each(['json', 'yaml'])('saved %s application conflict warns without retry', async format => {
    const { config, fetchMock } = await fixture(409, { config_saved: true, ok: false, error: 'runtime_configuration_apply_failed', hot_reload_failed: true })
    const receipt = format === 'json' ? await config.updateConfig({} as import('./config').Config) : await config.updateConfigYAML('test: true')
    expect(receipt.saved).toBe(true)
    expect(configReceiptFeedback(receipt)).toEqual({ title: 'configSave.failed', status: 'warning' })
    const start = vi.fn()
    expect(await startAfterVerifiedConfigSave(receipt, 'target', start)).toBe(false)
    expect(start).not.toHaveBeenCalled()
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it.each([
    { config_saved: false, error: 'runtime_configuration_apply_failed' },
    { config_saved: true, error: 'configuration_changed' },
  ])('unrelated/rejected 409 stays rejected: %j', async body => {
    const { config } = await fixture(409, body)
    await expect(config.updateConfigYAML('test: true')).rejects.toMatchObject({ status: 409 })
  })

  it.each([
    { ...appliedBody, runtime_update: null },
    { ...appliedBody, ok: false },
    { ...appliedBody, requires_restart: undefined },
    { ...appliedBody, runtime_update: { verified: false, applied: ['target'], failed: {}, not_running: [] } },
    { ...appliedBody, runtime_update: { verified: true, applied: ['target'], failed: {}, not_running: ['target'] } },
    { ...appliedBody, runtime_update: { verified: true, applied: [null], failed: {}, not_running: [] } },
    { ok: true },
  ])('unknown/malformed evidence cannot start: %j', async body => {
    const receipt = readConfigSaveReceipt(body)
    const start = vi.fn()
    expect(configReceiptFeedback(receipt).status).toBe('warning')
    expect(await startAfterVerifiedConfigSave(receipt, 'target', start)).toBe(false)
    expect(start).not.toHaveBeenCalled()
  })

  it.each([
    { ...appliedBody, requires_restart: true },
    { ...appliedBody, runtime_update: { verified: true, applied: [], failed: {}, not_running: ['target'] } },
    { ...appliedBody, runtime_update: { verified: true, applied: ['other'], failed: {}, not_running: [] } },
    { ...appliedBody, runtime_update: { verified: true, applied: ['target'], failed: { other: 'risk_apply_failed' }, not_running: [] } },
  ])('restart/stopped/wrong target/partial failure cannot auto-start: %j', async body => {
    const start = vi.fn()
    expect(await startAfterVerifiedConfigSave(readConfigSaveReceipt(body), 'target', start)).toBe(false)
    expect(start).not.toHaveBeenCalled()
  })

  it('requires explicit target identity, invokes start once, and propagates its failure', async () => {
    const receipt = readConfigSaveReceipt(appliedBody)
    const start = vi.fn().mockResolvedValue(undefined)
    expect(await startAfterVerifiedConfigSave(receipt, undefined, start)).toBe(false)
    expect(await startAfterVerifiedConfigSave(receipt, 'target', start)).toBe(true)
    expect(start).toHaveBeenCalledTimes(1)
    start.mockRejectedValue(new Error('start denied'))
    await expect(startAfterVerifiedConfigSave(receipt, 'target', start)).rejects.toThrow('start denied')
    expect(start).toHaveBeenCalledTimes(2)
  })

  it('transport interruption does not retry or manufacture saved state', async () => {
    const { config, fetchMock } = await fixture(200, appliedBody)
    fetchMock.mockRejectedValue(new TypeError('interrupted'))
    await expect(config.updateConfigYAML('test: true')).rejects.toThrow('interrupted')
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it.each(['zh-CN', 'zh-TW', 'en-US', 'ja-JP'])('feedback has localized resources: %s', language => {
    const texts = withConfigSave(language, {}).configSave
    for (const state of ['applied', 'failed', 'unverified', 'notRunning', 'restartRequired'] as const) {
      const key = configReceiptFeedback({ saved: true, state, requires_restart: false, applied: [] }).title.split('.')[1]
      expect(texts[key as keyof typeof texts]).toBeTruthy()
    }
    expect(texts.manualStartRequired).toBeTruthy()
  })
})
