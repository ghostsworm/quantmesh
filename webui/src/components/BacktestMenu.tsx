import React, { useEffect, useState } from 'react'
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
  Tab,
  TabList,
  TabPanel,
  TabPanels,
  Tabs,
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
  IconButton,
  Tooltip,
} from '@chakra-ui/react'
import { RepeatIcon, DownloadIcon, DeleteIcon, CheckIcon } from '@chakra-ui/icons'
import {
  getBacktestStrategies,
  getBacktestPreset,
  postCacheGenerate,
  getCacheStatus,
  postBacktestTask,
  getBacktestTasks,
  getBacktestTask,
  getBacktestTaskResult,
  getBacktestTaskReport,
  deleteBacktestTask,
  type StrategyParamDefinition,
  type SymbolBacktestPreset,
  type BacktestTask,
} from '../services/backtest'
import { getSymbols, type SymbolInfo } from '../services/api'

const formatDate = (s: string) => {
  try {
    const d = new Date(s)
    return isNaN(d.getTime()) ? s : d.toLocaleString()
  } catch {
    return s
  }
}

export default function BacktestMenu() {
  const [strategies, setStrategies] = useState<StrategyParamDefinition[]>([])
  const [symbols, setSymbols] = useState<SymbolInfo[]>([])
  const [symbol, setSymbol] = useState('')
  const [preset, setPreset] = useState<SymbolBacktestPreset | null>(null)
  const [interval, setInterval] = useState('')
  const [days, setDays] = useState(30)
  const [startDate, setStartDate] = useState('')
  const [endDate, setEndDate] = useState('')
  const [cacheExists, setCacheExists] = useState(false)
  const [cacheGenerating, setCacheGenerating] = useState(false)
  const [strategyType, setStrategyType] = useState('')
  const [params, setParams] = useState<Record<string, unknown>>({})
  const [totalCapital, setTotalCapital] = useState(10000)
  const [tasks, setTasks] = useState<BacktestTask[]>([])
  const [tasksLoading, setTasksLoading] = useState(true)
  const [selectedTaskId, setSelectedTaskId] = useState<string | null>(null)
  const [resultData, setResultData] = useState<unknown>(null)
  const [reportMd, setReportMd] = useState('')
  const [running, setRunning] = useState(false)
  const toast = useToast()

  useEffect(() => {
    getBacktestStrategies().then((r) => r.success && setStrategies(r.strategies || []))
    getSymbols()
      .then((r) => setSymbols(r.symbols || []))
      .catch(() => setSymbols([]))
  }, [])

  useEffect(() => {
    if (!symbol) {
      setPreset(null)
      return
    }
    getBacktestPreset(symbol).then((r) => {
      if (r.success && r.preset) {
        setPreset(r.preset)
        if (!interval) setInterval(r.preset.recommended_interval)
        if (r.preset.recommended_days?.length) setDays(r.preset.recommended_days[0])
      } else {
        setPreset(null)
      }
    })
  }, [symbol])

  const updateDateRange = () => {
    const end = new Date()
    const start = new Date()
    start.setDate(start.getDate() - days)
    setStartDate(start.toISOString().slice(0, 10))
    setEndDate(end.toISOString().slice(0, 10))
  }
  useEffect(updateDateRange, [days])

  useEffect(() => {
    if (!symbol || !interval || !startDate || !endDate) {
      setCacheExists(false)
      return
    }
    getCacheStatus({ symbol, interval, start_date: startDate, end_date: endDate }).then((r) => {
      if (r.success) setCacheExists(r.exists)
    })
  }, [symbol, interval, startDate, endDate])

  const loadTasks = () => {
    setTasksLoading(true)
    getBacktestTasks(50, 0)
      .then((r) => (r.success && r.tasks ? setTasks(r.tasks) : setTasks([])))
      .finally(() => setTasksLoading(false))
  }
  useEffect(loadTasks, [])
  const pollTasks = () => {
    const hasRunning = tasks.some((t) => t.status === 'running' || t.status === 'pending')
    if (hasRunning) setTimeout(loadTasks, 3000)
  }
  useEffect(pollTasks, [tasks])

  const handleGenerateCache = () => {
    if (!symbol || !interval || !startDate || !endDate) {
      toast({ title: '请先选择交易对、周期和日期范围', status: 'warning' })
      return
    }
    setCacheGenerating(true)
    postCacheGenerate({ symbol, interval, start_date: startDate, end_date: endDate })
      .then((r) => {
        if (r.success) {
          toast({ title: '已在后台生成 K 线缓存', status: 'success' })
          setTimeout(() => getCacheStatus({ symbol, interval, start_date: startDate, end_date: endDate }).then((s) => s.success && setCacheExists(s.exists)), 2000)
        }
      })
      .catch((e) => toast({ title: e.message || '生成失败', status: 'error' }))
      .finally(() => setCacheGenerating(false))
  }

  const handleRunBacktest = () => {
    if (!symbol || !interval || !startDate || !endDate) {
      toast({ title: '请选择交易对、周期和日期范围', status: 'warning' })
      return
    }
    if (!strategyType) {
      toast({ title: '请选择策略', status: 'warning' })
      return
    }
    if (totalCapital <= 0) {
      toast({ title: '总资金需大于 0', status: 'warning' })
      return
    }
    const start = new Date(startDate)
    const end = new Date(endDate)
    end.setHours(23, 59, 59, 999)
    setRunning(true)
    postBacktestTask({
      strategy: strategyType,
      symbol,
      interval,
      start_time: start.toISOString(),
      end_time: end.toISOString(),
      params: Object.keys(params).length ? params : undefined,
      total_capital: totalCapital,
    })
      .then((r) => {
        if (r.success) {
          toast({ title: '回测任务已创建', status: 'success' })
          loadTasks()
          setSelectedTaskId(r.task_id)
        }
      })
      .catch((e) => toast({ title: e.message || '创建失败', status: 'error' }))
      .finally(() => setRunning(false))
  }

  useEffect(() => {
    if (!selectedTaskId) {
      setResultData(null)
      setReportMd('')
      return
    }
    getBacktestTask(selectedTaskId).then((r) => {
      if (r.success && r.task?.status === 'completed') {
        getBacktestTaskResult(selectedTaskId).then((data) => setResultData(data))
        getBacktestTaskReport(selectedTaskId).then(setReportMd).catch(() => setReportMd(''))
      } else {
        setResultData(null)
        setReportMd('')
      }
    })
  }, [selectedTaskId])

  const currentStrategyDef = strategies.find((s) => s.strategy_type === strategyType)

  return (
    <Box>
      <Heading size="md" mb={4}>回测</Heading>
      <Tabs>
        <TabList>
          <Tab>新建回测</Tab>
          <Tab>任务列表</Tab>
        </TabList>
        <TabPanels>
          <TabPanel>
            <SimpleGrid columns={{ base: 1, md: 2 }} spacing={4}>
              <Card>
                <CardHeader fontWeight="600">1. 交易对与数据</CardHeader>
                <CardBody>
                  <FormControl mb={3}>
                    <FormLabel>交易对</FormLabel>
                    <Select
                      placeholder="选择交易对"
                      value={symbol}
                      onChange={(e) => setSymbol(e.target.value)}
                    >
                      {(symbols.length ? symbols : [
                        { exchange: '', symbol: 'BTCUSDT' },
                        { exchange: '', symbol: 'ETHUSDT' },
                        { exchange: '', symbol: 'PAXGUSDT' },
                        { exchange: '', symbol: 'SOLUSDT' },
                        { exchange: '', symbol: 'BNBUSDT' },
                      ]).map((s) => (
                        <option key={`${s.exchange}-${s.symbol}`} value={s.symbol}>{s.symbol}</option>
                      ))}
                    </Select>
                  </FormControl>
                  {preset && (
                    <Box mb={3} p={2} bg="gray.50" borderRadius="md" fontSize="sm">
                      <Text>推荐: {preset.recommended_interval} K线, {preset.recommended_days?.join('/')} 天, 网格间距 {preset.grid_gap_range}</Text>
                    </Box>
                  )}
                  <FormControl mb={3}>
                    <FormLabel>回测天数</FormLabel>
                    <NumberInput value={days} min={1} max={365} onChange={(_: string, v: number) => setDays(v)}>
                      <NumberInputField />
                    </NumberInput>
                  </FormControl>
                  <FormControl mb={3}>
                    <FormLabel>K 线周期</FormLabel>
                    <Select value={interval} onChange={(e) => setInterval(e.target.value)}>
                      {['1m', '3m', '5m', '15m', '30m', '1h', '4h', '1d'].map((i) => (
                        <option key={i} value={i}>{i}</option>
                      ))}
                    </Select>
                  </FormControl>
                  <HStack mb={3}>
                    <FormControl flex={1}>
                      <FormLabel>开始日期</FormLabel>
                      <Input type="date" value={startDate} onChange={(e) => setStartDate(e.target.value)} />
                    </FormControl>
                    <FormControl flex={1}>
                      <FormLabel>结束日期</FormLabel>
                      <Input type="date" value={endDate} onChange={(e) => setEndDate(e.target.value)} />
                    </FormControl>
                  </HStack>
                  <HStack>
                    <Button
                      size="sm"
                      leftIcon={cacheGenerating ? <Spinner size="sm" /> : <RepeatIcon />}
                      onClick={handleGenerateCache}
                      isDisabled={cacheGenerating || !symbol}
                    >
                      {cacheExists ? '已缓存' : '生成 K 线缓存'}
                    </Button>
                    {cacheExists && <Badge colorScheme="green"><CheckIcon mr={1} /> 缓存就绪</Badge>}
                  </HStack>
                </CardBody>
              </Card>
              <Card>
                <CardHeader fontWeight="600">2. 策略与参数</CardHeader>
                <CardBody>
                  <FormControl mb={3}>
                    <FormLabel>策略</FormLabel>
                    <Select
                      placeholder="选择策略"
                      value={strategyType}
                      onChange={(e) => {
                        setStrategyType(e.target.value)
                        setParams({})
                      }}
                    >
                      {strategies.map((s) => (
                        <option key={s.strategy_type} value={s.strategy_type}>{s.name}</option>
                      ))}
                    </Select>
                  </FormControl>
                  {currentStrategyDef?.params?.map((p) => (
                    <FormControl key={p.name} mb={2}>
                      <FormLabel fontSize="sm">{p.label}{p.required ? ' *' : ''}</FormLabel>
                      {p.type === 'number' && (
                        <NumberInput
                          value={(params[p.name] as number) ?? (p.default as number)}
                          min={p.min}
                          max={p.max}
                          onChange={(_: string, v: number) => setParams((prev) => ({ ...prev, [p.name]: v }))}
                        >
                          <NumberInputField />
                        </NumberInput>
                      )}
                      {p.unit && <Text as="span" fontSize="xs" color="gray.500" ml={2}>{p.unit}</Text>}
                    </FormControl>
                  ))}
                  <FormControl mb={4}>
                    <FormLabel>总投入资金 (USDT) *</FormLabel>
                    <NumberInput value={totalCapital} min={1} onChange={(_: string, v: number) => setTotalCapital(v)}>
                      <NumberInputField />
                    </NumberInput>
                  </FormControl>
                  <Button
                    colorScheme="blue"
                    onClick={handleRunBacktest}
                    isLoading={running}
                    isDisabled={!strategyType || totalCapital <= 0}
                  >
                    开始回测
                  </Button>
                </CardBody>
              </Card>
            </SimpleGrid>
          </TabPanel>
          <TabPanel>
            {tasksLoading ? (
              <Flex justify="center" py={8}><Spinner /></Flex>
            ) : (
              <Box overflowX="auto">
                <Table size="sm">
                  <Thead>
                    <Tr>
                      <Th>状态</Th>
                      <Th>策略</Th>
                      <Th>交易对</Th>
                      <Th>周期</Th>
                      <Th>时间范围</Th>
                      <Th>创建时间</Th>
                      <Th>操作</Th>
                    </Tr>
                  </Thead>
                  <Tbody>
                    {tasks.map((t) => (
                      <Tr key={t.id}>
                        <Td>
                          <Badge
                            colorScheme={
                              t.status === 'completed' ? 'green' : t.status === 'failed' ? 'red' : t.status === 'running' ? 'blue' : 'gray'
                            }
                          >
                            {t.status}
                          </Badge>
                        </Td>
                        <Td>{t.strategy}</Td>
                        <Td>{t.symbol}</Td>
                        <Td>{t.interval}</Td>
                        <Td>{formatDate(t.start_time)} ~ {formatDate(t.end_time)}</Td>
                        <Td>{formatDate(t.created_at)}</Td>
                        <Td>
                          <HStack>
                            <Button size="xs" onClick={() => setSelectedTaskId(t.id)}>查看</Button>
                            {t.status === 'completed' && (
                              <Tooltip label="下载报告">
                                <IconButton
                                  aria-label="下载报告"
                                  size="xs"
                                  icon={<DownloadIcon />}
                                  onClick={() => {
                                    getBacktestTaskReport(t.id, true).then((md) => {
                                      const blob = new Blob([md], { type: 'text/markdown' })
                                      const a = document.createElement('a')
                                      a.href = URL.createObjectURL(blob)
                                      a.download = `backtest_${t.id}.md`
                                      a.click()
                                    })
                                  }}
                                />
                              </Tooltip>
                            )}
                            <IconButton
                              aria-label="删除"
                              size="xs"
                              icon={<DeleteIcon />}
                              onClick={() => {
                                deleteBacktestTask(t.id).then(() => loadTasks())
                                if (selectedTaskId === t.id) setSelectedTaskId(null)
                              }}
                            />
                          </HStack>
                        </Td>
                      </Tr>
                    ))}
                  </Tbody>
                </Table>
              </Box>
            )}
            {selectedTaskId && (
              <Card mt={4}>
                <CardHeader fontWeight="600">回测结果: {selectedTaskId}</CardHeader>
                <CardBody>
                  {resultData && typeof resultData === 'object' && 'result' in resultData && (
                    <Box mb={4}>
                      <SimpleGrid columns={4} spacing={2} mb={3}>
                        {(() => {
                          const res = (resultData as { result?: { metrics?: Record<string, unknown> } }).result
                          const m = res?.metrics
                          if (!m) return null
                          return (
                            <>
                              <Box p={2} bg="gray.50" borderRadius="md"><Text fontSize="xs">总收益率</Text><Text fontWeight="600">{String(m.total_return ?? '-')}%</Text></Box>
                              <Box p={2} bg="gray.50" borderRadius="md"><Text fontSize="xs">最大回撤</Text><Text fontWeight="600">{String(m.max_drawdown ?? '-')}%</Text></Box>
                              <Box p={2} bg="gray.50" borderRadius="md"><Text fontSize="xs">夏普比率</Text><Text fontWeight="600">{String(m.sharpe_ratio ?? '-')}</Text></Box>
                              <Box p={2} bg="gray.50" borderRadius="md"><Text fontSize="xs">交易次数</Text><Text fontWeight="600">{String(m.total_trades ?? '-')}</Text></Box>
                            </>
                          )
                        })()}
                      </SimpleGrid>
                    </Box>
                  )}
                  {reportMd && (
                    <Box as="pre" fontSize="sm" whiteSpace="pre-wrap" p={3} bg="gray.50" borderRadius="md" overflowX="auto">
                      {reportMd}
                    </Box>
                  )}
                  {selectedTaskId && !resultData && !reportMd && (
                    <Text color="gray.500">任务未完成或结果未生成，请稍后刷新。</Text>
                  )}
                </CardBody>
              </Card>
            )}
          </TabPanel>
        </TabPanels>
      </Tabs>
    </Box>
  )
}
