import React, { useCallback, useEffect, useMemo, useState, useRef } from 'react'
import {
  Box,
  Button,
  Card,
  CardBody,
  Flex,
  Heading,
  HStack,
  Spinner,
  Text,
  Badge,
  useToast,
  SimpleGrid,
  FormControl,
  FormLabel,
  Switch,
  NumberInput,
  NumberInputField,
  NumberInputStepper,
  NumberIncrementStepper,
  NumberDecrementStepper,
  Divider,
  VStack,
  Alert,
  AlertIcon,
  IconButton,
  Collapse,
  useDisclosure,
} from '@chakra-ui/react'
import { ChevronDownIcon, ChevronUpIcon, PauseIcon, PlayIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import {
  getBotRiskControl,
  updateBotRiskControl,
  getBotPositionStatus,
  pauseBotOpening,
  resumeBotOpening,
  BotRiskControl as BotRiskControlType,
  PositionStatus,
} from '../services/api'

interface BotRiskControlPanelProps {
  botId: string
  botRunning: boolean
}

const BotRiskControlPanel: React.FC<BotRiskControlPanelProps> = ({ botId, botRunning }) => {
  const { t } = useTranslation()
  const toast = useToast()
  const { isOpen: showConfig, onToggle: toggleConfig } = useDisclosure()

  const [riskControl, setRiskControl] = useState<BotRiskControlType | null>(null)
  const [positionStatus, setPositionStatus] = useState<PositionStatus | null>(null)
  const [loading, setLoading] = useState(true)
  const [updating, setUpdating] = useState(false)
  const [pausing, setPausing] = useState(false)

  // 使用 ref 避免依赖项变化导致定时器重建
  const botIdRef = useRef(botId)

  // 当 botId 变化时更新 ref
  useEffect(() => {
    botIdRef.current = botId
  }, [botId])

  // 使用 useCallback 缓存函数，避免不必要的重新渲染
  const fetchRiskControl = useCallback(async () => {
    const currentBotId = botIdRef.current
    if (!currentBotId) return

    try {
      const [rc, ps] = await Promise.all([
        getBotRiskControl(currentBotId),
        getBotPositionStatus(currentBotId),
      ])
      setRiskControl(rc)
      setPositionStatus(ps)
    } catch (err) {
      console.error('Failed to fetch risk control:', err)
      toast({ title: t('botRiskControl.positionStatusFailed'), status: 'error', duration: 3000 })
    } finally {
      setLoading(false)
    }
  }, [t, toast])

  useEffect(() => {
    fetchRiskControl()
    // 每 10 秒刷新一次仓位状态
    const interval = setInterval(fetchRiskControl, 10000)
    return () => clearInterval(interval)
  }, [fetchRiskControl])

  const handleUpdateConfig = useCallback(async () => {
    if (!riskControl || !botIdRef.current) return
    setUpdating(true)
    try {
      await updateBotRiskControl(botIdRef.current, riskControl)
      toast({ title: t('botRiskControl.configUpdateSuccess'), status: 'success', duration: 2000 })
      await fetchRiskControl()
    } catch (err) {
      console.error('Failed to update risk control:', err)
      toast({ title: t('botRiskControl.configUpdateFailed'), status: 'error', duration: 3000 })
    } finally {
      setUpdating(false)
    }
  }, [riskControl, fetchRiskControl, t, toast])

  const handlePauseOpening = useCallback(async () => {
    if (!botIdRef.current) return
    setPausing(true)
    try {
      await pauseBotOpening(botIdRef.current, '手动暂停')
      toast({ title: t('botRiskControl.pauseSuccess'), status: 'success', duration: 2000 })
      await fetchRiskControl()
    } catch (err) {
      console.error('Failed to pause opening:', err)
      toast({ title: t('botRiskControl.configUpdateFailed'), status: 'error', duration: 3000 })
    } finally {
      setPausing(false)
    }
  }, [fetchRiskControl, t, toast])

  const handleResumeOpening = useCallback(async () => {
    if (!botIdRef.current) return
    setPausing(true)
    try {
      await resumeBotOpening(botIdRef.current)
      toast({ title: t('botRiskControl.resumeSuccess'), status: 'success', duration: 2000 })
      await fetchRiskControl()
    } catch (err) {
      console.error('Failed to resume opening:', err)
      toast({ title: t('botRiskControl.configUpdateFailed'), status: 'error', duration: 3000 })
    } finally {
      setPausing(false)
    }
  }, [fetchRiskControl, t, toast])

  // 使用 useMemo 缓存计算值，避免不必要的重新计算
  const shouldStopOpening = useMemo(() => {
    return positionStatus?.should_stop_opening || false
  }, [positionStatus?.should_stop_opening])

  const isPaused = useMemo(() => {
    return positionStatus?.paused || false
  }, [positionStatus?.paused])

  // 缓存配置更新处理函数，避免子组件不必要的重新渲染
  const updateConfigField = useCallback(<K extends keyof BotRiskControlType>(
    key: K,
    value: BotRiskControlType[K]
  ) => {
    setRiskControl(prev => prev ? { ...prev, [key]: value } : null)
  }, [])

  if (loading) {
    return (
      <Flex justify="center" align="center" minH="200px">
        <Spinner size="lg" />
      </Flex>
    )
  }

  return (
    <VStack spacing={4} align="stretch">
      {/* 当前状态卡片 */}
      <Card>
        <CardBody>
          <Heading size="sm" mb={4}>{t('botRiskControl.currentStatus')}</Heading>
          <SimpleGrid columns={{ base: 1, md: 2, lg: 4 }} spacing={4} mb={4}>
            <Box>
              <Text fontSize="sm" color="gray.500">{t('botRiskControl.totalPositionQty')}</Text>
              <Text fontSize="lg" fontWeight="bold">
                {positionStatus?.total_position_qty?.toFixed(4) || '-'}
                {positionStatus?.max_position_qty && (
                  <Text as="span" fontSize="sm" color="gray.500">
                    {' '} / {positionStatus.max_position_qty}
                  </Text>
                )}
              </Text>
              {positionStatus?.reached_limit_qty && (
                <Badge colorScheme="red" size="sm">{t('botRiskControl.reachedLimitQty')}</Badge>
              )}
            </Box>
            <Box>
              <Text fontSize="sm" color="gray.500">{t('botRiskControl.totalPositionValue')}</Text>
              <Text fontSize="lg" fontWeight="bold">
                ${positionStatus?.total_position_value?.toFixed(2) || '-'}
                {positionStatus?.max_position_value && (
                  <Text as="span" fontSize="sm" color="gray.500">
                    {' '} / ${positionStatus.max_position_value}
                  </Text>
                )}
              </Text>
              {positionStatus?.reached_limit_value && (
                <Badge colorScheme="red" size="sm">{t('botRiskControl.reachedLimitValue')}</Badge>
              )}
            </Box>
            <Box>
              <Text fontSize="sm" color="gray.500">{t('botRiskControl.positionLayers')}</Text>
              <Text fontSize="lg" fontWeight="bold">
                {positionStatus?.position_layers || '-'}
                {positionStatus?.max_position_layers && (
                  <Text as="span" fontSize="sm" color="gray.500">
                    {' '} / {positionStatus.max_position_layers}
                  </Text>
                )}
              </Text>
              {positionStatus?.reached_limit_layers && (
                <Badge colorScheme="red" size="sm">{t('botRiskControl.reachedLimitLayers')}</Badge>
              )}
            </Box>
            <Box>
              <Text fontSize="sm" color="gray.500">{t('botRiskControl.currentPrice')}</Text>
              <Text fontSize="lg" fontWeight="bold">
                ${positionStatus?.current_price?.toFixed(2) || '-'}
              </Text>
              {isPaused && (
                <Badge colorScheme="orange" size="sm">{t('botRiskControl.paused')}</Badge>
              )}
            </Box>
          </SimpleGrid>

          {shouldStopOpening && (
            <Alert status="warning" borderRadius="md">
              <AlertIcon />
              <Text fontSize="sm">
                {t('botRiskControl.shouldStopOpening')}
                {isPaused && ` (${t('botRiskControl.paused')})`}
              </Text>
            </Alert>
          )}

          {/* 暂停/恢复按钮 */}
          <Flex justify="flex-end" mt={4}>
            {botRunning && (
              isPaused ? (
                <Button
                  size="sm"
                  colorScheme="green"
                  leftIcon={<PlayIcon />}
                  onClick={handleResumeOpening}
                  isLoading={pausing}
                >
                  {t('botRiskControl.resume')}
                </Button>
              ) : (
                <Button
                  size="sm"
                  colorScheme="orange"
                  leftIcon={<PauseIcon />}
                  onClick={handlePauseOpening}
                  isLoading={pausing}
                >
                  {t('botRiskControl.pause')}
                </Button>
              )
            )}
          </Flex>
        </CardBody>
      </Card>

      {/* 风控配置 */}
      <Card>
        <CardBody>
          <Flex justify="space-between" align="center" mb={4} cursor="pointer" onClick={toggleConfig}>
            <Heading size="sm">{t('botRiskControl.title')}</Heading>
            <IconButton
              aria-label="Toggle"
              icon={showConfig ? <ChevronUpIcon /> : <ChevronDownIcon />}
              size="sm"
              variant="ghost"
            />
          </Flex>

          <Collapse in={showConfig} animateOpacity>
            {riskControl && (
              <VStack spacing={4} align="stretch">
                <FormControl display="flex" alignItems="center">
                  <FormLabel htmlFor="enabled" mb="0" flex="1">
                    {t('botRiskControl.enabled')}
                  </FormLabel>
                  <Switch
                    id="enabled"
                    isChecked={riskControl.enabled ?? false}
                    onChange={(e) => updateConfigField('enabled', e.target.checked)}
                  />
                </FormControl>
                <Text fontSize="xs" color="gray.500">{t('botRiskControl.enabledDesc')}</Text>

                <Divider />
                <Heading size="xs" textTransform="uppercase" color="gray.500">
                  {t('botRiskControl.positionLimits')}
                </Heading>

                <SimpleGrid columns={{ base: 1, md: 2 }} spacing={4}>
                  <FormControl>
                    <FormLabel fontSize="sm">{t('botRiskControl.maxPositionQty')}</FormLabel>
                    <NumberInput
                      value={riskControl.max_position_qty ?? 0}
                      onChange={(_, val) => updateConfigField('max_position_qty', val)}
                      min={0}
                      precision={4}
                    >
                      <NumberInputField />
                      <NumberInputStepper>
                        <NumberIncrementStepper />
                        <NumberDecrementStepper />
                      </NumberInputStepper>
                    </NumberInput>
                    <Text fontSize="xs" color="gray.500">{t('botRiskControl.maxPositionQtyDesc')}</Text>
                  </FormControl>

                  <FormControl>
                    <FormLabel fontSize="sm">{t('botRiskControl.maxPositionValue')}</FormLabel>
                    <NumberInput
                      value={riskControl.max_position_value ?? 0}
                      onChange={(_, val) => updateConfigField('max_position_value', val)}
                      min={0}
                      precision={2}
                    >
                      <NumberInputField />
                      <NumberInputStepper>
                        <NumberIncrementStepper />
                        <NumberDecrementStepper />
                      </NumberInputStepper>
                    </NumberInput>
                    <Text fontSize="xs" color="gray.500">{t('botRiskControl.maxPositionValueDesc')}</Text>
                  </FormControl>

                  <FormControl>
                    <FormLabel fontSize="sm">{t('botRiskControl.maxPositionLayers')}</FormLabel>
                    <NumberInput
                      value={riskControl.max_position_layers ?? 0}
                      onChange={(_, val) => updateConfigField('max_position_layers', val)}
                      min={0}
                    >
                      <NumberInputField />
                      <NumberInputStepper>
                        <NumberIncrementStepper />
                        <NumberDecrementStepper />
                      </NumberInputStepper>
                    </NumberInput>
                    <Text fontSize="xs" color="gray.500">{t('botRiskControl.maxPositionLayersDesc')}</Text>
                  </FormControl>
                </SimpleGrid>

                <Divider />
                <Heading size="xs" textTransform="uppercase" color="gray.500">
                  {t('botRiskControl.stopLossTakeProfit')}
                </Heading>

                <SimpleGrid columns={{ base: 1, md: 3 }} spacing={4}>
                  <FormControl>
                    <FormLabel fontSize="sm">{t('botRiskControl.stopLossRatio')} (%)</FormLabel>
                    <NumberInput
                      value={(riskControl.stop_loss_ratio ?? 0) * 100}
                      onChange={(_, val) => updateConfigField('stop_loss_ratio', val / 100)}
                      min={0}
                      max={100}
                      precision={2}
                    >
                      <NumberInputField />
                      <NumberInputStepper>
                        <NumberIncrementStepper />
                        <NumberDecrementStepper />
                      </NumberInputStepper>
                    </NumberInput>
                    <Text fontSize="xs" color="gray.500">{t('botRiskControl.stopLossRatioDesc')}</Text>
                  </FormControl>

                  <FormControl>
                    <FormLabel fontSize="sm">{t('botRiskControl.takeProfitRatio')} (%)</FormLabel>
                    <NumberInput
                      value={(riskControl.take_profit_ratio ?? 0) * 100}
                      onChange={(_, val) => updateConfigField('take_profit_ratio', val / 100)}
                      min={0}
                      max={100}
                      precision={2}
                    >
                      <NumberInputField />
                      <NumberInputStepper>
                        <NumberIncrementStepper />
                        <NumberDecrementStepper />
                      </NumberInputStepper>
                    </NumberInput>
                    <Text fontSize="xs" color="gray.500">{t('botRiskControl.takeProfitRatioDesc')}</Text>
                  </FormControl>

                  <FormControl>
                    <FormLabel fontSize="sm">{t('botRiskControl.trailingStopRatio')} (%)</FormLabel>
                    <NumberInput
                      value={(riskControl.trailing_stop_ratio ?? 0) * 100}
                      onChange={(_, val) => updateConfigField('trailing_stop_ratio', val / 100)}
                      min={0}
                      max={100}
                      precision={2}
                    >
                      <NumberInputField />
                      <NumberInputStepper>
                        <NumberIncrementStepper />
                        <NumberDecrementStepper />
                      </NumberInputStepper>
                    </NumberInput>
                    <Text fontSize="xs" color="gray.500">{t('botRiskControl.trailingStopRatioDesc')}</Text>
                  </FormControl>
                </SimpleGrid>

                <Divider />
                <Heading size="xs" textTransform="uppercase" color="gray.500">
                  {t('botRiskControl.pauseControl')}
                </Heading>

                <SimpleGrid columns={{ base: 1, md: 2 }} spacing={4}>
                  <FormControl>
                    <FormLabel fontSize="sm">{t('botRiskControl.autoResumeAfter')}</FormLabel>
                    <NumberInput
                      value={riskControl.auto_resume_after ?? 0}
                      onChange={(_, val) => updateConfigField('auto_resume_after', val)}
                      min={0}
                    >
                      <NumberInputField />
                      <NumberInputStepper>
                        <NumberIncrementStepper />
                        <NumberDecrementStepper />
                      </NumberInputStepper>
                    </NumberInput>
                    <Text fontSize="xs" color="gray.500">{t('botRiskControl.autoResumeAfterDesc')}</Text>
                  </FormControl>

                  <FormControl display="flex" alignItems="center">
                    <FormLabel htmlFor="trend-filter" mb="0" flex="1">
                      {t('botRiskControl.trendFilterEnabled')}
                    </FormLabel>
                    <Switch
                      id="trend-filter"
                      isChecked={riskControl.trend_filter_enabled ?? false}
                      onChange={(e) => updateConfigField('trend_filter_enabled', e.target.checked)}
                    />
                  </FormControl>
                </SimpleGrid>

                <Flex justify="flex-end">
                  <Button
                    colorScheme="blue"
                    onClick={handleUpdateConfig}
                    isLoading={updating}
                  >
                    {t('common.save')}
                  </Button>
                </Flex>
              </VStack>
            )}
          </Collapse>
        </CardBody>
      </Card>
    </VStack>
  )
}

export default BotRiskControlPanel
