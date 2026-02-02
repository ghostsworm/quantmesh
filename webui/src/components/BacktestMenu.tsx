import React, { useEffect, useState, useCallback, useRef } from 'react'
import html2canvas from 'html2canvas'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip as RechartsTooltip,
  ResponsiveContainer,
} from 'recharts'
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
  Modal,
  ModalOverlay,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalFooter,
  ModalCloseButton,
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
  listCache,
  deleteCache,
  postBacktestTask,
  getBacktestTasks,
  getBacktestTask,
  getBacktestTaskResult,
  getBacktestTaskReport,
  getBacktestTaskKlines,
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
  type CacheInfo,
} from '../services/backtest'

/** 回测结果数据结构（含风控对比） */
interface BacktestResultData {
  result?: {
    metrics?: Record<string, unknown>
    trades?: Array<{ type?: string; quantity?: number }>
    price_curve?: { end_price?: number }
    final_capital?: number
  }
  comparison?: {
    no_risk_result?: {
      metrics?: Record<string, unknown>
      trades?: Array<{ type?: string; quantity?: number }>
      price_curve?: { end_price?: number }
      final_capital?: number
    }
    with_risk_result?: {
      metrics?: Record<string, unknown>
      trades?: Array<{ type?: string; quantity?: number }>
      price_curve?: { end_price?: number }
      final_capital?: number
      risk_interventions?: Array<{ time_str?: string; reason?: string; risk_type?: string; duration?: number; skipped_buys?: number }>
    }
    comparison?: { risk_intervention_count?: number; skipped_signals?: number }
  }
}

const formatDate = (s: string) => {
  try {
    const d = new Date(s)
    return isNaN(d.getTime()) ? s : d.toLocaleString()
  } catch {
    return s
  }
}

// 網格策略回测時價格區間從 K 線自動推導，不再需要預設價格上下限

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
  const [klinesData, setKlinesData] = useState<{ klines: { ts: number; time: string; close: number }[]; symbol: string } | null>(null)
  const [running, setRunning] = useState(false)
  const reportModal = useDisclosure()
  const [configParamsLoading, setConfigParamsLoading] = useState(false)
  const [savingImage, setSavingImage] = useState(false)
  const reportContentRef = useRef<HTMLDivElement>(null)
  const toast = useToast()

  // 智能推薦相關狀態
  const [smartRecommendation, setSmartRecommendation] = useState<SmartParamsRecommendation | null>(null)
  const [smartLoading, setSmartLoading] = useState(false)
  const [precomputedResults, setPrecomputedResults] = useState<PrecomputedResult[]>([])
  const [precomputedLoading, setPrecomputedLoading] = useState(true)
  const { isOpen: showPrecomputed, onToggle: togglePrecomputed } = useDisclosure({ defaultIsOpen: true })

  // K 线缓存列表
  const [cachedKlines, setCachedKlines] = useState<CacheInfo[]>([])
  const [cachedKlinesLoading, setCachedKlinesLoading] = useState(false)

  // 載入預計算結果（需在下方 useEffect 之前定義）
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

  // 載入已緩存的 K 線列表（需在下方 useEffect 之前定義）
  const loadCachedKlines = useCallback(async () => {
    setCachedKlinesLoading(true)
    try {
      const r = await listCache()
      if (r.success && r.caches) {
        setCachedKlines(r.caches)
      }
    } catch (err) {
      console.error('載入 K 線緩存列表失敗:', err)
      toast({ title: '載入緩存列表失敗', status: 'error' })
    } finally {
      setCachedKlinesLoading(false)
    }
  }, [toast])

  // 初始化：載入策略列表、交易所列表、預計算結果和 K 線緩存列表
  useEffect(() => {
    getBacktestStrategies().then((r) => r.success && setStrategies(r.strategies || []))
    getBacktestExchanges().then((r) => {
      if (r.success && r.exchanges) {
        const sorted = [...r.exchanges].sort((a, b) =>
          a.exchange.localeCompare(b.exchange, undefined, { sensitivity: 'base' })
        )
        setExchanges(sorted)
        // 自動選擇已配置的交易所，或預設第一個
        const configured = sorted.find(e => e.is_configured)
        if (configured) {
          setSelectedExchange(configured.exchange)
          setAvailableMarketTypes(configured.market_types || ['futures', 'spot'])
        } else if (sorted.length > 0) {
          setSelectedExchange(sorted[0].exchange)
          setAvailableMarketTypes(sorted[0].market_types || ['futures', 'spot'])
        }
      }
    })
    loadPrecomputedResults()
    loadCachedKlines()
  }, [loadPrecomputedResults, loadCachedKlines])

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
      let description: string | undefined
      const msg = err instanceof Error ? err.message : String(err)
      const jsonMatch = msg.match(/^HTTP \d+: (.+)$/)
      if (jsonMatch) {
        try {
          const body = JSON.parse(jsonMatch[1]) as { message?: string }
          if (body?.message) description = body.message
        } catch {
          description = msg
        }
      } else {
        description = msg
      }
      toast({ title: '獲取智能推薦失敗', status: 'error', description })
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
        
        // 根據策略定義過濾並設置参數（不覆蓋總投入資金，保留用戶已填寫的值）
        const strategyDef = strategies.find(s => s.strategy_type === strategyType)
        if (strategyDef?.params) {
          for (const p of strategyDef.params) {
            if (configParams[p.name] !== undefined) {
              newParams[p.name] = configParams[p.name]
            }
          }
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

  // 網格策略回测時價格區間從 K 線自動推導，不再預填 price_low / price_high

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
      .catch((e) => {
        const msg = e?.message || ''
        const is503 = msg.includes('503') || msg.includes('回测服務未初始化') || msg.includes('回测服务未初始化')
        let description: string | undefined
        if (is503) {
          try {
            const jsonMatch = msg.match(/\{[\s\S]*\}/)
            if (jsonMatch) {
              const body = JSON.parse(jsonMatch[0])
              if (body?.message) description = body.message
            }
          } catch (_) {}
          toast({
            title: '回测服务不可用',
            description: description || '请确保已启用存储（SQLite），并在「设置」中保存过配置后重启服务。',
            status: 'error',
            duration: 8000,
            isClosable: true,
          })
        } else {
          toast({ title: msg || '創建失敗', status: 'error' })
        }
      })
      .finally(() => setRunning(false))
  }

  useEffect(() => {
    if (!selectedTaskId) {
      setResultData(null)
      setReportMd('')
      setKlinesData(null)
      return
    }
    
    // 先檢查 tasks 列表中的狀態，避免額外的 API 調用
    const taskFromList = tasks.find((t) => t.id === selectedTaskId)
    const isCompleted = taskFromList?.status === 'completed'
    
    const loadResultData = (retryCount = 0) => {
      getBacktestTaskResult(selectedTaskId)
        .then((data) => setResultData(data))
        .catch(() => {
          // 結果文件可能還沒生成完成，重試最多 3 次，每次間隔 1 秒
          if (retryCount < 3) {
            setTimeout(() => loadResultData(retryCount + 1), 1000)
          }
        })
      getBacktestTaskReport(selectedTaskId).then(setReportMd).catch(() => setReportMd(''))
      getBacktestTaskKlines(selectedTaskId).then((res) => {
        const kls = res.klines || []
        const data = kls.map((k) => {
          const d = new Date(k.time * 1000)
          return { ts: k.time, time: d.toLocaleDateString('zh-CN', { month: 'short', day: 'numeric', hour: kls.length < 200 ? '2-digit' : undefined }), close: k.close }
        })
        setKlinesData({ klines: data, symbol: res.symbol })
      }).catch(() => setKlinesData(null))
    }
    
    if (isCompleted) {
      // 任務已完成，直接加載結果
      loadResultData()
    } else {
      // 任務未完成或不在列表中，查詢最新狀態
      getBacktestTask(selectedTaskId)
        .then((r) => {
          if (r.success && r.task?.status === 'completed') {
            loadResultData()
          } else {
            setResultData(null)
            setReportMd('')
            setKlinesData(null)
          }
        })
        .catch(() => {
          // API 調用失敗時，如果列表中顯示已完成，仍嘗試加載
          if (isCompleted) {
            loadResultData()
          }
        })
    }
  }, [selectedTaskId, tasks])

  // 保存報告為圖片並下載
  const handleSaveAsImage = async () => {
    if (!reportContentRef.current || !selectedTaskId) return
    
    setSavingImage(true)
    try {
      const element = reportContentRef.current
      
      // 克隆元素以避免修改原始 DOM
      const clone = element.cloneNode(true) as HTMLElement
      clone.style.position = 'absolute'
      clone.style.left = '-9999px'
      clone.style.top = '0'
      clone.style.width = `${element.scrollWidth}px`
      clone.style.overflow = 'visible'
      clone.style.maxHeight = 'none'
      clone.style.height = 'auto'
      clone.style.backgroundColor = '#ffffff'
      document.body.appendChild(clone)
      
      // 等待克隆元素渲染完成
      await new Promise(resolve => setTimeout(resolve, 200))
      
      const contentHeight = clone.scrollHeight
      const contentWidth = clone.scrollWidth
      console.log(`html2canvas: content=${contentWidth}x${contentHeight}`)
      
      const canvas = await html2canvas(clone, {
        backgroundColor: '#ffffff',
        scale: 1,
        useCORS: true,
        logging: false,
        allowTaint: true,
        foreignObjectRendering: false,
        removeContainer: false,
      })
      
      // 移除克隆元素
      document.body.removeChild(clone)
      
      // 檢查 canvas 是否有效
      if (canvas.width === 0 || canvas.height === 0) {
        throw new Error('Canvas 生成失敗，尺寸為 0')
      }
      
      // 下載圖片
      const dataUrl = canvas.toDataURL('image/png')
      if (!dataUrl || dataUrl === 'data:,') {
        throw new Error('圖片數據生成失敗，可能是內容太大')
      }
      
      const link = document.createElement('a')
      link.download = `backtest_${selectedTaskId}.png`
      link.href = dataUrl
      link.click()
      
      toast({
        title: '圖片保存成功',
        description: `已保存為 backtest_${selectedTaskId}.png`,
        status: 'success',
        duration: 3000,
        isClosable: true,
      })
    } catch (error) {
      console.error('保存圖片失敗:', error)
      toast({
        title: '圖片保存失敗',
        description: String(error),
        status: 'error',
        duration: 5000,
        isClosable: true,
      })
    } finally {
      setSavingImage(false)
    }
  }

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
                        {result.result?.metrics?.total_return?.toFixed(4) ?? '-'}%
                      </Badge>
                    </HStack>
                    <Text fontSize="sm" color="gray.600" mb={1}>
                      策略: {result.strategy} | {result.market_type === 'spot' ? '現貨' : '合約'}
                    </Text>
                    <HStack spacing={2} fontSize="xs" color="gray.500">
                      <Text>夏普: {result.result?.metrics?.sharpe_ratio?.toFixed(4) ?? '-'}</Text>
                      <Text>回撤: {result.result?.metrics?.max_drawdown?.toFixed(4) ?? '-'}%</Text>
                      <Text>勝率: {result.result?.metrics?.win_rate?.toFixed(4) ?? '-'}%</Text>
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
          <Tab>K 線緩存</Tab>
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
                  <FormControl mb={4}>
                    <FormLabel>總投入資金 (USDT) *</FormLabel>
                    <NumberInput value={totalCapital} min={1} onChange={(_: string, v: number) => setTotalCapital(v)}>
                      <NumberInputField />
                    </NumberInput>
                    <Text fontSize="xs" color="gray.500" mt={1}>默認 10000，選策略後不會被覆蓋</Text>
                  </FormControl>
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
                  {currentStrategyDef?.params
                    ?.filter((p) => p.name !== 'total_capital')
                    ?.map((p) => (
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
                      {p.hint && (
                        <Text fontSize="xs" color="gray.500" mt={1}>
                          {p.hint}
                        </Text>
                      )}
                      {p.unit && <Text as="span" fontSize="xs" color="gray.500" ml={2}>{p.unit}</Text>}
                    </FormControl>
                  ))}
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
                      <Tr
                        key={t.id}
                        bg={t.id === selectedTaskId ? 'blue.50' : undefined}
                        _dark={{ bg: t.id === selectedTaskId ? 'blue.900' : undefined }}
                      >
                        <Td>
                          <Tooltip
                            label={t.status === 'failed' && t.error ? t.error : undefined}
                            isDisabled={t.status !== 'failed' || !t.error}
                            placement="top"
                            maxW="320px"
                          >
                            <Badge
                              colorScheme={
                                t.status === 'completed' ? 'green' : t.status === 'failed' ? 'red' : t.status === 'running' ? 'blue' : 'gray'
                              }
                            >
                              {t.status}
                            </Badge>
                          </Tooltip>
                        </Td>
                        <Td>{t.strategy}</Td>
                        <Td>{t.symbol}</Td>
                        <Td>{t.interval}</Td>
                        <Td>{formatDate(t.start_time)} ~ {formatDate(t.end_time)}</Td>
                        <Td>{formatDate(t.created_at)}</Td>
                        <Td>
                          <HStack>
                            <Button
                              size="xs"
                              onClick={() => {
                                setSelectedTaskId(t.id)
                                reportModal.onOpen()
                              }}
                            >
                              查看
                            </Button>
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
                                if (selectedTaskId === t.id) {
                                  setSelectedTaskId(null)
                                  reportModal.onClose()
                                }
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
            <Modal
              isOpen={reportModal.isOpen}
              onClose={() => {
                reportModal.onClose()
                setSelectedTaskId(null)
              }}
              size="4xl"
              scrollBehavior="inside"
            >
              <ModalOverlay />
              <ModalContent maxH="90vh">
                <ModalHeader>回測結果: {selectedTaskId ?? '-'}</ModalHeader>
                <ModalCloseButton />
                <ModalBody pb={6} overflowY="auto">
                  <Box ref={reportContentRef} bg="white" _dark={{ bg: 'gray.900' }}>
                  {selectedTaskId && (() => {
                    const sel = tasks.find((t) => t.id === selectedTaskId)
                    if (sel?.status === 'failed' && sel?.error) {
                      return (
                        <Alert status="error" borderRadius="md" mb={4}>
                          <AlertIcon />
                          <Box>
                            <AlertTitle>任務執行失敗</AlertTitle>
                            <AlertDescription>{sel.error}</AlertDescription>
                          </Box>
                        </Alert>
                      )
                    }
                    return null
                  })()}
                  {resultData && typeof resultData === 'object' && ('result' in resultData || 'comparison' in resultData) && (
                    <Box mb={4}>
                      {(() => {
                        const data = resultData as BacktestResultData
                        const comp = data.comparison
                        const hasComparison = comp != null

                        const metricBox = (
                          m: Record<string, unknown> | undefined,
                          trades: Array<{ type?: string; quantity?: number }>,
                          endPrice: number,
                          finalCapital: number,
                          label?: string
                        ) => {
                          if (!m) return null
                          let endPosQty = 0
                          for (const t of trades) {
                            if (t.type === 'buy') endPosQty += t.quantity ?? 0
                            else if (t.type === 'sell') endPosQty -= t.quantity ?? 0
                          }
                          if (endPosQty < 0) endPosQty = 0
                          const endPosValue = endPosQty * endPrice
                          const endCashUSDT = Math.max(0, finalCapital - endPosValue)
                          return (
                            <Box key={label || 'single'}>
                              {label && <Text fontSize="xs" fontWeight="600" mb={2} color="gray.600" _dark={{ color: 'gray.400' }}>{label}</Text>}
                              <SimpleGrid columns={[2, 3, 5]} spacing={2}>
                                <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">總收益率</Text><Text fontWeight="600">{String(m.total_return ?? '-')}%</Text></Box>
                                <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">最大回撤</Text><Text fontWeight="600">{String(m.max_drawdown ?? '-')}%</Text></Box>
                                <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">夏普比率</Text><Text fontWeight="600">{String(m.sharpe_ratio ?? '-')}</Text></Box>
                                <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">交易次數</Text><Text fontWeight="600">{String(m.total_trades ?? '-')}</Text></Box>
                                <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">買/賣</Text><Text fontWeight="600">{String(m.buy_count ?? '-')} / {String(m.sell_count ?? '-')}</Text></Box>
                                <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">期末持倉</Text><Text fontWeight="600">{endPosQty.toFixed(6)}</Text></Box>
                                <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">期末持倉市值</Text><Text fontWeight="600">{endPosValue.toFixed(4)} USDT</Text></Box>
                                <Box p={2} bg="gray.50" borderRadius="md" _dark={{ bg: 'gray.700' }}><Text fontSize="xs">期末 USDT</Text><Text fontWeight="600">{endCashUSDT.toFixed(4)}</Text></Box>
                              </SimpleGrid>
                            </Box>
                          )
                        }

                        if (hasComparison && comp?.no_risk_result && comp?.with_risk_result) {
                          const noRisk = comp.no_risk_result
                          const withRisk = comp.with_risk_result
                          const cm = comp.comparison
                          const interventions = comp.with_risk_result?.risk_interventions ?? []
                          return (
                            <>
                              <Text fontSize="sm" fontWeight="600" mb={3}>風控對比（無風控 vs 有風控）</Text>
                              <SimpleGrid columns={{ base: 1, md: 2 }} spacing={4} mb={4}>
                                {metricBox(noRisk.metrics, noRisk.trades ?? [], noRisk.price_curve?.end_price ?? 0, noRisk.final_capital ?? 0, '無風控')}
                                {metricBox(withRisk.metrics, withRisk.trades ?? [], withRisk.price_curve?.end_price ?? 0, withRisk.final_capital ?? 0, '有風控')}
                              </SimpleGrid>
                              {(cm?.risk_intervention_count ?? 0) > 0 && (() => {
                                const maxDisplay = 50
                                const displayList = interventions.slice(0, maxDisplay)
                                const hasMore = interventions.length > maxDisplay
                                return (
                                  <Box mb={4}>
                                    <Text fontSize="sm" fontWeight="600" mb={2}>
                                      風控介入記錄（共 {cm?.risk_intervention_count ?? 0} 次，跳過 {cm?.skipped_signals ?? 0} 個買入信號）
                                      {hasMore && <Text as="span" color="gray.500" fontWeight="normal">（僅顯示前 {maxDisplay} 條）</Text>}
                                    </Text>
                                    <Box overflowX="auto" maxH="200px" overflowY="auto">
                                      <Table size="sm">
                                        <Thead>
                                          <Tr>
                                            <Th>時間</Th>
                                            <Th>原因</Th>
                                            <Th>類型</Th>
                                            <Th>持續K線</Th>
                                            <Th>跳過買入</Th>
                                          </Tr>
                                        </Thead>
                                        <Tbody>
                                          {displayList.map((inv, idx) => (
                                            <Tr key={idx}>
                                              <Td fontSize="xs">{inv.time_str ?? '-'}</Td>
                                              <Td fontSize="xs">{inv.reason ?? '-'}</Td>
                                              <Td fontSize="xs">{inv.risk_type ?? '-'}</Td>
                                              <Td fontSize="xs">{inv.duration ?? '-'}</Td>
                                              <Td fontSize="xs">{inv.skipped_buys ?? '-'}</Td>
                                            </Tr>
                                          ))}
                                        </Tbody>
                                      </Table>
                                    </Box>
                                  </Box>
                                )
                              })()}
                            </>
                          )
                        }

                        const res = data.result
                        const m = res?.metrics
                        const trades = res?.trades ?? []
                        const endPrice = res?.price_curve?.end_price ?? 0
                        const finalCapital = res?.final_capital ?? 0
                        return metricBox(m, trades, endPrice, finalCapital)
                      })()}
                    </Box>
                  )}
                  {klinesData && klinesData.klines.length > 0 && (
                    <Box mb={4}>
                      <Text fontSize="sm" fontWeight="600" mb={2}>期間 K 線走勢（收盤價，拆為 4 段）</Text>
                      <SimpleGrid columns={2} spacing={3}>
                        {(() => {
                          const kls = klinesData.klines
                          const n = kls.length
                          const seg = Math.max(1, Math.ceil(n / 4))
                          return [0, 1, 2, 3].map((i) => {
                            const start = i * seg
                            const end = Math.min((i + 1) * seg, n)
                            const segData = kls.slice(start, end)
                            if (segData.length === 0) return null
                            return (
                              <Box key={i} h="160px" bg="gray.50" borderRadius="md" p={2} _dark={{ bg: 'gray.800' }}>
                                <Text fontSize="xs" color="gray.500" mb={1}>第{i + 1}段</Text>
                                <ResponsiveContainer width="100%" height={130}>
                                  <LineChart data={segData} margin={{ top: 5, right: 5, left: -20, bottom: 0 }}>
                                    <CartesianGrid strokeDasharray="2 2" stroke="rgba(0,0,0,0.05)" />
                                    <XAxis dataKey="time" tick={{ fontSize: 9 }} interval="preserveStartEnd" />
                                    <YAxis domain={['auto', 'auto']} tick={{ fontSize: 9 }} width={45} />
                                    <RechartsTooltip formatter={(v: number) => [v.toFixed(2), '收盤']} />
                                    <Line type="monotone" dataKey="close" stroke="#3182ce" dot={false} strokeWidth={1.5} />
                                  </LineChart>
                                </ResponsiveContainer>
                              </Box>
                            )
                          })
                        })()}
                      </SimpleGrid>
                    </Box>
                  )}
                  {reportMd && (
                    <Box
                      p={4}
                      bg="gray.50"
                      borderRadius="md"
                      overflowX="auto"
                      fontSize="sm"
                      _dark={{ bg: 'gray.800' }}
                      sx={{
                        '& h1': { fontSize: 'xl', fontWeight: 'bold', mt: 2, mb: 2 },
                        '& h2': { fontSize: 'lg', fontWeight: 'semibold', mt: 3, mb: 2 },
                        '& h3': { fontSize: 'md', fontWeight: 'semibold', mt: 2, mb: 1 },
                        '& p': { mb: 2 },
                        '& ul, & ol': { pl: 6, mb: 2 },
                        '& table': { borderCollapse: 'collapse', width: '100%', mb: 3 },
                        '& th, & td': { border: '1px solid', borderColor: 'gray.300', px: 3, py: 2, textAlign: 'left' },
                        '& th': { bg: 'gray.200', fontWeight: 'semibold' },
                        '& code': { bg: 'gray.200', px: 1, py: 0.5, borderRadius: 'sm' },
                        '& pre': { bg: 'gray.200', p: 3, borderRadius: 'md', overflowX: 'auto', whiteSpace: 'pre-wrap' },
                      }}
                    >
                      <ReactMarkdown remarkPlugins={[remarkGfm]}>{reportMd}</ReactMarkdown>
                    </Box>
                  )}
                  {selectedTaskId && !resultData && !reportMd && !tasks.find((t) => t.id === selectedTaskId)?.error && (
                    <Flex justify="center" py={8}>
                      <Spinner />
                      <Text ml={3} color="gray.500">載入中...</Text>
                    </Flex>
                  )}
                  </Box>
                </ModalBody>
                {selectedTaskId && (
                  <ModalFooter gap={2}>
                    {tasks.find((t) => t.id === selectedTaskId)?.status === 'completed' && (
                      <>
                        <Button
                          size="sm"
                          leftIcon={<DownloadIcon />}
                          variant="outline"
                          onClick={handleSaveAsImage}
                          isLoading={savingImage}
                          loadingText="生成中..."
                        >
                          保存為圖片
                        </Button>
                        <Button
                          size="sm"
                          leftIcon={<DownloadIcon />}
                          variant="outline"
                          onClick={() => {
                            getBacktestTaskReport(selectedTaskId, true).then((md) => {
                              const blob = new Blob([md], { type: 'text/markdown' })
                              const a = document.createElement('a')
                              a.href = URL.createObjectURL(blob)
                              a.download = `backtest_${selectedTaskId}.md`
                              a.click()
                            })
                          }}
                        >
                          下載報告
                        </Button>
                      </>
                    )}
                  </ModalFooter>
                )}
              </ModalContent>
            </Modal>
          </TabPanel>
          <TabPanel>
            <Card>
              <CardHeader>
                <Flex justify="space-between" align="center">
                  <Text fontWeight="600">已緩存的 K 線</Text>
                  <Button
                    size="sm"
                    leftIcon={<RepeatIcon />}
                    onClick={loadCachedKlines}
                    isLoading={cachedKlinesLoading}
                  >
                    刷新
                  </Button>
                </Flex>
              </CardHeader>
              <CardBody>
                {cachedKlinesLoading && cachedKlines.length === 0 ? (
                  <Flex justify="center" py={6}><Spinner /></Flex>
                ) : cachedKlines.length === 0 ? (
                  <Text color="gray.500">暫無 K 線緩存。在「新建回測」中選擇交易對、周期與日期後點擊「生成 K 線緩存」即可生成。</Text>
                ) : (
                  <Box overflowX="auto">
                    <Table size="sm">
                      <Thead>
                        <Tr>
                          <Th>緩存名稱</Th>
                          <Th>交易對</Th>
                          <Th>周期</Th>
                          <Th>K 線數</Th>
                          <Th>大小 (MB)</Th>
                          <Th>創建時間</Th>
                          <Th>操作</Th>
                        </Tr>
                      </Thead>
                      <Tbody>
                        {cachedKlines.map((c) => (
                          <Tr key={c.name}>
                            <Td>
                              <Tooltip label={c.name}>
                                <Text fontSize="sm" noOfLines={1} maxW="200px">{c.name}</Text>
                              </Tooltip>
                            </Td>
                            <Td>{c.symbol || '-'}</Td>
                            <Td>{c.interval || '-'}</Td>
                            <Td>{c.candles.toLocaleString()}</Td>
                            <Td>{(c.size_mb ?? 0).toFixed(2)}</Td>
                            <Td>{formatDate(c.created)}</Td>
                            <Td>
                              <Tooltip label="刪除此緩存">
                                <IconButton
                                  aria-label="刪除緩存"
                                  size="xs"
                                  icon={<DeleteIcon />}
                                  colorScheme="red"
                                  variant="ghost"
                                  onClick={() => {
                                    deleteCache(c.name).then((r) => {
                                      if (r.success) {
                                        toast({ title: '緩存已刪除', status: 'success' })
                                        loadCachedKlines()
                                      }
                                    }).catch(() => toast({ title: '刪除失敗', status: 'error' }))
                                  }}
                                />
                              </Tooltip>
                            </Td>
                          </Tr>
                        ))}
                      </Tbody>
                    </Table>
                  </Box>
                )}
              </CardBody>
            </Card>
          </TabPanel>
        </TabPanels>
      </Tabs>
    </Box>
  )
}
