import React, { useEffect, useState } from 'react'
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
  Tabs,
  TabList,
  TabPanels,
  Tab,
  TabPanel,
  Stat,
  StatLabel,
  StatNumber,
  StatHelpText,
  SimpleGrid,
  Table,
  Thead,
  Tbody,
  Tr,
  Th,
  Td,
  TableContainer,
  useDisclosure,
  FormControl,
  FormLabel,
  Input,
  NumberInput,
  NumberInputField,
  NumberInputStepper,
  NumberIncrementStepper,
  NumberDecrementStepper,
  Select,
  VStack,
  Alert,
  AlertIcon,
  AlertTitle,
  AlertDescription,
  Divider,
  Switch,
} from '@chakra-ui/react'
import { ChevronLeftIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import { Link, useParams, useNavigate } from 'react-router-dom'
import { ExternalLinkIcon } from '@chakra-ui/icons'
import {
  getBotById,
  startBot,
  stopBot,
  closePositionsV2,
  getPositionsSummary,
  getStatistics,
  getLogs,
  updateBotStrategy,
  BotDetailInfo,
  UpdateBotStrategyRequest,
} from '../services/api'
import { useSymbol } from '../contexts/SymbolContext'
import BotRiskControlPanel from './BotRiskControlPanel'
import StopWithCloseConfirmDialog from './StopWithCloseConfirmDialog'

// 策略选项定义
interface StrategyOption {
  value: string
  label: string
}

// Helper function to get strategy options with i18n
const getGridRelatedStrategies = (t: any): StrategyOption[] => [
  { value: 'grid', label: t('strategyNames.grid', '网格策略') },
  { value: 'grid+trend', label: t('strategyNames.grid+trend', '网格+趋势组合') },
  { value: 'trend_following', label: t('strategyNames.trend_following', '趋势跟踪') },
  { value: 'momentum', label: t('strategyNames.momentum', '动量策略') },
  { value: 'mean_reversion', label: t('strategyNames.mean_reversion', '均值回归') },
]

const getDcaRelatedStrategies = (t: any): StrategyOption[] => [
  { value: 'dca', label: t('strategyNames.dca', 'DCA定投') },
  { value: 'martingale', label: t('strategyNames.martingale', '马丁格尔') },
]

const getMixedStrategies = (t: any): StrategyOption[] => [
  { value: 'grid+dca', label: t('strategyNames.grid+dca', '网格+DCA') },
  { value: 'grid+martingale', label: t('strategyNames.grid+martingale', '网格+马丁格尔') },
]

const BotDetail: React.FC = () => {
  const { botId } = useParams<{ botId: string }>()
  const navigate = useNavigate()
  const { t } = useTranslation()
  const toast = useToast()
  const { navigateToBot } = useSymbol()
  const [bot, setBot] = useState<BotDetailInfo | null>(null)
  const [loading, setLoading] = useState(true)
  const [actioning, setActioning] = useState(false)
  const [positionsSummary, setPositionsSummary] = useState<any>(null)
  const [statistics, setStatistics] = useState<any>(null)
  const [logs, setLogs] = useState<any[]>([])
  const [logsLoading, setLogsLoading] = useState(false)
  const { isOpen: isStopDialogOpen, onOpen: onStopDialogOpen, onClose: onStopDialogClose } = useDisclosure()

  const fetchBot = async () => {
    if (!botId) return
    try {
      const data = await getBotById(botId)
      setBot(data)
      return data
    } catch (err) {
      console.error('Failed to fetch bot:', err)
      toast({ title: t('botList.fetchFailed'), status: 'error', duration: 3000 })
      return null
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchBot()
  }, [botId])

  useEffect(() => {
    if (!bot?.running || !bot?.exchange || !bot?.symbol) return
    const fetchOverview = async () => {
      try {
        const [posRes, statRes] = await Promise.all([
          getPositionsSummary(bot.exchange, bot.symbol).catch(() => null),
          getStatistics(bot.exchange, bot.symbol).catch(() => null),
        ])
        setPositionsSummary(posRes)
        setStatistics(statRes)
      } catch {
        setPositionsSummary(null)
        setStatistics(null)
      }
    }
    fetchOverview()
    const interval = setInterval(fetchOverview, 10000)
    return () => clearInterval(interval)
  }, [bot?.running, bot?.exchange, bot?.symbol])

  const fetchLogs = async () => {
    if (!bot?.symbol) return
    setLogsLoading(true)
    try {
      const res = await getLogs({ limit: 50, keyword: bot.symbol })
      setLogs(res.logs || [])
    } catch {
      setLogs([])
    } finally {
      setLogsLoading(false)
    }
  }

  const handleStart = async () => {
    if (!botId) return
    setActioning(true)
    try {
      await startBot(botId)
      toast({ title: t('botList.startSuccess'), status: 'success', duration: 2000 })
      await fetchBot()
    } catch (err) {
      const e = err as Error & { errorKey?: string; groupName?: string }
      const msg = e.errorKey ? t(e.errorKey, { groupName: e.groupName ?? '' }) : t('botList.startFailed')
      toast({ title: msg, status: 'error', duration: 4000 })
    } finally {
      setActioning(false)
    }
  }

  const handleStopOnly = async () => {
    if (!botId) return
    setActioning(true)
    try {
      await stopBot(botId)
      toast({ title: t('botList.stopSuccess'), status: 'success', duration: 2000 })
      await fetchBot()
    } catch (err) {
      toast({ title: t('botList.stopFailed'), status: 'error', duration: 3000 })
    } finally {
      setActioning(false)
    }
  }

  const handleStopAndClose = async (req: Parameters<typeof closePositionsV2>[1]) => {
    if (!botId) return
    setActioning(true)
    try {
      await closePositionsV2(botId, req)
      await stopBot(botId)
      toast({ title: t('globalDashboard.closePositions.success'), status: 'success', duration: 2000 })
      await fetchBot()
    } catch (err) {
      toast({
        title: t('globalDashboard.closePositions.failed'),
        description: err instanceof Error ? err.message : String(err),
        status: 'error',
        duration: 4000,
      })
    } finally {
      setActioning(false)
    }
  }

  const handleOpenWorkspace = () => {
    if (!bot || !botId) return
    navigateToBot(botId, 'dashboard')
  }

  const handleNavigateToRisk = () => {
    if (!bot || !botId) return
    navigateToBot(botId, 'risk')
  }

  const handleNavigateToLogs = () => {
    navigate('/logs')
  }

  if (loading) {
    return (
      <Flex justify="center" align="center" minH="200px">
        <Spinner size="lg" />
      </Flex>
    )
  }

  if (!bot) {
    return (
      <Box>
        <Button as={Link} to="/bots" leftIcon={<ChevronLeftIcon />} variant="ghost" size="sm" mb={4}>
          {t('common.back')}
        </Button>
        <Text color="gray.500">{t('botList.fetchFailed')}</Text>
      </Box>
    )
  }

  return (
    <Box>
      <Button as={Link} to="/bots" leftIcon={<ChevronLeftIcon />} variant="ghost" size="sm" mb={4}>
        {t('common.back')}
      </Button>
      <Card mb={4}>
        <CardBody>
          <Flex justify="space-between" align="flex-start" flexWrap="wrap" gap={4}>
            <Box>
              <HStack spacing={2} mb={2}>
                <Badge colorScheme={bot.running ? 'green' : 'gray'} fontSize="10px">
                  {bot.running ? t('botList.running') : t('botList.stopped')}
                </Badge>
                {bot.risk_triggered && (
                  <Badge colorScheme="red" fontSize="10px">{t('botList.riskTriggered')}</Badge>
                )}
              </HStack>
              <Heading size="md">{bot.name || bot.symbol}</Heading>
              <Text fontSize="sm" color="gray.500" mt={1}>
                {bot.exchange} · {bot.symbol} ({bot.market_type})
              </Text>
              {bot.running && (
                <HStack spacing={4} mt={3} fontSize="sm">
                  {bot.current_price != null && (
                    <Text>${bot.current_price.toLocaleString(undefined, { minimumFractionDigits: 2 })}</Text>
                  )}
                  {bot.total_pnl != null && (
                    <Text color={bot.total_pnl >= 0 ? 'green.500' : 'red.500'}>
                      PnL: {bot.total_pnl >= 0 ? '+' : ''}{bot.total_pnl.toFixed(2)}
                    </Text>
                  )}
                </HStack>
              )}
            </Box>
            <HStack>
              {bot.running ? (
                <>
                  <Button size="sm" colorScheme="blue" onClick={handleOpenWorkspace}>
                    {t('botDetail.openWorkspace')}
                  </Button>
                  <Button
                    size="sm"
                    colorScheme="red"
                    variant="outline"
                    isLoading={actioning}
                    onClick={onStopDialogOpen}
                  >
                    {t('botList.stop')}
                  </Button>
                </>
              ) : (
                <Button size="sm" colorScheme="green" isLoading={actioning} onClick={handleStart}>
                  {t('botList.start')}
                </Button>
              )}
            </HStack>
          </Flex>
        </CardBody>
      </Card>

      <Tabs colorScheme="blue" variant="enclosed">
        <TabList>
          <Tab>{t('botDetail.tabOverview')}</Tab>
          <Tab>{t('botDetail.tabStrategy')}</Tab>
          <Tab>{t('botDetail.tabRisk')}</Tab>
          <Tab>{t('botDetail.tabBacktest')}</Tab>
          <Tab>{t('botDetail.tabLogs')}</Tab>
        </TabList>
        <TabPanels>
          <TabPanel px={0}>
            {bot.running ? (
              <>
                {/* 【全部】指标 */}
                <Text fontSize="sm" color="gray.500" mb={3}>{t('botDetail.allTimeMetrics')}</Text>
                <SimpleGrid columns={{ base: 1, md: 2, lg: 4 }} spacing={4} mb={6}>
                  <Card>
                    <CardBody>
                      <Stat>
                        <StatLabel>{t('botDetail.currentPrice')}</StatLabel>
                        <StatNumber>
                          ${(positionsSummary?.current_price ?? bot.current_price ?? 0).toLocaleString(undefined, { minimumFractionDigits: 2 })}
                        </StatNumber>
                      </Stat>
                    </CardBody>
                  </Card>
                  <Card>
                    <CardBody>
                      <Stat>
                        <StatLabel>{t('botDetail.unrealizedPnl')}</StatLabel>
                        <StatNumber color={(positionsSummary?.unrealized_pnl ?? 0) >= 0 ? 'green.500' : 'red.500'}>
                          {(positionsSummary?.unrealized_pnl ?? bot.total_pnl ?? 0) >= 0 ? '+' : ''}
                          {(positionsSummary?.unrealized_pnl ?? bot.total_pnl ?? 0).toFixed(2)}
                        </StatNumber>
                        {/* 未实现盈亏双口径展示 */}
                        {(positionsSummary?.slot_data || positionsSummary?.exchange_data) && (
                          <StatHelpText fontSize="xs" color="gray.500">
                            {positionsSummary?.slot_data && (
                              <Text display="block">{t('botDetail.ours')}: {positionsSummary.slot_data.unrealized_pnl?.toFixed(2) ?? '0.00'}</Text>
                            )}
                            {positionsSummary?.exchange_data?.has_data && (
                              <Text display="block">{t('botDetail.exchange')}: {positionsSummary.exchange_data.unrealized_pnl?.toFixed(2) ?? '0.00'}</Text>
                            )}
                          </StatHelpText>
                        )}
                      </Stat>
                    </CardBody>
                  </Card>
                  <Card>
                    <CardBody>
                      <Stat>
                        <StatLabel>{t('statistics.totalTrades')}</StatLabel>
                        <StatNumber>{statistics?.total_trades ?? 0}</StatNumber>
                      </Stat>
                    </CardBody>
                  </Card>
                  <Card>
                    <CardBody>
                      <Stat>
                        <StatLabel>{t('statistics.totalPnL')}</StatLabel>
                        <StatNumber color={(statistics?.total_pnl ?? 0) >= 0 ? 'green.500' : 'red.500'}>
                          {(statistics?.total_pnl ?? 0) >= 0 ? '+' : ''}{(statistics?.total_pnl ?? 0).toFixed(2)}
                        </StatNumber>
                      </Stat>
                    </CardBody>
                  </Card>
                </SimpleGrid>

                {/* 【当日】指标 */}
                <Text fontSize="sm" color="gray.500" mb={3}>{t('botDetail.todayMetrics')}</Text>
                <SimpleGrid columns={{ base: 1, md: 2, lg: 4 }} spacing={4}>
                  <Card>
                    <CardBody>
                      <Stat>
                        <StatLabel>{t('botDetail.todayTrades')}</StatLabel>
                        <StatNumber>{statistics?.today_trades ?? 0}</StatNumber>
                      </Stat>
                    </CardBody>
                  </Card>
                  <Card>
                    <CardBody>
                      <Stat>
                        <StatLabel>{t('botDetail.todayPnlOurs')}</StatLabel>
                        <StatNumber color={(statistics?.today_pnl ?? 0) >= 0 ? 'green.500' : 'red.500'}>
                          {(statistics?.today_pnl ?? 0) >= 0 ? '+' : ''}{(statistics?.today_pnl ?? 0).toFixed(2)}
                        </StatNumber>
                      </Stat>
                    </CardBody>
                  </Card>
                  <Card>
                    <CardBody>
                      <Stat>
                        <StatLabel>{t('botDetail.todayPnlExchange')}</StatLabel>
                        <StatNumber color={(statistics?.today_exchange_pnl ?? 0) >= 0 ? 'green.500' : 'red.500'}>
                          {(statistics?.today_exchange_pnl ?? 0) >= 0 ? '+' : ''}{(statistics?.today_exchange_pnl ?? 0).toFixed(2)}
                        </StatNumber>
                      </Stat>
                    </CardBody>
                  </Card>
                </SimpleGrid>
              </>
            ) : (
              <Text color="gray.500">{t('botDetail.startToViewOverview')}</Text>
            )}
          </TabPanel>
          <TabPanel px={0}>
            <BotStrategyConfigPanel botId={botId!} bot={bot} />
          </TabPanel>
          <TabPanel px={0}>
            {botId && (
              <BotRiskControlPanel botId={botId} botRunning={bot.running} />
            )}
          </TabPanel>
          <TabPanel px={0}>
            <BotBacktestPanel bot={bot} />
          </TabPanel>
          <TabPanel px={0}>
            <Card>
              <CardBody>
                <Flex justify="space-between" align="center" mb={4}>
                  <Text color="gray.600">{t('botDetail.logsHint')}</Text>
                  <Button size="sm" variant="outline" onClick={fetchLogs} isLoading={logsLoading}>
                    {t('common.refresh')}
                  </Button>
                </Flex>
                {logs.length > 0 ? (
                  <TableContainer maxH="300px" overflowY="auto">
                    <Table size="sm">
                      <Thead>
                        <Tr>
                          <Th>{t('botDetail.logTime')}</Th>
                          <Th>{t('botDetail.logLevel')}</Th>
                          <Th>{t('botDetail.logMessage')}</Th>
                        </Tr>
                      </Thead>
                      <Tbody>
                        {logs.map((log, i) => (
                          <Tr key={log.id || i}>
                            <Td fontSize="xs">{log.timestamp || '-'}</Td>
                            <Td><Badge size="sm">{log.level || 'info'}</Badge></Td>
                            <Td fontSize="xs" maxW="400px" isTruncated>{log.message || '-'}</Td>
                          </Tr>
                        ))}
                      </Tbody>
                    </Table>
                  </TableContainer>
                ) : (
                  <Button size="sm" onClick={fetchLogs} isLoading={logsLoading}>
                    {t('botDetail.loadLogs')}
                  </Button>
                )}
              </CardBody>
            </Card>
          </TabPanel>
        </TabPanels>
      </Tabs>

      <StopWithCloseConfirmDialog
        isOpen={isStopDialogOpen}
        onClose={onStopDialogClose}
        onStopOnly={handleStopOnly}
        onStopAndClose={handleStopAndClose}
        botId={botId!}
        botName={bot?.name || bot?.symbol}
      />
    </Box>
  )
}

/** 构建跳转到全局回测页的 URL，预填 Bot 参数 */
function buildBacktestUrl(bot: BotDetailInfo | null): string {
  if (!bot?.exchange || !bot?.symbol) return '/backtest'
  const cfg = bot.config as Record<string, unknown> | undefined
  const openCtrl = cfg?.open_position_control as Record<string, unknown> | undefined
  const strategies = cfg?.strategies as Array<{ type?: string; weight?: number; config?: Record<string, unknown> }> | undefined
  const strategyType = strategies?.[0]?.type || 'grid'
  const params = new URLSearchParams()
  params.set('exchange', bot.exchange)
  params.set('market_type', bot.market_type || 'futures')
  params.set('symbol', bot.symbol)
  params.set('strategy', strategyType)
  if (bot.bot_id) params.set('bot_id', bot.bot_id)
  if (strategies && strategies.length > 0) {
    params.set('mode', 'bot_strategies')
    params.set(
      'strategies',
      JSON.stringify(
        strategies.map((strategy) => ({
          type: strategy.type || 'grid',
          weight: typeof strategy.weight === 'number' ? strategy.weight : 0,
          config: strategy.config || {},
        }))
      )
    )
  }
  params.set('days', '7')
  const maxVal = openCtrl?.max_position_value
  if (typeof maxVal === 'number' && maxVal > 0) params.set('total_capital', String(maxVal))
  const priceInterval = cfg?.price_interval
  if (typeof priceInterval === 'number' && priceInterval > 0) params.set('grid_spacing', String(priceInterval))
  const orderQty = cfg?.order_quantity
  if (typeof orderQty === 'number' && orderQty > 0) params.set('order_quantity', String(orderQty))
  const profitSpread = cfg?.profit_spread
  if (typeof profitSpread === 'number' && profitSpread > 0) params.set('profit_spread', String(profitSpread))
  return `/backtest?${params.toString()}`
}

// BotBacktestPanel Bot 回测面板：展示参数并跳转全局回测
const BotBacktestPanel: React.FC<{ bot: BotDetailInfo | null }> = ({ bot }) => {
  const { t } = useTranslation()
  const cfg = bot?.config as Record<string, unknown> | undefined
  const openCtrl = cfg?.open_position_control as Record<string, unknown> | undefined
  const strategies = cfg?.strategies as Array<{ type?: string }> | undefined
  const strategyType = strategies?.[0]?.type || '-'

  return (
    <Card>
      <CardBody>
        <VStack spacing={4} align="stretch">
          <Flex justify="space-between" align="center">
            <Heading size="md">{t('botDetail.tabBacktest')}</Heading>
            <Text fontSize="sm" color="gray.600">
              {t('backtest.botBacktestHint')}
            </Text>
          </Flex>

          <Alert status="info" borderRadius="md">
            <AlertIcon />
            <Box>
              <AlertTitle fontSize="sm" mb={1}>
                {t('backtest.parametersFromBot')}
              </AlertTitle>
              <AlertDescription fontSize="xs">
                {t('backtest.parametersAutoFilled')}
              </AlertDescription>
            </Box>
          </Alert>

          <SimpleGrid columns={{ base: 1, md: 2, lg: 3 }} spacing={4}>
            <Stat>
              <StatLabel>{t('botDetail.strategyType')}</StatLabel>
              <StatNumber fontSize="md">{strategyType}</StatNumber>
            </Stat>
            <Stat>
              <StatLabel>{t('botDetail.maxPositionValue')}</StatLabel>
              <StatNumber>
                {openCtrl?.max_position_value ?? 0} USDT
              </StatNumber>
              <StatHelpText>{t('backtest.actualMarginUsed')}</StatHelpText>
            </Stat>
            <Stat>
              <StatLabel>{t('botDetail.maxPositionLayers')}</StatLabel>
              <StatNumber>
                {openCtrl?.max_position_layers ?? 0}
              </StatNumber>
              <StatHelpText>{t('backtest.maxLayers')}</StatHelpText>
            </Stat>
            <Stat>
              <StatLabel>{t('botDetail.priceInterval')}</StatLabel>
              <StatNumber>${cfg?.price_interval ?? 0}</StatNumber>
            </Stat>
            <Stat>
              <StatLabel>{t('botDetail.orderQuantity')}</StatLabel>
              <StatNumber>${cfg?.order_quantity ?? 0}</StatNumber>
            </Stat>
            <Stat>
              <StatLabel>{t('botDetail.profitSpread')}</StatLabel>
              <StatNumber>${cfg?.profit_spread ?? 0}</StatNumber>
            </Stat>
            <Stat>
              <StatLabel>{t('botDetail.priceLow')}</StatLabel>
              <StatNumber>${cfg?.price_low ?? 0}</StatNumber>
            </Stat>
            <Stat>
              <StatLabel>{t('botDetail.priceHigh')}</StatLabel>
              <StatNumber>${cfg?.price_high ?? 0}</StatNumber>
            </Stat>
          </SimpleGrid>

          <Box>
            <Text fontSize="sm" color="gray.600" mb={2}>
              {t('botDetail.goToBacktestDesc')}
            </Text>
            <Button
              as={Link}
              to={buildBacktestUrl(bot)}
              colorScheme="blue"
              size="lg"
              rightIcon={<ExternalLinkIcon />}
            >
              {t('botDetail.goToBacktest')}
            </Button>
          </Box>
        </VStack>
      </CardBody>
    </Card>
  )
}

// BotStrategyConfigPanel Bot 策略配置面板
interface BotStrategyConfigPanelProps {
  botId: string
  bot: BotDetailInfo | null
}

const BotStrategyConfigPanel: React.FC<BotStrategyConfigPanelProps> = ({ botId, bot }) => {
  const { t } = useTranslation()
  const toast = useToast()

  const [strategyType, setStrategyType] = useState<string>('grid')
  const [originalStrategyType, setOriginalStrategyType] = useState<string>('')
  const [priceInterval, setPriceInterval] = useState<string>('')
  const [profitSpread, setProfitSpread] = useState<string>('')
  const [orderQuantity, setOrderQuantity] = useState<string>('')
  const [priceLow, setPriceLow] = useState<string>('')
  const [priceHigh, setPriceHigh] = useState<string>('')
  const [hasChanges, setHasChanges] = useState(false)
  const [saving, setSaving] = useState(false)

  // 策略类型选项（根据当前策略类型限制可切换的类型）
  const getAvailableStrategies = () => {
    if (!bot?.config?.strategies || bot.config.strategies.length === 0) {
      return getGridRelatedStrategies(t)
    }
    const strategies = bot.config.strategies
    // 如果是混合策略（多个策略）
    if (strategies.length > 1) {
      return [
        ...getGridRelatedStrategies(t),
        ...getDcaRelatedStrategies(t),
        ...getMixedStrategies(t),
      ]
    }
    const currentType = strategies[0].type
    const gridStrategies = getGridRelatedStrategies(t)
    const dcaStrategies = getDcaRelatedStrategies(t)
    if (gridStrategies.some(s => s.value === currentType)) {
      return [...gridStrategies, ...getMixedStrategies(t)]
    }
    if (dcaStrategies.some(s => s.value === currentType)) {
      return [...dcaStrategies, ...getMixedStrategies(t)]
    }
    return getGridRelatedStrategies(t)
  }

  // 初始化表单数据
  useEffect(() => {
    if (bot?.config) {
      const cfg = bot.config as any
      if (cfg.strategies && cfg.strategies.length > 0) {
        const type = cfg.strategies[0].type || 'grid'
        setStrategyType(type)
        setOriginalStrategyType(type)
      }
      setPriceInterval(cfg.price_interval?.toString() || '')
      setProfitSpread(cfg.profit_spread?.toString() || '')
      setOrderQuantity(cfg.order_quantity?.toString() || '')
      setPriceLow(cfg.price_low?.toString() || '')
      setPriceHigh(cfg.price_high?.toString() || '')
    }
  }, [bot])

  // 检查是否尝试切换策略类型
  const isTryingToChangeStrategyType = strategyType !== originalStrategyType

  const handleSave = async () => {
    // 运行中不允许切换策略类型，但允许修改参数
    if (bot.running && isTryingToChangeStrategyType) {
      toast({
        title: t('botDetail.strategy.cannotChangeTypeRunning'),
        status: 'warning',
        duration: 3000,
      })
      return
    }

    setSaving(true)
    try {
      const updateData: UpdateBotStrategyRequest = {
        strategies: [{
          type: strategyType,
          weight: 1.0,
          config: {},
        }],
      }

      // 只添加有值的字段
      if (priceInterval) updateData.price_interval = parseFloat(priceInterval)
      if (profitSpread) updateData.profit_spread = parseFloat(profitSpread)
      if (orderQuantity) updateData.order_quantity = parseFloat(orderQuantity)
      if (priceLow) updateData.price_low = parseFloat(priceLow)
      if (priceHigh) updateData.price_high = parseFloat(priceHigh)

      await updateBotStrategy(botId, updateData)
      toast({
        title: t('botDetail.strategy.saveSuccess'),
        status: 'success',
        duration: 2000,
      })
      setHasChanges(false)
      setOriginalStrategyType(strategyType) // 更新原始策略类型
    } catch (err: any) {
      const errorMsg = err.errorKey ? t(err.errorKey) : t('botDetail.strategy.saveFailed')
      toast({
        title: errorMsg,
        status: 'error',
        duration: 3000,
      })
    } finally {
      setSaving(false)
    }
  }

  const handleReset = () => {
    if (bot?.config) {
      const cfg = bot.config as any
      if (cfg.strategies && cfg.strategies.length > 0) {
        setStrategyType(cfg.strategies[0].type || 'grid')
      }
      setPriceInterval(cfg.price_interval?.toString() || '')
      setProfitSpread(cfg.profit_spread?.toString() || '')
      setOrderQuantity(cfg.order_quantity?.toString() || '')
      setPriceLow(cfg.price_low?.toString() || '')
      setPriceHigh(cfg.price_high?.toString() || '')
      setHasChanges(false)
    }
  }

  return (
    <VStack align="stretch" spacing={4}>
      {/* 提示信息 */}
      {bot.running && isTryingToChangeStrategyType && (
        <Alert status="warning">
          <AlertIcon />
          <Text>{t('botDetail.strategy.cannotChangeTypeRunning')}</Text>
        </Alert>
      )}

      {/* 策略类型选择 */}
      <Card>
        <CardBody>
          <FormControl isDisabled={bot.running}>
            <FormLabel>{t('botDetail.strategy.strategyType')}</FormLabel>
            <Select
              value={strategyType}
              onChange={(e) => {
                setStrategyType(e.target.value)
                setHasChanges(true)
              }}
            >
              {getAvailableStrategies().map((s) => (
                <option key={s.value} value={s.value}>
                  {s.label}
                </option>
              ))}
            </Select>
            <Text fontSize="xs" color="gray.500" mt={1}>
              {t('botDetail.strategy.typeHint')}
            </Text>
            {bot.running && (
              <Text fontSize="xs" color="orange.500" mt={1}>
                {t('botDetail.strategy.typeChangeDisabledRunning')}
              </Text>
            )}
          </FormControl>
        </CardBody>
      </Card>

      {/* 网格参数配置 */}
      <Card>
        <CardBody>
          <Heading size="sm" mb={4}>{t('botDetail.strategy.gridParams')}</Heading>
          <VStack align="stretch" spacing={4}>
            <FormControl>
              <FormLabel>{t('botDetail.strategy.priceInterval')}</FormLabel>
              <NumberInput
                value={priceInterval}
                onChange={(valueString) => {
                  setPriceInterval(valueString)
                  setHasChanges(true)
                }}
                min={0}
                step={10}
              >
                <NumberInputField />
                <NumberInputStepper>
                  <NumberIncrementStepper />
                  <NumberDecrementStepper />
                </NumberInputStepper>
              </NumberInput>
              <Text fontSize="xs" color="gray.500" mt={1}>
                {t('botDetail.strategy.priceIntervalHint')}
              </Text>
            </FormControl>

            <FormControl>
              <FormLabel>{t('botDetail.strategy.profitSpread')}</FormLabel>
              <NumberInput
                value={profitSpread}
                onChange={(valueString) => {
                  setProfitSpread(valueString)
                  setHasChanges(true)
                }}
                min={0}
                step={10}
              >
                <NumberInputField />
                <NumberInputStepper>
                  <NumberIncrementStepper />
                  <NumberDecrementStepper />
                </NumberInputStepper>
              </NumberInput>
              <Text fontSize="xs" color="gray.500" mt={1}>
                {t('botDetail.strategy.profitSpreadHint')}
              </Text>
            </FormControl>

            <FormControl>
              <FormLabel>{t('botDetail.strategy.orderQuantity')}</FormLabel>
              <NumberInput
                value={orderQuantity}
                onChange={(valueString) => {
                  setOrderQuantity(valueString)
                  setHasChanges(true)
                }}
                min={0}
                step={10}
              >
                <NumberInputField />
                <NumberInputStepper>
                  <NumberIncrementStepper />
                  <NumberDecrementStepper />
                </NumberInputStepper>
              </NumberInput>
              <Text fontSize="xs" color="gray.500" mt={1}>
                {t('botDetail.strategy.orderQuantityHint')}
              </Text>
            </FormControl>

            <Divider />

            <FormControl>
              <FormLabel>{t('botDetail.strategy.priceLow')}</FormLabel>
              <NumberInput
                value={priceLow}
                onChange={(valueString) => {
                  setPriceLow(valueString)
                  setHasChanges(true)
                }}
                min={0}
                step={100}
              >
                <NumberInputField />
                <NumberInputStepper>
                  <NumberIncrementStepper />
                  <NumberDecrementStepper />
                </NumberInputStepper>
              </NumberInput>
              <Text fontSize="xs" color="gray.500" mt={1}>
                {t('botDetail.strategy.priceLowHint')}
              </Text>
            </FormControl>

            <FormControl>
              <FormLabel>{t('botDetail.strategy.priceHigh')}</FormLabel>
              <NumberInput
                value={priceHigh}
                onChange={(valueString) => {
                  setPriceHigh(valueString)
                  setHasChanges(true)
                }}
                min={0}
                step={100}
              >
                <NumberInputField />
                <NumberInputStepper>
                  <NumberIncrementStepper />
                  <NumberDecrementStepper />
                </NumberInputStepper>
              </NumberInput>
              <Text fontSize="xs" color="gray.500" mt={1}>
                {t('botDetail.strategy.priceHighHint')}
              </Text>
            </FormControl>
          </VStack>
        </CardBody>
      </Card>

      {/* 操作按钮 */}
      {hasChanges && (
        <HStack spacing={3} justifyContent="flex-end">
          <Button
            variant="ghost"
            onClick={handleReset}
            isDisabled={saving}
          >
            {t('common.reset')}
          </Button>
          <Button
            colorScheme="blue"
            onClick={handleSave}
            isLoading={saving}
            isDisabled={bot.running && isTryingToChangeStrategyType}
          >
            {t('common.save')}
          </Button>
        </HStack>
      )}

      {/* 跳转到工作区的按钮 */}
      {!hasChanges && (
        <Card>
          <CardBody>
            <Flex justify="space-between" align="center">
              <Text color="gray.600">{t('botDetail.strategy.workspaceHint')}</Text>
              <Button
                size="sm"
                colorScheme="blue"
                variant="outline"
                onClick={() => {
                  if (bot) {
                    navigateToBot(botId, 'dashboard')
                  }
                }}
              >
                {t('botDetail.openWorkspace')} → {t('sidebar.strategyAllocation')}
              </Button>
            </Flex>
          </CardBody>
        </Card>
      )}
    </VStack>
  )
}

export default BotDetail
