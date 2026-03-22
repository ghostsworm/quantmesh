import React, { useCallback, useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import {
  Box,
  Button,
  Container,
  FormControl,
  FormLabel,
  Heading,
  Select,
  Text,
  VStack,
} from '@chakra-ui/react'
import { getExchanges, getSymbols, type SymbolInfo } from '../services/api'
import KlineChart from './KlineChart'

const FALLBACK_SYMBOLS: Pick<SymbolInfo, 'symbol' | 'exchange'>[] = [
  { symbol: 'BTCUSDT', exchange: 'binance' },
  { symbol: 'ETHUSDT', exchange: 'binance' },
  { symbol: 'BNBUSDT', exchange: 'binance' },
  { symbol: 'SOLUSDT', exchange: 'binance' },
  { symbol: 'XRPUSDT', exchange: 'binance' },
  { symbol: 'DOGEUSDT', exchange: 'binance' },
  { symbol: 'ADAUSDT', exchange: 'binance' },
  { symbol: 'AVAXUSDT', exchange: 'binance' },
]

const GlobalKlinePage: React.FC = () => {
  const { t } = useTranslation()
  const [searchParams, setSearchParams] = useSearchParams()
  const exchangeFromUrl = searchParams.get('exchange')
  const symbolFromUrl = searchParams.get('symbol')

  const [symbols, setSymbols] = useState<Array<Pick<SymbolInfo, 'symbol' | 'exchange'>>>([])
  const [exchanges, setExchanges] = useState<string[]>([])
  const [selectedExchange, setSelectedExchange] = useState('')
  const [selectedSymbol, setSelectedSymbol] = useState('')

  const loadSymbolsAndExchanges = useCallback(async () => {
    try {
      const [symbolsRes, exchangesRes] = await Promise.all([
        getSymbols(),
        getExchanges(),
      ])
      const symList = symbolsRes.symbols?.length
        ? symbolsRes.symbols
        : FALLBACK_SYMBOLS
      const exList = exchangesRes.exchanges?.length
        ? exchangesRes.exchanges
        : ['binance']
      setSymbols(symList)
      setExchanges(exList)
      if (!selectedExchange && exList.length) {
        setSelectedExchange(exList[0])
      }
    } catch {
      setSymbols(FALLBACK_SYMBOLS)
      setExchanges(['binance'])
      setSelectedExchange('binance')
    }
  }, [selectedExchange])

  useEffect(() => {
    loadSymbolsAndExchanges()
  }, [])

  const filteredSymbols = selectedExchange
    ? symbols.filter((s) => s.exchange?.toLowerCase() === selectedExchange.toLowerCase())
    : symbols

  useEffect(() => {
    if (selectedExchange && filteredSymbols.length && !selectedSymbol) {
      setSelectedSymbol(filteredSymbols[0].symbol)
    }
  }, [selectedExchange, filteredSymbols])

  const hasSelection = exchangeFromUrl && symbolFromUrl

  const handleShowChart = () => {
    if (selectedExchange && selectedSymbol) {
      setSearchParams({ exchange: selectedExchange, symbol: selectedSymbol })
    }
  }

  if (hasSelection) {
    return (
      <Box>
        <Box mb={2} display="flex" alignItems="center" gap={2}>
          <Button
            size="sm"
            variant="ghost"
            onClick={() => setSearchParams({})}
          >
            {t('klineChart.changeSymbol')}
          </Button>
          <Text fontSize="sm" color="gray.500">
            {exchangeFromUrl} / {symbolFromUrl}
          </Text>
        </Box>
        <KlineChart
          overrideExchange={exchangeFromUrl}
          overrideSymbol={symbolFromUrl}
        />
      </Box>
    )
  }

  return (
    <Container maxW="container.md" py={6}>
      <Heading size="lg" mb={2}>
        {t('sidebar.klineDepth')}
      </Heading>
      <Text color="gray.600" mb={6} fontSize="sm">
        {t('klineChart.globalSelectSymbol')}
      </Text>

      <VStack spacing={4} align="stretch">
        <FormControl>
          <FormLabel>{t('oscillation.exchange')}</FormLabel>
          <Select
            value={selectedExchange}
            onChange={(e) => {
              setSelectedExchange(e.target.value)
              setSelectedSymbol('')
            }}
            placeholder={t('oscillation.selectExchange')}
          >
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
            placeholder={t('oscillation.selectSymbol')}
          >
            {filteredSymbols.map((s) => (
              <option key={`${s.exchange}-${s.symbol}`} value={s.symbol}>
                {s.symbol}
              </option>
            ))}
          </Select>
        </FormControl>

        <Button
          colorScheme="blue"
          onClick={handleShowChart}
          isDisabled={!selectedExchange || !selectedSymbol}
        >
          {t('klineChart.showChart')}
        </Button>
      </VStack>
    </Container>
  )
}

export default GlobalKlinePage
