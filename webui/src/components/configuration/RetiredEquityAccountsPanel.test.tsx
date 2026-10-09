import { describe, expect, it } from 'vitest'
import React from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { ChakraProvider } from '@chakra-ui/react'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import type { RetiredEquityAccount } from '../../services/api'
import en from '../../i18n/locales/en-US.json'
import { canResetRetiredEquityAccounts, RETIRED_EQUITY_FLAT_EVIDENCE_REQUIRED, retiredEquityEvidenceResultTranslationKey, RetiredEquityAccountsPanel } from './RetiredEquityAccountsPanel'

const readyAccount: RetiredEquityAccount = {
  id: 'account-hash',
  exchange: 'binance',
  market_type: 'futures',
  account_scope: 'scope-hash',
  status: 'ready_for_explicit_reset',
  retired_at: '2026-10-09T00:00:00Z',
  flat_evidence_count: RETIRED_EQUITY_FLAT_EVIDENCE_REQUIRED,
}

describe('retired equity account reset readiness', () => {
  it('requires at least one account and every account to have two complete-flat observations', () => {
    expect(canResetRetiredEquityAccounts([])).toBe(false)
    expect(canResetRetiredEquityAccounts([readyAccount])).toBe(true)
    expect(canResetRetiredEquityAccounts([readyAccount, { ...readyAccount, id: 'second-account' }])).toBe(true)
  })

  it('keeps reset disabled if any retired market remains incomplete or below evidence threshold', () => {
    expect(canResetRetiredEquityAccounts([
      readyAccount,
      { ...readyAccount, id: 'unsupported-spot', market_type: 'spot', status: 'pending_verification' },
    ])).toBe(false)
    expect(canResetRetiredEquityAccounts([{ ...readyAccount, flat_evidence_count: RETIRED_EQUITY_FLAT_EVIDENCE_REQUIRED - 1 }])).toBe(false)
  })

  it('maps safe verifier outcome codes to explicit localized evidence labels', () => {
    expect(retiredEquityEvidenceResultTranslationKey('unsupported_market')).toBe('configuration.retiredEquity.evidenceResults.unsupported_market')
    expect(retiredEquityEvidenceResultTranslationKey('query_failed')).toBe('configuration.retiredEquity.evidenceResults.query_failed')
    expect(retiredEquityEvidenceResultTranslationKey(undefined)).toBe('configuration.retiredEquity.notAvailable')
  })

  it('does not present empty reset history before the admin read has completed', async () => {
    const i18n = createInstance()
    await i18n.init({ lng: 'en-US', resources: { 'en-US': { translation: en } } })
    const markup = renderToStaticMarkup(
      <ChakraProvider>
        <I18nextProvider i18n={i18n}><RetiredEquityAccountsPanel /></I18nextProvider>
      </ChakraProvider>,
    )
    expect(markup).toContain(en.configuration.retiredEquity.loading)
    expect(markup).not.toContain(en.configuration.retiredEquity.noHistory)
  })
})
