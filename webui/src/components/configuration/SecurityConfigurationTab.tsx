import React from 'react'
import { Box, Heading, Button, Alert, AlertIcon, AlertTitle, AlertDescription, Textarea, FormControl, FormHelperText, FormLabel, Input, NumberInput, NumberInputField, NumberInputStepper, NumberIncrementStepper, NumberDecrementStepper, Select, Switch, Text, VStack, HStack, Divider, Flex, SimpleGrid, Slider, SliderTrack, SliderFilledTrack, SliderThumb } from '@chakra-ui/react'
import { SettingsIcon, BellIcon, InfoIcon, StarIcon, LockIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import type { Config } from '../../services/config'
import { applyPolymarketEnabledToConfig } from '../../utils/polymarketConfigDefaults'
import { parseMonitorSymbolsInput } from '../../utils/riskControlUi'
import { ConfigCard, PolymarketConfigSection, MacroEventConfigSection } from './ConfigurationSections'
import { RetiredEquityAccountsPanel } from './RetiredEquityAccountsPanel'
type Props = {
  config: Config
  updateConfigField: (path: string, value: unknown) => void
  securityStatus: { master_key_path: string; master_key_exists: boolean } | null
  generatingKey: boolean
  handleGenerateMasterKey: () => Promise<void>
}
export function SecurityConfigurationTab({ config, updateConfigField, securityStatus, generatingKey, handleGenerateMasterKey }: Props) {
  const { t } = useTranslation()
  return (
  <VStack spacing={6} align="stretch">
    <ConfigCard title={t('configuration.securitySettings')} icon={<LockIcon />}>
      <VStack spacing={6} align="stretch">
        <Alert status="info" borderRadius="lg">
          <AlertIcon />
          <AlertDescription fontSize="sm">
            {t('configuration.securitySettingsDesc')}
          </AlertDescription>
        </Alert>

        <FormControl display="flex" alignItems="center">
          <FormLabel fontSize="sm" fontWeight="bold" mb={0} flex="1">
            {t('configuration.enableEncryption')}
          </FormLabel>
          <Switch
            colorScheme="blue"
            isChecked={config.security?.encryption_enabled || false}
            onChange={(e) => updateConfigField('security.encryption_enabled', e.target.checked)}
          />
        </FormControl>

        {config.security?.encryption_enabled && (
          <>
            <FormControl>
              <FormLabel fontSize="xs" fontWeight="bold" color="gray.500">
                {t('configuration.masterKeyPath')}
              </FormLabel>
              <Input
                value={securityStatus?.master_key_path || config.security?.master_key_path || './data/master.key'}
                isReadOnly
                borderRadius="xl"
                bg="gray.50"
              />
              <Text fontSize="xs" color="gray.500" mt={1}>
                {t('configuration.masterKeyPathDesc')}
              </Text>
            </FormControl>

            <Divider />

            <VStack spacing={4} align="stretch">
              <Text fontSize="sm" fontWeight="600">
                {t('configuration.masterKeyManagement')}
              </Text>

              {securityStatus?.master_key_exists ? (
                <Alert status="success" borderRadius="md">
                  <AlertIcon />
                  <AlertDescription fontSize="xs">
                    {t('configuration.masterKeyExists')}
                  </AlertDescription>
                </Alert>
              ) : (
                <Alert status="warning" borderRadius="md">
                  <AlertIcon />
                  <AlertDescription fontSize="xs">
                    {t('configuration.masterKeyNotExists')}
                  </AlertDescription>
                </Alert>
              )}

              <Button
                size="sm"
                colorScheme="blue"
                variant="outline"
                onClick={handleGenerateMasterKey}
                isLoading={generatingKey}
                isDisabled={securityStatus?.master_key_exists || false}
                leftIcon={<LockIcon />}
              >
                {t('configuration.generateMasterKey')}
              </Button>
            </VStack>

            <Alert status="warning" borderRadius="md" mt={4}>
              <AlertIcon />
              <AlertDescription fontSize="xs">
                {t('configuration.encryptionWarning')}
              </AlertDescription>
            </Alert>
          </>
        )}
      </VStack>
    </ConfigCard>
    <RetiredEquityAccountsPanel />
  </VStack>
  )
}
