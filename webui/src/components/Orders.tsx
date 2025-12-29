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
} from '@chakra-ui/react'
import { getPendingOrders, getOrderHistory, PendingOrderInfo } from '../services/api'

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
    
    // 待成交订单每5秒刷新一次，历史订单每30秒刷新一次
    const interval = setInterval(() => {
      fetchPendingOrders()
      if (tabIndex === 1) {
        fetchHistoryOrders()
      }
    }, tabIndex === 0 ? 5000 : 30000)

    return () => clearInterval(interval)
  }, [tabIndex, selectedExchange, selectedSymbol])

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

  // 计算订单统计
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
          {/* 待成交订单 */}
          <TabPanel>
            {pendingOrders.length === 0 ? (
              <Text color="gray.500" textAlign="center" py={8}>{t('orders.noPendingOrders')}</Text>
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
                      <Th isNumeric>{t('orders.filled')}</Th>
                      <Th>{t('orders.status')}</Th>
                      <Th isNumeric>{t('orders.slotPrice')}</Th>
                      <Th>{t('orders.createdAt')}</Th>
                    </Tr>
                  </Thead>
                  <Tbody>
                    {pendingOrders.map((order) => (
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
                        <Td isNumeric>{order.filled_quantity != null ? order.filled_quantity.toFixed(4) : '-'}</Td>
                        <Td>
                          <Badge colorScheme={getStatusColorScheme(order.status)}>
                            {getStatusText(order.status)}
                          </Badge>
                        </Td>
                        <Td isNumeric>{order.slot_price != null ? order.slot_price.toFixed(2) : '-'}</Td>
                        <Td>{formatTime(order.created_at)}</Td>
                      </Tr>
                    ))}
                  </Tbody>
                </Table>
              </TableContainer>
            )}
          </TabPanel>

          {/* 历史订单 */}
          <TabPanel>
            {/* 订单统计卡片 */}
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
                    <StatLabel>已完成订单</StatLabel>
                    <StatNumber color="green.500">{successOrders}</StatNumber>
                  </Stat>
                </CardBody>
              </Card>
            </SimpleGrid>

            {historyOrders.length === 0 ? (
              <Text color="gray.500" textAlign="center" py={8}>暂无历史订单</Text>
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
                      <Th>更新时间</Th>
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
