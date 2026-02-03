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
} from '@chakra-ui/react'
import { CloseIcon } from '@chakra-ui/icons'
import { getPendingOrders, getOrderHistory, cancelOrder, batchCancelOrders, PendingOrderInfo } from '../services/api'

interface OrderInfo {
  order_id: number
  client_order_id: string
  symbol: string
  side: string
  price: number
  quantity: number
  status: string
  created_at: string
  updated_at: string
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
  const toast = useToast()

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
      await Promise.all([fetchPendingOrders(), tabIndex === 1 && fetchHistoryOrders()])
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

  // 刷新待成交订單
  const refreshPendingOrders = async () => {
    try {
      const data = await getPendingOrders(selectedExchange, selectedSymbol)
      setPendingOrders(data.orders || [])
    } catch (err) {
      console.error('Failed to refresh pending orders:', err)
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

  // 计算订單统计
  const todayOrders = historyOrders.filter(order => {
    const orderDate = new Date(order.created_at)
    const today = new Date()
    return orderDate.toDateString() === today.toDateString()
  })

  const successOrders = historyOrders.filter(order => order.status === 'FILLED').length
  const successRate = historyOrders.length > 0 ? (successOrders / historyOrders.length) * 100 : 0

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
      <Text fontSize="md" color="gray.600" mb={4}>
        {t('orders.currentPair', { exchange: selectedExchange, symbol: selectedSymbol })}
      </Text>

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
                {/* 批量操作按钮 */}
                <HStack mb={4} justify="flex-end">
                  <Button
                    colorScheme="red"
                    size="sm"
                    onClick={handleCancelAllOrders}
                    isLoading={cancellingAll}
                    loadingText={t('orders.cancelling')}
                    isDisabled={pendingOrders.length === 0}
                  >
                    {t('orders.cancelAll')} ({pendingOrders.length})
                  </Button>
                </HStack>
                <TableContainer>
                <Table variant="simple">
                  <Thead>
                    <Tr>
                      <Th>{t('orders.orderId')}</Th>
                      <Th>{t('orders.strategy')}</Th>
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
                    {pendingOrders.map((order) => (
                      <Tr key={order.order_id}>
                        <Td>{order.order_id}</Td>
                        <Td>
                          <Badge colorScheme={order.strategy_type === 'grid' ? 'blue' : order.strategy_type === 'dca' ? 'purple' : 'gray'} variant="subtle">
                            {order.strategy_name || order.strategy_type || '-'}
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
                    ))}
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
                    <StatNumber>{historyOrders.length}</StatNumber>
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

            {historyOrders.length === 0 ? (
              <Text color="gray.500" textAlign="center" py={8}>{t('orders.noHistoryOrders')}</Text>
            ) : (
              <TableContainer>
                <Table variant="simple">
                  <Thead>
                    <Tr>
                      <Th>{t('orders.orderId')}</Th>
                      <Th>{t('orders.symbol')}</Th>
                      <Th>{t('orders.side')}</Th>
                      <Th isNumeric>{t('orders.price')}</Th>
                      <Th isNumeric>{t('orders.quantity')}</Th>
                      <Th>{t('orders.status')}</Th>
                      <Th>{t('orders.createdAt')}</Th>
                      <Th>{t('orders.updatedAt')}</Th>
                    </Tr>
                  </Thead>
                  <Tbody>
                    {historyOrders.map((order) => (
                      <Tr key={order.order_id}>
                        <Td>{order.order_id}</Td>
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
