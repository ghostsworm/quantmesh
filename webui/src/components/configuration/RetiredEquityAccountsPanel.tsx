import React, { useCallback, useEffect, useState } from 'react'
import {
  Alert,
  AlertDescription,
  AlertIcon,
  AlertTitle,
  Badge,
  Box,
  Button,
  Checkbox,
  HStack,
  Modal,
  ModalBody,
  ModalCloseButton,
  ModalContent,
  ModalFooter,
  ModalHeader,
  ModalOverlay,
  Spinner,
  Table,
  TableContainer,
  Tbody,
  Td,
  Th,
  Thead,
  Tr,
  VStack,
  Text,
  useDisclosure,
} from '@chakra-ui/react'
import { RepeatIcon, WarningIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import type { RetiredEquityAccount, RetiredEquityResetOperation } from '../../services/api'
import { ConfigCard } from './ConfigurationSections'

type RequestError = Error & { status?: number; responseBody?: unknown }

export const RETIRED_EQUITY_FLAT_EVIDENCE_REQUIRED = 2

export function canResetRetiredEquityAccounts(accounts: RetiredEquityAccount[]): boolean {
  return accounts.length > 0 && accounts.every((account) => account.status === 'ready_for_explicit_reset' && account.flat_evidence_count >= RETIRED_EQUITY_FLAT_EVIDENCE_REQUIRED)
}

function operationFromError(error: unknown): RetiredEquityResetOperation | null {
  const body = (error as RequestError | null)?.responseBody
  if (!body || typeof body !== 'object' || !('operation' in body)) return null
  const operation = (body as { operation?: RetiredEquityResetOperation }).operation
  return operation && typeof operation.id === 'string' ? operation : null
}

function formatTimestamp(value: string | undefined, fallback: string): string {
  if (!value) return fallback
  const timestamp = new Date(value)
  return Number.isNaN(timestamp.getTime()) ? fallback : timestamp.toLocaleString()
}

export function RetiredEquityAccountsPanel() {
  const { t } = useTranslation()
  const [accounts, setAccounts] = useState<RetiredEquityAccount[]>([])
  const [history, setHistory] = useState<RetiredEquityResetOperation[]>([])
  const [pendingOperation, setPendingOperation] = useState<RetiredEquityResetOperation | null>(null)
  const [loading, setLoading] = useState(true)
  const [resetting, setResetting] = useState(false)
  const [loadError, setLoadError] = useState<'adminRequired' | 'unavailable' | null>(null)
  const [resetError, setResetError] = useState(false)
  const [acknowledged, setAcknowledged] = useState(false)
  const { isOpen, onOpen, onClose } = useDisclosure()

  const loadData = useCallback(async (signal?: AbortSignal) => {
    setLoading(true)
    setLoadError(null)
    try {
      const api = await import('../../services/api')
      const [accountResponse, historyResponse] = await Promise.all([
        api.getRetiredEquityAccounts(signal),
        api.getRetiredEquityResetHistory(signal),
      ])
      setAccounts(accountResponse.accounts)
      setHistory(historyResponse.history)
    } catch (error) {
      if (signal?.aborted) return
      const status = (error as RequestError | null)?.status
      if (status === 403) {
        setLoadError('adminRequired')
      } else if (!(error instanceof DOMException && error.name === 'AbortError')) {
        setLoadError('unavailable')
      }
    } finally {
      if (!signal?.aborted) setLoading(false)
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    void loadData(controller.signal)
    return () => controller.abort()
  }, [loadData])

  const openConfirmation = () => {
    setAcknowledged(false)
    setResetError(false)
    onOpen()
  }

  const confirmReset = async () => {
    setResetting(true)
    setResetError(false)
    setPendingOperation(null)
    try {
      const api = await import('../../services/api')
      const response = await api.resetRetiredEquityAccounts()
      setHistory((current) => [response.operation, ...current.filter((item) => item.id !== response.operation.id)])
      setPendingOperation(null)
      onClose()
      await loadData()
    } catch (error) {
      setResetError(true)
      setPendingOperation(operationFromError(error))
      await loadData()
    } finally {
      setResetting(false)
    }
  }

  return (
    <ConfigCard title={t('configuration.retiredEquity.title')} icon={<WarningIcon />}>
      <VStack spacing={4} align="stretch">
        <Alert status="warning" borderRadius="md">
          <AlertIcon />
          <AlertDescription fontSize="sm">{t('configuration.retiredEquity.warning')}</AlertDescription>
        </Alert>

        {loadError && (
          <Alert status={loadError === 'adminRequired' ? 'info' : 'error'} borderRadius="md">
            <AlertIcon />
            <AlertDescription fontSize="sm">{t(`configuration.retiredEquity.${loadError}`)}</AlertDescription>
          </Alert>
        )}

        {resetError && (
          <Alert status="error" borderRadius="md">
            <AlertIcon />
            <AlertTitle mr={2}>{t('configuration.retiredEquity.resetFailedTitle')}</AlertTitle>
            <AlertDescription fontSize="sm">{t('configuration.retiredEquity.resetFailed')}</AlertDescription>
          </Alert>
        )}

        {pendingOperation && (
          <Alert status="warning" borderRadius="md">
            <AlertIcon />
            <AlertDescription fontSize="sm">
              {t('configuration.retiredEquity.pendingOperation', { id: pendingOperation.id, actor: pendingOperation.actor })}
            </AlertDescription>
          </Alert>
        )}

        {loading ? (
          <HStack justify="center" py={4}><Spinner size="sm" /><Text>{t('configuration.retiredEquity.loading')}</Text></HStack>
        ) : !loadError && (
          <>
            {accounts.length === 0 ? (
              <Text fontSize="sm" color="gray.600">{t('configuration.retiredEquity.noAccounts')}</Text>
            ) : (
              <TableContainer>
                <Table size="sm" variant="simple">
                  <Thead><Tr>
                    <Th>{t('configuration.retiredEquity.exchange')}</Th>
                    <Th>{t('configuration.retiredEquity.market')}</Th>
                    <Th>{t('configuration.retiredEquity.status')}</Th>
                    <Th isNumeric>{t('configuration.retiredEquity.evidence')}</Th>
                    <Th>{t('configuration.retiredEquity.lastObserved')}</Th>
                  </Tr></Thead>
                  <Tbody>
                    {accounts.map((account) => (
                      <Tr key={account.id}>
                        <Td>{account.exchange}</Td>
                        <Td>{account.market_type}</Td>
                        <Td>
                          <Badge colorScheme={account.status === 'ready_for_explicit_reset' ? 'green' : 'orange'}>
                            {t(`configuration.retiredEquity.statuses.${account.status}`, { defaultValue: t('configuration.retiredEquity.unknownStatus') })}
                          </Badge>
                        </Td>
                        <Td isNumeric>{t('configuration.retiredEquity.evidenceCount', { count: account.flat_evidence_count, required: RETIRED_EQUITY_FLAT_EVIDENCE_REQUIRED })}</Td>
                        <Td>{formatTimestamp(account.last_observed_at, t('configuration.retiredEquity.notAvailable'))}</Td>
                      </Tr>
                    ))}
                  </Tbody>
                </Table>
              </TableContainer>
            )}

            <Button
              alignSelf="flex-start"
              colorScheme="orange"
              variant="outline"
              leftIcon={<RepeatIcon />}
              isDisabled={!canResetRetiredEquityAccounts(accounts) || resetting}
              onClick={openConfirmation}
            >
              {t('configuration.retiredEquity.reset')}
            </Button>
          </>
        )}

        {!loading && !loadError && (
          <Box>
            <Text fontSize="sm" fontWeight="semibold" mb={2}>{t('configuration.retiredEquity.historyTitle')}</Text>
            {history.length === 0 ? (
              <Text fontSize="sm" color="gray.600">{t('configuration.retiredEquity.noHistory')}</Text>
            ) : history.slice(0, 10).map((operation) => (
              <Text key={operation.id} fontSize="xs" color="gray.600">
                {operation.actor} · {formatTimestamp(operation.completed_at, t('configuration.retiredEquity.notAvailable'))} · {t('configuration.retiredEquity.baselineRevision', { revision: operation.baseline_revision ?? t('configuration.retiredEquity.notAvailable') })}
              </Text>
            ))}
          </Box>
        )}
      </VStack>

      <Modal isOpen={isOpen} onClose={() => { if (!resetting) onClose() }} isCentered>
        <ModalOverlay />
        <ModalContent>
          <ModalHeader>{t('configuration.retiredEquity.confirmTitle')}</ModalHeader>
          <ModalCloseButton isDisabled={resetting} />
          <ModalBody>
            <VStack align="stretch" spacing={4}>
              <Text fontSize="sm">{t('configuration.retiredEquity.confirmDescription')}</Text>
              <Checkbox isChecked={acknowledged} onChange={(event) => setAcknowledged(event.target.checked)}>
                {t('configuration.retiredEquity.confirmAcknowledgement')}
              </Checkbox>
            </VStack>
          </ModalBody>
          <ModalFooter>
            <Button variant="ghost" mr={3} onClick={onClose} isDisabled={resetting}>{t('configuration.retiredEquity.cancel')}</Button>
            <Button colorScheme="red" onClick={confirmReset} isDisabled={!acknowledged || resetting} isLoading={resetting}>
              {t('configuration.retiredEquity.confirmReset')}
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>
    </ConfigCard>
  )
}
