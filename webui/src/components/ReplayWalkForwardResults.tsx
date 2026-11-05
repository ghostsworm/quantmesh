import { Box, Heading, ListItem, Table, Tbody, Td, Text, Th, Thead, Tr, UnorderedList, VStack } from '@chakra-ui/react'
import { useTranslation } from 'react-i18next'
import type { ReplayEvaluation, ReplayWalkForwardResult } from '../services/replayWalkForward'

const METRICS: (keyof ReplayEvaluation)[] = [
  'total_return', 'net_pnl', 'max_drawdown', 'fills', 'closed_grids',
  'fees_maker', 'fees_taker', 'fees_total', 'funding_paid', 'unrealized_pnl', 'final_net_qty',
]
const EVALUATIONS = ['train', 'test', 'baseline_train', 'baseline_test'] as const
const CORE_LIMITATIONS = [
  'synthetic_ohlc_path', 'no_historical_order_book', 'zero_latency_no_market_impact',
  'no_historical_predicted_funding', 'settlement_funding_cost_only',
  'synthetic_or_historical_replay_is_not_live_profit_evidence',
]

function FrozenJSON({ label, value }: { label: string; value: unknown }) {
  return (
    <Box as="details" w="full">
      <Box as="summary" cursor="pointer" fontSize="sm">{label}</Box>
      <Box as="pre" fontSize="xs" overflowX="auto" p={2}>{JSON.stringify(value, null, 2)}</Box>
    </Box>
  )
}

export default function ReplayWalkForwardResults({ result }: { result: ReplayWalkForwardResult }) {
  const { t, i18n } = useTranslation()
  const number = (value: number, digits = 4) => new Intl.NumberFormat(i18n.language, { maximumFractionDigits: digits }).format(value)
  const date = (timestamp: number) => new Date(timestamp).toLocaleString(i18n.language, { timeZone: 'UTC', timeZoneName: 'short' })
  const range = (start: number, end: number) => `${date(start)} – ${date(end)}`
  const hash = (key: string, value: string) => (
    <Text fontSize="xs" overflowWrap="anywhere"><strong>{t(`replayWalkForward.${key}`)}</strong>: {value}</Text>
  )
  const limitations = [...new Set([...CORE_LIMITATIONS, ...(result.limitations ?? [])])]

  return (
    <VStack align="stretch" spacing={4} data-testid="replay-results">
      <Heading size="sm">{t('replayWalkForward.resultTitle')}</Heading>
      <Text fontSize="sm">{t('replayWalkForward.selection')}</Text>
      <Text fontSize="sm">{t('replayWalkForward.windowPolicy')}</Text>
      <Box>
        <Heading size="xs" mb={2}>{t('replayWalkForward.limits')}</Heading>
        <UnorderedList spacing={1} fontSize="sm">
          {limitations.map((code) => (
            <ListItem key={code}>
              {i18n.exists(`replayWalkForward.${code}`) ? t(`replayWalkForward.${code}`) : t('replayWalkForward.unknownLimitation', { code })}
            </ListItem>
          ))}
        </UnorderedList>
      </Box>
      <Box as="details">
        <Box as="summary" cursor="pointer">{t('replayWalkForward.audit')}</Box>
        <VStack align="stretch" spacing={2} mt={2}>
          {hash('protocol', result.protocol)}
          {hash('engine', result.engine)}
          {hash('scoreRule', result.score_rule)}
          {hash('warmupPolicy', result.warmup_policy)}
          {hash('windowPolicyCode', result.window_policy)}
          <Text fontSize="sm">{t('replayWalkForward.dataRange')}: {range(result.data_start, result.data_end)}</Text>
          <Text fontSize="sm">{t('replayWalkForward.dataBars')}: {number(result.data_bars, 0)}</Text>
          <Text fontSize="sm">{t('replayWalkForward.candleInterval')}: {number(result.candle_interval_ms, 0)}</Text>
          {hash('dataHash', result.data_sha256)}
          {hash('configHash', result.config_sha256)}
          {hash('experimentHash', result.experiment_sha256)}
          {hash('baselineHash', result.baseline_sha256)}
          <FrozenJSON label={t('replayWalkForward.frozenConfig')} value={result.config} />
          <FrozenJSON label={t('replayWalkForward.frozenBaseline')} value={result.baseline_params} />
        </VStack>
      </Box>
      {result.folds.length === 0 && <Text>{t('replayWalkForward.noFolds')}</Text>}
      {result.folds.map((fold) => (
        <Box key={fold.index} borderWidth="1px" borderRadius="md" p={3} data-testid={`replay-fold-${fold.index}`}>
          <Heading size="sm" mb={2}>{t('replayWalkForward.fold', { index: fold.index + 1 })}</Heading>
          <Text fontSize="xs">{t('replayWalkForward.trainRange')}: {range(fold.train_start, fold.train_end)}</Text>
          <Text fontSize="xs">{t('replayWalkForward.testRange')}: {range(fold.test_start, fold.test_end)}</Text>
          <Text fontWeight="semibold" fontSize="sm" my={2} data-testid="replay-excess">
            {t('replayWalkForward.excess', { value: number(fold.test_excess_return) })}
          </Text>
          <Box overflowX="auto" mb={3}>
            <Table size="sm" aria-label={t('replayWalkForward.fold', { index: fold.index + 1 })}>
              <Thead>
                <Tr><Th>{t('replayWalkForward.metric')}</Th>{EVALUATIONS.map((key) => <Th key={key} isNumeric>{t(`replayWalkForward.${key}`)}</Th>)}</Tr>
              </Thead>
              <Tbody>
                {METRICS.map((metric) => (
                  <Tr key={metric} data-testid={`replay-metric-${metric}`}>
                    <Th scope="row">{t(`replayWalkForward.${metric}`)}</Th>
                    {EVALUATIONS.map((key) => <Td key={key} isNumeric>{number(fold[key][metric], metric === 'final_net_qty' ? 8 : 4)}</Td>)}
                  </Tr>
                ))}
              </Tbody>
            </Table>
          </Box>
          <VStack align="stretch" spacing={2}>
            {hash('paramsHash', fold.params_sha256)}
            {hash('trainHash', fold.train_data_sha256)}
            {hash('testHash', fold.test_data_sha256)}
            <FrozenJSON label={t('replayWalkForward.frozenParams')} value={fold.selected_params} />
            <Box as="details">
              <Box as="summary" cursor="pointer" fontSize="sm">{t('replayWalkForward.candidates')}</Box>
              <Box overflowX="auto" mt={2}>
                <Table size="sm">
                  <Thead><Tr><Th>{t('replayWalkForward.frozenParams')}</Th><Th isNumeric>{t('replayWalkForward.total_return')}</Th><Th isNumeric>{t('replayWalkForward.max_drawdown')}</Th><Th isNumeric>{t('replayWalkForward.score')}</Th></Tr></Thead>
                  <Tbody>
                    {fold.candidates.map((candidate, index) => (
                      <Tr key={candidate.params_sha256}>
                        <Td>
                          <FrozenJSON label={t('replayWalkForward.candidate', { index: index + 1 })} value={candidate.params} />
                          {hash('paramsHash', candidate.params_sha256)}
                        </Td>
                        <Td isNumeric>{number(candidate.train.total_return)}</Td>
                        <Td isNumeric>{number(candidate.train.max_drawdown)}</Td>
                        <Td isNumeric>{number(candidate.train.score)}</Td>
                      </Tr>
                    ))}
                  </Tbody>
                </Table>
              </Box>
            </Box>
          </VStack>
        </Box>
      ))}
    </VStack>
  )
}
