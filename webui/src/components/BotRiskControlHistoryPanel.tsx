import React, { useCallback, useEffect, useState } from 'react'
import {
  Box,
  Button,
  Card,
  CardBody,
  Flex,
  Heading,
  HStack,
  IconButton,
  Spinner,
  Table,
  TableContainer,
  Tbody,
  Td,
  Text,
  Th,
  Thead,
  Tr,
  Badge,
  useToast,
} from '@chakra-ui/react'
import { ChevronLeftIcon, ChevronRightIcon, DownloadIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import {
  getBotRiskControlEvents,
  downloadBotRiskControlEventsCsv,
  BotRiskControlEventItem,
} from '../services/api'
import { formatDateTime } from '../utils/dateFormat'
import { useConfig } from '../contexts/ConfigContext'
import { displayPauseOpeningReason } from '../utils/botPauseReason'

const PAGE_SIZE = 20

function sourceLabel(source: string, t: (k: string) => string): string {
  const s = (source || '').trim()
  if (s === 'config') return t('botRiskControl.riskHistorySourceConfig')
  if (s === 'opening_manager') return t('botRiskControl.riskHistorySourceOpening')
  if (s === 'auto_timer') return t('botRiskControl.riskHistorySourceAutoTimer')
  return s || '—'
}

interface BotRiskControlHistoryPanelProps {
  botId: string
}

const BotRiskControlHistoryPanel: React.FC<BotRiskControlHistoryPanelProps> = ({ botId }) => {
  const { t, i18n } = useTranslation()
  const toast = useToast()
  const { timezone } = useConfig()
  const [page, setPage] = useState(1)
  const [loading, setLoading] = useState(true)
  const [downloading, setDownloading] = useState(false)
  const [events, setEvents] = useState<BotRiskControlEventItem[]>([])
  const [total, setTotal] = useState(0)
  const [totalPage, setTotalPage] = useState(0)

  const load = useCallback(async () => {
    if (!botId) return
    setLoading(true)
    try {
      const res = await getBotRiskControlEvents(botId, page, PAGE_SIZE)
      setEvents(res.events || [])
      setTotal(res.total ?? 0)
      setTotalPage(Number(res.total_page ?? 0))
    } catch (e) {
      console.error(e)
      toast({
        title: t('botRiskControl.riskHistoryLoadFailed'),
        status: 'error',
        duration: 4000,
      })
      setEvents([])
      setTotal(0)
      setTotalPage(0)
    } finally {
      setLoading(false)
    }
  }, [botId, page, t, toast])

  useEffect(() => {
    void load()
  }, [load])

  const handleDownload = async () => {
    if (!botId) return
    setDownloading(true)
    try {
      await downloadBotRiskControlEventsCsv(botId)
      toast({ title: t('botRiskControl.riskHistoryDownloadOk'), status: 'success', duration: 2000 })
    } catch (e) {
      console.error(e)
      toast({
        title: t('botRiskControl.riskHistoryDownloadFailed'),
        status: 'error',
        duration: 4000,
      })
    } finally {
      setDownloading(false)
    }
  }

  return (
    <Card mt={4}>
      <CardBody>
        <Flex justify="space-between" align="flex-start" flexWrap="wrap" gap={3} mb={4}>
          <Box>
            <Heading size="sm">{t('botRiskControl.riskHistoryTitle')}</Heading>
            <Text fontSize="sm" color="gray.500" mt={1}>
              {t('botRiskControl.riskHistoryDesc')}
            </Text>
          </Box>
          <Button
            size="sm"
            leftIcon={<DownloadIcon />}
            variant="outline"
            isLoading={downloading}
            onClick={() => void handleDownload()}
          >
            {t('botRiskControl.riskHistoryDownload')}
          </Button>
        </Flex>

        {loading && events.length === 0 ? (
          <Flex justify="center" py={8}>
            <Spinner />
          </Flex>
        ) : events.length === 0 ? (
          <Text color="gray.500" fontSize="sm">
            {t('botRiskControl.riskHistoryEmpty')}
          </Text>
        ) : (
          <>
            <TableContainer maxH="420px" overflowY="auto">
              <Table size="sm" variant="simple">
                <Thead>
                  <Tr>
                    <Th>{t('botRiskControl.riskHistoryTime')}</Th>
                    <Th>{t('botRiskControl.riskHistoryEventType')}</Th>
                    <Th>{t('botRiskControl.riskHistoryReason')}</Th>
                    <Th>{t('botRiskControl.riskHistorySource')}</Th>
                  </Tr>
                </Thead>
                <Tbody>
                  {events.map((ev) => (
                    <Tr key={ev.id}>
                      <Td whiteSpace="nowrap" fontSize="xs">
                        {formatDateTime(ev.created_at, timezone, i18n.language)}
                      </Td>
                      <Td>
                        <Badge
                          colorScheme={ev.event_type === 'paused' ? 'orange' : 'green'}
                          size="sm"
                        >
                          {ev.event_type === 'paused'
                            ? t('botRiskControl.riskHistoryPaused')
                            : t('botRiskControl.riskHistoryResumed')}
                        </Badge>
                      </Td>
                      <Td fontSize="sm" maxW="xs" noOfLines={2}>
                        {ev.event_type === 'paused'
                          ? displayPauseOpeningReason(ev.reason, t) || '—'
                          : ev.reason?.trim()
                            ? displayPauseOpeningReason(ev.reason, t)
                            : '—'}
                      </Td>
                      <Td fontSize="sm">{sourceLabel(ev.source, t)}</Td>
                    </Tr>
                  ))}
                </Tbody>
              </Table>
            </TableContainer>

            <Flex justify="space-between" align="center" mt={4} flexWrap="wrap" gap={2}>
              <Text fontSize="xs" color="gray.500">
                {t('botRiskControl.riskHistoryPageInfo', {
                  page,
                  totalPage: totalPage || 1,
                  total,
                })}
              </Text>
              <HStack>
                <IconButton
                  aria-label={t('botRiskControl.riskHistoryPrev')}
                  icon={<ChevronLeftIcon />}
                  size="sm"
                  variant="outline"
                  isDisabled={page <= 1 || loading}
                  onClick={() => setPage((p) => Math.max(1, p - 1))}
                />
                <IconButton
                  aria-label={t('botRiskControl.riskHistoryNext')}
                  icon={<ChevronRightIcon />}
                  size="sm"
                  variant="outline"
                  isDisabled={loading || totalPage <= 0 || page >= totalPage}
                  onClick={() => setPage((p) => p + 1)}
                />
              </HStack>
            </Flex>
          </>
        )}
      </CardBody>
    </Card>
  )
}

export default BotRiskControlHistoryPanel
