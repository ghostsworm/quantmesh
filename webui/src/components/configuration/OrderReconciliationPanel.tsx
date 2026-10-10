import React, { useCallback, useEffect, useRef, useState } from 'react'
import {
  Alert, AlertDescription, AlertIcon, AlertTitle, Badge, Box, Button, Code,
  FormControl, FormLabel, HStack, Input, Modal, ModalBody, ModalCloseButton,
  ModalContent, ModalFooter, ModalHeader, ModalOverlay, Spinner, Text,
  Textarea, VStack,
} from '@chakra-ui/react'
import { RepeatIcon, WarningIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import {
  canManuallyReconcile, canReleaseOrderQuarantine, createOrderReconciliation,
  listOrderReconciliations, reconcileOrderAccounting, releaseOrderQuarantine,
  type OrderReconciliationCase, type OrderReconciliationOwner,
} from '../../services/orderReconciliation'
import { ConfigCard } from './ConfigurationSections'

type RequestError = Error & { status?: number; responseBody?: unknown }
type PendingAction = { kind: 'reconcile' | 'release'; item: OrderReconciliationCase } | null

const emptyOwner: OrderReconciliationOwner = {
  exchange: '', market: '', account_scope: '', bot: '', symbol: '', strategy_name: '',
  strategy_type: '', client_order_id: '', venue_order_id: '',
}

function requestErrorMessage(error: unknown): string | null {
  const body = (error as RequestError | null)?.responseBody
  if (!body || typeof body !== 'object') return null
  const record = body as Record<string, unknown>
  for (const key of ['error', 'error_key', 'code', 'message']) {
    if (typeof record[key] === 'string' && record[key]) return record[key] as string
  }
  return null
}

function idempotencyKey(): string {
  const generator = globalThis.crypto?.randomUUID
  if (!generator) throw new Error('crypto_random_uuid_unavailable')
  return generator.call(globalThis.crypto)
}

export function OrderReconciliationPanel() {
  const { t } = useTranslation()
  const [cases, setCases] = useState<OrderReconciliationCase[]>([])
  const [owner, setOwner] = useState<OrderReconciliationOwner>(emptyOwner)
  const [expectedIntentRevision, setExpectedIntentRevision] = useState('')
  const [expectedStrategyRevision, setExpectedStrategyRevision] = useState('')
  const [reason, setReason] = useState('')
  const [evidenceText, setEvidenceText] = useState('')
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState<'adminRequired' | 'unavailable' | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)
  const [pendingAction, setPendingAction] = useState<PendingAction>(null)
  const [workingCaseId, setWorkingCaseId] = useState<string | null>(null)
  const [confirmationChecked, setConfirmationChecked] = useState(false)
  const createKeyRef = useRef<string | null>(null)
  const createInFlightRef = useRef(false)
  const actionInFlightRef = useRef(false)

  const loadCases = useCallback(async (signal?: AbortSignal) => {
    setLoading(true)
    setLoadError(null)
    try {
      const result = await listOrderReconciliations(signal)
      setCases(result)
    } catch (error) {
      if (signal?.aborted) return
      if ((error as RequestError | null)?.status === 403) setLoadError('adminRequired')
      else setLoadError('unavailable')
      setCases([])
    } finally {
      if (!signal?.aborted) setLoading(false)
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    void loadCases(controller.signal)
    return () => controller.abort()
  }, [loadCases])

  const updateCase = (updated: OrderReconciliationCase) => {
    setCases((current) => [updated, ...current.filter((item) => item.case_id !== updated.case_id)])
  }

  const submitCase = async (event: React.FormEvent) => {
    event.preventDefault()
    if (createInFlightRef.current) return
    const evidence_refs = evidenceText.split(/\r?\n/).map((line) => line.trim()).filter(Boolean)
    if (!reason.trim() || evidence_refs.length === 0 || Object.values(owner).some((value) => !value.trim()) || !expectedIntentRevision.trim() || !expectedStrategyRevision.trim()) return
    createInFlightRef.current = true
    setCreating(true)
    setActionError(null)
    try {
      createKeyRef.current ??= idempotencyKey()
      const created = await createOrderReconciliation({
        owner, expected_intent_revision: expectedIntentRevision.trim(), expected_strategy_revision: expectedStrategyRevision.trim(), reason: reason.trim(), evidence_refs,
        idempotency_key: createKeyRef.current,
      })
      createKeyRef.current = null
      updateCase(created)
      await loadCases()
    } catch (error) {
      setActionError(requestErrorMessage(error) || t('configuration.orderReconciliation.actionFailed'))
      await loadCases()
    } finally {
      createInFlightRef.current = false
      setCreating(false)
    }
  }

  const confirmAction = async () => {
    if (!pendingAction || !confirmationChecked || workingCaseId || actionInFlightRef.current) return
    const { kind, item } = pendingAction
    actionInFlightRef.current = true
    setWorkingCaseId(item.case_id)
    setActionError(null)
    try {
      const updated = kind === 'reconcile'
        ? await reconcileOrderAccounting(item)
        : await releaseOrderQuarantine(item)
      if (kind === 'release' && updated.status !== 'released') throw new Error('order_reconciliation_release_not_verified')
      if (kind === 'reconcile' && updated.status !== 'ready_to_release') throw new Error('order_reconciliation_reconcile_not_verified')
      updateCase(updated)
      setPendingAction(null)
      setConfirmationChecked(false)
      await loadCases()
    } catch (error) {
      setActionError(requestErrorMessage(error) || t('configuration.orderReconciliation.actionFailed'))
      setPendingAction(null)
      setConfirmationChecked(false)
      await loadCases()
    } finally {
      actionInFlightRef.current = false
      setWorkingCaseId(null)
    }
  }

  const openActionConfirmation = (kind: 'reconcile' | 'release', item: OrderReconciliationCase) => {
    setConfirmationChecked(false)
    setActionError(null)
    setPendingAction({ kind, item })
  }

  const prepareReleaseConfirmation = async (item: OrderReconciliationCase) => {
    setActionError(null)
    try {
      const fresh = (await listOrderReconciliations()).find((candidate) => candidate.case_id === item.case_id)
      if (!fresh || !canReleaseOrderQuarantine(fresh)) throw new Error('order_reconciliation_fresh_release_evidence_required')
      openActionConfirmation('release', fresh)
    } catch (error) {
      setActionError(requestErrorMessage(error) || t('configuration.orderReconciliation.actionFailed'))
      await loadCases()
    }
  }

  return (
    <ConfigCard title={t('configuration.orderReconciliation.title')} icon={<WarningIcon />}>
      <VStack align="stretch" spacing={4}>
        <Alert status="warning" borderRadius="md">
          <AlertIcon /><AlertDescription fontSize="sm">{t('configuration.orderReconciliation.warning')}</AlertDescription>
        </Alert>

        {loadError && <Alert status={loadError === 'adminRequired' ? 'info' : 'error'}>
          <AlertIcon /><AlertDescription>{t(`configuration.orderReconciliation.${loadError}`)}</AlertDescription>
        </Alert>}
        {actionError && <Alert status="error">
          <AlertIcon /><AlertTitle mr={2}>{t('configuration.orderReconciliation.actionFailed')}</AlertTitle>
          <AlertDescription>{actionError}</AlertDescription>
        </Alert>}

        <Button alignSelf="flex-start" size="sm" variant="outline" leftIcon={<RepeatIcon />} onClick={() => void loadCases()} isDisabled={loading || Boolean(workingCaseId)}>
          {t('configuration.orderReconciliation.refresh')}
        </Button>

        {loading ? <HStack justify="center" py={3}><Spinner size="sm" /><Text>{t('configuration.orderReconciliation.loading')}</Text></HStack>
          : !loadError && (cases.length === 0
            ? <Text fontSize="sm" color="gray.600">{t('configuration.orderReconciliation.noCases')}</Text>
            : cases.map((item) => (
              <Box key={item.case_id} borderWidth="1px" borderRadius="md" p={4}>
                <VStack align="stretch" spacing={3}>
                  <HStack justify="space-between" flexWrap="wrap">
                    <Text fontWeight="semibold">{t('configuration.orderReconciliation.caseTitle', { caseId: item.case_id })}</Text>
                    <Badge colorScheme={item.status === 'released' ? 'green' : 'orange'}>
                      {t(`configuration.orderReconciliation.statuses.${item.status}`, { defaultValue: item.status })}
                    </Badge>
                  </HStack>
                  <Text fontSize="sm">{item.owner.exchange} / {item.owner.market} / {item.owner.account_scope} / {item.owner.bot} / {item.owner.strategy_name} / {item.owner.strategy_type} / {item.owner.symbol} / {item.owner.venue_order_id} / {item.owner.client_order_id}</Text>
                  <Text fontSize="xs" color="gray.600">{t('configuration.orderReconciliation.expectedIntentRevision')}: {item.expected_intent_revision || '—'} · {t('configuration.orderReconciliation.expectedStrategyRevision')}: {item.expected_strategy_revision || '—'}</Text>
                  {item.validation.blockers.length > 0 && <Text fontSize="sm" color="orange.600">{t('configuration.orderReconciliation.validationBlocked', { count: item.validation.blockers.length })}</Text>}
                  {item.receipt ? <>
                    {item.receipt.outcome === 'already_accounted' && <Badge alignSelf="flex-start" colorScheme="blue">{t('configuration.orderReconciliation.alreadyAccounted')}</Badge>}
                    <Text fontSize="sm">{t('configuration.orderReconciliation.cursor', { current: `${item.receipt.from.sequence}:${item.receipt.from.trade_id || '—'}`, target: `${item.receipt.target.sequence}:${item.receipt.target.trade_id || '—'}`, revision: item.receipt.revision })}</Text>
                    <Badge alignSelf="flex-start" colorScheme="green">{t('configuration.orderReconciliation.cursorVerified')}</Badge>
                    <Box>
                      <Text fontSize="sm" fontWeight="semibold">{t('configuration.orderReconciliation.economicResult')}</Text>
                      <Code display="block" whiteSpace="pre-wrap" overflowWrap="anywhere">{JSON.stringify(item.receipt.result, null, 2)}</Code>
                    </Box>
                  </> : <Text fontSize="sm" color="orange.600">{t('configuration.orderReconciliation.cursorUnavailable')}</Text>}
                  <Text fontSize="sm">{t('configuration.orderReconciliation.evidenceRefs', { count: item.evidence_refs.length })}</Text>
                  {item.status === 'unverified' && <Text fontSize="sm" color="red.600">{t('configuration.orderReconciliation.serverUnverified')}</Text>}
                  {item.release_evidence_hash && <Text fontSize="xs" color="gray.600">{t('configuration.orderReconciliation.releaseEvidence', { revision: item.revision, hash: item.release_evidence_hash })}</Text>}
                  <HStack flexWrap="wrap">
                    <Button colorScheme="orange" onClick={() => openActionConfirmation('reconcile', item)} isDisabled={!canManuallyReconcile(item) || Boolean(workingCaseId) || creating} isLoading={workingCaseId === item.case_id && pendingAction?.kind === 'reconcile'}>
                      {t('configuration.orderReconciliation.reconcile')}
                    </Button>
                    <Button colorScheme="red" variant="outline" onClick={() => void prepareReleaseConfirmation(item)} isDisabled={!canReleaseOrderQuarantine(item) || Boolean(workingCaseId) || creating} isLoading={workingCaseId === item.case_id && pendingAction?.kind === 'release'}>
                      {t('configuration.orderReconciliation.release')}
                    </Button>
                  </HStack>
                </VStack>
              </Box>
            )))}

        {!loadError && <Box as="form" onSubmit={submitCase} borderTopWidth="1px" pt={4}>
          <VStack align="stretch" spacing={3}>
            <Text fontWeight="semibold">{t('configuration.orderReconciliation.createTitle')}</Text>
            <Text fontSize="sm" fontWeight="semibold">{t('configuration.orderReconciliation.identity')}</Text>
            {(Object.keys(emptyOwner) as (keyof OrderReconciliationOwner)[]).map((key) => (
              <FormControl key={key} isRequired>
                <FormLabel fontSize="sm">{t(`configuration.orderReconciliation.${key === 'market' ? 'marketType' : key === 'account_scope' ? 'accountScope' : key === 'bot' ? 'botId' : key === 'strategy_name' ? 'strategyName' : key === 'strategy_type' ? 'strategyType' : key === 'venue_order_id' ? 'orderId' : key === 'client_order_id' ? 'clientOrderId' : key}`)}</FormLabel>
                <Input value={owner[key]} onChange={(event) => setOwner((current) => ({ ...current, [key]: event.target.value }))} isDisabled={creating} />
              </FormControl>
            ))}
            <FormControl isRequired>
              <FormLabel>{t('configuration.orderReconciliation.expectedIntentRevision')}</FormLabel>
              <Input value={expectedIntentRevision} onChange={(event) => setExpectedIntentRevision(event.target.value)} isDisabled={creating} />
            </FormControl>
            <FormControl isRequired>
              <FormLabel>{t('configuration.orderReconciliation.expectedStrategyRevision')}</FormLabel>
              <Input value={expectedStrategyRevision} onChange={(event) => setExpectedStrategyRevision(event.target.value)} isDisabled={creating} />
            </FormControl>
            <FormControl isRequired>
              <FormLabel>{t('configuration.orderReconciliation.reason')}</FormLabel>
              <Textarea value={reason} onChange={(event) => setReason(event.target.value)} isDisabled={creating} />
            </FormControl>
            <FormControl isRequired>
              <FormLabel>{t('configuration.orderReconciliation.evidence')}</FormLabel>
              <Textarea value={evidenceText} onChange={(event) => setEvidenceText(event.target.value)} isDisabled={creating} />
            </FormControl>
            <Button type="submit" colorScheme="blue" alignSelf="flex-start" isLoading={creating} isDisabled={creating || loading || !reason.trim() || !evidenceText.trim() || !expectedIntentRevision.trim() || !expectedStrategyRevision.trim() || Object.values(owner).some((value) => !value.trim())}>
              {t(creating ? 'configuration.orderReconciliation.creating' : 'configuration.orderReconciliation.create')}
            </Button>
          </VStack>
        </Box>}
      </VStack>

      <Modal isOpen={Boolean(pendingAction)} onClose={() => { if (!workingCaseId) setPendingAction(null) }} isCentered>
        <ModalOverlay /><ModalContent>
          <ModalHeader>{t(pendingAction?.kind === 'release' ? 'configuration.orderReconciliation.releaseConfirmTitle' : 'configuration.orderReconciliation.reconcileConfirmTitle')}</ModalHeader>
          <ModalCloseButton isDisabled={Boolean(workingCaseId)} />
          <ModalBody>
            <VStack align="stretch" spacing={3}>
              <Text>{pendingAction?.kind === 'release'
                ? t('configuration.orderReconciliation.releaseConfirmDescription', {
                  exchange: pendingAction.item.owner.exchange, symbol: pendingAction.item.owner.symbol,
                  orderId: pendingAction.item.owner.venue_order_id, clientOrderId: pendingAction.item.owner.client_order_id,
                  revision: pendingAction.item.revision, evidenceHash: pendingAction.item.release_evidence_hash,
                })
                : t('configuration.orderReconciliation.reconcileConfirmDescription')}</Text>
              <label>
                <input type="checkbox" checked={confirmationChecked} onChange={(event) => setConfirmationChecked(event.target.checked)} disabled={Boolean(workingCaseId)} />{' '}
                {t(pendingAction?.kind === 'release' ? 'configuration.orderReconciliation.confirmRelease' : 'configuration.orderReconciliation.confirmReconcile')}
              </label>
            </VStack>
          </ModalBody>
          <ModalFooter>
            <Button variant="ghost" mr={3} onClick={() => setPendingAction(null)} isDisabled={Boolean(workingCaseId)}>{t('configuration.orderReconciliation.cancel')}</Button>
            <Button colorScheme={pendingAction?.kind === 'release' ? 'red' : 'orange'} onClick={() => void confirmAction()} isDisabled={!confirmationChecked || Boolean(workingCaseId)} isLoading={Boolean(workingCaseId)}>
              {t(pendingAction?.kind === 'release' ? 'configuration.orderReconciliation.confirmRelease' : 'configuration.orderReconciliation.confirmReconcile')}
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>
    </ConfigCard>
  )
}
