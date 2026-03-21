import React, { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import {
  Box,
  Heading,
  SimpleGrid,
  Card,
  CardBody,
  Stat,
  StatLabel,
  StatNumber,
  StatHelpText,
  Select,
  HStack,
  Progress,
  Text,
  Spinner,
  Center,
  Tooltip,
  VStack,
  HStack as TooltipHStack,
} from '@chakra-ui/react'
import { getCurrentSystemMetrics, getDailySystemMetrics, getSystemMetrics, SystemMetrics, DailySystemMetric } from '../services/api'
import { useConfig } from '../contexts/ConfigContext'
import { formatDateTime, formatTime } from '../utils/dateFormat'

const SystemMonitor: React.FC = () => {
  const { t, i18n } = useTranslation()
  const { timezone } = useConfig()
  const [currentMetrics, setCurrentMetrics] = useState<SystemMetrics | null>(null)
  const [metrics, setMetrics] = useState<SystemMetrics[]>([])
  const [dailyMetrics, setDailyMetrics] = useState<DailySystemMetric[]>([])
  const [timeRange, setTimeRange] = useState<string>('24h')
  const [metricType, setMetricType] = useState<'cpu' | 'memory'>('cpu')
  const [loading, setLoading] = useState<boolean>(false)

  // 獲取當前系统状態
  const fetchCurrentMetrics = async () => {
    try {
      const data = await getCurrentSystemMetrics()
      setCurrentMetrics(data)
    } catch (error) {
      console.error('獲取當前系统状態失败:', error)
      setCurrentMetrics(null)
    }
  }

  // 獲取監控數據
  const fetchMetrics = async () => {
    setLoading(true)
    try {
      const now = new Date()
      let startTime: Date
      let useDaily = false

      switch (timeRange) {
        case '1h':
          startTime = new Date(now.getTime() - 60 * 60 * 1000)
          break
        case '6h':
          startTime = new Date(now.getTime() - 6 * 60 * 60 * 1000)
          break
        case '24h':
          startTime = new Date(now.getTime() - 24 * 60 * 60 * 1000)
          break
        case '7d':
          startTime = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000)
          break
        case '30d':
          startTime = new Date(now.getTime() - 30 * 24 * 60 * 60 * 1000)
          useDaily = true
          break
        default:
          startTime = new Date(now.getTime() - 24 * 60 * 60 * 1000)
      }

      if (useDaily) {
        const days = Math.ceil((now.getTime() - startTime.getTime()) / (24 * 60 * 60 * 1000))
        const data = await getDailySystemMetrics(days)
        setDailyMetrics(data.metrics || [])
        setMetrics([])
      } else {
        const startTimeStr = startTime.toISOString()
        const endTimeStr = now.toISOString()
        const data = await getSystemMetrics({
          start_time: startTimeStr,
          end_time: endTimeStr,
          granularity: 'detail'
        })
        setMetrics(data.metrics || [])
        setDailyMetrics([])
      }
    } catch (error) {
      console.error('獲取監控數據失败:', error)
    } finally {
      setLoading(false)
    }
  }

  // 初始化數據
  useEffect(() => {
    fetchCurrentMetrics()
    fetchMetrics()

    // 每30秒刷新當前状態
    const interval = setInterval(() => {
      fetchCurrentMetrics()
    }, 30000)

    // 每5分钟刷新历史數據
    const metricsInterval = setInterval(() => {
      fetchMetrics()
    }, 5 * 60 * 1000)

    return () => {
      clearInterval(interval)
      clearInterval(metricsInterval)
    }
  }, [])

  // 當時间範圍改变時重新獲取數據
  useEffect(() => {
    fetchMetrics()
  }, [timeRange])

  // 准备图表數據
  const prepareChartData = () => {
    if (timeRange === '30d' && dailyMetrics.length > 0) {
      const labels = dailyMetrics.map((m) => m.date || '')
      const data = dailyMetrics.map((m) => {
        const value = metricType === 'cpu' ? m.avg_cpu_percent : m.avg_memory_mb
        return typeof value === 'number' && !isNaN(value) ? value : 0
      })
      const maxData = dailyMetrics.map((m) => {
        const value = metricType === 'cpu' ? m.max_cpu_percent : m.max_memory_mb
        return typeof value === 'number' && !isNaN(value) ? value : 0
      })
      const minData = dailyMetrics.map((m) => {
        const value = metricType === 'cpu' ? m.min_cpu_percent : m.min_memory_mb
        return typeof value === 'number' && !isNaN(value) ? value : 0
      })

      return {
        labels,
        datasets: [
          { label: t('systemMonitor.avgLabel'), data, color: '#4CAF50' },
          { label: t('systemMonitor.maxLabel'), data: maxData, color: '#f44336' },
          { label: t('systemMonitor.minLabel'), data: minData, color: '#2196F3' },
        ],
      }
    } else if (metrics.length > 0) {
      const labels = metrics.map((m) => m.timestamp || '')
      const data = metrics.map((m) => {
        const value = metricType === 'cpu' ? m.cpu_percent : m.memory_mb
        return typeof value === 'number' && !isNaN(value) ? value : 0
      })

      return {
        labels,
        datasets: [{ label: metricType === 'cpu' ? t('systemMonitor.cpuUsage') + ' (%)' : t('systemMonitor.memoryUsage') + ' (MB)', data, color: '#4CAF50' }],
      }
    }

    return {
      labels: [],
      datasets: [],
    }
  }

  const chartData = prepareChartData()

  // 简化的數據展示
  const renderSimpleChart = () => {
    if (chartData.datasets.length === 0) {
      return (
        <Box textAlign="center" py={8}>
          <Text color="gray.500">{t('systemMonitor.noData')}</Text>
          <Text color="gray.400" fontSize="sm" mt={2}>{t('systemMonitor.noDataHint')}</Text>
        </Box>
      )
    }

    const mainDataset = chartData.datasets[0]
    const rawValues = (mainDataset.data as number[]).filter((v) => v != null && !isNaN(v))
    const rawLabels = chartData.labels as string[]
    
    if (rawValues.length === 0) {
      return (
        <Box textAlign="center" py={8}>
          <Text color="gray.500">{t('systemMonitor.noData')}</Text>
          <Text color="gray.400" fontSize="sm" mt={2}>{t('systemMonitor.noDataHint')}</Text>
        </Box>
      )
    }

    // 對數據進行采样，最多显示 100 個數據点以保证图表可读性
    const maxDataPoints = 100
    let values: number[] = rawValues
    let sampledLabels: string[] = rawLabels
    
    if (rawValues.length > maxDataPoints) {
      const step = Math.ceil(rawValues.length / maxDataPoints)
      values = []
      sampledLabels = []
      for (let i = 0; i < rawValues.length; i += step) {
        // 取区间内的平均值
        const end = Math.min(i + step, rawValues.length)
        const slice = rawValues.slice(i, end)
        const avg = slice.reduce((a, b) => a + b, 0) / slice.length
        values.push(avg)
        sampledLabels.push(rawLabels[i] || '')
      }
    }
    
    const maxValue = Math.max(...values.map((v) => v || 0))
    const minValue = Math.min(...values.map((v) => v || 0))
    const range = maxValue - minValue || 1
    const avgValue = values.reduce((a, b) => a + b, 0) / values.length

    // 格式化時间標签
    const formatTimeLabel = (timestamp: string) => {
      if (!timestamp) return ''
      const date = new Date(timestamp)
      if (isNaN(date.getTime())) return ''

      // 根據時间範圍决定显示格式
      if (timeRange === '1h' || timeRange === '6h') {
        // 短時间範圍显示時:分
        return formatTime(timestamp, timezone, i18n.language).slice(-8) // HH:mm:ss
      } else if (timeRange === '24h' || timeRange === '7d') {
        // 中等時间範圍显示月/日 時:分
        return formatDateTime(timestamp, timezone, i18n.language).replace(/\//g, '-').slice(0, -3) // 去掉秒
      } else {
        // 长時间範圍显示月-日
        return formatDateTime(timestamp, timezone, i18n.language).replace(/\//g, '-').slice(0, 10)
      }
    }

    // 计算要显示的時间標签（避免太密集）
    const getTimeLabels = () => {
      if (sampledLabels.length === 0) return []
      
      // 根據數據点數量决定显示几個標签
      const maxLabels = 6
      const step = Math.max(1, Math.floor(sampledLabels.length / maxLabels))
      const result = []
      
      for (let i = 0; i < sampledLabels.length; i += step) {
        result.push({
          index: i,
          label: formatTimeLabel(sampledLabels[i])
        })
      }
      
      // 确保显示最后一個標签
      if (result[result.length - 1]?.index !== sampledLabels.length - 1) {
        result.push({
          index: sampledLabels.length - 1,
          label: formatTimeLabel(sampledLabels[sampledLabels.length - 1])
        })
      }
      
      return result
    }

    const timeLabels = getTimeLabels()

    return (
      <Box>
        <Heading size="md" mb={4}>
          {metricType === 'cpu' ? t('systemMonitor.cpuTrend') : t('systemMonitor.memoryTrend')}
        </Heading>
        <Box display="flex" h="200px" gap="1px">
          {/* Y轴標签 */}
          <VStack spacing={0} justify="space-between" h="100%" w="40px" pr={2}>
            <Text fontSize="xs" color="gray.600">
              {maxValue.toFixed(1)}{metricType === 'cpu' ? '%' : ' MB'}
            </Text>
            <Text fontSize="xs" color="gray.600">
              {((maxValue + minValue) / 2).toFixed(1)}{metricType === 'cpu' ? '%' : ' MB'}
            </Text>
            <Text fontSize="xs" color="gray.600">
              {minValue.toFixed(1)}{metricType === 'cpu' ? '%' : ' MB'}
            </Text>
          </VStack>
          
          <Box display="flex" alignItems="flex-end" flex="1" gap="1px">
            {values.map((value: number, index: number) => {
              if (value == null || isNaN(value)) {
                return null
              }
              const height = ((value - minValue) / range) * 100
              const label = sampledLabels[index] || ''
              const displayValue = value.toFixed(2)
              const displayLabel = formatTimeLabel(label)
              const unit = metricType === 'cpu' ? '%' : ' MB'
              
              return (
                <Tooltip
                  key={index}
                  label={`${displayLabel}\n${displayValue}${unit}`}
                  placement="top"
                  hasArrow
                  bg="gray.800"
                  color="white"
                  p={2}
                  borderRadius="md"
                  fontSize="sm"
                  closeOnClick={false}
                >
                  <Box
                    flex="1"
                    minW="3px"
                    h={`${Math.max(height, 5)}%`}
                    bg="blue.500"
                    borderRadius="sm"
                    cursor="pointer"
                    _hover={{ bg: 'blue.600' }}
                    transition="background-color 0.2s ease"
                  />
                </Tooltip>
              )
            })}
          </Box>
        </Box>
        {/* 時间轴 */}
        <Box display="flex" justifyContent="space-between" mt={2} px={1}>
          {timeLabels.map((item, idx) => (
            <Text key={idx} fontSize="xs" color="gray.500">
              {item.label}
            </Text>
          ))}
        </Box>
        <SimpleGrid columns={3} spacing={4} mt={4}>
          <Stat size="sm">
            <StatLabel>{t('systemMonitor.maxLabel')}</StatLabel>
            <StatNumber fontSize="md">
              {typeof maxValue === 'number' && !isNaN(maxValue) ? maxValue.toFixed(2) : '--'}
              {metricType === 'cpu' ? '%' : ' MB'}
            </StatNumber>
          </Stat>
          <Stat size="sm">
            <StatLabel>{t('systemMonitor.minLabel')}</StatLabel>
            <StatNumber fontSize="md">
              {typeof minValue === 'number' && !isNaN(minValue) ? minValue.toFixed(2) : '--'}
              {metricType === 'cpu' ? '%' : ' MB'}
            </StatNumber>
          </Stat>
          <Stat size="sm">
            <StatLabel>{t('systemMonitor.avgLabel')}</StatLabel>
            <StatNumber fontSize="md">
              {typeof avgValue === 'number' && !isNaN(avgValue) ? avgValue.toFixed(2) : '--'}
              {metricType === 'cpu' ? '%' : ' MB'}
            </StatNumber>
          </Stat>
        </SimpleGrid>
      </Box>
    )
  }

  return (
    <Box>
      <Box display="flex" justifyContent="space-between" alignItems="center" mb={6}>
        <Heading size="lg">{t('systemMonitor.title')}</Heading>
        <HStack spacing={4}>
          <Select
            value={timeRange}
            onChange={(e) => setTimeRange(e.target.value)}
            w="150px"
          >
            <option value="1h">{t('systemMonitor.last1h')}</option>
            <option value="6h">{t('systemMonitor.last6h')}</option>
            <option value="24h">{t('systemMonitor.last24h')}</option>
            <option value="7d">{t('systemMonitor.last7d')}</option>
            <option value="30d">{t('systemMonitor.last30d')}</option>
          </Select>
          <Select
            value={metricType}
            onChange={(e) => setMetricType(e.target.value as 'cpu' | 'memory')}
            w="150px"
          >
            <option value="cpu">{t('systemMonitor.cpuUsage')}</option>
            <option value="memory">{t('systemMonitor.memoryUsage')}</option>
          </Select>
        </HStack>
      </Box>

      {/* 當前状態卡片 */}
      <SimpleGrid columns={{ base: 1, md: 2, lg: 4 }} spacing={4} mb={8}>
        <Card>
          <CardBody>
            <Stat>
              <StatLabel>{t('systemMonitor.currentCpuUsage')}</StatLabel>
              <StatNumber>
                {currentMetrics && typeof currentMetrics.cpu_percent === 'number' 
                  ? `${currentMetrics.cpu_percent.toFixed(2)}%` 
                  : '--'}
              </StatNumber>
              {currentMetrics && typeof currentMetrics.cpu_percent === 'number' && (
                <Box mt={2}>
                  <Progress 
                    value={currentMetrics.cpu_percent} 
                    colorScheme={currentMetrics.cpu_percent > 80 ? 'red' : currentMetrics.cpu_percent > 50 ? 'orange' : 'green'}
                    size="sm"
                    borderRadius="full"
                  />
                </Box>
              )}
            </Stat>
          </CardBody>
        </Card>

        <Card>
          <CardBody>
            <Stat>
              <StatLabel>{t('systemMonitor.currentMemoryUsage')}</StatLabel>
              <StatNumber>
                {currentMetrics && typeof currentMetrics.memory_mb === 'number' 
                  ? `${currentMetrics.memory_mb.toFixed(2)} MB` 
                  : '--'}
              </StatNumber>
            </Stat>
          </CardBody>
        </Card>

        <Card>
          <CardBody>
            <Stat>
              <StatLabel>{t('systemMonitor.memoryPercent')}</StatLabel>
              <StatNumber>
                {currentMetrics && typeof currentMetrics.memory_percent === 'number' 
                  ? `${currentMetrics.memory_percent.toFixed(2)}%` 
                  : '--'}
              </StatNumber>
              {currentMetrics && typeof currentMetrics.memory_percent === 'number' && (
                <Box mt={2}>
                  <Progress 
                    value={currentMetrics.memory_percent} 
                    colorScheme={currentMetrics.memory_percent > 80 ? 'red' : currentMetrics.memory_percent > 50 ? 'orange' : 'green'}
                    size="sm"
                    borderRadius="full"
                  />
                </Box>
              )}
            </Stat>
          </CardBody>
        </Card>

        <Card>
          <CardBody>
            <Stat>
              <StatLabel>{t('systemMonitor.processId')}</StatLabel>
              <StatNumber>
                {currentMetrics && currentMetrics.process_id 
                  ? currentMetrics.process_id 
                  : '--'}
              </StatNumber>
            </Stat>
          </CardBody>
        </Card>
      </SimpleGrid>

      {/* 图表 */}
      <Card>
        <CardBody>
          {loading ? (
            <Center py={8}>
              <Spinner size="xl" />
            </Center>
          ) : (
            renderSimpleChart()
          )}
        </CardBody>
      </Card>
    </Box>
  )
}

export default SystemMonitor
