import React, { useState, useEffect, useCallback } from 'react'
import {
  Box, VStack, HStack, Heading, Text, SimpleGrid, Stat, StatLabel, StatNumber,
  StatHelpText, Table, Thead, Tbody, Tr, Th, Td, Badge, Spinner, Center,
  useColorModeValue, Card, CardBody, CardHeader, Tooltip,
} from '@chakra-ui/react'
import { useTranslation } from 'react-i18next'
import {
  BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip as RechartsTooltip,
  ResponsiveContainer, Line, ComposedChart,
} from 'recharts'
import { getFundingCarryDashboard } from '../services/fundingCarry'
import type { FundingCarryDashboardResponse, FundingCarryDailyIncome, FundingCarrySymbol } from '../types/fundingCarry'

const FundingCarryDashboard: React.FC = () => {
  const { t } = useTranslation()
  const [data, setData] = useState<FundingCarryDashboardResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const cardBg = useColorModeValue('white', 'gray.700')
  const statBg = useColorModeValue('blue.50', 'blue.900')

  const fetchData = useCallback(async () => {
    try {
      const res = await getFundingCarryDashboard()
      setData(res)
    } catch (e) {
      console.error('Failed to fetch funding carry dashboard:', e)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchData()
    const timer = setInterval(fetchData, 30000)
    return () => clearInterval(timer)
  }, [fetchData])

  if (loading) {
    return <Center h="400px"><Spinner size="xl" /></Center>
  }

  if (!data) {
    return <Center h="400px"><Text>{t('common.loadFailed', 'Load failed')}</Text></Center>
  }

  const { overview, symbols, daily_income } = data

  const sortedDaily = [...(daily_income || [])].sort((a, b) => a.date.localeCompare(b.date))

  let cumulative = 0
  const chartData = sortedDaily.map((d: FundingCarryDailyIncome) => {
    cumulative += d.income
    return { ...d, cumulative: Math.round(cumulative * 100) / 100 }
  })

  return (
    <Box p={6}>
      <VStack spacing={6} align="stretch">
        <Heading size="lg">{t('profitManagement.fundingCarryDashboard', 'Funding Carry Dashboard')}</Heading>

        {/* Overview cards */}
        <SimpleGrid columns={{ base: 2, md: 4, lg: 7 }} spacing={4}>
          <StatCard label={t('profitManagement.fundingCarryIncome24h', '24h')} value={overview.total_income_24h} suffix="USDT" bg={statBg} />
          <StatCard label={t('profitManagement.fundingCarryIncome7d', '7d')} value={overview.total_income_7d} suffix="USDT" bg={statBg} />
          <StatCard label={t('profitManagement.fundingCarryIncome30d', '30d')} value={overview.total_income_30d} suffix="USDT" bg={statBg} />
          <StatCard label={t('profitManagement.fundingCarryIncomeAll', 'Total')} value={overview.total_income_all} suffix="USDT" bg={statBg} />
          <StatCard label={t('profitManagement.fundingCarryAnnualized', 'APY')} value={overview.annualized_yield * 100} suffix="%" bg={statBg} />
          <StatCard label={t('profitManagement.fundingCarryActiveBots', 'Bots')} value={overview.active_bots} bg={statBg} />
          <StatCard label={t('profitManagement.fundingCarryCapitalDeployed', 'Capital')} value={overview.total_capital_deployed} suffix="USDT" bg={statBg} />
        </SimpleGrid>

        {/* Charts & table side by side on large screens */}
        <SimpleGrid columns={{ base: 1, lg: 2 }} spacing={6}>
          {/* Daily income chart */}
          <Card bg={cardBg} shadow="sm">
            <CardHeader pb={2}>
              <Heading size="sm">{t('profitManagement.fundingCarryDailyIncome', 'Daily Income')}</Heading>
            </CardHeader>
            <CardBody>
              {chartData.length > 0 ? (
                <ResponsiveContainer width="100%" height={300}>
                  <ComposedChart data={chartData}>
                    <CartesianGrid strokeDasharray="3 3" />
                    <XAxis dataKey="date" tick={{ fontSize: 11 }} />
                    <YAxis yAxisId="left" tick={{ fontSize: 11 }} />
                    <YAxis yAxisId="right" orientation="right" tick={{ fontSize: 11 }} />
                    <RechartsTooltip />
                    <Bar yAxisId="left" dataKey="income" fill="#3182CE" name="Daily" />
                    <Line yAxisId="right" type="monotone" dataKey="cumulative" stroke="#38A169" strokeWidth={2} dot={false} name="Cumulative" />
                  </ComposedChart>
                </ResponsiveContainer>
              ) : (
                <Center h="300px"><Text color="gray.400">{t('common.noData', 'No data')}</Text></Center>
              )}
            </CardBody>
          </Card>

          {/* Symbol status table */}
          <Card bg={cardBg} shadow="sm">
            <CardHeader pb={2}>
              <Heading size="sm">{t('profitManagement.fundingCarrySymbolTable', 'Symbol Status')}</Heading>
            </CardHeader>
            <CardBody overflowX="auto">
              <Table size="sm" variant="simple">
                <Thead>
                  <Tr>
                    <Th>{t('common.symbol', 'Symbol')}</Th>
                    <Th>{t('common.status', 'Status')}</Th>
                    <Th isNumeric>{t('common.capital', 'Capital')}</Th>
                    <Th isNumeric>{t('profitManagement.fundingCarryIncome24h', '24h')}</Th>
                    <Th isNumeric>{t('profitManagement.fundingCarryIncome7d', '7d')}</Th>
                  </Tr>
                </Thead>
                <Tbody>
                  {(symbols || []).map((sym: FundingCarrySymbol) => (
                    <Tr key={sym.symbol}>
                      <Td fontWeight="bold">{sym.symbol}</Td>
                      <Td>
                        <Badge colorScheme={sym.status === 'running' ? 'green' : 'gray'}>
                          {sym.status}
                        </Badge>
                      </Td>
                      <Td isNumeric>
                        <Tooltip label="USDT">{sym.capital.toFixed(2)}</Tooltip>
                      </Td>
                      <Td isNumeric color={sym.income_24h >= 0 ? 'green.500' : 'red.500'}>
                        {sym.income_24h >= 0 ? '+' : ''}{sym.income_24h.toFixed(4)}
                      </Td>
                      <Td isNumeric color={sym.income_7d >= 0 ? 'green.500' : 'red.500'}>
                        {sym.income_7d >= 0 ? '+' : ''}{sym.income_7d.toFixed(4)}
                      </Td>
                    </Tr>
                  ))}
                  {(!symbols || symbols.length === 0) && (
                    <Tr><Td colSpan={5} textAlign="center" color="gray.400">{t('common.noData', 'No data')}</Td></Tr>
                  )}
                </Tbody>
              </Table>
            </CardBody>
          </Card>
        </SimpleGrid>
      </VStack>
    </Box>
  )
}

const StatCard: React.FC<{ label: string; value: number; suffix?: string; bg: string }> = ({ label, value, suffix, bg }) => (
  <Stat bg={bg} p={3} borderRadius="lg" shadow="sm">
    <StatLabel fontSize="xs" noOfLines={1}>{label}</StatLabel>
    <StatNumber fontSize="md">
      {typeof value === 'number' ? value.toFixed(value < 100 ? 4 : 2) : value}
    </StatNumber>
    {suffix && <StatHelpText fontSize="xs" mb={0}>{suffix}</StatHelpText>}
  </Stat>
)

export default FundingCarryDashboard
