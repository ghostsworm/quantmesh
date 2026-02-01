import React, { useEffect, useState, useCallback } from 'react'
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
  Alert,
  AlertIcon,
  AlertTitle,
  AlertDescription,
  Progress,
  Divider,
  Collapse,
  useDisclosure,
} from '@chakra-ui/react'
import { RepeatIcon, DownloadIcon, DeleteIcon, CheckIcon, StarIcon, ChevronDownIcon, ChevronUpIcon } from '@chakra-ui/icons'
import {
  getBacktestStrategies,
  getBacktestPreset,
  getBacktestExchanges,
  getBacktestSymbols,
  getBacktestConfigParams,
  postCacheGenerate,
  getCacheStatus,
  postBacktestTask,
  getBacktestTasks,
  getBacktestTask,
  getBacktestTaskResult,
  getBacktestTaskReport,
  deleteBacktestTask,
  getSmartParamsRecommendation,
  getPrecomputedResults,
  triggerPrecompute,
  getAutoSchedulerStatus,
  type StrategyParamDefinition,
  type SymbolBacktestPreset,
  type BacktestTask,
  type BacktestExchangeInfo,
  type BacktestSymbolInfo,
  type SmartParamsRecommendation,
  type PrecomputedResult,
} from '../services/backtest'

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
  
  // 三級聯動：交易所 → 市場類型 → 交易對
  const [exchanges, setExchanges] = useState<BacktestExchangeInfo[]>([])
  const [selectedExchange, setSelectedExchange] = useState('')
  const [selectedMarketType, setSelectedMarketType] = useState('futures')
  const [availableMarketTypes, setAvailableMarketTypes] = useState<string[]>(['futures', 'spot'])
  const [symbols, setSymbols] = useState<BacktestSymbolInfo[]>([])
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
  const [configParamsLoading, setConfigParamsLoading] = useState(false)
  const toast = useToast()

  // 智能推薦相關狀態
  const [smartRecommendation, setSmartRecommendation] = useState<SmartParamsRecommendation | null>(null)
  const [smartLoading, setSmartLoading] = useState(false)
  const [precomputedResults, setPrecomputedResults] = useState<PrecomputedResult[]>([])
  const [precomputedLoading, setPrecomputedLoading] = useState(true)
  const { isOpen: showPrecomputed, onToggle: togglePrecomputed } = useDisclosure({ defaultIsOpen: true })

  // 初始化：載入策略列表、交易所列表和預計算結果
  useEffect(() => {
    getBacktestStrategies().then((r) => r.success && setStrategies(r.strategies || []))
    getBacktestExchanges().then((r) => {
      if (r.success && r.exchanges) {
        setExchanges(r.exchanges)
        // 自動選擇已配置的交易所，或預設第一個
        const configured = r.exchanges.find(e => e.is_configured)
        if (configured) {
          setSelectedExchange(configured.exchange)
          setAvailableMarketTypes(configured.market_types || ['futures', 'spot'])
        } else if (r.exchanges.length > 0) {
          setSelectedExchange(r.exchanges[0].exchange)
          setAvailableMarketTypes(r.exchanges[0].market_types || ['futures', 'spot'])
        }
      }
    })
    // 載入預計算結果
    loadPrecomputedResults()
  }, [])

  // 載入預計算結果
  const loadPrecomputedResults = useCallback(async () => {
    setPrecomputedLoading(true)
    try {
      const r = await getPrecomputedResults({ only_ready: true })
      if (r.success && r.results) {
        setPrecomputedResults(r.results)
      }
    } catch (err) {
      console.error('載入預計算結果失敗:', err)
    } finally {
      setPrecomputedLoading(false)
    }
  }, [])

  // 獲取智能參數推薦
  const handleGetSmartRecommendation = useCallback(async () => {
    if (!symbol || !strategyType) {
      toast({ title: '請先選擇交易對和策略', status: 'warning' })
      return
    }

    setSmartLoading(true)
    try {
      const r = await getSmartParamsRecommendation({
        exchange: selectedExchange,
        market_type: selectedMarketType,
        symbol,
        strategy: strategyType,
        total_capital: totalCapital,
      })
      if (r.success && r.recommendation) {
        setSmartRecommendation(r.recommendation)
        toast({
          title: '已獲取智能推薦',
          description: `置信度: ${r.recommendation.confidence.toFixed(0)}%`,
          status: 'success',
          duration: 3000,
        })
      }
    } catch (err) {
      console.error('獲取智能推薦失敗:', err)
      toast({ title: '獲取智能推薦失敗', status: 'error' })
    } finally {
      setSmartLoading(false)
    }
  }, [symbol, strategyType, selectedExchange, selectedMarketType, totalCapital, toast])

  // 應用智能推薦參數
  const applySmartRecommendation = useCallback((recommendation: SmartParamsRecommendation) => {
    if (!recommendation.params) return

    const newParams: Record<string, unknown> = {}
    const strategyDef = strategies.find(s => s.strategy_type === recommendation.strategy)
    
    if (strategyDef?.params) {
      for (const p of strategyDef.params) {
        if (recommendation.params[p.name] !== undefined) {
          newParams[p.name] = recommendation.params[p.name]
        }
      }
    }

    setParams(newParams)
    
    // 如果推薦中有 total_capital，也設置
    if (recommendation.params.total_capital && typeof recommendation.params.total_capital === 'number') {
      setTotalCapital(recommendation.params.total_capital)
    }

    toast({
      title: '已應用智能推薦參數',
      status: 'success',
      duration: 2000,
    })
  }, [strategies, toast])

  // 從預計算結果應用配置
  const applyPrecomputedResult = useCallback((result: PrecomputedResult) => {
    // 設置交易對和策略
    setSelectedExchange(result.exchange)
    setSelectedMarketType(result.market_type)
    setSymbol(result.symbol)
    setStrategyType(result.strategy)

    // 應用參數
    if (result.recommendation?.params) {
      setTimeout(() => {
        applySmartRecommendation(result.recommendation)
      }, 100)
    }

    toast({
      title: '已應用預計算配置',
      description: `${result.symbol} - ${result.strategy}`,
      status: 'success',
      duration: 3000,
    })
  }, [toast, applySmartRecommendation])

  // 當交易所改變時，更新可用的市場類型
  useEffect(() => {
    if (!selectedExchange) return
    const ex = exchanges.find(e => e.exchange === selectedExchange)
    if (ex) {
      setAvailableMarketTypes(ex.market_types || ['futures', 'spot'])
      // 如果當前選擇的市場類型不在可用列表中，重置
      if (!ex.market_types?.includes(selectedMarketType)) {
        setSelectedMarketType(ex.market_types?.[0] || 'futures')
      }
    }
    // 清空交易對選擇
    setSymbol('')
    setSymbols([])
  }, [selectedExchange])

  // 當交易所或市場類型改變時，載入交易對列表
  useEffect(() => {
    if (!selectedExchange || !selectedMarketType) return
    getBacktestSymbols(selectedExchange, selectedMarketType).then((r) => {
      if (r.success && r.symbols) {
        setSymbols(r.symbols)
        // 自動選擇已配置的交易對，或清空
        const configured = r.symbols.find(s => s.is_configured)
        if (configured) {
          setSymbol(configured.symbol)
        } else {
          setSymbol('')
        }
      }
    })
  }, [selectedExchange, selectedMarketType])

  // 當交易對改變時，載入預設配置
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

  // 當策略類型改變時，嘗試從配置中預填参數
  const loadConfigParams = useCallback(async () => {
    if (!symbol || !strategyType) return
    
    setConfigParamsLoading(true)
    try {
      const r = await getBacktestConfigParams({
        exchange: selectedExchange,
        symbol,
        strategy: strategyType,
      })
      if (r.success && r.found && r.params) {
        const newParams: Record<string, unknown> = {}
        const configParams = r.params as Record<string, unknown>
        
        // 根據策略定義過濾並設置参數
        const strategyDef = strategies.find(s => s.strategy_type === strategyType)
        if (strategyDef?.params) {
          for (const p of strategyDef.params) {
            if (configParams[p.name] !== undefined) {
              newParams[p.name] = configParams[p.name]
            }
          }
        }
        
        // 設置總资金
        if (configParams.total_capital && typeof configParams.total_capital === 'number') {
          setTotalCapital(configParams.total_capital)
        }
        
        if (Object.keys(newParams).length > 0) {
          setParams(newParams)
          toast({
            title: '已從配置中載入參數',
            description: `為 ${symbol} 的 ${strategyType} 策略預填了參數`,
            status: 'info',
            duration: 3000,
          })
        }
      }
    } catch (err) {
      console.error('載入配置參數失敗:', err)
    } finally {
      setConfigParamsLoading(false)
    }
  }, [symbol, strategyType, selectedExchange, strategies, toast])

  useEffect(() => {
    if (strategyType && symbol) {
      loadConfigParams()
    }
  }, [strategyType, symbol, loadConfigParams])

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
      toast({ title: '請先選擇交易對、周期和日期範圍', status: 'warning' })
      return
    }
    setCacheGenerating(true)
    postCacheGenerate({ symbol, interval, start_date: startDate, end_date: endDate })
      .then((r) => {
        if (r.success) {
          toast({ title: '已在后台生成 K 線緩存', status: 'success' })
          setTimeout(() => getCacheStatus({ symbol, interval, start_date: startDate, end_date: endDate }).then((s) => s.success && setCacheExists(s.exists)), 2000)
        }
      })
      .catch((e) => toast({ title: e.message || '生成失敗', status: 'error' }))
      .finally(() => setCacheGenerating(false))
  }

  const handleRunBacktest = () => {
    if (!symbol || !interval || !startDate || !endDate) {
      toast({ title: '請選擇交易對、周期和日期範圍', status: 'warning' })
      return
    }
    if (!strategyType) {
      toast({ title: '請選擇策略', status: 'warning' })
      return
    }
    if (totalCapital <= 0) {
      toast({ title: '總資金需大於 0', status: 'warning' })
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
          toast({ title: '回測任務已創建', status: 'success' })
          loadTasks()
          setSelectedTaskId(r.task_id)
        }
      })
      .catch((e) => toast({ title: e.message || '創建失敗', status: 'error' }))
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

  // 市場類型顯示名稱
  const marketTypeLabels: Record<string, string> = {
    futures: '合約',
    spot: '現貨',
  }

  return (
    <Box>
      <Heading size="md" mb={4}>回測</Heading>

      {/* 預計算推薦區域 */}
      {precomputedResults.length > 0 && (
        <Card mb={4} borderColor="blue.200" borderWidth={1}>
          <CardHeader pb={2}>
            <Flex justify="space-between" align="center">
              <HStack>
                <StarIcon color="yellow.500" />
                <Text fontWeight="600">智能推薦 - 預計算回測結果</Text>
                <Badge colorScheme="green">{precomputedResults.length} 個就緒</Badge>
              </HStack>
              <IconButton
                aria-label="展開/收起"
                size="sm"
                variant="ghost"
                icon={showPrecomputed ? <ChevronUpIcon /> : <ChevronDownIcon />}
                onClick={togglePrecomputed}
              />
            </Flex>
          </CardHeader>
          <Collapse in={showPrecomputed}>
            <CardBody pt={0}>
              <Text fontSize="sm" color="gray.600" mb={3}>
                系統已根據當前市場情況自動運行回測，您可以直接選用表現良好的配置。
              </Text>
              <SimpleGrid columns={{ base: 1, md: 2, lg: 3 }} spacing={3}>
                {precomputedResults.slice(0, 6).map((result, idx) => (
                  <Box
                    key={`${result.symbol}-${result.strategy}-${idx}`}
                    p={3}
                    borderRadius="md"
                    border="1px solid"
                    borderColor="gray.200"
                    _hover={{ borderColor: 'blue.300', bg: 'blue.50' }}
                    cursor="pointer"
                    onClick={() => applyPrecomputedResult(result)}
                  >
                    <HStack justify="space-between" mb={2}>
                      <Text fontWeight="600">{result.symbol}</Text>
                      <Badge colorScheme={result.result?.metrics?.total_return && result.result.metrics.total_return > 0 ? 'green' : 'red'}>
                        {result.result?.metrics?.total_return?.toFixed(2) ?? '-'}%
                      </Badge>
                    </HStack>
                    <Text fontSize="sm" color="gray.600" mb={1}>
                      策略: {result.strategy} | {result.market_type === 'spot' ? '現貨' : '合約'}
                    </Text>
                    <HStack spacing={2} fontSize="xs" color="gray.500">
                      <Text>夏普: {result.result?.metrics?.sharpe_ratio?.toFixed(2) ?? '-'}</Text>
                      <Text>回撤: {result.result?.metrics?.max_drawdown?.toFixed(2) ?? '-'}%</Text>
                      <Text>勝率: {result.result?.metrics?.win_rate?.toFixed(1) ?? '-'}%</Text>
                    </HStack>
                    {result.recommendation && (
                      <Progress
                        value={result.recommendation.confidence}
                        size="xs"
                        colorScheme={result.recommendation.confidence > 70 ? 'green' : result.recommendation.confidence > 50 ? 'yellow' : 'red'}
                        mt={2}
                      />
                    )}
                    <Text fontSize="xs" color="gray.400" mt={1}>
                      置信度: {result.recommendation?.confidence?.toFixed(0) ?? '-'}%
                    </Text>
                  </Box>
                ))}
              </SimpleGrid>
              {precomputedResults.length > 6 && (
                <Text fontSize="sm" color="gray.500" mt={2} textAlign="center">
                  還有 {precomputedResults.length - 6} 個推薦結果...
                </Text>
              )}
            </CardBody>
          </Collapse>
        </Card>
      )}

      {precomputedLoading && (
        <Alert status="info" mb={4}>
          <Spinner size="sm" mr={3} />
          <AlertDescription>正在載入智能推薦...</AlertDescription>
        </Alert>
      )}

      <Tabs>
        <TabList>
          <Tab>新建回測</Tab>
          <Tab>任務列表</Tab>
        </TabList>
        <TabPanels>
          <TabPanel>
            <SimpleGrid columns={{ base: 1, md: 2 }} spacing={4}>
              <Card>
                <CardHeader fontWeight="600">1. 交易對與數據</CardHeader>
                <CardBody>
                  {/* 交易所選擇 */}
                  <FormControl mb={3}>
                    <FormLabel>交易所</FormLabel>
                    <Select
                      placeholder="選擇交易所"
                      value={selectedExchange}
                      onChange={(e) => setSelectedExchange(e.target.value)}
                    >
                      {exchanges.map((ex) => (
                        <option key={ex.exchange} value={ex.exchange}>
                          {ex.exchange.toUpperCase()}
                          {ex.is_configured ? ' (已配置)' : ''}
                        </option>
                      ))}
                    </Select>
                  </FormControl>

                  {/* 市場類型選擇 */}
                  <FormControl mb={3}>
                    <FormLabel>市場類型</FormLabel>
                    <Select
                      value={selectedMarketType}
                      onChange={(e) => setSelectedMarketType(e.target.value)}
                      isDisabled={!selectedExchange}
                    >
                      {availableMarketTypes.map((mt) => (
                        <option key={mt} value={mt}>
                          {marketTypeLabels[mt] || mt}
                        </option>
                      ))}
                    </Select>
                  </FormControl>

                  {/* 交易對選擇 */}
                  <FormControl mb={3}>
                    <FormLabel>交易對</FormLabel>
                    <Select
                      placeholder="選擇交易對"
                      value={symbol}
                      onChange={(e) => setSymbol(e.target.value)}
                      isDisabled={!selectedExchange}
                    >
                      {symbols.map((s) => (
                        <option key={`${s.exchange}-${s.symbol}`} value={s.symbol}>
                          {s.symbol}
                          {s.is_configured ? ' (已配置)' : ''}
                        </option>
                      ))}
                    </Select>
                  </FormControl>

                  {preset && (
                    <Box mb={3} p={2} bg="gray.50" borderRadius="md" fontSize="sm">
                      <Text>推薦: {preset.recommended_interval} K線, {preset.recommended_days?.join('/')} 天, 網格間距 {preset.grid_gap_range}</Text>
                    </Box>
                  )}
                  <FormControl mb={3}>
                    <FormLabel>回測天數</FormLabel>
                    <NumberInput value={days} min={1} max={365} onChange={(_: string, v: number) => setDays(v)}>
                      <NumberInputField />
                    </NumberInput>
                  </FormControl>
                  <FormControl mb={3}>
                    <FormLabel>K 線周期</FormLabel>
                    <Select value={interval} onChange={(e) => setInterval(e.target.value)}>
                      {['1m', '3m', '5m', '15m', '30m', '1h', '4h', '1d'].map((i) => (
                        <option key={i} value={i}>{i}</option>
                      ))}
                    </Select>
                  </FormControl>
                  <HStack mb={3}>
                    <FormControl flex={1}>
                      <FormLabel>開始日期</FormLabel>
                      <Input type="date" value={startDate} onChange={(e) => setStartDate(e.target.value)} />
                    </FormControl>
                    <FormControl flex={1}>
                      <FormLabel>結束日期</FormLabel>
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
                      {cacheExists ? '已緩存' : '生成 K 線緩存'}
                    </Button>
                    {cacheExists && <Badge colorScheme="green"><CheckIcon mr={1} /> 緩存就緒</Badge>}
                  </HStack>
                </CardBody>
              </Card>
              <Card>
                <CardHeader fontWeight="600">2. 策略與參數</CardHeader>
                <CardBody>
                  <FormControl mb={3}>
                    <FormLabel>策略</FormLabel>
                    <Select
                      placeholder="選擇策略"
                      value={strategyType}
                      onChange={(e) => {
                        setStrategyType(e.target.value)
                        setParams({}) // 清空參數，loadConfigParams 會自動觸發
                        setSmartRecommendation(null) // 清空智能推薦
                      }}
                    >
                      {strategies.map((s) => (
                        <option key={s.strategy_type} value={s.strategy_type}>{s.name}</option>
                      ))}
                    </Select>
                  </FormControl>

                  {/* 智能推薦按鈕 */}
                  {symbol && strategyType && (
                    <Box mb={3}>
                      <Button
                        size="sm"
                        colorScheme="purple"
                        leftIcon={smartLoading ? <Spinner size="sm" /> : <StarIcon />}
                        onClick={handleGetSmartRecommendation}
                        isDisabled={smartLoading}
                        mr={2}
                      >
                        獲取智能推薦
                      </Button>
                      {smartRecommendation && (
                        <Button
                          size="sm"
                          colorScheme="green"
                          onClick={() => applySmartRecommendation(smartRecommendation)}
                        >
                          應用推薦
                        </Button>
                      )}
                    </Box>
                  )}

                  {/* 智能推薦結果展示 */}
                  {smartRecommendation && (
                    <Alert status="info" mb={3} borderRadius="md">
                      <Box flex="1">
                        <AlertTitle fontSize="sm">
                          智能推薦 (置信度: {smartRecommendation.confidence.toFixed(0)}%)
                        </AlertTitle>
                        <AlertDescription fontSize="xs">
                          <Text mb={1}>當前價格: ${smartRecommendation.current_price.toFixed(2)}</Text>
                          {smartRecommendation.volatility && (
                            <Text mb={1}>
                              7日波動率: {smartRecommendation.volatility.volatility_7d?.toFixed(1)}% |
                              趨勢: {smartRecommendation.volatility.trend_direction === 'up' ? '上漲' : smartRecommendation.volatility.trend_direction === 'down' ? '下跌' : '震盪'}
                            </Text>
                          )}
                          <Text>{smartRecommendation.reasoning}</Text>
                        </AlertDescription>
                      </Box>
                    </Alert>
                  )}

                  {configParamsLoading && (
                    <HStack mb={2}>
                      <Spinner size="sm" />
                      <Text fontSize="sm" color="gray.500">正在載入配置參數...</Text>
                    </HStack>
                  )}
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
                    <FormLabel>總投入資金 (USDT) *</FormLabel>
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
                    開始回測
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
                      <Th>狀態</Th>
                      <Th>策略</Th>
                      <Th>交易對</Th>
                      <Th>周期</Th>
                      <Th>時間範圍</Th>
                      <Th>創建時間</Th>
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
                              <Tooltip label="下載報告">
                                <IconButton
                                  aria-label="下載報告"
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
                              aria-label="刪除"
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
                <CardHeader fontWeight="600">回測結果: {selectedTaskId}</CardHeader>
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
                              <Box p={2} bg="gray.50" borderRadius="md"><Text fontSize="xs">總收益率</Text><Text fontWeight="600">{String(m.total_return ?? '-')}%</Text></Box>
                              <Box p={2} bg="gray.50" borderRadius="md"><Text fontSize="xs">最大回撤</Text><Text fontWeight="600">{String(m.max_drawdown ?? '-')}%</Text></Box>
                              <Box p={2} bg="gray.50" borderRadius="md"><Text fontSize="xs">夏普比率</Text><Text fontWeight="600">{String(m.sharpe_ratio ?? '-')}</Text></Box>
                              <Box p={2} bg="gray.50" borderRadius="md"><Text fontSize="xs">交易次數</Text><Text fontWeight="600">{String(m.total_trades ?? '-')}</Text></Box>
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
                    <Text color="gray.500">任務未完成或結果未生成，請稍後刷新。</Text>
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
