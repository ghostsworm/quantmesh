import { describe, expect, it, vi } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import { ChakraProvider } from '@chakra-ui/react'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import en from '../i18n/locales/en-US.json'
import zh from '../i18n/locales/zh-CN.json'
import { FundingCarryStatusBadge } from './FundingCarryDashboard'

// Render the production badge without initializing browser-only API transport.
vi.mock('../services/fundingCarry', () => ({ getFundingCarryDashboard: vi.fn() }))

describe('FundingCarry dashboard status', () => {
  it('renders reconciliation and unknown states using real dictionaries, without a running label', async () => {
    for (const [language, bundle] of [['en-US', en], ['zh-CN', zh]] as const) {
      const i18n = createInstance()
      await i18n.init({ lng: language, resources: { [language]: { translation: bundle } } })
      for (const status of ['reconciliation_required', 'unknown'] as const) {
        const html = renderToStaticMarkup(<ChakraProvider><I18nextProvider i18n={i18n}><FundingCarryStatusBadge status={status} /></I18nextProvider></ChakraProvider>)
        expect(html).toContain(bundle.profitManagement.fundingCarryStatus[status])
        expect(html).not.toContain('profitManagement.fundingCarryStatus.')
        expect(html).not.toContain(`>${bundle.botList.running}<`)
      }
    }
  })
})
