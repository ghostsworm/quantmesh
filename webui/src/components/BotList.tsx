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
} from '@chakra-ui/react'
import { AddIcon, ChevronRightIcon, RepeatIcon, DeleteIcon, TimeIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate } from 'react-router-dom'
import { getBots, getBotGroups, startBot, stopBot, deleteBot, BotInfo } from '../services/api'
import type { BotGroupResponse } from '../services/api'
import BotBacktestDialog from './BotBacktestDialog'
import { computeLiquidationPrice } from './ParamAdvisor'

type FilterStatus = 'all' | 'running' | 'stopped'

const BotList: React.FC = () => {
  const { t } = useTranslation()
  const toast = useToast()
  const navigate = useNavigate()
  const [bots, setBots] = useState<BotInfo[]>([])
  const [botGroups, setBotGroups] = useState<BotGroupResponse[]>([])
  const [loading, setLoading] = useState(true)
  const [actionBotId, setActionBotId] = useState<string | null>(null)
  const [filterStatus, setFilterStatus] = useState<FilterStatus>('all')
  const [deleteTarget, setDeleteTarget] = useState<BotInfo | null>(null)
  const { isOpen: isDeleteOpen, onOpen: onDeleteOpen, onClose: onDeleteClose } = useDisclosure()
  const cancelDeleteRef = React.useRef<HTMLButtonElement>(null)
  const stoppedCardBg = useColorModeValue('gray.50', 'whiteAlpha.50')

  // 回测对话框状态
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

  const filteredBots = bots.filter((b) => {
    if (filterStatus === 'running') return b.running
    if (filterStatus === 'stopped') return !b.running
    return true
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
      await startBot(botId)
      toast({ title: t('botList.startSuccess'), status: 'success', duration: 2000 })
      fetchBots()
    } catch (err) {
      const e = err as Error & { errorKey?: string; groupName?: string }
      const msg = e.errorKey ? t(e.errorKey, { groupName: e.groupName ?? '' }) : t('botList.startFailed')
      toast({ title: msg, status: 'error', duration: 4000 })
    } finally {
      setActionBotId(null)
    }
  }

  const handleStop = async (botId: string) => {
    setActionBotId(botId)
    try {
      await stopBot(botId)
      toast({ title: t('botList.stopSuccess'), status: 'success', duration: 2000 })
      fetchBots()
    } catch (err) {
      toast({ title: t('botList.stopFailed'), status: 'error', duration: 3000 })
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
      <Flex justify="space-between" align="center" mb={6} flexWrap="wrap" gap={4}>
        <Heading size="lg">{t('botList.title')}</Heading>
        <HStack spacing={2}>
          <Button
            as={Link}
            to="/bots/create"
            leftIcon={<AddIcon />}
            size="sm"
            colorScheme="blue"
          >
            {t('botList.createBot')}
          </Button>
          <ButtonGroup size="sm" isAttached variant="outline">
            <Button
              colorScheme={filterStatus === 'all' ? 'blue' : 'gray'}
              variant={filterStatus === 'all' ? 'solid' : 'outline'}
              onClick={() => setFilterStatus('all')}
            >
              {t('botList.filterAll')}
            </Button>
            <Button
              colorScheme={filterStatus === 'running' ? 'green' : 'gray'}
              variant={filterStatus === 'running' ? 'solid' : 'outline'}
              onClick={() => setFilterStatus('running')}
            >
              {t('botList.running')}
            </Button>
            <Button
              colorScheme={filterStatus === 'stopped' ? 'gray' : 'gray'}
              variant={filterStatus === 'stopped' ? 'solid' : 'outline'}
              onClick={() => setFilterStatus('stopped')}
            >
              {t('botList.stopped')}
            </Button>
          </ButtonGroup>
          <Button leftIcon={<RepeatIcon />} size="sm" variant="outline" onClick={fetchBots}>
            {t('common.refresh')}
          </Button>
        </HStack>
      </Flex>

      {/* 总投入资金汇总 */}
      {filteredBots.length > 0 && (() => {
        const total = filteredBots.reduce((sum, b) => sum + (b.total_allocated_capital ?? 0), 0)
        if (total <= 0) return null
        return (
          <Card mb={4} size="sm">
            <CardBody py={3}>
              <Text fontSize="sm" fontWeight="medium">
                {t('botList.totalInvestment')}: <Text as="span" color="blue.600" fontWeight="bold">${total.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}</Text>
              </Text>
            </CardBody>
          </Card>
        )
      })()}

      {filteredBots.length === 0 ? (
        <Card>
          <CardBody>
            <Text color="gray.500">
              {bots.length === 0 ? t('botList.noBots') : t('botList.noMatchFilter')}
            </Text>
          </CardBody>
        </Card>
      ) : (
        <Grid templateColumns={{ base: '1fr', md: 'repeat(2, 1fr)', lg: 'repeat(3, 1fr)' }} gap={4}>
          {filteredBots.map((bot) => (
            <Card
              key={bot.bot_id}
              bg={bot.running ? undefined : stoppedCardBg}
              _hover={{ shadow: 'md' }}
              cursor="pointer"
              onClick={() => navigate(`/bots/${bot.bot_id}`)}
            >
              <CardBody>
                <Flex justify="space-between" align="flex-start" mb={2}>
                  <Box>
                    <HStack spacing={2} flexWrap="wrap">
                      <Badge colorScheme={bot.running ? 'green' : 'gray'} fontSize="10px">
                        {bot.running ? t('botList.running') : t('botList.stopped')}
                      </Badge>
                      {bot.risk_triggered && (
                        <Badge colorScheme="red" fontSize="10px">{t('botList.riskTriggered')}</Badge>
                      )}
                      {botIdToGroup.get(bot.bot_id) && (
                        <Badge colorScheme="purple" fontSize="10px" title={botIdToGroup.get(bot.bot_id)!.name}>
                          {t('botList.hedgeGroup')}
                        </Badge>
                      )}
                    </HStack>
                    <Heading
                      size="sm"
                      mt={2}
                      color="blue.500"
                      _hover={{ textDecoration: 'underline' }}
                    >
                      {bot.name || bot.symbol}
                    </Heading>
                    <Text fontSize="xs" color="gray.500">{bot.exchange} · {bot.symbol} ({bot.market_type})</Text>

                    {/* 策略显示 */}
                    {bot.strategies && bot.strategies.length > 0 && (
                      <HStack spacing={1} mt={1} flexWrap="wrap">
                        {bot.strategies.map((strategy, idx) => (
                          <Badge
                            key={idx}
                            size="sm"
                            variant="outline"
                            fontSize="10px"
                            colorScheme="blue"
                          >
                            {strategy.name} {strategy.weight > 0 && `(${Math.round(strategy.weight * 100)}%)`}
                          </Badge>
                        ))}
                      </HStack>
                    )}

                    {/* 间距、每单金额、投入资金 */}
                    <HStack spacing={3} mt={2} fontSize="xs" color="gray.600" flexWrap="wrap">
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
                        <Text fontWeight="medium">{t('botList.totalCapital')}: ${bot.total_allocated_capital.toLocaleString(undefined, { minimumFractionDigits: 2 })}</Text>
                      )}
                    </HStack>
                  </Box>
                  <IconButton
                    as={Link}
                    to={`/bots/${bot.bot_id}`}
                    aria-label={t('botList.viewDetail')}
                    icon={<ChevronRightIcon />}
                    size="sm"
                    variant="ghost"
                    onClick={(e) => e.stopPropagation()}
                  />
                </Flex>
                {bot.running && (
                  <HStack spacing={4} mt={2} fontSize="sm">
                    {bot.current_price != null && (
                      <Text>${bot.current_price.toLocaleString(undefined, { minimumFractionDigits: 2 })}</Text>
                    )}
                    {bot.total_pnl != null && (
                      <Text color={bot.total_pnl >= 0 ? 'green.500' : 'red.500'}>
                        PnL: {bot.total_pnl >= 0 ? '+' : ''}{bot.total_pnl.toFixed(2)}
                      </Text>
                    )}
                    {/* 平仓价估算 */}
                    {(() => {
                      const leverage = bot.leverage || 1
                      const maxCapitalRatio = bot.max_capital_ratio ?? 1.0
                      const buyWindowSize = bot.buy_window_size || (bot.strategies?.find(s => s.type === 'grid') ? 50 : 20)
                      const liqEstimate = computeLiquidationPrice({
                        currentPrice: bot.current_price || 0,
                        buyWindowSize,
                        orderQuantity: bot.order_quantity || 100,
                        priceInterval: bot.price_interval || 0.0025,
                        totalCapital: bot.total_allocated_capital || 10000,
                        leverage,
                        maxCapitalRatio,
                      })
                      // 调试：如果计算失败，在控制台输出
                      if (!liqEstimate?.valid && process.env.NODE_ENV === 'development') {
                        console.debug('[BotList] 强平价计算失败:', {
                          botId: bot.bot_id,
                          currentPrice: bot.current_price,
                          buyWindowSize,
                          orderQuantity: bot.order_quantity,
                          priceInterval: bot.price_interval,
                          totalCapital: bot.total_allocated_capital,
                          leverage,
                          maxCapitalRatio,
                          liqEstimate
                        })
                      }
                      return liqEstimate?.valid && liqEstimate.liquidationPrice > 0 ? (
                        <Tooltip label={`基于最大仓位估算：${liqEstimate.positionBtc.toFixed(4)} BTC @ ${liqEstimate.avgEntryPrice.toFixed(2)} | 杠杆: ${leverage}x | 资金占用: ${Math.round(maxCapitalRatio * 100)}%`}>
                          <Text color="orange.500" fontWeight="medium">
                            强平: ${liqEstimate.liquidationPrice.toFixed(2)}
                          </Text>
                        </Tooltip>
                      ) : null
                    })()}
                  </HStack>
                )}
                <Flex mt={4} gap={2} onClick={(e) => e.stopPropagation()} align="center" justify="space-between">
                  <HStack spacing={2}>
                    {bot.running ? (
                      <Button
                        size="sm"
                        colorScheme="red"
                        variant="outline"
                        isLoading={actionBotId === bot.bot_id}
                        onClick={() => handleStop(bot.bot_id)}
                      >
                        {t('botList.stop')}
                      </Button>
                    ) : (
                      <Button
                        size="sm"
                        colorScheme="green"
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
                      onClick={() => handleBacktest(bot)}
                    >
                      {t('botList.backtest')}
                    </Button>
                  </HStack>
                  <Tooltip
                    label={
                      botIdToGroup.get(bot.bot_id)
                        ? t('botList.deleteInHedgeGroupHint', {
                            groupName: botIdToGroup.get(bot.bot_id)!.name,
                          })
                        : t('common.delete')
                    }
                  >
                    <IconButton
                      aria-label={t('common.delete')}
                      icon={<DeleteIcon />}
                      size="sm"
                      variant="ghost"
                      colorScheme="red"
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
          ))}
        </Grid>
      )}

      <AlertDialog
        isOpen={isDeleteOpen}
        leastDestructiveRef={cancelDeleteRef}
        onClose={handleDeleteCancel}
      >
        <AlertDialogOverlay>
          <AlertDialogContent>
            <AlertDialogHeader fontSize="lg" fontWeight="bold">
              {t('botList.deleteConfirmTitle')}
            </AlertDialogHeader>
            <AlertDialogBody>
              {deleteTarget && t('botList.deleteConfirmDesc', { name: deleteTarget.name || deleteTarget.symbol })}
            </AlertDialogBody>
            <AlertDialogFooter>
              <Button ref={cancelDeleteRef} onClick={handleDeleteCancel}>
                {t('common.cancel')}
              </Button>
              <Button colorScheme="red" onClick={handleDeleteConfirm} ml={3}>
                {t('common.delete')}
              </Button>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialogOverlay>
      </AlertDialog>

      {/* 回测对话框 */}
      {backtestBotId && (
        <BotBacktestDialog
          open={isBacktestOpen}
          onClose={handleCloseBacktest}
          botId={backtestBotId}
          botName={backtestBotName}
        />
      )}
    </Box>
  )
}

export default BotList
