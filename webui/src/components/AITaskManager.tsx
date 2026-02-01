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

  // 加載任務數據
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
        // 設置到當天的23:59:59
        const end = new Date(endDate)
        end.setHours(23, 59, 59, 999)
        filter.end_time = end.toISOString()
      }
      
      const data = await getAITasks(filter)
      setTasks(data.tasks || [])
    } catch (error) {
      console.error('加載任務失败:', error)
    } finally {
      setLoading(false)
    }
  }

  // 加載统计數據
  const loadStats = async () => {
    try {
      const data = await getAITaskStats()
      setStats(data)
    } catch (error) {
      console.error('加載统计失败:', error)
    }
  }

  useEffect(() => {
    loadTasks()
    loadStats()
    
    // 定時刷新
    const interval = setInterval(() => {
      loadTasks()
      loadStats()
    }, 30000) // 30秒刷新一次
    
    return () => clearInterval(interval)
  }, [statusFilter, startDate, endDate])

  // 打开任務详情
  const handleTaskClick = (task: AITask) => {
    setSelectedTask(task)
    onOpen()
  }

  // 獲取状態徽章
  const getStatusBadge = (status: string) => {
    const config: Record<string, { colorScheme: string; label: string }> = {
      pending: { colorScheme: 'yellow', label: t('aiTasks.status.pending') },
      running: { colorScheme: 'blue', label: t('aiTasks.status.running') },
      completed: { colorScheme: 'green', label: t('aiTasks.status.completed') },
      failed: { colorScheme: 'red', label: t('aiTasks.status.failed') },
      timeout: { colorScheme: 'red', label: t('aiTasks.status.timeout') },
    }
    
    const { colorScheme, label } = config[status] || { colorScheme: 'gray', label: status }
    
    return <Badge colorScheme={colorScheme}>{label}</Badge>
  }

  const formatTime = (timeStr?: string) => {
    if (!timeStr) return '-'
    const date = new Date(timeStr)
    const now = new Date()
    const diff = now.getTime() - date.getTime()
    const seconds = Math.floor(diff / 1000)
    const minutes = Math.floor(seconds / 60)
    const hours = Math.floor(minutes / 60)
    const days = Math.floor(hours / 24)

    if (days > 0) return t('eventCenter.daysAgo', { count: days })
    if (hours > 0) return t('eventCenter.hoursAgo', { count: hours })
    if (minutes > 0) return t('eventCenter.minutesAgo', { count: minutes })
    return t('eventCenter.secondsAgo', { count: seconds })
  }

  // 格式化時间（完整）
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

  // 格式化數字（添加千分位）
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

  const calculateDuration = (start?: string, end?: string) => {
    if (!start) return '-'
    const startTime = new Date(start).getTime()
    const endTime = end ? new Date(end).getTime() : Date.now()
    const diff = endTime - startTime
    const seconds = Math.floor(diff / 1000)
    const minutes = Math.floor(seconds / 60)

    if (minutes > 0) {
      return t('aiTasks.durationFormat', { minutes, seconds: seconds % 60 })
    }
    return t('aiTasks.durationSeconds', { seconds })
  }

  return (
    <Box>
      <VStack align="stretch" spacing={6}>
        {/* 页头 */}
        <Flex justify="space-between" align="center">
          <HStack>
            <StarIcon boxSize={6} color="blue.500" />
            <Heading size="lg">{t('aiTasks.title')}</Heading>
          </HStack>
          <Button size="sm" onClick={() => { loadTasks(); loadStats(); }}>
            {t('aiTasks.refresh')}
          </Button>
        </Flex>

        {/* 统计卡片 */}
        {stats && (
          <SimpleGrid columns={{ base: 2, md: 4 }} spacing={4}>
            <Card>
              <CardBody>
                <Stat>
                  <StatLabel>{t('aiTasks.totalTasks')}</StatLabel>
                  <StatNumber>{formatNumber(stats.total_tasks)}</StatNumber>
                  <StatHelpText>{t('aiTasks.totalTasksDesc')}</StatHelpText>
                </Stat>
              </CardBody>
            </Card>
            <Card>
              <CardBody>
                <Stat>
                  <StatLabel>{t('aiTasks.totalTokens')}</StatLabel>
                  <StatNumber color="blue.500">{formatNumber(stats.total_tokens)}</StatNumber>
                  <StatHelpText>{t('aiTasks.totalTokensDesc', { input: formatNumber(stats.total_input_tokens), output: formatNumber(stats.total_output_tokens) })}</StatHelpText>
                </Stat>
              </CardBody>
            </Card>
            <Card>
              <CardBody>
                <Stat>
                  <StatLabel>{t('aiTasks.todayTokens')}</StatLabel>
                  <StatNumber color="green.500">{formatNumber(stats.today_tokens)}</StatNumber>
                  <StatHelpText>{t('aiTasks.todayTokensDesc', { input: formatNumber(stats.today_input_tokens), output: formatNumber(stats.today_output_tokens) })}</StatHelpText>
                </Stat>
              </CardBody>
            </Card>
            <Card>
              <CardBody>
                <Stat>
                  <StatLabel>{t('aiTasks.dailyStats')}</StatLabel>
                  <StatNumber>{stats.daily_stats.length}</StatNumber>
                  <StatHelpText>{t('aiTasks.dailyStatsDesc', { days: stats.daily_stats.length })}</StatHelpText>
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
                <Text fontSize="sm" mb={2}>{t('aiTasks.statusFilter')}</Text>
                <Select
                  value={statusFilter}
                  onChange={(e) => setStatusFilter(e.target.value)}
                  placeholder={t('aiTasks.allStatus')}
                  size="sm"
                  maxW="200px"
                >
                  <option value="pending">{t('aiTasks.status.pending')}</option>
                  <option value="running">{t('aiTasks.status.running')}</option>
                  <option value="completed">{t('aiTasks.status.completed')}</option>
                  <option value="failed">{t('aiTasks.status.failed')}</option>
                  <option value="timeout">{t('aiTasks.status.timeout')}</option>
                </Select>
              </Box>
              <Box>
                <Text fontSize="sm" mb={2}>{t('aiTasks.startDate')}</Text>
                <Input
                  type="date"
                  value={startDate}
                  onChange={(e) => setStartDate(e.target.value)}
                  size="sm"
                  maxW="200px"
                />
              </Box>
              <Box>
                <Text fontSize="sm" mb={2}>{t('aiTasks.endDate')}</Text>
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
                  {t('aiTasks.clearFilters')}
                </Button>
              </Box>
            </HStack>
          </CardBody>
        </Card>

        {/* 任務列表 */}
        <Card>
          <CardBody>
            {loading ? (
              <Center py={10}>
                <Spinner size="xl" color="blue.500" />
              </Center>
            ) : tasks.length === 0 ? (
              <Center py={10}>
                <Text color="gray.500">{t('aiTasks.noTasks')}</Text>
              </Center>
            ) : (
              <Box overflowX="auto">
                <Table variant="simple">
                  <Thead>
                    <Tr>
                      <Th>{t('aiTasks.taskId')}</Th>
                      <Th>{t('aiTasks.type')}</Th>
                      <Th>{t('aiTasks.statusCol')}</Th>
                      <Th isNumeric>{t('aiTasks.inputTokens')}</Th>
                      <Th isNumeric>{t('aiTasks.outputTokens')}</Th>
                      <Th>{t('aiTasks.createdAt')}</Th>
                      <Th>{t('aiTasks.completedAt')}</Th>
                      <Th>{t('aiTasks.duration')}</Th>
                      <Th>{t('aiTasks.action')}</Th>
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
                            {t('aiTasks.details')}
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

      {/* 任務详情模態框 */}
      {selectedTask && (
        <Modal isOpen={isOpen} onClose={onClose} size="xl" scrollBehavior="inside">
          <ModalOverlay />
          <ModalContent>
            <ModalHeader>{t('aiTasks.taskDetails')}</ModalHeader>
            <ModalCloseButton />
            <ModalBody>
              <VStack align="stretch" spacing={4}>
                {/* 基本信息 */}
                <Box>
                  <Heading size="sm" mb={2}>{t('aiTasks.basicInfo')}</Heading>
                  <SimpleGrid columns={2} spacing={4}>
                    <Box>
                      <Text fontSize="sm" color="gray.600">{t('aiTasks.taskId')}</Text>
                      <Text fontFamily="mono" fontSize="sm">{selectedTask.id}</Text>
                    </Box>
                    <Box>
                      <Text fontSize="sm" color="gray.600">{t('aiTasks.taskType')}</Text>
                      <Badge colorScheme="purple">{selectedTask.task_type}</Badge>
                    </Box>
                    <Box>
                      <Text fontSize="sm" color="gray.600">{t('aiTasks.statusCol')}</Text>
                      {getStatusBadge(selectedTask.status)}
                    </Box>
                    <Box>
                      <Text fontSize="sm" color="gray.600">{t('aiTasks.model')}</Text>
                      <Text fontSize="sm">{selectedTask.model || '-'}</Text>
                    </Box>
                    <Box>
                      <Text fontSize="sm" color="gray.600">{t('aiTasks.createdAt')}</Text>
                      <Text fontSize="sm">{formatFullTime(selectedTask.created_at)}</Text>
                    </Box>
                    {selectedTask.started_at && (
                      <Box>
                        <Text fontSize="sm" color="gray.600">{t('aiTasks.startedAt')}</Text>
                        <Text fontSize="sm">{formatFullTime(selectedTask.started_at)}</Text>
                      </Box>
                    )}
                    {selectedTask.completed_at && (
                      <Box>
                        <Text fontSize="sm" color="gray.600">{t('aiTasks.completedAt')}</Text>
                        <Text fontSize="sm">{formatFullTime(selectedTask.completed_at)}</Text>
                      </Box>
                    )}
                    <Box>
                      <Text fontSize="sm" color="gray.600">{t('aiTasks.duration')}</Text>
                      <Text fontSize="sm">
                        {calculateDuration(selectedTask.started_at, selectedTask.completed_at)}
                      </Text>
                    </Box>
                  </SimpleGrid>
                </Box>

                <Divider />

                {/* Token使用量 */}
                <Box>
                  <Heading size="sm" mb={2}>{t('aiTasks.tokenUsage')}</Heading>
                  <SimpleGrid columns={3} spacing={4}>
                    <Box>
                      <Text fontSize="sm" color="gray.600">{t('aiTasks.inputTokens')}</Text>
                      <Text fontSize="lg" fontWeight="bold" color="blue.500">
                        {formatNumber(selectedTask.input_tokens)}
                      </Text>
                    </Box>
                    <Box>
                      <Text fontSize="sm" color="gray.600">{t('aiTasks.outputTokens')}</Text>
                      <Text fontSize="lg" fontWeight="bold" color="green.500">
                        {formatNumber(selectedTask.output_tokens)}
                      </Text>
                    </Box>
                    <Box>
                      <Text fontSize="sm" color="gray.600">{t('aiTasks.totalTokens')}</Text>
                      <Text fontSize="lg" fontWeight="bold">
                        {formatNumber(selectedTask.input_tokens + selectedTask.output_tokens)}
                      </Text>
                    </Box>
                  </SimpleGrid>
                  {selectedTask.processing_time_ms > 0 && (
                    <Box mt={2}>
                      <Text fontSize="sm" color="gray.600">{t('aiTasks.processingTime')}</Text>
                      <Text fontSize="sm">{selectedTask.processing_time_ms} ms</Text>
                    </Box>
                  )}
                </Box>

                <Divider />

                {/* 重試信息 */}
                {(selectedTask.retry_count > 0 || selectedTask.max_retries > 0) && (
                  <Box>
                    <Heading size="sm" mb={2}>{t('aiTasks.retryInfo')}</Heading>
                    <HStack spacing={4}>
                      <Text fontSize="sm">{t('aiTasks.retryCount', { current: selectedTask.retry_count, max: selectedTask.max_retries })}</Text>
                      <Text fontSize="sm">{t('aiTasks.timeoutSetting', { seconds: selectedTask.timeout_seconds })}</Text>
                    </HStack>
                  </Box>
                )}

                {/* 錯误信息 */}
                {selectedTask.error_message && (
                  <>
                    <Divider />
                    <Box>
                      <Heading size="sm" mb={2} color="red.500">{t('aiTasks.errorInfoTitle')}</Heading>
                      <Code p={3} borderRadius="md" display="block" whiteSpace="pre-wrap" colorScheme="red">
                        {selectedTask.error_message}
                      </Code>
                    </Box>
                  </>
                )}

                {/* 请求數據 */}
                {selectedTask.request_data && (
                  <>
                    <Divider />
                    <Box>
                      <Heading size="sm" mb={2}>{t('aiTasks.requestData')}</Heading>
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
                      <Heading size="sm" mb={2}>{t('aiTasks.aiInput')}</Heading>
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
                      <Heading size="sm" mb={2}>{t('aiTasks.aiOutput')}</Heading>
                      <Code p={3} borderRadius="md" display="block" whiteSpace="pre-wrap" fontSize="xs" maxH="300px" overflowY="auto">
                        {selectedTask.ai_output}
                      </Code>
                    </Box>
                  </>
                )}

                {/* 返回結果 */}
                {selectedTask.result && (
                  <>
                    <Divider />
                    <Box>
                      <Heading size="sm" mb={2}>{t('aiTasks.result')}</Heading>
                      <Code p={3} borderRadius="md" display="block" whiteSpace="pre-wrap" fontSize="xs" maxH="300px" overflowY="auto">
                        {formatJSON(selectedTask.result)}
                      </Code>
                    </Box>
                  </>
                )}
              </VStack>
            </ModalBody>
            <ModalFooter>
              <Button onClick={onClose}>{t('aiTasks.close')}</Button>
            </ModalFooter>
          </ModalContent>
        </Modal>
      )}
    </Box>
  )
}

export default AITaskManager
