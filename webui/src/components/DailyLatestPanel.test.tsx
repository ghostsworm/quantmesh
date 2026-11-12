import type { ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import { ChakraProvider } from '@chakra-ui/react'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { DailyLatestPanel } from './ProfitManagement'
import enUS from '../i18n/locales/en-US.json'
import zhCN from '../i18n/locales/zh-CN.json'
import type { DailyLatest } from '../types/profit'

vi.mock('./profit', () => ({}))
vi.mock('../services/profit', () => ({ getDailyLatest: vi.fn() }))
vi.mock('../services/api', () => ({ getExchanges: vi.fn(), getSymbols: vi.fn() }))

async function render(latest: DailyLatest, language: 'en-US' | 'zh-CN') {
  const i18n = createInstance()
  await i18n.init({ lng: language, resources: { 'en-US': { translation: enUS }, 'zh-CN': { translation: zhCN } } })
  return renderToStaticMarkup(
    <ChakraProvider><I18nextProvider i18n={i18n}><DailyLatestPanel latest={latest} /></I18nextProvider></ChakraProvider>,
  )
}

describe('daily latest panel', () => {
  it('shows a published day as unverified with its reasons', async () => {
    const html = await render({
      status: 'published', date: '2026-11-11', exchangeId: 'binance', equityReconciliationComplete: false, residual: '0',
      reasons: ['futures_evidence_candidate_sample', 'wallet_evidence_missing:spot', 'wallet_evidence_missing:spot_margin'],
    }, 'zh-CN')
    expect(html).toContain('2026-11-11')
    expect(html).toContain('已发布，尚未核实')
    expect(html).toContain('缺少现货钱包证据')
    expect(html).toContain('缺少全仓杠杆钱包证据')
    expect(html).not.toContain('已核实')
  })

  it('shows an unpublished day without a verified zero', async () => {
    const html = await render({ status: 'unpublished', date: '2026-11-11', exchangeId: 'binance' }, 'en-US')
    expect(html).toContain('Not published yet')
    expect(html).not.toContain('Residual')
    expect(html).not.toContain('verified zero')
  })

  it('keeps an unknown reason code visible', async () => {
    const html = await render({
      status: 'published', date: '2026-11-11', exchangeId: 'binance', equityReconciliationComplete: false,
      reasons: ['source_changed', 'some_new_reason'],
    }, 'en-US')
    expect(html).toContain('The ledger changed after publication')
    expect(html).toContain('some_new_reason')
  })
})
