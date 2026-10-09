import { describe, expect, it, vi } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import { ChakraProvider } from '@chakra-ui/react'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import type { Config } from '../../services/config'
import en from '../../i18n/locales/en-US.json'
import { configEditorResources } from '../../i18n/configEditor'
import { AIConfigurationTab } from './AIConfigurationTab'
import { NotificationConfigurationTab } from './NotificationConfigurationTab'
import { SecurityConfigurationTab } from './SecurityConfigurationTab'
import { StorageConfigurationTab } from './StorageConfigurationTab'
import { RiskConfigurationTab } from './RiskConfigurationTab'

const config = {
  ai: { provider: 'openai', model: 'fixture-model', api_key: 'fixture-only' },
  notifications: { enabled: true, telegram: { enabled: true, chat_id: 'fixture-chat' }, email: { provider: 'smtp' } },
  storage: { enabled: true, type: 'sqlite', path: './fixture.db' },
  security: { encryption_enabled: true },
  risk_control: { enabled: true, monitor_symbols: ['BTCUSDT'], average_window: 37 },
} as unknown as Config
const updateConfigField = vi.fn()
const renderPasswordInput = (path: string) => <input data-testid={path} />

async function render(element: React.ReactNode) {
  const i18n = createInstance()
  await i18n.init({ lng: 'en-US', resources: { 'en-US': { translation: { ...en, configEditor: configEditorResources } } } })
  return renderToStaticMarkup(<ChakraProvider><I18nextProvider i18n={i18n}>{element}</I18nextProvider></ChakraProvider>)
}

describe('extracted configuration display tabs', () => {
  it('AI tab retains provider, model and password binding', async () => {
    const html = await render(<AIConfigurationTab config={config} updateConfigField={updateConfigField} renderPasswordInput={renderPasswordInput} testingGemini={false} handleTestGemini={async () => {}} onAIWizardOpen={() => {}} getNestedValue={() => ''} />)
    expect(html).toContain('fixture-model')
    expect(html).toContain('data-testid="ai.api_key"')
    expect(html).toContain(en.configuration.aiConfigAssistant)
  })
  it('notification tab retains values, secret path and dictionary labels', async () => {
    const html = await render(<NotificationConfigurationTab config={config} updateConfigField={updateConfigField} renderPasswordInput={renderPasswordInput} testingChannel={null} handleTestNotification={async () => {}} />)
    expect(html).toContain('fixture-chat')
    expect(html).toContain('notifications.telegram.bot_token')
    expect(html).toContain(en.configuration.globalNotificationSwitch)
    expect(html).not.toContain('configEditor.')
  })
  it('storage tab retains configured path and web password input', async () => {
    const html = await render(<StorageConfigurationTab config={config} updateConfigField={updateConfigField} renderPasswordInput={renderPasswordInput} />)
    expect(html).toContain('./fixture.db')
    expect(html).toContain('data-testid="web.api_key"')
    expect(html).not.toContain('configEditor.')
  })
  it('security tab retains read-only key path and key-exists message', async () => {
    const html = await render(<SecurityConfigurationTab config={config} updateConfigField={updateConfigField} securityStatus={{ master_key_path: './fixture.key', master_key_exists: true }} generatingKey={false} handleGenerateMasterKey={async () => {}} />)
    expect(html).toContain('./fixture.key')
    expect(html).toContain(en.configuration.masterKeyExists)
    expect(html).toContain(en.configuration.retiredEquity.title.replace('&', '&amp;'))
    expect(html).toContain(en.configuration.retiredEquity.warning)
    expect(html).toContain('readonly')
  })
  it('risk tab retains monitoring symbols, average window and shared sections', async () => {
    const html = await render(<RiskConfigurationTab config={config} updateConfigField={updateConfigField} setConfig={() => {}} />)
    expect(html).toContain('BTCUSDT')
    expect(html).toContain('value="37"')
    expect(html).toContain(en.configuration.macroEventSectionTitle)
    expect(html).toContain(en.configuration.polymarketSectionTitle)
  })
})
