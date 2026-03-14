import React, { useEffect, useState, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import {
  Box,
  Container,
  Heading,
  Text,
  Select,
  FormControl,
  FormLabel,
  Button,
  Spinner,
  Center,
  Alert,
  AlertIcon,
  AlertTitle,
  AlertDescription,
  SimpleGrid,
  Stat,
  StatLabel,
  StatNumber,
  StatHelpText,
  Badge,
  VStack,
  HStack,
  Divider,
  useToast,
} from '@chakra-ui/react'
import { RepeatIcon } from '@chakra-ui/icons'
import { getKlines, getSymbols, getExchanges, type SymbolInfo } from '../services/api'
import { computeShakeStrength, computeGridFriendly } from '../utils/oscillationIndicators'

const N_BARS = 100

type TimeRangeKey = '12h' | '24h' | '72h' | '7d'

interface TimeRangeConfig {
  key: TimeRangeKey
  interval: string
  limit: number
}

const TIME_RANGES: TimeRangeConfig[] = [
  { key: '12h', interval: '5m', limit: 144 },
  { key: '24h', interval: '15m', limit: 100 },
  { key: '72h', interval: '1h', limit: 100 },
  { key: '7d', interval: '1h', limit: 168 },
]

function getShakeStrengthLabel(value: number, t: (k: string) => string): string {
  if (value > 1.5) return t('oscillation.shakeHigh')
  if (value >= 0.5) return t('oscillation.shakeMedium')
  return t('oscillation.shakeLow')
}

function getGridFriendlyLabel(value: number, t: (k: string) => string): string {
  if (value >= 0.7) return t('oscillation.gridGood')
  if (value >= 0.4) return t('oscillation.gridMedium')
  return t('oscillation.gridPoor')
}

function getGridRecommendation(
  shakeStrength: number,
  gridFriendly: number,
  mid: number,
  t: (k: string) => string
): string {
  if (gridFriendly < 0.4) return t('oscillation.recommendNo')
  if (gridFriendly < 0.7 && shakeStrength < 0.5) return t('oscillation.recommendNo')
  if (gridFriendly >= 0.7 && shakeStrength >= 0.5 && shakeStrength <= 1.5) {
    const suggestedInterval = mid * (shakeStrength / 100) * 0.5
    return t('oscillation.recommendYes', { interval: suggestedInterval.toFixed(2) })
  }
  if (gridFriendly >= 0.7 && shakeStrength > 1.5) {
    return t('oscillation.recommendYesVolatile')
  }
  return t('oscillation.recommendMaybe')
}

const OscillationAnalysis: React.FC = () => {
  const { t } = useTranslation()
  const toast = useToast()
  const [symbols, setSymbols] = useState<SymbolInfo[]>([])
  const [exchanges, setExchanges] = useState<string[]>([])
  const [selectedExchange, setSelectedExchange] = useState<string>('')
  const [selectedSymbol, setSelectedSymbol] = useState<string>('')
  const [timeRange, setTimeRange] = useState<TimeRangeKey>('24h')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [result, setResult] = useState<{
    shakeStrength: number
    gridFriendly: number
    mid: number
    barCount: number
  } | null>(null)

  const loadSymbolsAndExchanges = useCallback(async () => {
    try {
      const [symbolsRes, exchangesRes] = await Promise.all([
        getSymbols(),
        getExchanges(),
      ])
      setSymbols(symbolsRes.symbols || [])
      setExchanges(exchangesRes.exchanges || [])
      if (symbolsRes.symbols?.length && !selectedExchange) {
        const first = symbolsRes.symbols[0]
        setSelectedExchange(first.exchange)
        setSelectedSymbol(first.symbol)
      }
    } catch (err) {
      console.error('Failed to load symbols:', err)
      toast({ title: t('oscillation.loadSymbolsFailed'), status: 'error' })
    }
  }, [selectedExchange, t, toast])

  useEffect(() => {
    loadSymbolsAndExchanges()
  }, [])

  const filteredSymbols = selectedExchange
    ? symbols.filter((s) => s.exchange.toLowerCase() === selectedExchange.toLowerCase())
    : symbols

  const analyze = useCallback(async () => {
    if (!selectedExchange || !selectedSymbol) {
      toast({ title: t('oscillation.selectSymbolRequired'), status: 'warning' })
      return
    }
    const config = TIME_RANGES.find((r) => r.key === timeRange)
    if (!config) return

    setLoading(true)
    setError(null)
    setResult(null)

    try {
      const res = await getKlines(
        config.interval,
        config.limit,
        selectedExchange,
        selectedSymbol
      )
      const klines = res.klines || []
      if (klines.length < 10) {
        setError(t('oscillation.insufficientData'))
        setLoading(false)
        return
      }

      const closes = klines.map((k) => k.close)
      const usedCloses = closes.slice(-Math.min(N_BARS, closes.length))

      const shakeStrength = computeShakeStrength(usedCloses)
      const gridFriendly = computeGridFriendly(usedCloses)
      const maxC = Math.max(...usedCloses)
      const minC = Math.min(...usedCloses)
      const mid = (maxC + minC) / 2

      setResult({
        shakeStrength,
        gridFriendly,
        mid,
        barCount: usedCloses.length,
      })
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err)
      setError(msg)
      toast({ title: t('oscillation.fetchFailed'), description: msg, status: 'error' })
    } finally {
      setLoading(false)
    }
  }, [selectedExchange, selectedSymbol, timeRange, t, toast])

  useEffect(() => {
    if (selectedExchange && filteredSymbols.length && !selectedSymbol) {
      setSelectedSymbol(filteredSymbols[0].symbol)
    }
  }, [selectedExchange, filteredSymbols])

  return (
    <Container maxW="container.lg" py={6}>
      <Heading size="lg" mb={6}>
        {t('oscillation.title')}
      </Heading>
      <Text color="gray.600" mb={6} fontSize="sm">
        {t('oscillation.subtitle')}
      </Text>

      <SimpleGrid columns={{ base: 1, md: 3 }} spacing={4} mb={6}>
        <FormControl>
          <FormLabel>{t('oscillation.exchange')}</FormLabel>
          <Select
            value={selectedExchange}
            onChange={(e) => {
              setSelectedExchange(e.target.value)
              setSelectedSymbol('')
            }}
          >
            <option value="">{t('oscillation.selectExchange')}</option>
            {exchanges.map((ex) => (
              <option key={ex} value={ex}>
                {ex}
              </option>
            ))}
          </Select>
        </FormControl>
        <FormControl>
          <FormLabel>{t('oscillation.symbol')}</FormLabel>
          <Select
            value={selectedSymbol}
            onChange={(e) => setSelectedSymbol(e.target.value)}
            isDisabled={!selectedExchange}
          >
            <option value="">{t('oscillation.selectSymbol')}</option>
            {filteredSymbols.map((s) => (
              <option key={`${s.exchange}:${s.symbol}`} value={s.symbol}>
                {s.symbol}
              </option>
            ))}
          </Select>
        </FormControl>
        <FormControl>
          <FormLabel>{t('oscillation.timeRange')}</FormLabel>
          <Select
            value={timeRange}
            onChange={(e) => setTimeRange(e.target.value as TimeRangeKey)}
          >
            {TIME_RANGES.map((r) => (
              <option key={r.key} value={r.key}>
                {t(`oscillation.range${r.key}`)}
              </option>
            ))}
          </Select>
        </FormControl>
      </SimpleGrid>

      <HStack mb={6}>
        <Button
          leftIcon={<RepeatIcon />}
          colorScheme="blue"
          onClick={analyze}
          isLoading={loading}
          isDisabled={!selectedExchange || !selectedSymbol}
        >
          {t('oscillation.analyze')}
        </Button>
      </HStack>

      {error && (
        <Alert status="error" mb={6} borderRadius="md">
          <AlertIcon />
          <AlertTitle>{t('oscillation.error')}</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      {loading && (
        <Center py={12}>
          <Spinner size="lg" />
        </Center>
      )}

      {result && !loading && (
        <Box>
          <SimpleGrid columns={{ base: 1, md: 2 }} spacing={6} mb={6}>
            <Stat
              p={4}
              borderRadius="lg"
              bg="blue.50"
              borderWidth="1px"
              borderColor="blue.100"
            >
              <StatLabel>{t('oscillation.shakeStrength')}</StatLabel>
              <StatNumber fontSize="2xl">{result.shakeStrength.toFixed(3)}%</StatNumber>
              <StatHelpText>
                <Badge colorScheme="blue">{getShakeStrengthLabel(result.shakeStrength, t)}</Badge>
              </StatHelpText>
            </Stat>
            <Stat
              p={4}
              borderRadius="lg"
              bg="green.50"
              borderWidth="1px"
              borderColor="green.100"
            >
              <StatLabel>{t('oscillation.gridFriendly')}</StatLabel>
              <StatNumber fontSize="2xl">{result.gridFriendly.toFixed(3)}</StatNumber>
              <StatHelpText>
                <Badge colorScheme="green">{getGridFriendlyLabel(result.gridFriendly, t)}</Badge>
              </StatHelpText>
            </Stat>
          </SimpleGrid>

          <VStack align="stretch" spacing={4} p={4} bg="gray.50" borderRadius="lg">
            <Text fontWeight="600">{t('oscillation.recommendation')}</Text>
            <Text>
              {getGridRecommendation(
                result.shakeStrength,
                result.gridFriendly,
                result.mid,
                t
              )}
            </Text>
            <Divider />
            <Text fontSize="sm" color="gray.500">
              {t('oscillation.disclaimer')}
            </Text>
            <Text fontSize="xs" color="gray.400">
              {t('oscillation.barCount', { count: result.barCount })}
            </Text>
          </VStack>
        </Box>
      )}
    </Container>
  )
}

export default OscillationAnalysis
