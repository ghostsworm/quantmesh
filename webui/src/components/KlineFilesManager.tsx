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
          description: t('klineFiles.unprotectSuccess'),
          status: 'success',
          duration: 3000,
          isClosable: true,
        })
      } else {
        await protectKlineFile(filename)
        toast({
          title: t('common.success'),
          description: t('klineFiles.protectSuccess'),
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
        description: t('klineFiles.downloadSuccess'),
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
              {t('klineFiles.title')}
            </Heading>
            <Text color="gray.600">
              {t('klineFiles.subtitle')}
            </Text>
          </Box>
          <Button
            leftIcon={<RepeatIcon />}
            onClick={loadFiles}
            isLoading={loading}
            loadingText={t('klineFiles.refreshing')}
          >
            {t('klineFiles.refresh')}
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
                placeholder={t('klineFiles.searchPlaceholder')}
                value={searchTerm}
                onChange={(e) => setSearchTerm(e.target.value)}
              />
            </InputGroup>
          </CardBody>
        </Card>

        {/* 文件列表 */}
        <Card variant="outline">
          <CardHeader>
            <Heading size="sm">{t('klineFiles.fileList')} ({filteredFiles.length})</Heading>
          </CardHeader>
          <CardBody>
            {loading ? (
              <Flex justify="center" py={8}>
                <Spinner size="lg" />
              </Flex>
            ) : filteredFiles.length === 0 ? (
              <Text textAlign="center" color="gray.500" py={8}>
                {searchTerm ? t('klineFiles.noMatchingFiles') : t('klineFiles.noFiles')}
              </Text>
            ) : (
              <Box overflowX="auto">
                <Table variant="simple">
                  <Thead>
                    <Tr>
                      <Th>{t('klineFiles.filename')}</Th>
                      <Th>{t('klineFiles.exchange')}</Th>
                      <Th>{t('klineFiles.symbol')}</Th>
                      <Th>{t('klineFiles.interval')}</Th>
                      <Th>{t('klineFiles.orderDepth')}</Th>
                      <Th>{t('klineFiles.fileSize')}</Th>
                      <Th>{t('klineFiles.modifiedTime')}</Th>
                      <Th>{t('klineFiles.actions')}</Th>
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
                            <Badge colorScheme="purple">{t('klineFiles.yes')}</Badge>
                          ) : (
                            <Badge colorScheme="gray">{t('klineFiles.no')}</Badge>
                          )}
                        </Td>
                        <Td>{formatFileSize(file.file_size)}</Td>
                        <Td>
                          <Text fontSize="sm">{formatDate(file.modified_at)}</Text>
                        </Td>
                        <Td>
                          <HStack spacing={2}>
                            <Tooltip label={file.is_protected ? t('klineFiles.unprotect') : t('klineFiles.protect')}>
                              <IconButton
                                aria-label={file.is_protected ? t('klineFiles.unprotect') : t('klineFiles.protect')}
                                icon={<StarIcon />}
                                colorScheme={file.is_protected ? 'yellow' : 'gray'}
                                variant={file.is_protected ? 'solid' : 'outline'}
                                size="sm"
                                onClick={() => toggleProtection(file.filename, file.is_protected)}
                                isLoading={protectingFiles.has(file.filename)}
                              />
                            </Tooltip>
                            <Tooltip label={t('klineFiles.download')}>
                              <IconButton
                                aria-label={t('klineFiles.download')}
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
                {t('klineFiles.infoTitle')}
              </Text>
              <Text fontSize="sm" color="blue.600">
                • {t('klineFiles.infoTick')}
              </Text>
              <Text fontSize="sm" color="blue.600">
                • {t('klineFiles.infoMinute')}
              </Text>
              <Text fontSize="sm" color="blue.600">
                • {t('klineFiles.infoHour')}
              </Text>
              <Text fontSize="sm" color="blue.600">
                • {t('klineFiles.infoProtection')}
              </Text>
              <Text fontSize="sm" color="blue.600">
                • {t('klineFiles.infoUnprotected')}
              </Text>
            </VStack>
          </CardBody>
        </Card>
      </VStack>
    </Box>
  )
}

export default KlineFilesManager
