import React, { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useSymbol } from '../contexts/SymbolContext'
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
} from '@chakra-ui/react'
import { CloseIcon } from '@chakra-ui/icons'
import { getPendingOrders, getOrderHistory, cancelOrder, batchCancelOrders, syncOrders, getSymbols, PendingOrderInfo } from '../services/api'

interface OrderInfo {
  order_id: number
  client_order_id: string
  symbol: string
  side: string
  type?: string
  price: number
  quantity: number
  status: string
  created_at: string
  updated_at: string
  pnl?: number | null
  strategy_name?: string
  strategy_type?: string
}

const Orders: React.FC = () => {
  const { t } = useTranslation()
  const { selectedExchange, selectedSymbol } = useSymbol()
  const [pendingOrders, setPendingOrders] = useState<PendingOrderInfo[]>([])
  const [historyOrders, setHistoryOrders] = useState<OrderInfo[]>([])
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
  const [historyFilterStatus, setHistoryFilterStatus] = useState<string>('all')
  const [historyFilterSide, setHistoryFilterSide] = useState<string>('all')
  const [symbolDirection, setSymbolDirection] = useState<'LONG' | 'SHORT' | null>(null)

  useEffect(() => {
    const fetchPendingOrders = async () => {
      try {
        const data = await getPendingOrders(selectedExchange, selectedSymbol)
        setPendingOrders(data.orders || [])
      } catch (err) {
        console.error('Failed to fetch pending orders:', err)
        setPendingOrders([])
      }
    }

    const fetchHistoryOrders = async () => {
      try {
        const data = await getOrderHistory({
          exchange: selectedExchange,
          symbol: selectedSymbol,
        })
        setHistoryOrders(data.orders || [])
      } catch (err) {
        console.error('Failed to fetch history orders:', err)
        setHistoryOrders([])
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
  }, [tabIndex, selectedExchange, selectedSymbol])

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
          s => s.exchange?.toLowerCase() === selectedExchange?.toLowerCase() && s.symbol === selectedSymbol
        )
        setSymbolDirection(sym?.direction === 'SHORT' ? 'SHORT' : 'LONG')
      } catch {
        setSymbolDirection('LONG')
      }
    }
    loadDirection()
  }, [selectedExchange, selectedSymbol])

  // 刷新待成交订單
  const refreshPendingOrders = async () => {
    try {
      const data = await getPendingOrders(selectedExchange, selectedSymbol)
      setPendingOrders(data.orders || [])
    } catch (err) {
      console.error('Failed to refresh pending orders:', err)
    }
  }

  // 刷新历史订單
  const refreshHistoryOrders = async () => {
    try {
      const data = await getOrderHistory({
        exchange: selectedExchange,
        symbol: selectedSymbol,
      })
      setHistoryOrders(data.orders || [])
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
      const result = await cancelOrder(orderId, selectedExchange, selectedSymbol)
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
      const result = await batchCancelOrders(orderIds, selectedExchange, selectedSymbol)
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
    try {
      return new Date(timeStr).toLocaleString('zh-CN')
    } catch {
      return timeStr
    }
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
    if (historyFilterStatus !== 'all' && order.status !== historyFilterStatus) return false
    if (historyFilterSide !== 'all' && order.side !== historyFilterSide) return false
    return true
  })

  // 计算订單统计（基于筛选后的订单）
  const todayOrders = filteredHistoryOrders.filter(order => {
    const orderDate = new Date(order.created_at)
    const today = new Date()
    return orderDate.toDateString() === today.toDateString()
  })

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
          <Tab>{t('orders.historyTab')} ({historyOrders.length})</Tab>
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
                  <Select
                    size="sm"
                    width="100px"
                    value={pendingFilterSide}
                    onChange={(e) => setPendingFilterSide(e.target.value)}
                  >
                    <option value="all">{t('orders.allSides')}</option>
                    <option value="BUY">{t('orders.buy')}</option>
                    <option value="SELL">{t('orders.sell')}</option>
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
                <TableContainer>
                <Table variant="simple">
                  <Thead>
                    <Tr>
                      <Th>{t('orders.orderId')}</Th>
                      <Th>{t('orders.strategy')}</Th>
                      <Th>{t('orders.orderType')}</Th>
                      <Th>{t('orders.symbol')}</Th>
                      <Th>{t('orders.side')}</Th>
                      <Th isNumeric>{t('orders.price')}</Th>
                      <Th isNumeric>{t('orders.quantity')}</Th>
                      <Th isNumeric>{t('orders.filled')}</Th>
                      <Th>{t('orders.status')}</Th>
                      <Th isNumeric>{t('orders.slotPrice')}</Th>
                      <Th>{t('orders.createdAt')}</Th>
                      <Th>{t('common.actions')}</Th>
                    </Tr>
                  </Thead>
                  <Tbody>
                    {filteredPendingOrders.length === 0 ? (
                      <Tr>
                        <Td colSpan={11} textAlign="center" color="gray.500" py={8}>
                          {t('orders.noMatchingOrders')}
                        </Td>
                      </Tr>
                    ) : (
                      filteredPendingOrders.map((order) => (
                        <Tr key={order.order_id}>
                          <Td>{order.order_id}</Td>
                          <Td>
                            <Badge colorScheme={order.strategy_type === 'grid' ? 'blue' : order.strategy_type === 'dca' ? 'purple' : 'gray'} variant="subtle">
                              {order.strategy_name || order.strategy_type || '-'}
                            </Badge>
                          </Td>
                          <Td>
                            <Badge colorScheme="cyan" variant="outline">
                              {getOrderTypeText((order as any).type || 'LIMIT')}
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
                          <Td isNumeric>{order.filled_quantity != null ? order.filled_quantity.toFixed(4) : '-'}</Td>
                          <Td>
                            <Badge colorScheme={getStatusColorScheme(order.status)}>
                              {getStatusText(order.status)}
                            </Badge>
                          </Td>
                          <Td isNumeric>{order.slot_price != null ? order.slot_price.toFixed(2) : '-'}</Td>
                          <Td>{formatTime(order.created_at)}</Td>
                          <Td>
                            <Tooltip label={t('orders.cancelOrder')} hasArrow>
                              <IconButton
                                aria-label={t('orders.cancelOrder')}
                                icon={<CloseIcon />}
                                size="sm"
                                colorScheme="red"
                                variant="ghost"
                                isLoading={cancellingOrderId === order.order_id}
                                onClick={() => handleCancelOrder(order.order_id)}
                              />
                            </Tooltip>
                          </Td>
                        </Tr>
                      ))
                    )}
                  </Tbody>
                </Table>
              </TableContainer>
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
                    <StatNumber>{todayOrders.length}</StatNumber>
                  </Stat>
                </CardBody>
              </Card>

              <Card>
                <CardBody>
                  <Stat>
                    <StatLabel>{t('orders.totalOrders')}</StatLabel>
                    <StatNumber>{filteredHistoryOrders.length}</StatNumber>
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
            <Flex mb={4} gap={2} wrap="wrap" align="center">
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
                      <Th>{t('orders.symbol')}</Th>
                      <Th>{t('orders.side')}</Th>
                      <Th isNumeric>{t('orders.price')}</Th>
                      <Th isNumeric>{t('orders.quantity')}</Th>
                      <Th>{t('orders.status')}</Th>
                      <Th isNumeric>{t('orders.pnl')}</Th>
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
                          <Td>
                            <Badge colorScheme={getStatusColorScheme(order.status)}>
                              {getStatusText(order.status)}
                            </Badge>
                          </Td>
                          <Td isNumeric>
                            {order.pnl != null && order.pnl !== undefined ? (
                              <Text color={order.pnl >= 0 ? 'green.500' : 'red.500'} fontWeight="medium">
                                {order.pnl >= 0 ? '+' : ''}{order.pnl.toFixed(4)}
                              </Text>
                            ) : (
                              '-'
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
    </Box>
  )
}

export default Orders
