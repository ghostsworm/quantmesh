import React, { useEffect, useState, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import {
  Box,
  Button,
  Card,
  CardBody,
  CardHeader,
  FormControl,
  FormLabel,
  Heading,
  HStack,
  Input,
  Select,
  SimpleGrid,
  Spinner,
  Table,
  Thead,
  Tbody,
  Tr,
  Th,
  Td,
  Badge,
  Text,
  useToast,
  VStack,
  Flex,
  Progress,
  Stat,
  StatLabel,
  StatNumber,
  StatHelpText,
  Divider,
} from '@chakra-ui/react'
import * as echarts from 'echarts'
import {
  postOptimizerRun,
  getOptimizerStatus,
  getOptimizerResult,
  postOptimizerStop,
  getOptimizerPrice,
  type OptimSearchSpace,
  type OptimConfig,
  type OptimResult,
  type OptimizerTaskStatus,
} from '../services/optimizer'
import { getBacktestExchanges, getBacktestSymbols, type BacktestExchangeInfo, type BacktestSymbolInfo } from '../services/backtest'
import DecimalNumberInput from './DecimalNumberInput'

const KLINE_INTERVALS = ['1m', '5m', '15m', '30m', '1h', '4h', '1d'] as const
const MARKET_TYPE = 'futures' // 网格优化默认合约

export default function OptimizerPage() {
  const toast = useToast()
  const { t } = useTranslation()
  const [exchanges, setExchanges] = useState<BacktestExchangeInfo[]>([])
  const [exchange, setExchange] = useState('binance')
  const [symbolList, setSymbolList] = useState<BacktestSymbolInfo[]>([])
  const [symbol, setSymbol] = useState('BTCUSDT')
  const [interval, setInterval] = useState('1h')
  const [days, setDays] = useState(90)
  const [startDate, setStartDate] = useState('')
  const [endDate, setEndDate] = useState('')
  const [initialCapital, setInitialCapital] = useState(10000)

  // 搜索空间
  const [priceLowMin, setPriceLowMin] = useState(2200)
  const [priceLowMax, setPriceLowMax] = useState(2400)
  const [priceLowStep, setPriceLowStep] = useState(50)
  const [priceHighMin, setPriceHighMin] = useState(2400)
  const [priceHighMax, setPriceHighMax] = useState(2800)
  const [priceHighStep, setPriceHighStep] = useState(50)
  const [gridCountMin, setGridCountMin] = useState(10)
  const [gridCountMax, setGridCountMax] = useState(30)
  const [gridCountStep, setGridCountStep] = useState(5)
  const [orderQtyMin, setOrderQtyMin] = useState(50)
  const [orderQtyMax, setOrderQtyMax] = useState(200)
  const [orderQtyStep, setOrderQtyStep] = useState(50)

  // 优化配置
  const [method, setMethod] = useState<'grid' | 'bayesian' | 'genetic'>('grid')
  const [lambda, setLambda] = useState(0.5)
  const [maxIterations, setMaxIterations] = useState(50)

  // 任務状態
  const [taskId, setTaskId] = useState<string | null>(null)
  const [taskStatus, setTaskStatus] = useState<OptimizerTaskStatus | null>(null)
  const [result, setResult] = useState<OptimResult | null>(null)
  const [running, setRunning] = useState(false)

  const chartRef = useRef<HTMLDivElement>(null)
  const chartInstance = useRef<echarts.ECharts | null>(null)

  // 根據價格计算步长
  const calcStep = (price: number) => {
    if (price >= 50000) return 500
    if (price >= 1000) return 50
    if (price >= 100) return 10
    if (price >= 10) return 1
    return 0.1
  }

  // 从 API 拉取交易所列表
  useEffect(() => {
    getBacktestExchanges()
      .then((r) => {
        if (r.success && r.exchanges?.length) {
          setExchanges(r.exchanges)
          const configured = r.exchanges.find((e) => e.is_configured)
          const first = r.exchanges[0]
          if (configured) setExchange(configured.exchange)
          else if (first) setExchange(first.exchange)
        }
      })
      .catch(() => setExchanges([]))
  }, [])

  // 切换交易所时从 API 拉取该交易所的交易对列表
  useEffect(() => {
    if (!exchange) return
    getBacktestSymbols(exchange, MARKET_TYPE)
      .then((r) => {
        if (r.success && r.symbols?.length) {
          setSymbolList(r.symbols)
          const hasCurrent = r.symbols.some((s) => s.symbol === symbol)
          if (!hasCurrent && r.symbols[0]) setSymbol(r.symbols[0].symbol)
        } else {
          setSymbolList([])
        }
      })
      .catch(() => setSymbolList([]))
  }, [exchange])

  // 切换交易對時，根據當前價格自动初始化搜索空间
  useEffect(() => {
    if (!symbol) return
    getOptimizerPrice(exchange, symbol)
      .then((r) => {
        if (r.price <= 0) return
        const p = r.price
        const step = calcStep(p)
        setPriceLowMin(Math.round(p * 0.85 * 100) / 100)
        setPriceLowMax(Math.round(p * 0.95 * 100) / 100)
        setPriceLowStep(step)
        setPriceHighMin(Math.round(p * 1.05 * 100) / 100)
        setPriceHighMax(Math.round(p * 1.2 * 100) / 100)
        setPriceHighStep(step)
        // orderQty 根據價格量级設置合理默认值
        const baseQty = p >= 50000 ? 100 : p >= 1000 ? 50 : 20
        setOrderQtyMin(baseQty)
        setOrderQtyMax(baseQty * 4)
        setOrderQtyStep(Math.max(10, Math.floor(baseQty / 2)))
      })
      .catch(() => {})
  }, [exchange, symbol])

  const toNum = (v: number | string | undefined, fallback: number) => {
    if (typeof v === 'number' && !Number.isNaN(v)) return v
    const n = parseFloat(String(v))
    return !Number.isNaN(n) ? n : fallback
  }

  useEffect(() => {
    const end = new Date()
    const start = new Date()
    start.setDate(start.getDate() - toNum(days, 90))
    setStartDate(start.toISOString().slice(0, 10))
    setEndDate(end.toISOString().slice(0, 10))
  }, [days])

  // 輪詢任務状態
  useEffect(() => {
    if (!taskId) return
    const poll = async () => {
      try {
        const status = await getOptimizerStatus(taskId)
        setTaskStatus(status)
        if (status.status === 'completed') {
          const res = await getOptimizerResult(taskId)
          if (res.result) setResult(res.result)
          setRunning(false)
        } else if (status.status === 'failed' || status.status === 'stopped') {
          setRunning(false)
        } else {
          // pending, loading_data, running 都繼續輪詢
          setTimeout(poll, 2000)
        }
      } catch (e) {
        console.error(e)
        setTimeout(poll, 3000)
      }
    }
    poll()
  }, [taskId])

  // 绘制热力图
  useEffect(() => {
    if (!chartRef.current || !result?.heatmap_data) return
    if (!chartInstance.current) {
      chartInstance.current = echarts.init(chartRef.current)
    }
    const hm = result.heatmap_data
    // 轉换為 ECharts 格式: [xIdx, yIdx, value]
    const data: [number, number, number][] = []
    hm.data.forEach((row, yi) => {
      row.forEach((val, xi) => {
        if (!isNaN(val)) data.push([xi, yi, val])
      })
    })
    const minVal = Math.min(...data.map((d) => d[2]))
    const maxVal = Math.max(...data.map((d) => d[2]))
    chartInstance.current.setOption({
      tooltip: { position: 'top' },
      grid: { top: 40, left: 80, right: 40, bottom: 60 },
      xAxis: {
        type: 'category',
        data: hm.x_axis.map(String),
        name: 'GridCount',
        splitArea: { show: true },
      },
      yAxis: {
        type: 'category',
        data: hm.y_axis.map(String),
        name: 'PriceRange',
        splitArea: { show: true },
      },
      visualMap: {
        min: minVal,
        max: maxVal,
        calculable: true,
        orient: 'horizontal',
        left: 'center',
        bottom: 0,
        inRange: { color: ['#313695', '#4575b4', '#74add1', '#abd9e9', '#e0f3f8', '#ffffbf', '#fee090', '#fdae61', '#f46d43', '#d73027', '#a50026'] },
      },
      series: [
        {
          name: 'Score',
          type: 'heatmap',
          data,
          label: { show: false },
          emphasis: { itemStyle: { shadowBlur: 10, shadowColor: 'rgba(0, 0, 0, 0.5)' } },
        },
      ],
    })
  }, [result])

  const handleRun = async () => {
    if (!symbol || !startDate || !endDate) {
      toast({ title: t('optimizer.fillAllParams'), status: 'warning' })
      return
    }
    const searchSpace: OptimSearchSpace = {
      price_low_range: { min: toNum(priceLowMin, 2200), max: toNum(priceLowMax, 2400), step: toNum(priceLowStep, 50) },
      price_high_range: { min: toNum(priceHighMin, 2400), max: toNum(priceHighMax, 2800), step: toNum(priceHighStep, 50) },
      grid_count_range: { min: Math.round(toNum(gridCountMin, 10)), max: Math.round(toNum(gridCountMax, 30)), step: Math.round(toNum(gridCountStep, 5)) },
      order_qty_range: { min: toNum(orderQtyMin, 50), max: toNum(orderQtyMax, 200), step: toNum(orderQtyStep, 50) },
    }
    const config: OptimConfig = {
      method,
      lambda: toNum(lambda, 0.5),
      max_iterations: Math.round(toNum(maxIterations, 50)),
      tolerance: 1e-4,
      parallelism: 0,
    }
    setRunning(true)
    setResult(null)
    setTaskStatus(null)
    try {
      const start = new Date(startDate)
      const end = new Date(endDate)
      end.setHours(23, 59, 59, 999)
      const res = await postOptimizerRun({
        exchange,
        symbol,
        interval,
        start_time: start.toISOString(),
        end_time: end.toISOString(),
        initial_capital: toNum(initialCapital, 10000),
        search_space: searchSpace,
        config,
      })
      if (res.success) {
        setTaskId(res.task_id)
        toast({ title: t('optimizer.taskStarted'), status: 'success' })
      } else {
        toast({ title: res.message || t('optimizer.startFailed'), status: 'error' })
        setRunning(false)
      }
    } catch (e: unknown) {
      toast({ title: (e as Error).message || t('optimizer.requestFailed'), status: 'error' })
      setRunning(false)
    }
  }

  const handleStop = async () => {
    if (!taskId) return
    try {
      await postOptimizerStop(taskId)
      toast({ title: t('optimizer.stopRequested'), status: 'info' })
    } catch (e: unknown) {
      toast({ title: (e as Error).message || t('optimizer.stopFailed'), status: 'error' })
    }
  }

  return (
    <Box>
      <Heading size="md" mb={4}>{t('optimizer.title')}</Heading>
      <SimpleGrid columns={{ base: 1, lg: 2 }} spacing={4}>
        {/* 左侧：配置 */}
        <VStack spacing={4} align="stretch">
          <Card>
            <CardHeader fontWeight="600">{t('optimizer.dataConfig')}</CardHeader>
            <CardBody>
              <SimpleGrid columns={2} spacing={3}>
                <FormControl>
                  <FormLabel fontSize="sm">{t('optimizer.exchange')}</FormLabel>
                  <Select size="sm" value={exchange} onChange={(e) => setExchange(e.target.value)} placeholder={t('optimizer.selectExchange')}>
                    {exchanges.map((ex) => (
                      <option key={ex.exchange} value={ex.exchange}>
                        {ex.exchange.toUpperCase()}
                        {ex.is_configured ? ` (${t('optimizer.configured')})` : ''}
                      </option>
                    ))}
                  </Select>
                </FormControl>
                <FormControl>
                  <FormLabel fontSize="sm">{t('optimizer.tradingPair')}</FormLabel>
                  <Select size="sm" value={symbol} onChange={(e) => setSymbol(e.target.value)} placeholder={t('optimizer.selectTradingPair')}>
                    {symbolList.map((s) => (
                      <option key={s.symbol} value={s.symbol}>
                        {s.symbol}
                        {s.is_configured ? ` (${t('optimizer.configured')})` : ''}
                      </option>
                    ))}
                  </Select>
                </FormControl>
                <FormControl>
                  <FormLabel fontSize="sm">{t('optimizer.klineInterval')}</FormLabel>
                  <Select size="sm" value={interval} onChange={(e) => setInterval(e.target.value)}>
                    {KLINE_INTERVALS.map((i) => <option key={i} value={i}>{i}</option>)}
                  </Select>
                </FormControl>
                <FormControl>
                  <FormLabel fontSize="sm">{t('optimizer.backtestDays')}</FormLabel>
                  <DecimalNumberInput size="sm" value={days} min={7} max={365} step={1} onChange={(v) => setDays(v ?? 7)} />
                </FormControl>
                <FormControl>
                  <FormLabel fontSize="sm">{t('optimizer.initialCapital')}</FormLabel>
                  <DecimalNumberInput size="sm" value={initialCapital} min={100} step={0.01} onChange={(v) => setInitialCapital(v ?? 100)} />
                </FormControl>
                <FormControl>
                  <FormLabel fontSize="sm">{t('optimizer.startDate')}</FormLabel>
                  <Input size="sm" type="date" value={startDate} onChange={(e) => setStartDate(e.target.value)} />
                </FormControl>
                <FormControl>
                  <FormLabel fontSize="sm">{t('optimizer.endDate')}</FormLabel>
                  <Input size="sm" type="date" value={endDate} onChange={(e) => setEndDate(e.target.value)} />
                </FormControl>
              </SimpleGrid>
            </CardBody>
          </Card>

          <Card>
            <CardHeader fontWeight="600">{t('optimizer.searchSpace')}</CardHeader>
            <CardBody>
              <Text fontSize="xs" color="gray.500" mb={2}>{t('optimizer.priceLowRange')}</Text>
              <HStack mb={3}>
                <FormControl><FormLabel fontSize="xs">Min</FormLabel><DecimalNumberInput size="sm" value={priceLowMin} step={0.01} onChange={(v) => setPriceLowMin(v ?? 0)} /></FormControl>
                <FormControl><FormLabel fontSize="xs">Max</FormLabel><DecimalNumberInput size="sm" value={priceLowMax} step={0.01} onChange={(v) => setPriceLowMax(v ?? 0)} /></FormControl>
                <FormControl><FormLabel fontSize="xs">Step</FormLabel><DecimalNumberInput size="sm" value={priceLowStep} step={0.01} onChange={(v) => setPriceLowStep(v ?? 0)} /></FormControl>
              </HStack>
              <Text fontSize="xs" color="gray.500" mb={2}>{t('optimizer.priceHighRange')}</Text>
              <HStack mb={3}>
                <FormControl><FormLabel fontSize="xs">Min</FormLabel><DecimalNumberInput size="sm" value={priceHighMin} step={0.01} onChange={(v) => setPriceHighMin(v ?? 0)} /></FormControl>
                <FormControl><FormLabel fontSize="xs">Max</FormLabel><DecimalNumberInput size="sm" value={priceHighMax} step={0.01} onChange={(v) => setPriceHighMax(v ?? 0)} /></FormControl>
                <FormControl><FormLabel fontSize="xs">Step</FormLabel><DecimalNumberInput size="sm" value={priceHighStep} step={0.01} onChange={(v) => setPriceHighStep(v ?? 0)} /></FormControl>
              </HStack>
              <Text fontSize="xs" color="gray.500" mb={2}>{t('optimizer.gridCountRange')}</Text>
              <HStack mb={3}>
                <FormControl><FormLabel fontSize="xs">Min</FormLabel><DecimalNumberInput size="sm" value={gridCountMin} step={1} onChange={(v) => setGridCountMin(v ?? 0)} /></FormControl>
                <FormControl><FormLabel fontSize="xs">Max</FormLabel><DecimalNumberInput size="sm" value={gridCountMax} step={1} onChange={(v) => setGridCountMax(v ?? 0)} /></FormControl>
                <FormControl><FormLabel fontSize="xs">Step</FormLabel><DecimalNumberInput size="sm" value={gridCountStep} step={1} onChange={(v) => setGridCountStep(v ?? 0)} /></FormControl>
              </HStack>
              <Text fontSize="xs" color="gray.500" mb={2}>{t('optimizer.orderQtyRange')}</Text>
              <HStack>
                <FormControl><FormLabel fontSize="xs">Min</FormLabel><DecimalNumberInput size="sm" value={orderQtyMin} step={0.01} onChange={(v) => setOrderQtyMin(v ?? 0)} /></FormControl>
                <FormControl><FormLabel fontSize="xs">Max</FormLabel><DecimalNumberInput size="sm" value={orderQtyMax} step={0.01} onChange={(v) => setOrderQtyMax(v ?? 0)} /></FormControl>
                <FormControl><FormLabel fontSize="xs">Step</FormLabel><DecimalNumberInput size="sm" value={orderQtyStep} step={0.01} onChange={(v) => setOrderQtyStep(v ?? 0)} /></FormControl>
              </HStack>
            </CardBody>
          </Card>

          <Card>
            <CardHeader fontWeight="600">{t('optimizer.algorithm')}</CardHeader>
            <CardBody>
              <SimpleGrid columns={3} spacing={3}>
                <FormControl>
                  <FormLabel fontSize="sm">{t('optimizer.method')}</FormLabel>
                  <Select size="sm" value={method} onChange={(e) => setMethod(e.target.value as 'grid' | 'bayesian' | 'genetic')}>
                    <option value="grid">{t('optimizer.methodGrid')}</option>
                    <option value="bayesian">{t('optimizer.methodBayesian')}</option>
                    <option value="genetic">{t('optimizer.methodGenetic')}</option>
                  </Select>
                </FormControl>
                <FormControl>
                  <FormLabel fontSize="sm">{t('optimizer.riskWeight')}</FormLabel>
                  <DecimalNumberInput size="sm" value={lambda} min={0} max={1} step={0.01} onChange={(v) => setLambda(v ?? 0)} />
                </FormControl>
                <FormControl>
                  <FormLabel fontSize="sm">{t('optimizer.maxIterations')}</FormLabel>
                  <DecimalNumberInput size="sm" value={maxIterations} min={10} max={500} step={1} onChange={(v) => setMaxIterations(v ?? 10)} />
                </FormControl>
              </SimpleGrid>
              <HStack mt={4}>
                <Button colorScheme="blue" onClick={handleRun} isLoading={running} isDisabled={running}>
                  {t('optimizer.startOptimization')}
                </Button>
                {running && <Button colorScheme="red" variant="outline" onClick={handleStop}>{t('optimizer.stop')}</Button>}
              </HStack>
            </CardBody>
          </Card>
        </VStack>

        {/* 右侧：結果 */}
        <VStack spacing={4} align="stretch">
          {taskStatus && (
            <Card>
              <CardHeader fontWeight="600">{t('optimizer.taskStatus')}</CardHeader>
              <CardBody>
                <HStack mb={2}>
                  <Badge colorScheme={
                    taskStatus.status === 'completed' ? 'green' :
                    taskStatus.status === 'running' ? 'blue' :
                    taskStatus.status === 'loading_data' ? 'purple' :
                    taskStatus.status === 'failed' ? 'red' : 'gray'
                  }>
                    {taskStatus.status === 'loading_data' ? t('optimizer.statusLoadingData') :
                     taskStatus.status === 'running' ? t('optimizer.statusRunning') :
                     taskStatus.status === 'completed' ? t('optimizer.statusCompleted') :
                     taskStatus.status === 'failed' ? t('optimizer.statusFailed') :
                     taskStatus.status === 'pending' ? t('optimizer.statusPending') :
                     taskStatus.status === 'stopped' ? t('optimizer.statusStopped') :
                     taskStatus.status}
                  </Badge>
                  <Text fontSize="sm" color="gray.500">ID: {taskStatus.task_id}</Text>
                </HStack>
                {taskStatus.status === 'loading_data' && (
                  <VStack align="start" spacing={1}>
                    <Text fontSize="sm" color="purple.600">{t('optimizer.downloadingKlines')}</Text>
                    <Progress size="sm" isIndeterminate colorScheme="purple" w="100%" />
                  </VStack>
                )}
                {taskStatus.status === 'running' && (
                  <Progress size="sm" isIndeterminate colorScheme="blue" />
                )}
                {taskStatus.error && <Text fontSize="sm" color="red.500">{taskStatus.error}</Text>}
              </CardBody>
            </Card>
          )}

          {result && (
            <>
              <Card>
                <CardHeader fontWeight="600">{t('optimizer.bestParams')}</CardHeader>
                <CardBody>
                  <SimpleGrid columns={4} spacing={2}>
                    <Stat size="sm">
                      <StatLabel>{t('optimizer.priceLow')}</StatLabel>
                      <StatNumber fontSize="md">{result.best_params.price_low?.toFixed(2) || '-'}</StatNumber>
                    </Stat>
                    <Stat size="sm">
                      <StatLabel>{t('optimizer.priceHigh')}</StatLabel>
                      <StatNumber fontSize="md">{result.best_params.price_high?.toFixed(2) || '-'}</StatNumber>
                    </Stat>
                    <Stat size="sm">
                      <StatLabel>{t('optimizer.gridCount')}</StatLabel>
                      <StatNumber fontSize="md">{result.best_params.grid_count || '-'}</StatNumber>
                    </Stat>
                    <Stat size="sm">
                      <StatLabel>{t('optimizer.orderAmount')}</StatLabel>
                      <StatNumber fontSize="md">{result.best_params.order_quantity?.toFixed(0) || '-'}</StatNumber>
                    </Stat>
                  </SimpleGrid>
                  <Divider my={3} />
                  <SimpleGrid columns={4} spacing={2}>
                    <Stat size="sm">
                      <StatLabel>{t('optimizer.bestScore')}</StatLabel>
                      <StatNumber fontSize="md" color="blue.600">{result.best_score?.toFixed(4) || '-'}</StatNumber>
                    </Stat>
                    <Stat size="sm">
                      <StatLabel>{t('optimizer.annualizedReturn')}</StatLabel>
                      <StatNumber fontSize="md">{result.best_metrics?.annualized_return?.toFixed(4) || '-'}%</StatNumber>
                    </Stat>
                    <Stat size="sm">
                      <StatLabel>{t('optimizer.maxDrawdown')}</StatLabel>
                      <StatNumber fontSize="md">{result.best_metrics?.max_drawdown?.toFixed(4) || '-'}%</StatNumber>
                    </Stat>
                    <Stat size="sm">
                      <StatLabel>{t('optimizer.sharpeRatio')}</StatLabel>
                      <StatNumber fontSize="md">{result.best_metrics?.sharpe_ratio?.toFixed(4) || '-'}</StatNumber>
                    </Stat>
                  </SimpleGrid>
                  <Text fontSize="xs" color="gray.500" mt={2}>
                    {t('optimizer.methodLabel')}: {result.method} | {t('optimizer.iterations')}: {result.iterations} | {t('optimizer.elapsed')}: {(result.elapsed / 1e9).toFixed(1)}s
                  </Text>
                </CardBody>
              </Card>

              {result.heatmap_data && (
                <Card>
                  <CardHeader fontWeight="600">{t('optimizer.heatmap')}</CardHeader>
                  <CardBody>
                    <Box ref={chartRef} h="300px" />
                  </CardBody>
                </Card>
              )}

              {result.all_results && result.all_results.length > 0 && (
                <Card>
                  <CardHeader fontWeight="600">{t('optimizer.topCombinations')}</CardHeader>
                  <CardBody>
                    <Box overflowX="auto">
                      <Table size="sm">
                        <Thead>
                          <Tr>
                            <Th>P_low</Th>
                            <Th>P_high</Th>
                            <Th>GridCount</Th>
                            <Th>OrderQty</Th>
                            <Th>Score</Th>
                            <Th>{t('optimizer.annualizedReturn')}</Th>
                            <Th>{t('optimizer.maxDrawdown')}</Th>
                          </Tr>
                        </Thead>
                        <Tbody>
                          {[...result.all_results]
                            .sort((a, b) => b.score - a.score)
                            .slice(0, 10)
                            .map((r, i) => (
                              <Tr key={i} bg={i === 0 ? 'green.50' : undefined}>
                                <Td>{r.params.price_low?.toFixed(0)}</Td>
                                <Td>{r.params.price_high?.toFixed(0)}</Td>
                                <Td>{r.params.grid_count}</Td>
                                <Td>{r.params.order_quantity?.toFixed(0)}</Td>
                                <Td fontWeight="600">{r.score?.toFixed(4)}</Td>
                                <Td>{r.metrics?.annualized_return?.toFixed(4)}%</Td>
                                <Td>{r.metrics?.max_drawdown?.toFixed(4)}%</Td>
                              </Tr>
                            ))}
                        </Tbody>
                      </Table>
                    </Box>
                  </CardBody>
                </Card>
              )}
            </>
          )}

          {!result && !running && (
            <Flex justify="center" align="center" h="200px" color="gray.400">
              <Text>{t('optimizer.placeholderText')}</Text>
            </Flex>
          )}
          {running && !result && (
            <Flex justify="center" align="center" h="200px">
              <VStack>
                <Spinner size="lg" color="blue.500" />
                <Text color="gray.500">{t('optimizer.optimizing')}</Text>
              </VStack>
            </Flex>
          )}
        </VStack>
      </SimpleGrid>
    </Box>
  )
}
