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

  // 加載事件數據
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
      console.error('加載事件失败:', error)
    } finally {
      setLoading(false)
    }
  }

  // 加載统计數據
  const loadStats = async () => {
    try {
      const data = await getEventStats()
      setStats(data)
    } catch (error) {
      console.error('加載统计失败:', error)
    }
  }

  useEffect(() => {
    loadEvents(activeFilter)
    loadStats()
    
    // 定時刷新
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

  // 獲取严重程度徽章
  const getSeverityBadge = (severity: string) => {
    const config = {
      critical: { colorScheme: 'red', icon: WarningIcon, label: t('eventCenter.critical') },
      warning: { colorScheme: 'orange', icon: InfoIcon, label: t('eventCenter.warning') },
      info: { colorScheme: 'blue', icon: CheckCircleIcon, label: t('eventCenter.info') },
    }
    
    const { colorScheme, icon, label } = config[severity as keyof typeof config] || config.info
    
    return (
      <Badge colorScheme={colorScheme} display="flex" alignItems="center" gap={1}>
        <Icon as={icon} boxSize={3} />
        {label}
      </Badge>
    )
  }

  // 獲取来源標签
  const getSourceLabel = (source: string) => {
    const labels: Record<string, string> = {
      exchange: t('eventCenter.exchange'),
      network: t('eventCenter.network'),
      system: t('eventCenter.system'),
      strategy: t('eventCenter.strategy'),
      risk: t('eventCenter.risk'),
      api: 'API',
    }
    return labels[source] || source
  }

  // 格式化時间
  const formatTime = (timeStr: string) => {
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

  return (
    <Box>
      <VStack align="stretch" spacing={6}>
        {/* 页头 */}
        <Flex justify="space-between" align="center">
          <HStack>
            <Icon as={BellIcon} boxSize={6} color="blue.500" />
            <Heading size="lg">{t('eventCenter.title')}</Heading>
          </HStack>
          <Button size="sm" onClick={() => { loadEvents(activeFilter); loadStats(); }}>
            {t('eventCenter.refresh')}
          </Button>
        </Flex>

        {/* 统计卡片 */}
        {stats && (
          <SimpleGrid columns={{ base: 2, md: 4 }} spacing={4}>
            <Card>
              <CardBody>
                <Stat>
                  <StatLabel>{t('eventCenter.totalEvents')}</StatLabel>
                  <StatNumber>{stats.total_count}</StatNumber>
                  <StatHelpText>{t('eventCenter.last24h')}: {stats.last_24_hours_count}</StatHelpText>
                </Stat>
              </CardBody>
            </Card>
            <Card>
              <CardBody>
                <Stat>
                  <StatLabel>{t('eventCenter.criticalEvents')}</StatLabel>
                  <StatNumber color="red.500">{stats.critical_count}</StatNumber>
                </Stat>
              </CardBody>
            </Card>
            <Card>
              <CardBody>
                <Stat>
                  <StatLabel>{t('eventCenter.warningEvents')}</StatLabel>
                  <StatNumber color="orange.500">{stats.warning_count}</StatNumber>
                </Stat>
              </CardBody>
            </Card>
            <Card>
              <CardBody>
                <Stat>
                  <StatLabel>{t('eventCenter.infoEvents')}</StatLabel>
                  <StatNumber color="blue.500">{stats.info_count}</StatNumber>
                </Stat>
              </CardBody>
            </Card>
          </SimpleGrid>
        )}

        {/* 筛选標签 */}
        <Tabs
          variant="soft-rounded"
          colorScheme="blue"
          onChange={(index) => {
            const filters = ['all', 'critical', 'warning', 'info', 'exchange', 'network', 'system', 'api', 'risk']
            setActiveFilter(filters[index])
          }}
        >
          <TabList flexWrap="wrap">
            <Tab>{t('eventCenter.all')}</Tab>
            <Tab>{t('eventCenter.critical')}</Tab>
            <Tab>{t('eventCenter.warning')}</Tab>
            <Tab>{t('eventCenter.info')}</Tab>
            <Tab>{t('eventCenter.exchange')}</Tab>
            <Tab>{t('eventCenter.network')}</Tab>
            <Tab>{t('eventCenter.system')}</Tab>
            <Tab>API</Tab>
            <Tab>{t('eventCenter.risk')}</Tab>
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
                <Text color="gray.500">{t('eventCenter.noEvents')}</Text>
              </Center>
            ) : (
              <Box overflowX="auto">
                <Table variant="simple">
                  <Thead>
                    <Tr>
                      <Th>{t('eventCenter.time')}</Th>
                      <Th>{t('eventCenter.severity')}</Th>
                      <Th>{t('eventCenter.source')}</Th>
                      <Th>{t('eventCenter.titleCol')}</Th>
                      <Th>{t('eventCenter.message')}</Th>
                      <Th>{t('eventCenter.tradingPair')}</Th>
                      <Th>{t('eventCenter.action')}</Th>
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
                            {t('eventCenter.details')}
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

