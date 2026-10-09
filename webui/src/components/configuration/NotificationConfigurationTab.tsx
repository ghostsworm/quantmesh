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
  testingChannel: string | null
  handleTestNotification: (channel: string) => Promise<void>
}
export function NotificationConfigurationTab({ config, updateConfigField, renderPasswordInput, testingChannel, handleTestNotification }: Props) {
  const { t } = useTranslation()
  return (
  <VStack spacing={6} align="stretch">
    <ConfigCard title={t('configuration.globalNotificationSwitch')} icon={<BellIcon />}>
      <Flex justify="space-between" align="center">
        <Text fontWeight="600">{t('configuration.enableNotifications')}</Text>
        <Switch
          isChecked={config.notifications?.enabled || false}
          onChange={(e) => updateConfigField('notifications.enabled', e.target.checked)}
        />
      </Flex>
    </ConfigCard>
    <SimpleGrid columns={2} spacing={6}>
      <ConfigCard title={t('configuration.telegramBot')}>
        <FormControl mb={4} display="flex" alignItems="center" justifyContent="space-between">
          <FormLabel fontSize="sm" mb={0}>{t('configuration.enableTelegram')}</FormLabel>
          <Switch
            isChecked={config.notifications?.telegram?.enabled || false}
            onChange={(e) => updateConfigField('notifications.telegram.enabled', e.target.checked)}
          />
        </FormControl>
        <FormControl mb={4}>
          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.token')}</FormLabel>
          {renderPasswordInput('notifications.telegram.bot_token')}
        </FormControl>
        <FormControl>
          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.chatId')}</FormLabel>
          <Input
            value={config.notifications?.telegram?.chat_id || ''}
            onChange={(e) => updateConfigField('notifications.telegram.chat_id', e.target.value)}
            borderRadius="xl"
          />
          <FormHelperText fontSize="xs" color="gray.600" whiteSpace="pre-line" mt={2}>
            {t('configuration.telegramChatIdHelp')}
          </FormHelperText>
        </FormControl>
        <Divider />
        <Button
          size="sm"
          variant="outline"
          colorScheme="blue"
          isLoading={testingChannel === 'telegram'}
          loadingText={t('configuration.testConnectionSending')}
          onClick={() => handleTestNotification('telegram')}
          isDisabled={!config.notifications?.telegram?.bot_token || !config.notifications?.telegram?.chat_id}
        >
          {t('configuration.testConnection')}
        </Button>
      </ConfigCard>
      <ConfigCard title={t('configuration.webhook')}>
        <FormControl mb={4} display="flex" alignItems="center" justifyContent="space-between">
          <FormLabel fontSize="sm" mb={0}>{t('configuration.enableWebhook')}</FormLabel>
          <Switch
            isChecked={config.notifications?.webhook?.enabled || false}
            onChange={(e) => updateConfigField('notifications.webhook.enabled', e.target.checked)}
          />
        </FormControl>
        <FormControl mb={4}>
          <FormLabel fontSize="xs" fontWeight="bold">{t('configEditor.url')}</FormLabel>
          <Input
            value={config.notifications?.webhook?.url || ''}
            onChange={(e) => updateConfigField('notifications.webhook.url', e.target.value)}
            placeholder={t('configEditor.webhook')}
            borderRadius="xl"
          />
        </FormControl>
        <Divider />
        <Button
          size="sm"
          variant="outline"
          colorScheme="blue"
          isLoading={testingChannel === 'webhook'}
          loadingText={t('configuration.testConnectionSending')}
          onClick={() => handleTestNotification('webhook')}
          isDisabled={!config.notifications?.webhook?.url}
        >
          {t('configuration.testConnection')}
        </Button>
      </ConfigCard>
      <ConfigCard title={t('configuration.email')}>
        <FormControl mb={4} display="flex" alignItems="center" justifyContent="space-between">
          <FormLabel fontSize="sm" mb={0}>{t('configuration.enableEmail')}</FormLabel>
          <Switch
            isChecked={config.notifications?.email?.enabled || false}
            onChange={(e) => updateConfigField('notifications.email.enabled', e.target.checked)}
          />
        </FormControl>
        <FormControl mb={4}>
          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.emailProvider')}</FormLabel>
          <Select
            value={config.notifications?.email?.provider || 'smtp'}
            onChange={(e) => updateConfigField('notifications.email.provider', e.target.value)}
            borderRadius="xl"
          >
            <option value="smtp">{t('configEditor.smtp')}</option>
            <option value="resend">{t('configEditor.resend')}</option>
            <option value="mailgun">{t('configEditor.mailgun')}</option>
          </Select>
        </FormControl>
        {config.notifications?.email?.provider === 'smtp' && (
          <>
            <FormControl mb={4}>
              <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.smtpHost')}</FormLabel>
              <Input
                value={config.notifications?.email?.smtp?.host || ''}
                onChange={(e) => updateConfigField('notifications.email.smtp.host', e.target.value)}
                borderRadius="xl"
              />
            </FormControl>
            <FormControl mb={4}>
              <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.smtpPort')}</FormLabel>
              <NumberInput value={config.notifications?.email?.smtp?.port || 587} onChange={(_, v) => updateConfigField('notifications.email.smtp.port', v)}>
                <NumberInputField borderRadius="xl" />
              </NumberInput>
            </FormControl>
            <FormControl mb={4}>
              <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.smtpUsername')}</FormLabel>
              <Input
                value={config.notifications?.email?.smtp?.username || ''}
                onChange={(e) => updateConfigField('notifications.email.smtp.username', e.target.value)}
                borderRadius="xl"
              />
            </FormControl>
            <FormControl mb={4}>
              <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.smtpPassword')}</FormLabel>
              {renderPasswordInput('notifications.email.smtp.password')}
            </FormControl>
          </>
        )}
        {config.notifications?.email?.provider === 'resend' && (
          <FormControl mb={4}>
            <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.resendApiKey')}</FormLabel>
            {renderPasswordInput('notifications.email.resend.api_key')}
          </FormControl>
        )}
        {config.notifications?.email?.provider === 'mailgun' && (
          <>
            <FormControl mb={4}>
              <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.mailgunApiKey')}</FormLabel>
              {renderPasswordInput('notifications.email.mailgun.api_key')}
            </FormControl>
            <FormControl mb={4}>
              <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.mailgunDomain')}</FormLabel>
              <Input
                value={config.notifications?.email?.mailgun?.domain || ''}
                onChange={(e) => updateConfigField('notifications.email.mailgun.domain', e.target.value)}
                borderRadius="xl"
              />
            </FormControl>
          </>
        )}
        <FormControl mb={4}>
          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.emailFrom')}</FormLabel>
          <Input
            value={config.notifications?.email?.from || ''}
            onChange={(e) => updateConfigField('notifications.email.from', e.target.value)}
            placeholder={t('configEditor.sender')}
            borderRadius="xl"
          />
        </FormControl>
        <FormControl>
          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.emailTo')}</FormLabel>
          <Input
            value={config.notifications?.email?.to || ''}
            onChange={(e) => updateConfigField('notifications.email.to', e.target.value)}
            placeholder={t('configEditor.recipient')}
            borderRadius="xl"
          />
        </FormControl>
        <Divider />
        <Button
          size="sm"
          variant="outline"
          colorScheme="blue"
          isLoading={testingChannel === 'email'}
          loadingText={t('configuration.testConnectionSending')}
          onClick={() => handleTestNotification('email')}
          isDisabled={!config.notifications?.email?.from || !config.notifications?.email?.to}
        >
          {t('configuration.testConnection')}
        </Button>
      </ConfigCard>
      <ConfigCard title={t('configuration.feishu')}>
        <FormControl mb={4} display="flex" alignItems="center" justifyContent="space-between">
          <FormLabel fontSize="sm" mb={0}>{t('configuration.enableFeishu')}</FormLabel>
          <Switch
            isChecked={config.notifications?.feishu?.enabled || false}
            onChange={(e) => updateConfigField('notifications.feishu.enabled', e.target.checked)}
          />
        </FormControl>
        <FormControl mb={4}>
          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.webhookUrl')}</FormLabel>
          <Input
            value={config.notifications?.feishu?.webhook || ''}
            onChange={(e) => updateConfigField('notifications.feishu.webhook', e.target.value)}
            placeholder={t('configEditor.feishu')}
            borderRadius="xl"
          />
        </FormControl>
        <Divider />
        <Button
          size="sm"
          variant="outline"
          colorScheme="blue"
          isLoading={testingChannel === 'feishu'}
          loadingText={t('configuration.testConnectionSending')}
          onClick={() => handleTestNotification('feishu')}
          isDisabled={!config.notifications?.feishu?.webhook}
        >
          {t('configuration.testConnection')}
        </Button>
      </ConfigCard>
      <ConfigCard title={t('configuration.dingtalk')}>
        <FormControl mb={4} display="flex" alignItems="center" justifyContent="space-between">
          <FormLabel fontSize="sm" mb={0}>{t('configuration.enableDingtalk')}</FormLabel>
          <Switch
            isChecked={config.notifications?.dingtalk?.enabled || false}
            onChange={(e) => updateConfigField('notifications.dingtalk.enabled', e.target.checked)}
          />
        </FormControl>
        <FormControl mb={4}>
          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.webhookUrl')}</FormLabel>
          <Input
            value={config.notifications?.dingtalk?.webhook || ''}
            onChange={(e) => updateConfigField('notifications.dingtalk.webhook', e.target.value)}
            placeholder={t('configEditor.dingtalk')}
            borderRadius="xl"
          />
        </FormControl>
        <FormControl>
          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.dingtalkSecret')}</FormLabel>
          {renderPasswordInput('notifications.dingtalk.secret')}
        </FormControl>
        <Divider />
        <Button
          size="sm"
          variant="outline"
          colorScheme="blue"
          isLoading={testingChannel === 'dingtalk'}
          loadingText={t('configuration.testConnectionSending')}
          onClick={() => handleTestNotification('dingtalk')}
          isDisabled={!config.notifications?.dingtalk?.webhook}
        >
          {t('configuration.testConnection')}
        </Button>
      </ConfigCard>
      <ConfigCard title={t('configuration.wechatWork')}>
        <FormControl mb={4} display="flex" alignItems="center" justifyContent="space-between">
          <FormLabel fontSize="sm" mb={0}>{t('configuration.enableWechatWork')}</FormLabel>
          <Switch
            isChecked={config.notifications?.wechat_work?.enabled || false}
            onChange={(e) => updateConfigField('notifications.wechat_work.enabled', e.target.checked)}
          />
        </FormControl>
        <FormControl mb={4}>
          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.webhookUrl')}</FormLabel>
          <Input
            value={config.notifications?.wechat_work?.webhook || ''}
            onChange={(e) => updateConfigField('notifications.wechat_work.webhook', e.target.value)}
            placeholder={t('configEditor.wechat')}
            borderRadius="xl"
          />
        </FormControl>
        <Divider />
        <Button
          size="sm"
          variant="outline"
          colorScheme="blue"
          isLoading={testingChannel === 'wechat_work'}
          loadingText={t('configuration.testConnectionSending')}
          onClick={() => handleTestNotification('wechat_work')}
          isDisabled={!config.notifications?.wechat_work?.webhook}
        >
          {t('configuration.testConnection')}
        </Button>
      </ConfigCard>
      <ConfigCard title={t('configuration.slack')}>
        <FormControl mb={4} display="flex" alignItems="center" justifyContent="space-between">
          <FormLabel fontSize="sm" mb={0}>{t('configuration.enableSlack')}</FormLabel>
          <Switch
            isChecked={config.notifications?.slack?.enabled || false}
            onChange={(e) => updateConfigField('notifications.slack.enabled', e.target.checked)}
          />
        </FormControl>
        <FormControl mb={4}>
          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.webhookUrl')}</FormLabel>
          <Input
            value={config.notifications?.slack?.webhook || ''}
            onChange={(e) => updateConfigField('notifications.slack.webhook', e.target.value)}
            placeholder={t('configEditor.slack')}
            borderRadius="xl"
          />
        </FormControl>
        <Divider />
        <Button
          size="sm"
          variant="outline"
          colorScheme="blue"
          isLoading={testingChannel === 'slack'}
          loadingText={t('configuration.testConnectionSending')}
          onClick={() => handleTestNotification('slack')}
          isDisabled={!config.notifications?.slack?.webhook}
        >
          {t('configuration.testConnection')}
        </Button>
      </ConfigCard>
    </SimpleGrid>
  </VStack>
  )
}
