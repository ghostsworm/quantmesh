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
      console.error('獲取策略列表失败:', err)
      setStrategies([])
      setError(t('strategyMarket.fetchError'))
    } finally {
      setLoading(false)
    }
  }

  const filteredStrategies = useMemo(() => {
    const validStrategies = (strategies || []).filter(s => s !== null && s !== undefined)
    let result = [...validStrategies]

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
          (s.name || '').toLowerCase().includes(query) ||
          (s.description || '').toLowerCase().includes(query) ||
          (s.features || []).some((f) => f.toLowerCase().includes(query))
      )
    }

    return result
  }, [strategies, activeTab, selectedType, searchQuery])

  const stats = useMemo(() => {
    const validStrategies = (strategies || []).filter(s => s !== null && s !== undefined)
    return {
      total: validStrategies.length,
      enabled: validStrategies.filter((s) => s.isEnabled).length,
      premium: validStrategies.filter((s) => s.isPremium).length,
      free: validStrategies.filter((s) => !s.isPremium).length,
    }
  }, [strategies])

  const handleEnable = async (strategyId: string) => {
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
    } catch (err: any) {
      toast({
        title: t('strategyMarket.enableFailed'),
        description: err.message || t('strategyMarket.checkConnection'),
        status: 'error',
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
    } catch (err: any) {
      toast({
        title: t('strategyMarket.disableFailed'),
        description: err.message || t('strategyMarket.checkConnection'),
        status: 'error',
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
    } catch (err: any) {
      toast({
        title: t('strategyMarket.fetchDetailFailed'),
        description: err.message || t('strategyMarket.cannotFetchDetail'),
        status: 'error',
        duration: 3000,
      })
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
