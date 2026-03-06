import React from 'react'
import { useTranslation } from 'react-i18next'
import {
  Modal,
  ModalOverlay,
  ModalContent,
  ModalHeader,
  ModalFooter,
  ModalBody,
  ModalCloseButton,
  Button,
  VStack,
  HStack,
  Text,
  Badge,
  Divider,
  Box,
  Code,
  Icon,
} from '@chakra-ui/react'
import { WarningIcon, InfoIcon, CheckCircleIcon, TimeIcon, RepeatIcon } from '@chakra-ui/icons'
import { EventRecord } from '../services/api'
import { useConfig } from '../contexts/ConfigContext'
import { formatDateTime } from '../utils/dateFormat'

interface EventDetailModalProps {
  event: EventRecord
  isOpen: boolean
  onClose: () => void
}

const EventDetailModal: React.FC<EventDetailModalProps> = ({ event, isOpen, onClose }) => {
  const { t, i18n } = useTranslation()
  const { timezone } = useConfig()

  // 解析详细信息
  const parseDetails = () => {
    try {
      if (!event.details || event.details.trim() === '' || event.details === 'null') {
        return {}
      }
      return JSON.parse(event.details)
    } catch (err) {
      console.error('解析事件详情JSON失败:', err, 'Raw details:', event.details)
      return { error: t('eventDetail.parseDetailsFailed'), raw: event.details }
    }
  }

  const details = parseDetails()

  // 獲取严重程度配置
  const getSeverityConfig = (severity: string) => {
    const config = {
      critical: { colorScheme: 'red', icon: WarningIcon, label: t('eventDetail.severityCritical') },
      warning: { colorScheme: 'orange', icon: InfoIcon, label: t('eventDetail.severityWarning') },
      info: { colorScheme: 'blue', icon: CheckCircleIcon, label: t('eventDetail.severityInfo') },
    }
    return config[severity as keyof typeof config] || config.info
  }

  // 獲取来源標签
  const getSourceLabel = (source: string) => {
    const labels: Record<string, string> = {
      exchange: t('eventDetail.sourceExchange'),
      network: t('eventDetail.sourceNetwork'),
      system: t('eventDetail.sourceSystem'),
      strategy: t('eventDetail.sourceStrategy'),
      risk: t('eventDetail.sourceRisk'),
      api: 'API',
    }
    return labels[source] || source
  }

  // 格式化時间
  const formatTime = (timeStr: string) => {
    return formatDateTime(timeStr, timezone, i18n.language)
  }

  // 格式化JSON
  const formatJSON = (obj: any) => {
    return JSON.stringify(obj, null, 2)
  }

  const severityConfig = getSeverityConfig(event.severity)

  return (
    <Modal isOpen={isOpen} onClose={onClose} size="xl" scrollBehavior="inside">
      <ModalOverlay />
      <ModalContent>
        <ModalHeader>
          <HStack spacing={3}>
            <Icon as={severityConfig.icon} color={`${severityConfig.colorScheme}.500`} boxSize={5} />
            <Text>{t('eventDetail.title')}</Text>
          </HStack>
        </ModalHeader>
        <ModalCloseButton />
        
        <ModalBody>
          <VStack align="stretch" spacing={4}>
            {/* 基本信息 */}
            <Box>
              <Text fontSize="sm" color="gray.500" mb={2}>{t('eventDetail.basicInfo')}</Text>
              <VStack align="stretch" spacing={3} p={4} bg="gray.50" borderRadius="md">
                <HStack justify="space-between">
                  <Text fontWeight="medium">{t('eventDetail.eventId')}:</Text>
                  <Text>{event.id}</Text>
                </HStack>
                
                <HStack justify="space-between">
                  <Text fontWeight="medium">{t('eventDetail.severity')}:</Text>
                  <Badge colorScheme={severityConfig.colorScheme} display="flex" alignItems="center" gap={1}>
                    <Icon as={severityConfig.icon} boxSize={3} />
                    {severityConfig.label}
                  </Badge>
                </HStack>
                
                <HStack justify="space-between">
                  <Text fontWeight="medium">{t('eventDetail.eventSource')}:</Text>
                  <Badge colorScheme="gray">{getSourceLabel(event.source)}</Badge>
                </HStack>
                
                <HStack justify="space-between">
                  <Text fontWeight="medium">{t('eventDetail.eventType')}:</Text>
                  <Code fontSize="sm">{event.type}</Code>
                </HStack>
                
                {event.exchange && (
                  <HStack justify="space-between">
                    <Text fontWeight="medium">{t('eventDetail.exchange')}:</Text>
                    <Text>{event.exchange}</Text>
                  </HStack>
                )}
                
                {event.symbol && (
                  <HStack justify="space-between">
                    <Text fontWeight="medium">{t('eventDetail.tradingPair')}:</Text>
                    <Badge colorScheme="purple">{event.symbol}</Badge>
                  </HStack>
                )}
                
                <HStack justify="space-between">
                  <Text fontWeight="medium">{t('eventDetail.occurredAt')}:</Text>
                  <HStack spacing={1}>
                    <Icon as={TimeIcon} boxSize={3} color="gray.500" />
                    <Text fontSize="sm">{formatTime(event.created_at)}</Text>
                  </HStack>
                </HStack>
              </VStack>
            </Box>

            <Divider />

            {/* 事件標题 */}
            <Box>
              <Text fontSize="sm" color="gray.500" mb={2}>{t('eventDetail.eventTitle')}</Text>
              <Text fontSize="lg" fontWeight="semibold">{event.title}</Text>
            </Box>

            <Divider />

            {/* 事件消息 */}
            <Box>
              <Text fontSize="sm" color="gray.500" mb={2}>{t('eventDetail.eventMessage')}</Text>
              <Box p={4} bg="gray.50" borderRadius="md">
                <Text>{event.message || t('eventDetail.noMessage')}</Text>
              </Box>
            </Box>

            <Divider />

            {/* 详细信息 */}
            <Box>
              <Text fontSize="sm" color="gray.500" mb={2}>{t('eventDetail.detailedInfo')}</Text>
              {Object.keys(details).length > 0 ? (
                <Box
                  p={4}
                  bg="gray.900"
                  borderRadius="md"
                  maxH="300px"
                  overflowY="auto"
                  css={{
                    '&::-webkit-scrollbar': {
                      width: '8px',
                    },
                    '&::-webkit-scrollbar-track': {
                      background: '#2D3748',
                    },
                    '&::-webkit-scrollbar-thumb': {
                      background: '#4A5568',
                      borderRadius: '4px',
                    },
                  }}
                >
                  <Code
                    display="block"
                    whiteSpace="pre"
                    fontSize="sm"
                    bg="transparent"
                    color="green.300"
                    p={0}
                  >
                    {formatJSON(details)}
                  </Code>
                </Box>
              ) : (
                <Box p={4} bg="gray.50" borderRadius="md" textAlign="center">
                  <Text color="gray.500" fontSize="sm">
                    {t('eventDetail.noDetails')}
                  </Text>
                </Box>
              )}
            </Box>
          </VStack>
        </ModalBody>

        <ModalFooter>
          <Button onClick={onClose}>{t('common.close')}</Button>
        </ModalFooter>
      </ModalContent>
    </Modal>
  )
}

export default EventDetailModal

