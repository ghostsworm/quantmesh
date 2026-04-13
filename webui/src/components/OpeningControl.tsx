import React, { useEffect, useState } from 'react'
import {
  Box,
  Heading,
  Card,
  CardBody,
  CardHeader,
  Switch,
  FormControl,
  FormLabel,
  FormHelperText,
  Input,
  NumberInput,
  NumberInputField,
  NumberInputStepper,
  NumberIncrementStepper,
  NumberDecrementStepper,
  Button,
  Text,
  Spinner,
  Center,
  Flex,
  useToast,
  Divider,
  Wrap,
  WrapItem,
  Checkbox,
  Select,
} from '@chakra-ui/react'
import { useTranslation } from 'react-i18next'
import { useSymbol } from '../contexts/SymbolContext'
import { useBot } from '../contexts/BotContext'
import SymbolSelector from './SymbolSelector'
import {
  getOpeningControlStatus,
  pauseOpening,
  resumeOpening,
  updateOpeningControlConfig,
  OpeningControlStatus,
  OpenPositionControlConfig,
  ScheduleRule,
  PeriodicRule,
} from '../services/api'

const WEEKDAYS = [
  { value: 0, labelKey: 'openingControl.weekday0' },
  { value: 1, labelKey: 'openingControl.weekday1' },
  { value: 2, labelKey: 'openingControl.weekday2' },
  { value: 3, labelKey: 'openingControl.weekday3' },
  { value: 4, labelKey: 'openingControl.weekday4' },
  { value: 5, labelKey: 'openingControl.weekday5' },
  { value: 6, labelKey: 'openingControl.weekday6' },
]

const OpeningControl: React.FC = () => {
  const { t } = useTranslation()
  const toast = useToast()
  const { selectedExchange, selectedSymbol, selectedMarketType } = useSymbol()
  const { botId } = useBot()

  const [status, setStatus] = useState<OpeningControlStatus | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [toggling, setToggling] = useState(false)
  const [saving, setSaving] = useState(false)

  // Form state for config
  const [maxPositionValue, setMaxPositionValue] = useState<string>('')
  const [maxPositionLayers, setMaxPositionLayers] = useState<string>('')
  const [scheduleRules, setScheduleRules] = useState<ScheduleRule[]>([])
  const [periodicEnabled, setPeriodicEnabled] = useState(false)
  const [openDurationMin, setOpenDurationMin] = useState<string>('60')
  const [closeDurationMin, setCloseDurationMin] = useState<string>('30')

  const hasSelection = !!selectedExchange && !!selectedSymbol

  const fetchStatus = async () => {
    if (!selectedExchange || !selectedSymbol) {
      setStatus(null)
      setLoading(false)
      return
    }
    try {
      setLoading(true)
      const data = await getOpeningControlStatus(
        selectedExchange,
        selectedSymbol,
        selectedMarketType ?? undefined,
        botId ?? undefined
      )
      setStatus(data)
      setError(null)

      setMaxPositionValue(data.config.max_position_value ? String(data.config.max_position_value) : '')
      setMaxPositionLayers(data.config.max_position_layers ? String(data.config.max_position_layers) : '')
      setScheduleRules(data.config.schedule_rules || [])
      if (data.config.periodic_rule) {
        setPeriodicEnabled(data.config.periodic_rule.enabled)
        setOpenDurationMin(String(data.config.periodic_rule.open_duration_min || 60))
        setCloseDurationMin(String(data.config.periodic_rule.close_duration_min || 30))
      } else {
        setPeriodicEnabled(false)
        setOpenDurationMin('60')
        setCloseDurationMin('30')
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch status')
      setStatus(null)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchStatus()
    const interval = setInterval(fetchStatus, 10000)
    return () => clearInterval(interval)
  }, [selectedExchange, selectedSymbol, selectedMarketType, botId])

  const handleToggleOpening = async () => {
    if (!selectedExchange || !selectedSymbol) return
    setToggling(true)
    try {
      if (status?.opening_paused) {
        await resumeOpening(selectedExchange, selectedSymbol, selectedMarketType ?? undefined, botId ?? undefined)
        toast({ title: t('openingControl.resumeSuccess'), status: 'success', duration: 2000 })
      } else {
        await pauseOpening(selectedExchange, selectedSymbol, selectedMarketType ?? undefined, botId ?? undefined)
        toast({ title: t('openingControl.pauseSuccess'), status: 'info', duration: 2000 })
      }
      await fetchStatus()
    } catch (err) {
      toast({ title: err instanceof Error ? err.message : 'Failed', status: 'error' })
    } finally {
      setToggling(false)
    }
  }

  const handleSaveConfig = async () => {
    if (!selectedExchange || !selectedSymbol) return
    setSaving(true)
    try {
      const cfg: Partial<OpenPositionControlConfig> = {
        max_position_value: maxPositionValue ? parseFloat(maxPositionValue) : 0,
        max_position_layers: maxPositionLayers ? parseInt(maxPositionLayers, 10) : 0,
        schedule_rules: scheduleRules,
        periodic_rule: periodicEnabled
          ? { enabled: true, open_duration_min: parseInt(openDurationMin, 10) || 60, close_duration_min: parseInt(closeDurationMin, 10) || 30 }
          : { enabled: false, open_duration_min: 60, close_duration_min: 30 },
      }
      await updateOpeningControlConfig(
        selectedExchange,
        selectedSymbol,
        cfg,
        selectedMarketType ?? undefined,
        botId ?? undefined
      )
      toast({ title: t('openingControl.configSaved'), status: 'success', duration: 2000 })
      await fetchStatus()
    } catch (err) {
      toast({ title: err instanceof Error ? err.message : 'Failed', status: 'error' })
    } finally {
      setSaving(false)
    }
  }

  const addScheduleRule = () => {
    setScheduleRules([...scheduleRules, { enabled: true, action: 'pause', time: '22:00', weekdays: [] }])
  }

  const updateScheduleRule = (index: number, field: keyof ScheduleRule, value: unknown) => {
    const next = [...scheduleRules]
    ;(next[index] as Record<string, unknown>)[field] = value
    setScheduleRules(next)
  }

  const removeScheduleRule = (index: number) => {
    setScheduleRules(scheduleRules.filter((_, i) => i !== index))
  }

  if (!hasSelection) {
    return (
      <Box p={4}>
        <Heading size="lg" mb={4}>
          {t('openingControl.title')}
        </Heading>
        <Text mb={4} color="gray.600">
          {t('openingControl.selectSymbolHint')}
        </Text>
        <SymbolSelector />
      </Box>
    )
  }

  return (
    <Box p={4}>
      <Flex justify="space-between" align="center" mb={4}>
        <Heading size="lg">{t('openingControl.title')}</Heading>
        <SymbolSelector />
      </Flex>

      {loading ? (
        <Center py={8}>
          <Spinner />
        </Center>
      ) : error ? (
        <Text color="red.500">{error}</Text>
      ) : status ? (
        <>
          {/* 狀態卡片 */}
          <Card mb={4}>
            <CardHeader>
              <Heading size="md">{t('openingControl.statusCard')}</Heading>
            </CardHeader>
            <CardBody>
              <Flex align="center" gap={4} flexWrap="wrap">
                <FormControl display="flex" alignItems="center" w="auto">
                  <FormLabel mb={0} mr={2}>
                    {t('openingControl.manualControl')}
                  </FormLabel>
                  <Switch
                    isChecked={!status.opening_paused}
                    onChange={handleToggleOpening}
                    isDisabled={toggling || status.pause_reason === 'bot_stopped'}
                    colorScheme="green"
                  />
                </FormControl>
                <Text color={status.opening_paused ? 'orange.600' : 'green.600'} fontWeight="medium">
                  {status.opening_paused
                    ? t('openingControl.paused') + (status.pause_reason ? ` (${status.pause_reason === 'bot_stopped' ? t('openingControl.botStopped') : status.pause_reason})` : '')
                    : t('openingControl.opening')}
                </Text>
              </Flex>
              <Flex mt={4} gap={6} flexWrap="wrap">
                <Box>
                  <Text fontSize="sm" color="gray.500">
                    {t('openingControl.currentPositionValue')}
                  </Text>
                  <Text fontWeight="bold">{status.current_position_value_usdt.toFixed(2)} USDT</Text>
                </Box>
                <Box>
                  <Text fontSize="sm" color="gray.500">
                    {t('openingControl.currentLayers')}
                  </Text>
                  <Text fontWeight="bold">{status.current_layers}</Text>
                </Box>
              </Flex>
            </CardBody>
          </Card>

          {/* 限倉設置 */}
          <Card mb={4}>
            <CardHeader>
              <Heading size="md">{t('openingControl.limitCard')}</Heading>
            </CardHeader>
            <CardBody>
              <Flex gap={6} flexWrap="wrap">
                <FormControl maxW="200px">
                  <FormLabel>{t('openingControl.maxPositionValue')}</FormLabel>
                  <NumberInput value={maxPositionValue} onChange={(_, v) => setMaxPositionValue(v || '')} min={0}>
                    <NumberInputField placeholder="0 = " />
                    <NumberInputStepper>
                      <NumberIncrementStepper />
                      <NumberDecrementStepper />
                    </NumberInputStepper>
                  </NumberInput>
                  <FormHelperText>{t('openingControl.maxPositionValueHint')}</FormHelperText>
                </FormControl>
                <FormControl maxW="200px">
                  <FormLabel>{t('openingControl.maxPositionLayers')}</FormLabel>
                  <NumberInput value={maxPositionLayers} onChange={(_, v) => setMaxPositionLayers(v || '')} min={0}>
                    <NumberInputField placeholder="0 = " />
                    <NumberInputStepper>
                      <NumberIncrementStepper />
                      <NumberDecrementStepper />
                    </NumberInputStepper>
                  </NumberInput>
                  <FormHelperText>{t('openingControl.maxPositionLayersHint')}</FormHelperText>
                </FormControl>
              </Flex>
            </CardBody>
          </Card>

          {/* 定時規則 */}
          <Card mb={4}>
            <CardHeader>
              <Heading size="md">{t('openingControl.scheduleCard')}</Heading>
            </CardHeader>
            <CardBody>
              {scheduleRules.map((rule, i) => (
                <Box key={i} mb={4} p={3} borderWidth={1} borderRadius="md">
                  <Flex gap={4} flexWrap="wrap" align="center">
                    <FormControl display="flex" alignItems="center" w="auto">
                      <Switch
                        isChecked={rule.enabled}
                        onChange={(e) => updateScheduleRule(i, 'enabled', e.target.checked)}
                      />
                    </FormControl>
                    <FormControl maxW="120px">
                      <FormLabel fontSize="sm">{t('openingControl.action')}</FormLabel>
                      <Select
                        size="sm"
                        value={rule.action}
                        onChange={(e) => updateScheduleRule(i, 'action', e.target.value)}
                      >
                        <option value="pause">{t('openingControl.actionPause')}</option>
                        <option value="resume">{t('openingControl.actionResume')}</option>
                      </Select>
                    </FormControl>
                    <FormControl maxW="100px">
                      <FormLabel fontSize="sm">{t('openingControl.timeUTC')}</FormLabel>
                      <Input
                        type="time"
                        value={rule.time || '22:00'}
                        onChange={(e) => updateScheduleRule(i, 'time', e.target.value)}
                        size="sm"
                      />
                    </FormControl>
                    <Box>
                      <FormLabel fontSize="sm">{t('openingControl.weekdays')}</FormLabel>
                      <Wrap spacing={1}>
                        {WEEKDAYS.map(({ value, labelKey }) => (
                          <WrapItem key={value}>
                            <Checkbox
                              size="sm"
                              isChecked={rule.weekdays?.includes(value) ?? false}
                              onChange={(e) => {
                                const wd = rule.weekdays || []
                                const next = e.target.checked
                                  ? [...wd, value]
                                  : wd.filter((x) => x !== value)
                                updateScheduleRule(i, 'weekdays', next.length ? next : undefined)
                              }}
                            >
                              {t(labelKey)}
                            </Checkbox>
                          </WrapItem>
                        ))}
                      </Wrap>
                    </Box>
                    <Button size="sm" colorScheme="red" variant="outline" onClick={() => removeScheduleRule(i)}>
                      {t('common.delete')}
                    </Button>
                  </Flex>
                </Box>
              ))}
              <Button size="sm" onClick={addScheduleRule}>
                {t('openingControl.addScheduleRule')}
              </Button>
            </CardBody>
          </Card>

          {/* 週期規則 */}
          <Card mb={4}>
            <CardHeader>
              <Heading size="md">{t('openingControl.periodicCard')}</Heading>
            </CardHeader>
            <CardBody>
              <FormControl display="flex" alignItems="center" mb={4}>
                <FormLabel mb={0} mr={2}>
                  {t('openingControl.periodicEnabled')}
                </FormLabel>
                <Switch isChecked={periodicEnabled} onChange={(e) => setPeriodicEnabled(e.target.checked)} />
              </FormControl>
              {periodicEnabled && (
                <Flex gap={6} flexWrap="wrap">
                  <FormControl maxW="180px">
                    <FormLabel>{t('openingControl.openDurationMin')}</FormLabel>
                    <NumberInput value={openDurationMin} onChange={(_, v) => setOpenDurationMin(v || '60')} min={1}>
                      <NumberInputField />
                      <NumberInputStepper>
                        <NumberIncrementStepper />
                        <NumberDecrementStepper />
                      </NumberInputStepper>
                    </NumberInput>
                    <FormHelperText>{t('openingControl.minutes')}</FormHelperText>
                  </FormControl>
                  <FormControl maxW="180px">
                    <FormLabel>{t('openingControl.closeDurationMin')}</FormLabel>
                    <NumberInput value={closeDurationMin} onChange={(_, v) => setCloseDurationMin(v || '30')} min={1}>
                      <NumberInputField />
                      <NumberInputStepper>
                        <NumberIncrementStepper />
                        <NumberDecrementStepper />
                      </NumberInputStepper>
                    </NumberInput>
                    <FormHelperText>{t('openingControl.minutes')}</FormHelperText>
                  </FormControl>
                </Flex>
              )}
            </CardBody>
          </Card>

          <Button colorScheme="blue" onClick={handleSaveConfig} isLoading={saving}>
            {t('common.save')}
          </Button>
        </>
      ) : null}
    </Box>
  )
}

export default OpeningControl
