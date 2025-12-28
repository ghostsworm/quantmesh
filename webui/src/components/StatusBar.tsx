import React, { useEffect, useState } from 'react'
import { Flex, Text, Badge, Spinner, HStack, Tooltip } from '@chakra-ui/react'
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
    const interval = setInterval(fetchFundingRate, 60000)
    return () => clearInterval(interval)
  }, [selectedExchange, selectedSymbol, isGlobalView])

  if (isGlobalView) {
    return (
      <Badge colorScheme="purple" variant="subtle" fontSize="10px" borderRadius="full" px={3}>
        Global View
      </Badge>
    )
  }

  if (!selectedExchange || !selectedSymbol) {
    return null
  }

  return (
    <HStack spacing={3}>
      <Tooltip label={`${selectedExchange.toUpperCase()} 资金费率`}>
        <HStack spacing={2} bg="gray.50" px={3} py={1} borderRadius="full" border="1px" borderColor="gray.100">
          <Text fontSize="10px" fontWeight="bold" color="gray.400">FR</Text>
          {loading ? (
            <Spinner size="xs" speed="0.8s" thickness="1px" />
          ) : fundingRate !== null ? (
            <Text
              fontSize="11px"
              fontWeight="bold"
              color={fundingRate >= 0 ? 'green.500' : 'red.500'}
            >
              {fundingRate >= 0 ? '+' : ''}
              {(fundingRate * 100).toFixed(4)}%
            </Text>
          ) : (
            <Text fontSize="11px" color="gray.400">--</Text>
          )}
        </HStack>
      </Tooltip>
      
      <Badge colorScheme="blue" variant="solid" fontSize="10px" borderRadius="full" px={3}>
        {selectedSymbol}
      </Badge>
    </HStack>
  )
}

export default StatusBar
