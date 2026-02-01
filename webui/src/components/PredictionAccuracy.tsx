import React, { useEffect, useState } from 'react'
import {
  Box,
  Container,
  Heading,
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
  FormControl,
  FormLabel,
  Select,
} from '@chakra-ui/react'
import { Link as RouterLink } from 'react-router-dom'
import { getPredictionsAccuracy, getPredictionsHistory, PredictionHistoryItem } from '../services/api'

const ASSET_OPTIONS = [
  { value: '', label: '全部' },
  { value: 'crypto_btc', label: 'BTC' },
  { value: 'commodity_gold', label: '黃金' },
]

const PredictionAccuracy: React.FC = () => {
  const [assetType, setAssetType] = useState('')
  const [accuracy, setAccuracy] = useState<{ total: number; correct: number; accuracy: number } | null>(null)
  const [history, setHistory] = useState<PredictionHistoryItem[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [sinceDays, setSinceDays] = useState(7)

  const fetchData = async () => {
    try {
      setLoading(true)
      const [accRes, histRes] = await Promise.all([
        getPredictionsAccuracy(assetType || undefined, sinceDays),
        getPredictionsHistory({
          asset_type: assetType || undefined,
          limit: 50,
          offset: 0,
        }),
      ])
      setAccuracy(accRes ? { total: accRes.total, correct: accRes.correct, accuracy: accRes.accuracy } : null)
      setHistory(histRes.items || [])
      setTotal(histRes.total || 0)
    } catch (err) {
      console.error(err)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchData()
  }, [assetType, sinceDays])

  return (
    <Container maxW="container.xl" py={8}>
      <VStack align="stretch" spacing={6}>
        <HStack justify="space-between">
          <Heading size="lg">預测准确率</Heading>
          <Button as={RouterLink} to="/news-analysis" size="sm" variant="outline">
            返回新聞分析
          </Button>
        </HStack>

        <HStack>
          <FormControl w="150px">
            <FormLabel fontSize="sm">资產</FormLabel>
            <Select size="sm" value={assetType} onChange={(e) => setAssetType(e.target.value)}>
              {ASSET_OPTIONS.map((a) => (
                <option key={a.value || 'all'} value={a.value}>{a.label}</option>
              ))}
            </Select>
          </FormControl>
          <FormControl w="120px">
            <FormLabel fontSize="sm">统计周期</FormLabel>
            <Select size="sm" value={String(sinceDays)} onChange={(e) => setSinceDays(Number(e.target.value))}>
              <option value="7">近7天</option>
              <option value="14">近14天</option>
              <option value="30">近30天</option>
            </Select>
          </FormControl>
        </HStack>

        {loading ? (
          <Box py={10} textAlign="center">
            <Spinner size="lg" />
          </Box>
        ) : (
          <>
            {accuracy && (
              <HStack spacing={6} p={4} bg="gray.50" borderRadius="lg">
                <Text>總預测次數: <strong>{accuracy.total}</strong></Text>
                <Text>正确次數: <strong>{accuracy.correct}</strong></Text>
                <Text>准确率: <strong>{accuracy.accuracy.toFixed(1)}%</strong></Text>
              </HStack>
            )}

            <Box>
              <Text fontWeight="600" mb={2}>最近預测驗证記錄</Text>
              <Table size="sm">
                <Thead>
                  <Tr>
                    <Th>預测時间</Th>
                    <Th>币种</Th>
                    <Th>時间窗口</Th>
                    <Th>預测方向</Th>
                    <Th>實際方向</Th>
                    <Th>結果</Th>
                  </Tr>
                </Thead>
                <Tbody>
                  {history.length === 0 ? (
                    <Tr><Td colSpan={6} color="gray.500">暂無驗证記錄</Td></Tr>
                  ) : (
                    history.map((row) => (
                      <Tr key={row.id}>
                        <Td>{new Date(row.prediction_time).toLocaleString()}</Td>
                        <Td>{row.symbol}</Td>
                        <Td>{row.timeframe}</Td>
                        <Td>{row.predicted_direction}</Td>
                        <Td>{row.actual_direction}</Td>
                        <Td>
                          {row.status === 'verified' ? (
                            <Badge colorScheme={row.is_correct ? 'green' : 'red'}>
                              {row.is_correct ? '正确' : '錯误'}
                            </Badge>
                          ) : (
                            <Badge colorScheme="gray">{row.status}</Badge>
                          )}
                        </Td>
                      </Tr>
                    ))
                  )}
                </Tbody>
              </Table>
            </Box>
          </>
        )}
      </VStack>
    </Container>
  )
}

export default PredictionAccuracy
