import React, { useEffect, useState } from 'react'
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
} from '@chakra-ui/react'
import { RepeatIcon } from '@chakra-ui/icons'
import { Link as RouterLink } from 'react-router-dom'
import {
  getNewsAnalysis,
  getNewsCollected,
  triggerNewsAnalyze,
  getPredictionsAccuracy,
  NewsRiskAssessment,
  NewsItem,
} from '../services/api'

const ASSET_OPTIONS = [
  { value: 'crypto_btc', label: 'BTC', symbol: 'BTCUSDT' },
  { value: 'commodity_gold', label: '黃金 (PAXG)', symbol: 'PAXGUSDT' },
]

const REC_LABELS: Record<string, { label: string; color: string }> = {
  normal: { label: '正常', color: 'green' },
  caution: { label: '谨慎', color: 'yellow' },
  reduce_position: { label: '减倉', color: 'orange' },
  stop_trading: { label: '暂停交易', color: 'red' },
}

const NewsAnalysis: React.FC = () => {
  const [assetType, setAssetType] = useState('crypto_btc')
  const [assessment, setAssessment] = useState<NewsRiskAssessment | null>(null)
  const [isAnalyzing, setIsAnalyzing] = useState(false)
  const [news, setNews] = useState<NewsItem[]>([])
  const [accuracy, setAccuracy] = useState<{ total: number; correct: number; accuracy: number } | null>(null)
  const [loading, setLoading] = useState(true)
  const [triggering, setTriggering] = useState(false)
  const [focusEvent, setFocusEvent] = useState('')
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
      toast({ title: '獲取數據失败', status: 'error', duration: 3000 })
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchData()
    const interval = setInterval(fetchData, 30000)
    return () => clearInterval(interval)
  }, [assetType])

  const handleTrigger = async () => {
    try {
      setTriggering(true)
      await triggerNewsAnalyze(currentAsset.symbol, focusEvent || undefined, assetType)
      toast({ title: '分析任務已提交', status: 'success', duration: 2000 })
      setTimeout(fetchData, 2000)
    } catch (err) {
      toast({ title: (err as Error).message || '触发失败', status: 'error', duration: 3000 })
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
            <Heading size="lg">新聞分析</Heading>
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
            <Button as={RouterLink} to="/news-analysis/history" size="sm" variant="ghost">
              历史記錄
            </Button>
            <Button as={RouterLink} to="/news-analysis/predictions" size="sm" variant="ghost">
              預测准确率
            </Button>
          </HStack>
          <HStack>
            <FormControl w="280px">
              <FormLabel fontSize="sm">焦点事件（可選）</FormLabel>
              <Input
                placeholder="如：伊朗港口大爆炸事件"
                value={focusEvent}
                onChange={(e) => setFocusEvent(e.target.value)}
                size="sm"
              />
            </FormControl>
            <Button
              leftIcon={<RepeatIcon />}
              colorScheme="blue"
              isLoading={triggering || isAnalyzing}
              loadingText={isAnalyzing ? '分析中...' : '提交中...'}
              onClick={handleTrigger}
              size="sm"
              mt={6}
            >
              手动触发分析
            </Button>
            <Button variant="ghost" size="sm" onClick={fetchData} mt={6}>
              刷新
            </Button>
          </HStack>
        </HStack>

        {isAnalyzing && (
          <Alert status="info">
            <Spinner size="sm" mr={2} />
            <AlertTitle>Gemini 正在分析中...</AlertTitle>
          </Alert>
        )}

        {accuracy !== null && accuracy.total > 0 && (
          <Card>
            <CardHeader py={3}>
              <Text fontSize="sm" fontWeight="600">預测准确率（近7天）</Text>
            </CardHeader>
            <CardBody pt={0}>
              <HStack spacing={6}>
                <Text>總计 <strong>{accuracy.total}</strong> 次</Text>
                <Text>正确 <strong>{accuracy.correct}</strong> 次</Text>
                <Text>准确率 <strong>{accuracy.accuracy.toFixed(1)}%</strong></Text>
              </HStack>
            </CardBody>
          </Card>
        )}

        {assessment && (
          <>
            <SimpleGrid columns={{ base: 1, md: 3 }} spacing={4}>
              <Card>
                <CardHeader py={3}>
                  <Text fontSize="sm" color="gray.500">建议操作</Text>
                </CardHeader>
                <CardBody pt={0}>
                  <Badge colorScheme={recInfo.color} fontSize="md" px={3} py={1}>
                    {recInfo.label}
                  </Badge>
                </CardBody>
              </Card>
              <Card>
                <CardHeader py={3}>
                  <Text fontSize="sm" color="gray.500">风險评分</Text>
                </CardHeader>
                <CardBody pt={0}>
                  <Text fontSize="2xl" fontWeight="bold">{assessment.overall_risk_score.toFixed(1)}</Text>
                  <Progress value={assessment.overall_risk_score} colorScheme="red" size="sm" mt={2} borderRadius="full" />
                </CardBody>
              </Card>
              <Card>
                <CardHeader py={3}>
                  <Text fontSize="sm" color="gray.500">大跌概率</Text>
                </CardHeader>
                <CardBody pt={0}>
                  <Text fontSize="2xl" fontWeight="bold">{(assessment.crash_probability * 100).toFixed(1)}%</Text>
                </CardBody>
              </Card>
            </SimpleGrid>

            {assessment.analysis_summary && (
              <Card>
                <CardHeader py={3}>
                  <Text fontSize="sm" fontWeight="600">分析摘要</Text>
                </CardHeader>
                <CardBody pt={0}>
                  <Text fontSize="sm" color="gray.600">{assessment.analysis_summary}</Text>
                </CardBody>
              </Card>
            )}

            {assessment.price_predictions && assessment.price_predictions.length > 0 && (
              <Card>
                <CardHeader py={3}>
                  <Text fontSize="sm" fontWeight="600">價格預测概率</Text>
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
                  <Text fontSize="sm" fontWeight="600">风險因素</Text>
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
            <Text fontSize="sm" fontWeight="600">已收集新闻（最近 24 小時）</Text>
          </CardHeader>
          <CardBody pt={0}>
            {news.length === 0 ? (
              <Text color="gray.500" fontSize="sm">暂無（请检查 NewsAPI Key 配置或网络连接）</Text>
            ) : (
              <Table size="sm">
                <Thead>
                  <Tr>
                    <Th>時间</Th>
                    <Th>標题</Th>
                    <Th>来源</Th>
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
    </Container>
  )
}

export default NewsAnalysis
