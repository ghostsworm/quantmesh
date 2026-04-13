import React, { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Box,
  Button,
  Heading,
  HStack,
  Input,
  Spinner,
  Table,
  Tbody,
  Td,
  Text,
  Th,
  Thead,
  Tr,
  Badge,
  useColorModeValue,
} from '@chakra-ui/react'
import { useTranslation } from 'react-i18next'
import { getGeminiUsageLog, type GeminiUsageEntry, type GeminiUsageResponse } from '../services/api'

const PAGE_SIZE = 50

function formatTime(iso: string, locale: string): string {
  try {
    const d = new Date(iso)
    if (Number.isNaN(d.getTime())) return iso
    return d.toLocaleString(locale.replace('_', '-'))
  } catch {
    return iso
  }
}

/** datetime-local 轉 RFC3339；空則 undefined */
function localInputToRFC3339(v: string): string | undefined {
  if (!v.trim()) return undefined
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return undefined
  return d.toISOString()
}

const GeminiUsagePage: React.FC = () => {
  const { t, i18n } = useTranslation()
  const [data, setData] = useState<GeminiUsageResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState<string | null>(null)
  const [offset, setOffset] = useState(0)
  const [filterNonce, setFilterNonce] = useState(0)
  const [startLocal, setStartLocal] = useState('')
  const [endLocal, setEndLocal] = useState('')

  const headBg = useColorModeValue('gray.50', 'gray.700')
  const borderColor = useColorModeValue('gray.200', 'gray.600')

  const load = useCallback(async () => {
    setLoading(true)
    setErr(null)
    try {
      const startTime = localInputToRFC3339(startLocal)
      const endTime = localInputToRFC3339(endLocal)
      const res = await getGeminiUsageLog({
        limit: PAGE_SIZE,
        offset,
        startTime,
        endTime,
      })
      setData(res)
    } catch {
      setErr(t('geminiUsage.loadFailed'))
      setData(null)
    } finally {
      setLoading(false)
    }
  }, [offset, startLocal, endLocal, t])

  useEffect(() => {
    void load()
  }, [load, filterNonce])

  const entries: GeminiUsageEntry[] = data?.entries ?? []
  const sum = data?.summary
  const total = data?.total ?? 0
  const source = data?.source

  const hasPrev = offset > 0
  const hasNext = useMemo(() => offset + entries.length < total, [offset, entries.length, total])

  const onApplyFilter = () => {
    setOffset(0)
    setFilterNonce((n) => n + 1)
  }

  const onClearFilter = () => {
    setStartLocal('')
    setEndLocal('')
    setOffset(0)
    setFilterNonce((n) => n + 1)
  }

  return (
    <Box>
      <Heading size="lg" mb={1}>
        {t('geminiUsage.pageTitle')}
      </Heading>
      <Text fontSize="sm" color="gray.600" mb={4}>
        {t('geminiUsage.pageSubtitle')}
      </Text>

      {source != null && (
        <Badge colorScheme={source === 'database' ? 'green' : 'orange'} mb={3}>
          {source === 'database' ? t('geminiUsage.sourceDatabase') : t('geminiUsage.sourceMemory')}
        </Badge>
      )}

      {sum != null && (
        <Text fontSize="sm" mb={4}>
          {t('geminiUsage.summary', {
            count: sum.call_count,
            inTok: sum.total_input_tokens,
            outTok: sum.total_output_tokens,
          })}
          {total > 0 && (
            <Text as="span" ml={2} color="gray.500">
              {t('geminiUsage.totalRows', { total })}
            </Text>
          )}
        </Text>
      )}

      <HStack
        flexWrap="wrap"
        gap={2}
        mb={4}
        align="flex-end"
        p={3}
        borderWidth="1px"
        borderRadius="md"
        borderColor={borderColor}
      >
        <Box>
          <Text fontSize="xs" mb={1}>
            {t('geminiUsage.filterStart')}
          </Text>
          <Input
            type="datetime-local"
            size="sm"
            value={startLocal}
            onChange={(e) => setStartLocal(e.target.value)}
            maxW="220px"
          />
        </Box>
        <Box>
          <Text fontSize="xs" mb={1}>
            {t('geminiUsage.filterEnd')}
          </Text>
          <Input
            type="datetime-local"
            size="sm"
            value={endLocal}
            onChange={(e) => setEndLocal(e.target.value)}
            maxW="220px"
          />
        </Box>
        <Button size="sm" colorScheme="blue" onClick={onApplyFilter}>
          {t('geminiUsage.applyFilter')}
        </Button>
        <Button size="sm" variant="outline" onClick={onClearFilter}>
          {t('geminiUsage.clearFilter')}
        </Button>
      </HStack>

      {loading && (
        <Box py={12} textAlign="center">
          <Spinner />
        </Box>
      )}

      {!loading && err != null && (
        <Text color="red.500" mb={4}>
          {err}
        </Text>
      )}

      {!loading && err == null && entries.length === 0 && (
        <Text color="gray.500" py={8}>
          {t('geminiUsage.empty')}
        </Text>
      )}

      {!loading && err == null && entries.length > 0 && (
        <>
          <Box overflowX="auto">
            <Table size="sm" variant="simple">
              <Thead bg={headBg}>
                <Tr>
                  <Th>{t('geminiUsage.time')}</Th>
                  <Th>{t('geminiUsage.model')}</Th>
                  <Th display={{ base: 'none', md: 'table-cell' }}>{t('geminiUsage.source')}</Th>
                  <Th isNumeric>{t('geminiUsage.inputTokens')}</Th>
                  <Th isNumeric>{t('geminiUsage.outputTokens')}</Th>
                  <Th isNumeric>{t('geminiUsage.durationMs')}</Th>
                </Tr>
              </Thead>
              <Tbody>
                {entries.map((row, idx) => (
                  <Tr key={`${row.at}-${idx}`}>
                    <Td whiteSpace="nowrap" fontSize="xs">
                      {formatTime(row.at, i18n.language)}
                    </Td>
                    <Td fontSize="xs" maxW="140px" isTruncated title={row.model}>
                      {row.model || '—'}
                    </Td>
                    <Td display={{ base: 'none', md: 'table-cell' }} fontSize="xs" maxW="120px" isTruncated title={row.source}>
                      {row.source || '—'}
                    </Td>
                    <Td isNumeric fontSize="xs">
                      {row.input_tokens}
                    </Td>
                    <Td isNumeric fontSize="xs">
                      {row.output_tokens}
                    </Td>
                    <Td isNumeric fontSize="xs">
                      {row.duration_ms}
                    </Td>
                  </Tr>
                ))}
              </Tbody>
            </Table>
          </Box>

          <HStack justify="space-between" mt={4} flexWrap="wrap" gap={2}>
            <Button
              size="sm"
              disabled={!hasPrev}
              onClick={() => setOffset((o) => Math.max(0, o - PAGE_SIZE))}
            >
              {t('geminiUsage.prevPage')}
            </Button>
            <Text fontSize="sm" color="gray.600">
              {t('geminiUsage.pageInfo', {
                from: total === 0 ? 0 : offset + 1,
                to: offset + entries.length,
                total,
              })}
            </Text>
            <Button size="sm" disabled={!hasNext} onClick={() => setOffset((o) => o + PAGE_SIZE)}>
              {t('geminiUsage.nextPage')}
            </Button>
          </HStack>
        </>
      )}
    </Box>
  )
}

export default GeminiUsagePage
