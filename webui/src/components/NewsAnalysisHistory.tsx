import React, { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  Box,
  Button,
  VStack,
  HStack,
  Text,
  Badge,
  Spinner,
  Table,
  Thead,
  Tbody,
  Tr,
  Th,
  Td,
  Link as ChakraLink,
  Modal,
  ModalOverlay,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalCloseButton,
  useDisclosure,
  FormControl,
  FormLabel,
  Select,
  Code,
} from '@chakra-ui/react'
import { getNewsHistory, getNewsHistoryById, NewsHistoryItem } from '../services/api'

const REC_COLORS: Record<string, string> = {
  normal: 'green',
  caution: 'yellow',
  reduce_position: 'orange',
  stop_trading: 'red',
}

const NewsAnalysisHistory: React.FC = () => {
  const { t } = useTranslation()
  const [items, setItems] = useState<NewsHistoryItem[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [detail, setDetail] = useState<Record<string, unknown> | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const [symbol, setSymbol] = useState('')
  const [page, setPage] = useState(0)
  const limit = 20
  const { isOpen, onOpen, onClose } = useDisclosure()

  const fetchHistory = async () => {
    try {
      setLoading(true)
      const end = new Date()
      const start = new Date()
      start.setDate(start.getDate() - 7)
      const res = await getNewsHistory({
        symbol: symbol || undefined,
        start_time: start.toISOString(),
        end_time: end.toISOString(),
        limit,
        offset: page * limit,
      })
      setItems(res.items || [])
      setTotal(res.total || 0)
    } catch (err) {
      console.error(err)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchHistory()
  }, [symbol, page])

  const openDetail = async (id: number) => {
    setDetailLoading(true)
    onOpen()
    try {
      const d = await getNewsHistoryById(id)
      setDetail(d)
    } catch (err) {
      console.error(err)
      setDetail(null)
    } finally {
      setDetailLoading(false)
    }
  }

  const totalPages = Math.ceil(total / limit)

  return (
    <VStack align="stretch" spacing={6}>
      <HStack>
          <FormControl w="200px">
            <FormLabel fontSize="sm">{t('newsAnalysis.symbol')}</FormLabel>
            <Select
              size="sm"
              value={symbol}
              onChange={(e) => { setSymbol(e.target.value); setPage(0) }}
            >
              <option value="">{t('newsAnalysis.all')}</option>
              <option value="BTCUSDT">BTCUSDT</option>
              <option value="ETHUSDT">ETHUSDT</option>
            </Select>
          </FormControl>
        </HStack>

        {loading ? (
          <Box py={10} textAlign="center">
            <Spinner size="lg" />
          </Box>
        ) : (
          <>
            <Table size="sm">
              <Thead>
                <Tr>
                  <Th>{t('newsAnalysis.time')}</Th>
                  <Th>{t('newsAnalysis.symbol')}</Th>
                  <Th>{t('newsAnalysis.price')}</Th>
                  <Th>{t('newsAnalysis.recommendation')}</Th>
                  <Th>{t('newsAnalysis.action')}</Th>
                </Tr>
              </Thead>
              <Tbody>
                {items.map((row) => (
                  <Tr key={row.id}>
                    <Td>{new Date(row.analysis_time).toLocaleString()}</Td>
                    <Td>{row.symbol}</Td>
                    <Td>${row.current_price?.toLocaleString()}</Td>
                    <Td>
                      <Badge colorScheme={REC_COLORS[row.recommendation] || 'gray'}>
                        {row.recommendation}
                      </Badge>
                    </Td>
                    <Td>
                      <ChakraLink as="button" color="blue.600" onClick={() => openDetail(row.id)} fontSize="sm">
                        {t('newsAnalysis.details')}
                      </ChakraLink>
                    </Td>
                  </Tr>
                ))}
              </Tbody>
            </Table>

            {totalPages > 1 && (
              <HStack>
                <Button size="sm" isDisabled={page === 0} onClick={() => setPage(p => Math.max(0, p - 1))}>
                  {t('newsAnalysis.prevPage')}
                </Button>
                <Text fontSize="sm">{page + 1} / {totalPages}</Text>
                <Button size="sm" isDisabled={page >= totalPages - 1} onClick={() => setPage(p => p + 1)}>
                  {t('newsAnalysis.nextPage')}
                </Button>
              </HStack>
            )}
          </>
        )}

      <Modal isOpen={isOpen} onClose={onClose} size="4xl">
        <ModalOverlay />
        <ModalContent>
          <ModalHeader>{t('newsAnalysis.analysisDetail')}</ModalHeader>
          <ModalCloseButton />
          <ModalBody pb={6}>
            {detailLoading ? (
              <Spinner />
            ) : detail ? (
              <VStack align="stretch" spacing={4}>
                {detail.assessment && (
                  <Box>
                    <Text fontWeight="600" mb={2}>{t('newsAnalysis.evaluationResult')}</Text>
                    <Code as="pre" p={4} display="block" overflow="auto" maxH="400px" fontSize="xs" whiteSpace="pre-wrap">
                      {JSON.stringify(detail.assessment, null, 2)}
                    </Code>
                  </Box>
                )}
                {detail.recent_news_summary && (
                  <Box>
                    <Text fontWeight="600" mb={2}>{t('newsAnalysis.newsSummary')}</Text>
                    <Code as="pre" p={4} display="block" overflow="auto" maxH="200px" fontSize="xs" whiteSpace="pre-wrap">
                      {(detail.recent_news_summary as string).slice(0, 2000)}
                    </Code>
                  </Box>
                )}
              </VStack>
            ) : (
              <Text>{t('aiAnalysis.noData')}</Text>
            )}
          </ModalBody>
        </ModalContent>
      </Modal>
    </VStack>
  )
}

export default NewsAnalysisHistory
