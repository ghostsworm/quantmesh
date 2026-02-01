import React, { useEffect, useState, useMemo } from 'react'
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
  AlertDescription,
  useToast,
  Spinner,
  Center,
  Flex,
  Switch,
  FormControl,
  FormLabel,
  useColorModeValue,
  Tabs,
  TabList,
  TabPanels,
  TabPanel,
  Tab,
  Badge,
  Slider,
  SliderTrack,
  SliderFilledTrack,
  SliderThumb,
  Tooltip,
  Input,
  InputGroup,
  InputRightAddon,
  Divider,
  Icon,
} from '@chakra-ui/react'
import { InfoIcon, RepeatIcon, CheckIcon, ViewIcon, SettingsIcon } from '@chakra-ui/icons'
import { motion } from 'framer-motion'
import { useTranslation } from 'react-i18next'
import {
  getCapitalOverview,
  getCapitalAllocation,
  updateCapitalAllocation,
  rebalanceCapital,
} from '../services/capital'
import type {
  CapitalOverview,
  StrategyCapitalInfo,
  CapitalAllocationConfig,
  ExchangeCapitalDetail,
} from '../types/capital'
import StrategyRuntimeStatusPanel from './StrategyRuntimeStatus'

const MotionBox = motion(Box)

// 策略权重滑塊组件
interface StrategyWeightSliderProps {
  strategy: StrategyCapitalInfo
  totalCapital: number
  pendingWeight: number | undefined
  onChange: (strategyId: string, weight: number) => void
  disabled?: boolean
  isPercentageMode: boolean
}

const StrategyWeightSlider: React.FC<StrategyWeightSliderProps> = ({
  strategy,
  totalCapital,
  pendingWeight,
  onChange,
  disabled = false,
  isPercentageMode,
}) => {
  const { t } = useTranslation()
  const [showTooltip, setShowTooltip] = useState(false)
  const bgColor = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue('gray.200', 'gray.600')
  
  // 使用 pendingWeight 或原始权重
  const currentWeight = pendingWeight !== undefined ? pendingWeight : strategy.weight
  const currentPercentage = currentWeight * 100
  const allocatedAmount = totalCapital * currentWeight

  const handleSliderChange = (value: number) => {
    onChange(strategy.strategyId, value / 100)
  }

  const handleInputChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const value = parseFloat(e.target.value)
    if (!isNaN(value)) {
      const clampedValue = Math.min(Math.max(value, 0), 100)
      onChange(strategy.strategyId, clampedValue / 100)
    }
  }

  const getStrategyTypeLabel = (type: string) => {
    return t('strategyNames.' + type, { defaultValue: type })
  }

  const getStatusColor = (status: string) => {
    switch (status) {
      case 'active':
        return 'green'
      case 'paused':
        return 'yellow'
      case 'error':
        return 'red'
      default:
        return 'gray'
    }
  }

  return (
    <Box
      p={4}
      bg={bgColor}
      borderWidth="1px"
      borderColor={borderColor}
      borderRadius="lg"
      opacity={disabled ? 0.6 : 1}
    >
      <VStack align="stretch" spacing={3}>
        <HStack justify="space-between">
          <VStack align="start" spacing={0}>
            <HStack spacing={2}>
              <Text fontWeight="bold" fontSize="md">
                {t('strategyNames.' + (strategy.strategyId || ''), { defaultValue: strategy.strategyName })}
              </Text>
              <Badge colorScheme={getStatusColor(strategy.status)} fontSize="xs">
                {strategy.status === 'active' ? t('dashboard.running') : strategy.status === 'paused' ? t('dashboard.stopped') : t('common.error')}
              </Badge>
            </HStack>
            <Text fontSize="xs" color="gray.500">
              {getStrategyTypeLabel(strategy.strategyType)}
              {strategy.exchangeId && ` · ${strategy.exchangeId}`}
            </Text>
          </VStack>
          <VStack align="end" spacing={0}>
            <Text fontWeight="bold" color="blue.500" fontSize="lg">
              {currentPercentage.toFixed(1)}%
            </Text>
            <Text fontSize="xs" color="gray.500">
              ≈ {allocatedAmount.toFixed(2)} USDT
            </Text>
          </VStack>
        </HStack>

        <Slider
          value={currentPercentage}
          min={0}
          max={100}
          step={0.1}
          onChange={handleSliderChange}
          onMouseEnter={() => setShowTooltip(true)}
          onMouseLeave={() => setShowTooltip(false)}
          isDisabled={disabled}
        >
          <SliderTrack bg="gray.200">
            <SliderFilledTrack bg="blue.500" />
          </SliderTrack>
          <Tooltip
            hasArrow
            bg="blue.500"
            color="white"
            placement="top"
            isOpen={showTooltip && !disabled}
            label={`${currentPercentage.toFixed(1)}% (${allocatedAmount.toFixed(2)} USDT)`}
          >
            <SliderThumb boxSize={4} />
          </Tooltip>
        </Slider>

        <HStack justify="space-between">
          <InputGroup size="sm" maxW="120px">
            <Input
              type="number"
              value={currentPercentage.toFixed(1)}
              onChange={handleInputChange}
              isDisabled={disabled}
              textAlign="right"
              step={0.1}
              min={0}
              max={100}
            />
            <InputRightAddon>%</InputRightAddon>
          </InputGroup>
          <HStack spacing={4} fontSize="xs" color="gray.500">
            <Text>已使用: {strategy.used.toFixed(2)} USDT</Text>
            <Text>使用率: {strategy.utilizationRate.toFixed(1)}%</Text>
          </HStack>
        </HStack>
      </VStack>
    </Box>
  )
}

const StrategyAllocation: React.FC = () => {
  const { t } = useTranslation()
  const toast = useToast()

  const [overview, setOverview] = useState<CapitalOverview | null>(null)
  const [exchanges, setExchanges] = useState<ExchangeCapitalDetail[]>([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [rebalancing, setRebalancing] = useState(false)
  const [isPercentageMode, setIsPercentageMode] = useState(true)
  const [pendingChanges, setPendingChanges] = useState<Record<string, number>>({})
  const [selectedExchangeIndex, setSelectedExchangeIndex] = useState(0)
  const [mainTabIndex, setMainTabIndex] = useState(0) // 0: 策略配比, 1: 運行狀態

  const bgColor = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue('gray.200', 'gray.600')
  const infoBgColor = useColorModeValue('blue.50', 'blue.900')

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
      console.error('Failed to fetch data:', err)
      toast({
        title: t('capitalManagement.loadFailed'),
        description: t('capitalManagement.loadFailedDesc'),
        status: 'error',
        duration: 5000,
      })
    } finally {
      setLoading(false)
    }
  }

  // 獲取當前选中的交易所的所有策略
  const currentStrategies = useMemo(() => {
    if (!exchanges || exchanges.length === 0) return []

    if (selectedExchangeIndex === 0) {
      // "全部" 視图：彙總所有交易所的所有策略
      return exchanges
        .filter((ex) => ex && ex.assets)
        .flatMap((ex) =>
          ex.assets
            .filter((asset) => asset && asset.strategies)
            .flatMap((asset) => asset.strategies)
        )
        .filter((s) => s !== null && s !== undefined)
    }
    const ex = exchanges[selectedExchangeIndex - 1]
    return ex && ex.assets
      ? ex.assets
          .filter((asset) => asset && asset.strategies)
          .flatMap((asset) => asset.strategies)
          .filter((s) => s !== null && s !== undefined)
      : []
  }, [exchanges, selectedExchangeIndex])

  // 獲取當前視图的總权益
  const currentTotalBalance = useMemo(() => {
    if (selectedExchangeIndex === 0) return overview?.totalBalance || 0
    const ex = exchanges[selectedExchangeIndex - 1]
    if (!ex) return 0
    const exId = ex.exchangeId
    const summary = overview?.exchanges
      ?.filter((e) => e !== null && e !== undefined)
      .find((e) => e.exchangeId === exId)
    return summary?.totalBalance || 0
  }, [overview, exchanges, selectedExchangeIndex])

  // 计算總权重
  const totalWeight = useMemo(() => {
    return currentStrategies.reduce((sum, s) => {
      const weight = pendingChanges[s.strategyId] !== undefined 
        ? pendingChanges[s.strategyId] 
        : s.weight
      return sum + weight
    }, 0)
  }, [currentStrategies, pendingChanges])

  const handleWeightChange = (strategyId: string, weight: number) => {
    // 只有在选中具体交易所時才能調整
    if (selectedExchangeIndex === 0) {
      return
    }
    setPendingChanges((prev) => ({
      ...prev,
      [strategyId]: weight,
    }))
  }

  const hasPendingChanges = Object.keys(pendingChanges).length > 0

  const handleSaveChanges = async () => {
    if (selectedExchangeIndex === 0) {
      toast({
        title: t('capitalManagement.selectExchangeFirst'),
        description: t('capitalManagement.selectExchangeFirstDesc'),
        status: 'warning',
        duration: 5000,
      })
      return
    }

    if (!hasPendingChanges) return

    setSaving(true)
    try {
      const allocations: CapitalAllocationConfig[] = Object.entries(pendingChanges).map(
        ([strategyId, weight]) => {
          const existing = currentStrategies.find((s) => s.strategyId === strategyId)
          const maxCapital = currentTotalBalance * weight
          const maxPercentage = weight * 100

          return {
            strategyId,
            maxCapital: Math.max(0, maxCapital),
            maxPercentage: Math.min(100, Math.max(0, maxPercentage)),
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
          description: t('capitalManagement.saveSuccessDesc'),
          status: 'success',
          duration: 3000,
        })
      }
    } catch (err) {
      toast({
        title: t('capitalManagement.saveFailed'),
        description: t('capitalManagement.saveFailedDesc'),
        status: 'error',
        duration: 3000,
      })
    } finally {
      setSaving(false)
    }
  }

  const handleRebalance = async () => {
    setRebalancing(true)
    try {
      const result = await rebalanceCapital({ mode: 'weighted', dryRun: false })
      if (result.success) {
        fetchData()
        toast({
          title: t('capitalManagement.rebalanceSuccess'),
          description: result.message || t('capitalManagement.rebalanceSuccessDesc'),
          status: 'success',
          duration: 3000,
        })
      }
    } catch (err) {
      toast({
        title: t('capitalManagement.rebalanceError'),
        status: 'error',
        duration: 3000,
      })
    } finally {
      setRebalancing(false)
    }
  }

  const handleCancelChanges = () => {
    setPendingChanges({})
  }

  if (loading) {
    return (
      <Center py={12}>
        <Spinner size="xl" thickness="4px" color="blue.500" />
      </Center>
    )
  }

  // 獲取當前選中交易所的信息
  const currentExchangeInfo = useMemo(() => {
    if (selectedExchangeIndex === 0 || !exchanges || exchanges.length === 0) {
      return { exchange: '', symbol: '' }
    }
    const ex = exchanges[selectedExchangeIndex - 1]
    const symbol = ex?.assets?.[0]?.asset || ''
    return { exchange: ex?.exchangeId || '', symbol }
  }, [exchanges, selectedExchangeIndex])

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
              <Heading size="lg">{t('capitalManagement.strategyAllocationTitle')}</Heading>
              <Text color="gray.500">{t('capitalManagement.strategyAllocationDesc')}</Text>
            </VStack>
            <HStack spacing={3}>
              {mainTabIndex === 0 && (
                <>
                  <Button
                    variant="outline"
                    leftIcon={<RepeatIcon />}
                    onClick={handleRebalance}
                    isLoading={rebalancing}
                    loadingText={t('capitalManagement.rebalancing')}
                  >
                    {t('capitalManagement.rebalance')}
                  </Button>
                  {hasPendingChanges && (
                    <>
                      <Button variant="ghost" onClick={handleCancelChanges}>
                        {t('common.cancel')}
                      </Button>
                      <Button
                        colorScheme="blue"
                        leftIcon={<CheckIcon />}
                        onClick={handleSaveChanges}
                        isLoading={saving}
                      >
                        保存更改
                      </Button>
                    </>
                  )}
                </>
              )}
            </HStack>
          </Flex>
        </MotionBox>

        {/* 主標籤頁：策略配比 / 運行狀態 */}
        <Tabs
          variant="enclosed"
          colorScheme="blue"
          index={mainTabIndex}
          onChange={setMainTabIndex}
        >
          <TabList>
            <Tab>
              <HStack spacing={2}>
                <Icon as={SettingsIcon} />
                <Text>策略配比</Text>
              </HStack>
            </Tab>
            <Tab>
              <HStack spacing={2}>
                <Icon as={ViewIcon} />
                <Text>運行狀態</Text>
              </HStack>
            </Tab>
          </TabList>

          <TabPanels>
            {/* 策略配比面板 */}
            <TabPanel px={0}>
              <VStack align="stretch" spacing={6}>
                {/* 重要提示：不同交易所/币种使用不同策略比例 */}
                <Alert status="info" borderRadius="lg" bg={infoBgColor}>
          <AlertIcon />
          <Box flex="1">
            <Text fontWeight="bold" fontSize="sm">
              💡 提示：不同交易所/币种可以配置不同的策略比例
            </Text>
            <AlertDescription fontSize="sm" mt={1}>
              每個交易所的策略配比是独立的。请先在上方选擇具体的交易所，然后調整該交易所下各策略的资金分配比例。
              例如：在 Binance 上可以配置 70% 网格 + 30% 马丁格尔，而在 OKX 上可以配置 50% 网格 + 50% DCA。
            </AlertDescription>
          </Box>
        </Alert>

        {/* Overview Stats */}
        {overview && (
          <SimpleGrid columns={{ base: 2, md: 4 }} spacing={4}>
            <Box
              p={4}
              bg={bgColor}
              borderRadius="lg"
              borderWidth="1px"
              borderColor={borderColor}
            >
              <Stat>
                <StatLabel>總分配资金</StatLabel>
                <StatNumber>
                  {(overview.allocatedCapital || 0).toLocaleString(undefined, {
                    minimumFractionDigits: 2,
                  })}
                </StatNumber>
                <StatHelpText>USDT</StatHelpText>
              </Stat>
            </Box>
            <Box
              p={4}
              bg={bgColor}
              borderRadius="lg"
              borderWidth="1px"
              borderColor={borderColor}
            >
              <Stat>
                <StatLabel>已使用</StatLabel>
                <StatNumber color="red.500">
                  {(overview.usedCapital || 0).toLocaleString(undefined, {
                    minimumFractionDigits: 2,
                  })}
                </StatNumber>
                <StatHelpText>USDT</StatHelpText>
              </Stat>
            </Box>
            <Box
              p={4}
              bg={bgColor}
              borderRadius="lg"
              borderWidth="1px"
              borderColor={borderColor}
            >
              <Stat>
                <StatLabel>可用资金</StatLabel>
                <StatNumber color="green.500">
                  {(overview.availableCapital || 0).toLocaleString(undefined, {
                    minimumFractionDigits: 2,
                  })}
                </StatNumber>
                <StatHelpText>USDT</StatHelpText>
              </Stat>
            </Box>
            <Box
              p={4}
              bg={bgColor}
              borderRadius="lg"
              borderWidth="1px"
              borderColor={borderColor}
            >
              <Stat>
                <StatLabel>總权重</StatLabel>
                <StatNumber color={Math.abs(totalWeight - 1) < 0.01 ? 'green.500' : 'orange.500'}>
                  {(totalWeight * 100).toFixed(1)}%
                </StatNumber>
                <StatHelpText>
                  {Math.abs(totalWeight - 1) < 0.01 ? `✓ ${t('capitalManagement.balanced')}` : t('capitalManagement.adjustTo100')}
                </StatHelpText>
              </Stat>
            </Box>
          </SimpleGrid>
        )}

        {/* Exchange Selector Tabs */}
        <Tabs
          variant="soft-rounded"
          colorScheme="blue"
          onChange={(index) => {
            setSelectedExchangeIndex(index)
            setPendingChanges({}) // 切换交易所時清空待保存的更改
          }}
          index={selectedExchangeIndex}
        >
          <TabList mb={4} overflowX="auto" pb={2}>
            <Tab px={6}>全部交易所</Tab>
            {exchanges
              .filter((ex) => ex !== null && ex !== undefined)
              .map((ex) => {
                const exchangeSummary = overview?.exchanges
                  ?.filter((e) => e !== null && e !== undefined)
                  .find((e) => e.exchangeId === ex.exchangeId)
                return (
                  <Tab key={ex.exchangeId} px={6}>
                    {ex.exchangeName}
                    {exchangeSummary?.isTestnet && (
                      <Badge ml={2} colorScheme="orange" fontSize="xs">
                        測試網
                      </Badge>
                    )}
                  </Tab>
                )
              })}
          </TabList>
        </Tabs>

        {/* 选中全部時的提示 */}
        {selectedExchangeIndex === 0 && (
          <Alert status="warning" borderRadius="md">
            <AlertIcon />
            <Box flex="1">
              <Text fontWeight="bold" fontSize="sm">
                {t('capitalManagement.viewOnlyLabel')}
              </Text>
              <Text fontSize="xs" mt={1}>
                请选擇具体的交易所（如 BINANCE、OKX 等）来調整該交易所的策略配比。
              </Text>
            </Box>
          </Alert>
        )}

        {/* 策略列表 */}
        <Box
          p={6}
          bg={bgColor}
          borderRadius="xl"
          borderWidth="1px"
          borderColor={borderColor}
        >
          <VStack align="stretch" spacing={4}>
            <Flex justify="space-between" align="center">
              <Heading size="md">
                {selectedExchangeIndex === 0
                  ? t('capitalManagement.allStrategiesOverview')
                  : t('capitalManagement.exchangeStrategies', { exchange: exchanges[selectedExchangeIndex - 1]?.exchangeName || '' })}
              </Heading>
              {selectedExchangeIndex !== 0 && (
                <FormControl display="flex" alignItems="center" w="auto">
                  <FormLabel mb={0} fontSize="sm">
                    百分比模式
                  </FormLabel>
                  <Switch
                    isChecked={isPercentageMode}
                    onChange={(e) => setIsPercentageMode(e.target.checked)}
                  />
                </FormControl>
              )}
            </Flex>

            {hasPendingChanges && (
              <Alert status="info" borderRadius="md">
                <AlertIcon />
                <Text fontSize="sm">{t('capitalManagement.unsavedChangesHint')}</Text>
              </Alert>
            )}

            <Divider />

            <VStack align="stretch" spacing={3}>
              {currentStrategies && currentStrategies.length > 0 ? (
                currentStrategies
                  .filter((s) => s !== null && s !== undefined)
                  .map((strategy, index) => {
                    const uniqueKey = `${strategy.exchangeId || 'unknown'}-${
                      strategy.strategyId || 'unknown'
                    }-${strategy.asset || 'unknown'}-${index}`
                    return (
                      <StrategyWeightSlider
                        key={uniqueKey}
                        strategy={strategy}
                        totalCapital={currentTotalBalance}
                        pendingWeight={pendingChanges[strategy.strategyId]}
                        onChange={handleWeightChange}
                        disabled={selectedExchangeIndex === 0}
                        isPercentageMode={isPercentageMode}
                      />
                    )
                  })
              ) : (
                <Center py={8} flexDirection="column">
                  <Icon as={InfoIcon} boxSize={8} color="gray.300" mb={2} />
                  <Text color="gray.500">
                    {selectedExchangeIndex === 0
                      ? t('capitalManagement.noStrategiesHint')
                      : t('capitalManagement.noStrategiesForExchange')}
                  </Text>
                </Center>
              )}
            </VStack>
          </VStack>
        </Box>

        {/* 资金分配比例饼图（简化版） */}
        {currentStrategies.length > 0 && (
          <Box
            p={6}
            bg={bgColor}
            borderRadius="xl"
            borderWidth="1px"
            borderColor={borderColor}
          >
            <Heading size="md" mb={4}>
              资金分配比例
            </Heading>
            <Flex wrap="wrap" gap={4}>
              {currentStrategies.map((strategy, index) => {
                const weight =
                  pendingChanges[strategy.strategyId] !== undefined
                    ? pendingChanges[strategy.strategyId]
                    : strategy.weight
                const colors = [
                  '#3182CE',
                  '#38A169',
                  '#D69E2E',
                  '#E53E3E',
                  '#805AD5',
                  '#00B5D8',
                ]
                const color = colors[index % colors.length]
                return (
                  <HStack key={strategy.strategyId} spacing={2}>
                    <Box
                      w="16px"
                      h="16px"
                      bg={color}
                      borderRadius="sm"
                    />
                    <Text fontSize="sm">
                      {strategy.strategyName}: {(weight * 100).toFixed(1)}%
                    </Text>
                  </HStack>
                )
              })}
            </Flex>
          </Box>
        )}
              </VStack>
            </TabPanel>

            {/* 運行狀態面板 */}
            <TabPanel px={0}>
              <StrategyRuntimeStatusPanel
                exchange={currentExchangeInfo.exchange}
                symbol={currentExchangeInfo.symbol}
                refreshInterval={10000}
              />
            </TabPanel>
          </TabPanels>
        </Tabs>
      </VStack>
    </Box>
  )
}

export default StrategyAllocation
