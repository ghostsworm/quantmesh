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
  getDailyKlines,
  getFundingHistory,
} from '../services/profit'
import { getExchanges, getSymbols } from '../services/api'
import type {
  ProfitSummary,
  StrategyProfit,
  ProfitTrendItem,
  ProfitWithdrawRule,
  WithdrawRecord,
  FundingPaymentItem,
  PriceChangeItem,
} from '../types/profit'

const MotionBox = motion(Box)

const ProfitManagement: React.FC = () => {
  const { t } = useTranslation()
  const toast = useToast()
  const { isOpen, onOpen, onClose } = useDisclosure()

  const [summary, setSummary] = useState<ProfitSummary | null>(null)
  const [strategyProfits, setStrategyProfits] = useState<StrategyProfit[]>([])
  const [trend, setTrend] = useState<ProfitTrendItem[]>([])
  const [allWithdrawRules, setAllWithdrawRules] = useState<ProfitWithdrawRule[]>([])
  const [withdrawHistory, setWithdrawHistory] = useState<WithdrawRecord[]>([])
  const [loading, setLoading] = useState(true)
  const [period, setPeriod] = useState<'7d' | '30d' | '90d' | '1y'>('30d')
  const [activeExchange, setActiveExchange] = useState<string>('all')
  const [exchanges, setExchanges] = useState<string[]>([])
  const [selectedExchangeIndex, setSelectedExchangeIndex] = useState(0)
  const [selectedSymbol, setSelectedSymbol] = useState<string | null>(null)
  const [symbols, setSymbols] = useState<Array<{ symbol: string; exchange: string }>>([])
  const [priceData, setPriceData] = useState<PriceChangeItem[]>([])
  const [isLoadingPriceData, setIsLoadingPriceData] = useState(false)
  const [fundingRecords, setFundingRecords] = useState<FundingPaymentItem[]>([])
  const [loadingFunding, setLoadingFunding] = useState(false)
  const [fundingTabIndex, setFundingTabIndex] = useState(0)

  const normalizeExchangeId = (id: string) => (id || '').trim().toLowerCase()

  const visibleWithdrawRules =
    activeExchange === 'all'
      ? allWithdrawRules
      : allWithdrawRules.filter((r) => normalizeExchangeId(r.exchangeId) === normalizeExchangeId(activeExchange))

  const bgColor = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue('gray.200', 'gray.600')

  useEffect(() => {
    // 加載交易所列表
    const loadExchanges = async () => {
      try {
        const res = await getExchanges()
        setExchanges(res.exchanges || [])
      } catch (err) {
        console.error('獲取交易所列表失败:', err)
        // 使用默认列表作為后备
        setExchanges(['binance', 'gate', 'okx'])
      }
    }
    loadExchanges()
  }, [])

  useEffect(() => {
    // 更新选中的交易所索引
    if (activeExchange === 'all') {
      setSelectedExchangeIndex(0)
    } else {
      const index = exchanges.findIndex(ex => ex.toLowerCase() === activeExchange.toLowerCase())
      if (index >= 0) {
        setSelectedExchangeIndex(index + 1)
      }
    }
  }, [activeExchange, exchanges])

  useEffect(() => {
    fetchData()
  }, [activeExchange])

  useEffect(() => {
    const filtered =
      activeExchange === 'all'
        ? symbols
        : symbols.filter((s) => s.exchange.toLowerCase() === activeExchange.toLowerCase())
    if (
      selectedSymbol &&
      filtered.length > 0 &&
      !filtered.some((s) => s.symbol === selectedSymbol)
    ) {
      setSelectedSymbol(null)
    }
  }, [activeExchange, symbols, selectedSymbol])

  useEffect(() => {
    fetchTrend()
  }, [period, activeExchange])

  // 切換到資金費明細 Tab 時加載
  useEffect(() => {
    if (fundingTabIndex !== 2) return
    const exchangeId = activeExchange === 'all' ? undefined : activeExchange
    setLoadingFunding(true)
    getFundingHistory(exchangeId)
      .then((res) => {
        if (res.success && res.records) setFundingRecords(res.records)
        else setFundingRecords([])
      })
      .catch(() => setFundingRecords([]))
      .finally(() => setLoadingFunding(false))
  }, [fundingTabIndex, activeExchange])

  useEffect(() => {
    const loadSymbols = async () => {
      try {
        const res = await getSymbols()
        const list = (res.symbols || []).map((s) => ({ symbol: s.symbol, exchange: s.exchange }))
        setSymbols(list)
      } catch (err) {
        console.warn('獲取交易對列表失败:', err)
        setSymbols([])
      }
    }
    loadSymbols()
  }, [])

  useEffect(() => {
    if (!selectedSymbol) {
      setPriceData([])
      return
    }
    const limitMap = { '7d': 7, '30d': 30, '90d': 90, '1y': 365 }
    const limit = limitMap[period] || 90
    const exchangeId = activeExchange === 'all' ? undefined : activeExchange

    const loadPriceData = async () => {
      setIsLoadingPriceData(true)
      try {
        const res = await getDailyKlines(selectedSymbol, limit, exchangeId)
        const items: PriceChangeItem[] = (res.klines || []).map((k) => {
          const date = new Date(k.time * 1000).toISOString().split('T')[0]
          return {
            date,
            open: k.open,
            close: k.close,
            priceChange: k.close - k.open,
          }
        })
        setPriceData(items)
      } catch (err) {
        console.warn('獲取 K 線數據失败:', err)
        setPriceData([])
      } finally {
        setIsLoadingPriceData(false)
      }
    }
    loadPriceData()
  }, [selectedSymbol, period, activeExchange])

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
      setAllWithdrawRules(rulesRes.rules)
      setWithdrawHistory(historyRes.records)
    } catch (err: any) {
      console.error('獲取盈利数据失败:', err)
      setSummary(null)
      setStrategyProfits([])
      setTrend([])
      setAllWithdrawRules([])
      setWithdrawHistory([])
      toast({
        title: t('profitManagement.fetchFailed'),
        description: err?.message || String(err),
        status: 'error',
        duration: 5000,
        isClosable: true,
      })
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
      console.warn('獲取盈利趨勢失败:', err)
      setTrend([])
    }
  }

  const handleSaveRules = async (rules: ProfitWithdrawRule[]) => {
    const mergedRules =
      activeExchange === 'all'
        ? rules
        : [
            ...allWithdrawRules.filter(
              (r) => normalizeExchangeId(r.exchangeId) !== normalizeExchangeId(activeExchange)
            ),
            ...rules.map((r) => ({
              ...r,
              exchangeId: r.exchangeId || activeExchange,
            })),
          ]

    await updateWithdrawRules({ rules: mergedRules })
    setAllWithdrawRules(mergedRules)
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
            index={selectedExchangeIndex}
            onChange={(index) => {
              setSelectedExchangeIndex(index)
              if (index === 0) {
                setActiveExchange('all')
              } else {
                const exchangeList = exchanges
                if (index > 0 && index <= exchangeList.length) {
                  setActiveExchange(exchangeList[index - 1])
                }
              }
            }}
          >
            <TabList overflowX="auto" pb={2}>
              <Tab px={6}>{t('common.allExchanges')}</Tab>
              {exchanges.map((ex) => {
                // 格式化交易所名称显示
                const displayName = ex === 'binance' ? 'Binance' 
                  : ex === 'gate' ? 'Gate.io'
                  : ex === 'okx' ? 'OKX'
                  : ex.charAt(0).toUpperCase() + ex.slice(1)
                return (
                  <Tab key={ex} px={6}>
                    {displayName}
                  </Tab>
                )
              })}
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
                <StatLabel>{t('profitManagement.netProfit')}</StatLabel>
                <StatNumber color={(summary.totalProfit || 0) >= 0 ? 'green.500' : 'red.500'}>
                  {(summary.totalProfit || 0) >= 0 ? '+' : ''}{(summary.totalProfit || 0).toFixed(2)}
                </StatNumber>
                <StatHelpText>USDT</StatHelpText>
              </Stat>
            </Box>
            {(summary.grossProfit !== undefined || summary.totalFee !== undefined) && (
              <>
                <Box p={4} bg={bgColor} borderRadius="lg" borderWidth="1px" borderColor={borderColor}>
                  <Stat>
                    <StatLabel>{t('profitManagement.grossProfit')}</StatLabel>
                    <StatNumber color={(summary.grossProfit ?? 0) >= 0 ? 'green.500' : 'red.500'}>
                      {(summary.grossProfit ?? 0) >= 0 ? '+' : ''}{(summary.grossProfit ?? 0).toFixed(2)}
                    </StatNumber>
                    <StatHelpText>USDT</StatHelpText>
                  </Stat>
                </Box>
                <Box p={4} bg={bgColor} borderRadius="lg" borderWidth="1px" borderColor={borderColor}>
                  <Stat>
                    <StatLabel>{t('profitManagement.totalFee')}</StatLabel>
                    <StatNumber color="orange.500">-{(summary.totalFee ?? 0).toFixed(2)}</StatNumber>
                    <StatHelpText>USDT</StatHelpText>
                  </Stat>
                </Box>
              </>
            )}
            {summary.fundingNet !== undefined && (
              <Box p={4} bg={bgColor} borderRadius="lg" borderWidth="1px" borderColor={borderColor}>
                <Stat>
                  <StatLabel>{t('profitManagement.fundingNet')}</StatLabel>
                  <StatNumber color={(summary.fundingNet ?? 0) >= 0 ? 'green.500' : 'red.500'}>
                    {(summary.fundingNet ?? 0) >= 0 ? '+' : ''}{(summary.fundingNet ?? 0).toFixed(2)}
                  </StatNumber>
                  <StatHelpText>USDT</StatHelpText>
                </Stat>
              </Box>
            )}
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
            {summary.exchangeProfit !== undefined && (
              <Box p={4} bg={bgColor} borderRadius="lg" borderWidth="1px" borderColor={borderColor}>
                <Stat>
                  <StatLabel>{t('profitManagement.exchangeProfit')}</StatLabel>
                  <StatNumber color={(summary.exchangeProfit ?? 0) >= 0 ? 'green.500' : 'red.500'}>
                    {(summary.exchangeProfit ?? 0) >= 0 ? '+' : ''}{(summary.exchangeProfit ?? 0).toFixed(2)}
                  </StatNumber>
                  <StatHelpText>USDT</StatHelpText>
                </Stat>
              </Box>
            )}
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
          selectedSymbol={selectedSymbol}
          onSymbolChange={setSelectedSymbol}
          priceData={priceData}
          symbols={
            activeExchange === 'all'
              ? symbols
              : symbols.filter((s) => s.exchange.toLowerCase() === activeExchange.toLowerCase())
          }
          isLoadingPriceData={isLoadingPriceData}
        />

        {/* Tabs for Rules, History, Funding */}
        <Tabs
          variant="enclosed"
          colorScheme="blue"
          index={fundingTabIndex}
          onChange={setFundingTabIndex}
        >
          <TabList>
            <Tab>{t('profitManagement.autoWithdrawRules')}</Tab>
            <Tab>{t('profitManagement.withdrawHistory')}</Tab>
            <Tab>{t('profitManagement.fundingDetail')}</Tab>
          </TabList>

          <TabPanels>
            {/* Auto Withdraw Rules */}
            <TabPanel p={0} pt={4}>
              <WithdrawRuleForm
                rules={visibleWithdrawRules}
                strategyOptions={strategyProfits.map((s) => ({
                  id: s.strategyId,
                  name: s.strategyName,
                }))}
                onSave={handleSaveRules}
                activeExchange={activeExchange}
                exchangeOptions={exchanges}
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

            {/* 資金費明細 */}
            <TabPanel p={0} pt={4}>
              <Box
                p={6}
                bg={bgColor}
                borderRadius="xl"
                borderWidth="1px"
                borderColor={borderColor}
              >
                {loadingFunding ? (
                  <Center py={8}>
                    <Spinner size="lg" />
                  </Center>
                ) : (
                  <Table variant="simple" size="sm">
                    <Thead>
                      <Tr>
                        <Th>{t('profitManagement.date')}</Th>
                        <Th>Exchange</Th>
                        <Th>Symbol</Th>
                        <Th>Type</Th>
                        <Th isNumeric>{t('profitManagement.amount')}</Th>
                        <Th>Asset</Th>
                      </Tr>
                    </Thead>
                    <Tbody>
                      {fundingRecords.length === 0 ? (
                        <Tr>
                          <Td colSpan={6} textAlign="center" py={8} color="gray.500">
                            {t('profitManagement.noFundingHistory')}
                          </Td>
                        </Tr>
                      ) : (
                        fundingRecords.map((record) => (
                          <Tr key={record.id}>
                            <Td fontSize="sm">
                              {new Date(record.tradeTime).toLocaleString()}
                            </Td>
                            <Td>{record.exchange}</Td>
                            <Td>{record.symbol}</Td>
                            <Td>{record.incomeType}</Td>
                            <Td
                              isNumeric
                              fontWeight="medium"
                              color={record.income >= 0 ? 'green.500' : 'red.500'}
                            >
                              {record.income >= 0 ? '+' : ''}{record.income.toFixed(4)} {record.asset}
                            </Td>
                            <Td>{record.asset}</Td>
                          </Tr>
                        ))
                      )}
                    </Tbody>
                  </Table>
                )}
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
