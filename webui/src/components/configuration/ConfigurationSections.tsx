import React from 'react'
import { Box, Heading, Button, Alert, AlertIcon, AlertTitle, AlertDescription, Textarea, FormControl, FormHelperText, FormLabel, Input, NumberInput, NumberInputField, NumberInputStepper, NumberIncrementStepper, NumberDecrementStepper, Select, Switch, Text, VStack, HStack, Divider, Flex, SimpleGrid, Slider, SliderTrack, SliderFilledTrack, SliderThumb } from '@chakra-ui/react'
import { SettingsIcon, BellIcon, InfoIcon, StarIcon, LockIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import type { Config } from '../../services/config'
import { applyPolymarketEnabledToConfig } from '../../utils/polymarketConfigDefaults'
import { parseMonitorSymbolsInput } from '../../utils/riskControlUi'
const WINDOW_SIZE_PRESETS = [10, 20, 30, 50, 100] as const

export const WindowSizeSlider: React.FC<{
  value: number
  onChange: (v: number) => void
  size?: 'sm' | 'md'
}> = ({ value, onChange, size = 'md' }) => {
  const displayValue = Math.max(1, Math.min(100, value || 10))
  const handleChange = (v: number) => onChange(Math.max(1, Math.min(100, v)))
  return (
    <VStack align="stretch" spacing={2}>
      <HStack spacing={3} align="center">
        <Slider
          flex={1}
          value={displayValue}
          min={1}
          max={100}
          step={1}
          onChange={handleChange}
        >
          <SliderTrack bg="gray.200">
            <SliderFilledTrack bg="blue.500" />
          </SliderTrack>
          <SliderThumb boxSize={size === 'sm' ? 3 : 4} />
        </Slider>
        <Text fontWeight="bold" minW={8} textAlign="right" fontSize={size === 'sm' ? 'sm' : 'md'}>
          {displayValue}
        </Text>
      </HStack>
      <HStack flexWrap="wrap" gap={1}>
        {WINDOW_SIZE_PRESETS.map((preset) => (
          <Button
            key={preset}
            size="xs"
            variant={displayValue === preset ? 'solid' : 'outline'}
            colorScheme="blue"
            onClick={() => handleChange(preset)}
          >
            {preset}
          </Button>
        ))}
      </HStack>
    </VStack>
  )
}

export const PolymarketConfigSection: React.FC<{
  config: Config
  setConfig: React.Dispatch<React.SetStateAction<Config | null>>
}> = ({ config, setConfig }) => {
  const { t } = useTranslation()
  const ps = config.ai?.modules?.polymarket_signal
  const enabled = ps?.enabled ?? false
  const gammaUrl = config.macro_event?.gamma_api_url || ps?.api_url || ''

  return (
    <ConfigCard title={t('configuration.polymarketSectionTitle')} icon={<StarIcon />}>
      <Flex justify="space-between" align="center" mb={4}>
        <Box>
          <Text fontWeight="600">{t('configuration.polymarketEnable')}</Text>
          <Text fontSize="xs" color="gray.500">
            {t('configuration.polymarketEnableDesc')}
          </Text>
        </Box>
        <Switch
          colorScheme="purple"
          isChecked={enabled}
          onChange={(e) => {
            const checked = e.target.checked
            setConfig((prev) => (prev ? applyPolymarketEnabledToConfig(prev, checked) : null))
          }}
        />
      </Flex>
      {enabled && (
        <Text fontSize="xs" color="gray.600" whiteSpace="pre-line">
          {t('configuration.polymarketFilledHint', {
            url: gammaUrl || '—',
            interval: ps?.analysis_interval ?? 300,
          })}
        </Text>
      )}
    </ConfigCard>
  )
}

export const MacroEventConfigSection: React.FC<{
  config: Config
  updateConfigField: (path: string, value: unknown) => void
}> = ({ config, updateConfigField }) => {
  const { t } = useTranslation()
  const me = config.macro_event
  const enabled = me?.enabled ?? false
  const interval = me?.fetch_interval ?? 300
  const gammaUrl = me?.gamma_api_url ?? ''

  return (
    <ConfigCard title={t('configuration.macroEventSectionTitle')} icon={<StarIcon />}>
      <Text fontSize="xs" color="gray.500" mb={4}>{t('configuration.macroEventSectionDesc')}</Text>
      <Flex justify="space-between" align="center" mb={4}>
        <Box>
          <Text fontWeight="600">{t('configuration.macroEventEnable')}</Text>
          <Text fontSize="xs" color="gray.500">{t('configuration.macroEventEnableDesc')}</Text>
        </Box>
        <Switch
          colorScheme="purple"
          isChecked={enabled}
          onChange={(e) => updateConfigField('macro_event.enabled', e.target.checked)}
        />
      </Flex>
      <FormControl mb={4}>
        <FormLabel fontSize="xs" fontWeight="bold">{t('configuration.macroEventFetchInterval')}</FormLabel>
        <NumberInput
          value={interval}
          min={60}
          max={86400}
          step={60}
          onChange={(_, v) => updateConfigField('macro_event.fetch_interval', v ?? 300)}
        >
          <NumberInputField borderRadius="xl" />
          <NumberInputStepper>
            <NumberIncrementStepper />
            <NumberDecrementStepper />
          </NumberInputStepper>
        </NumberInput>
        <Text fontSize="xs" color="gray.500" mt={1}>{t('configuration.macroEventFetchIntervalDesc')}</Text>
      </FormControl>
      {gammaUrl ? (
        <Text fontSize="xs" color="gray.600" mb={2}>{t('configuration.macroEventGammaUrl')}: {gammaUrl}</Text>
      ) : null}
      <Alert status="info" borderRadius="lg">
        <AlertIcon />
        <Text fontSize="xs">{t('configuration.macroEventRestartHint')}</Text>
      </Alert>
    </ConfigCard>
  )
}

export const ConfigCard: React.FC<{ title: string; children: React.ReactNode; icon?: React.ReactNode; headerRight?: React.ReactNode }> = ({
  title,
  children,
  icon,
  headerRight,
}) => {
  const bg = 'white'
  const borderColor = 'gray.100'

  return (
    <Box
      bg={bg}
      p={6}
      borderRadius="2xl"
      border="1px"
      borderColor={borderColor}
      boxShadow="sm"
      mb={6}
    >
      <HStack mb={5} spacing={3} justify="space-between" align="center">
        <HStack spacing={3} minW={0}>
          {icon && <Box color="blue.500" flexShrink={0}>{icon}</Box>}
          <Heading size="sm" fontWeight="600" noOfLines={1}>{title}</Heading>
        </HStack>
        {headerRight}
      </HStack>
      <VStack spacing={5} align="stretch">
        {children}
      </VStack>
    </Box>
  )
}
