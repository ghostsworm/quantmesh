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
  const [accuracy, setAccuracy] = useState<PredictionAccuracyResponse | null>(null)
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
      setAccuracy(accRes || null)
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
              <VStack align="stretch" spacing={4}>
                <HStack spacing={6} p={4} bg="gray.50" borderRadius="lg">
                  <Text>{t('predictionAccuracy.totalPredictions')}: <strong>{accuracy.total}</strong></Text>
                  <Text>{t('predictionAccuracy.correctCount')}: <strong>{accuracy.correct}</strong></Text>
                  <Text>{t('predictionAccuracy.accuracy')}: <strong>{accuracy.accuracy.toFixed(1)}%</strong></Text>
                </HStack>

                {accuracy.timeframe_breakdown && Object.keys(accuracy.timeframe_breakdown).length > 0 && (
                  <VStack align="stretch" spacing={4}>
                    {Object.entries(accuracy.timeframe_breakdown)
                      .sort(([a], [b]) => a.localeCompare(b, undefined, { numeric: true }))
                      .map(([tf, stats]) => (
                        <Box key={tf} p={4} border="1px" borderColor="gray.200" borderRadius="lg" bg="white">
                          <HStack justify="space-between" mb={3}>
                            <Text fontWeight="bold" fontSize="md">{tf} {t('predictionAccuracy.timeWindow')}</Text>
                            <Badge colorScheme="blue" fontSize="sm">
                              {t('predictionAccuracy.accuracy')}: {stats.accuracy.toFixed(1)}% ({stats.correct}/{stats.total})
                            </Badge>
                          </HStack>
                          
                          {stats.directions && Object.keys(stats.directions).length > 0 && (
                            <HStack spacing={4} wrap="wrap">
                              {['up', 'down', 'stable'].map(dir => {
                                const dStat = stats.directions?.[dir];
                                if (!dStat) return null;
                                
                                let dirLabel = dir;
                                let color = 'gray';
                                if (dir === 'up') {
                                  dirLabel = t('predictionAccuracy.up') || '涨';
                                  color = 'green';
                                } else if (dir === 'down') {
                                  dirLabel = t('predictionAccuracy.down') || '跌';
                                  color = 'red';
                                } else if (dir === 'stable') {
                                  dirLabel = t('predictionAccuracy.stable') || '平';
                                  color = 'blue';
                                }

                                return (
                                  <Box key={dir} p={2} border="1px" borderColor={`${color}.100`} borderRadius="md" bg={`${color}.50`} minW="120px">
                                    <Text fontSize="xs" color={`${color}.600`} fontWeight="600">{dirLabel}</Text>
                                    <HStack justify="space-between" mt={1}>
                                      <Text fontSize="sm" fontWeight="bold">{dStat.accuracy.toFixed(1)}%</Text>
                                      <Text fontSize="xs" color="gray.500">{dStat.correct}/{dStat.total}</Text>
                                    </HStack>
                                  </Box>
                                );
                              })}
                            </HStack>
                          )}
                        </Box>
                      ))}
                  </VStack>
                )}
              </VStack>
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
