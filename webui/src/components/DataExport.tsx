import React, { useState } from 'react'
import {
  Box,
  VStack,
  HStack,
  Heading,
  Text,
  Button,
  SimpleGrid,
  Card,
  CardHeader,
  CardBody,
  FormControl,
  FormLabel,
  Select,
  Input,
  Divider,
  Badge,
  useToast,
  Icon,
  Flex,
  Spinner,
} from '@chakra-ui/react'
import {
  DownloadIcon,
  SettingsIcon,
  TimeIcon,
  InfoIcon,
  CheckCircleIcon,
  WarningIcon,
  AttachmentIcon,
} from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import {
  exportConfig,
  exportTrades,
  exportOrders,
  exportPositions,
  exportStatistics,
  exportReconciliation,
  exportRiskChecks,
  exportSystemMetrics,
  exportLogs,
  exportAuditLogs,
  exportAll,
  ExportParams,
} from '../services/export'

interface ExportItem {
  id: string
  titleKey: string
  descKey: string
  icon: React.ElementType
  color: string
  exportFn: (params: ExportParams) => Promise<void>
  supportsFormat: boolean
  supportsTimeRange: boolean
}

const DataExport: React.FC = () => {
  const { t } = useTranslation()
  const toast = useToast()
  
  // 導出格式
  const [format, setFormat] = useState<'json' | 'csv'>('json')
  // 時間範圍
  const [startTime, setStartTime] = useState<string>('')
  const [endTime, setEndTime] = useState<string>('')
  // 載入狀態
  const [loadingItems, setLoadingItems] = useState<Set<string>>(new Set())

  // 導出項目配置
  const exportItems: ExportItem[] = [
    {
      id: 'config',
      titleKey: 'dataExport.items.config.title',
      descKey: 'dataExport.items.config.desc',
      icon: SettingsIcon,
      color: 'blue',
      exportFn: async () => exportConfig(),
      supportsFormat: false,
      supportsTimeRange: false,
    },
    {
      id: 'trades',
      titleKey: 'dataExport.items.trades.title',
      descKey: 'dataExport.items.trades.desc',
      icon: CheckCircleIcon,
      color: 'green',
      exportFn: exportTrades,
      supportsFormat: true,
      supportsTimeRange: true,
    },
    {
      id: 'orders',
      titleKey: 'dataExport.items.orders.title',
      descKey: 'dataExport.items.orders.desc',
      icon: TimeIcon,
      color: 'purple',
      exportFn: exportOrders,
      supportsFormat: true,
      supportsTimeRange: true,
    },
    {
      id: 'positions',
      titleKey: 'dataExport.items.positions.title',
      descKey: 'dataExport.items.positions.desc',
      icon: InfoIcon,
      color: 'orange',
      exportFn: exportPositions,
      supportsFormat: true,
      supportsTimeRange: true,
    },
    {
      id: 'statistics',
      titleKey: 'dataExport.items.statistics.title',
      descKey: 'dataExport.items.statistics.desc',
      icon: InfoIcon,
      color: 'teal',
      exportFn: exportStatistics,
      supportsFormat: true,
      supportsTimeRange: true,
    },
    {
      id: 'reconciliation',
      titleKey: 'dataExport.items.reconciliation.title',
      descKey: 'dataExport.items.reconciliation.desc',
      icon: CheckCircleIcon,
      color: 'cyan',
      exportFn: exportReconciliation,
      supportsFormat: true,
      supportsTimeRange: true,
    },
    {
      id: 'riskChecks',
      titleKey: 'dataExport.items.riskChecks.title',
      descKey: 'dataExport.items.riskChecks.desc',
      icon: WarningIcon,
      color: 'red',
      exportFn: exportRiskChecks,
      supportsFormat: true,
      supportsTimeRange: true,
    },
    {
      id: 'systemMetrics',
      titleKey: 'dataExport.items.systemMetrics.title',
      descKey: 'dataExport.items.systemMetrics.desc',
      icon: SettingsIcon,
      color: 'gray',
      exportFn: exportSystemMetrics,
      supportsFormat: true,
      supportsTimeRange: true,
    },
    {
      id: 'logs',
      titleKey: 'dataExport.items.logs.title',
      descKey: 'dataExport.items.logs.desc',
      icon: TimeIcon,
      color: 'yellow',
      exportFn: exportLogs,
      supportsFormat: true,
      supportsTimeRange: true,
    },
    {
      id: 'auditLogs',
      titleKey: 'dataExport.items.auditLogs.title',
      descKey: 'dataExport.items.auditLogs.desc',
      icon: AttachmentIcon,
      color: 'pink',
      exportFn: exportAuditLogs,
      supportsFormat: false,
      supportsTimeRange: true,
    },
  ]

  // 處理單項導出
  const handleExport = async (item: ExportItem) => {
    setLoadingItems(prev => new Set(prev).add(item.id))
    
    try {
      const params: ExportParams = {}
      if (item.supportsFormat) {
        params.format = format
      }
      if (item.supportsTimeRange) {
        if (startTime) params.start_time = new Date(startTime).toISOString()
        if (endTime) params.end_time = new Date(endTime).toISOString()
      }
      
      await item.exportFn(params)
      
      toast({
        title: t('dataExport.exportSuccess'),
        description: t(item.titleKey),
        status: 'success',
        duration: 3000,
        isClosable: true,
      })
    } catch (error) {
      console.error('Export error:', error)
      toast({
        title: t('dataExport.exportError'),
        description: error instanceof Error ? error.message : t('common.unknownError'),
        status: 'error',
        duration: 5000,
        isClosable: true,
      })
    } finally {
      setLoadingItems(prev => {
        const next = new Set(prev)
        next.delete(item.id)
        return next
      })
    }
  }

  // 處理全量導出
  const handleExportAll = async () => {
    setLoadingItems(prev => new Set(prev).add('all'))
    
    try {
      const params: ExportParams = {}
      if (startTime) params.start_time = new Date(startTime).toISOString()
      if (endTime) params.end_time = new Date(endTime).toISOString()
      
      await exportAll(params)
      
      toast({
        title: t('dataExport.exportSuccess'),
        description: t('dataExport.allDataExported'),
        status: 'success',
        duration: 3000,
        isClosable: true,
      })
    } catch (error) {
      console.error('Export all error:', error)
      toast({
        title: t('dataExport.exportError'),
        description: error instanceof Error ? error.message : t('common.unknownError'),
        status: 'error',
        duration: 5000,
        isClosable: true,
      })
    } finally {
      setLoadingItems(prev => {
        const next = new Set(prev)
        next.delete('all')
        return next
      })
    }
  }

  return (
    <Box>
      <VStack spacing={6} align="stretch">
        {/* 頁面標題 */}
        <Box>
          <Heading size="lg" mb={2}>
            {t('dataExport.title')}
          </Heading>
          <Text color="gray.600">
            {t('dataExport.description')}
          </Text>
        </Box>

        {/* 全局設置卡片 */}
        <Card variant="outline">
          <CardHeader pb={2}>
            <Heading size="sm">{t('dataExport.settings')}</Heading>
          </CardHeader>
          <CardBody>
            <SimpleGrid columns={{ base: 1, md: 3 }} spacing={4}>
              <FormControl>
                <FormLabel fontSize="sm">{t('dataExport.format')}</FormLabel>
                <Select
                  value={format}
                  onChange={(e) => setFormat(e.target.value as 'json' | 'csv')}
                  size="sm"
                >
                  <option value="json">JSON</option>
                  <option value="csv">CSV</option>
                </Select>
              </FormControl>
              <FormControl>
                <FormLabel fontSize="sm">{t('dataExport.startTime')}</FormLabel>
                <Input
                  type="datetime-local"
                  value={startTime}
                  onChange={(e) => setStartTime(e.target.value)}
                  size="sm"
                />
              </FormControl>
              <FormControl>
                <FormLabel fontSize="sm">{t('dataExport.endTime')}</FormLabel>
                <Input
                  type="datetime-local"
                  value={endTime}
                  onChange={(e) => setEndTime(e.target.value)}
                  size="sm"
                />
              </FormControl>
            </SimpleGrid>
          </CardBody>
        </Card>

        {/* 全量導出按鈕 */}
        <Card variant="outline" bg="blue.50" borderColor="blue.200">
          <CardBody>
            <Flex justify="space-between" align="center" wrap="wrap" gap={4}>
              <Box>
                <HStack mb={1}>
                  <Icon as={AttachmentIcon} color="blue.500" />
                  <Heading size="sm" color="blue.700">
                    {t('dataExport.exportAll.title')}
                  </Heading>
                  <Badge colorScheme="blue">ZIP</Badge>
                </HStack>
                <Text fontSize="sm" color="gray.600">
                  {t('dataExport.exportAll.desc')}
                </Text>
              </Box>
              <Button
                colorScheme="blue"
                leftIcon={loadingItems.has('all') ? <Spinner size="sm" /> : <DownloadIcon />}
                onClick={handleExportAll}
                isLoading={loadingItems.has('all')}
                loadingText={t('dataExport.exporting')}
                size="md"
              >
                {t('dataExport.downloadAll')}
              </Button>
            </Flex>
          </CardBody>
        </Card>

        <Divider />

        {/* 單項導出列表 */}
        <SimpleGrid columns={{ base: 1, md: 2, lg: 3 }} spacing={4}>
          {exportItems.map((item) => (
            <Card key={item.id} variant="outline" _hover={{ shadow: 'md' }} transition="all 0.2s">
              <CardBody>
                <VStack align="stretch" spacing={3}>
                  <HStack justify="space-between">
                    <HStack>
                      <Icon as={item.icon} color={`${item.color}.500`} boxSize={5} />
                      <Heading size="sm">{t(item.titleKey)}</Heading>
                    </HStack>
                    <HStack spacing={1}>
                      {item.supportsFormat && (
                        <Badge colorScheme="gray" fontSize="xs">
                          {format.toUpperCase()}
                        </Badge>
                      )}
                      {!item.supportsFormat && (
                        <Badge colorScheme="purple" fontSize="xs">
                          {item.id === 'config' ? 'YAML' : 'ZIP'}
                        </Badge>
                      )}
                    </HStack>
                  </HStack>
                  <Text fontSize="sm" color="gray.600" noOfLines={2}>
                    {t(item.descKey)}
                  </Text>
                  <Button
                    size="sm"
                    variant="outline"
                    colorScheme={item.color}
                    leftIcon={loadingItems.has(item.id) ? <Spinner size="xs" /> : <DownloadIcon />}
                    onClick={() => handleExport(item)}
                    isLoading={loadingItems.has(item.id)}
                    loadingText={t('dataExport.exporting')}
                  >
                    {t('dataExport.download')}
                  </Button>
                </VStack>
              </CardBody>
            </Card>
          ))}
        </SimpleGrid>

        {/* 提示信息 */}
        <Card variant="outline" bg="gray.50">
          <CardBody>
            <HStack spacing={3} align="flex-start">
              <Icon as={InfoIcon} color="gray.500" mt={1} />
              <Box>
                <Text fontSize="sm" fontWeight="medium" color="gray.700">
                  {t('dataExport.tips.title')}
                </Text>
                <Text fontSize="sm" color="gray.600" mt={1}>
                  {t('dataExport.tips.content')}
                </Text>
              </Box>
            </HStack>
          </CardBody>
        </Card>
      </VStack>
    </Box>
  )
}

export default DataExport
