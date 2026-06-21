import React, { useState, useEffect } from 'react'
import {
  Box,
  VStack,
  Heading,
  Text,
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
  Table,
  Thead,
  Tbody,
  Tr,
  Th,
  Td,
  TableContainer,
  Badge,
  useColorModeValue,
  Card,
  CardHeader,
  CardBody,
  HStack,
  IconButton,
  Tooltip,
} from '@chakra-ui/react'
import { RepeatIcon, ViewIcon } from '@chakra-ui/icons'
import { motion } from 'framer-motion'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { getCapitalUsage, getCapitalOverview } from '../services/capital'
import type { ExchangeUsageDetail, BotUsageInfo } from '../types/capital'

const MotionBox = motion(Box)

const CapitalManagement: React.FC = () => {
  const { t } = useTranslation()
  const toast = useToast()
  const navigate = useNavigate()
  const [exchanges, setExchanges] = useState<ExchangeUsageDetail[]>([])
  const [overview, setOverview] = useState<{ totalBalance: number; unrealizedPnL: number } | null>(null)
  const [loading, setLoading] = useState(true)

  const bgColor = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue('gray.200', 'gray.600')
  const mutedColor = useColorModeValue('gray.500', 'gray.400')

  const fetchData = async () => {
    setLoading(true)
    try {
      const [usageRes, overviewRes] = await Promise.all([
        getCapitalUsage(),
        getCapitalOverview(),
      ])
      if (usageRes.success && usageRes.exchanges) {
        setExchanges(usageRes.exchanges)
      } else if (!usageRes.success) {
        const isDataSourceMissing = usageRes.code === 'capital_data_source_unavailable'
        toast({
          title: t('capitalManagement.fetchDataFailed'),
          description: isDataSourceMissing
            ? t('capitalManagement.dataSourceUnavailableHint')
            : (usageRes.message || t('capitalManagement.checkBackendConnection')),
          status: isDataSourceMissing ? 'info' : 'warning',
          duration: 7000,
          isClosable: true,
        })
      }
      if (overviewRes.success && overviewRes.overview) {
        setOverview({
          totalBalance: overviewRes.overview.totalBalance,
          unrealizedPnL: overviewRes.overview.unrealizedPnL,
        })
      }
    } catch (err: unknown) {
      console.error('Failed to fetch capital data:', err)
      const msg = err instanceof Error ? err.message : String(err)
      const desc = msg.includes('500') || msg.includes('Internal')
        ? t('capitalManagement.backendError')
        : t('capitalManagement.checkBackendConnection')
      toast({
        title: t('capitalManagement.fetchDataFailed'),
        description: desc,
        status: 'error',
        duration: 5000,
      })
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchData()
  }, [])

  const formatUsdt = (v: number) =>
    v.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })
  const formatPct = (v: number) =>
    v.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 }) + '%'

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
          <HStack justify="space-between" align="center" wrap="wrap" gap={4}>
            <VStack align="start" spacing={1}>
              <Heading size="lg">{t('capitalManagement.title')}</Heading>
              <Text color={mutedColor}>{t('capitalManagement.subtitleView')}</Text>
            </VStack>
            <HStack>
              <Tooltip label={t('capitalManagement.refresh')}>
                <IconButton
                  aria-label="Refresh"
                  icon={<RepeatIcon />}
                  onClick={fetchData}
                  variant="outline"
                />
              </Tooltip>
              <Tooltip label={t('capitalManagement.viewBots')}>
                <IconButton
                  aria-label="View Bots"
                  icon={<ViewIcon />}
                  onClick={() => navigate('/bots')}
                  variant="outline"
                />
              </Tooltip>
            </HStack>
          </HStack>
        </MotionBox>

        {/* Testnet Warning */}
        {exchanges.some((e) => e.isTestnet) && (
          <Alert status="warning" borderRadius="lg">
            <AlertIcon />
            <Box flex="1">
              <Text fontWeight="bold">⚠️ {t('capitalManagement.testnetMode')}</Text>
              <Text fontSize="sm">
                {t('capitalManagement.testnetDesc')}
                {exchanges.filter((e) => e.isTestnet).map((e) => e.exchangeName).join(', ')}{' '}
                {t('capitalManagement.usingTestnet')}
              </Text>
            </Box>
          </Alert>
        )}

        {/* 全局汇总 */}
        {overview && (
          <SimpleGrid columns={{ base: 2, md: 3 }} spacing={4}>
            <Box p={4} bg={bgColor} borderRadius="lg" borderWidth="1px" borderColor={borderColor}>
              <Stat>
                <StatLabel>{t('capitalManagement.totalBalance')}</StatLabel>
                <StatNumber>{formatUsdt(overview.totalBalance)}</StatNumber>
                <StatHelpText>USDT</StatHelpText>
              </Stat>
            </Box>
            <Box p={4} bg={bgColor} borderRadius="lg" borderWidth="1px" borderColor={borderColor}>
              <Stat>
                <StatLabel>{t('capitalManagement.unrealizedPnL')}</StatLabel>
                <StatNumber color={overview.unrealizedPnL >= 0 ? 'green.500' : 'red.500'}>
                  {overview.unrealizedPnL >= 0 ? '+' : ''}
                  {formatUsdt(overview.unrealizedPnL)}
                </StatNumber>
                <StatHelpText>USDT</StatHelpText>
              </Stat>
            </Box>
            <Box p={4} bg={bgColor} borderRadius="lg" borderWidth="1px" borderColor={borderColor}>
              <Stat>
                <StatLabel>{t('capitalManagement.exchangeCount')}</StatLabel>
                <StatNumber>{exchanges.length}</StatNumber>
                <StatHelpText>{t('capitalManagement.exchanges')}</StatHelpText>
              </Stat>
            </Box>
          </SimpleGrid>
        )}

        {/* 各交易所 + Bot 占用明细 */}
        <VStack align="stretch" spacing={4}>
          {exchanges.length === 0 ? (
            <Center py={12} flexDirection="column">
              <Text color={mutedColor}>{t('capitalManagement.noExchanges')}</Text>
            </Center>
          ) : (
            exchanges.map((ex) => (
              <Card key={ex.exchangeId} bg={bgColor} borderWidth="1px" borderColor={borderColor}>
                <CardHeader pb={2}>
                  <HStack justify="space-between" flexWrap="wrap" gap={2}>
                    <HStack>
                      <Heading size="md">{ex.exchangeName}</Heading>
                      {ex.isTestnet && (
                        <Badge colorScheme="orange" fontSize="xs">
                          {t('capitalManagement.testnet')}
                        </Badge>
                      )}
                      {ex.status === 'error' && (
                        <Badge colorScheme="red">ERROR</Badge>
                      )}
                      {ex.status === 'offline' && (
                        <Badge colorScheme="gray">{t('capitalManagement.offline')}</Badge>
                      )}
                    </HStack>
                    <HStack spacing={6}>
                      <Text fontSize="sm" color={mutedColor}>
                        {t('capitalManagement.totalBalance')}:{' '}
                        <Text as="span" fontWeight="bold" color="inherit">
                          {formatUsdt(ex.totalBalance)} USDT
                        </Text>
                      </Text>
                      <Text fontSize="sm" color={mutedColor}>
                        {t('capitalManagement.available')}:{' '}
                        <Text as="span" fontWeight="bold" color="inherit">
                          {formatUsdt(ex.available)} USDT
                        </Text>
                      </Text>
                      {ex.pnl !== 0 && (
                        <Text
                          fontSize="sm"
                          color={ex.pnl >= 0 ? 'green.500' : 'red.500'}
                        >
                          {t('capitalManagement.unrealizedPnL')}:{' '}
                          {ex.pnl >= 0 ? '+' : ''}
                          {formatUsdt(ex.pnl)}
                        </Text>
                      )}
                    </HStack>
                  </HStack>
                </CardHeader>
                <CardBody pt={0}>
                  {ex.bots.length === 0 ? (
                    <Text color={mutedColor} fontSize="sm">
                      {t('capitalManagement.noBots')}
                    </Text>
                  ) : (
                    <TableContainer>
                      <Table size="sm" variant="simple">
                        <Thead>
                          <Tr>
                            <Th>{t('capitalManagement.symbol')}</Th>
                            <Th isNumeric>{t('capitalManagement.orderValue')}</Th>
                            <Th isNumeric>{t('capitalManagement.positionValue')}</Th>
                            <Th isNumeric>{t('capitalManagement.totalUsed')}</Th>
                            <Th isNumeric>{t('capitalManagement.orderPct')}</Th>
                            <Th isNumeric>{t('capitalManagement.positionPct')}</Th>
                            <Th isNumeric>{t('capitalManagement.totalUsedPct')}</Th>
                          </Tr>
                        </Thead>
                        <Tbody>
                          {ex.bots.map((bot: BotUsageInfo) => (
                            <Tr key={bot.botId}>
                              <Td>
                                <Text
                                  fontWeight="medium"
                                  cursor="pointer"
                                  _hover={{ color: 'blue.500' }}
                                  onClick={() => navigate(`/bots?highlight=${bot.botId}`)}
                                >
                                  {bot.symbol}
                                </Text>
                              </Td>
                              <Td isNumeric>{formatUsdt(bot.orderValue)}</Td>
                              <Td isNumeric>{formatUsdt(bot.positionValue)}</Td>
                              <Td isNumeric fontWeight="medium">
                                {formatUsdt(bot.totalUsed)}
                              </Td>
                              <Td isNumeric color={mutedColor}>
                                {formatPct(bot.orderPct)}
                              </Td>
                              <Td isNumeric color={mutedColor}>
                                {formatPct(bot.positionPct)}
                              </Td>
                              <Td isNumeric fontWeight="medium">
                                {formatPct(bot.totalUsedPct)}
                              </Td>
                            </Tr>
                          ))}
                        </Tbody>
                      </Table>
                    </TableContainer>
                  )}
                </CardBody>
              </Card>
            ))
          )}
        </VStack>
      </VStack>
    </Box>
  )
}

export default CapitalManagement
