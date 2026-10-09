import React from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { ChakraProvider } from '@chakra-ui/react'
import { I18nextProvider } from 'react-i18next'
import { createInstance } from 'i18next'
import { describe, expect, it } from 'vitest'
import en from '../../i18n/locales/en-US.json'
import zh from '../../i18n/locales/zh-CN.json'
import FundingCarryVisualization from './FundingCarryVisualization'
import type { FundingCarryVisualizationData } from '../../services/strategy'

const diagnostics: FundingCarryVisualizationData = {
  type: 'funding_carry',
  reconciliation_required: true,
  reconciliation_reasons: ['startup_recovery_failed', 'unknown_reason'],
  direction: 'REVERSE',
  spot_qty: 0,
  futures_qty: 0,
  margin_debt: 0.26,
  margin_debt_basis: 'durable_strategy_ledger_not_live_exchange_liability',
  margin_borrow_transfer_id: 123456789,
  margin_borrowed_at: '2026-10-09T00:00:00.123Z',
  margin_cover_remaining_qty: null,
  margin_cover_remaining_known: false,
  margin_cover_remaining_basis: 'historical_net_less_confirmed_repayment',
}

describe('FundingCarryVisualization', () => {
  it('shows localized durable evidence with a warning that it is not live account proof', async () => {
    for (const [language, bundle] of [['en-US', en], ['zh-CN', zh]] as const) {
      const i18n = createInstance()
      await i18n.init({ lng: language, resources: { [language]: { translation: bundle } } })
      const html = renderToStaticMarkup(
        <ChakraProvider>
          <I18nextProvider i18n={i18n}>
            <FundingCarryVisualization data={diagnostics} />
          </I18nextProvider>
        </ChakraProvider>,
      )
      expect(html).toContain(bundle.strategyViz.fundingCarry.reconciliationRequired)
      expect(html).toContain('0.26')
      expect(html).toContain('123456789')
      expect(html).toContain(bundle.strategyViz.fundingCarry.ledgerWarning)
      expect(html).toContain(bundle.strategyViz.fundingCarry.reasons.startup_recovery_failed)
      expect(html).not.toContain('startup_recovery_failed')
      expect(html).not.toContain('unknown_reason')
    }
  })

  it('does not show a recovery warning for a verified state', async () => {
    const i18n = createInstance()
    await i18n.init({ lng: 'en-US', resources: { 'en-US': { translation: en } } })
    const html = renderToStaticMarkup(
      <ChakraProvider>
        <I18nextProvider i18n={i18n}>
          <FundingCarryVisualization data={{ ...diagnostics, reconciliation_required: false, reconciliation_reasons: [] }} />
        </I18nextProvider>
      </ChakraProvider>,
    )
    expect(html).not.toContain(en.strategyViz.fundingCarry.reconciliationRequired)
  })
})
