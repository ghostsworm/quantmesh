import React, { useCallback, useEffect, useMemo, useState } from 'react'
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
  Tooltip,
  Icon,
} from '@chakra-ui/react'
import { ChevronLeftIcon } from '@chakra-ui/icons'

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
import { useTranslation } from 'react-i18next'
import { Link, useParams, useNavigate } from 'react-router-dom'
import {
  ExternalLinkIcon,
  ViewIcon,
  RepeatIcon,
  WarningIcon,
  ArrowUpIcon,
  TimeIcon,
  StarIcon,
  ChatIcon,
} from '@chakra-ui/icons'
import {
  getBotById,
  startBot,
  stopBot,
  pollBotUntilRunning,
  closePositionsV2,
  getPositionsSummary,
  getExchangePositionsSummary,
  getStatistics,
  getLogs,
  getOrderHistory,
  getBotPositionStatus,
  pauseBotOpening,
  resumeBotOpening,
  updateBotStrategy,
  getExchangeOpenOrders,
  cancelAllExchangeOrders,
  BotDetailInfo,
  UpdateBotStrategyRequest,
  PositionStatus,
  ExchangeOpenOrderInfo,
  getMarketTicker,
  MarketTickerResponse,
} from '../services/api'
import { useSymbol } from '../contexts/SymbolContext'
import { useConfig } from '../contexts/ConfigContext'
import { formatTime as formatTimeUtil } from '../utils/dateFormat'
import { buildBacktestUrl } from '../utils/backtestUrl'
import BotRiskControlPanel from './BotRiskControlPanel'
import BotRiskControlHistoryPanel from './BotRiskControlHistoryPanel'
import OptionHedgePanel from './OptionHedgePanel'
import StopWithCloseConfirmDialog from './StopWithCloseConfirmDialog'
import { computeLiquidationPrice } from './ParamAdvisor'
import { parseFundingPerpSpread } from '../utils/fundingPerpSpread'
import { ResponsiveTabLabel, useCompactConfigTabs } from './ResponsiveTabLabel'

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

/** Bot 详情「实时日志」默认条数；与后端 /api/logs 上限 2000 对齐 */
const BOT_DETAIL_LOG_LIMIT_DEFAULT = 500
const BOT_DETAIL_LOG_LIMIT_OPTIONS = [200, 500, 1000, 2000] as const

/** Bot 主內容區分頁圖標（與 Tab 順序一致） */
const BOT_MAIN_TAB_ICONS = [ViewIcon, RepeatIcon, WarningIcon, ArrowUpIcon, TimeIcon, StarIcon, ChatIcon] as const

type BotLogLevelFilter = '' | 'DEBUG' | 'INFO' | 'WARN' | 'ERROR' | 'FATAL'

const BotDetail: React.FC = () => {
  const { botId } = useParams<{ botId: string }>()
  const navigate = useNavigate()
  const { t, i18n } = useTranslation()
  const toast = useToast()
  const compactTabs = useCompactConfigTabs()
  const [mainTabIndex, setMainTabIndex] = useState(0)
  const { navigateToBot } = useSymbol()
  const { timezone } = useConfig()
  const [bot, setBot] = useState<BotDetailInfo | null>(null)
  const [loading, setLoading] = useState(true)
  const [actioning, setActioning] = useState(false)
  const [positionsSummary, setPositionsSummary] = useState<any>(null)
  const [statistics, setStatistics] = useState<any>(null)
  const [positionStatus, setPositionStatus] = useState<PositionStatus | null>(null)
  const [logs, setLogs] = useState<any[]>([])
  const [logsLoading, setLogsLoading] = useState(false)
  const [logsTotal, setLogsTotal] = useState(0)
  const [logLevelFilter, setLogLevelFilter] = useState<BotLogLevelFilter>('')
  const [logLimit, setLogLimit] = useState(BOT_DETAIL_LOG_LIMIT_DEFAULT)
  const [tpSlOrders, setTpSlOrders] = useState<any[]>([])
  const [tpSlLoading, setTpSlLoading] = useState(false)
  const [exchangeOrders, setExchangeOrders] = useState<ExchangeOpenOrderInfo[]>([])
  const [exchangeOrdersLoading, setExchangeOrdersLoading] = useState(false)
  const [cancellingAll, setCancellingAll] = useState(false)
  const [exchangePositionsSummary, setExchangePositionsSummary] = useState<{
    has_data: boolean
    quantity: number
    entry_price: number
    mark_price: number
    unrealized_pnl: number
    leverage: number
    current_price: number
    total_value?: number
  } | null>(null)
  const { isOpen: isStopDialogOpen, onOpen: onStopDialogOpen, onClose: onStopDialogClose } = useDisclosure()

  const fundingPerpLegs = useMemo(() => parseFundingPerpSpread(bot), [bot])
  const [legTicks, setLegTicks] = useState<{ a: MarketTickerResponse | null; b: MarketTickerResponse | null }>({
    a: null,
    b: null,
  })

  useEffect(() => {
    if (!fundingPerpLegs) {
      setLegTicks({ a: null, b: null })
      return
    }
    const fetchBoth = async () => {
      try {
        const [a, b] = await Promise.all([
          getMarketTicker(fundingPerpLegs.leg_a.exchange, fundingPerpLegs.leg_a.symbol, 'futures').catch(() => null),
          getMarketTicker(fundingPerpLegs.leg_b.exchange, fundingPerpLegs.leg_b.symbol, 'futures').catch(() => null),
        ])
        setLegTicks({ a, b })
      } catch {
        setLegTicks({ a: null, b: null })
      }
    }
    fetchBoth()
    const id = window.setInterval(fetchBoth, 8000)
    return () => clearInterval(id)
  }, [fundingPerpLegs])

  const perpBasisMidPct = useMemo(() => {
    const a = legTicks.a?.mark_price
    const b = legTicks.b?.mark_price
    if (a == null || b == null || a <= 0 || b <= 0) return null
    const mid = (a + b) / 2
    return ((a - b) / mid) * 100
  }, [legTicks])

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
    if (!bot?.running || !botId) return
    const id = window.setInterval(() => {
      fetchBot()
    }, 15000)
    return () => clearInterval(id)
  }, [bot?.running, botId])

  useEffect(() => {
    if (!bot?.running || !bot?.exchange || !bot?.symbol) {
      if (!bot?.running) setPositionStatus(null)
      return
    }
    const fetchOverview = async () => {
      try {
        const mt = bot.market_type || 'futures'
        const [posRes, statRes, posStatus] = await Promise.all([
          getPositionsSummary(bot.exchange, bot.symbol, mt).catch(() => null),
          getStatistics(bot.exchange, bot.symbol, mt, botId || undefined).catch(() => null),
          botId ? getBotPositionStatus(botId).catch(() => null) : Promise.resolve(null),
        ])
        setPositionsSummary(posRes)
        setStatistics(statRes)
        setPositionStatus(posStatus ?? null)
      } catch {
        setPositionsSummary(null)
        setStatistics(null)
        setPositionStatus(null)
      }
    }
    fetchOverview()
    const interval = setInterval(fetchOverview, 10000)
    return () => clearInterval(interval)
  }, [bot?.running, bot?.exchange, bot?.symbol, bot?.market_type, botId])

  // 已停止的 Bot：仍拉取該交易對的交易所持倉（可能非本 Bot 開倉）
  useEffect(() => {
    if (bot?.running || !bot?.exchange || !bot?.symbol) {
      setExchangePositionsSummary(null)
      return
    }
    const fetchExchangePositions = async () => {
      try {
        const res = await getExchangePositionsSummary(
          bot.exchange,
          bot.symbol,
          bot.market_type || 'futures'
        )
        setExchangePositionsSummary(res)
      } catch {
        setExchangePositionsSummary(null)
      }
    }
    fetchExchangePositions()
    const interval = setInterval(fetchExchangePositions, 10000)
    return () => clearInterval(interval)
  }, [bot?.running, bot?.exchange, bot?.symbol, bot?.market_type])

  const fetchLogs = useCallback(async () => {
    if (!bot?.symbol || !botId) return
    setLogsLoading(true)
    try {
      const res = await getLogs({
        limit: logLimit,
        keyword: bot.symbol,
        exchange: bot.exchange,
        market_type: bot.market_type || 'futures',
        bot_id: botId,
        ...(logLevelFilter ? { level: logLevelFilter } : {}),
      })
      setLogs(res.logs || [])
      setLogsTotal(typeof res.total === 'number' ? res.total : 0)
    } catch {
      setLogs([])
      setLogsTotal(0)
    } finally {
      setLogsLoading(false)
    }
  }, [bot?.symbol, bot?.exchange, bot?.market_type, botId, logLevelFilter, logLimit])

  useEffect(() => {
    void fetchLogs()
  }, [fetchLogs])

  const fetchTpSlOrders = async () => {
    if (!bot?.exchange || !bot?.symbol) return
    setTpSlLoading(true)
    try {
      const now = new Date()
      const start = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000)
      const res = await getOrderHistory({
        exchange: bot.exchange,
        symbol: bot.symbol,
        market_type: bot.market_type || 'futures',
        limit: 200,
        start_time: start.toISOString(),
        end_time: now.toISOString(),
      })
      const orders = (res.orders || []).filter(
        (o: { order_source?: string }) =>
          o.order_source === 'stop_loss' || o.order_source === 'take_profit'
      )
      setTpSlOrders(orders)
    } catch {
      setTpSlOrders([])
    } finally {
      setTpSlLoading(false)
    }
  }

  useEffect(() => {
    if (bot?.exchange && bot?.symbol) fetchTpSlOrders()
    const interval = setInterval(() => {
      if (bot?.exchange && bot?.symbol) fetchTpSlOrders()
    }, 30000)
    return () => clearInterval(interval)
  }, [bot?.exchange, bot?.symbol, bot?.market_type])

  const fetchExchangeOrders = async () => {
    if (!bot?.exchange || !bot?.symbol) return
    setExchangeOrdersLoading(true)
    try {
      const res = await getExchangeOpenOrders(
        bot.exchange,
        bot.symbol,
        bot.market_type || 'futures'
      )
      setExchangeOrders(res.orders || [])
    } catch {
      setExchangeOrders([])
    } finally {
      setExchangeOrdersLoading(false)
    }
  }

  const handleCancelAllExchange = async () => {
    if (!bot?.exchange || !bot?.symbol) return
    if (!window.confirm(t('botDetail.cancelAllExchangeConfirm'))) return
    setCancellingAll(true)
    try {
      const res = await cancelAllExchangeOrders(
        bot.exchange,
        bot.symbol,
        bot.market_type || 'futures'
      )
      if (res.success) {
        toast({ title: t('botDetail.cancelAllExchangeSuccess'), description: res.message, status: 'success', duration: 3000 })
        await fetchExchangeOrders()
      } else {
        toast({ title: t('botDetail.cancelAllExchangeFailed'), description: res.message, status: 'error', duration: 4000 })
      }
    } catch (err: any) {
      toast({ title: t('botDetail.cancelAllExchangeFailed'), description: err?.message, status: 'error', duration: 4000 })
    } finally {
      setCancellingAll(false)
    }
  }

  const handlePauseOpening = async () => {
    if (!botId) return
    try {
      await pauseBotOpening(botId)
      toast({ title: t('botRiskControl.pauseSuccess'), status: 'success', duration: 2000 })
      const ps = await getBotPositionStatus(botId)
      setPositionStatus(ps)
    } catch {
      toast({ title: t('botRiskControl.configUpdateFailed'), status: 'error', duration: 3000 })
    }
  }

  const handleResumeOpening = async () => {
    if (!botId) return
    try {
      await resumeBotOpening(botId)
      toast({ title: t('botRiskControl.resumeSuccess'), status: 'success', duration: 2000 })
      const ps = await getBotPositionStatus(botId)
      setPositionStatus(ps)
    } catch {
      toast({ title: t('botRiskControl.configUpdateFailed'), status: 'error', duration: 3000 })
    }
  }

  const formatLogTime = (ts: string) => formatTimeUtil(ts, timezone, i18n.language)

  const buildFullLogText = (log: { timestamp?: string; level?: string; message?: string }) =>
    [log.timestamp || '-', `[${(log.level || 'info').toUpperCase()}]`, log.message || '-'].join(' ')

  const handleCopyLog = async (log: { timestamp?: string; level?: string; message?: string }) => {
    try {
      await navigator.clipboard.writeText(buildFullLogText(log))
      toast({ title: t('botDetail.logCopied'), status: 'success', duration: 1500 })
    } catch {
      toast({ title: t('botDetail.logCopyFailed'), status: 'error', duration: 2000 })
    }
  }

  const handleStart = async () => {
    if (!botId) return
    setActioning(true)
    try {
      const res = await startBot(botId)
      if (res.status === 'starting') {
        toast({ title: t('botList.starting'), status: 'info', duration: 3000 })
        const outcome = await pollBotUntilRunning(botId)
        if (outcome.running) {
          toast({ title: t('botList.startSuccess'), status: 'success', duration: 2000 })
        } else if (outcome.lastStartError) {
          toast({
            title: t('botList.startFailed'),
            description: outcome.lastStartError,
            status: 'error',
            duration: 12000,
            isClosable: true,
          })
        } else {
          toast({ title: t('botList.startPending'), status: 'warning', duration: 4000 })
        }
      } else {
        toast({ title: t('botList.startSuccess'), status: 'success', duration: 2000 })
      }
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
                {bot.testnet === true ? (
                  <Badge colorScheme="orange" fontSize="10px" variant="solid">
                    {t('botList.envTestnet')}
                  </Badge>
                ) : (
                  <Badge colorScheme="red" fontSize="10px" variant="outline">
                    {t('botList.envLive')}
                  </Badge>
                )}
              </HStack>
              <Heading size="md">{bot.name || bot.symbol}</Heading>
              <Text fontSize="sm" color="gray.500" mt={1}>
                {fundingPerpLegs ? (
                  <>
                    {fundingPerpLegs.leg_a.exchange} · {fundingPerpLegs.leg_a.symbol} ·{' '}
                    {fundingPerpLegs.leg_b.exchange} · {fundingPerpLegs.leg_b.symbol} ({bot.market_type})
                  </>
                ) : (
                  <>
                    {bot.exchange} · {bot.symbol} ({bot.market_type})
                  </>
                )}
              </Text>
              {bot.running && (
                <HStack spacing={4} mt={3} fontSize="sm" flexWrap="wrap">
                  {bot.market_type === 'funding_perp_spread' && fundingPerpLegs && legTicks.a && legTicks.b ? (
                    <>
                      <Text>
                        A ${legTicks.a.mark_price.toLocaleString(undefined, { minimumFractionDigits: 2 })}
                      </Text>
                      <Text>
                        B ${legTicks.b.mark_price.toLocaleString(undefined, { minimumFractionDigits: 2 })}
                      </Text>
                      {perpBasisMidPct != null && (
                        <Text color="blue.500">
                          {t('botDetail.perpBasisMid')}: {perpBasisMidPct >= 0 ? '+' : ''}
                          {perpBasisMidPct.toFixed(4)}%
                        </Text>
                      )}
                    </>
                  ) : (
                    bot.current_price != null && (
                      <Text>${bot.current_price.toLocaleString(undefined, { minimumFractionDigits: 2 })}</Text>
                    )
                  )}
                  {bot.total_pnl != null && (
                    <Text color={bot.total_pnl >= 0 ? 'green.500' : 'red.500'}>
                      PnL: {bot.total_pnl >= 0 ? '+' : ''}{bot.total_pnl.toFixed(2)}
                    </Text>
                  )}
                  {/* 平仓价估算（网格类；双永续不适用） */}
                  {bot.market_type !== 'funding_perp_spread' &&
                    (() => {
                    if (!bot.current_price || bot.current_price <= 0) {
                      return <Text color="gray.400">强平价: -</Text>
                    }

                    const leverage = bot.leverage || 1
                    const maxCapitalRatio = bot.max_capital_ratio ?? 1.0
                    const buyWindowSize = bot.buy_window_size || (bot.strategies?.find(s => s.type === 'grid') ? 50 : 20)
                    const orderQty = bot.order_quantity || 100

                    // 调试输出
                    if (process.env.NODE_ENV === 'development') {
                      console.debug('[BotDetail] 计算强平价:', {
                        botId: bot.bot_id,
                        symbol: bot.symbol,
                        currentPrice: bot.current_price,
                        buyWindowSize,
                        orderQty,
                        priceInterval: bot.price_interval,
                        totalCapital: bot.total_allocated_capital,
                        leverage,
                        maxCapitalRatio,
                      })
                    }

                    const liqEstimate = computeLiquidationPrice({
                      currentPrice: bot.current_price,
                      buyWindowSize,
                      orderQuantity: orderQty,
                      priceInterval: bot.price_interval || 0.0025,
                      totalCapital: bot.total_allocated_capital || 10000,
                      leverage,
                      maxCapitalRatio,
                    })

                    if (process.env.NODE_ENV === 'development') {
                      console.debug('[BotDetail] 计算结果:', {
                        botId: bot.bot_id,
                        liqEstimate
                      })
                    }

                    if (liqEstimate?.valid && liqEstimate.liquidationPrice > 0) {
                      return (
                        <Tooltip label={`基于最大仓位估算：${liqEstimate.positionBtc.toFixed(4)} BTC @ ${liqEstimate.avgEntryPrice.toFixed(2)} | 杠杆: ${leverage}x | 资金占用: ${Math.round(maxCapitalRatio * 100)}%`}>
                          <Text color="orange.500" fontWeight="medium">
                            强平价: ${liqEstimate.liquidationPrice.toFixed(2)}
                          </Text>
                        </Tooltip>
                      )
                    }
                    return <Text color="gray.400">强平价: -</Text>
                  })()}
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

      {!bot.running && bot.last_start_error && (
        <Alert status="error" borderRadius="md" mb={4}>
          <AlertIcon />
          <Box flex="1">
            <AlertTitle fontSize="sm">{t('botDetail.lastStartErrorTitle')}</AlertTitle>
            <AlertDescription fontSize="sm" mt={1}>
              {bot.last_start_error}
              {bot.last_start_error_at ? (
                <Text mt={2} fontSize="xs" color="gray.600">
                  {t('botDetail.lastStartErrorAt', {
                    time: formatLogTime(bot.last_start_error_at),
                  })}
                </Text>
              ) : null}
            </AlertDescription>
          </Box>
        </Alert>
      )}

      {fundingPerpLegs && (
        <Card mb={4}>
          <CardBody>
            <Heading size="sm" mb={3}>
              {t('botDetail.perpSpreadSectionTitle')}
            </Heading>
            <SimpleGrid columns={{ base: 1, md: 2 }} spacing={4}>
              <Box borderWidth="1px" borderRadius="md" p={3}>
                <Badge mb={2}>{t('botCreate.perpSpreadLegA')}</Badge>
                <Text fontSize="xs" color="gray.500">
                  {fundingPerpLegs.leg_a.exchange} · {fundingPerpLegs.leg_a.symbol}
                </Text>
                {legTicks.a ? (
                  <VStack align="stretch" spacing={1} mt={2} fontSize="sm">
                    <Text>
                      {t('botCreate.markPrice')}:{' '}
                      {legTicks.a.mark_price.toLocaleString(undefined, { minimumFractionDigits: 2 })}
                    </Text>
                    <Text>
                      {t('botDetail.lastPrice')}:{' '}
                      {legTicks.a.last_price.toLocaleString(undefined, { minimumFractionDigits: 2 })}
                    </Text>
                    <Text fontSize="xs" color="gray.500">
                      {t('botCreate.last24hHigh')} / {t('botCreate.last24hLow')}:{' '}
                      {legTicks.a.high_24h.toLocaleString(undefined, { minimumFractionDigits: 2 })} /{' '}
                      {legTicks.a.low_24h.toLocaleString(undefined, { minimumFractionDigits: 2 })}
                    </Text>
                  </VStack>
                ) : (
                  <Text mt={2} color="gray.400" fontSize="sm">
                    —
                  </Text>
                )}
              </Box>
              <Box borderWidth="1px" borderRadius="md" p={3}>
                <Badge mb={2}>{t('botCreate.perpSpreadLegB')}</Badge>
                <Text fontSize="xs" color="gray.500">
                  {fundingPerpLegs.leg_b.exchange} · {fundingPerpLegs.leg_b.symbol}
                </Text>
                {legTicks.b ? (
                  <VStack align="stretch" spacing={1} mt={2} fontSize="sm">
                    <Text>
                      {t('botCreate.markPrice')}:{' '}
                      {legTicks.b.mark_price.toLocaleString(undefined, { minimumFractionDigits: 2 })}
                    </Text>
                    <Text>
                      {t('botDetail.lastPrice')}:{' '}
                      {legTicks.b.last_price.toLocaleString(undefined, { minimumFractionDigits: 2 })}
                    </Text>
                    <Text fontSize="xs" color="gray.500">
                      {t('botCreate.last24hHigh')} / {t('botCreate.last24hLow')}:{' '}
                      {legTicks.b.high_24h.toLocaleString(undefined, { minimumFractionDigits: 2 })} /{' '}
                      {legTicks.b.low_24h.toLocaleString(undefined, { minimumFractionDigits: 2 })}
                    </Text>
                  </VStack>
                ) : (
                  <Text mt={2} color="gray.400" fontSize="sm">
                    —
                  </Text>
                )}
              </Box>
            </SimpleGrid>
            {perpBasisMidPct != null && (
              <Text mt={4} fontSize="sm" fontWeight="medium">
                {t('botDetail.perpBasisMid')}: {perpBasisMidPct >= 0 ? '+' : ''}
                {perpBasisMidPct.toFixed(4)}%
              </Text>
            )}
          </CardBody>
        </Card>
      )}

      <Tabs
        colorScheme="blue"
        variant="enclosed"
        index={mainTabIndex}
        onChange={(idx) => {
          setMainTabIndex(idx)
          if (idx === 4) void fetchExchangeOrders()
        }}
      >
        <TabList
          flexWrap="nowrap"
          overflowX={{ base: 'auto', md: 'visible' }}
          maxW="100%"
          sx={{
            scrollbarWidth: 'thin',
            '&::-webkit-scrollbar': { height: '6px' },
            '&::-webkit-scrollbar-thumb': { borderRadius: 'full', bg: 'blackAlpha.300' },
          }}
        >
          {(
            [
              t('botDetail.tabOverview'),
              t('botDetail.tabStrategy'),
              t('botDetail.tabRisk'),
              t('botDetail.tabTpSl'),
              t('botDetail.tabOrders'),
              t('botDetail.tabBacktest'),
              t('botDetail.tabLogs'),
            ] as const
          ).map((label, i) => {
            const IconComp = BOT_MAIN_TAB_ICONS[i]
            const selected = mainTabIndex === i
            const isOrdersTab = i === 4
            const ordersCount = exchangeOrders.length
            const ordersBadge = isOrdersTab && ordersCount > 0
            const iconCompactBadge = isOrdersTab && compactTabs && !selected && ordersBadge

            const tabIcon =
              iconCompactBadge ? (
                <Box position="relative" display="inline-flex" alignItems="center" justifyContent="center">
                  <TimeIcon boxSize={4} />
                  <Badge
                    position="absolute"
                    top="-4px"
                    right="-8px"
                    px={1}
                    minW={4}
                    h={4}
                    borderRadius="full"
                    fontSize="10px"
                    colorScheme="orange"
                    display="flex"
                    alignItems="center"
                    justifyContent="center"
                  >
                    {ordersCount}
                  </Badge>
                </Box>
              ) : (
                <IconComp boxSize={4} />
              )

            return (
              <Tab
                key={label}
                px={compactTabs ? 2 : 3}
                py={2}
                whiteSpace="nowrap"
                aria-label={label}
                title={compactTabs ? label : undefined}
              >
                <HStack spacing={2}>
                  <ResponsiveTabLabel
                    icon={tabIcon}
                    label={label}
                    selected={selected}
                    compact={compactTabs}
                  />
                  {ordersBadge && (!compactTabs || selected) && (
                    <Badge ml={0} colorScheme="orange" borderRadius="full" fontSize="xs">
                      {ordersCount}
                    </Badge>
                  )}
                </HStack>
              </Tab>
            )
          })}
        </TabList>
        <TabPanels>
          <TabPanel px={0}>
            {bot.running ? (
              <>
                {/* 持倉與資金（從風控移入） */}
                {positionStatus && !positionStatus.stopped && (
                  <Box mb={6}>
                    <Text fontSize="sm" color="gray.500" mb={3}>{t('botRiskControl.currentStatus')}</Text>
                    <SimpleGrid columns={{ base: 1, md: 2, lg: 5 }} spacing={4} mb={4}>
                      <Card>
                        <CardBody>
                          <Stat>
                            <StatLabel>{t('botRiskControl.totalPositionQty')}</StatLabel>
                            <StatNumber fontSize="lg">
                              {positionStatus.total_position_qty?.toFixed(4) || '-'}
                              {positionStatus.max_position_qty && (
                                <Text as="span" fontSize="sm" color="gray.500" fontWeight="normal">
                                  {' '}/ {positionStatus.max_position_qty}
                                </Text>
                              )}
                            </StatNumber>
                            {positionStatus.reached_limit_qty && (
                              <Badge colorScheme="red" size="sm" mt={1}>{t('botRiskControl.reachedLimitQty')}</Badge>
                            )}
                          </Stat>
                        </CardBody>
                      </Card>
                      <Card>
                        <CardBody>
                          <Stat>
                            <StatLabel>{t('botRiskControl.totalPositionValue')}</StatLabel>
                            <StatNumber fontSize="lg">${positionStatus.total_position_value?.toFixed(2) || '-'}</StatNumber>
                          </Stat>
                        </CardBody>
                      </Card>
                      <Card>
                        <CardBody>
                          <Stat>
                            <StatLabel>{t('botRiskControl.totalActualMargin')}</StatLabel>
                            <StatNumber fontSize="lg">
                              ${positionStatus.total_actual_margin?.toFixed(2) || '-'}
                              {positionStatus.max_position_value && (
                                <Text as="span" fontSize="sm" color="gray.500" fontWeight="normal">
                                  {' '}/ ${positionStatus.max_position_value}
                                </Text>
                              )}
                            </StatNumber>
                            {positionStatus.reached_limit_value && (
                              <Badge colorScheme="red" size="sm" mt={1}>{t('botRiskControl.reachedLimitValue')}</Badge>
                            )}
                          </Stat>
                        </CardBody>
                      </Card>
                      <Card>
                        <CardBody>
                          <Stat>
                            <StatLabel>{t('botRiskControl.positionLayers')}</StatLabel>
                            <StatNumber fontSize="lg">
                              {positionStatus.position_layers ?? '-'}
                              {positionStatus.max_position_layers && (
                                <Text as="span" fontSize="sm" color="gray.500" fontWeight="normal">
                                  {' '}/ {positionStatus.max_position_layers}
                                </Text>
                              )}
                            </StatNumber>
                            {positionStatus.reached_limit_layers && (
                              <Badge colorScheme="red" size="sm" mt={1}>{t('botRiskControl.reachedLimitLayers')}</Badge>
                            )}
                          </Stat>
                        </CardBody>
                      </Card>
                      <Card>
                        <CardBody>
                          <Stat>
                            <StatLabel>{t('botRiskControl.currentPrice')}</StatLabel>
                            <StatNumber fontSize="lg">${positionStatus.current_price?.toFixed(2) || '-'}</StatNumber>
                            {positionStatus.paused && (
                              <Badge colorScheme="orange" size="sm" mt={1}>{t('botRiskControl.paused')}</Badge>
                            )}
                          </Stat>
                        </CardBody>
                      </Card>
                    </SimpleGrid>
                    {(positionStatus.should_stop_opening || positionStatus.paused) && (
                      <Alert status="warning" borderRadius="md" mb={4}>
                        <AlertIcon />
                        <Text fontSize="sm">
                          {t('botRiskControl.shouldStopOpening')}
                          {positionStatus.paused && ` (${t('botRiskControl.paused')})`}
                        </Text>
                      </Alert>
                    )}
                    <Flex justify="flex-end" mb={4}>
                      {positionStatus.paused ? (
                        <Button size="sm" colorScheme="green" leftIcon={<PlayIcon />} onClick={handleResumeOpening}>
                          {t('botRiskControl.resume')}
                        </Button>
                      ) : (
                        <Button size="sm" colorScheme="orange" leftIcon={<PauseIcon />} onClick={handlePauseOpening}>
                          {t('botRiskControl.pause')}
                        </Button>
                      )}
                    </Flex>
                  </Box>
                )}

                {/* 【全部】指标 */}
                <Text fontSize="sm" color="gray.500" mb={3}>{t('botDetail.allTimeMetrics')}</Text>
                <SimpleGrid columns={{ base: 1, md: 2, lg: 4 }} spacing={4} mb={6}>
                  <Card>
                    <CardBody>
                      <Stat>
                        <StatLabel>
                          {fundingPerpLegs ? t('botDetail.perpMidPrice') : t('botDetail.currentPrice')}
                        </StatLabel>
                        <StatNumber>
                          {fundingPerpLegs && legTicks.a && legTicks.b
                            ? `$${((legTicks.a.mark_price + legTicks.b.mark_price) / 2).toLocaleString(undefined, { minimumFractionDigits: 2 })}`
                            : `$${(positionsSummary?.current_price ?? bot.current_price ?? 0).toLocaleString(undefined, { minimumFractionDigits: 2 })}`}
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
              <>
                {exchangePositionsSummary?.has_data ? (
                  <Box>
                    <Alert status="info" borderRadius="md" mb={4}>
                      <AlertIcon />
                      <AlertDescription>{t('botDetail.stoppedBotPositionsDisclaimer')}</AlertDescription>
                    </Alert>
                    <Text fontSize="sm" color="gray.500" mb={3}>{t('botDetail.exchangePositionsForSymbol')}</Text>
                    <SimpleGrid columns={{ base: 1, md: 2, lg: 4 }} spacing={4}>
                      <Card>
                        <CardBody>
                          <Stat>
                            <StatLabel>{t('botDetail.currentPrice')}</StatLabel>
                            <StatNumber>
                              ${(exchangePositionsSummary.current_price ?? 0).toLocaleString(undefined, { minimumFractionDigits: 2 })}
                            </StatNumber>
                          </Stat>
                        </CardBody>
                      </Card>
                      <Card>
                        <CardBody>
                          <Stat>
                            <StatLabel>{t('botDetail.unrealizedPnl')}</StatLabel>
                            <StatNumber color={(exchangePositionsSummary.unrealized_pnl ?? 0) >= 0 ? 'green.500' : 'red.500'}>
                              {(exchangePositionsSummary.unrealized_pnl ?? 0) >= 0 ? '+' : ''}
                              {(exchangePositionsSummary.unrealized_pnl ?? 0).toFixed(2)}
                            </StatNumber>
                          </Stat>
                        </CardBody>
                      </Card>
                      <Card>
                        <CardBody>
                          <Stat>
                            <StatLabel>{t('botRiskControl.totalPositionQty')}</StatLabel>
                            <StatNumber>{exchangePositionsSummary.quantity?.toFixed(4) ?? '-'}</StatNumber>
                          </Stat>
                        </CardBody>
                      </Card>
                      <Card>
                        <CardBody>
                          <Stat>
                            <StatLabel>{t('botRiskControl.totalPositionValue')}</StatLabel>
                            <StatNumber>
                              ${(exchangePositionsSummary.total_value ?? exchangePositionsSummary.quantity * (exchangePositionsSummary.current_price || 0)).toFixed(2)}
                            </StatNumber>
                          </Stat>
                        </CardBody>
                      </Card>
                    </SimpleGrid>
                  </Box>
                ) : (
                  <Text color="gray.500">
                    {fundingPerpLegs ? t('botDetail.perpOverviewStoppedHint') : t('botDetail.startToViewOverview')}
                  </Text>
                )}
              </>
            )}
          </TabPanel>
          <TabPanel px={0}>
            <BotStrategyConfigPanel botId={botId!} bot={bot} onSaved={fetchBot} />
          </TabPanel>
          <TabPanel px={0}>
            {botId && (
              <VStack align="stretch" spacing={2}>
                <BotRiskControlPanel
                  botId={botId}
                  botRunning={bot.running}
                  hidePositionStatus
                  riskTriggered={bot.risk_triggered}
                  riskTriggerMessage={bot.risk_trigger_message}
                />
                <BotRiskControlHistoryPanel botId={botId} />
                <OptionHedgePanel botId={botId} />
              </VStack>
            )}
          </TabPanel>
          <TabPanel px={0}>
            <Card>
              <CardBody>
                <Flex justify="space-between" align="center" mb={4}>
                  <Text fontWeight="medium">{t('botDetail.tabTpSl')}</Text>
                  <Button size="sm" variant="outline" onClick={fetchTpSlOrders} isLoading={tpSlLoading}>
                    {t('common.refresh')}
                  </Button>
                </Flex>
                {tpSlLoading && tpSlOrders.length === 0 ? (
                  <Flex justify="center" py={8}><Spinner /></Flex>
                ) : tpSlOrders.length > 0 ? (
                  <TableContainer maxH="400px" overflowY="auto">
                    <Table size="sm">
                      <Thead>
                        <Tr>
                          <Th>{t('botDetail.logTime')}</Th>
                          <Th>{t('orders.side')}</Th>
                          <Th>{t('orders.price')}</Th>
                          <Th>{t('orders.quantity')}</Th>
                          <Th>{t('orders.orderSource')}</Th>
                          <Th>{t('orders.exchangePnl')}</Th>
                        </Tr>
                      </Thead>
                      <Tbody>
                        {tpSlOrders.map((o: any, i: number) => (
                          <Tr key={o.order_id || i}>
                            <Td fontSize="xs" whiteSpace="nowrap">
                              {formatTimeUtil(o.created_at || o.updated_at || '', timezone, i18n.language)}
                            </Td>
                            <Td>{o.side || '-'}</Td>
                            <Td>${o.price != null ? o.price.toFixed(2) : '-'}</Td>
                            <Td>{o.filled_quantity ?? o.quantity ?? '-'}</Td>
                            <Td>
                              <Badge size="sm" colorScheme={o.order_source === 'stop_loss' ? 'red' : 'green'}>
                                {o.order_source === 'stop_loss' ? t('orders.sourceStopLoss') : t('orders.sourceTakeProfit')}
                              </Badge>
                            </Td>
                            <Td color={((o.exchange_pnl ?? o.realized_pnl) ?? 0) >= 0 ? 'green.500' : 'red.500'}>
                              {(o.exchange_pnl ?? o.realized_pnl) != null ? ((o.exchange_pnl ?? o.realized_pnl) >= 0 ? '+' : '') + (o.exchange_pnl ?? o.realized_pnl).toFixed(2) : '-'}
                            </Td>
                          </Tr>
                        ))}
                      </Tbody>
                    </Table>
                  </TableContainer>
                ) : (
                  <Text color="gray.500">{t('botDetail.noTpSlOrders')}</Text>
                )}
              </CardBody>
            </Card>
          </TabPanel>
          {/* 当前委托 tab */}
          <TabPanel px={0}>
            <Card>
              <CardBody>
                <Flex justify="space-between" align="center" mb={3}>
                  <Box>
                    <Text fontWeight="medium">{t('botDetail.exchangeOpenOrders')}</Text>
                    <Text fontSize="xs" color="gray.500" mt={0.5}>{t('botDetail.exchangeOpenOrdersDesc')}</Text>
                  </Box>
                  <HStack>
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={fetchExchangeOrders}
                      isLoading={exchangeOrdersLoading}
                    >
                      {t('botDetail.syncFromExchange')}
                    </Button>
                    <Button
                      size="sm"
                      colorScheme="red"
                      variant="solid"
                      onClick={handleCancelAllExchange}
                      isLoading={cancellingAll}
                      isDisabled={exchangeOrders.length === 0}
                    >
                      {t('botDetail.cancelAllExchange')}
                    </Button>
                  </HStack>
                </Flex>

                {/* 统计概要 */}
                {exchangeOrders.length > 0 && (
                  <HStack mb={3} spacing={4}>
                    <Badge colorScheme="blue" px={2} py={1} borderRadius="md">
                      {t('botDetail.exchangeOrderCount')}: {exchangeOrders.length}
                    </Badge>
                    <Badge colorScheme="green" px={2} py={1} borderRadius="md">
                      {t('botDetail.myOrderCount')}: {exchangeOrders.filter(o => o.is_mine).length}
                    </Badge>
                    {exchangeOrders.filter(o => !o.is_mine).length > 0 && (
                      <Badge colorScheme="orange" px={2} py={1} borderRadius="md">
                        {t('botDetail.orderIsUnknown')}: {exchangeOrders.filter(o => !o.is_mine).length}
                      </Badge>
                    )}
                  </HStack>
                )}

                {exchangeOrdersLoading && exchangeOrders.length === 0 ? (
                  <Flex justify="center" py={8}><Spinner /></Flex>
                ) : exchangeOrders.length > 0 ? (
                  <TableContainer maxH="450px" overflowY="auto">
                    <Table size="sm">
                      <Thead>
                        <Tr>
                          <Th>{t('botDetail.logTime')}</Th>
                          <Th>{t('orders.side')}</Th>
                          <Th isNumeric>{t('orders.price')}</Th>
                          <Th isNumeric>{t('orders.quantity')}</Th>
                          <Th isNumeric>{t('orders.filled')}</Th>
                          <Th>{t('orders.status')}</Th>
                          <Th>{t('orders.orderSource')}</Th>
                          <Th>Order ID</Th>
                        </Tr>
                      </Thead>
                      <Tbody>
                        {exchangeOrders.map((o) => (
                          <Tr
                            key={o.order_id}
                            bg={!o.is_mine ? 'orange.50' : undefined}
                            _dark={{ bg: !o.is_mine ? 'orange.900' : undefined }}
                          >
                            <Td fontSize="xs" whiteSpace="nowrap">
                              {formatTimeUtil(o.created_at, timezone, i18n.language)}
                            </Td>
                            <Td>
                              <Badge colorScheme={o.side === 'BUY' ? 'green' : 'red'} size="sm">
                                {o.side}
                              </Badge>
                            </Td>
                            <Td isNumeric fontFamily="mono">
                              ${o.price != null ? o.price.toFixed(2) : '-'}
                            </Td>
                            <Td isNumeric fontFamily="mono">{o.quantity ?? '-'}</Td>
                            <Td isNumeric fontFamily="mono">{o.executed_qty ?? 0}</Td>
                            <Td>
                              <Badge size="sm" colorScheme="blue">{o.status}</Badge>
                            </Td>
                            <Td>
                              {o.is_mine ? (
                                <Tooltip label={o.strategy_name || ''}>
                                  <Badge colorScheme="green" size="sm">{t('botDetail.orderIsMine')}</Badge>
                                </Tooltip>
                              ) : (
                                <Badge colorScheme="orange" size="sm">{t('botDetail.orderIsUnknown')}</Badge>
                              )}
                            </Td>
                            <Td fontSize="xs" color="gray.500" fontFamily="mono">{o.order_id}</Td>
                          </Tr>
                        ))}
                      </Tbody>
                    </Table>
                  </TableContainer>
                ) : (
                  <Flex direction="column" align="center" py={8} color="gray.400">
                    <Text>{t('botDetail.noExchangeOpenOrders')}</Text>
                    <Text fontSize="xs" mt={1}>{t('botDetail.syncFromExchange')}</Text>
                  </Flex>
                )}
              </CardBody>
            </Card>
          </TabPanel>
          <TabPanel px={0}>
            <BotBacktestPanel bot={bot} />
          </TabPanel>
          <TabPanel px={0}>
            <Card>
              <CardBody>
                <Flex
                  justify="space-between"
                  align={{ base: 'stretch', md: 'center' }}
                  mb={4}
                  gap={3}
                  direction={{ base: 'column', md: 'row' }}
                  flexWrap="wrap"
                >
                  <Text color="gray.600" flex="1" minW="0">
                    {t('botDetail.logsHint', { max: logLimit })}
                  </Text>
                  <HStack spacing={2} flexWrap="wrap" justify={{ base: 'flex-start', md: 'flex-end' }}>
                    <FormControl w={{ base: '100%', sm: '140px' }} minW="120px">
                      <FormLabel fontSize="xs" mb={1}>{t('botDetail.logLevelFilter')}</FormLabel>
                      <Select
                        size="sm"
                        value={logLevelFilter}
                        onChange={(e) => setLogLevelFilter(e.target.value as BotLogLevelFilter)}
                      >
                        <option value="">{t('botDetail.logLevelAll')}</option>
                        <option value="DEBUG">DEBUG</option>
                        <option value="INFO">INFO</option>
                        <option value="WARN">WARN</option>
                        <option value="ERROR">ERROR</option>
                        <option value="FATAL">FATAL</option>
                      </Select>
                    </FormControl>
                    <FormControl w={{ base: '100%', sm: '120px' }} minW="100px">
                      <FormLabel fontSize="xs" mb={1}>{t('botDetail.logFetchLimit')}</FormLabel>
                      <Select
                        size="sm"
                        value={logLimit}
                        onChange={(e) => setLogLimit(Number(e.target.value))}
                      >
                        {BOT_DETAIL_LOG_LIMIT_OPTIONS.map((n) => (
                          <option key={n} value={n}>{n}</option>
                        ))}
                      </Select>
                    </FormControl>
                    <Button size="sm" variant="outline" onClick={() => void fetchLogs()} isLoading={logsLoading} alignSelf={{ base: 'stretch', md: 'flex-end' }}>
                      {t('common.refresh')}
                    </Button>
                  </HStack>
                </Flex>
                <Text fontSize="xs" color="gray.500" mb={3}>
                  {t('botDetail.logsCountHint', { shown: logs.length, total: logsTotal, max: logLimit })}
                </Text>
                {logs.length > 0 ? (
                  <TableContainer maxH="min(60vh, 640px)" overflowY="auto">
                    <Table size="sm" sx={{ tableLayout: 'fixed' }}>
                      <Thead>
                        <Tr>
                          <Th w="80px">{t('botDetail.logTime')}</Th>
                          <Th w="72px">{t('botDetail.logLevel')}</Th>
                          <Th>{t('botDetail.logMessage')}</Th>
                        </Tr>
                      </Thead>
                      <Tbody>
                        {logs.map((log, i) => (
                          <Tr
                            key={log.id || i}
                            onDoubleClick={() => handleCopyLog(log)}
                            cursor="pointer"
                            title={t('botDetail.logCopyHint')}
                            _hover={{ bg: 'gray.50' }}
                          >
                            <Td fontSize="xs" whiteSpace="nowrap">
                              <Tooltip label={log.timestamp || '-'} placement="top">
                                <span>{formatLogTime(log.timestamp || '')}</span>
                              </Tooltip>
                            </Td>
                            <Td><Badge size="sm">{log.level || 'info'}</Badge></Td>
                            <Td fontSize="xs" overflow="hidden" textOverflow="ellipsis" whiteSpace="nowrap">
                              <Tooltip label={log.message || '-'} placement="top" maxW="400px">
                                <span>{log.message || '-'}</span>
                              </Tooltip>
                            </Td>
                          </Tr>
                        ))}
                      </Tbody>
                    </Table>
                  </TableContainer>
                ) : logsLoading ? (
                  <Flex justify="center" py={8}><Spinner /></Flex>
                ) : (
                  <Text color="gray.500">{t('botDetail.noLogs')}</Text>
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

// 策略参数定义映射（用于展示各策略专属参数）
const STRATEGY_PARAM_DEFS: Record<string, Array<{ key: string; labelKey: string; fallback: string }>> = {
  grid: [
    { key: 'grid_spacing', labelKey: 'backtest.paramLabels.grid_spacing', fallback: 'Grid Spacing' },
    { key: 'profit_spread', labelKey: 'backtest.paramLabels.profit_spread', fallback: 'Profit Spread' },
    { key: 'grid_count', labelKey: 'backtest.paramLabels.grid_count', fallback: 'Grid Count' },
    { key: 'order_quantity', labelKey: 'backtest.paramLabels.order_quantity', fallback: 'Order Quantity' },
  ],
  trend_following: [
    { key: 'fast_period', labelKey: 'backtest.paramLabels.fast_period', fallback: 'Fast Period' },
    { key: 'slow_period', labelKey: 'backtest.paramLabels.slow_period', fallback: 'Slow Period' },
  ],
  momentum: [
    { key: 'rsi_period', labelKey: 'backtest.paramLabels.rsi_period', fallback: 'RSI Period' },
  ],
  mean_reversion: [
    { key: 'period', labelKey: 'backtest.paramLabels.period', fallback: 'Period' },
  ],
  'grid+trend': [
    { key: 'grid_weight', labelKey: 'botDetail.strategy.gridComboGridWeight', fallback: 'Grid weight' },
    { key: 'trend_weight', labelKey: 'botDetail.strategy.gridComboTrendWeight', fallback: 'Trend weight' },
  ],
  dca: [
    { key: 'interval_days', labelKey: 'backtest.paramLabels.interval_days', fallback: 'DCA interval (days)' },
    { key: 'amount_per_trade', labelKey: 'backtest.paramLabels.amount_per_trade', fallback: 'Amount per trade' },
  ],
  martingale: [
    { key: 'base_amount', labelKey: 'backtest.paramLabels.base_amount', fallback: 'Base amount' },
    { key: 'multiplier', labelKey: 'backtest.paramLabels.multiplier', fallback: 'Multiplier' },
  ],
}

function strategyUsesGridParams(strategyType: string): boolean {
  return strategyType === 'grid' || strategyType.startsWith('grid+')
}

function shouldShowStrategySpecificForm(strategyType: string): boolean {
  if (strategyType === 'grid' || strategyType === 'grid+dca' || strategyType === 'grid+martingale') return false
  if (strategyType === 'grid+trend') return true
  const defs = STRATEGY_PARAM_DEFS[strategyType]
  return defs != null && defs.length > 0
}

function buildStrategyConfigPayload(
  strategyType: string,
  paramValues: Record<string, string>
): Record<string, unknown> {
  const defs = STRATEGY_PARAM_DEFS[strategyType] || []
  const out: Record<string, unknown> = {}
  for (const d of defs) {
    const raw = (paramValues[d.key] ?? '').trim()
    if (raw === '') continue
    const num = parseFloat(raw)
    if (!Number.isNaN(num)) out[d.key] = num
  }
  return out
}

// BotBacktestPanel Bot 回测面板：展示参数并跳转全局回测
const BotBacktestPanel: React.FC<{ bot: BotDetailInfo | null }> = ({ bot }) => {
  const { t } = useTranslation()
  const cfg = bot?.config as Record<string, unknown> | undefined
  const openCtrl = cfg?.open_position_control as Record<string, unknown> | undefined
  const strategies = cfg?.strategies as Array<{ type?: string; weight?: number; config?: Record<string, unknown> }> | undefined

  // 计算网格数量
  const gridCount = (() => {
    const maxLayers = openCtrl?.max_position_layers
    if (typeof maxLayers === 'number' && maxLayers > 0) return maxLayers
    const buyWin = cfg?.buy_window_size as number | undefined
    const sellWin = cfg?.sell_window_size as number | undefined
    return (typeof buyWin === 'number' ? buyWin : 0) + (typeof sellWin === 'number' ? sellWin : 0)
  })()

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

          {/* 策略组合概览 */}
          {strategies && strategies.length > 0 && (
            <Box>
              <Text fontSize="sm" fontWeight="medium" color="gray.600" mb={2}>
                {t('botDetail.strategyType')}
              </Text>
              <HStack spacing={2} flexWrap="wrap">
                {strategies.map((s, idx) => (
                  <Badge key={idx} colorScheme="blue" fontSize="sm" px={2} py={1}>
                    {t(`backtest.strategyNames.${s.type}`, { defaultValue: s.type })}
                    {s.weight != null && s.weight > 0 && ` (${Math.round(s.weight * 100)}%)`}
                  </Badge>
                ))}
              </HStack>
            </Box>
          )}

          {/* 网格基础参数 */}
          <SimpleGrid columns={{ base: 1, md: 2, lg: 3 }} spacing={4}>
            <Stat>
              <StatLabel>{t('botDetail.maxPositionValue')}</StatLabel>
              <StatNumber>
                {openCtrl?.max_position_value ?? 0} USDT
              </StatNumber>
              <StatHelpText>{t('backtest.actualMarginUsed')}</StatHelpText>
            </Stat>
            <Stat>
              <StatLabel>{t('botDetail.priceInterval')}</StatLabel>
              <StatNumber>{cfg?.price_interval ?? 0} USDT</StatNumber>
              <StatHelpText>{t('botDetail.strategy.priceIntervalHint')}</StatHelpText>
            </Stat>
            <Stat>
              <StatLabel>{t('botDetail.profitSpread')}</StatLabel>
              <StatNumber>{cfg?.profit_spread ?? 0} USDT</StatNumber>
              <StatHelpText>{t('botDetail.strategy.profitSpreadHint')}</StatHelpText>
            </Stat>
            <Stat>
              <StatLabel>{t('botDetail.orderQuantity')}</StatLabel>
              <StatNumber>{cfg?.order_quantity ?? 0} USDT</StatNumber>
            </Stat>
            <Stat>
              <StatLabel>{t('botDetail.gridCount')}</StatLabel>
              <StatNumber>{gridCount || '-'}</StatNumber>
              <StatHelpText>{t('backtest.maxLayers')}</StatHelpText>
            </Stat>
            <Stat>
              <StatLabel>{t('botDetail.priceLow')}</StatLabel>
              <StatNumber>{cfg?.price_low ?? 0} USDT</StatNumber>
            </Stat>
            <Stat>
              <StatLabel>{t('botDetail.priceHigh')}</StatLabel>
              <StatNumber>{cfg?.price_high ?? 0} USDT</StatNumber>
            </Stat>
          </SimpleGrid>

          {/* 各策略专属参数 */}
          {strategies && strategies.length > 0 && strategies.some(s => s.type !== 'grid' && s.config && Object.keys(s.config).length > 0) && (
            <>
              <Divider />
              <Text fontSize="sm" fontWeight="medium" color="gray.600">
                {t('botDetail.strategySpecificParams')}
              </Text>
              {strategies.filter(s => s.type !== 'grid').map((s, idx) => {
                const paramDefs = STRATEGY_PARAM_DEFS[s.type || ''] || []
                const configEntries = s.config ? Object.entries(s.config) : []
                if (paramDefs.length === 0 && configEntries.length === 0) return null
                return (
                  <Box key={idx}>
                    <Badge colorScheme="purple" mb={2}>
                      {t(`backtest.strategyNames.${s.type}`, { defaultValue: s.type })}
                    </Badge>
                    <SimpleGrid columns={{ base: 1, md: 2, lg: 3 }} spacing={3}>
                      {paramDefs.map((pd) => {
                        const val = s.config?.[pd.key]
                        return (
                          <Stat key={pd.key} size="sm">
                            <StatLabel fontSize="xs">{t(pd.labelKey, { defaultValue: pd.fallback })}</StatLabel>
                            <StatNumber fontSize="md">{val != null ? String(val) : '-'}</StatNumber>
                          </Stat>
                        )
                      })}
                      {configEntries
                        .filter(([k]) => !paramDefs.some(pd => pd.key === k))
                        .map(([k, v]) => (
                          <Stat key={k} size="sm">
                            <StatLabel fontSize="xs">{t(`backtest.paramLabels.${k}`, { defaultValue: k })}</StatLabel>
                            <StatNumber fontSize="md">{v != null ? String(v) : '-'}</StatNumber>
                          </Stat>
                        ))
                      }
                    </SimpleGrid>
                  </Box>
                )
              })}
            </>
          )}

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
  onSaved?: () => void | Promise<void>
}

const BotStrategyConfigPanel: React.FC<BotStrategyConfigPanelProps> = ({ botId, bot, onSaved }) => {
  const { t } = useTranslation()
  const toast = useToast()

  const [strategyType, setStrategyType] = useState<string>('grid')
  const [originalStrategyType, setOriginalStrategyType] = useState<string>('')
  const [priceInterval, setPriceInterval] = useState<string>('')
  const [profitSpread, setProfitSpread] = useState<string>('')
  const [orderQuantity, setOrderQuantity] = useState<string>('')
  const [priceLow, setPriceLow] = useState<string>('')
  const [priceHigh, setPriceHigh] = useState<string>('')
  const [direction, setDirection] = useState<string>('LONG')
  const [hasChanges, setHasChanges] = useState(false)
  const [saving, setSaving] = useState(false)

  // 智能挂单配置状态
  const [smartOrderEnabled, setSmartOrderEnabled] = useState(false)
  const [smartOrderMaxOpenOrders, setSmartOrderMaxOpenOrders] = useState('3')
  const [smartOrderOpenOrderDistance, setSmartOrderOpenOrderDistance] = useState('5')

  // 三级火箭网格
  const [rocketTieredGridEnabled, setRocketTieredGridEnabled] = useState(false)

  // 非网格策略 / grid+trend 组合权重等专属参数（与 strategies[0].config 同步）
  const [strategyParamValues, setStrategyParamValues] = useState<Record<string, string>>({})

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
      setDirection(cfg.direction || 'LONG')

      // 初始化智能挂单配置
      const smartOrder = cfg.smart_order || {}
      setSmartOrderEnabled(smartOrder.enabled || false)
      setSmartOrderMaxOpenOrders(smartOrder.max_open_orders?.toString() || '3')
      setSmartOrderOpenOrderDistance(smartOrder.open_order_distance?.toString() || '5')

      // 三级火箭网格
      const rtg = cfg.rocket_tiered_grid as { enabled?: boolean } | undefined
      setRocketTieredGridEnabled(rtg?.enabled ?? false)

      const firstType = cfg.strategies?.[0]?.type || 'grid'
      const firstStratCfg = cfg.strategies?.[0]?.config as Record<string, unknown> | undefined
      const initParams: Record<string, string> = {}
      for (const d of STRATEGY_PARAM_DEFS[firstType] || []) {
        const v = firstStratCfg?.[d.key]
        initParams[d.key] = v != null && v !== '' ? String(v) : ''
      }
      setStrategyParamValues(initParams)
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
      const stratCfg = buildStrategyConfigPayload(strategyType, strategyParamValues)
      const updateData: UpdateBotStrategyRequest = {
        strategies: [{
          type: strategyType,
          weight: 1.0,
          config: stratCfg,
        }],
      }

      // 只添加有值的字段
      if (priceInterval) updateData.price_interval = parseFloat(priceInterval)
      if (profitSpread) updateData.profit_spread = parseFloat(profitSpread)
      if (orderQuantity) updateData.order_quantity = parseFloat(orderQuantity)
      if (priceLow) updateData.price_low = parseFloat(priceLow)
      if (priceHigh) updateData.price_high = parseFloat(priceHigh)
      if (direction) updateData.direction = direction

      // 智能挂单配置
      updateData.smart_order_enabled = smartOrderEnabled
      if (smartOrderEnabled) {
        updateData.smart_order_max_open_orders = parseInt(smartOrderMaxOpenOrders) || 3
        updateData.smart_order_open_order_distance = parseFloat(smartOrderOpenOrderDistance) || 5
      }

      // 三级火箭网格
      updateData.rocket_tiered_grid = rocketTieredGridEnabled
        ? {
            enabled: true,
            tiers: [
              { filled_threshold: 4, interval: 100, profit_spread: 100 },
              { filled_threshold: 8, interval: 300, profit_spread: 300 },
              { filled_threshold: 0, interval: 600, profit_spread: 600 },
            ],
          }
        : { enabled: false, tiers: [] }

      await updateBotStrategy(botId, updateData)
      toast({
        title: t('botDetail.strategy.saveSuccess'),
        status: 'success',
        duration: 2000,
      })
      setHasChanges(false)
      setOriginalStrategyType(strategyType) // 更新原始策略类型
      await onSaved?.() // 刷新 Bot 詳情，確保 smart_order 等配置與後端一致
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
      setDirection(cfg.direction || 'LONG')

      // 重置智能挂单配置
      const smartOrder = cfg.smart_order || {}
      setSmartOrderEnabled(smartOrder.enabled || false)
      setSmartOrderMaxOpenOrders(smartOrder.max_open_orders?.toString() || '3')
      setSmartOrderOpenOrderDistance(smartOrder.open_order_distance?.toString() || '5')

      const rtgReset = cfg.rocket_tiered_grid as { enabled?: boolean } | undefined
      setRocketTieredGridEnabled(rtgReset?.enabled ?? false)

      const firstType = cfg.strategies?.[0]?.type || 'grid'
      const firstStratCfg = cfg.strategies?.[0]?.config as Record<string, unknown> | undefined
      const initParams: Record<string, string> = {}
      for (const d of STRATEGY_PARAM_DEFS[firstType] || []) {
        const v = firstStratCfg?.[d.key]
        initParams[d.key] = v != null && v !== '' ? String(v) : ''
      }
      setStrategyParamValues(initParams)

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
            {/* 多策略时展示全部策略及权重，与列表页一致 */}
            {bot?.config?.strategies && (bot.config as any).strategies.length > 1 ? (
              <>
                <HStack spacing={1} flexWrap="wrap" mb={2}>
                  {(bot.config as any).strategies.map((s: { type: string; weight?: number }, idx: number) => (
                    <Badge
                      key={idx}
                      size="sm"
                      variant="outline"
                      fontSize="10px"
                      colorScheme="blue"
                    >
                      {t(`strategyRuntime.strategyNames.${s.type}`, { defaultValue: s.type })}
                      {s.weight != null && s.weight > 0 && ` (${Math.round(s.weight * 100)}%)`}
                    </Badge>
                  ))}
                </HStack>
                <Text fontSize="xs" color="gray.500" mt={1}>
                  {t('botDetail.strategy.typeHint')}
                </Text>
                {bot.running && (
                  <Text fontSize="xs" color="orange.500" mt={1}>
                    {t('botDetail.strategy.typeChangeDisabledRunning')}
                  </Text>
                )}
              </>
            ) : (
              <>
                <Select
                  value={strategyType}
                  onChange={(e) => {
                    const next = e.target.value
                    setStrategyType(next)
                    setHasChanges(true)
                    const nextInit: Record<string, string> = {}
                    for (const d of STRATEGY_PARAM_DEFS[next] || []) {
                      nextInit[d.key] = ''
                    }
                    setStrategyParamValues(nextInit)
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
              </>
            )}
          </FormControl>
        </CardBody>
      </Card>

      {/* 网格参数配置（网格及 grid+ 组合中需要网格的腿） */}
      {strategyUsesGridParams(strategyType) && (
      <Card>
        <CardBody>
          <Heading size="sm" mb={4}>{t('botDetail.strategy.gridParams')}</Heading>
          <VStack align="stretch" spacing={4}>
            {/* 网格方向 */}
            <FormControl>
              <FormLabel>{t('botDetail.strategy.direction')}</FormLabel>
              <Select
                value={direction || 'LONG'}
                onChange={(e) => {
                  setDirection(e.target.value)
                  setHasChanges(true)
                }}
              >
                <option value="LONG">{t('botDetail.strategy.directionLong')}</option>
                <option value="SHORT">{t('botDetail.strategy.directionShort')}</option>
                <option value="BOTH">{t('botDetail.strategy.directionBoth')}</option>
              </Select>
              <Text fontSize="xs" color="gray.500" mt={1}>
                {t('botCreate.directionHint')}
              </Text>
            </FormControl>

            <Divider />

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

            <Divider />

            <FormControl display="flex" alignItems="center" gap={3}>
              <FormLabel htmlFor="rocket-tiered-grid" mb={0}>
                {t('botDetail.strategy.rocketTieredGrid')}
              </FormLabel>
              <Switch
                id="rocket-tiered-grid"
                isChecked={rocketTieredGridEnabled}
                onChange={(e) => {
                  setRocketTieredGridEnabled(e.target.checked)
                  setHasChanges(true)
                }}
                isDisabled={bot.running}
              />
              <Text fontSize="xs" color="gray.500">
                {t('botDetail.strategy.rocketTieredGridHint')}
              </Text>
            </FormControl>
          </VStack>
        </CardBody>
      </Card>
      )}

      {/* 趋势/动量/均值回归/DCA/马丁/网格+趋势权重等专属参数 */}
      {shouldShowStrategySpecificForm(strategyType) && (
      <Card>
        <CardBody>
          <Heading size="sm" mb={4}>{t('botDetail.strategy.strategySpecificParamsEditable')}</Heading>
          <VStack align="stretch" spacing={4}>
            {(STRATEGY_PARAM_DEFS[strategyType] || []).map((pd) => (
              <FormControl key={pd.key}>
                <FormLabel>{t(pd.labelKey, { defaultValue: pd.fallback })}</FormLabel>
                <NumberInput
                  value={strategyParamValues[pd.key] ?? ''}
                  onChange={(valueString) => {
                    setStrategyParamValues((prev) => ({ ...prev, [pd.key]: valueString }))
                    setHasChanges(true)
                  }}
                  min={0}
                >
                  <NumberInputField />
                  <NumberInputStepper>
                    <NumberIncrementStepper />
                    <NumberDecrementStepper />
                  </NumberInputStepper>
                </NumberInput>
              </FormControl>
            ))}
          </VStack>
        </CardBody>
      </Card>
      )}

      {/* 智能挂单配置（仅网格类策略） */}
      {strategyUsesGridParams(strategyType) && (
      <Card>
        <CardBody>
          <Heading size="sm" mb={4}>🧠 {t('botDetail.strategy.smartOrderConfig')}</Heading>
          <VStack align="stretch" spacing={4}>
            <Alert status="info" borderRadius="md" py={2}>
              <AlertIcon />
              <AlertDescription fontSize="sm">
                {t('botDetail.strategy.smartOrderDescription')}
              </AlertDescription>
            </Alert>

            <FormControl display="flex" alignItems="center" gap={3}>
              <FormLabel htmlFor="smart-order-enabled" mb={0}>
                {t('botDetail.strategy.smartOrderEnabled')}
              </FormLabel>
              <Switch
                id="smart-order-enabled"
                isChecked={smartOrderEnabled}
                onChange={(e) => {
                  setSmartOrderEnabled(e.target.checked)
                  setHasChanges(true)
                }}
                isDisabled={bot.running}
              />
            </FormControl>

            {smartOrderEnabled && (
              <>
                <Divider />

                <FormControl>
                  <FormLabel>{t('botDetail.strategy.maxOpenOrders')}</FormLabel>
                  <NumberInput
                    value={smartOrderMaxOpenOrders}
                    onChange={(valueString) => {
                      setSmartOrderMaxOpenOrders(valueString)
                      setHasChanges(true)
                    }}
                    min={1}
                    max={10}
                    step={1}
                  >
                    <NumberInputField />
                    <NumberInputStepper>
                      <NumberIncrementStepper />
                      <NumberDecrementStepper />
                    </NumberInputStepper>
                  </NumberInput>
                  <Text fontSize="xs" color="gray.500" mt={1}>
                    {t('botDetail.strategy.maxOpenOrdersHint')}
                  </Text>
                </FormControl>

                <FormControl>
                  <FormLabel>{t('botDetail.strategy.openOrderDistance')}</FormLabel>
                  <NumberInput
                    value={smartOrderOpenOrderDistance}
                    onChange={(valueString) => {
                      setSmartOrderOpenOrderDistance(valueString)
                      setHasChanges(true)
                    }}
                    min={1}
                    max={20}
                    step={1}
                  >
                    <NumberInputField />
                    <NumberInputStepper>
                      <NumberIncrementStepper />
                      <NumberDecrementStepper />
                    </NumberInputStepper>
                  </NumberInput>
                  <Text fontSize="xs" color="gray.500" mt={1}>
                    {t('botDetail.strategy.openOrderDistanceHint')}
                  </Text>
                </FormControl>

                <Alert status="success" borderRadius="md" py={2} mt={2}>
                  <AlertIcon />
                  <AlertDescription fontSize="xs">
                    {t('botDetail.strategy.smartOrderEffect', { count: smartOrderMaxOpenOrders })}
                  </AlertDescription>
                </Alert>
              </>
            )}
          </VStack>
        </CardBody>
      </Card>
      )}

      {/* 各策略专属参数展示（只读） */}
      {bot?.config?.strategies && (bot.config as any).strategies.length > 1 && (() => {
        const strats = (bot.config as any).strategies as Array<{ type: string; weight?: number; config?: Record<string, unknown> }>
        const nonGridStrats = strats.filter(s => s.type !== 'grid')
        if (nonGridStrats.length === 0) return null
        return (
          <Card>
            <CardBody>
              <Heading size="sm" mb={4}>{t('botDetail.strategySpecificParams')}</Heading>
              <VStack align="stretch" spacing={4}>
                {nonGridStrats.map((s, idx) => {
                  const paramDefs = STRATEGY_PARAM_DEFS[s.type] || []
                  const cfgEntries = s.config ? Object.entries(s.config) : []
                  return (
                    <Box key={idx}>
                      <HStack mb={2}>
                        <Badge colorScheme="purple">
                          {t(`backtest.strategyNames.${s.type}`, { defaultValue: s.type })}
                        </Badge>
                        {s.weight != null && s.weight > 0 && (
                          <Badge colorScheme="blue">{Math.round(s.weight * 100)}%</Badge>
                        )}
                      </HStack>
                      <SimpleGrid columns={{ base: 1, md: 2, lg: 3 }} spacing={3}>
                        {paramDefs.map((pd) => (
                          <Box key={pd.key}>
                            <Text fontSize="xs" color="gray.500">{t(pd.labelKey, { defaultValue: pd.fallback })}</Text>
                            <Text fontSize="md" fontWeight="semibold">{s.config?.[pd.key] != null ? String(s.config[pd.key]) : '-'}</Text>
                          </Box>
                        ))}
                        {cfgEntries
                          .filter(([k]) => !paramDefs.some(pd => pd.key === k))
                          .map(([k, v]) => (
                            <Box key={k}>
                              <Text fontSize="xs" color="gray.500">{t(`backtest.paramLabels.${k}`, { defaultValue: k })}</Text>
                              <Text fontSize="md" fontWeight="semibold">{v != null ? String(v) : '-'}</Text>
                            </Box>
                          ))
                        }
                      </SimpleGrid>
                    </Box>
                  )
                })}
              </VStack>
            </CardBody>
          </Card>
        )
      })()}

      {/* 网格方向与风控配置（只读显示） */}
      <Card>
        <CardBody>
          <Heading size="sm" mb={4}>{t('botDetail.strategy.gridDirectionAndRisk')}</Heading>
          <VStack align="stretch" spacing={4}>
            {/* 网格方向 */}
            <SimpleGrid columns={{ base: 1, md: 2 }} spacing={4}>
              <Box>
                <Text fontSize="sm" fontWeight="medium" color="gray.600">
                  {t('botDetail.strategy.direction')}
                </Text>
                <Text fontSize="lg" fontWeight="bold" mt={1}>
                  {(() => {
                    const cfg = bot?.config as any
                    const direction = cfg?.direction || cfg?.grid_mode || 'LONG'
                    const directionMap: Record<string, string> = {
                      'LONG': t('botDetail.strategy.directionLong'),
                      'SHORT': t('botDetail.strategy.directionShort'),
                      'BOTH': t('botDetail.strategy.directionBoth'),
                      'long': t('botDetail.strategy.directionLong'),
                      'short': t('botDetail.strategy.directionShort'),
                      'both': t('botDetail.strategy.directionBoth'),
                    }
                    return directionMap[direction] || direction
                  })()}
                </Text>
              </Box>

              {/* 趋势检测 */}
              <Box>
                <Text fontSize="sm" fontWeight="medium" color="gray.600">
                  {t('botDetail.strategy.trendFilter')}
                </Text>
                <Badge mt={1} colorScheme={(() => {
                  const cfg = bot?.config as any
                  const enabled = cfg?.grid_risk_control?.trend_filter_enabled || false
                  return enabled ? 'green' : 'gray'
                })()}>
                  {(() => {
                    const cfg = bot?.config as any
                    const enabled = cfg?.grid_risk_control?.trend_filter_enabled || false
                    return enabled ? t('common.enabled', { defaultValue: '启用' }) : t('common.disabled', { defaultValue: '禁用' })
                  })()}
                </Badge>
              </Box>
            </SimpleGrid>

            <Divider />

            {/* 网格风控配置 */}
            <Text fontSize="sm" fontWeight="medium" color="gray.600">
              {t('botDetail.strategy.gridRiskControl')}
            </Text>

            <SimpleGrid columns={{ base: 1, md: 2 }} spacing={4}>
              {/* 止损比例 */}
              <Box>
                <Text fontSize="xs" color="gray.500">{t('botDetail.strategy.stopLossRatio')}</Text>
                <Text fontSize="md" fontWeight="semibold">
                  {(() => {
                    const cfg = bot?.config as any
                    const ratio = cfg?.grid_risk_control?.stop_loss_ratio ?? cfg?.stop_loss_ratio
                    return ratio !== undefined && ratio !== null ? `${(ratio * 100).toFixed(1)}%` : '-'
                  })()}
                </Text>
              </Box>

              {/* 止盈触发比例 */}
              <Box>
                <Text fontSize="xs" color="gray.500">{t('botDetail.strategy.takeProfitTriggerRatio')}</Text>
                <Text fontSize="md" fontWeight="semibold">
                  {(() => {
                    const cfg = bot?.config as any
                    const ratio = cfg?.grid_risk_control?.take_profit_trigger_ratio ?? cfg?.take_profit_ratio
                    return ratio !== undefined && ratio !== null ? `${(ratio * 100).toFixed(1)}%` : '-'
                  })()}
                </Text>
              </Box>

              {/* 移动止盈比例 */}
              <Box>
                <Text fontSize="xs" color="gray.500">{t('botDetail.strategy.trailingTakeProfitRatio')}</Text>
                <Text fontSize="md" fontWeight="semibold">
                  {(() => {
                    const cfg = bot?.config as any
                    const ratio = cfg?.grid_risk_control?.trailing_take_profit_ratio ?? cfg?.trailing_stop_ratio
                    return ratio !== undefined && ratio !== null ? `${(ratio * 100).toFixed(1)}%` : '-'
                  })()}
                </Text>
              </Box>

              {/* 最大网格层数 */}
              <Box>
                <Text fontSize="xs" color="gray.500">{t('botDetail.strategy.maxGridLayers')}</Text>
                <Text fontSize="md" fontWeight="semibold">
                  {(() => {
                    const cfg = bot?.config as any
                    const layers = cfg?.grid_risk_control?.max_grid_layers
                    return layers ? `${layers} ${t('botDetail.strategy.layers')}` : '-'
                  })()}
                </Text>
              </Box>

              {/* 达到层数限制时的最大挂单数 */}
              <Box>
                <Text fontSize="xs" color="gray.500">{t('botDetail.strategy.maxOpenOrdersAtCap')}</Text>
                <Text fontSize="md" fontWeight="semibold">
                  {(() => {
                    const cfg = bot?.config as any
                    const orders = cfg?.grid_risk_control?.max_open_orders_at_cap
                    return orders !== undefined && orders !== null ? `${orders}` : '-'
                  })()}
                </Text>
              </Box>

              {/* 风控启用状态 */}
              <Box>
                <Text fontSize="xs" color="gray.500">{t('botDetail.strategy.riskControlEnabled')}</Text>
                <Badge mt={1} colorScheme={(() => {
                  const cfg = bot?.config as any
                  const enabled = cfg?.grid_risk_control?.enabled || false
                  return enabled ? 'green' : 'gray'
                })()}>
                  {(() => {
                    const cfg = bot?.config as any
                    const enabled = cfg?.grid_risk_control?.enabled || false
                    return enabled ? t('common.enabled', { defaultValue: '启用' }) : t('common.disabled', { defaultValue: '禁用' })
                  })()}
                </Badge>
              </Box>
            </SimpleGrid>

            <Alert status="info" fontSize="sm">
              <AlertIcon />
              <Text>{t('botDetail.strategy.riskConfigHint')}</Text>
            </Alert>
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
