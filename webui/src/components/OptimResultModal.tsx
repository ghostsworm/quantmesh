import React, { useMemo, useState } from 'react'
import {
  Modal,
  ModalOverlay,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalFooter,
  ModalCloseButton,
  Box,
  Table,
  Thead,
  Tbody,
  Tr,
  Th,
  Td,
  Button,
  HStack,
  Input,
  FormControl,
  FormLabel,
  Select,
  Text,
  useToast,
  Badge,
} from '@chakra-ui/react'
import { DownloadIcon } from '@chakra-ui/icons'
import type { OptimParamResult, OptimResult } from '../services/backtest'

type SortKey = 'total_return' | 'sharpe_ratio' | 'max_drawdown' | 'win_rate' | 'total_trades'
type SortOrder = 'asc' | 'desc'

interface OptimResultModalProps {
  isOpen: boolean
  onClose: () => void
  result: OptimResult | null
  isLoading?: boolean
}

export default function OptimResultModal({ isOpen, onClose, result, isLoading }: OptimResultModalProps) {
  const toast = useToast()
  const [maxDrawdown, setMaxDrawdown] = useState<string>('')
  const [minReturn, setMinReturn] = useState<string>('')
  const [minTrades, setMinTrades] = useState<string>('')
  const [sortKey, setSortKey] = useState<SortKey>('total_return')
  const [sortOrder, setSortOrder] = useState<SortOrder>('desc')

  const filteredAndSorted = useMemo(() => {
    if (!result?.all_results) return []
    let list = [...result.all_results]
    const md = parseFloat(maxDrawdown)
    if (!Number.isNaN(md) && md >= 0) {
      list = list.filter((r) => r.max_drawdown <= md)
    }
    const mr = parseFloat(minReturn)
    if (!Number.isNaN(mr)) {
      list = list.filter((r) => r.total_return >= mr)
    }
    const mt = parseInt(minTrades, 10)
    if (!Number.isNaN(mt) && mt >= 0) {
      list = list.filter((r) => r.total_trades >= mt)
    }
    list.sort((a, b) => {
      let va: number
      let vb: number
      switch (sortKey) {
        case 'total_return':
          va = a.total_return
          vb = b.total_return
          break
        case 'sharpe_ratio':
          va = a.sharpe_ratio
          vb = b.sharpe_ratio
          break
        case 'max_drawdown':
          va = a.max_drawdown
          vb = b.max_drawdown
          break
        case 'win_rate':
          va = a.win_rate
          vb = b.win_rate
          break
        case 'total_trades':
          va = a.total_trades
          vb = b.total_trades
          break
        default:
          return 0
      }
      const cmp = va < vb ? -1 : va > vb ? 1 : 0
      return sortOrder === 'asc' ? cmp : -cmp
    })
    return list
  }, [result?.all_results, maxDrawdown, minReturn, minTrades, sortKey, sortOrder])

  const paramKeys = useMemo(() => {
    if (filteredAndSorted.length === 0) return []
    const keys = new Set<string>()
    filteredAndSorted.forEach((r) => {
      if (r.params) Object.keys(r.params).forEach((k) => keys.add(k))
    })
    return Array.from(keys).sort()
  }, [filteredAndSorted])

  const handleExportCSV = () => {
    if (filteredAndSorted.length === 0) {
      toast({ title: '无数据可导出', status: 'warning' })
      return
    }
    const headers = [...paramKeys, 'total_return', 'max_drawdown', 'sharpe_ratio', 'win_rate', 'total_trades']
    const rows = filteredAndSorted.map((r) => {
      const cells = paramKeys.map((k) => String(r.params?.[k] ?? ''))
      cells.push(
        r.total_return.toFixed(4),
        r.max_drawdown.toFixed(4),
        r.sharpe_ratio.toFixed(4),
        r.win_rate.toFixed(4),
        String(r.total_trades)
      )
      return cells.join(',')
    })
    const csv = [headers.join(','), ...rows].join('\n')
    const blob = new Blob(['\ufeff' + csv], { type: 'text/csv;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `optim_result_${result?.task_id ?? 'export'}.csv`
    a.click()
    URL.revokeObjectURL(url)
    toast({ title: '已导出 CSV', status: 'success' })
  }

  const bestByReturn = result?.best_by_return
  const bestBySharpe = result?.best_by_sharpe

  const isBestByReturn = (r: OptimParamResult) =>
    bestByReturn && r.total_return === bestByReturn.total_return && JSON.stringify(r.params) === JSON.stringify(bestByReturn.params)
  const isBestBySharpe = (r: OptimParamResult) =>
    bestBySharpe && r.sharpe_ratio === bestBySharpe.sharpe_ratio && JSON.stringify(r.params) === JSON.stringify(bestBySharpe.params)

  return (
    <Modal isOpen={isOpen} onClose={onClose} size="6xl" scrollBehavior="inside">
      <ModalOverlay />
      <ModalContent maxH="90vh">
        <ModalHeader>参数优化结果</ModalHeader>
        <ModalCloseButton />
        <ModalBody overflowY="auto">
          {isLoading && (
            <Box py={8} textAlign="center">
              <Text color="gray.500">载入中...</Text>
            </Box>
          )}
          {!isLoading && result && (
            <>
              <HStack mb={4} flexWrap="wrap" gap={2}>
                <FormControl width="120px">
                  <FormLabel fontSize="xs">最大回撤 ≤ %</FormLabel>
                  <Input
                    size="sm"
                    placeholder="不限"
                    value={maxDrawdown}
                    onChange={(e) => setMaxDrawdown(e.target.value)}
                  />
                </FormControl>
                <FormControl width="120px">
                  <FormLabel fontSize="xs">收益率 ≥ %</FormLabel>
                  <Input
                    size="sm"
                    placeholder="不限"
                    value={minReturn}
                    onChange={(e) => setMinReturn(e.target.value)}
                  />
                </FormControl>
                <FormControl width="100px">
                  <FormLabel fontSize="xs">交易次数 ≥</FormLabel>
                  <Input
                    size="sm"
                    placeholder="不限"
                    value={minTrades}
                    onChange={(e) => setMinTrades(e.target.value)}
                  />
                </FormControl>
                <FormControl width="140px">
                  <FormLabel fontSize="xs">排序</FormLabel>
                  <Select
                    size="sm"
                    value={sortKey}
                    onChange={(e) => setSortKey(e.target.value as SortKey)}
                  >
                    <option value="total_return">收益率</option>
                    <option value="sharpe_ratio">夏普比率</option>
                    <option value="max_drawdown">最大回撤</option>
                    <option value="win_rate">胜率</option>
                    <option value="total_trades">交易次数</option>
                  </Select>
                </FormControl>
                <FormControl width="80px">
                  <FormLabel fontSize="xs">顺序</FormLabel>
                  <Select
                    size="sm"
                    value={sortOrder}
                    onChange={(e) => setSortOrder(e.target.value as SortOrder)}
                  >
                    <option value="desc">降序</option>
                    <option value="asc">升序</option>
                  </Select>
                </FormControl>
                <Text fontSize="sm" color="gray.500" alignSelf="flex-end" pb={1}>
                  共 {filteredAndSorted.length} 组（总 {result.all_results?.length ?? 0} 组）
                </Text>
              </HStack>
              <Box overflowX="auto">
                <Table size="sm">
                  <Thead>
                    <Tr>
                      {paramKeys.map((k) => (
                        <Th key={k}>{k}</Th>
                      ))}
                      <Th>收益率 %</Th>
                      <Th>最大回撤 %</Th>
                      <Th>夏普</Th>
                      <Th>胜率 %</Th>
                      <Th>交易数</Th>
                    </Tr>
                  </Thead>
                  <Tbody>
                    {filteredAndSorted.map((r, idx) => (
                      <Tr
                        key={idx}
                        bg={
                          isBestByReturn(r)
                            ? 'green.50'
                            : isBestBySharpe(r)
                              ? 'blue.50'
                              : undefined
                        }
                        _dark={{
                          bg: isBestByReturn(r) ? 'green.900' : isBestBySharpe(r) ? 'blue.900' : undefined,
                        }}
                      >
                        {paramKeys.map((k) => (
                          <Td key={k} fontSize="xs">
                            {typeof r.params?.[k] === 'number'
                              ? Number(r.params[k]).toFixed(4)
                              : String(r.params?.[k] ?? '-')}
                          </Td>
                        ))}
                        <Td fontWeight={isBestByReturn(r) ? 'bold' : undefined}>
                          {r.total_return.toFixed(4)}
                          {isBestByReturn(r) && (
                            <Badge ml={1} colorScheme="green" fontSize="xs">
                              最佳
                            </Badge>
                          )}
                        </Td>
                        <Td>{r.max_drawdown.toFixed(4)}</Td>
                        <Td fontWeight={isBestBySharpe(r) ? 'bold' : undefined}>
                          {r.sharpe_ratio.toFixed(4)}
                          {isBestBySharpe(r) && (
                            <Badge ml={1} colorScheme="blue" fontSize="xs">
                              最佳
                            </Badge>
                          )}
                        </Td>
                        <Td>{r.win_rate.toFixed(2)}</Td>
                        <Td>{r.total_trades}</Td>
                      </Tr>
                    ))}
                  </Tbody>
                </Table>
              </Box>
            </>
          )}
        </ModalBody>
        {result && !isLoading && (
          <ModalFooter>
            <Button leftIcon={<DownloadIcon />} size="sm" onClick={handleExportCSV}>
              导出 CSV
            </Button>
          </ModalFooter>
        )}
      </ModalContent>
    </Modal>
  )
}
