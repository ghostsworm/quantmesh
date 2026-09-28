import { describe, expect, it } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import { ChakraProvider } from '@chakra-ui/react'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import en from '../i18n/locales/en-US.json'
import { RiskPositionValueMetrics } from './RiskPositionValueMetrics'

async function renderValues(valued = true) {
  const i18n = createInstance()
  await i18n.init({ lng: 'en', resources: { en: { translation: en } } })
  return renderToStaticMarkup(
    <ChakraProvider>
      <I18nextProvider i18n={i18n}>
        <RiskPositionValueMetrics status={{ total_position_value: 200, total_actual_margin: 20, max_position_value: 190, reached_limit_value: true, leverage: 10, valuation_available: valued }} />
      </I18nextProvider>
    </ChakraProvider>,
  )
}

describe('RiskPositionValueMetrics', () => {
  it('renders the limit and badge beside notional, never beside leveraged margin', async () => {
    const html = await renderValues()
    const [notional, margin] = html.split('data-testid="risk-notional-value"')[1].split('data-testid="risk-margin-value"')
    expect(notional).toContain('$200.00')
    expect(notional).toContain('/ $190')
    expect(notional).toContain(en.botRiskControl.reachedLimitValue)
    expect(margin).toContain('$20.00')
    expect(margin).not.toContain('/ $190')
    expect(margin).not.toContain(en.botRiskControl.reachedLimitValue)
    expect(margin).toContain('Leverage')
    expect(margin).not.toContain('杠杆')
  })

  it('does not render unavailable valuation as a known amount', async () => {
    const html = await renderValues(false)
    expect(html).not.toContain('$200.00')
    expect(html).not.toContain('$20.00')
    expect(html).toContain('$-')
  })
})
