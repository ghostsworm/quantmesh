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
import { useTranslation } from 'react-i18next'
import { getPredictionsAccuracy, getPredictionsHistory, PredictionHistoryItem } from '../services/api'

const PredictionAccuracy: React.FC = () => {
  const { t } = useTranslation()
  const [assetType, setAssetType] = useState('')
  const [accuracy, setAccuracy] = useState<{ total: number; correct: number; accuracy: number } | null>(null)
  const [history, setHistory] = useState<PredictionHistoryItem[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [sinceDays, setSinceDays] = useState(7)

  const ASSET_OPTIONS = [
    { value: '', label: t('predictionAccuracy.all') },
    { value: 'crypto_btc', label: 'BTC' },
    { value: 'commodity_gold', label: t('predictionAccuracy.gold') },
  ]

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
          <Heading size="lg">{t('predictionAccuracy.title')}</Heading>
          <Button as={RouterLink} to="/news-analysis" size="sm" variant="outline">
            {t('predictionAccuracy.backToNewsAnalysis')}
          </Button>
        </HStack>

        <HStack>
          <FormControl w="150px">
            <FormLabel fontSize="sm">{t('predictionAccuracy.asset')}</FormLabel>
            <Select size="sm" value={assetType} onChange={(e) => setAssetType(e.target.value)}>
              {ASSET_OPTIONS.map((a) => (
                <option key={a.value || 'all'} value={a.value}>{a.label}</option>
              ))}
            </Select>
          </FormControl>
          <FormControl w="120px">
            <FormLabel fontSize="sm">{t('predictionAccuracy.period')}</FormLabel>
            <Select size="sm" value={String(sinceDays)} onChange={(e) => setSinceDays(Number(e.target.value))}>
              <option value="7">{t('predictionAccuracy.last7d')}</option>
              <option value="14">{t('predictionAccuracy.last14d')}</option>
              <option value="30">{t('predictionAccuracy.last30d')}</option>
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
                <Text>{t('predictionAccuracy.totalPredictions')}: <strong>{accuracy.total}</strong></Text>
                <Text>{t('predictionAccuracy.correctCount')}: <strong>{accuracy.correct}</strong></Text>
                <Text>{t('predictionAccuracy.accuracy')}: <strong>{accuracy.accuracy.toFixed(1)}%</strong></Text>
              </HStack>
            )}

            <Box>
              <Text fontWeight="600" mb={2}>{t('predictionAccuracy.recentRecords')}</Text>
              <Table size="sm">
                <Thead>
                  <Tr>
                    <Th>{t('predictionAccuracy.predictionTime')}</Th>
                    <Th>{t('predictionAccuracy.symbol')}</Th>
                    <Th>{t('predictionAccuracy.timeWindow')}</Th>
                    <Th>{t('predictionAccuracy.predictedDirection')}</Th>
                    <Th>{t('predictionAccuracy.actualDirection')}</Th>
                    <Th>{t('predictionAccuracy.result')}</Th>
                  </Tr>
                </Thead>
                <Tbody>
                  {history.length === 0 ? (
                    <Tr><Td colSpan={6} color="gray.500">{t('predictionAccuracy.noRecords')}</Td></Tr>
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
                              {row.is_correct ? t('predictionAccuracy.correct') : t('predictionAccuracy.incorrect')}
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
