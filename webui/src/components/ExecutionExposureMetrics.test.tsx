import { describe, expect, it } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import { ChakraProvider } from '@chakra-ui/react'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { ExecutionExposureMetrics } from './ExecutionExposureMetrics'
import type { ExecutionExposure } from '../services/executionExposure'
import { executionExposureLocales, withExecutionExposure } from '../i18n/executionExposure'

const base: ExecutionExposure = {
  ready: true, reason_code: 'ready', opening_available: false, new_lot_available: false,
  position_quantity: 0, pending_quantity: 2, projected_quantity: 2, projected_notional: 200,
  layers: 2, limits: { quantity: 2, notional: 200, layers: 2 }, mark: 100, mark_at: '2026-09-24T00:00:00Z',
}
async function render(exposure?: ExecutionExposure, language = 'en-US') {
  const i18n = createInstance()
  await i18n.init({ lng: language, resources: { [language]: { translation: withExecutionExposure(language, {}) } } })
  return renderToStaticMarkup(<ChakraProvider><I18nextProvider i18n={i18n}><ExecutionExposureMetrics exposure={exposure} /></I18nextProvider></ChakraProvider>)
}

describe('ExecutionExposureMetrics', () => {
  it('shows pending capacity exhaustion even with zero filled inventory', async () => {
    const html = await render(base)
    expect(html).toContain(executionExposureLocales['en-US'].exhausted)
    expect(html).toContain('Pending / uncertain opening quantity')
    expect(html).toContain('data-testid="exposure-projected"')
    expect(html).toContain('Limit: 200')
  })
  it('does not confuse a full layer count with zero quantity capacity', async () => {
    const html = await render({ ...base, opening_available: true })
    expect(html).toContain(executionExposureLocales['en-US'].layersFull)
    expect(html).not.toContain(executionExposureLocales['en-US'].exhausted)
  })
  it('warns on missing data and never implies readiness', async () => {
    const html = await render()
    expect(html).toContain(executionExposureLocales['en-US'].missing)
    expect(html).not.toContain('data-testid="exposure-position"')
  })
  it('hides unverified valuation and localizes readiness without raw backend errors', async () => {
    const html = await render({ ...base, ready: false, reason_code: 'quote_unavailable' }, 'zh-CN')
    expect(html).toContain(executionExposureLocales['zh-CN'].quote_unavailable)
    const notional = html.split('data-testid="exposure-notional"')[1].split('data-testid="exposure-layers"')[0]
    expect(notional).toContain('未核实')
    expect(notional).not.toContain('>200<')
  })
  it('keeps complete translation keys with an explicit English fallback', async () => {
    for (const bundle of Object.values(executionExposureLocales)) expect(Object.keys(bundle).sort()).toEqual(Object.keys(executionExposureLocales['en-US']).sort())
    expect(withExecutionExposure('de-DE', {}).executionExposure).toEqual(executionExposureLocales['en-US'])
    expect(await render({ ...base, ready: false, reason_code: 'not_initialized' }, 'zh-TW')).toContain('未核實')
  })
})
