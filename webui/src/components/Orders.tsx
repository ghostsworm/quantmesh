import React, { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useSymbol } from '../contexts/SymbolContext'
import { useConfig } from '../contexts/ConfigContext'
import { formatDateTime } from '../utils/dateFormat'
import {
  Box,
  Heading,
  Tabs,
  TabList,
  TabPanels,
  Tab,
  TabPanel,
  Table,
  Thead,
  Tbody,
  Tr,
  Th,
  Td,
  TableContainer,
  Badge,
  SimpleGrid,
  Card,
  CardBody,
  Stat,
  StatLabel,
  StatNumber,
  Text,
  Spinner,
  Center,
  Button,
  IconButton,
  useToast,
  Tooltip,
  HStack,
  Select,
  VStack,
  Flex,
  Modal,
  ModalOverlay,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalCloseButton,
  useDisclosure,
  Divider,
  Input,
  FormControl,
  FormLabel,
  FormErrorMessage,
} from '@chakra-ui/react'
import { CloseIcon } from '@chakra-ui/icons'
import { getPendingOrders, getOrderHistory, cancelOrder, batchCancelOrders, syncOrders, getSymbols, getTradeDetails, PendingOrderInfo, TradeDetailResponse } from '../services/api'
import { computeOrderTotalPrice, computeOrderCapitalUsage } from '../utils/orderCalculations'

interface OrderInfo {
  order_id: number
  client_order_id: string
  symbol: string
  side: string
  exchange?: string
  type?: string
  price: number
  quantity: number
  filled_qty?: number
  status: string
  created_at: string
  updated_at: string
  pnl?: number | null           // 网格策略盈亏（基于买卖配对）
  exchange_pnl?: number | null  // 交易所已实现盈亏（基于加权平均成本）
  strategy_name?: string
  strategy_type?: string
  order_source?: string         // 订单来源（normal=正常限价, stop_loss=止损平仓）
}

const Orders: React.FC = () => {
  const { t, i18n } = useTranslation()
  const { selectedExchange, selectedSymbol, selectedMarketType } = useSymbol()
  const { timezone } = useConfig()
  const [pendingOrders, setPendingOrders] = useState<PendingOrderInfo[]>([])
  const [pendingLeverage, setPendingLeverage] = useState<number>(1)
  const [historyOrders, setHistoryOrders] = useState<OrderInfo[]>([])
  const [historyLeverage, setHistoryLeverage] = useState<number>(1)
  const [historyTotalCount, setHistoryTotalCount] = useState<number>(0)
  const [historyTodayCount, setHistoryTodayCount] = useState<number>(0)
  const [tabIndex, setTabIndex] = useState(0)
  const [loading, setLoading] = useState(true)
  const [cancellingOrderId, setCancellingOrderId] = useState<number | null>(null)
  const [cancellingAll, setCancellingAll] = useState(false)
  const [syncingOrders, setSyncingOrders] = useState(false)
  const toast = useToast()
  
  // 筛选状态
  const [pendingFilterStrategy, setPendingFilterStrategy] = useState<string>('all')
  const [pendingFilterType, setPendingFilterType] = useState<string>('all')
  const [pendingFilterStatus, setPendingFilterStatus] = useState<string>('all')
  const [pendingFilterSide, setPendingFilterSide] = useState<string>('all')
  const [historyFilterStrategy, setHistoryFilterStrategy] = useState<string>('all')
  const [historyFilterType, setHistoryFilterType] = useState<string>('all')
  const [historyFilterOrderSource, setHistoryFilterOrderSource] = useState<string>('all')
  const [historyFilterStatus, setHistoryFilterStatus] = useState<string>('all')
  const [historyFilterSide, setHistoryFilterSide] = useState<string>('all')
  const [symbolDirection, setSymbolDirection] = useState<'LONG' | 'SHORT' | null>(null)
  
  // 历史订单时间范围状态（默认最近24小时）
  const getDefaultTimeRange = () => {
    const now = new Date()
    const endTime = new Date(now.getTime() - now.getTimezoneOffset() * 60000).toISOString().slice(0, 16)
    const startTime = new Date(now.getTime() - 24 * 60 * 60 * 1000 - now.getTimezoneOffset() * 60000).toISOString().slice(0, 16)
    return { startTime, endTime }
  }
  const [historyStartTime, setHistoryStartTime] = useState<string>(getDefaultTimeRange().startTime)
  const [historyEndTime, setHistoryEndTime] = useState<string>(getDefaultTimeRange().endTime)
  
  // 将 datetime-local 格式转换为 RFC3339 格式
  const toRFC3339 = (datetimeLocal: string): string => {
    if (!datetimeLocal) return ''
    // datetime-local 格式: YYYY-MM-DDTHH:mm
    // RFC3339 格式: YYYY-MM-DDTHH:mm:ssZ
    return new Date(datetimeLocal).toISOString()
  }
  
  // 验证时间范围
  const validateTimeRange = (): { valid: boolean; error?: string } => {
    if (!historyStartTime || !historyEndTime) {
      return { valid: false, error: t('orders.timeRangeRequired') }
    }
    
    const start = new Date(historyStartTime)
    const end = new Date(historyEndTime)
    
    if (end < start) {
      return { valid: false, error: t('orders.timeRangeInvalid') }
    }
    
    const diffDays = (end.getTime() - start.getTime()) / (1000 * 60 * 60 * 24)
    if (diffDays > 7) {
      return { valid: false, error: t('orders.timeRangeMaxDays') }
    }
    
    return { valid: true }
  }

  // 🔥 成交明细 Modal 状态
  const { isOpen: isDetailOpen, onOpen: onDetailOpen, onClose: onDetailClose } = useDisclosure()
  const [tradeDetail, setTradeDetail] = useState<TradeDetailResponse | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)

  const handleShowTradeDetail = async (orderID: number) => {
    setDetailLoading(true)
    onDetailOpen()
    try {
      const detail = await getTradeDetails(orderID)
      setTradeDetail(detail)
    } catch {
      toast({ title: t('orders.loadDetailFailed'), status: 'error', duration: 3000 })
    } finally {
      setDetailLoading(false)
    }
  }

  useEffect(() => {
    const mt = selectedExchange && selectedSymbol ? (selectedMarketType ?? 'futures') : undefined
    const fetchPendingOrders = async () => {
      try {
        const data = await getPendingOrders(selectedExchange, selectedSymbol, mt)
        setPendingOrders(data.orders || [])
        setPendingLeverage(data.leverage ?? 1)
      } catch (err) {
        console.error('Failed to fetch pending orders:', err)
        setPendingOrders([])
      }
    }

    const fetchHistoryOrders = async () => {
      // 验证时间范围
      const validation = validateTimeRange()
      if (!validation.valid) {
        toast({
          title: t('common.error'),
          description: validation.error,
          status: 'error',
          duration: 3000,
        })
        return
      }
      
      try {
        const data = await getOrderHistory({
          exchange: selectedExchange,
          symbol: selectedSymbol,
          market_type: mt,
          limit: 500,
          start_time: toRFC3339(historyStartTime),
          end_time: toRFC3339(historyEndTime),
        })
        setHistoryOrders(data.orders || [])
        setHistoryLeverage(data.leverage ?? 1)
        if (data.total_count !== undefined) {
          setHistoryTotalCount(data.total_count)
        } else {
          setHistoryTotalCount((data.orders || []).length)
        }
        if (data.today_count !== undefined) {
          setHistoryTodayCount(data.today_count)
        }
      } catch (err) {
        console.error('Failed to fetch history orders:', err)
        setHistoryOrders([])
        setHistoryTotalCount(0)
        setHistoryTodayCount(0)
      }
    }

    const fetchData = async () => {
      setLoading(true)
      // 初始加载时同时获取待成交订单和历史订单，以便在标签上显示正确的数量
      await Promise.all([fetchPendingOrders(), fetchHistoryOrders()])
      setLoading(false)
    }

    fetchData()
    
    // 待成交订單每5秒刷新一次，历史订單每30秒刷新一次
    const interval = setInterval(() => {
      fetchPendingOrders()
      if (tabIndex === 1) {
        fetchHistoryOrders()
      }
    }, tabIndex === 0 ? 5000 : 30000)

    return () => clearInterval(interval)
  }, [tabIndex, selectedExchange, selectedSymbol, selectedMarketType, historyStartTime, historyEndTime])

  // 獲取當前交易對的方向（做多/做空）
  useEffect(() => {
    if (!selectedExchange || !selectedSymbol) {
      setSymbolDirection(null)
      return
    }
    const loadDirection = async () => {
      try {
        const res = await getSymbols()
        const sym = res.symbols?.find(
          s => s.exchange?.toLowerCase() === selectedExchange?.toLowerCase() && s.symbol === selectedSymbol && (s.market_type ?? 'futures') === (selectedMarketType ?? 'futures')
        )
        setSymbolDirection(sym?.direction === 'SHORT' ? 'SHORT' : 'LONG')
      } catch {
        setSymbolDirection('LONG')
      }
    }
    loadDirection()
  }, [selectedExchange, selectedSymbol, selectedMarketType])

  // 刷新待成交订單
  const refreshPendingOrders = async () => {
    try {
      const mt = selectedExchange && selectedSymbol ? (selectedMarketType ?? 'futures') : undefined
      const data = await getPendingOrders(selectedExchange, selectedSymbol, mt)
      setPendingOrders(data.orders || [])
      setPendingLeverage(data.leverage ?? 1)
    } catch (err) {
      console.error('Failed to refresh pending orders:', err)
    }
  }

  // 刷新历史订單
  const refreshHistoryOrders = async () => {
    // 验证时间范围
    const validation = validateTimeRange()
    if (!validation.valid) {
      toast({
        title: t('common.error'),
        description: validation.error,
        status: 'error',
        duration: 3000,
      })
      return
    }
    
    try {
      const mt = selectedExchange && selectedSymbol ? (selectedMarketType ?? 'futures') : undefined
      const data = await getOrderHistory({
        exchange: selectedExchange,
        symbol: selectedSymbol,
        market_type: mt,
        limit: 500,
        start_time: toRFC3339(historyStartTime),
        end_time: toRFC3339(historyEndTime),
      })
      setHistoryOrders(data.orders || [])
      setHistoryLeverage(data.leverage ?? 1)
      if (data.total_count !== undefined) {
        setHistoryTotalCount(data.total_count)
      } else {
        setHistoryTotalCount((data.orders || []).length)
      }
      if (data.today_count !== undefined) {
        setHistoryTodayCount(data.today_count)
      }
    } catch (err) {
      console.error('Failed to refresh history orders:', err)
    }
  }

  // 手动同步订单（仅币安）
  const handleSyncOrders = async () => {
    if (!selectedExchange || !selectedSymbol) {
      toast({
        title: t('orders.syncFailed'),
        description: t('orders.missingExchangeOrSymbol'),
        status: 'error',
        duration: 3000,
      })
      return
    }

    if (selectedExchange.toLowerCase() !== 'binance') {
      toast({
        title: t('orders.syncFailed'),
        description: t('orders.onlyBinanceSupported'),
        status: 'error',
        duration: 3000,
      })
      return
    }

    setSyncingOrders(true)
    try {
      const result = await syncOrders(selectedExchange, selectedSymbol)
      if (result.success) {
        toast({
          title: t('orders.syncSuccess'),
          description: result.message || t('orders.syncSuccessMessage'),
          status: 'success',
          duration: 3000,
        })
        // 刷新历史订单列表
        await refreshHistoryOrders()
      } else {
        toast({
          title: t('orders.syncFailed'),
          description: result.message || t('orders.syncFailedMessage'),
          status: 'error',
          duration: 5000,
        })
      }
    } catch (err) {
      toast({
        title: t('orders.syncFailed'),
        description: err instanceof Error ? err.message : String(err),
        status: 'error',
        duration: 5000,
      })
    } finally {
      setSyncingOrders(false)
    }
  }

  // 取消單個订單
  const handleCancelOrder = async (orderId: number) => {
    if (!selectedExchange || !selectedSymbol) {
      toast({
        title: t('orders.cancelFailed'),
        description: t('orders.missingExchangeOrSymbol'),
        status: 'error',
        duration: 3000,
      })
      return
    }

    setCancellingOrderId(orderId)
    try {
      const result = await cancelOrder(orderId, selectedExchange, selectedSymbol, selectedMarketType || 'futures')
      if (result.success) {
        toast({
          title: t('orders.cancelSuccess'),
          description: t('orders.orderCancelled', { orderId }),
          status: 'success',
          duration: 3000,
        })
        // 刷新订單列表
        await refreshPendingOrders()
      } else {
        toast({
          title: t('orders.cancelFailed'),
          description: result.message,
          status: 'error',
          duration: 5000,
        })
      }
    } catch (err) {
      toast({
        title: t('orders.cancelFailed'),
        description: err instanceof Error ? err.message : String(err),
        status: 'error',
        duration: 5000,
      })
    } finally {
      setCancellingOrderId(null)
    }
  }

  // 取消所有待成交订單
  const handleCancelAllOrders = async () => {
    if (!selectedExchange || !selectedSymbol) {
      toast({
        title: t('orders.cancelFailed'),
        description: t('orders.missingExchangeOrSymbol'),
        status: 'error',
        duration: 3000,
      })
      return
    }

    const orderIds = pendingOrders.map(o => o.order_id)
    if (orderIds.length === 0) {
      toast({
        title: t('orders.noOrdersToCancel'),
        status: 'info',
        duration: 3000,
      })
      return
    }

    setCancellingAll(true)
    try {
      const result = await batchCancelOrders(orderIds, selectedExchange, selectedSymbol, selectedMarketType || 'futures')
      if (result.success) {
        toast({
          title: t('orders.cancelAllSuccess'),
          description: t('orders.ordersCancelled', { count: result.count || orderIds.length }),
          status: 'success',
          duration: 3000,
        })
        // 刷新订單列表
        await refreshPendingOrders()
      } else {
        toast({
          title: t('orders.cancelFailed'),
          description: result.message,
          status: 'error',
          duration: 5000,
        })
      }
    } catch (err) {
      toast({
        title: t('orders.cancelFailed'),
        description: err instanceof Error ? err.message : String(err),
        status: 'error',
        duration: 5000,
      })
    } finally {
      setCancellingAll(false)
    }
  }

  const formatTime = (timeStr: string) => {
    return formatDateTime(timeStr, timezone, i18n.language)
  }

  const getStatusColorScheme = (status: string) => {
    switch (status) {
      case 'PLACED':
        return 'blue'
      case 'CONFIRMED':
        return 'green'
      case 'PARTIALLY_FILLED':
        return 'orange'
      case 'FILLED':
        return 'green'
      case 'CANCELED':
        return 'gray'
      default:
        return 'gray'
    }
  }

  const getStatusText = (status: string) => {
    switch (status) {
      case 'PLACED':
        return t('orders.placed')
      case 'CONFIRMED':
        return t('orders.confirmed')
      case 'PARTIALLY_FILLED':
        return t('orders.partiallyFilled')
      case 'FILLED':
        return t('orders.filled')
      case 'CANCELED':
        return t('orders.canceled')
      default:
        return status
    }
  }

  const getOrderTypeText = (type?: string) => {
    if (!type) return '-'
    switch (type.toUpperCase()) {
      case 'LIMIT':
        return t('orders.limitOrder')
      case 'MARKET':
        return t('orders.marketOrder')
      case 'STOP_LOSS':
        return t('orders.stopLoss')
      case 'STOP_LOSS_LIMIT':
        return t('orders.stopLossLimit')
      case 'TAKE_PROFIT':
        return t('orders.takeProfit')
      case 'TAKE_PROFIT_LIMIT':
        return t('orders.takeProfitLimit')
      default:
        return type
    }
  }

  const getOrderSourceText = (source?: string) => {
    if (!source || source === '' || source === 'normal') return t('orders.sourceNormal')
    switch (source) {
      case 'stop_loss':
        return t('orders.sourceStopLoss')
      case 'liquidation':
        return t('orders.sourceLiquidation')
      default:
        return source
    }
  }

  const getOrderSourceColorScheme = (source?: string) => {
    if (!source || source === '' || source === 'normal') return 'green'
    switch (source) {
      case 'stop_loss':
        return 'red'
      case 'liquidation':
        return 'orange'
      default:
        return 'gray'
    }
  }

  // 获取所有唯一的策略名称
  const getUniqueStrategies = (orders: (PendingOrderInfo | OrderInfo)[]) => {
    const strategies = new Set<string>()
    orders.forEach(order => {
      const strategyName = 'strategy_name' in order ? order.strategy_name : (order as OrderInfo).strategy_name
      if (strategyName) {
        strategies.add(strategyName)
      }
    })
    return Array.from(strategies).sort()
  }

  // 筛选待成交订单
  const filteredPendingOrders = pendingOrders.filter(order => {
    if (pendingFilterStrategy !== 'all' && order.strategy_name !== pendingFilterStrategy) return false
    if (pendingFilterType !== 'all') {
      const orderType = (order as any).type || 'LIMIT' // 默认限价单
      if (orderType !== pendingFilterType) return false
    }
    if (pendingFilterStatus !== 'all' && order.status !== pendingFilterStatus) return false
    if (pendingFilterSide !== 'all' && order.side !== pendingFilterSide) return false
    return true
  })

  // 筛选历史订单
  const filteredHistoryOrders = historyOrders.filter(order => {
    if (historyFilterStrategy !== 'all' && order.strategy_name !== historyFilterStrategy) return false
    if (historyFilterType !== 'all') {
      const orderType = order.type || 'LIMIT' // 默认限价单
      if (orderType !== historyFilterType) return false
    }
    if (historyFilterOrderSource !== 'all') {
      const src = order.order_source || 'normal'
      if (src !== historyFilterOrderSource) return false
    }
    if (historyFilterStatus !== 'all' && order.status !== historyFilterStatus) return false
    if (historyFilterSide !== 'all' && order.side !== historyFilterSide) return false
    return true
  })

  // 计算订單统计
  // 今日订单数和总订单数使用后端返回的真实数据
  const todayOrderCount = historyTodayCount
  const totalOrderCount = historyTotalCount

  const successOrders = filteredHistoryOrders.filter(order => order.status === 'FILLED').length
  const successRate = filteredHistoryOrders.length > 0 ? (successOrders / filteredHistoryOrders.length) * 100 : 0

  if (loading && pendingOrders.length === 0 && historyOrders.length === 0) {
    return (
      <Center h="200px">
        <Spinner size="xl" />
      </Center>
    )
  }

  return (
    <Box>
      <Heading size="lg" mb={4}>{t('orders.title')}</Heading>
      <Flex align="center" gap={2} mb={4} flexWrap="wrap">
        <Text fontSize="md" color="gray.600">
          {t('orders.currentPair', { exchange: selectedExchange, symbol: selectedSymbol })}
        </Text>
        {symbolDirection != null && (
          <Badge colorScheme={symbolDirection === 'SHORT' ? 'orange' : 'green'} fontSize="sm">
            {symbolDirection === 'SHORT' ? t('configuration.directionShort') : t('configuration.directionLong')}
          </Badge>
        )}
      </Flex>

      <Tabs index={tabIndex} onChange={setTabIndex} colorScheme="blue">
        <TabList>
          <Tab>{t('orders.pendingTab')} ({pendingOrders.length})</Tab>
          <Tab>{t('orders.historyTab')} ({historyTotalCount || historyOrders.length})</Tab>
        </TabList>

        <TabPanels>
          {/* 待成交订單 */}
          <TabPanel>
            {pendingOrders.length === 0 ? (
              <Text color="gray.500" textAlign="center" py={8}>{t('orders.noPendingOrders')}</Text>
            ) : (
              <>
                {/* 筛选器 */}
                <Flex mb={4} gap={2} wrap="wrap" align="center">
                  <Select
                    size="sm"
                    width="150px"
                    value={pendingFilterStrategy}
                    onChange={(e) => setPendingFilterStrategy(e.target.value)}
                  >
                    <option value="all">{t('orders.allStrategies')}</option>
                    {getUniqueStrategies(pendingOrders).map(strategy => (
                      <option key={strategy} value={strategy}>{strategy}</option>
                    ))}
                  </Select>
                  <Select
                    size="sm"
                    width="120px"
                    value={pendingFilterType}
                    onChange={(e) => setPendingFilterType(e.target.value)}
                  >
                    <option value="all">{t('orders.allTypes')}</option>
                    <option value="LIMIT">{t('orders.limitOrder')}</option>
                    <option value="MARKET">{t('orders.marketOrder')}</option>
                  </Select>
                  <Select
                    size="sm"
                    width="120px"
                    value={pendingFilterStatus}
                    onChange={(e) => setPendingFilterStatus(e.target.value)}
                  >
                    <option value="all">{t('orders.allStatuses')}</option>
                    <option value="PLACED">{t('orders.placed')}</option>
                    <option value="CONFIRMED">{t('orders.confirmed')}</option>
                    <option value="PARTIALLY_FILLED">{t('orders.partiallyFilled')}</option>
                  </Select>
                  <Box flex="1" />
                  <Button
                    colorScheme="red"
                    size="sm"
                    onClick={handleCancelAllOrders}
                    isLoading={cancellingAll}
                    loadingText={t('orders.cancelling')}
                    isDisabled={filteredPendingOrders.length === 0}
                  >
                    {t('orders.cancelAll')} ({filteredPendingOrders.length})
                  </Button>
                </Flex>

                {/* 分列显示买单和卖单委托 */}
                <SimpleGrid columns={{ base: 1, lg: 2 }} spacing={4}>
                  {/* 买单委托 */}
                  <Card>
                    <CardBody>
                      <Heading size="sm" mb={3} color="green.600">
                        {t('orders.buy')} ({filteredPendingOrders.filter(o => o.side === 'BUY').length})
                      </Heading>
                      {filteredPendingOrders.filter(o => o.side === 'BUY').length === 0 ? (
                        <Text color="gray.500" textAlign="center" py={4}>
                          {t('orders.noBuyOrders')}
                        </Text>
                      ) : (
                        <TableContainer>
                          <Table size="sm" variant="simple">
                            <Thead>
                              <Tr>
                                <Th>{t('orders.price')}</Th>
                                <Th isNumeric>{t('orders.quantity')}</Th>
                                <Th isNumeric>{t('orders.totalPrice')}</Th>
                                <Th isNumeric>
                                  <Tooltip label={t('orders.capitalUsageTooltip')} hasArrow placement="top">
                                    <span>{t('orders.capitalUsage')}</span>
                                  </Tooltip>
                                </Th>
                                <Th isNumeric>{t('orders.filled')}</Th>
                                <Th>{t('orders.status')}</Th>
                                <Th>{t('common.actions')}</Th>
                              </Tr>
                            </Thead>
                            <Tbody>
                              {filteredPendingOrders
                                .filter(o => o.side === 'BUY')
                                .sort((a, b) => b.price - a.price) // 按价格降序排列
                                .map((order) => {
                                  const totalPrice = computeOrderTotalPrice(order.price, order.quantity)
                                  const capitalUsage = computeOrderCapitalUsage(totalPrice, pendingLeverage)
                                  return (
                                  <Tr key={order.order_id}>
                                    <Td>
                                      <Text fontWeight="bold" color="green.600">
                                        {order.price != null ? order.price.toFixed(2) : '-'}
                                      </Text>
                                    </Td>
                                    <Td isNumeric>{order.quantity != null ? order.quantity.toFixed(4) : '-'}</Td>
                                    <Td isNumeric>{totalPrice > 0 ? totalPrice.toFixed(2) : '-'}</Td>
                                    <Td isNumeric>{capitalUsage > 0 ? capitalUsage.toFixed(2) : '-'}</Td>
                                    <Td isNumeric>{order.filled_quantity != null ? order.filled_quantity.toFixed(4) : '-'}</Td>
                                    <Td>
                                      <Badge colorScheme={getStatusColorScheme(order.status)} fontSize="xs">
                                        {getStatusText(order.status)}
                                      </Badge>
                                    </Td>
                                    <Td>
                                      <Tooltip label={t('orders.cancelOrder')} hasArrow>
                                        <IconButton
                                          aria-label={t('orders.cancelOrder')}
                                          icon={<CloseIcon />}
                                          size="xs"
                                          colorScheme="red"
                                          variant="ghost"
                                          isLoading={cancellingOrderId === order.order_id}
                                          onClick={() => handleCancelOrder(order.order_id)}
                                        />
                                      </Tooltip>
                                    </Td>
                                  </Tr>
                                )})}
                            </Tbody>
                          </Table>
                        </TableContainer>
                      )}
                    </CardBody>
                  </Card>

                  {/* 卖单委托 */}
                  <Card>
                    <CardBody>
                      <Heading size="sm" mb={3} color="red.600">
                        {t('orders.sell')} ({filteredPendingOrders.filter(o => o.side === 'SELL').length})
                      </Heading>
                      {filteredPendingOrders.filter(o => o.side === 'SELL').length === 0 ? (
                        <Text color="gray.500" textAlign="center" py={4}>
                          {t('orders.noSellOrders')}
                        </Text>
                      ) : (
                        <TableContainer>
                          <Table size="sm" variant="simple">
                            <Thead>
                              <Tr>
                                <Th>{t('orders.price')}</Th>
                                <Th isNumeric>{t('orders.quantity')}</Th>
                                <Th isNumeric>{t('orders.totalPrice')}</Th>
                                <Th isNumeric>
                                  <Tooltip label={t('orders.capitalUsageTooltip')} hasArrow placement="top">
                                    <span>{t('orders.capitalUsage')}</span>
                                  </Tooltip>
                                </Th>
                                <Th isNumeric>{t('orders.filled')}</Th>
                                <Th>{t('orders.status')}</Th>
                                <Th>{t('common.actions')}</Th>
                              </Tr>
                            </Thead>
                            <Tbody>
                              {filteredPendingOrders
                                .filter(o => o.side === 'SELL')
                                .sort((a, b) => a.price - b.price) // 按价格升序排列
                                .map((order) => {
                                  const totalPrice = computeOrderTotalPrice(order.price, order.quantity)
                                  const capitalUsage = computeOrderCapitalUsage(totalPrice, pendingLeverage)
                                  return (
                                  <Tr key={order.order_id}>
                                    <Td>
                                      <Text fontWeight="bold" color="red.600">
                                        {order.price != null ? order.price.toFixed(2) : '-'}
                                      </Text>
                                    </Td>
                                    <Td isNumeric>{order.quantity != null ? order.quantity.toFixed(4) : '-'}</Td>
                                    <Td isNumeric>{totalPrice > 0 ? totalPrice.toFixed(2) : '-'}</Td>
                                    <Td isNumeric>{capitalUsage > 0 ? capitalUsage.toFixed(2) : '-'}</Td>
                                    <Td isNumeric>{order.filled_quantity != null ? order.filled_quantity.toFixed(4) : '-'}</Td>
                                    <Td>
                                      <Badge colorScheme={getStatusColorScheme(order.status)} fontSize="xs">
                                        {getStatusText(order.status)}
                                      </Badge>
                                    </Td>
                                    <Td>
                                      <Tooltip label={t('orders.cancelOrder')} hasArrow>
                                        <IconButton
                                          aria-label={t('orders.cancelOrder')}
                                          icon={<CloseIcon />}
                                          size="xs"
                                          colorScheme="red"
                                          variant="ghost"
                                          isLoading={cancellingOrderId === order.order_id}
                                          onClick={() => handleCancelOrder(order.order_id)}
                                        />
                                      </Tooltip>
                                    </Td>
                                  </Tr>
                                )})}
                            </Tbody>
                          </Table>
                        </TableContainer>
                      )}
                    </CardBody>
                  </Card>
                </SimpleGrid>
              </>
            )}
          </TabPanel>

          {/* 历史订單 */}
          <TabPanel>
            {/* 订單统计卡片 */}
            <SimpleGrid columns={{ base: 1, md: 2, lg: 4 }} spacing={4} mb={6}>
              <Card>
                <CardBody>
                  <Stat>
                    <StatLabel>{t('orders.todayOrders')}</StatLabel>
                    <StatNumber>{todayOrderCount}</StatNumber>
                  </Stat>
                </CardBody>
              </Card>

              <Card>
                <CardBody>
                  <Stat>
                    <StatLabel>{t('orders.totalOrders')}</StatLabel>
                    <StatNumber>{totalOrderCount}</StatNumber>
                  </Stat>
                </CardBody>
              </Card>

              <Card>
                <CardBody>
                  <Stat>
                    <StatLabel>{t('orders.successRate')}</StatLabel>
                    <StatNumber>{successRate.toFixed(2)}%</StatNumber>
                  </Stat>
                </CardBody>
              </Card>

              <Card>
                <CardBody>
                  <Stat>
                    <StatLabel>{t('orders.completedOrders')}</StatLabel>
                    <StatNumber color="green.500">{successOrders}</StatNumber>
                  </Stat>
                </CardBody>
              </Card>
            </SimpleGrid>

            {/* 筛选器和同步按钮 */}
            <VStack spacing={4} align="stretch" mb={4}>
              {/* 时间范围选择器 */}
              <Box>
                <Text fontSize="sm" fontWeight="medium" mb={2}>{t('orders.timeRange')}</Text>
                <Text fontSize="xs" color="gray.500" mb={2}>{t('orders.timeRangeFilterHint')}</Text>
                <Flex gap={2} wrap="wrap" align="center">
                  <FormControl isInvalid={!validateTimeRange().valid && historyStartTime && historyEndTime} maxW="200px">
                    <FormLabel fontSize="xs">{t('orders.startTime')}</FormLabel>
                    <Input
                      type="datetime-local"
                      size="sm"
                      value={historyStartTime}
                      onChange={(e) => setHistoryStartTime(e.target.value)}
                    />
                    {!validateTimeRange().valid && historyStartTime && historyEndTime && (
                      <FormErrorMessage fontSize="xs">{validateTimeRange().error}</FormErrorMessage>
                    )}
                  </FormControl>
                  <FormControl isInvalid={!validateTimeRange().valid && historyStartTime && historyEndTime} maxW="200px">
                    <FormLabel fontSize="xs">{t('orders.endTime')}</FormLabel>
                    <Input
                      type="datetime-local"
                      size="sm"
                      value={historyEndTime}
                      onChange={(e) => setHistoryEndTime(e.target.value)}
                    />
                  </FormControl>
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => {
                      const defaultRange = getDefaultTimeRange()
                      setHistoryStartTime(defaultRange.startTime)
                      setHistoryEndTime(defaultRange.endTime)
                    }}
                  >
                    {t('orders.defaultTimeRange')}
                  </Button>
                  <Box flex="1" />
                  <Button
                    size="sm"
                    colorScheme="blue"
                    onClick={refreshHistoryOrders}
                  >
                    {t('common.refresh')}
                  </Button>
                </Flex>
              </Box>
              
              {/* 其他筛选器 */}
              <Flex gap={2} wrap="wrap" align="center">
                {historyOrders.length > 0 && (
                  <>
                    <Select
                      size="sm"
                      width="150px"
                      value={historyFilterStrategy}
                      onChange={(e) => setHistoryFilterStrategy(e.target.value)}
                    >
                      <option value="all">{t('orders.allStrategies')}</option>
                      {getUniqueStrategies(historyOrders).map(strategy => (
                        <option key={strategy} value={strategy}>{strategy}</option>
                      ))}
                    </Select>
                    <Select
                      size="sm"
                      width="120px"
                      value={historyFilterType}
                      onChange={(e) => setHistoryFilterType(e.target.value)}
                    >
                      <option value="all">{t('orders.allTypes')}</option>
                      <option value="LIMIT">{t('orders.limitOrder')}</option>
                      <option value="MARKET">{t('orders.marketOrder')}</option>
                    </Select>
                    <Select
                      size="sm"
                      width="130px"
                      value={historyFilterOrderSource}
                      onChange={(e) => setHistoryFilterOrderSource(e.target.value)}
                    >
                      <option value="all">{t('orders.allSources')}</option>
                      <option value="normal">{t('orders.sourceNormal')}</option>
                      <option value="stop_loss">{t('orders.sourceStopLoss')}</option>
                      <option value="liquidation">{t('orders.sourceLiquidation')}</option>
                    </Select>
                    <Select
                      size="sm"
                      width="120px"
                      value={historyFilterStatus}
                      onChange={(e) => setHistoryFilterStatus(e.target.value)}
                    >
                      <option value="all">{t('orders.allStatuses')}</option>
                      <option value="FILLED">{t('orders.filled')}</option>
                      <option value="CANCELED">{t('orders.canceled')}</option>
                      <option value="PARTIALLY_FILLED">{t('orders.partiallyFilled')}</option>
                    </Select>
                    <Select
                      size="sm"
                      width="100px"
                      value={historyFilterSide}
                      onChange={(e) => setHistoryFilterSide(e.target.value)}
                    >
                      <option value="all">{t('orders.allSides')}</option>
                      <option value="BUY">{t('orders.buy')}</option>
                      <option value="SELL">{t('orders.sell')}</option>
                    </Select>
                  </>
                )}
                <Box flex="1" />
                {/* 仅币安显示同步按钮 */}
                {selectedExchange && selectedExchange.toLowerCase() === 'binance' && (
                  <Button
                    colorScheme="blue"
                    size="sm"
                    onClick={handleSyncOrders}
                    isLoading={syncingOrders}
                    loadingText={t('orders.syncing')}
                  >
                    {t('orders.syncOrders')}
                  </Button>
                )}
              </Flex>
            </VStack>

            {historyOrders.length === 0 ? (
              <Text color="gray.500" textAlign="center" py={8}>{t('orders.noHistoryOrders')}</Text>
            ) : filteredHistoryOrders.length === 0 ? (
              <Text color="gray.500" textAlign="center" py={8}>{t('orders.noMatchingOrders')}</Text>
            ) : (
              <TableContainer>
                <Table variant="simple">
                  <Thead>
                    <Tr>
                      <Th>{t('orders.orderId')}</Th>
                      <Th>{t('orders.strategy')}</Th>
                      <Th>{t('orders.orderType')}</Th>
                      <Th>{t('orders.orderSource')}</Th>
                      <Th>{t('orders.symbol')}</Th>
                      <Th>{t('orders.side')}</Th>
                      <Th isNumeric>{t('orders.price')}</Th>
                      <Th isNumeric>{t('orders.quantity')}</Th>
                      <Th isNumeric>{t('orders.totalPrice')}</Th>
                      <Th isNumeric>
                        <Tooltip label={t('orders.capitalUsageTooltip')} hasArrow placement="top">
                          <span>{t('orders.capitalUsage')}</span>
                        </Tooltip>
                      </Th>
                      <Th>{t('orders.status')}</Th>
                      <Th isNumeric>{t('orders.gridPnl')}</Th>
                      <Th isNumeric>{t('orders.exchangePnl')}</Th>
                      <Th>{t('orders.createdAt')}</Th>
                      <Th>{t('orders.updatedAt')}</Th>
                    </Tr>
                  </Thead>
                  <Tbody>
                    {filteredHistoryOrders.map((order) => (
                        <Tr key={order.order_id}>
                          <Td>{order.order_id}</Td>
                          <Td>
                            <Badge colorScheme={order.strategy_type === 'grid' ? 'blue' : order.strategy_type === 'dca' ? 'purple' : 'gray'} variant="subtle">
                              {order.strategy_name || order.strategy_type || '-'}
                            </Badge>
                          </Td>
                          <Td>
                            <Badge colorScheme="cyan" variant="outline">
                              {getOrderTypeText(order.type || 'LIMIT')}
                            </Badge>
                          </Td>
                          <Td>
                            <Badge colorScheme={getOrderSourceColorScheme(order.order_source)} variant="subtle">
                              {getOrderSourceText(order.order_source)}
                            </Badge>
                          </Td>
                          <Td>
                            <Badge colorScheme="purple" variant="subtle">
                              {order.symbol}
                            </Badge>
                          </Td>
                          <Td>
                            <Badge colorScheme={order.side === 'BUY' ? 'green' : 'red'}>
                              {order.side === 'BUY' ? t('orders.buy') : t('orders.sell')}
                            </Badge>
                          </Td>
                          <Td isNumeric>{order.price != null ? order.price.toFixed(2) : '-'}</Td>
                          <Td isNumeric>{order.quantity != null ? order.quantity.toFixed(4) : '-'}</Td>
                          <Td isNumeric>
                            {(() => {
                              const totalPrice = computeOrderTotalPrice(order.price, order.quantity)
                              return totalPrice > 0 ? totalPrice.toFixed(2) : '-'
                            })()}
                          </Td>
                          <Td isNumeric>
                            {(() => {
                              const totalPrice = computeOrderTotalPrice(order.price, order.quantity)
                              const capitalUsage = computeOrderCapitalUsage(totalPrice, historyLeverage)
                              return capitalUsage > 0 ? capitalUsage.toFixed(2) : '-'
                            })()}
                          </Td>
                          <Td>
                            <Badge colorScheme={getStatusColorScheme(order.status)}>
                              {getStatusText(order.status)}
                            </Badge>
                          </Td>
                          <Td isNumeric>
                            {order.pnl != null && order.pnl !== undefined ? (
                              <Tooltip label={t('orders.clickToViewDetail')} placement="top" hasArrow>
                                <Text
                                  color={order.pnl >= 0 ? 'green.500' : 'red.500'}
                                  fontWeight="medium"
                                  cursor="pointer"
                                  _hover={{ textDecoration: 'underline', opacity: 0.8 }}
                                  onClick={() => handleShowTradeDetail(order.order_id)}
                                >
                                  {order.pnl >= 0 ? '+' : ''}{order.pnl.toFixed(4)}
                                </Text>
                              </Tooltip>
                            ) : (
                              '-'
                            )}
                          </Td>
                          <Td isNumeric>
                            {order.exchange_pnl != null && order.exchange_pnl !== undefined ? (
                              <Tooltip label={t('orders.exchangePnlTooltip')} placement="top" hasArrow>
                                <Text
                                  color={order.exchange_pnl >= 0 ? 'green.500' : 'red.500'}
                                  fontWeight="medium"
                                  fontSize="sm"
                                >
                                  {order.exchange_pnl >= 0 ? '+' : ''}{order.exchange_pnl.toFixed(4)}
                                </Text>
                              </Tooltip>
                            ) : (
                              <Text color="gray.400" fontSize="sm">-</Text>
                            )}
                          </Td>
                          <Td>{formatTime(order.created_at)}</Td>
                          <Td>{formatTime(order.updated_at)}</Td>
                        </Tr>
                      ))}
                  </Tbody>
                </Table>
              </TableContainer>
            )}
          </TabPanel>
        </TabPanels>
      </Tabs>

      {/* 🔥 成交明细 Modal */}
      <Modal isOpen={isDetailOpen} onClose={onDetailClose} size="xl" scrollBehavior="inside">
        <ModalOverlay />
        <ModalContent>
          <ModalHeader>{t('orders.tradeDetail')}</ModalHeader>
          <ModalCloseButton />
          <ModalBody pb={6}>
            {detailLoading ? (
              <Center py={8}><Spinner size="lg" /></Center>
            ) : tradeDetail ? (
              <VStack spacing={4} align="stretch">
                {/* 委托信息 */}
                {tradeDetail.order && (
                  <Box bg="gray.50" p={4} borderRadius="lg">
                    <Text fontWeight="bold" mb={2}>{t('orders.orderInfo')}</Text>
                    <SimpleGrid columns={2} spacing={2} fontSize="sm">
                      <Text color="gray.500">{t('orders.orderId')}</Text>
                      <Text fontFamily="mono">{tradeDetail.order.order_id}</Text>
                      <Text color="gray.500">{t('orders.side')}</Text>
                      <Badge colorScheme={tradeDetail.order.side === 'BUY' ? 'green' : 'red'} w="fit-content">
                        {tradeDetail.order.side === 'BUY' ? t('orders.buy') : t('orders.sell')}
                      </Badge>
                      <Text color="gray.500">{t('orders.price')}</Text>
                      <Text fontWeight="600">{tradeDetail.order.price?.toFixed(2)}</Text>
                      <Text color="gray.500">{t('orders.orderQty')}</Text>
                      <Text>{tradeDetail.order.quantity > 0 ? tradeDetail.order.quantity.toFixed(6) : '-'}</Text>
                      <Text color="gray.500">{t('orders.filledQty')}</Text>
                      <Text>{tradeDetail.order.filled_qty > 0 ? tradeDetail.order.filled_qty.toFixed(6) : '-'}</Text>
                      <Text color="gray.500">{t('orders.createdAt')}</Text>
                      <Text>{formatTime(tradeDetail.order.created_at)}</Text>
                    </SimpleGrid>
                  </Box>
                )}

                <Divider />

                {/* 成交明细 */}
                <Box>
                  <Text fontWeight="bold" mb={2}>
                    {t('orders.fillRecords')} ({tradeDetail.fill_count} {t('orders.fills')})
                  </Text>
                  {tradeDetail.fills.length === 0 ? (
                    <Text color="gray.400" textAlign="center" py={4}>{t('orders.noFills')}</Text>
                  ) : (
                    <TableContainer>
                      <Table size="sm" variant="simple">
                        <Thead>
                          <Tr>
                            <Th>{t('orders.buyPrice')}</Th>
                            <Th>{t('orders.sellPrice')}</Th>
                            <Th isNumeric>{t('orders.quantity')}</Th>
                            <Th isNumeric>{t('orders.pnl')}</Th>
                            <Th isNumeric>{t('orders.fee')}</Th>
                            <Th>{t('orders.time')}</Th>
                          </Tr>
                        </Thead>
                        <Tbody>
                          {tradeDetail.fills.map((fill) => (
                            <Tr key={fill.id}>
                              <Td>{fill.buy_price.toFixed(2)}</Td>
                              <Td>{fill.sell_price.toFixed(2)}</Td>
                              <Td isNumeric>{fill.quantity.toFixed(6)}</Td>
                              <Td isNumeric>
                                <Text color={fill.pnl >= 0 ? 'green.500' : 'red.500'} fontWeight="600">
                                  {fill.pnl >= 0 ? '+' : ''}{fill.pnl.toFixed(4)}
                                </Text>
                              </Td>
                              <Td isNumeric>{fill.fee > 0 ? fill.fee.toFixed(6) : '-'}</Td>
                              <Td fontSize="xs">{formatTime(fill.created_at)}</Td>
                            </Tr>
                          ))}
                        </Tbody>
                      </Table>
                    </TableContainer>
                  )}
                </Box>

                <Divider />

                {/* 汇总 */}
                <Box bg="blue.50" p={4} borderRadius="lg">
                  <Text fontWeight="bold" mb={2}>{t('orders.summary')}</Text>
                  <SimpleGrid columns={2} spacing={2} fontSize="sm">
                    <Text color="gray.600">{t('orders.totalFilledQty')}</Text>
                    <Text fontWeight="600">{tradeDetail.summary.total_quantity.toFixed(6)}</Text>
                    <Text color="gray.600">{t('orders.grossPnl')}</Text>
                    <Text fontWeight="700" color={tradeDetail.summary.total_pnl >= 0 ? 'green.600' : 'red.500'}>
                      {tradeDetail.summary.total_pnl >= 0 ? '+' : ''}{tradeDetail.summary.total_pnl.toFixed(4)} USDT
                    </Text>
                    <Text color="gray.600">{t('orders.totalFee')}</Text>
                    <Text color="orange.500">-{tradeDetail.summary.total_fee.toFixed(6)} USDT</Text>
                    <Text color="gray.600">{t('orders.netPnl')}</Text>
                    <Text fontWeight="800" fontSize="md" color={tradeDetail.summary.net_pnl >= 0 ? 'green.600' : 'red.500'}>
                      {tradeDetail.summary.net_pnl >= 0 ? '+' : ''}{tradeDetail.summary.net_pnl.toFixed(4)} USDT
                    </Text>
                  </SimpleGrid>
                </Box>
              </VStack>
            ) : (
              <Text color="gray.400" textAlign="center" py={8}>{t('orders.noData')}</Text>
            )}
          </ModalBody>
        </ModalContent>
      </Modal>
    </Box>
  )
}

export default Orders
