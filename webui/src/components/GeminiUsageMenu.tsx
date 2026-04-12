import React, { useCallback, useState } from 'react'
import {
  Button,
  Menu,
  MenuButton,
  MenuList,
  Box,
  Text,
  Table,
  Thead,
  Tbody,
  Tr,
  Th,
  Td,
  Spinner,
  useColorModeValue,
} from '@chakra-ui/react'
import { ChevronDownIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import { getGeminiUsageLog, type GeminiUsageEntry, type GeminiUsageResponse } from '../services/api'

function formatTime(iso: string, locale: string): string {
  try {
    const d = new Date(iso)
    if (Number.isNaN(d.getTime())) return iso
    return d.toLocaleString(locale.replace('_', '-'))
  } catch {
    return iso
  }
}

const GeminiUsageMenu: React.FC = () => {
  const { t, i18n } = useTranslation()
  const [data, setData] = useState<GeminiUsageResponse | null>(null)
  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState<string | null>(null)

  const headBg = useColorModeValue('gray.50', 'gray.700')

  const load = useCallback(async () => {
    setLoading(true)
    setErr(null)
    try {
      const res = await getGeminiUsageLog()
      setData(res)
    } catch {
      setErr(t('geminiUsage.loadFailed'))
      setData(null)
    } finally {
      setLoading(false)
    }
  }, [t])

  const onOpen = () => {
    void load()
  }

  const entries: GeminiUsageEntry[] = data?.entries ?? []
  const sum = data?.summary

  return (
    <Menu onOpen={onOpen} placement="bottom-end" closeOnSelect={false}>
      <MenuButton
        as={Button}
        size="xs"
        variant="ghost"
        fontWeight="600"
        borderRadius="full"
        rightIcon={<ChevronDownIcon />}
        aria-label={t('geminiUsage.menuButton')}
      >
        {t('geminiUsage.menuButton')}
      </MenuButton>
      <MenuList
        p={0}
        minW={{ base: 'min(100vw - 24px, 520px)', md: '520px' }}
        maxW="min(100vw - 24px, 640px)"
      >
        <Box px={3} py={2} borderBottomWidth="1px">
          <Text fontSize="sm" fontWeight="700">
            {t('geminiUsage.title')}
          </Text>
          {sum != null && (
            <Text fontSize="xs" color="gray.500" mt={1}>
              {t('geminiUsage.summary', {
                count: sum.call_count,
                inTok: sum.total_input_tokens,
                outTok: sum.total_output_tokens,
              })}
            </Text>
          )}
        </Box>
        <Box maxH="min(60vh, 360px)" overflowY="auto" px={0}>
          {loading && (
            <Box py={8} textAlign="center">
              <Spinner size="sm" />
            </Box>
          )}
          {!loading && err != null && (
            <Text fontSize="sm" color="red.500" px={3} py={4}>
              {err}
            </Text>
          )}
          {!loading && err == null && entries.length === 0 && (
            <Text fontSize="sm" color="gray.500" px={3} py={4}>
              {t('geminiUsage.empty')}
            </Text>
          )}
          {!loading && err == null && entries.length > 0 && (
            <Table size="sm" variant="simple">
              <Thead position="sticky" top={0} bg={headBg} zIndex={1}>
                <Tr>
                  <Th>{t('geminiUsage.time')}</Th>
                  <Th>{t('geminiUsage.model')}</Th>
                  <Th display={{ base: 'none', sm: 'table-cell' }}>{t('geminiUsage.source')}</Th>
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
                    <Td fontSize="xs" maxW="120px" isTruncated title={row.model}>
                      {row.model || '—'}
                    </Td>
                    <Td display={{ base: 'none', sm: 'table-cell' }} fontSize="xs" maxW="100px" isTruncated title={row.source}>
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
          )}
        </Box>
      </MenuList>
    </Menu>
  )
}

export default GeminiUsageMenu
