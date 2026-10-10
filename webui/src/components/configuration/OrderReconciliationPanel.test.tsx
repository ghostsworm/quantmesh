import React from 'react'
import { describe, expect, it } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import { ChakraProvider } from '@chakra-ui/react'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import en from '../../i18n/locales/en-US.json'
import { OrderReconciliationPanel } from './OrderReconciliationPanel'

describe('订单人工核账面板', () => {
  it('初始只显示加载态，不提前展示空数据或任何解锁成功状态', async () => {
    const i18n = createInstance()
    await i18n.init({ lng: 'en-US', resources: { 'en-US': { translation: en } } })
    const markup = renderToStaticMarkup(
      <ChakraProvider><I18nextProvider i18n={i18n}><OrderReconciliationPanel /></I18nextProvider></ChakraProvider>,
    )
    expect(markup).toContain(en.configuration.orderReconciliation.loading)
    expect(markup).not.toContain(en.configuration.orderReconciliation.noCases)
    expect(markup).not.toContain(en.configuration.orderReconciliation.confirmRelease)
  })
})
