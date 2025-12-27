import React, { useState, useEffect } from 'react'
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
} from '@chakra-ui/react'
import { getCurrentSystemMetrics, getDailySystemMetrics, getSystemMetrics, SystemMetrics, DailySystemMetric } from '../services/api'

const SystemMonitor: React.FC = () => {
  const [currentMetrics, setCurrentMetrics] = useState<SystemMetrics | null>(null)
  const [metrics, setMetrics] = useState<SystemMetrics[]>([])
  const [dailyMetrics, setDailyMetrics] = useState<DailySystemMetric[]>([])
  const [timeRange, setTimeRange] = useState<string>('24h')
  const [metricType, setMetricType] = useState<'cpu' | 'memory'>('cpu')
  const [loading, setLoading] = useState<boolean>(false)

  // 获取当前系统状态
  const fetchCurrentMetrics = async () => {
    try {
      const data = await getCurrentSystemMetrics()
      setCurrentMetrics(data)
    } catch (error) {
      console.error('获取当前系统状态失败:', error)
      setCurrentMetrics(null)
    }
  }

  // 获取监控数据
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
      console.error('获取监控数据失败:', error)
    } finally {
      setLoading(false)
    }
  }

  // 初始化数据
  useEffect(() => {
    fetchCurrentMetrics()
    fetchMetrics()

    // 每30秒刷新当前状态
    const interval = setInterval(() => {
      fetchCurrentMetrics()
    }, 30000)

    // 每5分钟刷新历史数据
    const metricsInterval = setInterval(() => {
      fetchMetrics()
    }, 5 * 60 * 1000)

    return () => {
      clearInterval(interval)
      clearInterval(metricsInterval)
    }
  }, [])

  // 当时间范围改变时重新获取数据
  useEffect(() => {
    fetchMetrics()
  }, [timeRange])

  // 准备图表数据
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
          { label: '平均值', data, color: '#4CAF50' },
          { label: '最大值', data: maxData, color: '#f44336' },
          { label: '最小值', data: minData, color: '#2196F3' },
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
        datasets: [{ label: metricType === 'cpu' ? 'CPU使用率 (%)' : '内存使用 (MB)', data, color: '#4CAF50' }],
      }
    }

    return {
      labels: [],
      datasets: [],
    }
  }

  const chartData = prepareChartData()

  // 简化的数据展示
  const renderSimpleChart = () => {
    if (chartData.datasets.length === 0) {
      return <Text color="gray.500" textAlign="center" py={8}>暂无数据</Text>
    }

    const mainDataset = chartData.datasets[0]
    const values = (mainDataset.data as number[]).filter((v) => v != null && !isNaN(v))
    
    if (values.length === 0) {
      return <Text color="gray.500" textAlign="center" py={8}>暂无数据</Text>
    }
    
    const maxValue = Math.max(...values.map((v) => v || 0))
    const minValue = Math.min(...values.map((v) => v || 0))
    const range = maxValue - minValue || 1
    const avgValue = values.reduce((a, b) => a + b, 0) / values.length

    return (
      <Box>
        <Heading size="md" mb={4}>
          {metricType === 'cpu' ? 'CPU使用率趋势' : '内存使用趋势'}
        </Heading>
        <Box display="flex" alignItems="flex-end" h="200px" gap={1}>
          {values.map((value: number, index: number) => {
            if (value == null || isNaN(value)) {
              return null
            }
            const height = ((value - minValue) / range) * 100
            const label = chartData.labels[index] || ''
            return (
              <Box
                key={index}
                flex="1"
                h={`${Math.max(height, 2)}%`}
                bg="blue.500"
                borderRadius="sm"
                title={`${label}: ${value.toFixed(2)}${metricType === 'cpu' ? '%' : ' MB'}`}
                cursor="pointer"
                _hover={{ bg: 'blue.600' }}
              />
            )
          })}
        </Box>
        <SimpleGrid columns={3} spacing={4} mt={4}>
          <Stat size="sm">
            <StatLabel>最大值</StatLabel>
            <StatNumber fontSize="md">
              {typeof maxValue === 'number' && !isNaN(maxValue) ? maxValue.toFixed(2) : '--'}
              {metricType === 'cpu' ? '%' : ' MB'}
            </StatNumber>
          </Stat>
          <Stat size="sm">
            <StatLabel>最小值</StatLabel>
            <StatNumber fontSize="md">
              {typeof minValue === 'number' && !isNaN(minValue) ? minValue.toFixed(2) : '--'}
              {metricType === 'cpu' ? '%' : ' MB'}
            </StatNumber>
          </Stat>
          <Stat size="sm">
            <StatLabel>平均值</StatLabel>
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
        <Heading size="lg">系统监控</Heading>
        <HStack spacing={4}>
          <Select
            value={timeRange}
            onChange={(e) => setTimeRange(e.target.value)}
            w="150px"
          >
            <option value="1h">最近1小时</option>
            <option value="6h">最近6小时</option>
            <option value="24h">最近24小时</option>
            <option value="7d">最近7天</option>
            <option value="30d">最近30天</option>
          </Select>
          <Select
            value={metricType}
            onChange={(e) => setMetricType(e.target.value as 'cpu' | 'memory')}
            w="150px"
          >
            <option value="cpu">CPU使用率</option>
            <option value="memory">内存使用</option>
          </Select>
        </HStack>
      </Box>

      {/* 当前状态卡片 */}
      <SimpleGrid columns={{ base: 1, md: 2, lg: 4 }} spacing={4} mb={8}>
        <Card>
          <CardBody>
            <Stat>
              <StatLabel>当前CPU使用率</StatLabel>
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
              <StatLabel>当前内存使用</StatLabel>
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
              <StatLabel>内存占用百分比</StatLabel>
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
              <StatLabel>进程ID</StatLabel>
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
