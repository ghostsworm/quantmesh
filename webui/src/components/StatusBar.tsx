import React, { useEffect, useState } from 'react'
import { Box, Flex, Text, Badge, Spinner } from '@chakra-ui/react'
import { useSymbol } from '../contexts/SymbolContext'
import { getFundingRateCurrent } from '../services/api'

const StatusBar: React.FC = () => {
  const { selectedExchange, selectedSymbol, isGlobalView } = useSymbol()
  const [fundingRate, setFundingRate] = useState<number | null>(null)
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    if (isGlobalView || !selectedExchange || !selectedSymbol) {
      setFundingRate(null)
      return
    }

    const fetchFundingRate = async () => {
      setLoading(true)
      try {
        const data = await getFundingRateCurrent(selectedExchange, selectedSymbol)
        // 从返回的 rates 对象中获取对应交易对的费率
        const symbolKey = selectedSymbol
        if (data.rates && data.rates[symbolKey]) {
          setFundingRate(data.rates[symbolKey].rate_pct)
        } else {
          setFundingRate(null)
        }
      } catch (error) {
        console.error('获取资金费率失败:', error)
        setFundingRate(null)
      } finally {
        setLoading(false)
      }
    }

    fetchFundingRate()
    const interval = setInterval(fetchFundingRate, 60000) // 每分钟更新一次
    return () => clearInterval(interval)
  }, [selectedExchange, selectedSymbol, isGlobalView])

  if (isGlobalView) {
    return (
      <Box
        bg="gray.100"
        borderBottom="1px"
        borderColor="gray.200"
        py={2}
      >
        <Flex maxW="container.xl" mx="auto" px={4} align="center" justify="center">
          <Badge colorScheme="purple" fontSize="sm" px={3} py={1}>
            全局概览模式
          </Badge>
        </Flex>
      </Box>
    )
  }

  if (!selectedExchange || !selectedSymbol) {
    return null
  }

  return (
    <Box
      bg="gray.100"
      borderBottom="1px"
      borderColor="gray.200"
      py={2}
    >
      <Flex
        maxW="container.xl"
        mx="auto"
        px={4}
        align="center"
        justify="space-between"
        gap={4}
      >
        <Flex align="center" gap={4}>
          <Text fontSize="sm" fontWeight="medium" color="gray.700">
            当前交易对:
          </Text>
          <Badge colorScheme="blue" fontSize="sm" px={3} py={1}>
            {selectedExchange.toUpperCase()} / {selectedSymbol}
          </Badge>
        </Flex>

        {loading ? (
          <Flex align="center" gap={2}>
            <Spinner size="xs" color="gray.600" />
            <Text fontSize="sm" color="gray.600">
              加载资金费率...
            </Text>
          </Flex>
        ) : fundingRate !== null ? (
          <Flex align="center" gap={2}>
            <Text fontSize="sm" fontWeight="medium" color="gray.700">
              资金费率:
            </Text>
            <Text
              fontSize="sm"
              fontWeight="bold"
              color={fundingRate >= 0 ? 'green.600' : 'red.600'}
            >
              {fundingRate >= 0 ? '+' : ''}
              {(fundingRate * 100).toFixed(4)}%
            </Text>
          </Flex>
        ) : null}
      </Flex>
    </Box>
  )
}

export default StatusBar

