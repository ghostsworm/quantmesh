import React, { useEffect, useState } from 'react'
import {
  Box,
  Heading,
  SimpleGrid,
  Card,
  CardBody,
  Stat,
  StatLabel,
  StatNumber,
  Table,
  Thead,
  Tbody,
  Tr,
  Th,
  Td,
  TableContainer,
  Text,
  Spinner,
  Center,
  Skeleton,
  SkeletonText,
  Badge,
  Flex,
  Button,
  useToast,
} from '@chakra-ui/react'
import { useTranslation } from 'react-i18next'
import { useSymbol } from '../contexts/SymbolContext'
import { getPositions, getSymbols, getPendingOrders, batchCancelOrders, type PositionInfo, type PositionSummary, type PositionsResponse } from '../services/api'

const Positions: React.FC = () => {
  const { t } = useTranslation()
  const toast = useToast()
  const { selectedExchange, selectedSymbol, selectedMarketType } = useSymbol()
  const [summary, setSummary] = useState<PositionSummary | null>(null)
  const [positions, setPositions] = useState<PositionInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [symbolDirection, setSymbolDirection] = useState<'LONG' | 'SHORT' | null>(null)
  const [cancellingAllBuy, setCancellingAllBuy] = useState(false)

  useEffect(() => {
    const fetchData = async () => {
      try {
        setLoading(true)
        const data = await getPositions(selectedExchange || undefined, selectedSymbol || undefined)
        console.log('[Positions] API Response:', data)
        console.log('[Positions] Response keys:', Object.keys(data || {}))
        console.log('[Positions] Summary:', data?.summary)
        
        if (data && data.summary) {
          setSummary(data.summary)
          setPositions(data.summary.positions || [])
          setError(null)
        } else {
          const errorMsg = `Invalid response format. Response keys: ${Object.keys(data || {}).join(', ')}`
          setError(errorMsg)
          console.error('[Positions] Invalid response:', data)
        }
      } catch (err) {
        const errorMsg = err instanceof Error ? err.message : 'Failed to fetch positions'
        setError(errorMsg)
        console.error('[Positions] Failed to fetch positions:', err)
      } finally {
        setLoading(false)
      }
    }

    fetchData()
    // 每5秒刷新一次
    const interval = setInterval(fetchData, 5000)

    return () => clearInterval(interval)
  }, [selectedExchange, selectedSymbol])

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

  const handleCancelAllBuyOrders = async () => {
    if (!selectedExchange || !selectedSymbol) return
    setCancellingAllBuy(true)
    try {
      const { orders } = await getPendingOrders(selectedExchange, selectedSymbol)
      const buyOrderIds = (orders || []).filter(o => (o.side || '').toUpperCase() === 'BUY').map(o => o.order_id)
      if (buyOrderIds.length === 0) {
        toast({
          title: t('positionsPage.cancelAllBuyOrdersNone'),
          status: 'info',
          duration: 3000,
        })
        return
      }
      const result = await batchCancelOrders(buyOrderIds, selectedExchange, selectedSymbol)
      if (result.success) {
        toast({
          title: t('positionsPage.cancelAllBuyOrdersSuccess'),
          description: t('positionsPage.cancelAllBuyOrdersCount', { count: result.count ?? buyOrderIds.length }),
          status: 'success',
          duration: 3000,
        })
      } else {
        toast({
          title: t('positionsPage.cancelAllBuyOrdersFailed'),
          description: result.message,
          status: 'error',
          duration: 5000,
        })
      }
    } catch (err) {
      toast({
        title: t('positionsPage.cancelAllBuyOrdersFailed'),
        description: err instanceof Error ? err.message : String(err),
        status: 'error',
        duration: 5000,
      })
    } finally {
      setCancellingAllBuy(false)
    }
  }

  if (loading && !summary) {
    return (
      <Box>
        <Heading size="lg" mb={6}>{t('positionsPage.title')}</Heading>
        <SimpleGrid columns={{ base: 1, md: 2, lg: 3 }} spacing={4}>
          {[1, 2, 3, 4, 5, 6].map((i) => (
            <Card key={i}>
              <CardBody>
                <Skeleton height="20px" mb={2} />
                <SkeletonText noOfLines={2} spacing={2} />
              </CardBody>
            </Card>
          ))}
        </SimpleGrid>
      </Box>
    )
  }

  if (error) {
    return (
      <Box>
        <Heading size="lg" mb={6}>{t('positionsPage.title')}</Heading>
        <Text color="red.500">{t('common.error')}: {error}</Text>
      </Box>
    )
  }

  return (
    <Box>
      <Flex align="center" gap={3} mb={6} flexWrap="wrap">
        <Heading size="lg">{t('positionsPage.title')}</Heading>
        {symbolDirection != null && (
          <Badge colorScheme={symbolDirection === 'SHORT' ? 'orange' : 'green'} fontSize="sm">
            {symbolDirection === 'SHORT' ? t('configuration.directionShort') : t('configuration.directionLong')}
          </Badge>
        )}
        {selectedExchange && selectedSymbol && (
          <Button
            size="sm"
            colorScheme="orange"
            variant="outline"
            isLoading={cancellingAllBuy}
            loadingText={t('positionsPage.cancelAllBuyOrders')}
            onClick={handleCancelAllBuyOrders}
          >
            {t('positionsPage.cancelAllBuyOrders')}
          </Button>
        )}
      </Flex>

      {/* 持倉彙總卡片 */}
      {summary && (
        <SimpleGrid columns={{ base: 1, md: 2, lg: 3 }} spacing={4} mb={8}>
          <Card>
            <CardBody>
              <Stat>
                <StatLabel>{t('positionsPage.totalQuantity')}</StatLabel>
                <StatNumber>{summary.total_quantity.toFixed(4)}</StatNumber>
              </Stat>
            </CardBody>
          </Card>

          <Card>
            <CardBody>
              <Stat>
                <StatLabel>{t('positionsPage.totalValue')}</StatLabel>
                <StatNumber>{summary.total_value.toFixed(2)}</StatNumber>
              </Stat>
            </CardBody>
          </Card>

          <Card>
            <CardBody>
              <Stat>
                <StatLabel>{t('positionsPage.positionSlots')}</StatLabel>
                <StatNumber>{summary.position_count}</StatNumber>
              </Stat>
            </CardBody>
          </Card>

          <Card>
            <CardBody>
              <Stat>
                <StatLabel>{t('positionsPage.averagePrice')}</StatLabel>
                <StatNumber>{summary.average_price.toFixed(2)}</StatNumber>
              </Stat>
            </CardBody>
          </Card>

          <Card>
            <CardBody>
              <Stat>
                <StatLabel>{t('positionsPage.currentMarketPrice')}</StatLabel>
                <StatNumber>{summary.current_price.toFixed(2)}</StatNumber>
              </Stat>
            </CardBody>
          </Card>

          <Card>
            <CardBody>
              <Stat>
                <StatLabel>{t('positionsPage.unrealizedPnl')}</StatLabel>
                <StatNumber color={summary.unrealized_pnl >= 0 ? 'green.500' : 'red.500'}>
                  {summary.unrealized_pnl >= 0 ? '+' : ''}{summary.unrealized_pnl.toFixed(2)}
                </StatNumber>
              </Stat>
            </CardBody>
          </Card>

          <Card>
            <CardBody>
              <Stat>
                <StatLabel>
                  {t('positionsPage.actualMargin')}
                  {summary.leverage && summary.leverage > 1 && (
                    <Text as="span" fontSize="xs" color="gray.500" ml={2}>
                      ({t('positionsPage.leverage')} {summary.leverage}x)
                    </Text>
                  )}
                </StatLabel>
                <StatNumber>{(summary.actual_margin || 0).toFixed(2)}</StatNumber>
                {summary.leverage && summary.leverage > 1 && (
                  <Text fontSize="xs" color="gray.500" mt={1}>
                    {t('positionsPage.positionValue')}: {summary.total_value.toFixed(2)}
                  </Text>
                )}
              </Stat>
            </CardBody>
          </Card>
        </SimpleGrid>
      )}

      {/* 持倉列表表格 */}
      {positions.length > 0 && (
        <Box>
          <Heading size="md" mb={4}>{t('positionsPage.positionList')}</Heading>
          <TableContainer>
            <Table variant="simple">
              <Thead>
                <Tr>
                  <Th>{t('positionsPage.positionPrice')}</Th>
                  <Th isNumeric>{t('positionsPage.positionQuantity')}</Th>
                  <Th isNumeric>{t('positionsPage.positionValueCol')}</Th>
                  <Th isNumeric>{t('positionsPage.unrealizedPnl')}</Th>
                </Tr>
              </Thead>
              <Tbody>
                {positions.map((pos, index) => {
                  // 计算價格偏差（相對於當前價格）
                  const priceDeviation = summary && summary.current_price > 0 
                    ? ((pos.price - summary.current_price) / summary.current_price * 100)
                    : 0
                  const isPriceAnomaly = Math.abs(priceDeviation) > 50 // 偏差超過50%視為异常
                  
                  return (
                    <Tr key={index}>
                      <Td>
                        <Box>
                          <Text fontWeight={isPriceAnomaly ? 'bold' : 'normal'} color={isPriceAnomaly ? 'orange.500' : 'inherit'}>
                            {pos.price.toFixed(2)}
                          </Text>
                          {summary && summary.current_price > 0 && (
                            <Text fontSize="xs" color="gray.500">
                              {priceDeviation >= 0 ? '+' : ''}{priceDeviation.toFixed(1)}%
                            </Text>
                          )}
                        </Box>
                      </Td>
                      <Td isNumeric>{pos.quantity.toFixed(4)}</Td>
                      <Td isNumeric>{pos.value.toFixed(2)}</Td>
                      <Td isNumeric color={pos.unrealized_pnl >= 0 ? 'green.500' : 'red.500'}>
                        {pos.unrealized_pnl >= 0 ? '+' : ''}{pos.unrealized_pnl.toFixed(2)}
                      </Td>
                    </Tr>
                  )
                })}
              </Tbody>
            </Table>
          </TableContainer>
        </Box>
      )}

      {positions.length === 0 && summary && summary.position_count === 0 && (
        <Box textAlign="center" py={12}>
          <Text color="gray.500" fontSize="lg">{t('positionsPage.noPositions')}</Text>
        </Box>
      )}
    </Box>
  )
}

export default Positions
