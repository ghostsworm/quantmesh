import React, { useEffect, useState, useCallback } from 'react'
import {
  Box,
  Container,
  Heading,
  SimpleGrid,
  Card,
  CardBody,
  CardHeader,
  Text,
  Badge,
  Spinner,
  Center,
  useToast,
  Flex,
  HStack,
  VStack,
  useColorModeValue,
  Input,
  InputGroup,
  InputLeftElement,
  Select,
} from '@chakra-ui/react'
import { SearchIcon, RepeatIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { motion } from 'framer-motion'
import { getStrategyRuntimeStatusAll, type SymbolStrategyRuntimeItem, type StrategyRuntimeStatus } from '../services/strategy'

const MotionCard = motion(Card)

interface StrategyCardData {
  exchange: string
  symbol: string
  marketType: string
  strategy: StrategyRuntimeStatus
}

const StrategyOverview: React.FC = () => {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { setSymbolPair } = useSymbol()
  const [data, setData] = useState<SymbolStrategyRuntimeItem[]>([])
  const [loading, setLoading] = useState(true)
  const [filterType, setFilterType] = useState<string>('')
  const [searchText, setSearchText] = useState('')
  const toast = useToast()

  const cardBg = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue('gray.200', 'gray.600')
  const hoverBg = useColorModeValue('gray.50', 'gray.700')

  const fetchData = useCallback(async () => {
    setLoading(true)
    try {
      const res = await getStrategyRuntimeStatusAll()
      if (res.success && res.data) {
        setData(res.data)
      } else {
        setData([])
      }
    } catch (err) {
      console.error('Failed to fetch strategy overview:', err)
      toast({
        title: t('common.error'),
        description: t('common.loadFailed'),
        status: 'error',
        duration: 3000,
      })
      setData([])
    } finally {
      setLoading(false)
    }
  }, [t, toast])

  useEffect(() => {
    fetchData()
    const interval = setInterval(fetchData, 15000)
    return () => clearInterval(interval)
  }, [fetchData])

  const flatStrategies: StrategyCardData[] = []
  for (const item of data) {
    for (const s of item.strategies) {
      if (!s.isEnabled) continue
      flatStrategies.push({
        exchange: item.exchange,
        symbol: item.symbol,
        marketType: item.marketType,
        strategy: s,
      })
    }
  }

  const filtered = flatStrategies.filter((item) => {
    const typeMatch = !filterType || item.strategy.type === filterType || item.strategy.name === filterType
    const searchLower = searchText.toLowerCase()
    const searchMatch =
      !searchText ||
      item.exchange.toLowerCase().includes(searchLower) ||
      item.symbol.toLowerCase().includes(searchLower) ||
      item.strategy.name.toLowerCase().includes(searchLower) ||
      item.strategy.type.toLowerCase().includes(searchLower)
    return typeMatch && searchMatch
  })

  const sorted = [...filtered].sort((a, b) => {
    const pnlA = a.strategy.statistics?.totalPnL ?? 0
    const pnlB = b.strategy.statistics?.totalPnL ?? 0
    return pnlB - pnlA
  })

  const handleCardClick = (item: StrategyCardData) => {
    const params = new URLSearchParams({
      exchange: item.exchange,
      symbol: item.symbol,
      market_type: item.marketType,
      strategy: item.strategy.name,
    })
    navigate(`/strategy-detail?${params.toString()}`)
  }

  const strategyTypes = Array.from(new Set(flatStrategies.map((s) => s.strategy.type)))

  if (loading && data.length === 0) {
    return (
      <Center minH="300px">
        <Spinner size="xl" thickness="3px" color="blue.500" />
      </Center>
    )
  }

  return (
    <Container maxW="container.xl" py={6}>
      <Flex justify="space-between" align="center" mb={6} flexWrap="wrap" gap={4}>
        <Heading size="lg">{t('strategyOverview.title', '策略总览')}</Heading>
        <HStack spacing={3}>
          <InputGroup size="sm" maxW="200px">
            <InputLeftElement pointerEvents="none">
              <SearchIcon color="gray.400" />
            </InputLeftElement>
            <Input
              placeholder={t('strategyOverview.searchPlaceholder', '搜索交易所/币种/策略')}
              value={searchText}
              onChange={(e) => setSearchText(e.target.value)}
            />
          </InputGroup>
          <Select
            size="sm"
            maxW="140px"
            value={filterType}
            onChange={(e) => setFilterType(e.target.value)}
            placeholder={t('strategyOverview.filterByType', '按类型筛选')}
          >
            {strategyTypes.map((type) => (
              <option key={type} value={type}>
                {t(`strategyNames.${type}`, { defaultValue: type })}
              </option>
            ))}
          </Select>
          <Box
            as="button"
            onClick={fetchData}
            p={2}
            borderRadius="md"
            _hover={{ bg: hoverBg }}
            title={t('common.refresh')}
          >
            <RepeatIcon />
          </Box>
        </HStack>
      </Flex>

      {sorted.length === 0 ? (
        <Center py={16}>
          <Text color="gray.500">{t('strategyOverview.noStrategies', '暂无运行中的策略')}</Text>
        </Center>
      ) : (
        <SimpleGrid columns={{ base: 1, md: 2, lg: 3 }} spacing={4}>
          {sorted.map((item, idx) => (
            <MotionCard
              key={`${item.exchange}:${item.symbol}:${item.marketType}:${item.strategy.name}:${idx}`}
              bg={cardBg}
              borderWidth="1px"
              borderColor={borderColor}
              cursor="pointer"
              onClick={() => handleCardClick(item)}
              initial={{ opacity: 0, y: 10 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ delay: idx * 0.03 }}
              whileHover={{ y: -2, boxShadow: 'md' }}
              _hover={{ bg: hoverBg }}
            >
              <CardHeader pb={2}>
                <Flex justify="space-between" align="flex-start">
                  <VStack align="stretch" spacing={0}>
                    <Text fontWeight="bold" fontSize="md">
                      {t(`strategyNames.${item.strategy.name}`, { defaultValue: item.strategy.name })} · {item.symbol}
                    </Text>
                    <HStack spacing={2} fontSize="sm" color="gray.500">
                      <Text>{item.exchange}</Text>
                      <Badge size="sm" variant="outline">
                        {item.marketType === 'spot' ? t('strategyOverview.spot', '现货') : t('strategyOverview.futures', '合约')}
                      </Badge>
                    </HStack>
                  </VStack>
                  <Badge colorScheme={item.strategy.isRunning ? 'green' : 'gray'}>
                    {item.strategy.isRunning ? t('strategyOverview.running', '运行中') : t('strategyOverview.stopped', '已停止')}
                  </Badge>
                </Flex>
              </CardHeader>
              <CardBody pt={0}>
                <SimpleGrid columns={2} spacing={3} fontSize="sm">
                  <Box>
                    <Text color="gray.500">{t('strategyOverview.pnl', '累计盈亏')}</Text>
                    <Text fontWeight="semibold" color={item.strategy.statistics && item.strategy.statistics.totalPnL >= 0 ? 'green.500' : 'red.500'}>
                      {item.strategy.statistics?.totalPnL != null
                        ? `${item.strategy.statistics.totalPnL >= 0 ? '+' : ''}${item.strategy.statistics.totalPnL.toFixed(2)}`
                        : '-'}{' '}
                      USDT
                    </Text>
                  </Box>
                  <Box>
                    <Text color="gray.500">{t('strategyOverview.positions', '持仓数')}</Text>
                    <Text fontWeight="semibold">{item.strategy.positionCount ?? 0}</Text>
                  </Box>
                  <Box>
                    <Text color="gray.500">{t('strategyOverview.orders', '挂单数')}</Text>
                    <Text fontWeight="semibold">{item.strategy.orderCount ?? 0}</Text>
                  </Box>
                  <Box>
                    <Text color="gray.500">{t('strategyOverview.fundsUsed', '资金使用')}</Text>
                    <Text fontWeight="semibold">
                      {item.strategy.allocatedFunds > 0
                        ? `${((item.strategy.usedFunds / item.strategy.allocatedFunds) * 100).toFixed(0)}%`
                        : '-'}
                    </Text>
                  </Box>
                </SimpleGrid>
              </CardBody>
            </MotionCard>
          ))}
        </SimpleGrid>
      )}
    </Container>
  )
}

export default StrategyOverview
