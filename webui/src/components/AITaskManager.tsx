import React, { useState, useEffect } from 'react'
import {
  Box,
  VStack,
  HStack,
  Flex,
  Heading,
  Text,
  Badge,
  Button,
  Table,
  Thead,
  Tbody,
  Tr,
  Th,
  Td,
  Spinner,
  Center,
  useDisclosure,
  Card,
  CardBody,
  SimpleGrid,
  Stat,
  StatLabel,
  StatNumber,
  StatHelpText,
  Select,
  Input,
  Modal,
  ModalOverlay,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalFooter,
  ModalCloseButton,
  Code,
  Divider,
  useColorModeValue,
} from '@chakra-ui/react'
import { StarIcon } from '@chakra-ui/icons'
import { getAITasks, getAITaskStats, AITask, AITaskFilter, AITaskStats } from '../services/api'
import { useTranslation } from 'react-i18next'

const AITaskManager: React.FC = () => {
  const { t } = useTranslation()
  const [tasks, setTasks] = useState<AITask[]>([])
  const [stats, setStats] = useState<AITaskStats | null>(null)
  const [loading, setLoading] = useState(true)
  const [selectedTask, setSelectedTask] = useState<AITask | null>(null)
  const [statusFilter, setStatusFilter] = useState<string>('')
  const [startDate, setStartDate] = useState<string>('')
  const [endDate, setEndDate] = useState<string>('')
  const { isOpen, onOpen, onClose } = useDisclosure()

  const bg = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue('gray.200', 'gray.700')

  // 加载任务数据
  const loadTasks = async () => {
    try {
      setLoading(true)
      const filter: AITaskFilter = { limit: 100 }
      
      if (statusFilter) {
        filter.status = statusFilter
      }
      if (startDate) {
        filter.start_time = new Date(startDate).toISOString()
      }
      if (endDate) {
        // 设置到当天的23:59:59
        const end = new Date(endDate)
        end.setHours(23, 59, 59, 999)
        filter.end_time = end.toISOString()
      }
      
      const data = await getAITasks(filter)
      setTasks(data.tasks || [])
    } catch (error) {
      console.error('加载任务失败:', error)
    } finally {
      setLoading(false)
    }
  }

  // 加载统计数据
  const loadStats = async () => {
    try {
      const data = await getAITaskStats()
      setStats(data)
    } catch (error) {
      console.error('加载统计失败:', error)
    }
  }

  useEffect(() => {
    loadTasks()
    loadStats()
    
    // 定时刷新
    const interval = setInterval(() => {
      loadTasks()
      loadStats()
    }, 30000) // 30秒刷新一次
    
    return () => clearInterval(interval)
  }, [statusFilter, startDate, endDate])

  // 打开任务详情
  const handleTaskClick = (task: AITask) => {
    setSelectedTask(task)
    onOpen()
  }

  // 获取状态徽章
  const getStatusBadge = (status: string) => {
    const config: Record<string, { colorScheme: string; label: string }> = {
      pending: { colorScheme: 'yellow', label: '待处理' },
      running: { colorScheme: 'blue', label: '运行中' },
      completed: { colorScheme: 'green', label: '已完成' },
      failed: { colorScheme: 'red', label: '失败' },
      timeout: { colorScheme: 'red', label: '超时' },
    }
    
    const { colorScheme, label } = config[status] || { colorScheme: 'gray', label: status }
    
    return <Badge colorScheme={colorScheme}>{label}</Badge>
  }

  // 格式化时间
  const formatTime = (timeStr?: string) => {
    if (!timeStr) return '-'
    const date = new Date(timeStr)
    const now = new Date()
    const diff = now.getTime() - date.getTime()
    const seconds = Math.floor(diff / 1000)
    const minutes = Math.floor(seconds / 60)
    const hours = Math.floor(minutes / 60)
    const days = Math.floor(hours / 24)
    
    if (days > 0) return `${days}天前`
    if (hours > 0) return `${hours}小时前`
    if (minutes > 0) return `${minutes}分钟前`
    return `${seconds}秒前`
  }

  // 格式化时间（完整）
  const formatFullTime = (timeStr?: string) => {
    if (!timeStr) return '-'
    const date = new Date(timeStr)
    return date.toLocaleString('zh-CN', {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    })
  }

  // 格式化数字（添加千分位）
  const formatNumber = (num: number) => {
    return num.toLocaleString('zh-CN')
  }

  // 格式化JSON（美化显示）
  const formatJSON = (jsonStr: string) => {
    try {
      const obj = JSON.parse(jsonStr)
      return JSON.stringify(obj, null, 2)
    } catch {
      return jsonStr
    }
  }

  // 计算处理时间
  const calculateDuration = (start?: string, end?: string) => {
    if (!start) return '-'
    const startTime = new Date(start).getTime()
    const endTime = end ? new Date(end).getTime() : Date.now()
    const diff = endTime - startTime
    const seconds = Math.floor(diff / 1000)
    const minutes = Math.floor(seconds / 60)
    
    if (minutes > 0) {
      return `${minutes}分${seconds % 60}秒`
    }
    return `${seconds}秒`
  }

  return (
    <Box>
      <VStack align="stretch" spacing={6}>
        {/* 页头 */}
        <Flex justify="space-between" align="center">
          <HStack>
            <StarIcon boxSize={6} color="blue.500" />
            <Heading size="lg">AI 异步任务管理</Heading>
          </HStack>
          <Button size="sm" onClick={() => { loadTasks(); loadStats(); }}>
            刷新
          </Button>
        </Flex>

        {/* 统计卡片 */}
        {stats && (
          <SimpleGrid columns={{ base: 2, md: 4 }} spacing={4}>
            <Card>
              <CardBody>
                <Stat>
                  <StatLabel>总任务数</StatLabel>
                  <StatNumber>{formatNumber(stats.total_tasks)}</StatNumber>
                  <StatHelpText>所有任务</StatHelpText>
                </Stat>
              </CardBody>
            </Card>
            <Card>
              <CardBody>
                <Stat>
                  <StatLabel>总计 Token</StatLabel>
                  <StatNumber color="blue.500">{formatNumber(stats.total_tokens)}</StatNumber>
                  <StatHelpText>输入: {formatNumber(stats.total_input_tokens)} / 输出: {formatNumber(stats.total_output_tokens)}</StatHelpText>
                </Stat>
              </CardBody>
            </Card>
            <Card>
              <CardBody>
                <Stat>
                  <StatLabel>今日 Token</StatLabel>
                  <StatNumber color="green.500">{formatNumber(stats.today_tokens)}</StatNumber>
                  <StatHelpText>输入: {formatNumber(stats.today_input_tokens)} / 输出: {formatNumber(stats.today_output_tokens)}</StatHelpText>
                </Stat>
              </CardBody>
            </Card>
            <Card>
              <CardBody>
                <Stat>
                  <StatLabel>每日统计</StatLabel>
                  <StatNumber>{stats.daily_stats.length}</StatNumber>
                  <StatHelpText>最近 {stats.daily_stats.length} 天</StatHelpText>
                </Stat>
              </CardBody>
            </Card>
          </SimpleGrid>
        )}

        {/* 筛选区域 */}
        <Card>
          <CardBody>
            <HStack spacing={4} flexWrap="wrap">
              <Box>
                <Text fontSize="sm" mb={2}>状态筛选</Text>
                <Select
                  value={statusFilter}
                  onChange={(e) => setStatusFilter(e.target.value)}
                  placeholder="全部状态"
                  size="sm"
                  maxW="200px"
                >
                  <option value="pending">待处理</option>
                  <option value="running">运行中</option>
                  <option value="completed">已完成</option>
                  <option value="failed">失败</option>
                  <option value="timeout">超时</option>
                </Select>
              </Box>
              <Box>
                <Text fontSize="sm" mb={2}>开始日期</Text>
                <Input
                  type="date"
                  value={startDate}
                  onChange={(e) => setStartDate(e.target.value)}
                  size="sm"
                  maxW="200px"
                />
              </Box>
              <Box>
                <Text fontSize="sm" mb={2}>结束日期</Text>
                <Input
                  type="date"
                  value={endDate}
                  onChange={(e) => setEndDate(e.target.value)}
                  size="sm"
                  maxW="200px"
                />
              </Box>
              <Box pt={8}>
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => {
                    setStatusFilter('')
                    setStartDate('')
                    setEndDate('')
                  }}
                >
                  清除筛选
                </Button>
              </Box>
            </HStack>
          </CardBody>
        </Card>

        {/* 任务列表 */}
        <Card>
          <CardBody>
            {loading ? (
              <Center py={10}>
                <Spinner size="xl" color="blue.500" />
              </Center>
            ) : tasks.length === 0 ? (
              <Center py={10}>
                <Text color="gray.500">暂无任务</Text>
              </Center>
            ) : (
              <Box overflowX="auto">
                <Table variant="simple">
                  <Thead>
                    <Tr>
                      <Th>任务ID</Th>
                      <Th>类型</Th>
                      <Th>状态</Th>
                      <Th isNumeric>输入 Token</Th>
                      <Th isNumeric>输出 Token</Th>
                      <Th>创建时间</Th>
                      <Th>完成时间</Th>
                      <Th>处理时长</Th>
                      <Th>操作</Th>
                    </Tr>
                  </Thead>
                  <Tbody>
                    {tasks.map((task) => (
                      <Tr
                        key={task.id}
                        _hover={{ bg: 'gray.50', cursor: 'pointer' }}
                        onClick={() => handleTaskClick(task)}
                      >
                        <Td>
                          <Text fontSize="sm" fontFamily="mono">
                            {task.id.substring(0, 12)}...
                          </Text>
                        </Td>
                        <Td>
                          <Badge colorScheme="purple" variant="subtle">
                            {task.task_type}
                          </Badge>
                        </Td>
                        <Td>{getStatusBadge(task.status)}</Td>
                        <Td isNumeric>
                          <Text fontSize="sm">{formatNumber(task.input_tokens)}</Text>
                        </Td>
                        <Td isNumeric>
                          <Text fontSize="sm">{formatNumber(task.output_tokens)}</Text>
                        </Td>
                        <Td>
                          <Text fontSize="sm" color="gray.600">
                            {formatTime(task.created_at)}
                          </Text>
                          <Text fontSize="xs" color="gray.400">
                            {formatFullTime(task.created_at)}
                          </Text>
                        </Td>
                        <Td>
                          {task.completed_at ? (
                            <>
                              <Text fontSize="sm" color="gray.600">
                                {formatTime(task.completed_at)}
                              </Text>
                              <Text fontSize="xs" color="gray.400">
                                {formatFullTime(task.completed_at)}
                              </Text>
                            </>
                          ) : (
                            <Text fontSize="sm" color="gray.400">-</Text>
                          )}
                        </Td>
                        <Td>
                          <Text fontSize="sm">
                            {calculateDuration(task.started_at, task.completed_at)}
                          </Text>
                        </Td>
                        <Td>
                          <Button size="xs" variant="ghost" colorScheme="blue">
                            详情
                          </Button>
                        </Td>
                      </Tr>
                    ))}
                  </Tbody>
                </Table>
              </Box>
            )}
          </CardBody>
        </Card>
      </VStack>

      {/* 任务详情模态框 */}
      {selectedTask && (
        <Modal isOpen={isOpen} onClose={onClose} size="xl" scrollBehavior="inside">
          <ModalOverlay />
          <ModalContent>
            <ModalHeader>任务详情</ModalHeader>
            <ModalCloseButton />
            <ModalBody>
              <VStack align="stretch" spacing={4}>
                {/* 基本信息 */}
                <Box>
                  <Heading size="sm" mb={2}>基本信息</Heading>
                  <SimpleGrid columns={2} spacing={4}>
                    <Box>
                      <Text fontSize="sm" color="gray.600">任务ID</Text>
                      <Text fontFamily="mono" fontSize="sm">{selectedTask.id}</Text>
                    </Box>
                    <Box>
                      <Text fontSize="sm" color="gray.600">任务类型</Text>
                      <Badge colorScheme="purple">{selectedTask.task_type}</Badge>
                    </Box>
                    <Box>
                      <Text fontSize="sm" color="gray.600">状态</Text>
                      {getStatusBadge(selectedTask.status)}
                    </Box>
                    <Box>
                      <Text fontSize="sm" color="gray.600">模型</Text>
                      <Text fontSize="sm">{selectedTask.model || '-'}</Text>
                    </Box>
                    <Box>
                      <Text fontSize="sm" color="gray.600">创建时间</Text>
                      <Text fontSize="sm">{formatFullTime(selectedTask.created_at)}</Text>
                    </Box>
                    {selectedTask.started_at && (
                      <Box>
                        <Text fontSize="sm" color="gray.600">开始时间</Text>
                        <Text fontSize="sm">{formatFullTime(selectedTask.started_at)}</Text>
                      </Box>
                    )}
                    {selectedTask.completed_at && (
                      <Box>
                        <Text fontSize="sm" color="gray.600">完成时间</Text>
                        <Text fontSize="sm">{formatFullTime(selectedTask.completed_at)}</Text>
                      </Box>
                    )}
                    <Box>
                      <Text fontSize="sm" color="gray.600">处理时长</Text>
                      <Text fontSize="sm">
                        {calculateDuration(selectedTask.started_at, selectedTask.completed_at)}
                      </Text>
                    </Box>
                  </SimpleGrid>
                </Box>

                <Divider />

                {/* Token使用量 */}
                <Box>
                  <Heading size="sm" mb={2}>Token 使用量</Heading>
                  <SimpleGrid columns={3} spacing={4}>
                    <Box>
                      <Text fontSize="sm" color="gray.600">输入 Token</Text>
                      <Text fontSize="lg" fontWeight="bold" color="blue.500">
                        {formatNumber(selectedTask.input_tokens)}
                      </Text>
                    </Box>
                    <Box>
                      <Text fontSize="sm" color="gray.600">输出 Token</Text>
                      <Text fontSize="lg" fontWeight="bold" color="green.500">
                        {formatNumber(selectedTask.output_tokens)}
                      </Text>
                    </Box>
                    <Box>
                      <Text fontSize="sm" color="gray.600">总计 Token</Text>
                      <Text fontSize="lg" fontWeight="bold">
                        {formatNumber(selectedTask.input_tokens + selectedTask.output_tokens)}
                      </Text>
                    </Box>
                  </SimpleGrid>
                  {selectedTask.processing_time_ms > 0 && (
                    <Box mt={2}>
                      <Text fontSize="sm" color="gray.600">处理时间</Text>
                      <Text fontSize="sm">{selectedTask.processing_time_ms} ms</Text>
                    </Box>
                  )}
                </Box>

                <Divider />

                {/* 重试信息 */}
                {(selectedTask.retry_count > 0 || selectedTask.max_retries > 0) && (
                  <Box>
                    <Heading size="sm" mb={2}>重试信息</Heading>
                    <HStack spacing={4}>
                      <Text fontSize="sm">重试次数: {selectedTask.retry_count} / {selectedTask.max_retries}</Text>
                      <Text fontSize="sm">超时设置: {selectedTask.timeout_seconds} 秒</Text>
                    </HStack>
                  </Box>
                )}

                {/* 错误信息 */}
                {selectedTask.error_message && (
                  <>
                    <Divider />
                    <Box>
                      <Heading size="sm" mb={2} color="red.500">错误信息</Heading>
                      <Code p={3} borderRadius="md" display="block" whiteSpace="pre-wrap" colorScheme="red">
                        {selectedTask.error_message}
                      </Code>
                    </Box>
                  </>
                )}

                {/* 请求数据 */}
                {selectedTask.request_data && (
                  <>
                    <Divider />
                    <Box>
                      <Heading size="sm" mb={2}>请求数据</Heading>
                      <Code p={3} borderRadius="md" display="block" whiteSpace="pre-wrap" fontSize="xs" maxH="300px" overflowY="auto">
                        {formatJSON(selectedTask.request_data)}
                      </Code>
                    </Box>
                  </>
                )}

                {/* AI输入 */}
                {selectedTask.ai_input && (
                  <>
                    <Divider />
                    <Box>
                      <Heading size="sm" mb={2}>AI 输入</Heading>
                      <Code p={3} borderRadius="md" display="block" whiteSpace="pre-wrap" fontSize="xs" maxH="300px" overflowY="auto">
                        {selectedTask.ai_input}
                      </Code>
                    </Box>
                  </>
                )}

                {/* AI输出 */}
                {selectedTask.ai_output && (
                  <>
                    <Divider />
                    <Box>
                      <Heading size="sm" mb={2}>AI 输出</Heading>
                      <Code p={3} borderRadius="md" display="block" whiteSpace="pre-wrap" fontSize="xs" maxH="300px" overflowY="auto">
                        {selectedTask.ai_output}
                      </Code>
                    </Box>
                  </>
                )}

                {/* 返回结果 */}
                {selectedTask.result && (
                  <>
                    <Divider />
                    <Box>
                      <Heading size="sm" mb={2}>返回结果</Heading>
                      <Code p={3} borderRadius="md" display="block" whiteSpace="pre-wrap" fontSize="xs" maxH="300px" overflowY="auto">
                        {formatJSON(selectedTask.result)}
                      </Code>
                    </Box>
                  </>
                )}
              </VStack>
            </ModalBody>
            <ModalFooter>
              <Button onClick={onClose}>关闭</Button>
            </ModalFooter>
          </ModalContent>
        </Modal>
      )}
    </Box>
  )
}

export default AITaskManager
