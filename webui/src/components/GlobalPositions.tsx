import React, { useEffect, useState, useCallback } from 'react'
import {
  Box,
  Heading,
  Table,
  Thead,
  Tbody,
  Tr,
  Th,
  Td,
  TableContainer,
  Text,
  Spinner,
  Center,
  Badge,
  Flex,
  Button,
  IconButton,
  HStack,
  VStack,
  Collapse,
  Divider,
  Tooltip,
  Card,
  CardBody,
  useToast,
  Tag,
  StatNumber,
  Stat,
  StatLabel,
} from '@chakra-ui/react'
import {
  ChevronDownIcon,
  ChevronUpIcon,
  RepeatIcon,
} from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import {
  getPositionsSummaryAll,
  getPendingOrders,
  type PositionSummaryItem,
  type PendingOrderInfo,
} from '../services/api'
import { useNavigate } from 'react-router-dom'
import TpSlRulesModal from './TpSlRulesModal'
import { loadRules } from '../utils/tpSlStorage'

// ─── Types ───────────────────────────────────────────────────────────────────

interface PositionRowData extends PositionSummaryItem {
  openOrders: PendingOrderInfo[]
  closeOrders: PendingOrderInfo[]
  ordersLoading: boolean
  ordersLoaded: boolean
}

// ─── Helpers ──────────────────────────────────────────────────────────────

function pnlColor(pnl: number): string {
  if (pnl > 0) return 'green.500'
  if (pnl < 0) return 'red.500'
  return 'gray.500'
}

function formatPrice(val: number, decimals = 4): string {
  if (!val && val !== 0) return '—'
  return val.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: decimals })
}

function formatPnl(val: number): string {
  const sign = val >= 0 ? '+' : ''
  return `${sign}${val.toFixed(2)}`
}

// ─── OrderMiniTable ────────────────────────────────────────────────────────

interface OrderMiniTableProps {
  orders: PendingOrderInfo[]
  emptyText: string
}

const OrderMiniTable: React.FC<OrderMiniTableProps> = ({ orders, emptyText }) => {
  const { t } = useTranslation()
  if (orders.length === 0) {
    return <Text fontSize="xs" color="gray.400" py={1}>{emptyText}</Text>
  }
  return (
    <TableContainer>
      <Table size="xs" variant="simple">
        <Thead>
          <Tr>
            <Th fontSize="10px">{t('globalPositions.side')}</Th>
            <Th fontSize="10px" isNumeric>{t('globalPositions.price')}</Th>
            <Th fontSize="10px" isNumeric>{t('globalPositions.orderQty')}</Th>
            <Th fontSize="10px">{t('globalPositions.orderType')}</Th>
            <Th fontSize="10px">{t('globalPositions.status')}</Th>
          </Tr>
        </Thead>
        <Tbody>
          {orders.slice(0, 20).map(o => (
            <Tr key={o.order_id}>
              <Td>
                <Badge
                  colorScheme={(o.side || '').toUpperCase() === 'BUY' ? 'green' : 'red'}
                  fontSize="10px"
                >
                  {o.side}
                </Badge>
              </Td>
              <Td isNumeric fontSize="xs">{formatPrice(o.price)}</Td>
              <Td isNumeric fontSize="xs">{o.quantity}</Td>
              <Td fontSize="xs">{o.strategy_name || o.strategy_type || '—'}</Td>
              <Td>
                <Badge fontSize="10px" colorScheme="blue">{o.status}</Badge>
              </Td>
            </Tr>
          ))}
        </Tbody>
      </Table>
    </TableContainer>
  )
}

// ─── PositionExpandedPanel ────────────────────────────────────────────────

interface ExpandedPanelProps {
  row: PositionRowData
}

const ExpandedPanel: React.FC<ExpandedPanelProps> = ({ row }) => {
  const { t } = useTranslation()

  return (
    <Box px={4} py={3} bg="gray.50" borderRadius="md" mt={1}>
      {row.ordersLoading ? (
        <Center py={4}><Spinner size="sm" /></Center>
      ) : (
        <VStack align="stretch" spacing={3}>
          {/* 开仓委托 */}
          <Box>
            <Text fontSize="sm" fontWeight="600" mb={1} color="green.700">
              {t('globalPositions.openOrders')}
            </Text>
            <OrderMiniTable
              orders={row.openOrders}
              emptyText={t('globalPositions.noOpenOrders')}
            />
          </Box>

          <Divider />

          {/* 平仓委托 */}
          <Box>
            <Text fontSize="sm" fontWeight="600" mb={1} color="red.700">
              {t('globalPositions.closeOrders')}
            </Text>
            <OrderMiniTable
              orders={row.closeOrders}
              emptyText={t('globalPositions.noCloseOrders')}
            />
          </Box>
        </VStack>
      )}
    </Box>
  )
}

// ─── Main Page ────────────────────────────────────────────────────────────

const GlobalPositions: React.FC = () => {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const toast = useToast()

  const [rows, setRows] = useState<PositionRowData[]>([])
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [expandedKeys, setExpandedKeys] = useState<Set<string>>(new Set())
  const [tpSlTarget, setTpSlTarget] = useState<PositionSummaryItem | null>(null)

  // ── 加载持仓 ──
  const fetchPositions = useCallback(async (silent = false) => {
    try {
      if (!silent) setLoading(true)
      else setRefreshing(true)

      const resp = await getPositionsSummaryAll()
      setRows((resp.positions || []).map(p => ({
        ...p,
        openOrders: [],
        closeOrders: [],
        ordersLoading: false,
        ordersLoaded: false,
      })))
    } catch (err) {
      if (!silent) {
        toast({
          title: t('common.error'),
          description: err instanceof Error ? err.message : String(err),
          status: 'error',
          duration: 4000,
        })
      }
    } finally {
      setLoading(false)
      setRefreshing(false)
    }
  }, [t, toast])

  useEffect(() => {
    fetchPositions()
    const timer = setInterval(() => fetchPositions(true), 10000)
    return () => clearInterval(timer)
  }, [fetchPositions])

  // ── 展开行，懒加载委托 ──
  const toggleExpand = useCallback(async (row: PositionRowData) => {
    const key = `${row.exchange}:${row.symbol}:${row.market_type || 'futures'}`

    setExpandedKeys(prev => {
      const next = new Set(prev)
      if (next.has(key)) { next.delete(key); return next }
      next.add(key)
      return next
    })

    if (row.ordersLoaded) return

    // 标记加载中
    setRows(prev => prev.map(r =>
      r.exchange === row.exchange && r.symbol === row.symbol
        ? { ...r, ordersLoading: true }
        : r
    ))

    try {
      const resp = await getPendingOrders(row.exchange, row.symbol)
      const all = resp.orders || []
      const openOrders = all.filter(o => (o.side || '').toUpperCase() === 'BUY')
      const closeOrders = all.filter(o => (o.side || '').toUpperCase() === 'SELL')

      setRows(prev => prev.map(r =>
        r.exchange === row.exchange && r.symbol === row.symbol
          ? { ...r, openOrders, closeOrders, ordersLoading: false, ordersLoaded: true }
          : r
      ))
    } catch {
      setRows(prev => prev.map(r =>
        r.exchange === row.exchange && r.symbol === row.symbol
          ? { ...r, ordersLoading: false, ordersLoaded: true }
          : r
      ))
    }
  }, [])

  // ── 汇总 PnL ──
  const totalPnl = rows.reduce((s, r) => s + r.unrealized_pnl, 0)
  const totalValue = rows.reduce((s, r) => s + r.total_value, 0)

  if (loading) {
    return (
      <Center h="300px">
        <Spinner size="lg" thickness="3px" color="blue.500" />
      </Center>
    )
  }

  return (
    <Box>
      {/* 页头 */}
      <Flex align="center" justify="space-between" mb={4} flexWrap="wrap" gap={2}>
        <Box>
          <Heading size="lg">{t('globalPositions.title')}</Heading>
          <Text fontSize="sm" color="gray.500" mt={0.5}>{t('globalPositions.subtitle')}</Text>
        </Box>
        <HStack spacing={2}>
          {refreshing && <Spinner size="xs" color="blue.400" />}
          <IconButton
            aria-label={t('globalPositions.refreshing')}
            icon={<RepeatIcon />}
            size="sm"
            variant="ghost"
            isLoading={refreshing}
            onClick={() => fetchPositions(true)}
          />
        </HStack>
      </Flex>

      {/* 汇总卡片 */}
      {rows.length > 0 && (
        <Flex gap={4} mb={5} flexWrap="wrap">
          <Card flex="1" minW="140px">
            <CardBody py={3}>
              <Stat>
                <StatLabel fontSize="xs" color="gray.500">{t('globalPositions.unrealizedPnl')}</StatLabel>
                <StatNumber fontSize="lg" color={pnlColor(totalPnl)}>
                  {formatPnl(totalPnl)}
                </StatNumber>
              </Stat>
            </CardBody>
          </Card>
          <Card flex="1" minW="140px">
            <CardBody py={3}>
              <Stat>
                <StatLabel fontSize="xs" color="gray.500">{t('globalPositions.value')}</StatLabel>
                <StatNumber fontSize="lg">{totalValue.toFixed(2)}</StatNumber>
              </Stat>
            </CardBody>
          </Card>
          <Card flex="1" minW="100px">
            <CardBody py={3}>
              <Stat>
                <StatLabel fontSize="xs" color="gray.500">{t('globalPositions.symbol')}</StatLabel>
                <StatNumber fontSize="lg">{rows.length}</StatNumber>
              </Stat>
            </CardBody>
          </Card>
        </Flex>
      )}

      {/* 无持仓 */}
      {rows.length === 0 && (
        <Box textAlign="center" py={16}>
          <Text color="gray.400" fontSize="lg">{t('globalPositions.noPositions')}</Text>
        </Box>
      )}

      {/* 持仓列表 */}
      {rows.length > 0 && (
        <TableContainer>
          <Table variant="simple" size="sm">
            <Thead>
              <Tr>
                <Th w="30px"></Th>
                <Th>{t('globalPositions.exchange')}</Th>
                <Th>{t('globalPositions.symbol')}</Th>
                <Th>{t('globalPositions.strategy')}</Th>
                <Th isNumeric>{t('globalPositions.avgPrice')}</Th>
                <Th isNumeric>{t('globalPositions.currentPrice')}</Th>
                <Th isNumeric>{t('globalPositions.quantity')}</Th>
                <Th isNumeric>{t('globalPositions.value')}</Th>
                <Th isNumeric>{t('globalPositions.unrealizedPnl')}</Th>
                <Th isNumeric>{t('globalPositions.pnlPercent')}</Th>
                <Th isNumeric>{t('globalPositions.margin')}</Th>
                <Th>{t('globalPositions.leverage')}</Th>
                <Th>{t('globalPositions.tpSlRules')}</Th>
                <Th>{t('globalPositions.bot')}</Th>
              </Tr>
            </Thead>
            <Tbody>
              {rows.map(row => {
                const key = `${row.exchange}:${row.symbol}:${row.market_type || 'futures'}`
                const isExpanded = expandedKeys.has(key)
                const ruleCount = loadRules(row.exchange, row.symbol).filter(r => !r.triggered).length

                return (
                  <React.Fragment key={key}>
                    <Tr
                      _hover={{ bg: 'gray.50' }}
                      cursor="pointer"
                    >
                      {/* 展开按钮 */}
                      <Td>
                        <IconButton
                          aria-label={isExpanded ? t('globalPositions.collapse') : t('globalPositions.expand')}
                          icon={isExpanded ? <ChevronUpIcon /> : <ChevronDownIcon />}
                          size="xs"
                          variant="ghost"
                          onClick={() => toggleExpand(row)}
                        />
                      </Td>

                      <Td>
                        <Text fontSize="sm" fontWeight="500">{row.exchange}</Text>
                      </Td>

                      <Td>
                        <HStack spacing={1}>
                          <Text fontSize="sm" fontWeight="600">{row.symbol}</Text>
                          {row.market_type && (
                            <Badge
                              colorScheme={row.market_type === 'spot' ? 'purple' : 'blue'}
                              fontSize="9px"
                              borderRadius="full"
                            >
                              {row.market_type}
                            </Badge>
                          )}
                        </HStack>
                      </Td>

                      <Td>
                        <Text fontSize="xs" color="gray.500">{row.strategy}</Text>
                      </Td>

                      <Td isNumeric>
                        <Text fontSize="sm">{formatPrice(row.average_price)}</Text>
                      </Td>

                      <Td isNumeric>
                        <Text fontSize="sm">{formatPrice(row.current_price)}</Text>
                      </Td>

                      <Td isNumeric>
                        <Text fontSize="sm">{row.total_quantity.toFixed(4)}</Text>
                      </Td>

                      <Td isNumeric>
                        <Text fontSize="sm">{row.total_value.toFixed(2)}</Text>
                      </Td>

                      <Td isNumeric>
                        <Text
                          fontSize="sm"
                          fontWeight="600"
                          color={pnlColor(row.unrealized_pnl)}
                        >
                          {formatPnl(row.unrealized_pnl)}
                        </Text>
                      </Td>

                      <Td isNumeric>
                        <Text
                          fontSize="sm"
                          color={pnlColor(row.pnl_percentage)}
                        >
                          {row.pnl_percentage >= 0 ? '+' : ''}{row.pnl_percentage.toFixed(2)}%
                        </Text>
                      </Td>

                      <Td isNumeric>
                        <Text fontSize="xs" color="gray.600">
                          {row.actual_margin > 0 ? row.actual_margin.toFixed(2) : '—'}
                        </Text>
                      </Td>

                      <Td>
                        {row.leverage && row.leverage > 1 ? (
                          <Badge colorScheme="orange" borderRadius="full" fontSize="xs">
                            {row.leverage}x
                          </Badge>
                        ) : '—'}
                      </Td>

                      {/* TP/SL 按钮 */}
                      <Td>
                        <Button
                          size="xs"
                          colorScheme={ruleCount > 0 ? 'green' : 'gray'}
                          variant={ruleCount > 0 ? 'solid' : 'outline'}
                          onClick={() => setTpSlTarget(row)}
                        >
                          {ruleCount > 0 ? `${ruleCount} ✓` : t('globalPositions.tpSlRules')}
                        </Button>
                      </Td>

                      {/* 跳转 Bot */}
                      <Td>
                        {row.bot_id ? (
                          <Tooltip label={row.bot_id} placement="top">
                            <Button
                              size="xs"
                              variant="ghost"
                              colorScheme="blue"
                              onClick={() => navigate(`/bots/${row.bot_id}/dashboard`)}
                            >
                              {t('globalPositions.bot')} →
                            </Button>
                          </Tooltip>
                        ) : (
                          <Text fontSize="xs" color="gray.400">—</Text>
                        )}
                      </Td>
                    </Tr>

                    {/* 展开面板 */}
                    {isExpanded && (
                      <Tr>
                        <Td colSpan={14} p={0} borderBottomWidth="1px">
                          <Collapse in={isExpanded} animateOpacity>
                            <ExpandedPanel row={row} />
                          </Collapse>
                        </Td>
                      </Tr>
                    )}
                  </React.Fragment>
                )
              })}
            </Tbody>
          </Table>
        </TableContainer>
      )}

      {/* TP/SL 弹窗 */}
      {tpSlTarget && (
        <TpSlRulesModal
          isOpen={!!tpSlTarget}
          onClose={() => setTpSlTarget(null)}
          exchange={tpSlTarget.exchange}
          symbol={tpSlTarget.symbol}
          marketType={tpSlTarget.market_type}
          botId={tpSlTarget.bot_id}
          currentPrice={tpSlTarget.current_price}
          totalQuantity={tpSlTarget.total_quantity}
        />
      )}
    </Box>
  )
}

export default GlobalPositions
