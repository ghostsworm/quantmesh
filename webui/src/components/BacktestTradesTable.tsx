import React, { useEffect, useState } from 'react'
import {
  Box,
  Table,
  Thead,
  Tbody,
  Tr,
  Th,
  Td,
  Spinner,
  Text,
  Select,
  HStack,
  Badge,
  Flex,
  Button,
  useDisclosure,
  Modal,
  ModalOverlay,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalFooter,
  ModalCloseButton,
  VStack,
} from '@chakra-ui/react'
import { useTranslation } from 'react-i18next'
import { getBacktestTaskTrades, type BacktestTradeRow } from '../services/backtest'

interface BacktestTradesTableProps {
  taskId: string
  isOpen: boolean
  onClose: () => void
}

const BacktestTradesTable: React.FC<BacktestTradesTableProps> = ({ taskId, isOpen, onClose }) => {
  const { t } = useTranslation()
  const [trades, setTrades] = useState<BacktestTradeRow[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [limit, setLimit] = useState(500)
  const [total, setTotal] = useState(0)

  const loadTrades = async () => {
    if (!taskId) return
    setLoading(true)
    setError(null)
    try {
      const response = await getBacktestTaskTrades(taskId, limit)
      if (response.success && response.data) {
        setTrades(response.data.trades)
        setTotal(response.data.total)
      } else {
        setError(t('backtest.loadTradesFailed'))
      }
    } catch (err) {
      setError(String(err))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    if (isOpen && taskId) {
      loadTrades()
    }
  }, [isOpen, taskId, limit])

  return (
    <Modal isOpen={isOpen} onClose={onClose} size="full">
      <ModalOverlay />
      <ModalContent maxW="90vw" h="90vh">
        <ModalHeader>
          <HStack spacing={4}>
            <Text>{t('backtest.tradeRecords')}</Text>
            <Text fontSize="sm" color="gray.500">
              {t('backtest.totalTradesCount', { count: total })}
            </Text>
          </HStack>
        </ModalHeader>
        <ModalCloseButton />
        <ModalBody pb={6}>
          <VStack spacing={4} align="stretch">
            <HStack justify="space-between">
              <HStack>
                <Text fontSize="sm">{t('backtest.displayLimit')}:</Text>
                <Select
                  size="sm"
                  width="150px"
                  value={String(limit)}
                  onChange={(e) => setLimit(Number(e.target.value))}
                >
                  <option value="100">100</option>
                  <option value="500">500</option>
                  <option value="1000">1000</option>
                  <option value="2000">2000</option>
                  <option value="5000">5000</option>
                  <option value="10000">10000</option>
                </Select>
                {total > limit && (
                  <Text fontSize="sm" color="orange.500">
                    {t('backtest.showingLatest', { count: limit })}
                  </Text>
                )}
              </HStack>
              <Button size="sm" onClick={loadTrades} isLoading={loading}>
                {t('common.refresh')}
              </Button>
            </HStack>

            {loading ? (
              <Flex justify="center" py={10}>
                <Spinner size="lg" />
              </Flex>
            ) : error ? (
              <Box p={4} bg="red.50" borderRadius="md">
                <Text color="red.500">{error}</Text>
              </Box>
            ) : trades.length === 0 ? (
              <Box p={4} bg="gray.50" borderRadius="md" textAlign="center">
                <Text color="gray.500">{t('backtest.noTrades')}</Text>
              </Box>
            ) : (
              <Box overflowX="auto" overflowY="auto" maxH="calc(90vh - 200px)">
                <Table size="sm">
                  <Thead position="sticky" top={0} bg="white" _dark={{ bg: 'gray.800' }} zIndex={1}>
                    <Tr>
                      <Th>{t('backtest.timestamp')}</Th>
                      <Th>{t('backtest.type')}</Th>
                      <Th isNumeric>{t('backtest.price')}</Th>
                      <Th isNumeric>{t('backtest.quantity')}</Th>
                      <Th isNumeric>{t('backtest.fee')}</Th>
                      <Th isNumeric>{t('backtest.pnl')}</Th>
                    </Tr>
                  </Thead>
                  <Tbody>
                    {trades.map((trade, index) => (
                      <Tr key={index}>
                        <Td fontSize="xs">{trade.timestamp}</Td>
                        <Td>
                          <Badge
                            colorScheme={trade.type === 'buy' || trade.type === 'BUY' ? 'green' : 'red'}
                            fontSize="xs"
                          >
                            {trade.type.toUpperCase()}
                          </Badge>
                        </Td>
                        <Td isNumeric fontSize="xs">
                          {trade.price.toFixed(4)}
                        </Td>
                        <Td isNumeric fontSize="xs">
                          {trade.quantity.toFixed(6)}
                        </Td>
                        <Td isNumeric fontSize="xs">
                          {trade.fee.toFixed(4)}
                        </Td>
                        <Td isNumeric fontSize="xs" color={trade.pnl >= 0 ? 'green.500' : 'red.500'}>
                          {trade.pnl >= 0 ? '+' : ''}{trade.pnl.toFixed(4)}
                        </Td>
                      </Tr>
                    ))}
                  </Tbody>
                </Table>
              </Box>
            )}
          </VStack>
        </ModalBody>
        <ModalFooter>
          <Button onClick={onClose}>{t('common.close')}</Button>
        </ModalFooter>
      </ModalContent>
    </Modal>
  )
}

export default BacktestTradesTable
