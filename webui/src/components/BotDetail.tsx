import React, { useEffect, useState } from 'react'
import {
  Box,
  Button,
  Card,
  CardBody,
  Flex,
  Heading,
  HStack,
  Spinner,
  Text,
  Badge,
  useToast,
  Tabs,
  TabList,
  TabPanels,
  Tab,
  TabPanel,
  Stat,
  StatLabel,
  StatNumber,
  StatHelpText,
  SimpleGrid,
  Table,
  Thead,
  Tbody,
  Tr,
  Th,
  Td,
  TableContainer,
} from '@chakra-ui/react'
import { ChevronLeftIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import { Link, useParams, useNavigate } from 'react-router-dom'
import {
  getBotById,
  startBot,
  stopBot,
  getPositionsSummary,
  getStatistics,
  getLogs,
  BotDetailInfo,
} from '../services/api'
import { useSymbol } from '../contexts/SymbolContext'
import BotRiskControlPanel from './BotRiskControlPanel'

const BotDetail: React.FC = () => {
  const { botId } = useParams<{ botId: string }>()
  const navigate = useNavigate()
  const { t } = useTranslation()
  const toast = useToast()
  const { navigateToBot } = useSymbol()
  const [bot, setBot] = useState<BotDetailInfo | null>(null)
  const [loading, setLoading] = useState(true)
  const [actioning, setActioning] = useState(false)
  const [positionsSummary, setPositionsSummary] = useState<any>(null)
  const [statistics, setStatistics] = useState<any>(null)
  const [logs, setLogs] = useState<any[]>([])
  const [logsLoading, setLogsLoading] = useState(false)

  const fetchBot = async () => {
    if (!botId) return
    try {
      const data = await getBotById(botId)
      setBot(data)
      return data
    } catch (err) {
      console.error('Failed to fetch bot:', err)
      toast({ title: t('botList.fetchFailed'), status: 'error', duration: 3000 })
      return null
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchBot()
  }, [botId])

  useEffect(() => {
    if (!bot?.running || !bot?.exchange || !bot?.symbol) return
    const fetchOverview = async () => {
      try {
        const [posRes, statRes] = await Promise.all([
          getPositionsSummary(bot.exchange, bot.symbol).catch(() => null),
          getStatistics(bot.exchange, bot.symbol).catch(() => null),
        ])
        setPositionsSummary(posRes)
        setStatistics(statRes)
      } catch {
        setPositionsSummary(null)
        setStatistics(null)
      }
    }
    fetchOverview()
    const interval = setInterval(fetchOverview, 10000)
    return () => clearInterval(interval)
  }, [bot?.running, bot?.exchange, bot?.symbol])

  const fetchLogs = async () => {
    if (!bot?.symbol) return
    setLogsLoading(true)
    try {
      const res = await getLogs({ limit: 50, keyword: bot.symbol })
      setLogs(res.logs || [])
    } catch {
      setLogs([])
    } finally {
      setLogsLoading(false)
    }
  }

  const handleStart = async () => {
    if (!botId) return
    setActioning(true)
    try {
      await startBot(botId)
      toast({ title: t('botList.startSuccess'), status: 'success', duration: 2000 })
      await fetchBot()
    } catch (err) {
      toast({ title: t('botList.startFailed'), status: 'error', duration: 3000 })
    } finally {
      setActioning(false)
    }
  }

  const handleStop = async () => {
    if (!botId) return
    setActioning(true)
    try {
      await stopBot(botId)
      toast({ title: t('botList.stopSuccess'), status: 'success', duration: 2000 })
      await fetchBot()
    } catch (err) {
      toast({ title: t('botList.stopFailed'), status: 'error', duration: 3000 })
    } finally {
      setActioning(false)
    }
  }

  const handleOpenWorkspace = () => {
    if (!bot || !botId) return
    navigateToBot(botId, 'dashboard')
  }

  const handleNavigateToRisk = () => {
    if (!bot || !botId) return
    navigateToBot(botId, 'risk')
  }

  const handleNavigateToLogs = () => {
    navigate('/logs')
  }

  if (loading) {
    return (
      <Flex justify="center" align="center" minH="200px">
        <Spinner size="lg" />
      </Flex>
    )
  }

  if (!bot) {
    return (
      <Box>
        <Button as={Link} to="/bots" leftIcon={<ChevronLeftIcon />} variant="ghost" size="sm" mb={4}>
          {t('common.back')}
        </Button>
        <Text color="gray.500">{t('botList.fetchFailed')}</Text>
      </Box>
    )
  }

  return (
    <Box>
      <Button as={Link} to="/bots" leftIcon={<ChevronLeftIcon />} variant="ghost" size="sm" mb={4}>
        {t('common.back')}
      </Button>
      <Card mb={4}>
        <CardBody>
          <Flex justify="space-between" align="flex-start" flexWrap="wrap" gap={4}>
            <Box>
              <HStack spacing={2} mb={2}>
                <Badge colorScheme={bot.running ? 'green' : 'gray'} fontSize="10px">
                  {bot.running ? t('botList.running') : t('botList.stopped')}
                </Badge>
                {bot.risk_triggered && (
                  <Badge colorScheme="red" fontSize="10px">{t('botList.riskTriggered')}</Badge>
                )}
              </HStack>
              <Heading size="md">{bot.name || bot.symbol}</Heading>
              <Text fontSize="sm" color="gray.500" mt={1}>
                {bot.exchange} · {bot.symbol} ({bot.market_type})
              </Text>
              {bot.running && (
                <HStack spacing={4} mt={3} fontSize="sm">
                  {bot.current_price != null && (
                    <Text>${bot.current_price.toLocaleString(undefined, { minimumFractionDigits: 2 })}</Text>
                  )}
                  {bot.total_pnl != null && (
                    <Text color={bot.total_pnl >= 0 ? 'green.500' : 'red.500'}>
                      PnL: {bot.total_pnl >= 0 ? '+' : ''}{bot.total_pnl.toFixed(2)}
                    </Text>
                  )}
                </HStack>
              )}
            </Box>
            <HStack>
              {bot.running ? (
                <>
                  <Button size="sm" colorScheme="blue" onClick={handleOpenWorkspace}>
                    {t('botDetail.openWorkspace')}
                  </Button>
                  <Button
                    size="sm"
                    colorScheme="red"
                    variant="outline"
                    isLoading={actioning}
                    onClick={handleStop}
                  >
                    {t('botList.stop')}
                  </Button>
                </>
              ) : (
                <Button size="sm" colorScheme="green" isLoading={actioning} onClick={handleStart}>
                  {t('botList.start')}
                </Button>
              )}
            </HStack>
          </Flex>
        </CardBody>
      </Card>

      <Tabs colorScheme="blue" variant="enclosed">
        <TabList>
          <Tab>{t('botDetail.tabOverview')}</Tab>
          <Tab>{t('botDetail.tabStrategy')}</Tab>
          <Tab>{t('botDetail.tabRisk')}</Tab>
          <Tab>{t('botDetail.tabLogs')}</Tab>
        </TabList>
        <TabPanels>
          <TabPanel px={0}>
            {bot.running ? (
              <SimpleGrid columns={{ base: 1, md: 2, lg: 4 }} spacing={4}>
                <Card>
                  <CardBody>
                    <Stat>
                      <StatLabel>{t('botDetail.currentPrice')}</StatLabel>
                      <StatNumber>
                        ${(positionsSummary?.current_price ?? bot.current_price ?? 0).toLocaleString(undefined, { minimumFractionDigits: 2 })}
                      </StatNumber>
                    </Stat>
                  </CardBody>
                </Card>
                <Card>
                  <CardBody>
                    <Stat>
                      <StatLabel>{t('botDetail.unrealizedPnl')}</StatLabel>
                      <StatNumber color={(positionsSummary?.unrealized_pnl ?? 0) >= 0 ? 'green.500' : 'red.500'}>
                        {(positionsSummary?.unrealized_pnl ?? bot.total_pnl ?? 0) >= 0 ? '+' : ''}
                        {(positionsSummary?.unrealized_pnl ?? bot.total_pnl ?? 0).toFixed(2)}
                      </StatNumber>
                    </Stat>
                  </CardBody>
                </Card>
                <Card>
                  <CardBody>
                    <Stat>
                      <StatLabel>{t('statistics.totalTrades')}</StatLabel>
                      <StatNumber>{statistics?.total_trades ?? 0}</StatNumber>
                    </Stat>
                  </CardBody>
                </Card>
                <Card>
                  <CardBody>
                    <Stat>
                      <StatLabel>{t('statistics.totalPnl')}</StatLabel>
                      <StatNumber color={(statistics?.total_pnl ?? 0) >= 0 ? 'green.500' : 'red.500'}>
                        {(statistics?.total_pnl ?? 0) >= 0 ? '+' : ''}{(statistics?.total_pnl ?? 0).toFixed(2)}
                      </StatNumber>
                    </Stat>
                  </CardBody>
                </Card>
              </SimpleGrid>
            ) : (
              <Text color="gray.500">{t('botDetail.startToViewOverview')}</Text>
            )}
          </TabPanel>
          <TabPanel px={0}>
            <Card>
              <CardBody>
                <Text color="gray.600" mb={4}>{t('botDetail.strategyHint')}</Text>
                <Button size="sm" colorScheme="blue" onClick={handleOpenWorkspace}>
                  {t('botDetail.openWorkspace')} → {t('sidebar.strategyAllocation')}
                </Button>
              </CardBody>
            </Card>
          </TabPanel>
          <TabPanel px={0}>
            {botId && (
              <BotRiskControlPanel botId={botId} botRunning={bot.running} />
            )}
          </TabPanel>
          <TabPanel px={0}>
            <Card>
              <CardBody>
                <Flex justify="space-between" align="center" mb={4}>
                  <Text color="gray.600">{t('botDetail.logsHint')}</Text>
                  <Button size="sm" variant="outline" onClick={fetchLogs} isLoading={logsLoading}>
                    {t('common.refresh')}
                  </Button>
                </Flex>
                {logs.length > 0 ? (
                  <TableContainer maxH="300px" overflowY="auto">
                    <Table size="sm">
                      <Thead>
                        <Tr>
                          <Th>{t('botDetail.logTime')}</Th>
                          <Th>{t('botDetail.logLevel')}</Th>
                          <Th>{t('botDetail.logMessage')}</Th>
                        </Tr>
                      </Thead>
                      <Tbody>
                        {logs.map((log, i) => (
                          <Tr key={log.id || i}>
                            <Td fontSize="xs">{log.timestamp || '-'}</Td>
                            <Td><Badge size="sm">{log.level || 'info'}</Badge></Td>
                            <Td fontSize="xs" maxW="400px" isTruncated>{log.message || '-'}</Td>
                          </Tr>
                        ))}
                      </Tbody>
                    </Table>
                  </TableContainer>
                ) : (
                  <Button size="sm" onClick={fetchLogs} isLoading={logsLoading}>
                    {t('botDetail.loadLogs')}
                  </Button>
                )}
              </CardBody>
            </Card>
          </TabPanel>
        </TabPanels>
      </Tabs>
    </Box>
  )
}

export default BotDetail
