import React from 'react'
import { Box, Heading, Button, Alert, AlertIcon, AlertTitle, AlertDescription, Textarea, FormControl, FormHelperText, FormLabel, Input, NumberInput, NumberInputField, NumberInputStepper, NumberIncrementStepper, NumberDecrementStepper, Select, Switch, Text, VStack, HStack, Divider, Flex, SimpleGrid, Slider, SliderTrack, SliderFilledTrack, SliderThumb } from '@chakra-ui/react'
import { SettingsIcon, BellIcon, InfoIcon, StarIcon, LockIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import type { Config } from '../../services/config'
import { applyPolymarketEnabledToConfig } from '../../utils/polymarketConfigDefaults'
import { parseMonitorSymbolsInput } from '../../utils/riskControlUi'
import { ConfigCard, PolymarketConfigSection, MacroEventConfigSection } from './ConfigurationSections'
type Props = {
  config: Config
  updateConfigField: (path: string, value: unknown) => void
  renderPasswordInput: (path: string, placeholder?: string) => React.ReactNode
  testingGemini: boolean
  handleTestGemini: () => Promise<void>
  onAIWizardOpen: () => void
  getNestedValue: (obj: Config, path: string) => unknown
}
export function AIConfigurationTab({ config, updateConfigField, renderPasswordInput, testingGemini, handleTestGemini, onAIWizardOpen, getNestedValue }: Props) {
  const { t } = useTranslation()
  return (
  <VStack spacing={6} align="stretch">
    <Alert status="info" borderRadius="lg" variant="subtle">
      <AlertIcon />
      <AlertDescription fontSize="sm">{t('configuration.aiAssistantTabIntro')}</AlertDescription>
    </Alert>
    <ConfigCard title={t('configuration.aiConfigAssistant')} icon={<StarIcon />}>
      <VStack spacing={4} align="stretch">
        <Text fontSize="xs" color="gray.500">{t('configuration.globalAIProviderDesc')}</Text>
        <SimpleGrid columns={2} spacing={4}>
          <FormControl>
            <FormLabel fontSize="xs" fontWeight="bold" color="gray.500">{t('configuration.protocolFamily')}</FormLabel>
            <Select
              value={config.ai?.provider || 'gemini'}
              onChange={(e) => {
                updateConfigField('ai.provider', e.target.value)
                // 切换协议族时清空 model，让用户重新选择/留空走默认
                updateConfigField('ai.model', '')
              }}
              borderRadius="xl"
              size="sm"
            >
              <option value="gemini">{t('aiConfig.wizard.providers.gemini')}</option>
              <option value="openai">{t('aiConfig.wizard.providers.openai')}</option>
              <option value="dashscope">{t('aiConfig.wizard.providers.dashscopeCn')}</option>
              <option value="dashscope_sg">{t('aiConfig.wizard.providers.dashscopeSg')}</option>
              <option value="kimi">{t('aiConfig.wizard.providers.kimiCn')}</option>
              <option value="kimi_intl">{t('aiConfig.wizard.providers.kimiIntl')}</option>
              <option value="deepseek">{t('aiConfig.wizard.providers.deepseek')}</option>
              <option value="claude">{t('configuration.protocolClaude')}</option>
              <option value="custom">{t('aiConfig.wizard.providers.custom')}</option>
            </Select>
          </FormControl>
          <FormControl>
            <FormLabel fontSize="xs" fontWeight="bold" color="gray.500">{t('configuration.model')}</FormLabel>
            <Input
              value={config.ai?.model || ''}
              onChange={(e) => updateConfigField('ai.model', e.target.value)}
              placeholder={t('configuration.useDefaultModel')}
              borderRadius="xl"
              size="sm"
            />
          </FormControl>
        </SimpleGrid>
        <FormControl>
          <FormLabel fontSize="xs" fontWeight="bold" color="gray.500">{t('configuration.apiKey')}</FormLabel>
          {renderPasswordInput('ai.api_key', t('configuration.enterApiKeyPlaceholder'))}
          <Text fontSize="xs" color="gray.500" mt={1}>
            {(config.ai?.provider || 'gemini') === 'gemini' && t('configuration.geminiApiKeyDesc')}
            {['openai', 'dashscope', 'dashscope_sg', 'kimi', 'kimi_intl', 'deepseek', 'custom'].includes(config.ai?.provider || '') && t('configuration.apiKeyFromOpenAI')}
            {config.ai?.provider === 'claude' && t('configuration.apiKeyFromAnthropic')}
          </Text>
        </FormControl>
        {(['openai', 'dashscope', 'dashscope_sg', 'kimi', 'kimi_intl', 'deepseek', 'custom', 'claude'].includes(config.ai?.provider || '')) && (
          <FormControl>
            <FormLabel fontSize="xs" fontWeight="bold" color="gray.500">{t('configuration.baseUrlOptional')}</FormLabel>
            <Input
              value={config.ai?.base_url || ''}
              onChange={(e) => updateConfigField('ai.base_url', e.target.value)}
              placeholder={t('configuration.baseUrlPlaceholder')}
              borderRadius="xl"
              size="sm"
            />
          </FormControl>
        )}
        <Button
          size="sm"
          variant="outline"
          colorScheme="blue"
          alignSelf="flex-start"
          isLoading={testingGemini}
          loadingText={t('configuration.testingCredentials')}
          onClick={handleTestGemini}
          isDisabled={
            !String(getNestedValue(config, 'ai.api_key') || '').trim() &&
            !String(getNestedValue(config, 'ai.gemini_api_key') || '').trim()
          }
        >
          {t('configuration.testAIConnection')}
        </Button>
        <Button
          leftIcon={<StarIcon />}
          colorScheme="purple"
          variant="outline"
          onClick={onAIWizardOpen}
          isDisabled={
            !getNestedValue(config, 'ai.gemini_api_key') &&
            !getNestedValue(config, 'ai.api_key') &&
            !(config.ai?.default_upstream && config.ai?.upstreams?.[config.ai.default_upstream]?.api_key) &&
            !Object.values(config.ai?.upstreams || {}).some((p) => p?.api_key)
          }
        >
          {t('configuration.openAIAssistant')}
        </Button>
        {!getNestedValue(config, 'ai.gemini_api_key') && !getNestedValue(config, 'ai.api_key') &&
          !(config.ai?.default_upstream && config.ai?.upstreams?.[config.ai.default_upstream]?.api_key) &&
          !Object.values(config.ai?.upstreams || {}).some((p) => p?.api_key) && (
          <Alert status="info" size="sm" borderRadius="md">
            <AlertIcon />
            <AlertDescription fontSize="xs">
              {t('configuration.configureGeminiFirst')}
            </AlertDescription>
          </Alert>
        )}
      </VStack>
    </ConfigCard>

    <ConfigCard title={t('configuration.aiUpstreamProfilesTitle')} icon={<InfoIcon />}>
      <VStack spacing={3} align="stretch">
        <Text fontSize="xs" color="gray.500">{t('configuration.aiUpstreamProfilesDesc')}</Text>
        <FormControl>
          <FormLabel fontSize="xs" fontWeight="bold" color="gray.500">{t('configuration.aiDefaultUpstream')}</FormLabel>
          <Input
            borderRadius="xl"
            value={config.ai?.default_upstream || ''}
            placeholder={t('configuration.aiDefaultUpstreamPlaceholder')}
            onChange={(e) => updateConfigField('ai.default_upstream', e.target.value)}
          />
        </FormControl>
        <Text fontSize="xs" color="gray.600">{t('configuration.aiUpstreamsYamlHint')}</Text>
      </VStack>
    </ConfigCard>
  </VStack>
  )
}
