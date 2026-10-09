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
  setConfig: React.Dispatch<React.SetStateAction<Config | null>>
}
export function RiskConfigurationTab({ config, updateConfigField, setConfig }: Props) {
  const { t } = useTranslation()
  return (
  <VStack spacing={6} align="stretch">
    <Alert status="info" borderRadius="lg">
      <AlertIcon />
      <Box>
        <AlertTitle fontSize="sm">{t('configuration.globalMarketRiskIntro')}</AlertTitle>
        <AlertDescription fontSize="xs" mt={2}>
          {t('configuration.globalMarketRiskVsBot')}
        </AlertDescription>
      </Box>
    </Alert>

    <PolymarketConfigSection config={config} setConfig={setConfig} />
    <MacroEventConfigSection config={config} updateConfigField={updateConfigField} />

    <ConfigCard title={t('configuration.riskControlSettings')} icon={<LockIcon />}>
      <Flex justify="space-between" align="center" mb={6}>
        <Box>
          <Text fontWeight="600">{t('configuration.enableRiskEngine')}</Text>
          <Text fontSize="xs" color="gray.500">{t('configuration.enableRiskEngineDesc')}</Text>
        </Box>
        <Switch
          colorScheme="orange"
          isChecked={config.risk_control?.enabled || false}
          onChange={(e) => updateConfigField('risk_control.enabled', e.target.checked)}
        />
      </Flex>
      <FormControl mb={4}>
        <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.monitorSymbols')}</FormLabel>
        <Textarea
          rows={3}
          value={(config.risk_control?.monitor_symbols || []).join(', ')}
          onChange={(e) => {
            updateConfigField('risk_control.monitor_symbols', parseMonitorSymbolsInput(e.target.value))
          }}
          borderRadius="xl"
          placeholder={t('configEditor.symbols')}
        />
        <Text fontSize="xs" color="gray.500" mt={1}>{t('configuration.monitorSymbolsDesc')}</Text>
      </FormControl>
      <SimpleGrid columns={2} spacing={6}>
        <FormControl>
          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.riskKlineInterval')}</FormLabel>
          <Select
            value={config.risk_control?.interval || '1m'}
            onChange={(e) => updateConfigField('risk_control.interval', e.target.value)}
            borderRadius="xl"
          >
            <option value="1m">{t('configEditor.1m')}</option>
            <option value="3m">{t('configEditor.3m')}</option>
            <option value="5m">{t('configEditor.5m')}</option>
            <option value="15m">{t('configEditor.15m')}</option>
            <option value="30m">{t('configEditor.30m')}</option>
            <option value="1h">{t('configEditor.1h')}</option>
          </Select>
        </FormControl>
        <FormControl>
          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.volumeMultiplier')}</FormLabel>
          <NumberInput
            value={config.risk_control?.volume_multiplier || 0}
            onChange={(_, v) => updateConfigField('risk_control.volume_multiplier', v)}
            precision={2}
            step={0.1}
            min={0}
          >
            <NumberInputField borderRadius="xl" />
          </NumberInput>
        </FormControl>
        <FormControl>
          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.averageWindow')}</FormLabel>
          <NumberInput
            value={config.risk_control?.average_window ?? 20}
            onChange={(_, v) => updateConfigField('risk_control.average_window', v ?? 20)}
            min={1}
            max={500}
          >
            <NumberInputField borderRadius="xl" />
          </NumberInput>
          <Text fontSize="xs" color="gray.500" mt={1}>{t('configuration.averageWindowDesc')}</Text>
        </FormControl>
        <FormControl>
          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.recoveryThresholdKline')}</FormLabel>
          <NumberInput
            value={config.risk_control?.recovery_threshold ?? 3}
            onChange={(_, v) => updateConfigField('risk_control.recovery_threshold', v ?? 3)}
            min={1}
            max={50}
          >
            <NumberInputField borderRadius="xl" />
          </NumberInput>
          <Text fontSize="xs" color="gray.500" mt={1}>{t('configuration.recoveryThresholdKlineDesc')}</Text>
        </FormControl>
        <FormControl>
          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.maxLeverage')}</FormLabel>
          <NumberInput
            value={config.risk_control?.max_leverage || 0}
            onChange={(_, v) => updateConfigField('risk_control.max_leverage', v)}
            min={0}
            max={125}
          >
            <NumberInputField borderRadius="xl" />
          </NumberInput>
          <Text fontSize="xs" color="gray.500" mt={1}>{t('configuration.maxLeverageZeroHint')}</Text>
        </FormControl>
      </SimpleGrid>
    </ConfigCard>

    <ConfigCard title={t('configuration.depthMonitorCard')} icon={<LockIcon />}>
      <Flex justify="space-between" align="center" mb={6}>
        <Box>
          <Text fontWeight="600">{t('configuration.depthMonitorEnabled')}</Text>
          <Text fontSize="xs" color="gray.500">{t('configuration.depthMonitorEnabledDesc')}</Text>
        </Box>
        <Switch
          colorScheme="orange"
          isChecked={config.risk_control?.depth_monitor?.enabled || false}
          onChange={(e) => {
            const dm = config.risk_control?.depth_monitor || {}
            updateConfigField('risk_control.depth_monitor', { ...dm, enabled: e.target.checked })
          }}
        />
      </Flex>
      <SimpleGrid columns={2} spacing={6}>
        <FormControl>
          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.depthCheckIntervalSec')}</FormLabel>
          <NumberInput
            value={config.risk_control?.depth_monitor?.check_interval ?? 5}
            onChange={(_, v) => {
              const dm = config.risk_control?.depth_monitor || {}
              updateConfigField('risk_control.depth_monitor', { ...dm, check_interval: v ?? 5 })
            }}
            min={1}
            max={3600}
          >
            <NumberInputField borderRadius="xl" />
          </NumberInput>
        </FormControl>
        <FormControl>
          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.depthLevels')}</FormLabel>
          <NumberInput
            value={config.risk_control?.depth_monitor?.depth_levels ?? 10}
            onChange={(_, v) => {
              const dm = config.risk_control?.depth_monitor || {}
              updateConfigField('risk_control.depth_monitor', { ...dm, depth_levels: v ?? 10 })
            }}
            min={1}
            max={100}
          >
            <NumberInputField borderRadius="xl" />
          </NumberInput>
        </FormControl>
        <FormControl>
          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.depthDropThreshold')}</FormLabel>
          <NumberInput
            value={config.risk_control?.depth_monitor?.drop_threshold ?? 0.5}
            onChange={(_, v) => {
              const dm = config.risk_control?.depth_monitor || {}
              updateConfigField('risk_control.depth_monitor', { ...dm, drop_threshold: v ?? 0.5 })
            }}
            precision={2}
            step={0.05}
            min={0}
            max={1}
          >
            <NumberInputField borderRadius="xl" />
          </NumberInput>
          <Text fontSize="xs" color="gray.500" mt={1}>{t('configuration.depthDropThresholdDesc')}</Text>
        </FormControl>
        <FormControl>
          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.depthRecoveryThreshold')}</FormLabel>
          <NumberInput
            value={config.risk_control?.depth_monitor?.recovery_threshold ?? 0.7}
            onChange={(_, v) => {
              const dm = config.risk_control?.depth_monitor || {}
              updateConfigField('risk_control.depth_monitor', { ...dm, recovery_threshold: v ?? 0.7 })
            }}
            precision={2}
            step={0.05}
            min={0}
            max={1}
          >
            <NumberInputField borderRadius="xl" />
          </NumberInput>
          <Text fontSize="xs" color="gray.500" mt={1}>{t('configuration.depthRecoveryThresholdDesc')}</Text>
        </FormControl>
        <FormControl gridColumn={{ base: '1', md: '1 / -1' }}>
          <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.minDepthUsdt')}</FormLabel>
          <NumberInput
            value={config.risk_control?.depth_monitor?.min_depth_usdt ?? 10000}
            onChange={(_, v) => {
              const dm = config.risk_control?.depth_monitor || {}
              updateConfigField('risk_control.depth_monitor', { ...dm, min_depth_usdt: v ?? 10000 })
            }}
            min={0}
            precision={0}
            step={100}
          >
            <NumberInputField borderRadius="xl" />
          </NumberInput>
          <Text fontSize="xs" color="gray.500" mt={1}>{t('configuration.minDepthUsdtDesc')}</Text>
        </FormControl>
      </SimpleGrid>
    </ConfigCard>
  </VStack>
  )
}
