import React, { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  Box,
  Center,
  Container,
  Heading,
  Button,
  VStack,
  HStack,
  Text,
  Badge,
  Spinner,
  Alert,
  AlertTitle,
  FormControl,
  FormLabel,
  Input,
  Card,
  CardHeader,
  CardBody,
  SimpleGrid,
  Progress,
  Table,
  Thead,
  Tbody,
  Tr,
  Th,
  Td,
  Link,
  useToast,
  Tabs,
  TabList,
  TabPanels,
  Tab,
  TabPanel,
} from '@chakra-ui/react'
import { RepeatIcon } from '@chakra-ui/icons'
import {
  getNewsAnalysis,
  getNewsCollected,
  triggerNewsAnalyze,
  getPredictionsAccuracy,
  getNewsHistory,
  NewsRiskAssessment,
  NewsItem,
  NewsHistoryItem,
} from '../services/api'
import NewsTrendChart from './NewsTrendChart'
import NewsAnalysisHistory from './NewsAnalysisHistory'
import PredictionAccuracy from './PredictionAccuracy'

const NewsAnalysis: React.FC = () => {
  const { t } = useTranslation()

  const ASSET_OPTIONS = [
    { value: 'crypto_btc', label: 'BTC', symbol: 'BTCUSDT' },
    { value: 'commodity_gold', label: t('newsAnalysis.goldPaxg'), symbol: 'PAXGUSDT' },
  ]

  const REC_LABELS: Record<string, { label: string; color: string }> = {
    normal: { label: t('newsAnalysis.recNormal'), color: 'green' },
    caution: { label: t('newsAnalysis.recCaution'), color: 'yellow' },
    reduce_position: { label: t('newsAnalysis.recReducePosition'), color: 'orange' },
    stop_trading: { label: t('newsAnalysis.recStopTrading'), color: 'red' },
  }

  const [assetType, setAssetType] = useState('crypto_btc')
  const [assessment, setAssessment] = useState<NewsRiskAssessment | null>(null)
  const [isAnalyzing, setIsAnalyzing] = useState(false)
  const [news, setNews] = useState<NewsItem[]>([])
  const [accuracy, setAccuracy] = useState<{ total: number; correct: number; accuracy: number } | null>(null)
  const [loading, setLoading] = useState(true)
  const [triggering, setTriggering] = useState(false)
  const [focusEvent, setFocusEvent] = useState('')
  const [historyData, setHistoryData] = useState<NewsHistoryItem[]>([])
  const [historyLoading, setHistoryLoading] = useState(false)
  const [historyPeriod, setHistoryPeriod] = useState('7')
  const toast = useToast()

  const currentAsset = ASSET_OPTIONS.find((a) => a.value === assetType) || ASSET_OPTIONS[0]

  const fetchData = async () => {
    try {
      setLoading(true)
      const [analysisRes, collectedRes, accRes] = await Promise.all([
        getNewsAnalysis(assetType),
        getNewsCollected(),
        getPredictionsAccuracy(assetType, 7),
      ])
      setAssessment(analysisRes.assessment || null)
      setIsAnalyzing(analysisRes.is_analyzing)
      setNews(collectedRes.news || [])
      setAccuracy(accRes ? { total: accRes.total, correct: accRes.correct, accuracy: accRes.accuracy } : null)
    } catch (err) {
      console.error(err)
      toast({ title: t('newsAnalysis.fetchDataFailed'), status: 'error', duration: 3000 })
    } finally {
      setLoading(false)
    }
  }

  const fetchHistory = async () => {
    try {
      setHistoryLoading(true)
      const end = new Date()
      const start = new Date()
      start.setDate(start.getDate() - parseInt(historyPeriod))
      const res = await getNewsHistory({
        symbol: currentAsset.symbol,
        start_time: start.toISOString(),
        end_time: end.toISOString(),
        limit: 100, // 获取足够多的点来画图
      })
      setHistoryData(res.items || [])
    } catch (err) {
      console.error('Fetch history failed:', err)
    } finally {
      setHistoryLoading(false)
    }
  }

  useEffect(() => {
    fetchData()
    const interval = setInterval(fetchData, 30000)
    return () => clearInterval(interval)
  }, [assetType])

  useEffect(() => {
    fetchHistory()
  }, [assetType, historyPeriod])

  const handleTrigger = async () => {
    try {
      setTriggering(true)
      await triggerNewsAnalyze(currentAsset.symbol, focusEvent || undefined, assetType)
      toast({ title: t('newsAnalysis.analysisSubmitted'), status: 'success', duration: 2000 })
      setTimeout(fetchData, 2000)
    } catch (err) {
      toast({ title: (err as Error).message || t('newsAnalysis.triggerFailed'), status: 'error', duration: 3000 })
    } finally {
      setTriggering(false)
    }
  }

  if (loading && !assessment) {
    return (
      <Container maxW="container.xl" py={8}>
        <Center py={20}>
          <Spinner size="xl" />
        </Center>
      </Container>
    )
  }

  const rec = assessment?.recommendation || 'normal'
  const recInfo = REC_LABELS[rec] || REC_LABELS.normal

  return (
    <Container maxW="container.xl" py={8}>
      <VStack align="stretch" spacing={6}>
        <HStack justify="space-between" wrap="wrap" gap={4}>
          <HStack>
            <Heading size="lg">{t('newsAnalysis.title')}</Heading>
            <HStack spacing={1}>
              {ASSET_OPTIONS.map((a) => (
                <Button
                  key={a.value}
                  size="sm"
                  variant={assetType === a.value ? 'solid' : 'outline'}
                  colorScheme={assetType === a.value ? 'blue' : 'gray'}
                  onClick={() => setAssetType(a.value)}
                >
                  {a.label}
                </Button>
              ))}
            </HStack>
          </HStack>
          <HStack>
            <FormControl w="280px">
              <FormLabel fontSize="sm">{t('newsAnalysis.focusEventLabel')}</FormLabel>
              <Input
                placeholder={t('newsAnalysis.focusEventPlaceholder')}
                value={focusEvent}
                onChange={(e) => setFocusEvent(e.target.value)}
                size="sm"
              />
            </FormControl>
            <Button
              leftIcon={<RepeatIcon />}
              colorScheme="blue"
              isLoading={triggering || isAnalyzing}
              loadingText={isAnalyzing ? t('newsAnalysis.analyzing') : t('newsAnalysis.submitting')}
              onClick={handleTrigger}
              size="sm"
              mt={6}
            >
              {t('newsAnalysis.manualTrigger')}
            </Button>
            <Button variant="ghost" size="sm" onClick={fetchData} mt={6}>
              {t('common.refresh')}
            </Button>
          </HStack>
        </HStack>

        <Tabs variant="enclosed" colorScheme="blue">
          <TabList>
            <Tab fontWeight="600">{t('newsAnalysis.latestAnalysis')}</Tab>
            <Tab fontWeight="600">{t('newsAnalysis.history')}</Tab>
            <Tab fontWeight="600">{t('newsAnalysis.predictionAccuracy')}</Tab>
          </TabList>

          <TabPanels>
            <TabPanel px={0} py={6}>
              <VStack align="stretch" spacing={6}>
                {isAnalyzing && (
                  <Alert status="info">
                    <Spinner size="sm" mr={2} />
                    <AlertTitle>{t('newsAnalysis.geminiAnalyzing')}</AlertTitle>
                  </Alert>
                )}

                {accuracy !== null && accuracy.total > 0 && (
                  <Card>
                    <CardHeader py={3}>
                      <Text fontSize="sm" fontWeight="600">{t('newsAnalysis.predictionAccuracy7d')}</Text>
                    </CardHeader>
                    <CardBody pt={0}>
                      <HStack spacing={6}>
                        <Text>{t('newsAnalysis.total')} <strong>{accuracy.total}</strong> {t('newsAnalysis.times')}</Text>
                        <Text>{t('newsAnalysis.correct')} <strong>{accuracy.correct}</strong> {t('newsAnalysis.times')}</Text>
                        <Text>{t('newsAnalysis.accuracyRate')} <strong>{accuracy.accuracy.toFixed(1)}%</strong></Text>
                      </HStack>
                    </CardBody>
                  </Card>
                )}

                {assessment && (
                  <>
                    <SimpleGrid columns={{ base: 1, md: 3 }} spacing={4}>
                      <Card>
                        <CardHeader py={3}>
                          <Text fontSize="sm" color="gray.500">{t('newsAnalysis.recommendedAction')}</Text>
                        </CardHeader>
                        <CardBody pt={0}>
                          <Badge colorScheme={recInfo.color} fontSize="md" px={3} py={1}>
                            {recInfo.label}
                          </Badge>
                        </CardBody>
                      </Card>
                      <Card>
                        <CardHeader py={3}>
                          <Text fontSize="sm" color="gray.500">{t('newsAnalysis.riskScore')}</Text>
                        </CardHeader>
                        <CardBody pt={0}>
                          <Text fontSize="2xl" fontWeight="bold">{assessment.overall_risk_score.toFixed(1)}</Text>
                          <Progress value={assessment.overall_risk_score} colorScheme="red" size="sm" mt={2} borderRadius="full" />
                        </CardBody>
                      </Card>
                      <Card>
                        <CardHeader py={3}>
                          <Text fontSize="sm" color="gray.500">{t('newsAnalysis.crashProbability')}</Text>
                        </CardHeader>
                        <CardBody pt={0}>
                          <Text fontSize="2xl" fontWeight="bold">{(assessment.crash_probability * 100).toFixed(1)}%</Text>
                        </CardBody>
                      </Card>
                    </SimpleGrid>

                    <NewsTrendChart
                      data={historyData}
                      loading={historyLoading}
                      period={historyPeriod}
                      onPeriodChange={setHistoryPeriod}
                    />

                    {assessment.analysis_summary && (
                      <Card>
                        <CardHeader py={3}>
                          <Text fontSize="sm" fontWeight="600">{t('newsAnalysis.analysisSummary')}</Text>
                        </CardHeader>
                        <CardBody pt={0}>
                          <Text fontSize="sm" color="gray.600">{assessment.analysis_summary}</Text>
                        </CardBody>
                      </Card>
                    )}

                    {assessment.price_predictions && assessment.price_predictions.length > 0 && (
                      <Card>
                        <CardHeader py={3}>
                          <Text fontSize="sm" fontWeight="600">{t('newsAnalysis.pricePrediction')}</Text>
                        </CardHeader>
                        <CardBody pt={0}>
                          <SimpleGrid columns={{ base: 1, md: 2, lg: 3 }} spacing={4}>
                            {assessment.price_predictions.map((pred) => (
                              <Box key={pred.timeframe} p={3} bg="gray.50" borderRadius="lg">
                                <Text fontWeight="600" mb={2}>{pred.timeframe}</Text>
                                {pred.scenarios?.map((s, i) => (
                                  <HStack key={i} justify="space-between" fontSize="sm" mb={1}>
                                    <Text>{s.direction} {s.change_percent}%</Text>
                                    <Text fontWeight="600">{(s.probability * 100).toFixed(0)}%</Text>
                                  </HStack>
                                ))}
                              </Box>
                            ))}
                          </SimpleGrid>
                        </CardBody>
                      </Card>
                    )}

                    {assessment.risk_factors && assessment.risk_factors.length > 0 && (
                      <Card>
                        <CardHeader py={3}>
                          <Text fontSize="sm" fontWeight="600">{t('newsAnalysis.riskFactors')}</Text>
                        </CardHeader>
                        <CardBody pt={0}>
                          <HStack flexWrap="wrap" gap={2}>
                            {assessment.risk_factors.map((f, i) => (
                              <Badge key={i} colorScheme="orange" variant="subtle">{f}</Badge>
                            ))}
                          </HStack>
                        </CardBody>
                      </Card>
                    )}
                  </>
                )}

                <Card>
                  <CardHeader py={3}>
                    <Text fontSize="sm" fontWeight="600">{t('newsAnalysis.collectedNews24h')}</Text>
                  </CardHeader>
                  <CardBody pt={0}>
                    {news.length === 0 ? (
                      <Text color="gray.500" fontSize="sm">{t('newsAnalysis.noNewsHint')}</Text>
                    ) : (
                      <Table size="sm">
                        <Thead>
                          <Tr>
                            <Th>{t('newsAnalysis.time')}</Th>
                            <Th>{t('newsAnalysis.newsTitle')}</Th>
                            <Th>{t('newsAnalysis.source')}</Th>
                          </Tr>
                        </Thead>
                        <Tbody>
                          {news.slice(0, 20).map((n, i) => (
                            <Tr key={i}>
                              <Td fontSize="xs">{n.published_at ? new Date(n.published_at).toLocaleString() : '-'}</Td>
                              <Td maxW="400px" isTruncated>
                                {n.url ? (
                                  <Link href={n.url} isExternal color="blue.600">{n.title}</Link>
                                ) : n.title}
                              </Td>
                              <Td fontSize="xs">{n.source}</Td>
                            </Tr>
                          ))}
                        </Tbody>
                      </Table>
                    )}
                  </CardBody>
                </Card>
              </VStack>
            </TabPanel>
            <TabPanel px={0} py={6}>
              <NewsAnalysisHistory />
            </TabPanel>
            <TabPanel px={0} py={6}>
              <PredictionAccuracy />
            </TabPanel>
          </TabPanels>
        </Tabs>
      </VStack>
    </Container>
  )
}

export default NewsAnalysis
