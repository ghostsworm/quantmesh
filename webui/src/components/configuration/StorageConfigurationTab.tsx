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
}
export function StorageConfigurationTab({ config, updateConfigField, renderPasswordInput }: Props) {
  const { t } = useTranslation()
  return (
  <SimpleGrid columns={2} spacing={6}>
    <ConfigCard title={t('configuration.dataStorage')} icon={<SettingsIcon />}>
      <FormControl mb={4} display="flex" alignItems="center">
        <FormLabel fontSize="xs" fontWeight="bold" mb={0} flex="1">
          {t('configuration.storageEnabled')}
        </FormLabel>
        <Switch
          isChecked={config.storage?.enabled !== false}
          onChange={(e) => updateConfigField('storage.enabled', e.target.checked)}
          colorScheme="blue"
        />
      </FormControl>
      <Text fontSize="xs" color="gray.500" mb={3}>
        {t('configuration.storageEnabledHint')}
      </Text>
      <FormControl mb={4}>
        <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.storageType')}</FormLabel>
        <Select
          value={config.storage?.type || 'sqlite'}
          onChange={(e) => updateConfigField('storage.type', e.target.value)}
          borderRadius="xl"
        >
          <option value="sqlite">{t('configuration.storageTypeSqlite')}</option>
          <option value="mysql">{t('configuration.storageTypeMysql')}</option>
          <option value="postgres">{t('configuration.storageTypePostgres')}</option>
        </Select>
      </FormControl>
      {(config.storage?.type || 'sqlite') === 'sqlite' && (
        <FormControl mb={4}>
          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.databasePath')}</FormLabel>
          <Input
            value={config.storage?.path || ''}
            onChange={(e) => updateConfigField('storage.path', e.target.value)}
            borderRadius="xl"
            placeholder={t('configEditor.sqlitePath')}
          />
        </FormControl>
      )}
      {(config.storage?.type || 'sqlite') === 'mysql' && (
        <FormControl mb={4}>
          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.mysqlDsn')}</FormLabel>
          <Input
            value={config.storage?.path || ''}
            onChange={(e) => updateConfigField('storage.path', e.target.value)}
            borderRadius="xl"
            placeholder={t('configEditor.mysqlDsn')}
          />
          <Text fontSize="xs" color="gray.500" mt={1}>{t('configuration.mysqlDsnHint')}</Text>
        </FormControl>
      )}
      {(config.storage?.type || 'sqlite') === 'postgres' && (
        <FormControl mb={4}>
          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.postgresDsn')}</FormLabel>
          <Input
            value={config.storage?.path || ''}
            onChange={(e) => updateConfigField('storage.path', e.target.value)}
            borderRadius="xl"
            placeholder={t('configEditor.postgresDsn')}
          />
          <Text fontSize="xs" color="gray.500" mt={1}>{t('configuration.postgresDsnHint')}</Text>
        </FormControl>
      )}
      <HStack spacing={4}>
        <FormControl>
          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.buffer')}</FormLabel>
          <NumberInput value={config.storage?.buffer_size || 1000} onChange={(_, v) => updateConfigField('storage.buffer_size', v)}>
            <NumberInputField borderRadius="xl" />
          </NumberInput>
        </FormControl>
        <FormControl>
          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.flushInterval')}</FormLabel>
          <NumberInput value={config.storage?.flush_interval || 5} onChange={(_, v) => updateConfigField('storage.flush_interval', v)}>
            <NumberInputField borderRadius="xl" />
          </NumberInput>
        </FormControl>
      </HStack>
    </ConfigCard>
    <ConfigCard title={t('configuration.webService')} icon={<SettingsIcon />}>
      <FormControl mb={4}>
        <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.listenPort')}</FormLabel>
        <NumberInput value={config.web?.port || 28888} onChange={(_, v) => updateConfigField('web.port', v)}>
          <NumberInputField borderRadius="xl" />
        </NumberInput>
      </FormControl>
      <FormControl>
        <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.apiKeyOptional')}</FormLabel>
        {renderPasswordInput('web.api_key')}
      </FormControl>
    </ConfigCard>
  </SimpleGrid>
  )
}
