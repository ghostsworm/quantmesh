import React, { useState, useEffect } from 'react'
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
  StatArrow,
  Table,
  Thead,
  Tbody,
  Tr,
  Th,
  Td,
  Badge,
  useDisclosure,
  useToast,
  Spinner,
  Center,
  Flex,
  Icon,
  Tabs,
  TabList,
  Tab,
  TabPanels,
  TabPanel,
  useColorModeValue,
} from '@chakra-ui/react'
import { DownloadIcon, TimeIcon, CheckCircleIcon, WarningIcon } from '@chakra-ui/icons'
import { motion } from 'framer-motion'
import { useTranslation } from 'react-i18next'
import { ProfitChart, WithdrawDialog, WithdrawRuleForm } from './profit'
import {
  getProfitSummary,
  getStrategyProfits,
  getProfitTrend,
  getWithdrawRules,
  getWithdrawHistory,
  updateWithdrawRules,
} from '../services/profit'
import type {
  ProfitSummary,
  StrategyProfit,
  ProfitTrendItem,
  ProfitWithdrawRule,
  WithdrawRecord,
} from '../types/profit'

const MotionBox = motion(Box)

// Mock data for development
const MOCK_SUMMARY: ProfitSummary = {
  totalProfit: 1234.56,
  todayProfit: 45.67,
  weekProfit: 234.56,
  monthProfit: 890.12,
  unrealizedProfit: 123.45,
  withdrawnProfit: 500,
  availableToWithdraw: 734.56,
  lastUpdated: new Date().toISOString(),
}

const MOCK_STRATEGY_PROFITS: StrategyProfit[] = [
  {
    strategyId: 'grid',
    strategyName: '网格交易',
    strategyType: 'grid',
    totalProfit: 678.90,
    todayProfit: 23.45,
    unrealizedProfit: 56.78,
    realizedProfit: 622.12,
    withdrawnProfit: 300,
    availableToWithdraw: 322.12,
    tradeCount: 156,
    winRate: 0.78,
    avgProfitPerTrade: 4.35,
    lastTradeAt: new Date().toISOString(),
  },
  {
    strategyId: 'dca_enhanced',
    strategyName: '增强型 DCA',
    strategyType: 'dca',
    totalProfit: 345.67,
    todayProfit: 12.34,
    unrealizedProfit: 45.67,
    realizedProfit: 300,
    withdrawnProfit: 100,
    availableToWithdraw: 200,
    tradeCount: 89,
    winRate: 0.82,
    avgProfitPerTrade: 3.88,
    lastTradeAt: new Date().toISOString(),
  },
  {
    strategyId: 'trend_following',
    strategyName: '趋势跟踪',
    strategyType: 'trend',
    totalProfit: 209.99,
    todayProfit: 9.88,
    unrealizedProfit: 21.00,
    realizedProfit: 188.99,
    withdrawnProfit: 100,
    availableToWithdraw: 88.99,
    tradeCount: 45,
    winRate: 0.65,
    avgProfitPerTrade: 4.67,
    lastTradeAt: new Date().toISOString(),
  },
]

const MOCK_TREND: ProfitTrendItem[] = Array.from({ length: 30 }, (_, i) => {
  const date = new Date()
  date.setDate(date.getDate() - (29 - i))
  const profit = Math.random() * 60 - 20
  return {
    date: date.toISOString().split('T')[0],
    profit,
    cumulativeProfit: MOCK_STRATEGY_PROFITS.reduce((sum, s) => sum + s.totalProfit, 0) * (i / 30),
  }
})

const MOCK_WITHDRAW_HISTORY: WithdrawRecord[] = [
  {
    id: '1',
    strategyId: 'grid',
    strategyName: '网格交易',
    amount: 200,
    fee: 1,
    netAmount: 199,
    type: 'auto',
    status: 'completed',
    destination: 'account',
    createdAt: new Date(Date.now() - 86400000 * 2).toISOString(),
    completedAt: new Date(Date.now() - 86400000 * 2).toISOString(),
  },
  {
    id: '2',
    strategyId: 'dca_enhanced',
    strategyName: '增强型 DCA',
    amount: 100,
    fee: 0.5,
    netAmount: 99.5,
    type: 'manual',
    status: 'completed',
    destination: 'account',
    createdAt: new Date(Date.now() - 86400000 * 5).toISOString(),
    completedAt: new Date(Date.now() - 86400000 * 5).toISOString(),
  },
]

const ProfitManagement: React.FC = () => {
  const { t } = useTranslation()
  const toast = useToast()
  const { isOpen, onOpen, onClose } = useDisclosure()

  const [summary, setSummary] = useState<ProfitSummary | null>(null)
  const [strategyProfits, setStrategyProfits] = useState<StrategyProfit[]>([])
  const [trend, setTrend] = useState<ProfitTrendItem[]>([])
  const [withdrawRules, setWithdrawRules] = useState<ProfitWithdrawRule[]>([])
  const [withdrawHistory, setWithdrawHistory] = useState<WithdrawRecord[]>([])
  const [loading, setLoading] = useState(true)
  const [period, setPeriod] = useState<'7d' | '30d' | '90d' | '1y'>('30d')
  const [activeExchange, setActiveExchange] = useState<string>('all')

  const bgColor = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue('gray.200', 'gray.600')

  useEffect(() => {
    fetchData()
  }, [activeExchange])

  useEffect(() => {
    fetchTrend()
  }, [period, activeExchange])

  const fetchData = async () => {
    setLoading(true)
    const exchangeId = activeExchange === 'all' ? undefined : activeExchange
    try {
      const [summaryRes, profitsRes, rulesRes, historyRes] = await Promise.all([
        getProfitSummary(exchangeId),
        getStrategyProfits(exchangeId),
        getWithdrawRules(),
        getWithdrawHistory({ exchangeId, limit: 10 }),
      ])
      setSummary(summaryRes.summary)
      setStrategyProfits(profitsRes.profits)
      setWithdrawRules(rulesRes.rules)
      setWithdrawHistory(historyRes.records)
    } catch (err) {
      // Use mock data for development
      console.warn('Using mock data:', err)
      setSummary(MOCK_SUMMARY)
      setStrategyProfits(MOCK_STRATEGY_PROFITS)
      setTrend(MOCK_TREND)
      setWithdrawHistory(MOCK_WITHDRAW_HISTORY)
    } finally {
      setLoading(false)
    }
  }

  const fetchTrend = async () => {
    const exchangeId = activeExchange === 'all' ? undefined : activeExchange
    try {
      const res = await getProfitTrend(period, exchangeId)
      setTrend(res.trend)
    } catch (err) {
      // Keep mock data
      console.warn('Using mock trend data:', err)
    }
  }

  const handleSaveRules = async (rules: ProfitWithdrawRule[]) => {
    await updateWithdrawRules({ rules })
    setWithdrawRules(rules)
  }

  const handleWithdrawComplete = () => {
    fetchData()
  }

  const getStatusBadge = (status: string) => {
    switch (status) {
      case 'completed':
        return <Badge colorScheme="green">{t('profitManagement.statusCompleted')}</Badge>
      case 'pending':
        return <Badge colorScheme="yellow">{t('profitManagement.statusPending')}</Badge>
      case 'processing':
        return <Badge colorScheme="blue">{t('profitManagement.statusProcessing')}</Badge>
      case 'failed':
        return <Badge colorScheme="red">{t('profitManagement.statusFailed')}</Badge>
      default:
        return <Badge>{status}</Badge>
    }
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
        {/* Exchange Switcher Tabs */}
        <Box
          bg={bgColor}
          p={1}
          borderRadius="xl"
          borderWidth="1px"
          borderColor={borderColor}
          display="inline-flex"
          alignSelf="flex-start"
        >
          <Tabs
            variant="soft-rounded"
            colorScheme="blue"
            size="sm"
            index={activeExchange === 'all' ? 0 : activeExchange === 'binance' ? 1 : 2}
            onChange={(index) => {
              const exchanges = ['all', 'binance', 'gate']
              setActiveExchange(exchanges[index])
            }}
          >
            <TabList>
              <Tab px={6}>{t('common.allExchanges') || '全部交易所'}</Tab>
              <Tab px={6}>Binance</Tab>
              <Tab px={6}>Gate.io</Tab>
            </TabList>
          </Tabs>
        </Box>

        {/* Header */}
        <MotionBox
          initial={{ opacity: 0, y: -20 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.5 }}
        >
          <Flex justify="space-between" align="center" wrap="wrap" gap={4}>
            <VStack align="start" spacing={1}>
              <Heading size="lg">{t('profitManagement.title')}</Heading>
              <Text color="gray.500">{t('profitManagement.subtitle')}</Text>
            </VStack>
            <Button
              colorScheme="blue"
              leftIcon={<DownloadIcon />}
              onClick={onOpen}
              isDisabled={!summary || summary.availableToWithdraw <= 0}
            >
              {t('profitManagement.withdraw')}
            </Button>
          </Flex>
        </MotionBox>

        {/* Summary Stats */}
        {summary && (
          <SimpleGrid columns={{ base: 2, md: 4 }} spacing={4}>
            <Box p={4} bg={bgColor} borderRadius="lg" borderWidth="1px" borderColor={borderColor}>
              <Stat>
                <StatLabel>{t('profitManagement.totalProfit')}</StatLabel>
                <StatNumber color={(summary.totalProfit || 0) >= 0 ? 'green.500' : 'red.500'}>
                  {(summary.totalProfit || 0) >= 0 ? '+' : ''}{(summary.totalProfit || 0).toFixed(2)}
                </StatNumber>
                <StatHelpText>USDT</StatHelpText>
              </Stat>
            </Box>
            <Box p={4} bg={bgColor} borderRadius="lg" borderWidth="1px" borderColor={borderColor}>
              <Stat>
                <StatLabel>{t('profitManagement.todayProfit')}</StatLabel>
                <StatNumber color={(summary.todayProfit || 0) >= 0 ? 'green.500' : 'red.500'}>
                  {(summary.todayProfit || 0) >= 0 ? '+' : ''}{(summary.todayProfit || 0).toFixed(2)}
                </StatNumber>
                <StatHelpText>
                  <StatArrow type={(summary.todayProfit || 0) >= 0 ? 'increase' : 'decrease'} />
                  {t('profitManagement.today')}
                </StatHelpText>
              </Stat>
            </Box>
            <Box p={4} bg={bgColor} borderRadius="lg" borderWidth="1px" borderColor={borderColor}>
              <Stat>
                <StatLabel>{t('profitManagement.unrealizedProfit')}</StatLabel>
                <StatNumber color="orange.500">
                  {(summary.unrealizedProfit || 0) >= 0 ? '+' : ''}{(summary.unrealizedProfit || 0).toFixed(2)}
                </StatNumber>
                <StatHelpText>USDT</StatHelpText>
              </Stat>
            </Box>
            <Box p={4} bg={bgColor} borderRadius="lg" borderWidth="1px" borderColor={borderColor}>
              <Stat>
                <StatLabel>{t('profitManagement.availableToWithdraw')}</StatLabel>
                <StatNumber color="blue.500">{(summary.availableToWithdraw || 0).toFixed(2)}</StatNumber>
                <StatHelpText>USDT</StatHelpText>
              </Stat>
            </Box>
          </SimpleGrid>
        )}

        {/* Profit Chart */}
        <ProfitChart
          trend={trend}
          strategyProfits={strategyProfits}
          period={period}
          onPeriodChange={setPeriod}
        />

        {/* Tabs for Rules and History */}
        <Tabs variant="enclosed" colorScheme="blue">
          <TabList>
            <Tab>{t('profitManagement.autoWithdrawRules')}</Tab>
            <Tab>{t('profitManagement.withdrawHistory')}</Tab>
          </TabList>

          <TabPanels>
            {/* Auto Withdraw Rules */}
            <TabPanel p={0} pt={4}>
              <WithdrawRuleForm
                rules={withdrawRules}
                strategyOptions={strategyProfits.map((s) => ({
                  id: s.strategyId,
                  name: s.strategyName,
                }))}
                onSave={handleSaveRules}
              />
            </TabPanel>

            {/* Withdraw History */}
            <TabPanel p={0} pt={4}>
              <Box
                p={6}
                bg={bgColor}
                borderRadius="xl"
                borderWidth="1px"
                borderColor={borderColor}
              >
                <Table variant="simple" size="sm">
                  <Thead>
                    <Tr>
                      <Th>{t('profitManagement.date')}</Th>
                      <Th>{t('profitManagement.strategy')}</Th>
                      <Th isNumeric>{t('profitManagement.amount')}</Th>
                      <Th isNumeric>{t('profitManagement.fee')}</Th>
                      <Th isNumeric>{t('profitManagement.netAmount')}</Th>
                      <Th>{t('profitManagement.type')}</Th>
                      <Th>{t('profitManagement.status')}</Th>
                    </Tr>
                  </Thead>
                  <Tbody>
                    {withdrawHistory.length === 0 ? (
                      <Tr>
                        <Td colSpan={7} textAlign="center" py={8} color="gray.500">
                          {t('profitManagement.noHistory')}
                        </Td>
                      </Tr>
                    ) : (
                      withdrawHistory.map((record) => (
                        <Tr key={record.id}>
                          <Td>
                            <HStack spacing={1}>
                              <Icon as={TimeIcon} color="gray.400" boxSize={3} />
                              <Text fontSize="sm">
                                {new Date(record.createdAt).toLocaleDateString()}
                              </Text>
                            </HStack>
                          </Td>
                          <Td>{record.strategyName}</Td>
                          <Td isNumeric fontWeight="medium">
                            {(record.amount || 0).toFixed(2)} USDT
                          </Td>
                          <Td isNumeric color="orange.500">
                            -{(record.fee || 0).toFixed(2)}
                          </Td>
                          <Td isNumeric fontWeight="bold" color="green.500">
                            {(record.netAmount || 0).toFixed(2)} USDT
                          </Td>
                          <Td>
                            <Badge
                              colorScheme={record.type === 'auto' ? 'purple' : 'blue'}
                              fontSize="xs"
                            >
                              {record.type === 'auto'
                                ? t('profitManagement.typeAuto')
                                : t('profitManagement.typeManual')}
                            </Badge>
                          </Td>
                          <Td>{getStatusBadge(record.status)}</Td>
                        </Tr>
                      ))
                    )}
                  </Tbody>
                </Table>
              </Box>
            </TabPanel>
          </TabPanels>
        </Tabs>
      </VStack>

      {/* Withdraw Dialog */}
      {summary && (
        <WithdrawDialog
          isOpen={isOpen}
          onClose={onClose}
          strategyProfits={strategyProfits}
          availableToWithdraw={summary.availableToWithdraw}
          onWithdrawComplete={handleWithdrawComplete}
        />
      )}
    </Box>
  )
}

export default ProfitManagement
