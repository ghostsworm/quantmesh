import React, { useState, useEffect } from 'react'
import {
  Box,
  VStack,
  HStack,
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
  Tabs,
  TabList,
  TabPanels,
  Tab,
  TabPanel,
  Icon,
  Flex,
  Card,
  CardBody,
  SimpleGrid,
  Stat,
  StatLabel,
  StatNumber,
  StatHelpText,
} from '@chakra-ui/react'
import { WarningIcon, InfoIcon, CheckCircleIcon, BellIcon } from '@chakra-ui/icons'
import { getEvents, getEventStats, EventRecord, EventStats } from '../services/api'
import EventDetailModal from './EventDetailModal'
import { useTranslation } from 'react-i18next'

const EventCenter: React.FC = () => {
  const { t } = useTranslation()
  const [events, setEvents] = useState<EventRecord[]>([])
  const [stats, setStats] = useState<EventStats | null>(null)
  const [loading, setLoading] = useState(true)
  const [selectedEvent, setSelectedEvent] = useState<EventRecord | null>(null)
  const [activeFilter, setActiveFilter] = useState<string>('all')
  const { isOpen, onOpen, onClose } = useDisclosure()

  // 加载事件数据
  const loadEvents = async (filter?: string) => {
    try {
      setLoading(true)
      const filterParams: any = { limit: 100 }
      
      if (filter && filter !== 'all') {
        if (filter === 'critical' || filter === 'warning' || filter === 'info') {
          filterParams.severity = filter
        } else {
          filterParams.source = filter
        }
      }
      
      const data = await getEvents(filterParams)
      setEvents(data.events || [])
    } catch (error) {
      console.error('加载事件失败:', error)
    } finally {
      setLoading(false)
    }
  }

  // 加载统计数据
  const loadStats = async () => {
    try {
      const data = await getEventStats()
      setStats(data)
    } catch (error) {
      console.error('加载统计失败:', error)
    }
  }

  useEffect(() => {
    loadEvents(activeFilter)
    loadStats()
    
    // 定时刷新
    const interval = setInterval(() => {
      loadEvents(activeFilter)
      loadStats()
    }, 30000) // 30秒刷新一次
    
    return () => clearInterval(interval)
  }, [activeFilter])

  // 打开事件详情
  const handleEventClick = (event: EventRecord) => {
    setSelectedEvent(event)
    onOpen()
  }

  // 获取严重程度徽章
  const getSeverityBadge = (severity: string) => {
    const config = {
      critical: { colorScheme: 'red', icon: WarningIcon, label: '严重' },
      warning: { colorScheme: 'orange', icon: InfoIcon, label: '警告' },
      info: { colorScheme: 'blue', icon: CheckCircleIcon, label: '信息' },
    }
    
    const { colorScheme, icon, label } = config[severity as keyof typeof config] || config.info
    
    return (
      <Badge colorScheme={colorScheme} display="flex" alignItems="center" gap={1}>
        <Icon as={icon} boxSize={3} />
        {label}
      </Badge>
    )
  }

  // 获取来源标签
  const getSourceLabel = (source: string) => {
    const labels: Record<string, string> = {
      exchange: '交易所',
      network: '网络',
      system: '系统',
      strategy: '策略',
      risk: '风控',
      api: 'API',
    }
    return labels[source] || source
  }

  // 格式化时间
  const formatTime = (timeStr: string) => {
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

  return (
    <Box>
      <VStack align="stretch" spacing={6}>
        {/* 页头 */}
        <Flex justify="space-between" align="center">
          <HStack>
            <Icon as={BellIcon} boxSize={6} color="blue.500" />
            <Heading size="lg">事件中心</Heading>
          </HStack>
          <Button size="sm" onClick={() => { loadEvents(activeFilter); loadStats(); }}>
            刷新
          </Button>
        </Flex>

        {/* 统计卡片 */}
        {stats && (
          <SimpleGrid columns={{ base: 2, md: 4 }} spacing={4}>
            <Card>
              <CardBody>
                <Stat>
                  <StatLabel>总事件数</StatLabel>
                  <StatNumber>{stats.total_count}</StatNumber>
                  <StatHelpText>24小时: {stats.last_24_hours_count}</StatHelpText>
                </Stat>
              </CardBody>
            </Card>
            <Card>
              <CardBody>
                <Stat>
                  <StatLabel>严重事件</StatLabel>
                  <StatNumber color="red.500">{stats.critical_count}</StatNumber>
                </Stat>
              </CardBody>
            </Card>
            <Card>
              <CardBody>
                <Stat>
                  <StatLabel>警告事件</StatLabel>
                  <StatNumber color="orange.500">{stats.warning_count}</StatNumber>
                </Stat>
              </CardBody>
            </Card>
            <Card>
              <CardBody>
                <Stat>
                  <StatLabel>信息事件</StatLabel>
                  <StatNumber color="blue.500">{stats.info_count}</StatNumber>
                </Stat>
              </CardBody>
            </Card>
          </SimpleGrid>
        )}

        {/* 筛选标签 */}
        <Tabs
          variant="soft-rounded"
          colorScheme="blue"
          onChange={(index) => {
            const filters = ['all', 'critical', 'warning', 'info', 'exchange', 'network', 'system', 'api', 'risk']
            setActiveFilter(filters[index])
          }}
        >
          <TabList flexWrap="wrap">
            <Tab>全部</Tab>
            <Tab>严重</Tab>
            <Tab>警告</Tab>
            <Tab>信息</Tab>
            <Tab>交易所</Tab>
            <Tab>网络</Tab>
            <Tab>系统</Tab>
            <Tab>API</Tab>
            <Tab>风控</Tab>
          </TabList>
        </Tabs>

        {/* 事件列表 */}
        <Card>
          <CardBody>
            {loading ? (
              <Center py={10}>
                <Spinner size="xl" color="blue.500" />
              </Center>
            ) : events.length === 0 ? (
              <Center py={10}>
                <Text color="gray.500">暂无事件</Text>
              </Center>
            ) : (
              <Box overflowX="auto">
                <Table variant="simple">
                  <Thead>
                    <Tr>
                      <Th>时间</Th>
                      <Th>严重程度</Th>
                      <Th>来源</Th>
                      <Th>标题</Th>
                      <Th>消息</Th>
                      <Th>交易对</Th>
                      <Th>操作</Th>
                    </Tr>
                  </Thead>
                  <Tbody>
                    {events.map((event) => (
                      <Tr
                        key={event.id}
                        _hover={{ bg: 'gray.50', cursor: 'pointer' }}
                        onClick={() => handleEventClick(event)}
                      >
                        <Td>
                          <Text fontSize="sm" color="gray.600">
                            {formatTime(event.created_at)}
                          </Text>
                        </Td>
                        <Td>{getSeverityBadge(event.severity)}</Td>
                        <Td>
                          <Badge colorScheme="gray">{getSourceLabel(event.source)}</Badge>
                        </Td>
                        <Td>
                          <Text fontWeight="medium">{event.title}</Text>
                        </Td>
                        <Td>
                          <Text noOfLines={2} fontSize="sm" color="gray.600">
                            {event.message}
                          </Text>
                        </Td>
                        <Td>
                          {event.symbol && (
                            <Badge colorScheme="purple" variant="subtle">
                              {event.exchange}/{event.symbol}
                            </Badge>
                          )}
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

      {/* 事件详情弹窗 */}
      {selectedEvent && (
        <EventDetailModal
          event={selectedEvent}
          isOpen={isOpen}
          onClose={onClose}
        />
      )}
    </Box>
  )
}

export default EventCenter

