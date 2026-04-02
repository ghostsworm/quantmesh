import React, { useEffect, useState } from 'react'
import { Flex, Text, Badge, Spinner, HStack, Tooltip, Button } from '@chakra-ui/react'
import { useParams } from 'react-router-dom'
import { useSymbol } from '../contexts/SymbolContext'
import { getFundingRateCurrent, getBots, getPnLByExchange, getEventStats, getBotById } from '../services/api'
import { getPnLExchangeDefaultRangeISO } from '../constants/pnl'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

const GlobalHeaderStatus: React.FC = () => {
  const { botId } = useParams<{ botId: string }>()
  const { selectedExchange, selectedSymbol, isGlobalView, clearSelection } = useSymbol()
  const [fundingRate, setFundingRate] = useState<number | null>(null)
  const [loading, setLoading] = useState(false)
  const [runningCount, setRunningCount] = useState<number>(0)
  const [totalPnL, setTotalPnL] = useState<number | null>(null)
  const [criticalCount, setCriticalCount] = useState<number>(0)
  const [botSymbol, setBotSymbol] = useState<string | null>(null)
  const { t } = useTranslation()

  // 在 bot 详情页时获取 bot 信息用于顶部展示
  useEffect(() => {
    if (!botId) {
      setBotSymbol(null)
      return
    }
    getBotById(botId)
      .then((b) => setBotSymbol(b?.symbol || null))
      .catch(() => setBotSymbol(null))
  }, [botId])

  useEffect(() => {
    if (isGlobalView || !selectedExchange || !selectedSymbol) {
      setFundingRate(null)
      return
    }
    const fetchFundingRate = async () => {
      setLoading(true)
      try {
        const data = await getFundingRateCurrent(selectedExchange, selectedSymbol)
        if (data.rates && data.rates[selectedSymbol]) {
          setFundingRate(data.rates[selectedSymbol].rate_pct)
        } else {
          setFundingRate(null)
        }
      } catch {
        setFundingRate(null)
      } finally {
        setLoading(false)
      }
    }
    fetchFundingRate()
    const interval = setInterval(fetchFundingRate, 60000)
    return () => clearInterval(interval)
  }, [selectedExchange, selectedSymbol, isGlobalView])

  useEffect(() => {
    if (!isGlobalView) return
    const fetchGlobal = async () => {
      try {
        const { startTime, endTime } = getPnLExchangeDefaultRangeISO()
        const [botsRes, pnlRes, eventRes] = await Promise.all([
          getBots().catch(() => ({ bots: [] })),
          getPnLByExchange(startTime, endTime).catch(() => ({ exchanges: [] })),
          getEventStats().catch(() => ({ total_count: 0, critical_count: 0 })),
        ])
        setRunningCount((botsRes.bots || []).filter((b) => b.running).length)
        const total = (pnlRes.exchanges || []).reduce((s, e) => s + (e.total_pnl || 0), 0)
        setTotalPnL(total)
        setCriticalCount(eventRes.critical_count || 0)
      } catch {
        setRunningCount(0)
        setTotalPnL(null)
        setCriticalCount(0)
      }
    }
    fetchGlobal()
    const interval = setInterval(fetchGlobal, 30000)
    return () => clearInterval(interval)
  }, [isGlobalView])

  // 1. Bot 工作区模式：已选 symbol，显示交易对与资金费率
  if (!isGlobalView && selectedExchange && selectedSymbol) {
    return (
      <HStack spacing={3}>
        <Button size="xs" variant="ghost" onClick={clearSelection}>
          {t('statusBar.backToGlobal')}
        </Button>
        <Tooltip label={`${selectedExchange.toUpperCase()} ${t('statusBar.fundingRate')}`}>
          <HStack spacing={2} bg="gray.50" px={3} py={1} borderRadius="full" border="1px" borderColor="gray.100">
            <Text fontSize="10px" fontWeight="bold" color="gray.400">FR</Text>
            {loading ? (
              <Spinner size="xs" speed="0.8s" thickness="1px" />
            ) : fundingRate !== null ? (
              <Text fontSize="11px" fontWeight="bold" color={fundingRate >= 0 ? 'green.500' : 'red.500'}>
                {fundingRate >= 0 ? '+' : ''}{fundingRate.toFixed(4)}%
              </Text>
            ) : (
              <Text fontSize="11px" color="gray.400">--</Text>
            )}
          </HStack>
        </Tooltip>
        <Badge colorScheme="blue" variant="solid" fontSize="10px" borderRadius="full" px={3}>
          {selectedSymbol}
        </Badge>
      </HStack>
    )
  }

  // 2. Bot 详情页模式：在 /bots/:botId，显示 bot 信息
  if (botId) {
    return (
      <HStack spacing={3}>
        <Button as={Link} to="/bots" size="xs" variant="ghost" colorScheme="blue">
          {t('statusBar.backToBotList')}
        </Button>
        {botSymbol && (
          <Badge colorScheme="blue" variant="outline" fontSize="10px" borderRadius="full" px={3}>
            {botSymbol}
          </Badge>
        )}
        <Badge colorScheme="cyan" variant="subtle" fontSize="10px" borderRadius="full" px={3}>
          {t('headerStatus.botDetailView')}
        </Badge>
      </HStack>
    )
  }

  // 3. 全局模式：未进入任一 bot
  return (
    <HStack spacing={4}>
      <Button as={Link} to="/bots" size="xs" variant="ghost" colorScheme="blue">
        {t('sidebar.botList')}
      </Button>
      <HStack spacing={2} fontSize="xs" color="gray.600">
        <Text fontWeight="medium">{t('headerStatus.runningBots')}:</Text>
        <Badge colorScheme="green" fontSize="10px">
          {runningCount}
        </Badge>
      </HStack>
      {totalPnL !== null && (
        <HStack spacing={1} fontSize="xs">
          <Text color="gray.500">{t('headerStatus.totalPnL')}:</Text>
          <Text fontWeight="bold" color={totalPnL >= 0 ? 'green.500' : 'red.500'}>
            {totalPnL >= 0 ? '+' : ''}{totalPnL.toFixed(2)} USDT
          </Text>
        </HStack>
      )}
      <Button
        as={Link}
        to="/events"
        size="xs"
        variant="ghost"
        colorScheme={criticalCount > 0 ? 'red' : 'gray'}
      >
        {t('sidebar.eventCenter')}
        {criticalCount > 0 && (
          <Badge ml={1} colorScheme="red" fontSize="9px">
            {criticalCount}
          </Badge>
        )}
      </Button>
      <Badge colorScheme="purple" variant="subtle" fontSize="10px" borderRadius="full" px={3}>
        {t('statusBar.globalView')}
      </Badge>
    </HStack>
  )
}

export default GlobalHeaderStatus
