import React, { useEffect, useRef, useState } from 'react'
import { createChart, ColorType, IChartApi, ISeriesApi } from 'lightweight-charts'
import { getStatus, getKlines, KlineData } from '../services/api'

const INTERVALS = ['1m', '5m', '15m', '30m', '1h', '4h', '1d'] as const
type Interval = typeof INTERVALS[number]

const KlineChart: React.FC = () => {
  const chartContainerRef = useRef<HTMLDivElement>(null)
  const chartRef = useRef<IChartApi | null>(null)
  const candlestickSeriesRef = useRef<ISeriesApi<'Candlestick'> | null>(null)
  const volumeSeriesRef = useRef<ISeriesApi<'Histogram'> | null>(null)
  
  const [interval, setInterval] = useState<Interval>('1m')
  const [symbol, setSymbol] = useState<string>('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  // 初始化图表
  useEffect(() => {
    if (!chartContainerRef.current) return

    // 创建图表
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

    // 创建K线系列
    const candlestickSeries = chart.addCandlestickSeries({
      upColor: '#26a69a',
      downColor: '#ef5350',
      borderVisible: false,
      wickUpColor: '#26a69a',
      wickDownColor: '#ef5350',
    })
    candlestickSeriesRef.current = candlestickSeries

    // 创建成交量系列（放在独立的坐标中）
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

    // 设置成交量价格刻度
    chart.priceScale('volume').applyOptions({
      scaleMargins: {
        top: 0.8,
        bottom: 0,
      },
    })

    // 响应式调整大小
    const handleResize = () => {
      if (chartContainerRef.current && chartRef.current) {
        chartRef.current.applyOptions({
          width: chartContainerRef.current.clientWidth,
        })
      }
    }
    window.addEventListener('resize', handleResize)

    return () => {
      window.removeEventListener('resize', handleResize)
      chart.remove()
    }
  }, [])

  // 获取当前交易币种
  useEffect(() => {
    const fetchSymbol = async () => {
      try {
        const response = await getStatus()
        setSymbol(response.status.symbol || '')
      } catch (err) {
        console.error('获取交易币种失败:', err)
      }
    }
    fetchSymbol()
  }, [])

  // 加载K线数据
  useEffect(() => {
    if (!symbol || !candlestickSeriesRef.current || !volumeSeriesRef.current) return

    const loadKlines = async () => {
      setLoading(true)
      setError(null)
      
      try {
        const response = await getKlines(interval, 500)
        
        // 转换数据格式（lightweight-charts使用Unix时间戳，单位为秒）
        const candleData = response.klines.map((k: KlineData) => ({
          time: k.time as number,
          open: k.open,
          high: k.high,
          low: k.low,
          close: k.close,
        }))

        const volumeData = response.klines.map((k: KlineData) => ({
          time: k.time as number,
          value: k.volume,
          color: k.close >= k.open ? '#26a69a80' : '#ef535080',
        }))

        // 更新图表数据
        if (candlestickSeriesRef.current) {
          candlestickSeriesRef.current.setData(candleData)
        }
        if (volumeSeriesRef.current) {
          volumeSeriesRef.current.setData(volumeData)
        }

        // 调整图表以适应数据
        if (chartRef.current) {
          chartRef.current.timeScale().fitContent()
        }
      } catch (err) {
        console.error('加载K线数据失败:', err)
        setError(err instanceof Error ? err.message : '加载K线数据失败')
      } finally {
        setLoading(false)
      }
    }

    loadKlines()

    // 定时刷新（每30秒）
    const intervalId = setInterval(loadKlines, 30000)
    return () => clearInterval(intervalId)
  }, [symbol, interval])

  return (
    <div style={{ padding: '20px' }}>
      <div style={{ marginBottom: '20px', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <h2>K线图 {symbol && `- ${symbol}`}</h2>
        <div style={{ display: 'flex', gap: '10px' }}>
          {INTERVALS.map((iv) => (
            <button
              key={iv}
              onClick={() => setInterval(iv)}
              style={{
                padding: '8px 16px',
                backgroundColor: interval === iv ? '#1890ff' : '#f0f0f0',
                color: interval === iv ? 'white' : 'black',
                border: 'none',
                borderRadius: '4px',
                cursor: 'pointer',
                fontSize: '14px',
              }}
            >
              {iv}
            </button>
          ))}
        </div>
      </div>

      {error && (
        <div style={{ padding: '10px', backgroundColor: '#fff2f0', color: '#ff4d4f', marginBottom: '10px', borderRadius: '4px' }}>
          错误: {error}
        </div>
      )}

      {loading && !error && (
        <div style={{ textAlign: 'center', padding: '40px' }}>加载中...</div>
      )}

      <div
        ref={chartContainerRef}
        style={{
          width: '100%',
          height: '600px',
          border: '1px solid #e0e0e0',
          borderRadius: '4px',
        }}
      />
    </div>
  )
}

export default KlineChart

