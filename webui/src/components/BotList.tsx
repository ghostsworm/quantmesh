import React, { useEffect, useState, useMemo } from 'react'
import {
  Box,
  Button,
  ButtonGroup,
  Card,
  CardBody,
  Flex,
  Grid,
  Heading,
  HStack,
  IconButton,
  Select,
  Spinner,
  Text,
  Badge,
  useToast,
  useDisclosure,
  useColorModeValue,
  AlertDialog,
  AlertDialogBody,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogContent,
  AlertDialogOverlay,
  Tooltip,
  VStack,
} from '@chakra-ui/react'
import { AddIcon, ChevronRightIcon, RepeatIcon, DeleteIcon, TimeIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate } from 'react-router-dom'
import { getBots, getBotGroups, startBot, stopBot, deleteBot, pollBotUntilRunning, closePositionsV2, BotInfo } from '../services/api'
import { getUniqueExchangesAndSymbols, filterBotsByExchangeAndSymbol } from '../utils/botListFilters'
import type { BotGroupResponse } from '../services/api'
import BotBacktestDialog from './BotBacktestDialog'
import StopWithCloseConfirmDialog from './StopWithCloseConfirmDialog'
import { computeLiquidationPrice } from './ParamAdvisor'

type FilterStatus = 'all' | 'running' | 'stopped'

const formatDateTime = (iso: string) =>
  new Date(iso).toLocaleString(undefined, { dateStyle: 'short', timeStyle: 'short' })

const BotList: React.FC = () => {
  const { t } = useTranslation()
  const toast = useToast()
  const navigate = useNavigate()
  const [bots, setBots] = useState<BotInfo[]>([])
  const [botGroups, setBotGroups] = useState<BotGroupResponse[]>([])
  const [loading, setLoading] = useState(true)
  const [actionBotId, setActionBotId] = useState<string | null>(null)
  const [filterStatus, setFilterStatus] = useState<FilterStatus>('all')
  const [filterExchange, setFilterExchange] = useState<string>('')
  const [filterSymbol, setFilterSymbol] = useState<string>('')
  const [deleteTarget, setDeleteTarget] = useState<BotInfo | null>(null)
  const [stopTarget, setStopTarget] = useState<BotInfo | null>(null)
  const { isOpen: isDeleteOpen, onOpen: onDeleteOpen, onClose: onDeleteClose } = useDisclosure()
  const { isOpen: isStopDialogOpen, onOpen: onStopDialogOpen, onClose: onStopDialogClose } = useDisclosure()
  const cancelDeleteRef = React.useRef<HTMLButtonElement>(null)

  // Apple-style: subtle, refined colors
  const cardBg = useColorModeValue('white', 'gray.800')
  const cardBorder = useColorModeValue('gray.100', 'whiteAlpha.100')
  const cardShadow = useColorModeValue('sm', 'dark-lg')
  const cardHoverShadow = useColorModeValue('md', 'xl')
  const runningAccent = useColorModeValue('green.500', 'green.400')
  const stoppedMuted = useColorModeValue('gray.500', 'gray.400')
  const metaColor = useColorModeValue('gray.600', 'gray.400')
  const summaryBg = useColorModeValue('gray.50', 'whiteAlpha.50')

  const [backtestBotId, setBacktestBotId] = useState<string | null>(null)
  const [backtestBotName, setBacktestBotName] = useState<string>('')
  const [isBacktestOpen, setIsBacktestOpen] = useState(false)

  const botIdToGroup = React.useMemo(() => {
    const m = new Map<string, BotGroupResponse>()
    for (const g of botGroups) {
      for (const id of g.bot_ids || []) {
        m.set(id, g)
      }
    }
    return m
  }, [botGroups])

  const { uniqueExchanges, uniqueSymbols } = useMemo(
    () => getUniqueExchangesAndSymbols(bots),
    [bots]
  )

  const statusFiltered = bots.filter((b) => {
    if (filterStatus === 'running') return b.running
    if (filterStatus === 'stopped') return !b.running
    return true
  })
  const filteredBots = filterBotsByExchangeAndSymbol(
    statusFiltered,
    filterExchange,
    filterSymbol
  ).sort((a, b) => {
    if (a.running && !b.running) return -1
    if (!a.running && b.running) return 1
    return 0
  })

  const fetchBots = async () => {
    try {
      const [botsRes, groupsRes] = await Promise.all([
        getBots(),
        getBotGroups().catch(() => ({ bot_groups: [] as BotGroupResponse[] })),
      ])
      setBots(botsRes.bots || [])
      setBotGroups(groupsRes.bot_groups || [])
    } catch (err) {
      console.error('Failed to fetch bots:', err)
      toast({ title: t('botList.fetchFailed'), status: 'error', duration: 3000 })
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchBots()
    const interval = setInterval(fetchBots, 15000)
    return () => clearInterval(interval)
  }, [])

  const handleStart = async (botId: string) => {
    setActionBotId(botId)
    try {
      const res = await startBot(botId)
      if (res.status === 'starting') {
        toast({ title: t('botList.starting'), status: 'info', duration: 3000 })
        const outcome = await pollBotUntilRunning(botId)
        if (outcome.running) {
          toast({ title: t('botList.startSuccess'), status: 'success', duration: 2000 })
        } else if (outcome.lastStartError) {
          toast({
            title: t('botList.startFailed'),
            description: outcome.lastStartError,
            status: 'error',
            duration: 12000,
            isClosable: true,
          })
        } else {
          toast({ title: t('botList.startPending'), status: 'warning', duration: 4000 })
        }
      } else {
        toast({ title: t('botList.startSuccess'), status: 'success', duration: 2000 })
      }
      fetchBots()
    } catch (err) {
      const e = err as Error & { errorKey?: string; groupName?: string }
      const msg = e.errorKey ? t(e.errorKey, { groupName: e.groupName ?? '' }) : t('botList.startFailed')
      toast({ title: msg, status: 'error', duration: 4000 })
    } finally {
      setActionBotId(null)
    }
  }

  const handleStopClick = (bot: BotInfo) => {
    setStopTarget(bot)
    onStopDialogOpen()
  }

  const handleStopDialogClose = () => {
    onStopDialogClose()
    setStopTarget(null)
  }

  const handleStopOnly = async () => {
    if (!stopTarget) return
    setActionBotId(stopTarget.bot_id)
    try {
      await stopBot(stopTarget.bot_id)
      toast({ title: t('botList.stopSuccess'), status: 'success', duration: 2000 })
      handleStopDialogClose()
      fetchBots()
    } catch (err) {
      toast({ title: t('botList.stopFailed'), status: 'error', duration: 3000 })
    } finally {
      setActionBotId(null)
    }
  }

  const handleStopAndClose = async (req: Parameters<typeof closePositionsV2>[1]) => {
    if (!stopTarget) return
    setActionBotId(stopTarget.bot_id)
    try {
      await closePositionsV2(stopTarget.bot_id, req)
      await stopBot(stopTarget.bot_id)
      toast({ title: t('globalDashboard.closePositions.success'), status: 'success', duration: 2000 })
      handleStopDialogClose()
      fetchBots()
    } catch (err) {
      toast({
        title: t('globalDashboard.closePositions.failed'),
        description: err instanceof Error ? err.message : String(err),
        status: 'error',
        duration: 4000,
      })
    } finally {
      setActionBotId(null)
    }
  }

  const handleDeleteClick = (bot: BotInfo) => {
    setDeleteTarget(bot)
    onDeleteOpen()
  }

  const handleDeleteConfirm = async () => {
    if (!deleteTarget) return
    setActionBotId(deleteTarget.bot_id)
    try {
      await deleteBot(deleteTarget.bot_id)
      toast({ title: t('botList.deleteSuccess'), status: 'success', duration: 2000 })
      onDeleteClose()
      setDeleteTarget(null)
      fetchBots()
    } catch (err) {
      const e = err as Error & { errorKey?: string; groupName?: string }
      const msg = e.errorKey ? t(e.errorKey, { groupName: e.groupName ?? '' }) : t('botList.deleteFailed')
      toast({ title: msg, status: 'error', duration: 4000 })
    } finally {
      setActionBotId(null)
    }
  }

  const handleBacktest = (bot: BotInfo) => {
    setBacktestBotId(bot.bot_id)
    setBacktestBotName(bot.name || bot.symbol)
    setIsBacktestOpen(true)
  }

  const handleCloseBacktest = () => {
    setIsBacktestOpen(false)
    setBacktestBotId(null)
    setBacktestBotName('')
  }

  const handleDeleteCancel = () => {
    onDeleteClose()
    setDeleteTarget(null)
  }

  if (loading) {
    return (
      <Flex justify="center" align="center" minH="200px">
        <Spinner size="lg" />
      </Flex>
    )
  }

  return (
    <Box>
      <Flex justify="space-between" align="center" mb={8} flexWrap="wrap" gap={4}>
        <Heading size="lg" fontWeight="600" letterSpacing="-0.02em">
          {t('botList.title')}
        </Heading>
        <HStack spacing={3}>
          <Button
            as={Link}
            to="/bots/create"
            leftIcon={<AddIcon />}
            size="sm"
            colorScheme="blue"
            borderRadius="lg"
          >
            {t('botList.createBot')}
          </Button>
          <ButtonGroup size="sm" isAttached variant="outline" borderRadius="lg">
            <Button
              colorScheme={filterStatus === 'all' ? 'blue' : 'gray'}
              variant={filterStatus === 'all' ? 'solid' : 'outline'}
              onClick={() => setFilterStatus('all')}
              borderRadius="lg"
            >
              {t('botList.filterAll')}
            </Button>
            <Button
              colorScheme={filterStatus === 'running' ? 'green' : 'gray'}
              variant={filterStatus === 'running' ? 'solid' : 'outline'}
              onClick={() => setFilterStatus('running')}
              borderRadius="lg"
            >
              {t('botList.running')}
            </Button>
            <Button
              colorScheme={filterStatus === 'stopped' ? 'gray' : 'gray'}
              variant={filterStatus === 'stopped' ? 'solid' : 'outline'}
              onClick={() => setFilterStatus('stopped')}
              borderRadius="lg"
            >
              {t('botList.stopped')}
            </Button>
          </ButtonGroup>
          {(uniqueExchanges.length > 0 || uniqueSymbols.length > 0) && (
            <HStack spacing={2}>
              {uniqueExchanges.length > 0 && (
                <Select
                  size="sm"
                  w="auto"
                  minW="120px"
                  value={filterExchange}
                  onChange={(e) => setFilterExchange(e.target.value)}
                  borderRadius="lg"
                >
                  <option value="">{t('botList.filterExchangeAll')}</option>
                  {uniqueExchanges.map((ex) => (
                    <option key={ex} value={ex}>{ex}</option>
                  ))}
                </Select>
              )}
              {uniqueSymbols.length > 0 && (
                <Select
                  size="sm"
                  w="auto"
                  minW="120px"
                  value={filterSymbol}
                  onChange={(e) => setFilterSymbol(e.target.value)}
                  borderRadius="lg"
                >
                  <option value="">{t('botList.filterSymbolAll')}</option>
                  {uniqueSymbols.map((sym) => (
                    <option key={sym} value={sym}>{sym}</option>
                  ))}
                </Select>
              )}
            </HStack>
          )}
          <Button leftIcon={<RepeatIcon />} size="sm" variant="outline" onClick={fetchBots} borderRadius="lg">
            {t('common.refresh')}
          </Button>
        </HStack>
      </Flex>

      {filteredBots.length > 0 && (() => {
        const total = filteredBots.reduce((sum, b) => sum + (b.total_allocated_capital ?? 0), 0)
        if (total <= 0) return null
        return (
          <Box mb={6} px={4} py={3} bg={summaryBg} borderRadius="xl">
            <Text fontSize="sm" fontWeight="500" color={metaColor}>
              {t('botList.totalInvestment')}:{' '}
              <Text as="span" color="blue.500" fontWeight="600">
                ${total.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}
              </Text>
            </Text>
          </Box>
        )
      })()}

      {filteredBots.length === 0 ? (
        <Card borderRadius="2xl" shadow={cardShadow} borderWidth="1px" borderColor={cardBorder}>
          <CardBody py={12}>
            <Text color={metaColor} textAlign="center">
              {bots.length === 0 ? t('botList.noBots') : t('botList.noMatchFilter')}
            </Text>
          </CardBody>
        </Card>
      ) : (
        <Grid templateColumns={{ base: '1fr', md: 'repeat(2, 1fr)', lg: 'repeat(3, 1fr)' }} gap={6}>
          {filteredBots.map((bot) => {
            const liqEstimate = bot.running && bot.current_price && bot.current_price > 0
              ? computeLiquidationPrice({
                  currentPrice: bot.current_price,
                  buyWindowSize: bot.buy_window_size || (bot.strategies?.find(s => s.type === 'grid') ? 50 : 20),
                  orderQuantity: bot.order_quantity || 100,
                  priceInterval: bot.price_interval || 0.0025,
                  totalCapital: bot.total_allocated_capital || 10000,
                  leverage: bot.leverage || 1,
                  maxCapitalRatio: bot.max_capital_ratio ?? 1.0,
                })
              : null

            return (
              <Card
                key={bot.bot_id}
                bg={cardBg}
                borderRadius="2xl"
                borderWidth="1px"
                borderColor={cardBorder}
                shadow={cardShadow}
                _hover={{ shadow: cardHoverShadow, transform: 'translateY(-2px)' }}
                cursor="pointer"
                transition="all 0.2s ease"
                onClick={() => navigate(`/bots/${bot.bot_id}`)}
              >
                <CardBody p={6}>
                  <Flex justify="space-between" align="flex-start" gap={4}>
                    <VStack align="stretch" spacing={3} flex={1} minW={0}>
                      <HStack spacing={2} flexWrap="wrap">
                        <Badge
                          colorScheme={bot.running ? 'green' : 'gray'}
                          fontSize="11px"
                          fontWeight="500"
                          px={2}
                          py={0.5}
                          borderRadius="full"
                          variant={bot.running ? 'solid' : 'subtle'}
                        >
                          {bot.running ? t('botList.running') : t('botList.stopped')}
                        </Badge>
                        {bot.risk_triggered && (
                          <Badge colorScheme="red" fontSize="11px" px={2} py={0.5} borderRadius="full">
                            {t('botList.riskTriggered')}
                          </Badge>
                        )}
                        {(bot.hedge_group_name || botIdToGroup.get(bot.bot_id)) && (
                          <Badge colorScheme="purple" fontSize="11px" variant="outline" px={2} py={0.5} borderRadius="full">
                            {bot.hedge_group_name ? t('botList.hedgeGroupWithName', { name: bot.hedge_group_name }) : t('botList.hedgeGroup')}
                          </Badge>
                        )}
                        {bot.direction && (
                          <Badge colorScheme="teal" fontSize="11px" variant="outline" px={2} py={0.5} borderRadius="full">
                            {bot.direction === 'SHORT' ? t('botList.gridShort') : bot.direction === 'BOTH' ? t('botList.gridBoth') : t('botList.gridLong')}
                          </Badge>
                        )}
                        {bot.testnet === true ? (
                          <Badge colorScheme="orange" fontSize="11px" variant="solid" px={2} py={0.5} borderRadius="full">
                            {t('botList.envTestnet')}
                          </Badge>
                        ) : (
                          <Badge colorScheme="red" fontSize="11px" variant="outline" px={2} py={0.5} borderRadius="full">
                            {t('botList.envLive')}
                          </Badge>
                        )}
                      </HStack>

                      <Heading
                        size="md"
                        fontWeight="600"
                        color={bot.running ? runningAccent : 'inherit'}
                        letterSpacing="-0.01em"
                        lineHeight="1.3"
                      >
                        {bot.name || bot.symbol}
                      </Heading>

                      <Text fontSize="sm" color={metaColor}>
                        {bot.exchange} · {bot.symbol} ({bot.market_type})
                      </Text>

                      {/* 创建时间 & 停止时间 */}
                      <VStack align="stretch" spacing={1}>
                        {bot.created_at && (
                          <Text fontSize="xs" color={metaColor}>
                            {t('botList.createdAt')} {formatDateTime(bot.created_at)}
                          </Text>
                        )}
                        {!bot.running && bot.stopped_at && (
                          <Text fontSize="xs" color={stoppedMuted}>
                            {t('botList.stoppedAt')} {formatDateTime(bot.stopped_at)}
                          </Text>
                        )}
                      </VStack>

                      {bot.strategies && bot.strategies.length > 0 && (
                        <HStack spacing={1.5} flexWrap="wrap">
                          {bot.strategies.map((strategy, idx) => (
                            <Badge key={idx} size="sm" variant="outline" fontSize="10px" colorScheme="blue" borderRadius="md">
                              {strategy.name} {strategy.weight > 0 && `(${Math.round(strategy.weight * 100)}%)`}
                            </Badge>
                          ))}
                        </HStack>
                      )}

                      <HStack spacing={4} fontSize="xs" color={metaColor} flexWrap="wrap">
                        {(bot.price_interval != null && bot.price_interval > 0) && (
                          <Text>{t('botList.priceInterval')}: {bot.price_interval.toLocaleString(undefined, { maximumFractionDigits: 4 })}</Text>
                        )}
                        {(bot.profit_spread != null && bot.profit_spread > 0) && (
                          <Text>{t('botList.profitSpread')}: {bot.profit_spread.toLocaleString(undefined, { maximumFractionDigits: 4 })}</Text>
                        )}
                        {(bot.order_quantity != null && bot.order_quantity > 0) && (
                          <Text>{t('botList.orderQuantity')}: ${bot.order_quantity.toLocaleString(undefined, { minimumFractionDigits: 2 })}</Text>
                        )}
                        {(bot.total_allocated_capital != null && bot.total_allocated_capital > 0) && (
                          <Text fontWeight="500">{t('botList.totalCapital')}: ${bot.total_allocated_capital.toLocaleString(undefined, { minimumFractionDigits: 2 })}</Text>
                        )}
                        {bot.running && liqEstimate?.valid && liqEstimate.liquidationPrice > 0 ? (
                          <Tooltip label={t('paramAdvisor.liquidationPriceTooltip')}>
                            <Text color="orange.500" fontWeight="500">
                              {t('botList.liquidationPrice')}: ${liqEstimate.liquidationPrice.toFixed(2)}
                            </Text>
                          </Tooltip>
                        ) : bot.running ? (
                          <Text color={metaColor}>{t('botList.liquidationPrice')}: -</Text>
                        ) : null}
                      </HStack>
                    </VStack>

                    <IconButton
                      as={Link}
                      to={`/bots/${bot.bot_id}`}
                      aria-label={t('botList.viewDetail')}
                      icon={<ChevronRightIcon />}
                      size="sm"
                      variant="ghost"
                      flexShrink={0}
                      onClick={(e) => e.stopPropagation()}
                    />
                  </Flex>

                  {bot.running && (bot.current_price != null || bot.total_pnl != null) && (
                    <HStack spacing={6} mt={4} pt={4} borderTopWidth="1px" borderColor={cardBorder} fontSize="sm">
                      {bot.current_price != null && (
                        <Text fontWeight="500">${bot.current_price.toLocaleString(undefined, { minimumFractionDigits: 2 })}</Text>
                      )}
                      {bot.total_pnl != null && (
                        <Text color={bot.total_pnl >= 0 ? 'green.500' : 'red.500'} fontWeight="500">
                          PnL: {bot.total_pnl >= 0 ? '+' : ''}{bot.total_pnl.toFixed(2)}
                        </Text>
                      )}
                    </HStack>
                  )}

                  <Flex mt={4} gap={2} onClick={(e) => e.stopPropagation()} align="center" justify="space-between" flexWrap="wrap">
                    <HStack spacing={2}>
                      {bot.running ? (
                        <Button
                          size="sm"
                          colorScheme="red"
                          variant="outline"
                          borderRadius="lg"
                          isLoading={actionBotId === bot.bot_id}
                          onClick={() => handleStopClick(bot)}
                        >
                          {t('botList.stop')}
                        </Button>
                      ) : (
                        <Button
                          size="sm"
                          colorScheme="green"
                          borderRadius="lg"
                          isLoading={actionBotId === bot.bot_id}
                          onClick={() => handleStart(bot.bot_id)}
                        >
                          {t('botList.start')}
                        </Button>
                      )}
                      <Button
                        size="sm"
                        colorScheme="purple"
                        variant="outline"
                        leftIcon={<TimeIcon />}
                        borderRadius="lg"
                        onClick={() => handleBacktest(bot)}
                      >
                        {t('botList.backtest')}
                      </Button>
                    </HStack>
                    <Tooltip
                      label={
                        botIdToGroup.get(bot.bot_id)
                          ? t('botList.deleteInHedgeGroupHint', { groupName: botIdToGroup.get(bot.bot_id)!.name })
                          : t('common.delete')
                      }
                    >
                      <IconButton
                        aria-label={t('common.delete')}
                        icon={<DeleteIcon />}
                        size="sm"
                        variant="ghost"
                        colorScheme="red"
                        borderRadius="lg"
                        isLoading={actionBotId === bot.bot_id}
                        onClick={(e) => {
                          e.stopPropagation()
                          handleDeleteClick(bot)
                        }}
                      />
                    </Tooltip>
                  </Flex>
                </CardBody>
              </Card>
            )
          })}
        </Grid>
      )}

      <AlertDialog isOpen={isDeleteOpen} leastDestructiveRef={cancelDeleteRef} onClose={handleDeleteCancel}>
        <AlertDialogOverlay>
          <AlertDialogContent borderRadius="2xl">
            <AlertDialogHeader fontSize="lg" fontWeight="bold">
              {t('botList.deleteConfirmTitle')}
            </AlertDialogHeader>
            <AlertDialogBody>
              {deleteTarget && t('botList.deleteConfirmDesc', { name: deleteTarget.name || deleteTarget.symbol })}
            </AlertDialogBody>
            <AlertDialogFooter>
              <Button ref={cancelDeleteRef} onClick={handleDeleteCancel} borderRadius="lg">
                {t('common.cancel')}
              </Button>
              <Button colorScheme="red" onClick={handleDeleteConfirm} ml={3} borderRadius="lg">
                {t('common.delete')}
              </Button>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialogOverlay>
      </AlertDialog>

      {backtestBotId && (
        <BotBacktestDialog
          open={isBacktestOpen}
          onClose={handleCloseBacktest}
          botId={backtestBotId}
          botName={backtestBotName}
        />
      )}

      {stopTarget && (
        <StopWithCloseConfirmDialog
          isOpen={isStopDialogOpen}
          onClose={handleStopDialogClose}
          onStopOnly={handleStopOnly}
          onStopAndClose={handleStopAndClose}
          botId={stopTarget.bot_id}
          botName={stopTarget.name || stopTarget.symbol}
        />
      )}
    </Box>
  )
}

export default BotList
