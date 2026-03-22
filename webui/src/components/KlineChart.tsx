import React, { useEffect, useRef, useState, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { createChart, ColorType, IChartApi, ISeriesApi } from 'lightweight-charts'
import { Box, HStack, Button } from '@chakra-ui/react'
import { useSymbol } from '../contexts/SymbolContext'
import { getStatus, getKlines, KlineData } from '../services/api'
import VolatilityIndicator from './VolatilityIndicator'

const INTERVALS = ['1m', '5m', '15m', '30m', '1h', '4h', '1d'] as const
type Interval = typeof INTERVALS[number]

// 节流函數
function throttle<T extends (...args: any[]) => any>(func: T, wait: number): T {
  let timeout: NodeJS.Timeout | null = null
  let previous = 0
  
  return ((...args: Parameters<T>) => {
    const now = Date.now()
    const remaining = wait - (now - previous)
    
    if (remaining <= 0 || remaining > wait) {
      if (timeout) {
        clearTimeout(timeout)
        timeout = null
      }
      previous = now
      func(...args)
    } else if (!timeout) {
      timeout = setTimeout(() => {
        previous = Date.now()
        timeout = null
        func(...args)
      }, remaining)
    }
  }) as T
}

// 根據interval獲取刷新间隔（毫秒）
const getRefreshInterval = (interval: Interval): number => {
  switch (interval) {
    case '1m': return 30000   // 30秒
    case '5m': return 120000  // 2分钟
    case '15m': return 300000 // 5分钟
    case '30m': return 600000 // 10分钟
    case '1h': return 900000  // 15分钟
    case '4h': return 1800000 // 30分钟
    case '1d': return 3600000 // 1小時
    default: return 30000
  }
}

// 根據interval獲取合适的limit
const getLimitByInterval = (interval: Interval): number => {
  switch (interval) {
    case '1m': return 500   // 1分钟，500条约8小時
    case '5m': return 300   // 5分钟，300条约25小時
    case '15m': return 200  // 15分钟，200条约50小時
    case '30m': return 200  // 30分钟，200条约100小時
    case '1h': return 200   // 1小時，200条约8天
    case '4h': return 150   // 4小時，150条约25天
    case '1d': return 100   // 1天，100条约3個月
    default: return 500
  }
}

interface KlineChartProps {
  /** 全局模式下覆蓋交易所（無 bot 時從 URL 傳入） */
  overrideExchange?: string | null
  /** 全局模式下覆蓋交易對（無 bot 時從 URL 傳入） */
  overrideSymbol?: string | null
}

const KlineChart: React.FC<KlineChartProps> = ({ overrideExchange, overrideSymbol } = {}) => {
  const { t } = useTranslation()
  const { selectedExchange, selectedSymbol } = useSymbol()
  const exchange = overrideExchange ?? selectedExchange
  const symbol = overrideSymbol ?? selectedSymbol
  const chartContainerRef = useRef<HTMLDivElement>(null)
  const chartRef = useRef<IChartApi | null>(null)
  const candlestickSeriesRef = useRef<ISeriesApi<'Candlestick'> | null>(null)
  const volumeSeriesRef = useRef<ISeriesApi<'Histogram'> | null>(null)
  
  const [interval, setInterval] = useState<Interval>('1m')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [klineDataForMetrics, setKlineDataForMetrics] = useState<KlineData[]>([])
  
  // 用於取消正在進行的请求
  const abortControllerRef = useRef<AbortController | null>(null)
  // 防抖定時器
  const debounceTimerRef = useRef<NodeJS.Timeout | null>(null)
  // 缓存已加載的數據，用於增量更新
  const cachedDataRef = useRef<{
    candleData: Array<{ time: number; open: number; high: number; low: number; close: number }>
    volumeData: Array<{ time: number; value: number; color: string }>
    lastUpdateTime: number
  } | null>(null)
  // 標記是否是首次加載
  const isFirstLoadRef = useRef<boolean>(true)

  // 初始化图表
  useEffect(() => {
    if (!chartContainerRef.current) return

    // 創建图表
    const chart = createChart(chartContainerRef.current, {
      layout: {
        background: { type: ColorType.Solid, color: 'white' },
        textColor: 'black',
      },
      width: chartContainerRef.current.clientWidth,
      height: 600,
      grid: {
        vertLines: { color: '#f0f0f0' },
        horzLines: { color: '#f0f0f0' },
      },
      timeScale: {
        timeVisible: true,
        secondsVisible: false,
      },
    })

    chartRef.current = chart

    // 創建K線系列
    const candlestickSeries = chart.addCandlestickSeries({
      upColor: '#26a69a',
      downColor: '#ef5350',
      borderVisible: false,
      wickUpColor: '#26a69a',
      wickDownColor: '#ef5350',
    })
    candlestickSeriesRef.current = candlestickSeries

    // 創建成交量系列（放在独立的坐標中）
    const volumeSeries = chart.addHistogramSeries({
      color: '#26a69a',
      priceFormat: {
        type: 'volume',
      },
      priceScaleId: 'volume',
      scaleMargins: {
        top: 0.8,
        bottom: 0,
      },
    })
    volumeSeriesRef.current = volumeSeries

    // 設置成交量價格刻度
    chart.priceScale('volume').applyOptions({
      scaleMargins: {
        top: 0.8,
        bottom: 0,
      },
    })

    // 响应式調整大小（使用节流优化性能）
    const handleResize = throttle(() => {
      if (chartContainerRef.current && chartRef.current) {
        chartRef.current.applyOptions({
          width: chartContainerRef.current.clientWidth,
        })
      }
    }, 150) // 150ms节流
    
    window.addEventListener('resize', handleResize)

    return () => {
      window.removeEventListener('resize', handleResize)
      chart.remove()
    }
  }, [])


  // 轉换K線數據格式（使用useMemo缓存）
  const transformKlineData = useCallback((klines: KlineData[]) => {
    const candleData = klines.map((k) => ({
      time: k.time as number,
      open: k.open,
      high: k.high,
      low: k.low,
      close: k.close,
    }))

    const volumeData = klines.map((k) => ({
      time: k.time as number,
      value: k.volume,
      color: k.close >= k.open ? '#26a69a80' : '#ef535080',
    }))

    return { candleData, volumeData }
  }, [])

  // 增量更新數據（只更新新增或变更的K線）
  const updateChartIncremental = useCallback((
    newCandleData: Array<{ time: number; open: number; high: number; low: number; close: number }>,
    newVolumeData: Array<{ time: number; value: number; color: string }>
  ) => {
    const cached = cachedDataRef.current
    const isFirstLoad = isFirstLoadRef.current

    if (isFirstLoad || !cached || cached.candleData.length === 0) {
      // 首次加載或缓存為空，使用全量更新
      if (candlestickSeriesRef.current) {
        candlestickSeriesRef.current.setData(newCandleData)
      }
      if (volumeSeriesRef.current) {
        volumeSeriesRef.current.setData(newVolumeData)
      }
      
      // 只在首次加載時調整視图
      if (isFirstLoad && chartRef.current) {
        chartRef.current.timeScale().fitContent()
        isFirstLoadRef.current = false
      }
    } else {
      // 增量更新：比较最后一根K線
      const lastCachedTime = cached.candleData[cached.candleData.length - 1]?.time
      const lastNewTime = newCandleData[newCandleData.length - 1]?.time
      
      if (lastCachedTime === lastNewTime) {
        // 最后一根K線時间戳相同，只更新这一根（可能是價格變化）
        const lastCandle = newCandleData[newCandleData.length - 1]
        const lastVolume = newVolumeData[newVolumeData.length - 1]
        
        if (lastCandle && candlestickSeriesRef.current) {
          candlestickSeriesRef.current.update(lastCandle)
        }
        if (lastVolume && volumeSeriesRef.current) {
          volumeSeriesRef.current.update(lastVolume)
        }
      } else if (lastNewTime > lastCachedTime) {
        // 有新K線，需要更新最后一根並添加新K線
        // lightweight-charts 的 update 方法：如果時间戳更新，會自动添加新K線
        // 所以先更新最后一根（如果存在），然后添加新K線
        const lastCachedIndex = newCandleData.findIndex(d => d.time === lastCachedTime)
        
        if (lastCachedIndex >= 0 && lastCachedIndex < newCandleData.length - 1) {
          // 更新最后一根已存在的K線
          if (candlestickSeriesRef.current) {
            candlestickSeriesRef.current.update(newCandleData[lastCachedIndex])
          }
          if (volumeSeriesRef.current) {
            volumeSeriesRef.current.update(newVolumeData[lastCachedIndex])
          }
        }
        
        // 添加新K線（lightweight-charts 會自动添加時间戳更新的K線）
        const newCandles = newCandleData.slice(lastCachedIndex + 1)
        const newVolumes = newVolumeData.slice(lastCachedIndex + 1)
        
        for (let i = 0; i < newCandles.length; i++) {
          if (candlestickSeriesRef.current) {
            candlestickSeriesRef.current.update(newCandles[i])
          }
          if (volumeSeriesRef.current) {
            volumeSeriesRef.current.update(newVolumes[i])
          }
        }
      } else {
        // 數據回退或大幅变化，使用全量更新
        if (candlestickSeriesRef.current) {
          candlestickSeriesRef.current.setData(newCandleData)
        }
        if (volumeSeriesRef.current) {
          volumeSeriesRef.current.setData(newVolumeData)
        }
      }
    }

    // 更新缓存
    cachedDataRef.current = {
      candleData: newCandleData,
      volumeData: newVolumeData,
      lastUpdateTime: Date.now(),
    }
  }, [])

  // 加載K線數據（带取消支援和增量更新）
  const loadKlines = useCallback(async (currentInterval: Interval, isInitialLoad = false) => {
    // 如果没有交易對，不加載
    if (!symbol) {
      return
    }

    // 取消之前的请求
    if (abortControllerRef.current) {
      abortControllerRef.current.abort()
    }
    
    // 創建新的AbortController
    const abortController = new AbortController()
    abortControllerRef.current = abortController
    
    // 只在初始加載時显示loading状態
    if (isInitialLoad) {
      setLoading(true)
    }
    setError(null)
    
    try {
      const limit = getLimitByInterval(currentInterval)
      const response = await getKlines(currentInterval, limit, exchange || undefined, symbol || undefined, abortController.signal)
      
      // 检查请求是否已被取消
      if (abortController.signal.aborted) {
        return
      }
      
      // 轉换數據格式
      const { candleData, volumeData } = transformKlineData(response.klines)

      // 检查请求是否已被取消（在數據处理后再次检查）
      if (abortController.signal.aborted) {
        return
      }

      // 增量更新图表數據
      updateChartIncremental(candleData, volumeData)

      // 保存最新 kline 数据用于波动率指标计算
      setKlineDataForMetrics(response.klines)
    } catch (err) {
      // 忽略取消的请求錯误
      if (abortController.signal.aborted) {
        return
      }
      console.error('加載K線數據失败:', err)
      setError(err instanceof Error ? err.message : t('klineChart.loadFailed'))
    } finally {
      if (!abortController.signal.aborted) {
        setLoading(false)
      }
    }
  }, [exchange, symbol, transformKlineData, updateChartIncremental])

  // 加載K線數據（带防抖）
  useEffect(() => {
    if (!symbol || !candlestickSeriesRef.current || !volumeSeriesRef.current) return

    // 清除之前的防抖定時器
    if (debounceTimerRef.current) {
      clearTimeout(debounceTimerRef.current)
    }

    // 切换interval時重置首次加載標記和缓存
    isFirstLoadRef.current = true
    cachedDataRef.current = null

    // 立即加載一次（初始加載）
    loadKlines(interval, true)

    // 根據interval設置定時刷新
    const refreshInterval = getRefreshInterval(interval)
    const intervalId = setInterval(() => {
      loadKlines(interval, false) // 后续更新不是初始加載
    }, refreshInterval)

    return () => {
      clearInterval(intervalId)
      if (debounceTimerRef.current) {
        clearTimeout(debounceTimerRef.current)
      }
      // 清理時取消正在進行的请求
      if (abortControllerRef.current) {
        abortControllerRef.current.abort()
      }
    }
  }, [symbol, exchange, interval, loadKlines])

  const handleIntervalClick = useCallback((iv: Interval) => {
    if (debounceTimerRef.current) {
      clearTimeout(debounceTimerRef.current)
    }
    debounceTimerRef.current = setTimeout(() => setInterval(iv), 300)
  }, [])

  return (
    <Box padding="20px">
      <Box mb="20px" display="flex" justifyContent="space-between" alignItems="center" flexWrap="wrap" gap="3">
        <h2 style={{ margin: 0 }}>{t('klineChart.title')} {symbol && `- ${symbol}`}</h2>
        {/* Apple HIG Segmented Control: 胶囊轨道 + 选中态白底阴影 */}
        <Box
          as="nav"
          display="inline-flex"
          p="3px"
          bg="gray.200"
          borderRadius="full"
          role="group"
          aria-label={t('klineChart.intervalSelector')}
          sx={{
            boxShadow: 'inset 0 1px 2px rgba(0,0,0,0.06)',
          }}
        >
          <HStack spacing="2px" role="tablist">
            {INTERVALS.map((iv) => {
              const isSelected = interval === iv
              return (
                <Button
                  key={iv}
                  role="tab"
                  aria-selected={isSelected}
                  aria-label={iv}
                  size="sm"
                  variant="ghost"
                  minW="44px"
                  h="32px"
                  px="14px"
                  borderRadius="full"
                  fontSize="13px"
                  fontWeight={isSelected ? 600 : 500}
                  color={isSelected ? 'gray.800' : 'gray.600'}
                  bg={isSelected ? 'white' : 'transparent'}
                  boxShadow={isSelected ? '0 1px 3px rgba(0,0,0,0.12), 0 1px 2px rgba(0,0,0,0.08)' : 'none'}
                  transition="all 0.2s cubic-bezier(0.4, 0, 0.2, 1)"
                  _hover={{
                    bg: isSelected ? 'white' : 'blackAlpha.100',
                    color: isSelected ? 'gray.800' : 'gray.700',
                  }}
                  _active={{
                    transform: 'scale(0.98)',
                  }}
                  _focusVisible={{
                    ring: 2,
                    ringColor: 'brand.400',
                  }}
                  onClick={() => handleIntervalClick(iv)}
                >
                  {iv}
                </Button>
              )
            })}
          </HStack>
        </Box>
      </Box>

      {error && (
        <Box p="10px" bg="red.50" color="red.600" mb="10px" borderRadius="md">
          {t('common.error')}: {error}
        </Box>
      )}

      {loading && !error && (
        <Box textAlign="center" p="40px">{t('common.loading')}</Box>
      )}

      {/* 波动率指标面板 */}
      {klineDataForMetrics.length > 0 && (
        <VolatilityIndicator klines={klineDataForMetrics} />
      )}

      <Box
        ref={chartContainerRef}
        w="100%"
        h="600px"
        border="1px solid"
        borderColor="gray.200"
        borderRadius="md"
      />
    </Box>
  )
}

export default KlineChart

