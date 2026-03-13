import React, { useCallback, useEffect, useState } from 'react'
import {
  Box,
  Button,
  Card,
  CardBody,
  Flex,
  Heading,
  Spinner,
  Text,
  Badge,
  useToast,
  VStack,
  Alert,
  AlertIcon,
} from '@chakra-ui/react'
import { useTranslation } from 'react-i18next'
import {
  getOptionHedgeStatus,
  syncOptionHedge,
  getOptionHedgeRollSuggestions,
  executeOptionHedgeRoll,
  OptionHedgeStatus,
  RollSuggestion,
} from '../services/api'

interface OptionHedgePanelProps {
  botId: string
}

const OptionHedgePanel: React.FC<OptionHedgePanelProps> = ({ botId }) => {
  const { t } = useTranslation()
  const toast = useToast()
  const [status, setStatus] = useState<OptionHedgeStatus | null>(null)
  const [suggestions, setSuggestions] = useState<RollSuggestion[]>([])
  const [loading, setLoading] = useState(true)
  const [syncing, setSyncing] = useState(false)

  const fetchStatus = useCallback(async () => {
    if (!botId) return
    try {
      const s = await getOptionHedgeStatus(botId)
      setStatus(s)
    } catch {
      setStatus(null)
    } finally {
      setLoading(false)
    }
  }, [botId])

  useEffect(() => {
    fetchStatus()
    const interval = setInterval(fetchStatus, 30000)
    return () => clearInterval(interval)
  }, [fetchStatus])

  const handleSync = useCallback(async () => {
    if (!botId) return
    setSyncing(true)
    try {
      const res = await syncOptionHedge(botId)
      toast({ title: t('optionHedge.syncSuccess'), status: 'success', duration: 2000 })
      await fetchStatus()
    } catch (err) {
      toast({ title: t('optionHedge.syncFailed'), status: 'error', duration: 3000 })
    } finally {
      setSyncing(false)
    }
  }, [botId, fetchStatus, t, toast])

  const handleLoadSuggestions = useCallback(async () => {
    if (!botId) return
    try {
      const { suggestions: s } = await getOptionHedgeRollSuggestions(botId)
      setSuggestions(s)
    } catch {
      setSuggestions([])
    }
  }, [botId])

  const handleRecordRoll = useCallback(
    async (fromInst: string, toInst: string) => {
      if (!botId) return
      try {
        await executeOptionHedgeRoll(botId, {
          from_instrument: fromInst,
          to_instrument: toInst,
          action: 'roll_executed',
          details: 'manual_record',
        })
        toast({ title: t('optionHedge.rollRecorded'), status: 'success', duration: 2000 })
        setSuggestions([])
      } catch {
        toast({ title: t('optionHedge.rollFailed'), status: 'error', duration: 3000 })
      }
    },
    [botId, t, toast]
  )

  if (loading) {
    return (
      <Flex justify="center" align="center" minH="120px">
        <Spinner size="lg" />
      </Flex>
    )
  }

  if (!status || !status.enabled) {
    return (
      <Card>
        <CardBody>
          <Text fontSize="sm" color="gray.500">
            {t('optionHedge.disabledHint')}
          </Text>
        </CardBody>
      </Card>
    )
  }

  return (
    <VStack spacing={4} align="stretch">
      <Card>
        <CardBody>
          <Flex justify="space-between" align="center" mb={4}>
            <Heading size="sm">{t('optionHedge.title')}</Heading>
            <Button size="sm" colorScheme="blue" onClick={handleSync} isLoading={syncing}>
              {t('optionHedge.sync')}
            </Button>
          </Flex>
          {status.alerts && status.alerts.length > 0 && (
            <Alert status="warning" borderRadius="md" mb={4}>
              <AlertIcon />
              <Text fontSize="sm">{status.alerts.map((a) => t(`optionHedge.alert.${a}`)).join('; ')}</Text>
            </Alert>
          )}
          {status.coverage && (
            <Box mb={4}>
              <Text fontSize="sm" color="gray.500">
                {t('optionHedge.nominalCoverage')}: {(status.coverage.nominal_coverage * 100).toFixed(1)}%
              </Text>
              <Text fontSize="sm" color="gray.500">
                {t('optionHedge.deltaCoverage')}: {(status.coverage.delta_coverage * 100).toFixed(1)}%
              </Text>
              <Text fontSize="sm" color="gray.500">
                {t('optionHedge.minDTE')}: {status.coverage.min_dte}
              </Text>
              <Text fontSize="sm" color="gray.500">
                {t('optionHedge.totalPremium')}: ${status.coverage.total_premium.toFixed(2)}
              </Text>
            </Box>
          )}
          {status.positions && status.positions.length > 0 ? (
            <Box>
              <Text fontSize="sm" fontWeight="bold" mb={2}>
                {t('optionHedge.positions')}
              </Text>
              {status.positions.map((p, i) => (
                <Box key={i} fontSize="sm" mb={1}>
                  {p.instrument} | {p.qty} × ${p.strike} | DTE: {p.expiry}
                </Box>
              ))}
            </Box>
          ) : (
            <Text fontSize="sm" color="gray.500">
              {t('optionHedge.noPositions')}
            </Text>
          )}
          <Badge colorScheme={status.sync_status === 'ok' ? 'green' : 'red'} mt={2}>
            {status.sync_status}
          </Badge>
        </CardBody>
      </Card>
      {status.coverage && (
        <Card>
          <CardBody>
            <Heading size="sm" mb={4}>
              {t('optionHedge.rollSuggestions')}
            </Heading>
            <Button size="sm" variant="outline" onClick={handleLoadSuggestions} mb={4}>
              {t('optionHedge.loadSuggestions')}
            </Button>
            {suggestions.length > 0 && (
              <VStack align="stretch" spacing={2}>
                {suggestions.map((s, i) => (
                  <Box key={i} p={2} borderWidth={1} borderRadius="md">
                    <Text fontSize="sm">
                      {t(`optionHedge.${s.label}`)}: Strike ${s.strike} DTE {s.dte}
                    </Text>
                    <Button size="xs" mt={2} onClick={() => handleRecordRoll('', s.instrument || '')}>
                      {t('optionHedge.recordRoll')}
                    </Button>
                  </Box>
                ))}
              </VStack>
            )}
          </CardBody>
        </Card>
      )}
    </VStack>
  )
}

export default OptionHedgePanel
