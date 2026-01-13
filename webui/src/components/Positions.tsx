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
} from '@chakra-ui/react'
import { useSymbol } from '../contexts/SymbolContext'
import { getPositions, getPositionsSummary, type PositionInfo, type PositionSummary, type PositionsResponse } from '../services/api'

const Positions: React.FC = () => {
  const { selectedExchange, selectedSymbol } = useSymbol()
  const [summary, setSummary] = useState<PositionSummary | null>(null)
  const [positions, setPositions] = useState<PositionInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

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

  if (loading && !summary) {
    return (
      <Box>
        <Heading size="lg" mb={6}>持仓汇总</Heading>
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
        <Heading size="lg" mb={6}>持仓汇总</Heading>
        <Text color="red.500">错误: {error}</Text>
      </Box>
    )
  }

  return (
    <Box>
      <Heading size="lg" mb={6}>持仓汇总</Heading>

      {/* 持仓汇总卡片 */}
      {summary && (
        <SimpleGrid columns={{ base: 1, md: 2, lg: 3 }} spacing={4} mb={8}>
          <Card>
            <CardBody>
              <Stat>
                <StatLabel>总持仓数量</StatLabel>
                <StatNumber>{summary.total_quantity.toFixed(4)}</StatNumber>
              </Stat>
            </CardBody>
          </Card>

          <Card>
            <CardBody>
              <Stat>
                <StatLabel>总持仓价值</StatLabel>
                <StatNumber>{summary.total_value.toFixed(2)}</StatNumber>
              </Stat>
            </CardBody>
          </Card>

          <Card>
            <CardBody>
              <Stat>
                <StatLabel>持仓槽位数</StatLabel>
                <StatNumber>{summary.position_count}</StatNumber>
              </Stat>
            </CardBody>
          </Card>

          <Card>
            <CardBody>
              <Stat>
                <StatLabel>平均持仓价格</StatLabel>
                <StatNumber>{summary.average_price.toFixed(2)}</StatNumber>
              </Stat>
            </CardBody>
          </Card>

          <Card>
            <CardBody>
              <Stat>
                <StatLabel>当前市场价格</StatLabel>
                <StatNumber>{summary.current_price.toFixed(2)}</StatNumber>
              </Stat>
            </CardBody>
          </Card>

          <Card>
            <CardBody>
              <Stat>
                <StatLabel>未实现盈亏</StatLabel>
                <StatNumber color={summary.unrealized_pnl >= 0 ? 'green.500' : 'red.500'}>
                  {summary.unrealized_pnl >= 0 ? '+' : ''}{summary.unrealized_pnl.toFixed(2)}
                </StatNumber>
              </Stat>
            </CardBody>
          </Card>
        </SimpleGrid>
      )}

      {/* 持仓列表表格 */}
      {positions.length > 0 && (
        <Box>
          <Heading size="md" mb={4}>持仓列表</Heading>
          <TableContainer>
            <Table variant="simple">
              <Thead>
                <Tr>
                  <Th>持仓价格</Th>
                  <Th isNumeric>持仓数量</Th>
                  <Th isNumeric>持仓价值</Th>
                  <Th isNumeric>未实现盈亏</Th>
                </Tr>
              </Thead>
              <Tbody>
                {positions.map((pos, index) => {
                  // 计算价格偏差（相对于当前价格）
                  const priceDeviation = summary && summary.current_price > 0 
                    ? ((pos.price - summary.current_price) / summary.current_price * 100)
                    : 0
                  const isPriceAnomaly = Math.abs(priceDeviation) > 50 // 偏差超过50%视为异常
                  
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
          <Text color="gray.500" fontSize="lg">暂无持仓</Text>
        </Box>
      )}
    </Box>
  )
}

export default Positions
