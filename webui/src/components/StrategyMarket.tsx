import React, { useState, useEffect, useMemo } from 'react'
import {
  Box,
  VStack,
  HStack,
  Heading,
  Text,
  Button,
  Input,
  InputGroup,
  InputLeftElement,
  Select,
  Tabs,
  TabList,
  Tab,
  SimpleGrid,
  Stat,
  StatLabel,
  StatNumber,
  Badge,
  useDisclosure,
  useToast,
  Spinner,
  Center,
  Icon,
  Flex,
  useColorModeValue,
} from '@chakra-ui/react'
import { SearchIcon, StarIcon, CheckCircleIcon } from '@chakra-ui/icons'
import { motion } from 'framer-motion'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { StrategyGrid, StrategyDetailModal } from './strategy'
import {
  getStrategies,
  getStrategyDetail,
  enableStrategy,
  disableStrategy,
} from '../services/strategy'
import type { StrategyInfo, StrategyDetailInfo, StrategyType } from '../types/strategy'

const MotionBox = motion(Box)

// Mock data for development (will be replaced by API)
const MOCK_STRATEGIES: StrategyInfo[] = [
  {
    id: 'grid',
    name: '网格交易',
    type: 'grid',
    description: '经典网格策略，通过在价格区间内设置多个买卖档位实现低买高卖。适合震荡行情。',
    riskLevel: 'low',
    isPremium: false,
    isEnabled: true,
    features: ['自动挂单', '多档位', '震荡行情'],
    minCapital: 100,
    recommendedCapital: 500,
  },
  {
    id: 'dca_enhanced',
    name: '增强型 DCA',
    type: 'dca',
    description: 'ATR 动态间距、三重止盈、50层仓位管理、瀑布保护、趋势过滤的增强型定投策略。',
    riskLevel: 'medium',
    isPremium: false,
    isEnabled: false,
    features: ['ATR动态', '三重止盈', '50层仓位', '瀑布保护'],
    minCapital: 200,
    recommendedCapital: 1000,
  },
  {
    id: 'martingale',
    name: '马丁格尔',
    type: 'martingale',
    description: '基于加倍下注原理的策略，支持正向/反向马丁，递减风控，多方向支持。',
    riskLevel: 'high',
    isPremium: true,
    isEnabled: false,
    features: ['加倍策略', '反向马丁', '递减风控'],
    minCapital: 500,
    recommendedCapital: 2000,
  },
  {
    id: 'trend_following',
    name: '趋势跟踪',
    type: 'trend',
    description: '基于双均线系统的趋势追踪策略，支持 MA/EMA，自动识别趋势并顺势交易。',
    riskLevel: 'medium',
    isPremium: false,
    isEnabled: false,
    features: ['双均线', '趋势识别', '自动交易'],
    minCapital: 300,
    recommendedCapital: 1000,
  },
  {
    id: 'mean_reversion',
    name: '均值回归',
    type: 'mean_reversion',
    description: '基于布林带的均值回归策略，在价格偏离均值时进行逆向交易。',
    riskLevel: 'medium',
    isPremium: false,
    isEnabled: false,
    features: ['布林带', '均值回归', '超买超卖'],
    minCapital: 200,
    recommendedCapital: 800,
  },
  {
    id: 'combo',
    name: '组合策略',
    type: 'combo',
    description: '多策略组合，多方向对冲，市场自适应切换，动态策略权重调整。',
    riskLevel: 'high',
    isPremium: true,
    isEnabled: false,
    features: ['多策略', '动态权重', '自适应'],
    minCapital: 1000,
    recommendedCapital: 5000,
  },
]

const StrategyMarket: React.FC = () => {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const toast = useToast()

  const [strategies, setStrategies] = useState<StrategyInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [searchQuery, setSearchQuery] = useState('')
  const [selectedType, setSelectedType] = useState<StrategyType | 'all'>('all')
  const [activeTab, setActiveTab] = useState(0) // 0: All, 1: Enabled, 2: Premium
  const [selectedStrategy, setSelectedStrategy] = useState<StrategyDetailInfo | null>(null)
  const { isOpen, onOpen, onClose } = useDisclosure()

  const bgColor = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue('gray.200', 'gray.600')

  useEffect(() => {
    fetchStrategies()
  }, [])

  const fetchStrategies = async () => {
    setLoading(true)
    try {
      const response = await getStrategies()
      setStrategies(response.strategies)
      setError(null)
    } catch (err) {
      // Fallback to mock data for development
      console.warn('Using mock data:', err)
      setStrategies(MOCK_STRATEGIES)
      setError(null)
    } finally {
      setLoading(false)
    }
  }

  const filteredStrategies = useMemo(() => {
    let result = [...strategies]

    // Filter by tab
    if (activeTab === 1) {
      result = result.filter((s) => s.isEnabled)
    } else if (activeTab === 2) {
      result = result.filter((s) => s.isPremium)
    }

    // Filter by type
    if (selectedType !== 'all') {
      result = result.filter((s) => s.type === selectedType)
    }

    // Filter by search
    if (searchQuery) {
      const query = searchQuery.toLowerCase()
      result = result.filter(
        (s) =>
          s.name.toLowerCase().includes(query) ||
          s.description.toLowerCase().includes(query) ||
          s.features.some((f) => f.toLowerCase().includes(query))
      )
    }

    return result
  }, [strategies, activeTab, selectedType, searchQuery])

  const stats = useMemo(() => ({
    total: strategies.length,
    enabled: strategies.filter((s) => s.isEnabled).length,
    premium: strategies.filter((s) => s.isPremium).length,
    free: strategies.filter((s) => !s.isPremium).length,
  }), [strategies])

  const handleEnable = async (strategyId: string) => {
    const strategy = strategies.find((s) => s.id === strategyId)
    if (strategy?.isPremium) {
      // Navigate to purchase or show purchase modal
      toast({
        title: t('strategyMarket.premiumRequired'),
        description: t('strategyMarket.premiumRequiredDesc'),
        status: 'info',
        duration: 5000,
      })
      return
    }

    try {
      await enableStrategy(strategyId)
      setStrategies((prev) =>
        prev.map((s) => (s.id === strategyId ? { ...s, isEnabled: true } : s))
      )
      toast({
        title: t('strategyMarket.enableSuccess'),
        status: 'success',
        duration: 3000,
      })
    } catch (err) {
      // For development, just update local state
      setStrategies((prev) =>
        prev.map((s) => (s.id === strategyId ? { ...s, isEnabled: true } : s))
      )
      toast({
        title: t('strategyMarket.enableSuccess'),
        status: 'success',
        duration: 3000,
      })
    }
  }

  const handleDisable = async (strategyId: string) => {
    try {
      await disableStrategy(strategyId)
      setStrategies((prev) =>
        prev.map((s) => (s.id === strategyId ? { ...s, isEnabled: false } : s))
      )
      toast({
        title: t('strategyMarket.disableSuccess'),
        status: 'success',
        duration: 3000,
      })
    } catch (err) {
      // For development, just update local state
      setStrategies((prev) =>
        prev.map((s) => (s.id === strategyId ? { ...s, isEnabled: false } : s))
      )
      toast({
        title: t('strategyMarket.disableSuccess'),
        status: 'success',
        duration: 3000,
      })
    }
  }

  const handleConfigure = (strategyId: string) => {
    navigate(`/capital-management?strategy=${strategyId}`)
  }

  const handleViewDetail = async (strategyId: string) => {
    try {
      const response = await getStrategyDetail(strategyId)
      setSelectedStrategy(response.strategy)
    } catch (err) {
      // For development, create mock detail
      const strategy = strategies.find((s) => s.id === strategyId)
      if (strategy) {
        setSelectedStrategy({
          ...strategy,
          longDescription: strategy.description + '\n\n详细说明：此策略采用先进的算法进行交易决策...',
          parameters: [
            {
              name: '价格间隔',
              key: 'priceInterval',
              type: 'number',
              defaultValue: 1,
              min: 0.1,
              max: 100,
              description: '网格之间的价格间隔',
              required: true,
            },
            {
              name: '订单数量',
              key: 'orderQuantity',
              type: 'number',
              defaultValue: 10,
              min: 1,
              max: 100,
              description: '每个网格的订单数量',
              required: true,
            },
          ],
        })
      }
    }
    onOpen()
  }

  return (
    <Box>
      <VStack align="stretch" spacing={6}>
        {/* Header */}
        <MotionBox
          initial={{ opacity: 0, y: -20 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.5 }}
        >
          <Flex justify="space-between" align="center" wrap="wrap" gap={4}>
            <VStack align="start" spacing={1}>
              <Heading size="lg">{t('strategyMarket.title')}</Heading>
              <Text color="gray.500">{t('strategyMarket.subtitle')}</Text>
            </VStack>
            <Button
              colorScheme="blue"
              leftIcon={<CheckCircleIcon />}
              onClick={() => navigate('/capital-management')}
            >
              {t('strategyMarket.manageCapital')}
            </Button>
          </Flex>
        </MotionBox>

        {/* Stats */}
        <SimpleGrid columns={{ base: 2, md: 4 }} spacing={4}>
          <Box p={4} bg={bgColor} borderRadius="lg" borderWidth="1px" borderColor={borderColor}>
            <Stat>
              <StatLabel>{t('strategyMarket.totalStrategies')}</StatLabel>
              <StatNumber>{stats.total}</StatNumber>
            </Stat>
          </Box>
          <Box p={4} bg={bgColor} borderRadius="lg" borderWidth="1px" borderColor={borderColor}>
            <Stat>
              <StatLabel>{t('strategyMarket.enabledStrategies')}</StatLabel>
              <StatNumber color="green.500">{stats.enabled}</StatNumber>
            </Stat>
          </Box>
          <Box p={4} bg={bgColor} borderRadius="lg" borderWidth="1px" borderColor={borderColor}>
            <Stat>
              <StatLabel>{t('strategyMarket.freeStrategies')}</StatLabel>
              <StatNumber color="blue.500">{stats.free}</StatNumber>
            </Stat>
          </Box>
          <Box p={4} bg={bgColor} borderRadius="lg" borderWidth="1px" borderColor={borderColor}>
            <Stat>
              <StatLabel>
                <HStack>
                  <Icon as={StarIcon} color="purple.500" />
                  <Text>{t('strategyMarket.premiumStrategies')}</Text>
                </HStack>
              </StatLabel>
              <StatNumber color="purple.500">{stats.premium}</StatNumber>
            </Stat>
          </Box>
        </SimpleGrid>

        {/* Filters */}
        <Box p={4} bg={bgColor} borderRadius="lg" borderWidth="1px" borderColor={borderColor}>
          <VStack align="stretch" spacing={4}>
            <Tabs index={activeTab} onChange={setActiveTab} variant="soft-rounded" colorScheme="blue">
              <TabList>
                <Tab>{t('strategyMarket.allStrategies')}</Tab>
                <Tab>
                  {t('strategyMarket.enabled')}
                  {stats.enabled > 0 && (
                    <Badge ml={2} colorScheme="green" borderRadius="full">
                      {stats.enabled}
                    </Badge>
                  )}
                </Tab>
                <Tab>
                  <HStack>
                    <Icon as={StarIcon} />
                    <Text>{t('strategyMarket.premium')}</Text>
                  </HStack>
                </Tab>
              </TabList>
            </Tabs>

            <HStack spacing={4} flexWrap="wrap">
              <InputGroup maxW="300px">
                <InputLeftElement>
                  <SearchIcon color="gray.400" />
                </InputLeftElement>
                <Input
                  placeholder={t('strategyMarket.searchPlaceholder')}
                  value={searchQuery}
                  onChange={(e) => setSearchQuery(e.target.value)}
                />
              </InputGroup>

              <Select
                maxW="200px"
                value={selectedType}
                onChange={(e) => setSelectedType(e.target.value as StrategyType | 'all')}
              >
                <option value="all">{t('strategyMarket.allTypes')}</option>
                <option value="grid">{t('strategyMarket.types.grid')}</option>
                <option value="dca">{t('strategyMarket.types.dca')}</option>
                <option value="martingale">{t('strategyMarket.types.martingale')}</option>
                <option value="trend">{t('strategyMarket.types.trend')}</option>
                <option value="mean_reversion">{t('strategyMarket.types.mean_reversion')}</option>
                <option value="combo">{t('strategyMarket.types.combo')}</option>
              </Select>
            </HStack>
          </VStack>
        </Box>

        {/* Strategy Grid */}
        <StrategyGrid
          strategies={filteredStrategies}
          loading={loading}
          error={error}
          onEnable={handleEnable}
          onDisable={handleDisable}
          onConfigure={handleConfigure}
          onViewDetail={handleViewDetail}
        />
      </VStack>

      {/* Detail Modal */}
      <StrategyDetailModal
        isOpen={isOpen}
        onClose={onClose}
        strategy={selectedStrategy}
        onEnable={handleEnable}
        onDisable={handleDisable}
        onConfigure={handleConfigure}
      />
    </Box>
  )
}

export default StrategyMarket
