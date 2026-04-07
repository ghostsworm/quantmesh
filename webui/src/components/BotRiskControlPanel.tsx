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
  Icon,
  IconButton,
  Collapse,
  useDisclosure,
  Link as ChakraLink,
} from '@chakra-ui/react'
import DecimalNumberInput from './DecimalNumberInput'
import { normalizeGridRiskControlPayload } from '../utils/gridRiskControlPayload'
import { ChevronDownIcon, ChevronUpIcon } from '@chakra-ui/icons'

// @chakra-ui/icons 不提供 PauseIcon/PlayIcon，使用自定义 SVG
const PlayIcon = (props: React.ComponentProps<typeof Icon>) => (
  <Icon viewBox="0 0 24 24" {...props}>
    <path fill="currentColor" d="M8 5v14l11-7z" />
  </Icon>
)
const PauseIcon = (props: React.ComponentProps<typeof Icon>) => (
  <Icon viewBox="0 0 24 24" {...props}>
    <path fill="currentColor" d="M6 19h4V5H6v14zm8-14v14h4V5h-4z" />
  </Icon>
)
import { Trans, useTranslation } from 'react-i18next'
import { Link as RouterLink } from 'react-router-dom'
import {
  getBotRiskControl,
  updateBotRiskControl,
  getBotPositionStatus,
  pauseBotOpening,
  resumeBotOpening,
  BotRiskControl as BotRiskControlType,
  PositionStatus,
} from '../services/api'
import { displayPauseOpeningReason } from '../utils/botPauseReason'

interface BotRiskControlPanelProps {
  botId: string
  botRunning: boolean
  /** 隱藏持倉狀態區塊（已移至概覽標籤） */
  hidePositionStatus?: boolean
  /** 與列表/詳情徽章一致：市場或深度風控是否觸發 */
  riskTriggered?: boolean
  /** 後端 RiskMonitor/DepthMonitor 的 lastMsg（多條分號拼接） */
  riskTriggerMessage?: string
}

const BotRiskControlPanel: React.FC<BotRiskControlPanelProps> = ({
  botId,
  botRunning,
  hidePositionStatus,
  riskTriggered,
  riskTriggerMessage,
}) => {
  const { t } = useTranslation()
  const toast = useToast()
  const { isOpen: showConfig, onToggle: toggleConfig } = useDisclosure({ defaultIsOpen: true })

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
      // 已停止的 Bot 由后端返回 200+stopped，不会进 catch；进 catch 多为网络等真实错误
      if (botRunning) {
        toast({ title: t('botRiskControl.positionStatusFailed'), status: 'error', duration: 3000 })
      }
      // 停止的 bot 不弹 toast，在仓位区域会显示 stoppedBotNoPosition
    } finally {
      setLoading(false)
    }
  }, [t, toast, botRunning])

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
      const toSend: BotRiskControlType = { ...riskControl }
      if (typeof toSend.stop_loss_ratio === 'string') toSend.stop_loss_ratio = parseFloat(toSend.stop_loss_ratio || '0') / 100
      if (typeof toSend.take_profit_ratio === 'string') toSend.take_profit_ratio = parseFloat(toSend.take_profit_ratio || '0') / 100
      if (typeof toSend.trailing_stop_ratio === 'string') toSend.trailing_stop_ratio = parseFloat(toSend.trailing_stop_ratio || '0') / 100
      const mpq = toSend.max_position_quantity ?? toSend.max_position_qty
      if (mpq != null) toSend.max_position_quantity = typeof mpq === 'string' ? parseFloat(mpq || '0') : mpq
      if (typeof toSend.max_position_value === 'string') toSend.max_position_value = parseFloat(toSend.max_position_value || '0')
      if (typeof toSend.open_order_distance === 'string') toSend.open_order_distance = parseFloat(toSend.open_order_distance || '0')
      // 網格風控比例轉換（前端用 % 顯示，後端用 0-1）；DecimalNumberInput 可能傳 string，需統一轉 number
      if (toSend.grid_risk_control) {
        toSend.grid_risk_control = normalizeGridRiskControlPayload(toSend.grid_risk_control) as typeof toSend.grid_risk_control
      }
      await updateBotRiskControl(botIdRef.current, toSend)
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

  const updateGridRiskControlField = useCallback(<K extends keyof NonNullable<BotRiskControlType['grid_risk_control']>>(
    key: K,
    value: NonNullable<BotRiskControlType['grid_risk_control']>[K]
  ) => {
    setRiskControl(prev => prev ? {
      ...prev,
      grid_risk_control: { ...(prev.grid_risk_control || {}), [key]: value },
    } : null)
  }, [])

  const marketRiskBanner = !!riskTriggered && (
    <Alert status="error" borderRadius="md" variant="subtle">
      <AlertIcon />
      <Box>
        <Text fontWeight="bold" fontSize="sm">{t('botRiskControl.riskTriggerBannerTitle')}</Text>
        <Text fontSize="sm" mt={1} whiteSpace="pre-wrap" wordBreak="break-word">
          {(riskTriggerMessage && riskTriggerMessage.trim()) ? riskTriggerMessage.trim() : t('botRiskControl.riskTriggerNoDetail')}
        </Text>
      </Box>
    </Alert>
  )

  const pauseOpeningBanner = riskControl?.pause_opening && (
    <Alert status="warning" borderRadius="md" variant="subtle">
      <AlertIcon />
      <Box>
        <Text fontWeight="bold" fontSize="sm">{t('botRiskControl.pauseOpeningBannerTitle')}</Text>
        <Text fontSize="sm" mt={1} whiteSpace="pre-wrap" wordBreak="break-word">
          {displayPauseOpeningReason(riskControl.pause_opening_reason, t)}
        </Text>
      </Box>
    </Alert>
  )

  const globalMarketRiskHintBanner = (
    <Alert status="info" borderRadius="md" variant="subtle">
      <AlertIcon />
      <Box fontSize="sm" lineHeight="tall">
        <Trans
          i18nKey="botRiskControl.globalMarketRiskHint"
          components={{
            configLink: (
              <ChakraLink as={RouterLink} to="/config" color="blue.500" fontWeight="600" />
            ),
          }}
        />
      </Box>
    </Alert>
  )

  if (loading) {
    return (
      <VStack spacing={4} align="stretch">
        {marketRiskBanner}
        {globalMarketRiskHintBanner}
        <Flex justify="center" align="center" minH="200px">
          <Spinner size="lg" />
        </Flex>
      </VStack>
    )
  }

  return (
    <VStack spacing={4} align="stretch">
      {marketRiskBanner}
      {pauseOpeningBanner}
      {globalMarketRiskHintBanner}
      {/* 当前状态卡片（可隱藏，已移至概覽） */}
      {!hidePositionStatus && (
      <Card>
        <CardBody>
          <Heading size="sm" mb={4}>{t('botRiskControl.currentStatus')}</Heading>
          {(positionStatus?.stopped || !botRunning) ? (
            <Alert status="info" borderRadius="md">
              <AlertIcon />
              <Text fontSize="sm">{t('botRiskControl.stoppedBotNoPosition')}</Text>
            </Alert>
          ) : (
          <>
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
              <Text fontSize="xs" color="gray.600">
                ${positionStatus?.total_position_value?.toFixed(2) || '-'}
              </Text>
            </Box>
            <Box>
              <Text fontSize="sm" color="gray.500">{t('botRiskControl.totalActualMargin')}</Text>
              <Text fontSize="lg" fontWeight="bold">
                ${positionStatus?.total_actual_margin?.toFixed(2) || '-'}
                {positionStatus?.max_position_value && (
                  <Text as="span" fontSize="sm" color="gray.500">
                    {' '} / ${positionStatus.max_position_value}
                  </Text>
                )}
              </Text>
              {positionStatus?.leverage && positionStatus.leverage > 1 && (
                <Text fontSize="xs" color="gray.500">
                  ({positionStatus.leverage}x杠杆)
                </Text>
              )}
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
            {botRunning && !positionStatus?.stopped && (
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
          </>
          )}
        </CardBody>
      </Card>
      )}

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
                    <DecimalNumberInput
                      value={riskControl.max_position_quantity ?? riskControl.max_position_qty ?? 0}
                      onChange={(val) => updateConfigField('max_position_quantity', val)}
                      min={0}
                      precision={4}
                      step={0.0001}
                      showStepper
                    />
                    <Text fontSize="xs" color="gray.500">{t('botRiskControl.maxPositionQtyDesc')}</Text>
                  </FormControl>

                  <FormControl>
                    <FormLabel fontSize="sm">{t('botRiskControl.maxPositionValue')}</FormLabel>
                    <DecimalNumberInput
                      value={riskControl.max_position_value ?? 0}
                      onChange={(val) => updateConfigField('max_position_value', val)}
                      min={0}
                      precision={2}
                      step={0.01}
                      showStepper
                    />
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

                  <FormControl>
                    <FormLabel fontSize="sm">{t('botRiskControl.maxOpenOrders')}</FormLabel>
                    <NumberInput
                      value={riskControl.max_open_orders ?? 0}
                      onChange={(_, val) => updateConfigField('max_open_orders', val)}
                      min={0}
                    >
                      <NumberInputField />
                      <NumberInputStepper>
                        <NumberIncrementStepper />
                        <NumberDecrementStepper />
                      </NumberInputStepper>
                    </NumberInput>
                    <Text fontSize="xs" color="gray.500">{t('botRiskControl.maxOpenOrdersDesc')}</Text>
                  </FormControl>

                  <FormControl>
                    <FormLabel fontSize="sm">{t('botRiskControl.openOrderDistance')}</FormLabel>
                    <DecimalNumberInput
                      value={riskControl.open_order_distance ?? 0}
                      onChange={(val) => updateConfigField('open_order_distance', val)}
                      min={0}
                      precision={1}
                      step={0.1}
                      showStepper
                    />
                    <Text fontSize="xs" color="gray.500">{t('botRiskControl.openOrderDistanceDesc')}</Text>
                  </FormControl>
                </SimpleGrid>

                <Divider />
                <Heading size="xs" textTransform="uppercase" color="gray.500">
                  {t('botRiskControl.stopLossTakeProfit')}
                </Heading>

                <SimpleGrid columns={{ base: 1, md: 3 }} spacing={4}>
                  <FormControl>
                    <FormLabel fontSize="sm">{t('botRiskControl.stopLossRatio')} (%)</FormLabel>
                    <DecimalNumberInput
                      value={typeof riskControl.stop_loss_ratio === 'string' ? riskControl.stop_loss_ratio : (riskControl.stop_loss_ratio ?? 0) * 100}
                      onChange={(val) => updateConfigField('stop_loss_ratio', typeof val === 'number' ? val / 100 : val)}
                      min={0}
                      max={100}
                      precision={2}
                      step={0.01}
                      showStepper
                    />
                    <Text fontSize="xs" color="gray.500">{t('botRiskControl.stopLossRatioDesc')}</Text>
                  </FormControl>

                  <FormControl>
                    <FormLabel fontSize="sm">{t('botRiskControl.takeProfitRatio')} (%)</FormLabel>
                    <DecimalNumberInput
                      value={typeof riskControl.take_profit_ratio === 'string' ? riskControl.take_profit_ratio : (riskControl.take_profit_ratio ?? 0) * 100}
                      onChange={(val) => updateConfigField('take_profit_ratio', typeof val === 'number' ? val / 100 : val)}
                      min={0}
                      max={100}
                      precision={2}
                      step={0.01}
                      showStepper
                    />
                    <Text fontSize="xs" color="gray.500">{t('botRiskControl.takeProfitRatioDesc')}</Text>
                  </FormControl>

                  <FormControl>
                    <FormLabel fontSize="sm">{t('botRiskControl.trailingStopRatio')} (%)</FormLabel>
                    <DecimalNumberInput
                      value={typeof riskControl.trailing_stop_ratio === 'string' ? riskControl.trailing_stop_ratio : (riskControl.trailing_stop_ratio ?? 0) * 100}
                      onChange={(val) => updateConfigField('trailing_stop_ratio', typeof val === 'number' ? val / 100 : val)}
                      min={0}
                      max={100}
                      precision={2}
                      step={0.01}
                      showStepper
                    />
                    <Text fontSize="xs" color="gray.500">{t('botRiskControl.trailingStopRatioDesc')}</Text>
                  </FormControl>
                </SimpleGrid>

                <Divider />
                <Heading size="xs" textTransform="uppercase" color="gray.500">
                  {t('botRiskControl.gridRiskControl')}
                </Heading>
                <Text fontSize="xs" color="gray.500">{t('botRiskControl.gridRiskControlDesc')}</Text>
                <SimpleGrid columns={{ base: 1, md: 2, lg: 3 }} spacing={4}>
                  <FormControl display="flex" alignItems="center">
                    <FormLabel htmlFor="grc-enabled" mb="0" flex="1">
                      {t('botRiskControl.gridRiskControlEnabled')}
                    </FormLabel>
                    <Switch
                      id="grc-enabled"
                      isChecked={riskControl.grid_risk_control?.enabled ?? false}
                      onChange={(e) => updateGridRiskControlField('enabled', e.target.checked)}
                    />
                  </FormControl>
                  <FormControl>
                    <FormLabel fontSize="sm">{t('botRiskControl.gridStopLossRatio')} (%)</FormLabel>
                    <DecimalNumberInput
                      value={typeof riskControl.grid_risk_control?.stop_loss_ratio === 'number'
                        ? (riskControl.grid_risk_control.stop_loss_ratio <= 1 ? riskControl.grid_risk_control.stop_loss_ratio * 100 : riskControl.grid_risk_control.stop_loss_ratio)
                        : 0}
                      onChange={(val) => updateGridRiskControlField('stop_loss_ratio', typeof val === 'number' ? val / 100 : val)}
                      min={0}
                      max={100}
                      precision={2}
                      step={0.5}
                      showStepper
                    />
                    <Text fontSize="xs" color="gray.500">{t('botRiskControl.gridStopLossRatioDesc')}</Text>
                  </FormControl>
                  <FormControl>
                    <FormLabel fontSize="sm">{t('botRiskControl.gridTakeProfitTrigger')} (%)</FormLabel>
                    <DecimalNumberInput
                      value={typeof riskControl.grid_risk_control?.take_profit_trigger_ratio === 'number'
                        ? (riskControl.grid_risk_control.take_profit_trigger_ratio <= 1 ? riskControl.grid_risk_control.take_profit_trigger_ratio * 100 : riskControl.grid_risk_control.take_profit_trigger_ratio)
                        : 0}
                      onChange={(val) => updateGridRiskControlField('take_profit_trigger_ratio', typeof val === 'number' ? val / 100 : val)}
                      min={0}
                      max={100}
                      precision={2}
                      step={0.5}
                      showStepper
                    />
                  </FormControl>
                  <FormControl>
                    <FormLabel fontSize="sm">{t('botRiskControl.gridTrailingTakeProfit')} (%)</FormLabel>
                    <DecimalNumberInput
                      value={typeof riskControl.grid_risk_control?.trailing_take_profit_ratio === 'number'
                        ? (riskControl.grid_risk_control.trailing_take_profit_ratio <= 1 ? riskControl.grid_risk_control.trailing_take_profit_ratio * 100 : riskControl.grid_risk_control.trailing_take_profit_ratio)
                        : 0}
                      onChange={(val) => updateGridRiskControlField('trailing_take_profit_ratio', typeof val === 'number' ? val / 100 : val)}
                      min={0}
                      max={100}
                      precision={2}
                      step={0.5}
                      showStepper
                    />
                  </FormControl>
                  <FormControl>
                    <FormLabel fontSize="sm">{t('botRiskControl.gridMaxLayers')}</FormLabel>
                    <NumberInput
                      value={riskControl.grid_risk_control?.max_grid_layers ?? 0}
                      onChange={(_, val) => updateGridRiskControlField('max_grid_layers', val)}
                      min={0}
                    >
                      <NumberInputField />
                      <NumberInputStepper>
                        <NumberIncrementStepper />
                        <NumberDecrementStepper />
                      </NumberInputStepper>
                    </NumberInput>
                  </FormControl>
                  <FormControl display="flex" alignItems="center">
                    <FormLabel htmlFor="grc-trend-filter" mb="0" flex="1">
                      {t('botRiskControl.gridTrendFilter')}
                    </FormLabel>
                    <Switch
                      id="grc-trend-filter"
                      isChecked={riskControl.grid_risk_control?.trend_filter_enabled ?? false}
                      onChange={(e) => updateGridRiskControlField('trend_filter_enabled', e.target.checked)}
                    />
                  </FormControl>
                </SimpleGrid>

                <Divider />
                <Heading size="xs" textTransform="uppercase" color="gray.500">
                  {t('botRiskControl.closeCondition')}
                </Heading>
                <Text fontSize="xs" color="gray.500">{t('botRiskControl.closeConditionDesc')}</Text>
                <SimpleGrid columns={{ base: 1, md: 2, lg: 3 }} spacing={4}>
                  <FormControl display="flex" alignItems="center">
                    <FormLabel htmlFor="grc-close-condition" mb="0" flex="1">
                      {t('botRiskControl.closeConditionEnabled')}
                    </FormLabel>
                    <Switch
                      id="grc-close-condition"
                      isChecked={riskControl.grid_risk_control?.close_condition_enabled ?? false}
                      onChange={(e) => updateGridRiskControlField('close_condition_enabled', e.target.checked)}
                    />
                  </FormControl>
                  <FormControl>
                    <FormLabel fontSize="sm">{t('botRiskControl.closeConditionProfitTarget')} (%)</FormLabel>
                    <DecimalNumberInput
                      value={typeof riskControl.grid_risk_control?.close_condition_profit_target === 'number'
                        ? (riskControl.grid_risk_control.close_condition_profit_target <= 1 ? riskControl.grid_risk_control.close_condition_profit_target * 100 : riskControl.grid_risk_control.close_condition_profit_target)
                        : 0}
                      onChange={(val) => updateGridRiskControlField('close_condition_profit_target', typeof val === 'number' ? val / 100 : val)}
                      min={0}
                      max={1000}
                      precision={2}
                      step={1}
                      showStepper
                    />
                    <Text fontSize="xs" color="gray.500">{t('botRiskControl.closeConditionProfitTargetDesc')}</Text>
                  </FormControl>
                  <FormControl>
                    <FormLabel fontSize="sm">{t('botRiskControl.closeConditionLossLimit')} (%)</FormLabel>
                    <DecimalNumberInput
                      value={typeof riskControl.grid_risk_control?.close_condition_loss_limit === 'number'
                        ? (riskControl.grid_risk_control.close_condition_loss_limit <= 1 ? riskControl.grid_risk_control.close_condition_loss_limit * 100 : riskControl.grid_risk_control.close_condition_loss_limit)
                        : 0}
                      onChange={(val) => updateGridRiskControlField('close_condition_loss_limit', typeof val === 'number' ? val / 100 : val)}
                      min={0}
                      max={100}
                      precision={2}
                      step={0.5}
                      showStepper
                    />
                    <Text fontSize="xs" color="gray.500">{t('botRiskControl.closeConditionLossLimitDesc')}</Text>
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
