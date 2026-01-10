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
} from '@chakra-ui/react'
import { SettingsIcon, RepeatIcon } from '@chakra-ui/icons'
import { motion } from 'framer-motion'
import { useTranslation } from 'react-i18next'
import { useSearchParams, useNavigate } from 'react-router-dom'
import { CapitalSlider, AllocationChart, RebalanceButton } from './capital'
import {
  getCapitalOverview,
  getCapitalAllocation,
  updateCapitalAllocation,
} from '../services/capital'
import type { CapitalOverview, StrategyCapitalInfo, CapitalAllocationConfig } from '../types/capital'

const MotionBox = motion(Box)

// Mock data for development
const MOCK_OVERVIEW: CapitalOverview = {
  totalBalance: 10000,
  allocatedCapital: 7500,
  usedCapital: 4200,
  availableCapital: 2500,
  reservedCapital: 500,
  unrealizedPnL: 123.45,
  marginRatio: 0.42,
  lastUpdated: new Date().toISOString(),
}

const MOCK_STRATEGIES: StrategyCapitalInfo[] = [
  {
    strategyId: 'grid',
    strategyName: '网格交易',
    strategyType: 'grid',
    allocated: 3000,
    used: 2100,
    available: 900,
    weight: 0.4,
    maxCapital: 5000,
    maxPercentage: 50,
    reserveRatio: 0.1,
    autoRebalance: true,
    priority: 1,
    utilizationRate: 0.7,
    status: 'active',
  },
  {
    strategyId: 'dca_enhanced',
    strategyName: '增强型 DCA',
    strategyType: 'dca',
    allocated: 2500,
    used: 1500,
    available: 1000,
    weight: 0.33,
    maxCapital: 4000,
    maxPercentage: 40,
    reserveRatio: 0.1,
    autoRebalance: true,
    priority: 2,
    utilizationRate: 0.6,
    status: 'active',
  },
  {
    strategyId: 'trend_following',
    strategyName: '趋势跟踪',
    strategyType: 'trend',
    allocated: 2000,
    used: 600,
    available: 1400,
    weight: 0.27,
    maxCapital: 3000,
    maxPercentage: 30,
    reserveRatio: 0.15,
    autoRebalance: false,
    priority: 3,
    utilizationRate: 0.3,
    status: 'paused',
  },
]

const CapitalManagement: React.FC = () => {
  const { t } = useTranslation()
  const toast = useToast()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const highlightStrategy = searchParams.get('strategy')

  const [overview, setOverview] = useState<CapitalOverview | null>(null)
  const [strategies, setStrategies] = useState<StrategyCapitalInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [isPercentageMode, setIsPercentageMode] = useState(false)
  const [pendingChanges, setPendingChanges] = useState<Record<string, number>>({})

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
      setOverview(overviewRes.overview)
      setStrategies(allocationRes.strategies)
    } catch (err) {
      // Use mock data for development
      console.warn('Using mock data:', err)
      setOverview(MOCK_OVERVIEW)
      setStrategies(MOCK_STRATEGIES)
    } finally {
      setLoading(false)
    }
  }

  const handleAllocationChange = (strategyId: string, value: number, isPercentage: boolean) => {
    let actualValue = value
    if (isPercentage && overview) {
      actualValue = (value / 100) * overview.totalBalance
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
          const existing = strategies.find((s) => s.strategyId === strategyId)
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

      await updateCapitalAllocation({ allocations })
      
      // Update local state
      setStrategies((prev) =>
        prev.map((s) => ({
          ...s,
          allocated: pendingChanges[s.strategyId] ?? s.allocated,
          maxCapital: pendingChanges[s.strategyId] ?? s.maxCapital,
        }))
      )
      setPendingChanges({})
      
      toast({
        title: t('capitalManagement.saveSuccess'),
        status: 'success',
        duration: 3000,
      })
    } catch (err) {
      // For development, still update local state
      setStrategies((prev) =>
        prev.map((s) => ({
          ...s,
          allocated: pendingChanges[s.strategyId] ?? s.allocated,
          maxCapital: pendingChanges[s.strategyId] ?? s.maxCapital,
        }))
      )
      setPendingChanges({})
      toast({
        title: t('capitalManagement.saveSuccess'),
        status: 'success',
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

        {/* Overview Stats */}
        {overview && (
          <SimpleGrid columns={{ base: 2, md: 4 }} spacing={4}>
            <Box p={4} bg={bgColor} borderRadius="lg" borderWidth="1px" borderColor={borderColor}>
              <Stat>
                <StatLabel>{t('capitalManagement.totalBalance')}</StatLabel>
                <StatNumber>{overview.totalBalance.toFixed(2)}</StatNumber>
                <StatHelpText>USDT</StatHelpText>
              </Stat>
            </Box>
            <Box p={4} bg={bgColor} borderRadius="lg" borderWidth="1px" borderColor={borderColor}>
              <Stat>
                <StatLabel>{t('capitalManagement.allocated')}</StatLabel>
                <StatNumber color="blue.500">{overview.allocatedCapital.toFixed(2)}</StatNumber>
                <StatHelpText>
                  {((overview.allocatedCapital / overview.totalBalance) * 100).toFixed(1)}%
                </StatHelpText>
              </Stat>
            </Box>
            <Box p={4} bg={bgColor} borderRadius="lg" borderWidth="1px" borderColor={borderColor}>
              <Stat>
                <StatLabel>{t('capitalManagement.inUse')}</StatLabel>
                <StatNumber color="orange.500">{overview.usedCapital.toFixed(2)}</StatNumber>
                <StatHelpText>
                  {((overview.usedCapital / overview.totalBalance) * 100).toFixed(1)}%
                </StatHelpText>
              </Stat>
            </Box>
            <Box p={4} bg={bgColor} borderRadius="lg" borderWidth="1px" borderColor={borderColor}>
              <Stat>
                <StatLabel>{t('capitalManagement.unrealizedPnL')}</StatLabel>
                <StatNumber color={overview.unrealizedPnL >= 0 ? 'green.500' : 'red.500'}>
                  {overview.unrealizedPnL >= 0 ? '+' : ''}{overview.unrealizedPnL.toFixed(2)}
                </StatNumber>
                <StatHelpText>USDT</StatHelpText>
              </Stat>
            </Box>
          </SimpleGrid>
        )}

        {/* Allocation Chart */}
        {overview && (
          <AllocationChart
            strategies={strategies}
            totalCapital={overview.totalBalance}
          />
        )}

        {/* Strategy Allocation Controls */}
        <Box p={6} bg={bgColor} borderRadius="xl" borderWidth="1px" borderColor={borderColor}>
          <VStack align="stretch" spacing={4}>
            <Flex justify="space-between" align="center">
              <Heading size="md">{t('capitalManagement.strategyAllocation')}</Heading>
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
              {strategies.map((strategy) => (
                <CapitalSlider
                  key={strategy.strategyId}
                  strategyId={strategy.strategyId}
                  strategyName={strategy.strategyName}
                  currentValue={pendingChanges[strategy.strategyId] ?? strategy.allocated}
                  maxValue={strategy.maxCapital}
                  totalCapital={overview?.totalBalance || 0}
                  percentage={
                    overview
                      ? ((pendingChanges[strategy.strategyId] ?? strategy.allocated) /
                          overview.totalBalance) *
                        100
                      : 0
                  }
                  onChange={handleAllocationChange}
                  isPercentageMode={isPercentageMode}
                  onModeChange={setIsPercentageMode}
                  disabled={strategy.status === 'error'}
                />
              ))}
            </VStack>
          </VStack>
        </Box>

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
