import React, { useEffect, useState } from 'react'
import {
  Box,
  VStack,
  HStack,
  Heading,
  Text,
  Button,
  Table,
  Thead,
  Tbody,
  Tr,
  Th,
  Td,
  Badge,
  useToast,
  IconButton,
  Tooltip,
  Spinner,
  Card,
  CardHeader,
  CardBody,
  Flex,
  Input,
  InputGroup,
  InputLeftElement,
} from '@chakra-ui/react'
import {
  DownloadIcon,
  StarIcon,
  SearchIcon,
  RepeatIcon,
} from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import {
  listKlineFiles,
  protectKlineFile,
  unprotectKlineFile,
  downloadKlineFile,
  type KlineFileInfo,
} from '../services/klineFiles'

const KlineFilesManager: React.FC = () => {
  const { t } = useTranslation()
  const toast = useToast()
  
  const [files, setFiles] = useState<KlineFileInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [searchTerm, setSearchTerm] = useState('')
  const [protectingFiles, setProtectingFiles] = useState<Set<string>>(new Set())
  const [downloadingFiles, setDownloadingFiles] = useState<Set<string>>(new Set())

  // 加载文件列表
  const loadFiles = async () => {
    setLoading(true)
    try {
      const fileList = await listKlineFiles()
      setFiles(fileList)
    } catch (error) {
      console.error('加载文件列表失败:', error)
      toast({
        title: t('common.error'),
        description: error instanceof Error ? error.message : t('common.unknownError'),
        status: 'error',
        duration: 5000,
        isClosable: true,
      })
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    loadFiles()
  }, [])

  // 切换保护状态
  const toggleProtection = async (filename: string, isProtected: boolean) => {
    setProtectingFiles(prev => new Set(prev).add(filename))
    try {
      if (isProtected) {
        await unprotectKlineFile(filename)
        toast({
          title: t('common.success'),
          description: '文件保护已取消',
          status: 'success',
          duration: 3000,
          isClosable: true,
        })
      } else {
        await protectKlineFile(filename)
        toast({
          title: t('common.success'),
          description: '文件已保护',
          status: 'success',
          duration: 3000,
          isClosable: true,
        })
      }
      // 重新加载文件列表
      await loadFiles()
    } catch (error) {
      console.error('操作失败:', error)
      toast({
        title: t('common.error'),
        description: error instanceof Error ? error.message : t('common.unknownError'),
        status: 'error',
        duration: 5000,
        isClosable: true,
      })
    } finally {
      setProtectingFiles(prev => {
        const next = new Set(prev)
        next.delete(filename)
        return next
      })
    }
  }

  // 下载文件
  const handleDownload = async (filename: string) => {
    setDownloadingFiles(prev => new Set(prev).add(filename))
    try {
      await downloadKlineFile(filename)
      toast({
        title: t('common.success'),
        description: '文件下载成功',
        status: 'success',
        duration: 3000,
        isClosable: true,
      })
    } catch (error) {
      console.error('下载失败:', error)
      toast({
        title: t('common.error'),
        description: error instanceof Error ? error.message : t('common.unknownError'),
        status: 'error',
        duration: 5000,
        isClosable: true,
      })
    } finally {
      setDownloadingFiles(prev => {
        const next = new Set(prev)
        next.delete(filename)
        return next
      })
    }
  }

  // 格式化文件大小
  const formatFileSize = (bytes: number): string => {
    if (bytes < 1024) return `${bytes} B`
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(2)} KB`
    return `${(bytes / (1024 * 1024)).toFixed(2)} MB`
  }

  // 格式化日期
  const formatDate = (dateStr: string): string => {
    try {
      const date = new Date(dateStr)
      return date.toLocaleString('zh-CN')
    } catch {
      return dateStr
    }
  }

  // 获取间隔标签颜色
  const getIntervalColor = (interval: string): string => {
    switch (interval) {
      case 'tick':
        return 'blue'
      case '1m':
        return 'green'
      case '1h':
        return 'purple'
      default:
        return 'gray'
    }
  }

  // 过滤文件
  const filteredFiles = files.filter(file => {
    if (!searchTerm) return true
    const term = searchTerm.toLowerCase()
    return (
      file.filename.toLowerCase().includes(term) ||
      file.exchange.toLowerCase().includes(term) ||
      file.symbol.toLowerCase().includes(term) ||
      file.interval.toLowerCase().includes(term)
    )
  })

  return (
    <Box>
      <VStack spacing={6} align="stretch">
        {/* 页面标题 */}
        <Flex justify="space-between" align="center">
          <Box>
            <Heading size="lg" mb={2}>
              K线数据文件管理
            </Heading>
            <Text color="gray.600">
              管理tick级、分钟级、小时级K线数据文件，保护重要文件不被自动删除
            </Text>
          </Box>
          <Button
            leftIcon={<RepeatIcon />}
            onClick={loadFiles}
            isLoading={loading}
            loadingText="刷新中"
          >
            刷新
          </Button>
        </Flex>

        {/* 搜索框 */}
        <Card variant="outline">
          <CardBody>
            <InputGroup>
              <InputLeftElement pointerEvents="none">
                <SearchIcon color="gray.300" />
              </InputLeftElement>
              <Input
                placeholder="搜索文件名、交易所、币种..."
                value={searchTerm}
                onChange={(e) => setSearchTerm(e.target.value)}
              />
            </InputGroup>
          </CardBody>
        </Card>

        {/* 文件列表 */}
        <Card variant="outline">
          <CardHeader>
            <Heading size="sm">文件列表 ({filteredFiles.length})</Heading>
          </CardHeader>
          <CardBody>
            {loading ? (
              <Flex justify="center" py={8}>
                <Spinner size="lg" />
              </Flex>
            ) : filteredFiles.length === 0 ? (
              <Text textAlign="center" color="gray.500" py={8}>
                {searchTerm ? '没有找到匹配的文件' : '暂无文件'}
              </Text>
            ) : (
              <Box overflowX="auto">
                <Table variant="simple">
                  <Thead>
                    <Tr>
                      <Th>文件名</Th>
                      <Th>交易所</Th>
                      <Th>币种</Th>
                      <Th>间隔</Th>
                      <Th>订单深度</Th>
                      <Th>文件大小</Th>
                      <Th>修改时间</Th>
                      <Th>操作</Th>
                    </Tr>
                  </Thead>
                  <Tbody>
                    {filteredFiles.map((file) => (
                      <Tr key={file.filename}>
                        <Td>
                          <Text fontSize="sm" fontFamily="mono">
                            {file.filename}
                          </Text>
                        </Td>
                        <Td>
                          <Badge colorScheme="blue">{file.exchange}</Badge>
                        </Td>
                        <Td>
                          <Badge colorScheme="green">{file.symbol}</Badge>
                        </Td>
                        <Td>
                          <Badge colorScheme={getIntervalColor(file.interval)}>
                            {file.interval}
                          </Badge>
                        </Td>
                        <Td>
                          {file.has_depth ? (
                            <Badge colorScheme="purple">是</Badge>
                          ) : (
                            <Badge colorScheme="gray">否</Badge>
                          )}
                        </Td>
                        <Td>{formatFileSize(file.file_size)}</Td>
                        <Td>
                          <Text fontSize="sm">{formatDate(file.modified_at)}</Text>
                        </Td>
                        <Td>
                          <HStack spacing={2}>
                            <Tooltip label={file.is_protected ? '取消保护' : '保护文件'}>
                              <IconButton
                                aria-label={file.is_protected ? '取消保护' : '保护文件'}
                                icon={<StarIcon />}
                                colorScheme={file.is_protected ? 'yellow' : 'gray'}
                                variant={file.is_protected ? 'solid' : 'outline'}
                                size="sm"
                                onClick={() => toggleProtection(file.filename, file.is_protected)}
                                isLoading={protectingFiles.has(file.filename)}
                              />
                            </Tooltip>
                            <Tooltip label="下载文件">
                              <IconButton
                                aria-label="下载文件"
                                icon={<DownloadIcon />}
                                colorScheme="blue"
                                variant="outline"
                                size="sm"
                                onClick={() => handleDownload(file.filename)}
                                isLoading={downloadingFiles.has(file.filename)}
                              />
                            </Tooltip>
                          </HStack>
                        </Td>
                      </Tr>
                    ))}
                  </Tbody>
                </Table>
              </Box>
            )}
          </CardBody>
        </Card>

        {/* 说明信息 */}
        <Card variant="outline" bg="blue.50" borderColor="blue.200">
          <CardBody>
            <VStack align="stretch" spacing={2}>
              <Text fontWeight="medium" color="blue.700">
                说明：
              </Text>
              <Text fontSize="sm" color="blue.600">
                • tick级数据：最新24小时的tick级K线数据，每分钟更新
              </Text>
              <Text fontSize="sm" color="blue.600">
                • 分钟级数据：带订单深度的1分钟K线数据，每分钟更新
              </Text>
              <Text fontSize="sm" color="blue.600">
                • 小时级数据：带订单深度的1小时K线数据，每小时更新
              </Text>
              <Text fontSize="sm" color="blue.600">
                • 文件保护：被保护的文件不会被7天自动清理机制删除
              </Text>
              <Text fontSize="sm" color="blue.600">
                • 未保护的文件会在7天后自动删除
              </Text>
            </VStack>
          </CardBody>
        </Card>
      </VStack>
    </Box>
  )
}

export default KlineFilesManager
