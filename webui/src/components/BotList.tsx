import React, { useEffect, useState } from 'react'
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
