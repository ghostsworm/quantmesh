import React, { useEffect, useState } from 'react'
import {
  Box,
  VStack,
  Checkbox,
  CheckboxGroup,
  Input,
  InputGroup,
  InputLeftElement,
  Text,
  Badge,
  Spinner,
  useColorModeValue,
  Alert,
  AlertIcon,
  AlertDescription,
} from '@chakra-ui/react'
import { SearchIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import { getSymbols, SymbolInfo } from '../services/api'
import { getExchangeSymbols } from '../services/setup'

interface SymbolMultiSelectProps {
  exchange: string
  selectedSymbols: string[]
  onChange: (symbols: string[]) => void
  isDisabled?: boolean
  // 首次设置向导时需要的 API 凭证
  apiKey?: string
  secretKey?: string
  passphrase?: string
  testnet?: boolean
}

const SymbolMultiSelect: React.FC<SymbolMultiSelectProps> = ({
  exchange,
  selectedSymbols,
  onChange,
  isDisabled = false,
  apiKey,
  secretKey,
  passphrase,
  testnet,
}) => {
  const { t } = useTranslation()
  const [symbols, setSymbols] = useState<SymbolInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [searchQuery, setSearchQuery] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [hasTriedFetch, setHasTriedFetch] = useState(false)
  
  const bgColor = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue('gray.200', 'gray.600')

  useEffect(() => {
    const fetchSymbols = async () => {
      if (!exchange) {
        setSymbols([])
        setLoading(false)
        return
      }

      try {
        setLoading(true)
        setError(null)
        setHasTriedFetch(true)
        
        // 如果提供了 API 凭证，从交易所获取所有交易对
        if (apiKey && secretKey) {
          try {
            const response = await getExchangeSymbols({
              exchange,
              api_key: apiKey,
              secret_key: secretKey,
              passphrase,
              testnet,
            })
            
            if (response.success && response.symbols.length > 0) {
              // 转换为 SymbolInfo 格式
              // 注意：保持后端返回的顺序（后端已经按优先级排序）
              const symbolInfos: SymbolInfo[] = response.symbols.map((symbol) => ({
                exchange: exchange.toLowerCase(),
                symbol,
                is_active: false, // 首次设置时都是未运行状态
                current_price: 0,
              }))
              // 不在这里排序，保持后端返回的优先级顺序
              setSymbols(symbolInfos)
              setLoading(false)
              return
            } else {
              setError(response.message || t('wizard.exchange.fetchSymbolsFailed'))
            }
          } catch (error: any) {
            console.error('从交易所获取交易对失败:', error)
            const errorMessage = error?.message || t('wizard.exchange.fetchSymbolsFailed')
            // 检查是否是认证错误
            if (errorMessage.includes('API') || errorMessage.includes('key') || errorMessage.includes('认证') || errorMessage.includes('auth')) {
              setError(t('wizard.exchange.invalidCredentials'))
            } else {
              setError(errorMessage)
            }
            // 如果失败，继续使用配置列表
          }
        } else {
          // 没有提供 API 凭证，使用配置列表
          setHasTriedFetch(false)
        }
        
        // 回退方案：从配置中获取交易对
        const response = await getSymbols()
        // 根据选中的交易所过滤交易对（忽略大小写）
        const filtered = response.symbols.filter(
          (s) => s.exchange.toLowerCase() === exchange.toLowerCase()
        )
        // 按交易对名称排序
        filtered.sort((a, b) => a.symbol.localeCompare(b.symbol))
        setSymbols(filtered)
      } catch (error) {
        console.error('获取交易对列表失败:', error)
        setSymbols([])
        setError(t('wizard.exchange.fetchSymbolsFailed'))
      } finally {
        setLoading(false)
      }
    }

    fetchSymbols()
  }, [exchange, apiKey, secretKey, passphrase, testnet])

  // 根据搜索查询过滤交易对
  const filteredSymbols = symbols.filter((s) =>
    s.symbol.toLowerCase().includes(searchQuery.toLowerCase())
  )

  // 分组：活跃和非活跃
  const activeSymbols = filteredSymbols.filter((s) => s.is_active)
  const inactiveSymbols = filteredSymbols.filter((s) => !s.is_active)

  const handleCheckboxChange = (values: string[]) => {
    onChange(values)
  }

  if (!exchange) {
    return (
      <Box
        p={4}
        borderWidth="1px"
        borderRadius="md"
        borderColor={borderColor}
        bg={bgColor}
      >
        <Text color="gray.500" fontSize="sm">
          {t('wizard.exchange.symbolsPlaceholder')}
        </Text>
      </Box>
    )
  }

  // 如果没有输入 API Key 和 Secret Key，显示提示
  const needsCredentials = !apiKey || !secretKey

  if (loading) {
    return (
      <Box
        p={8}
        borderWidth="1px"
        borderRadius="md"
        borderColor={borderColor}
        bg={bgColor}
        textAlign="center"
      >
        <Spinner size="md" />
        <Text mt={4} color="gray.500" fontSize="sm">
          加载交易对列表...
        </Text>
      </Box>
    )
  }

  return (
    <Box
      borderWidth="1px"
      borderRadius="md"
      borderColor={borderColor}
      bg={bgColor}
      maxH="400px"
      overflowY="auto"
    >
      {/* 提示信息 */}
      {needsCredentials && !hasTriedFetch && (
        <Box p={3} borderBottomWidth="1px" borderColor={borderColor}>
          <Alert status="info" size="sm" borderRadius="md">
            <AlertIcon />
            <AlertDescription fontSize="xs">
              {t('wizard.exchange.enterCredentialsToLoadSymbols')}
            </AlertDescription>
          </Alert>
        </Box>
      )}

      {/* 错误提示 */}
      {error && (
        <Box p={3} borderBottomWidth="1px" borderColor={borderColor}>
          <Alert status="error" size="sm" borderRadius="md">
            <AlertIcon />
            <AlertDescription fontSize="xs">
              {error}
            </AlertDescription>
          </Alert>
        </Box>
      )}

      <Box p={3} borderBottomWidth={needsCredentials && !hasTriedFetch || error ? "0px" : "1px"} borderColor={borderColor}>
        <InputGroup size="sm">
          <InputLeftElement pointerEvents="none">
            <SearchIcon color="gray.400" />
          </InputLeftElement>
          <Input
            placeholder={t('wizard.exchange.symbolsPlaceholder')}
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            isDisabled={isDisabled || needsCredentials}
          />
        </InputGroup>
      </Box>

      <Box p={3}>
        {needsCredentials && !hasTriedFetch ? (
          <Text color="gray.500" fontSize="sm" textAlign="center" py={4}>
            {t('wizard.exchange.enterCredentialsFirst')}
          </Text>
        ) : filteredSymbols.length === 0 ? (
          <Text color="gray.500" fontSize="sm" textAlign="center" py={4}>
            {t('wizard.exchange.noSymbolsFound')}
          </Text>
        ) : (
          <CheckboxGroup
            value={selectedSymbols}
            onChange={handleCheckboxChange}
            isDisabled={isDisabled}
          >
            <VStack align="stretch" spacing={2}>
              {activeSymbols.length > 0 && (
                <>
                  <Text fontSize="xs" fontWeight="bold" color="green.500" mt={2}>
                    运行中 ({activeSymbols.length})
                  </Text>
                  {activeSymbols.map((sym) => (
                    <Checkbox key={sym.symbol} value={sym.symbol} size="sm">
                      <Box display="flex" alignItems="center" gap={2}>
                        <Badge colorScheme="green" fontSize="xs">
                          运行
                        </Badge>
                        <Text>{sym.symbol}</Text>
                        {sym.current_price > 0 && (
                          <Text fontSize="xs" color="gray.500">
                            ${sym.current_price.toFixed(2)}
                          </Text>
                        )}
                      </Box>
                    </Checkbox>
                  ))}
                </>
              )}

              {inactiveSymbols.length > 0 && (
                <>
                  <Text fontSize="xs" fontWeight="bold" color="gray.500" mt={activeSymbols.length > 0 ? 4 : 2}>
                    未运行 ({inactiveSymbols.length})
                  </Text>
                  {inactiveSymbols.map((sym) => (
                    <Checkbox key={sym.symbol} value={sym.symbol} size="sm">
                      <Box display="flex" alignItems="center" gap={2}>
                        <Badge colorScheme="gray" fontSize="xs">
                          未运行
                        </Badge>
                        <Text>{sym.symbol}</Text>
                        {sym.current_price > 0 && (
                          <Text fontSize="xs" color="gray.500">
                            ${sym.current_price.toFixed(2)}
                          </Text>
                        )}
                      </Box>
                    </Checkbox>
                  ))}
                </>
              )}
            </VStack>
          </CheckboxGroup>
        )}
      </Box>

      {selectedSymbols.length > 0 && (
        <Box
          p={2}
          borderTopWidth="1px"
          borderColor={borderColor}
          bg={useColorModeValue('blue.50', 'blue.900')}
        >
          <Text fontSize="xs" color="blue.600" fontWeight="medium">
            已选择 {selectedSymbols.length} 个交易对
          </Text>
        </Box>
      )}
    </Box>
  )
}

export default SymbolMultiSelect
