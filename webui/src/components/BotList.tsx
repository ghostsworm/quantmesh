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
} from '@chakra-ui/react'
import { AddIcon, ChevronRightIcon, RepeatIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { getBots, startBot, stopBot, BotInfo } from '../services/api'

type FilterStatus = 'all' | 'running' | 'stopped'

const BotList: React.FC = () => {
  const { t } = useTranslation()
  const toast = useToast()
  const [bots, setBots] = useState<BotInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [actionBotId, setActionBotId] = useState<string | null>(null)
  const [filterStatus, setFilterStatus] = useState<FilterStatus>('all')

  const filteredBots = bots.filter((b) => {
    if (filterStatus === 'running') return b.running
    if (filterStatus === 'stopped') return !b.running
    return true
  })

  const fetchBots = async () => {
    try {
      const res = await getBots()
      setBots(res.bots || [])
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
      toast({ title: t('botList.startFailed'), status: 'error', duration: 3000 })
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
            <Card key={bot.bot_id} _hover={{ shadow: 'md' }}>
              <CardBody>
                <Flex justify="space-between" align="flex-start" mb={2}>
                  <Box>
                    <HStack spacing={2}>
                      <Badge colorScheme={bot.running ? 'green' : 'gray'} fontSize="10px">
                        {bot.running ? t('botList.running') : t('botList.stopped')}
                      </Badge>
                      {bot.risk_triggered && (
                        <Badge colorScheme="red" fontSize="10px">{t('botList.riskTriggered')}</Badge>
                      )}
                    </HStack>
                    <Heading size="sm" mt={2}>{bot.name || bot.symbol}</Heading>
                    <Text fontSize="xs" color="gray.500">{bot.exchange} · {bot.symbol} ({bot.market_type})</Text>
                  </Box>
                  <IconButton
                    as={Link}
                    to={`/bots/${bot.bot_id}`}
                    aria-label={t('botList.viewDetail')}
                    icon={<ChevronRightIcon />}
                    size="sm"
                    variant="ghost"
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
                <Flex mt={4} gap={2}>
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
                </Flex>
              </CardBody>
            </Card>
          ))}
        </Grid>
      )}
    </Box>
  )
}

export default BotList
