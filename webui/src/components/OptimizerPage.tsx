import React, { useEffect, useState, useRef } from 'react'
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
  NumberInput,
  NumberInputField,
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
import { getSymbols, type SymbolInfo } from '../services/api'

const KLINE_INTERVALS = ['1m', '5m', '15m', '30m', '1h', '4h', '1d'] as const
const COMMON_SYMBOLS = ['BTCUSDT', 'ETHUSDT', 'BNBUSDT', 'SOLUSDT', 'PAXGUSDT', 'XRPUSDT', 'DOGEUSDT', 'ADAUSDT']

export default function OptimizerPage() {
  const toast = useToast()
  const [exchange, setExchange] = useState('binance')
  const [symbols, setSymbols] = useState<SymbolInfo[]>([])
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

  useEffect(() => {
    getSymbols()
      .then((r) => setSymbols(r.symbols || []))
      .catch(() => setSymbols([]))
  }, [])

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

  useEffect(() => {
    const end = new Date()
    const start = new Date()
    start.setDate(start.getDate() - days)
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
      toast({ title: '请填写完整参數', status: 'warning' })
      return
    }
    const searchSpace: OptimSearchSpace = {
      price_low_range: { min: priceLowMin, max: priceLowMax, step: priceLowStep },
      price_high_range: { min: priceHighMin, max: priceHighMax, step: priceHighStep },
      grid_count_range: { min: gridCountMin, max: gridCountMax, step: gridCountStep },
      order_qty_range: { min: orderQtyMin, max: orderQtyMax, step: orderQtyStep },
    }
    const config: OptimConfig = {
      method,
      lambda,
      max_iterations: maxIterations,
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
        initial_capital: initialCapital,
        search_space: searchSpace,
        config,
      })
      if (res.success) {
        setTaskId(res.task_id)
        toast({ title: '优化任務已啟动', status: 'success' })
      } else {
        toast({ title: res.message || '啟动失败', status: 'error' })
        setRunning(false)
      }
    } catch (e: unknown) {
      toast({ title: (e as Error).message || '请求失败', status: 'error' })
      setRunning(false)
    }
  }

  const handleStop = async () => {
    if (!taskId) return
    try {
      await postOptimizerStop(taskId)
      toast({ title: '已发送停止请求', status: 'info' })
    } catch (e: unknown) {
      toast({ title: (e as Error).message || '停止失败', status: 'error' })
    }
  }

  return (
    <Box>
      <Heading size="md" mb={4}>网格参數优化</Heading>
      <SimpleGrid columns={{ base: 1, lg: 2 }} spacing={4}>
        {/* 左侧：配置 */}
        <VStack spacing={4} align="stretch">
          <Card>
            <CardHeader fontWeight="600">1. 數據配置</CardHeader>
            <CardBody>
              <SimpleGrid columns={2} spacing={3}>
                <FormControl>
                  <FormLabel fontSize="sm">交易所</FormLabel>
                  <Select size="sm" value={exchange} onChange={(e) => setExchange(e.target.value)}>
                    <option value="binance">Binance</option>
                    <option value="bitget">Bitget</option>
                  </Select>
                </FormControl>
                <FormControl>
                  <FormLabel fontSize="sm">交易對</FormLabel>
                  <Select size="sm" value={symbol} onChange={(e) => setSymbol(e.target.value)}>
                    {(() => {
                      const fromApi = symbols
                        .filter((s) => s.exchange?.toLowerCase() === exchange)
                        .map((s) => s.symbol)
                      const merged = [...new Set([...COMMON_SYMBOLS, ...fromApi])]
                      return merged.map((sym) => (
                        <option key={sym} value={sym}>{sym}</option>
                      ))
                    })()}
                  </Select>
                </FormControl>
                <FormControl>
                  <FormLabel fontSize="sm">K 線周期</FormLabel>
                  <Select size="sm" value={interval} onChange={(e) => setInterval(e.target.value)}>
                    {KLINE_INTERVALS.map((i) => <option key={i} value={i}>{i}</option>)}
                  </Select>
                </FormControl>
                <FormControl>
                  <FormLabel fontSize="sm">回测天數</FormLabel>
                  <NumberInput size="sm" value={days} min={7} max={365} onChange={(_s, v) => setDays(v)}>
                    <NumberInputField />
                  </NumberInput>
                </FormControl>
                <FormControl>
                  <FormLabel fontSize="sm">初始资金 (USDT)</FormLabel>
                  <NumberInput size="sm" value={initialCapital} min={100} onChange={(_s, v) => setInitialCapital(v)}>
                    <NumberInputField />
                  </NumberInput>
                </FormControl>
                <FormControl>
                  <FormLabel fontSize="sm">开始日期</FormLabel>
                  <Input size="sm" type="date" value={startDate} onChange={(e) => setStartDate(e.target.value)} />
                </FormControl>
                <FormControl>
                  <FormLabel fontSize="sm">結束日期</FormLabel>
                  <Input size="sm" type="date" value={endDate} onChange={(e) => setEndDate(e.target.value)} />
                </FormControl>
              </SimpleGrid>
            </CardBody>
          </Card>

          <Card>
            <CardHeader fontWeight="600">2. 搜索空间</CardHeader>
            <CardBody>
              <Text fontSize="xs" color="gray.500" mb={2}>價格下限範圍</Text>
              <HStack mb={3}>
                <FormControl><FormLabel fontSize="xs">Min</FormLabel><NumberInput size="sm" value={priceLowMin} onChange={(_s, v) => setPriceLowMin(v)}><NumberInputField /></NumberInput></FormControl>
                <FormControl><FormLabel fontSize="xs">Max</FormLabel><NumberInput size="sm" value={priceLowMax} onChange={(_s, v) => setPriceLowMax(v)}><NumberInputField /></NumberInput></FormControl>
                <FormControl><FormLabel fontSize="xs">Step</FormLabel><NumberInput size="sm" value={priceLowStep} onChange={(_s, v) => setPriceLowStep(v)}><NumberInputField /></NumberInput></FormControl>
              </HStack>
              <Text fontSize="xs" color="gray.500" mb={2}>價格上限範圍</Text>
              <HStack mb={3}>
                <FormControl><FormLabel fontSize="xs">Min</FormLabel><NumberInput size="sm" value={priceHighMin} onChange={(_s, v) => setPriceHighMin(v)}><NumberInputField /></NumberInput></FormControl>
                <FormControl><FormLabel fontSize="xs">Max</FormLabel><NumberInput size="sm" value={priceHighMax} onChange={(_s, v) => setPriceHighMax(v)}><NumberInputField /></NumberInput></FormControl>
                <FormControl><FormLabel fontSize="xs">Step</FormLabel><NumberInput size="sm" value={priceHighStep} onChange={(_s, v) => setPriceHighStep(v)}><NumberInputField /></NumberInput></FormControl>
              </HStack>
              <Text fontSize="xs" color="gray.500" mb={2}>网格數量範圍</Text>
              <HStack mb={3}>
                <FormControl><FormLabel fontSize="xs">Min</FormLabel><NumberInput size="sm" value={gridCountMin} onChange={(_s, v) => setGridCountMin(v)}><NumberInputField /></NumberInput></FormControl>
                <FormControl><FormLabel fontSize="xs">Max</FormLabel><NumberInput size="sm" value={gridCountMax} onChange={(_s, v) => setGridCountMax(v)}><NumberInputField /></NumberInput></FormControl>
                <FormControl><FormLabel fontSize="xs">Step</FormLabel><NumberInput size="sm" value={gridCountStep} onChange={(_s, v) => setGridCountStep(v)}><NumberInputField /></NumberInput></FormControl>
              </HStack>
              <Text fontSize="xs" color="gray.500" mb={2}>單笔订單金額範圍 (USDT)</Text>
              <HStack>
                <FormControl><FormLabel fontSize="xs">Min</FormLabel><NumberInput size="sm" value={orderQtyMin} onChange={(_s, v) => setOrderQtyMin(v)}><NumberInputField /></NumberInput></FormControl>
                <FormControl><FormLabel fontSize="xs">Max</FormLabel><NumberInput size="sm" value={orderQtyMax} onChange={(_s, v) => setOrderQtyMax(v)}><NumberInputField /></NumberInput></FormControl>
                <FormControl><FormLabel fontSize="xs">Step</FormLabel><NumberInput size="sm" value={orderQtyStep} onChange={(_s, v) => setOrderQtyStep(v)}><NumberInputField /></NumberInput></FormControl>
              </HStack>
            </CardBody>
          </Card>

          <Card>
            <CardHeader fontWeight="600">3. 优化算法</CardHeader>
            <CardBody>
              <SimpleGrid columns={3} spacing={3}>
                <FormControl>
                  <FormLabel fontSize="sm">方法</FormLabel>
                  <Select size="sm" value={method} onChange={(e) => setMethod(e.target.value as 'grid' | 'bayesian' | 'genetic')}>
                    <option value="grid">网格搜索</option>
                    <option value="bayesian">贝叶斯优化</option>
                    <option value="genetic">遗傳算法</option>
                  </Select>
                </FormControl>
                <FormControl>
                  <FormLabel fontSize="sm">风險权重 (λ)</FormLabel>
                  <NumberInput size="sm" value={lambda} min={0} max={1} step={0.1} onChange={(_s, v) => setLambda(v)}>
                    <NumberInputField />
                  </NumberInput>
                </FormControl>
                <FormControl>
                  <FormLabel fontSize="sm">最大迭代</FormLabel>
                  <NumberInput size="sm" value={maxIterations} min={10} max={500} onChange={(_s, v) => setMaxIterations(v)}>
                    <NumberInputField />
                  </NumberInput>
                </FormControl>
              </SimpleGrid>
              <HStack mt={4}>
                <Button colorScheme="blue" onClick={handleRun} isLoading={running} isDisabled={running}>
                  开始优化
                </Button>
                {running && <Button colorScheme="red" variant="outline" onClick={handleStop}>停止</Button>}
              </HStack>
            </CardBody>
          </Card>
        </VStack>

        {/* 右侧：結果 */}
        <VStack spacing={4} align="stretch">
          {taskStatus && (
            <Card>
              <CardHeader fontWeight="600">任務状態</CardHeader>
              <CardBody>
                <HStack mb={2}>
                  <Badge colorScheme={
                    taskStatus.status === 'completed' ? 'green' :
                    taskStatus.status === 'running' ? 'blue' :
                    taskStatus.status === 'loading_data' ? 'purple' :
                    taskStatus.status === 'failed' ? 'red' : 'gray'
                  }>
                    {taskStatus.status === 'loading_data' ? '加載數據中' :
                     taskStatus.status === 'running' ? '优化中' :
                     taskStatus.status === 'completed' ? '已完成' :
                     taskStatus.status === 'failed' ? '失敗' :
                     taskStatus.status === 'pending' ? '等待中' :
                     taskStatus.status === 'stopped' ? '已停止' :
                     taskStatus.status}
                  </Badge>
                  <Text fontSize="sm" color="gray.500">ID: {taskStatus.task_id}</Text>
                </HStack>
                {taskStatus.status === 'loading_data' && (
                  <VStack align="start" spacing={1}>
                    <Text fontSize="sm" color="purple.600">正在從交易所下載歷史K線數據...</Text>
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
                <CardHeader fontWeight="600">最优参數</CardHeader>
                <CardBody>
                  <SimpleGrid columns={4} spacing={2}>
                    <Stat size="sm">
                      <StatLabel>價格下限</StatLabel>
                      <StatNumber fontSize="md">{result.best_params.price_low?.toFixed(2) || '-'}</StatNumber>
                    </Stat>
                    <Stat size="sm">
                      <StatLabel>價格上限</StatLabel>
                      <StatNumber fontSize="md">{result.best_params.price_high?.toFixed(2) || '-'}</StatNumber>
                    </Stat>
                    <Stat size="sm">
                      <StatLabel>网格數量</StatLabel>
                      <StatNumber fontSize="md">{result.best_params.grid_count || '-'}</StatNumber>
                    </Stat>
                    <Stat size="sm">
                      <StatLabel>單笔金額</StatLabel>
                      <StatNumber fontSize="md">{result.best_params.order_quantity?.toFixed(0) || '-'}</StatNumber>
                    </Stat>
                  </SimpleGrid>
                  <Divider my={3} />
                  <SimpleGrid columns={4} spacing={2}>
                    <Stat size="sm">
                      <StatLabel>最优得分</StatLabel>
                      <StatNumber fontSize="md" color="blue.600">{result.best_score?.toFixed(2) || '-'}</StatNumber>
                    </Stat>
                    <Stat size="sm">
                      <StatLabel>年化收益</StatLabel>
                      <StatNumber fontSize="md">{result.best_metrics?.annualized_return?.toFixed(2) || '-'}%</StatNumber>
                    </Stat>
                    <Stat size="sm">
                      <StatLabel>最大回撤</StatLabel>
                      <StatNumber fontSize="md">{result.best_metrics?.max_drawdown?.toFixed(2) || '-'}%</StatNumber>
                    </Stat>
                    <Stat size="sm">
                      <StatLabel>夏普比率</StatLabel>
                      <StatNumber fontSize="md">{result.best_metrics?.sharpe_ratio?.toFixed(2) || '-'}</StatNumber>
                    </Stat>
                  </SimpleGrid>
                  <Text fontSize="xs" color="gray.500" mt={2}>
                    方法: {result.method} | 迭代: {result.iterations} | 耗時: {(result.elapsed / 1e9).toFixed(1)}s
                  </Text>
                </CardBody>
              </Card>

              {result.heatmap_data && (
                <Card>
                  <CardHeader fontWeight="600">热力图 (Score)</CardHeader>
                  <CardBody>
                    <Box ref={chartRef} h="300px" />
                  </CardBody>
                </Card>
              )}

              {result.all_results && result.all_results.length > 0 && (
                <Card>
                  <CardHeader fontWeight="600">Top 10 参數组合</CardHeader>
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
                            <Th>年化收益</Th>
                            <Th>最大回撤</Th>
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
                                <Td fontWeight="600">{r.score?.toFixed(2)}</Td>
                                <Td>{r.metrics?.annualized_return?.toFixed(2)}%</Td>
                                <Td>{r.metrics?.max_drawdown?.toFixed(2)}%</Td>
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
              <Text>配置参數后点击"开始优化"</Text>
            </Flex>
          )}
          {running && !result && (
            <Flex justify="center" align="center" h="200px">
              <VStack>
                <Spinner size="lg" color="blue.500" />
                <Text color="gray.500">优化進行中...</Text>
              </VStack>
            </Flex>
          )}
        </VStack>
      </SimpleGrid>
    </Box>
  )
}
