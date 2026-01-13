import React, { useState, useEffect, useMemo } from 'react'
import {
  Box,
  VStack,
  HStack,
  Heading,
  Text,
  Button,
  SimpleGrid,
  Stat,
  StatLabel,
  StatNumber,
  StatHelpText,
  Alert,
  AlertIcon,
  useToast,
  Spinner,
  Center,
  Divider,
  Flex,
  Switch,
  FormControl,
  FormLabel,
  useColorModeValue,
  Tabs,
  TabList,
  TabPanels,
  Tab,
  TabPanel,
  Badge,
} from '@chakra-ui/react'
import { SettingsIcon, RepeatIcon, InfoIcon } from '@chakra-ui/icons'
import { motion } from 'framer-motion'
import { useTranslation } from 'react-i18next'
import { useSearchParams, useNavigate } from 'react-router-dom'
import { CapitalSlider, AllocationChart, RebalanceButton } from './capital'
import {
  getCapitalOverview,
  getCapitalAllocation,
  updateCapitalAllocation,
} from '../services/capital'
import type { CapitalOverview, StrategyCapitalInfo, CapitalAllocationConfig, ExchangeCapitalDetail, AssetAllocation } from '../types/capital'

const MotionBox = motion(Box)

const CapitalManagement: React.FC = () => {
  const { t } = useTranslation()
  const toast = useToast()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const highlightStrategy = searchParams.get('strategy')

  const [overview, setOverview] = useState<CapitalOverview | null>(null)
  const [exchanges, setExchanges] = useState<ExchangeCapitalDetail[]>([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [isPercentageMode, setIsPercentageMode] = useState(false)
  const [pendingChanges, setPendingChanges] = useState<Record<string, number>>({})
  const [selectedExchangeIndex, setSelectedExchangeIndex] = useState(0)

  const bgColor = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue('gray.200', 'gray.600')

  useEffect(() => {
    fetchData()
  }, [])

  const fetchData = async () => {
    setLoading(true)
    try {
      const [overviewRes, allocationRes] = await Promise.all([
        getCapitalOverview(),
        getCapitalAllocation(),
      ])
      if (overviewRes.success) setOverview(overviewRes.overview)
      if (allocationRes.success) setExchanges(allocationRes.exchanges)
    } catch (err) {
      console.error('Failed to fetch capital data:', err)
      toast({
        title: '获取资金数据失败',
        description: '请检查后端服务连接',
        status: 'error',
        duration: 5000,
      })
    } finally {
      setLoading(false)
    }
  }

  // 获取当前选中的交易所的所有策略
  const currentStrategies = useMemo(() => {
    if (!exchanges || exchanges.length === 0) return []
    
    if (selectedExchangeIndex === 0) {
      // "全部" 视图：汇总所有交易所的所有策略
      return exchanges
        .filter(ex => ex && ex.assets)
        .flatMap(ex => ex.assets
          .filter(asset => asset && asset.strategies)
          .flatMap(asset => asset.strategies)
        )
        .filter(s => s !== null && s !== undefined)
    }
    const ex = exchanges[selectedExchangeIndex - 1]
    return (ex && ex.assets)
      ? ex.assets
          .filter(asset => asset && asset.strategies)
          .flatMap(asset => asset.strategies)
          .filter(s => s !== null && s !== undefined)
      : []
  }, [exchanges, selectedExchangeIndex])

  // 获取当前视图的总权益
  const currentTotalBalance = useMemo(() => {
    if (selectedExchangeIndex === 0) return overview?.totalBalance || 0
    const ex = exchanges[selectedExchangeIndex - 1]
    if (!ex) return 0
    const exId = ex.exchangeId
    const summary = overview?.exchanges?.filter(e => e !== null && e !== undefined).find(e => e.exchangeId === exId)
    return summary?.totalBalance || 0
  }, [overview, exchanges, selectedExchangeIndex])

  const handleAllocationChange = (strategyId: string, value: number, isPercentage: boolean) => {
    let actualValue = value
    if (isPercentage && currentTotalBalance > 0) {
      actualValue = (value / 100) * currentTotalBalance
    }
    setPendingChanges((prev) => ({
      ...prev,
      [strategyId]: actualValue,
    }))
  }

  const hasPendingChanges = Object.keys(pendingChanges).length > 0

  const handleSaveChanges = async () => {
    if (!hasPendingChanges) return

    setSaving(true)
    try {
      const allocations: CapitalAllocationConfig[] = Object.entries(pendingChanges).map(
        ([strategyId, maxCapital]) => {
          const existing = currentStrategies.find((s) => s.strategyId === strategyId)
          return {
            strategyId,
            maxCapital,
            maxPercentage: existing?.maxPercentage || 100,
            reserveRatio: existing?.reserveRatio || 0.1,
            autoRebalance: existing?.autoRebalance || false,
            priority: existing?.priority || 1,
          }
        }
      )

      const res = await updateCapitalAllocation({ allocations })
      if (res.success) {
        setPendingChanges({})
        fetchData()
        toast({
          title: t('capitalManagement.saveSuccess'),
          status: 'success',
          duration: 3000,
        })
      }
    } catch (err) {
      toast({
        title: '保存失败',
        status: 'error',
        duration: 3000,
      })
    } finally {
      setSaving(false)
    }
  }

  const handleRebalanceComplete = () => {
    fetchData()
  }

  if (loading) {
    return (
      <Center py={12}>
        <Spinner size="xl" thickness="4px" color="blue.500" />
      </Center>
    )
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
              <Heading size="lg">{t('capitalManagement.title')}</Heading>
              <Text color="gray.500">{t('capitalManagement.subtitle')}</Text>
            </VStack>
            <HStack spacing={3}>
              <RebalanceButton onRebalanceComplete={handleRebalanceComplete} />
              {hasPendingChanges && (
                <Button
                  colorScheme="blue"
                  onClick={handleSaveChanges}
                  isLoading={saving}
                >
                  {t('capitalManagement.saveChanges')}
                </Button>
              )}
            </HStack>
          </Flex>
        </MotionBox>

        {/* Testnet Warning */}
        {overview?.exchanges?.some(e => e.isTestnet) && (
          <Alert status="warning" borderRadius="lg" mb={4}>
            <AlertIcon />
            <Box flex="1">
              <Text fontWeight="bold">⚠️ 测试网模式</Text>
              <Text fontSize="sm">
                当前正在使用测试网环境，显示的资产为虚拟测试币，不会产生真实交易。
                {overview.exchanges.filter(e => e.isTestnet).map(e => e.exchangeName).join('、')} 正在使用测试网。
              </Text>
            </Box>
          </Alert>
        )}

        {/* Overview Stats */}
        {overview && (
          <SimpleGrid columns={{ base: 2, md: 4 }} spacing={4}>
            <Box p={4} bg={bgColor} borderRadius="lg" borderWidth="1px" borderColor={borderColor}>
              <Stat>
                <StatLabel>
                  <HStack spacing={2}>
                    <Text>{t('capitalManagement.totalBalance')}</Text>
                    {overview.exchanges?.some(e => e.isTestnet) && (
                      <Badge colorScheme="orange" fontSize="xs">测试网</Badge>
                    )}
                  </HStack>
                </StatLabel>
                <StatNumber>{overview.totalBalance.toLocaleString(undefined, { minimumFractionDigits: 2 })}</StatNumber>
                <StatHelpText>USDT</StatHelpText>
              </Stat>
            </Box>
            <Box p={4} bg={bgColor} borderRadius="lg" borderWidth="1px" borderColor={borderColor}>
              <Stat>
                <StatLabel>{t('capitalManagement.allocated')}</StatLabel>
                <StatNumber color="blue.500">{overview.allocatedCapital.toLocaleString(undefined, { minimumFractionDigits: 2 })}</StatNumber>
                <StatHelpText>
                  {overview.totalBalance > 0 
                    ? ((overview.allocatedCapital / overview.totalBalance) * 100).toFixed(1) 
                    : 0}%
                </StatHelpText>
              </Stat>
            </Box>
            <Box p={4} bg={bgColor} borderRadius="lg" borderWidth="1px" borderColor={borderColor}>
              <Stat>
                <StatLabel>{t('capitalManagement.inUse')}</StatLabel>
                <StatNumber color="orange.500">{overview.usedCapital.toLocaleString(undefined, { minimumFractionDigits: 2 })}</StatNumber>
                <StatHelpText>
                  {overview.totalBalance > 0 
                    ? ((overview.usedCapital / overview.totalBalance) * 100).toFixed(1) 
                    : 0}%
                </StatHelpText>
              </Stat>
            </Box>
            <Box p={4} bg={bgColor} borderRadius="lg" borderWidth="1px" borderColor={borderColor}>
              <Stat>
                <StatLabel>{t('capitalManagement.unrealizedPnL')}</StatLabel>
                <StatNumber color={overview.unrealizedPnL >= 0 ? 'green.500' : 'red.500'}>
                  {overview.unrealizedPnL >= 0 ? '+' : ''}{overview.unrealizedPnL.toLocaleString(undefined, { minimumFractionDigits: 2 })}
                </StatNumber>
                <StatHelpText>USDT</StatHelpText>
              </Stat>
            </Box>
          </SimpleGrid>
        )}

        {/* Exchange Selector Tabs */}
        <Tabs 
          variant="soft-rounded" 
          colorScheme="blue" 
          onChange={(index) => setSelectedExchangeIndex(index)}
          index={selectedExchangeIndex}
        >
          <TabList mb={4} overflowX="auto" pb={2}>
            <Tab px={6}>{t('capitalManagement.allExchanges') || '全局概览'}</Tab>
            {exchanges.filter(ex => ex !== null && ex !== undefined).map((ex) => {
              const exchangeSummary = overview?.exchanges?.filter(e => e !== null && e !== undefined).find(e => e.exchangeId === ex.exchangeId)
              return (
                <Tab key={ex.exchangeId} px={6}>
                  {ex.exchangeName}
                  {exchangeSummary?.isTestnet && (
                    <Badge ml={2} colorScheme="orange" fontSize="xs">测试网</Badge>
                  )}
                  {exchangeSummary?.status === 'error' && (
                    <Badge ml={2} colorScheme="red">ERROR</Badge>
                  )}
                </Tab>
              )
            })}
          </TabList>

          {/* Allocation Chart */}
          <AllocationChart
            strategies={currentStrategies}
            totalCapital={currentTotalBalance}
          />

          {/* Strategy Allocation Controls */}
          <Box mt={6} p={6} bg={bgColor} borderRadius="xl" borderWidth="1px" borderColor={borderColor}>
            <VStack align="stretch" spacing={4}>
              <Flex justify="space-between" align="center">
                <Heading size="md">
                  {selectedExchangeIndex === 0 ? '全部策略分配' : `${exchanges[selectedExchangeIndex-1]?.exchangeName || '未知交易所'} 策略分配`}
                </Heading>
                <FormControl display="flex" alignItems="center" w="auto">
                  <FormLabel mb={0} fontSize="sm">
                    {t('capitalManagement.percentageMode')}
                  </FormLabel>
                  <Switch
                    isChecked={isPercentageMode}
                    onChange={(e) => setIsPercentageMode(e.target.checked)}
                  />
                </FormControl>
              </Flex>

              {hasPendingChanges && (
                <Alert status="info" borderRadius="md">
                  <AlertIcon />
                  <Text fontSize="sm">{t('capitalManagement.unsavedChanges')}</Text>
                </Alert>
              )}

              <VStack align="stretch" spacing={3}>
                {currentStrategies && currentStrategies.length > 0 ? (
                  currentStrategies
                    .filter(s => s !== null && s !== undefined)
                    .map((strategy) => (
                    <CapitalSlider
                      key={`${strategy.exchangeId || 'unknown'}-${strategy.strategyId}`}
                      strategyId={strategy.strategyId}
                      strategyName={strategy.strategyName}
                      currentValue={pendingChanges[strategy.strategyId] ?? strategy.allocated}
                      maxValue={strategy.maxCapital || (currentTotalBalance * 2)}
                      totalCapital={currentTotalBalance}
                      percentage={
                        currentTotalBalance > 0
                          ? ((pendingChanges[strategy.strategyId] ?? strategy.allocated) /
                              currentTotalBalance) *
                            100
                          : 0
                      }
                      onChange={handleAllocationChange}
                      isPercentageMode={isPercentageMode}
                      onModeChange={setIsPercentageMode}
                      disabled={strategy.status === 'error'}
                    />
                  ))
                ) : (
                  <Center py={8} flexDirection="column">
                    <InfoIcon boxSize={8} color="gray.300" mb={2} />
                    <Text color="gray.500">该交易所暂无运行中的策略</Text>
                  </Center>
                )}
              </VStack>
            </VStack>
          </Box>
        </Tabs>

        {/* Quick Actions */}
        <HStack spacing={4} justify="flex-end">
          <Button
            variant="outline"
            leftIcon={<SettingsIcon />}
            onClick={() => navigate('/strategy-market')}
          >
            {t('capitalManagement.manageStrategies')}
          </Button>
          <Button
            variant="outline"
            leftIcon={<RepeatIcon />}
            onClick={() => navigate('/profit-management')}
          >
            {t('capitalManagement.viewProfits')}
          </Button>
        </HStack>
      </VStack>
    </Box>
  )
}

export default CapitalManagement
