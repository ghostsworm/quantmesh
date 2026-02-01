import React, { useState, useEffect, useCallback } from 'react'
import {
  Box,
  VStack,
  HStack,
  Text,
  Button,
  Table,
  Thead,
  Tbody,
  Tr,
  Th,
  Td,
  TableContainer,
  Badge,
  Spinner,
  Center,
  Alert,
  AlertIcon,
  AlertDescription,
  Modal,
  ModalOverlay,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalFooter,
  ModalCloseButton,
  Select,
  useDisclosure,
  useToast,
  IconButton,
  Tooltip,
  Menu,
  MenuButton,
  MenuList,
  MenuItem,
} from '@chakra-ui/react'
import { 
  ViewIcon, 
  RepeatIcon, 
  ChevronDownIcon, 
  TimeIcon,
  InfoIcon,
} from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import {
  getConfigHistory,
  getConfigHistoryDetail,
  restoreConfigHistory,
  diffConfigHistory,
  getConfigYAML,
  ConfigHistoryItem,
  HistoryDiffResponse,
} from '../services/config'
import YamlEditor from './YamlEditor'
import DiffPreviewModal from './DiffPreviewModal'

interface ConfigHistoryProps {
  onRestore?: () => void  // 恢複成功后的回呼
}

const ConfigHistory: React.FC<ConfigHistoryProps> = ({ onRestore }) => {
  const { t } = useTranslation()
  const toast = useToast()

  // 状態
  const [histories, setHistories] = useState<ConfigHistoryItem[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [total, setTotal] = useState(0)

  // 查看版本 Modal
  const viewModal = useDisclosure()
  const [viewContent, setViewContent] = useState('')
  const [viewVersion, setViewVersion] = useState<number | null>(null)
  const [viewLoading, setViewLoading] = useState(false)

  // 恢複确认 Modal
  const restoreModal = useDisclosure()
  const [restoreVersion, setRestoreVersion] = useState<number | null>(null)
  const [restoreLoading, setRestoreLoading] = useState(false)

  // Diff Modal
  const diffModal = useDisclosure()
  const [diffData, setDiffData] = useState<HistoryDiffResponse | null>(null)
  const [diffLoading, setDiffLoading] = useState(false)
  const [selectedVersionForDiff, setSelectedVersionForDiff] = useState<number | null>(null)

  // 版本选擇 Modal（用於选擇對比目標）
  const selectVersionModal = useDisclosure()
  const [sourceVersionForDiff, setSourceVersionForDiff] = useState<number>(0)

  // 加載历史列表
  const loadHistory = useCallback(async () => {
    try {
      setLoading(true)
      setError(null)
      const result = await getConfigHistory(50, 0)
      setHistories(result.histories || [])
      setTotal(result.total)
    } catch (err) {
      setError(err instanceof Error ? err.message : '加載历史記錄失败')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    loadHistory()
  }, [loadHistory])

  // 查看版本详情
  const handleView = async (version: number) => {
    try {
      setViewLoading(true)
      setViewVersion(version)
      viewModal.onOpen()
      
      const detail = await getConfigHistoryDetail(version)
      setViewContent(detail.content)
    } catch (err) {
      toast({
        title: '加載失败',
        description: err instanceof Error ? err.message : '獲取版本详情失败',
        status: 'error',
        duration: 3000,
      })
      viewModal.onClose()
    } finally {
      setViewLoading(false)
    }
  }

  // 打开版本选擇對话框
  const handleOpenDiffSelect = (sourceVersion: number) => {
    setSourceVersionForDiff(sourceVersion)
    setSelectedVersionForDiff(0) // 默认选擇當前版本
    selectVersionModal.onOpen()
  }

  // 執行版本對比
  const handleDiff = async () => {
    if (selectedVersionForDiff === null) return

    try {
      setDiffLoading(true)
      selectVersionModal.onClose()
      
      const result = await diffConfigHistory(sourceVersionForDiff, selectedVersionForDiff)
      setDiffData(result)
      diffModal.onOpen()
    } catch (err) {
      toast({
        title: '對比失败',
        description: err instanceof Error ? err.message : '獲取版本差异失败',
        status: 'error',
        duration: 3000,
      })
    } finally {
      setDiffLoading(false)
    }
  }

  // 快速對比：與當前版本對比
  const handleQuickDiffWithCurrent = async (version: number) => {
    try {
      setDiffLoading(true)
      
      const result = await diffConfigHistory(version, 0)
      setDiffData(result)
      diffModal.onOpen()
    } catch (err) {
      toast({
        title: '對比失败',
        description: err instanceof Error ? err.message : '獲取版本差异失败',
        status: 'error',
        duration: 3000,
      })
    } finally {
      setDiffLoading(false)
    }
  }

  // 恢複版本
  const handleRestore = async () => {
    if (restoreVersion === null) return

    try {
      setRestoreLoading(true)
      await restoreConfigHistory(restoreVersion)
      
      toast({
        title: '恢複成功',
        description: `已恢複到版本 ${restoreVersion}`,
        status: 'success',
        duration: 3000,
      })

      restoreModal.onClose()
      loadHistory() // 刷新列表
      onRestore?.() // 通知父组件
    } catch (err) {
      toast({
        title: '恢複失败',
        description: err instanceof Error ? err.message : '恢複版本失败',
        status: 'error',
        duration: 3000,
      })
    } finally {
      setRestoreLoading(false)
    }
  }

  // 格式化時间
  const formatTime = (timeStr: string) => {
    const date = new Date(timeStr)
    return date.toLocaleString('zh-CN', {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    })
  }

  // 格式化文件大小
  const formatSize = (bytes: number) => {
    if (bytes < 1024) return `${bytes} B`
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
    return `${(bytes / 1024 / 1024).toFixed(1)} MB`
  }

  if (loading) {
    return (
      <Center py={10}>
        <VStack spacing={3}>
          <Spinner size="lg" />
          <Text color="gray.500">加載历史記錄...</Text>
        </VStack>
      </Center>
    )
  }

  if (error) {
    return (
      <Alert status="error" borderRadius="md">
        <AlertIcon />
        <AlertDescription>{error}</AlertDescription>
        <Button size="sm" ml="auto" onClick={loadHistory}>
          重試
        </Button>
      </Alert>
    )
  }

  if (histories.length === 0) {
    return (
      <Center py={10}>
        <VStack spacing={3}>
          <TimeIcon boxSize={10} color="gray.400" />
          <Text color="gray.500">暂無历史記錄</Text>
          <Text fontSize="sm" color="gray.400">
            配置修改后會自动保存历史版本
          </Text>
        </VStack>
      </Center>
    )
  }

  return (
    <VStack spacing={4} align="stretch">
      {/* 標题和刷新按钮 */}
      <HStack justify="space-between">
        <HStack spacing={2}>
          <Text fontWeight="semibold">配置历史版本</Text>
          <Badge colorScheme="blue">{total} 個版本</Badge>
        </HStack>
        <Button
          size="sm"
          leftIcon={<RepeatIcon />}
          onClick={loadHistory}
          variant="ghost"
        >
          刷新
        </Button>
      </HStack>

      {/* 历史記錄表格 */}
      <TableContainer>
        <Table size="sm">
          <Thead>
            <Tr>
              <Th>版本</Th>
              <Th>時间</Th>
              <Th>描述</Th>
              <Th>大小</Th>
              <Th>操作</Th>
            </Tr>
          </Thead>
          <Tbody>
            {histories.map((history) => (
              <Tr key={history.id}>
                <Td>
                  <Badge colorScheme="gray">v{history.version}</Badge>
                </Td>
                <Td>
                  <Text fontSize="sm">{formatTime(history.created_at)}</Text>
                </Td>
                <Td>
                  <Text fontSize="sm" maxW="300px" isTruncated>
                    {history.description || '-'}
                  </Text>
                </Td>
                <Td>
                  <Text fontSize="sm" color="gray.500">
                    {formatSize(history.size)}
                  </Text>
                </Td>
                <Td>
                  <HStack spacing={1}>
                    {/* 查看按钮 */}
                    <Tooltip label="查看此版本">
                      <IconButton
                        aria-label="查看"
                        icon={<ViewIcon />}
                        size="sm"
                        variant="ghost"
                        onClick={() => handleView(history.version)}
                      />
                    </Tooltip>

                    {/* 對比菜單 */}
                    <Menu>
                      <MenuButton
                        as={IconButton}
                        aria-label="對比"
                        icon={<InfoIcon />}
                        size="sm"
                        variant="ghost"
                      />
                      <MenuList>
                        <MenuItem onClick={() => handleQuickDiffWithCurrent(history.version)}>
                          與當前配置對比
                        </MenuItem>
                        <MenuItem onClick={() => handleOpenDiffSelect(history.version)}>
                          與其他版本對比...
                        </MenuItem>
                      </MenuList>
                    </Menu>

                    {/* 恢複按钮 */}
                    <Tooltip label="恢複到此版本">
                      <IconButton
                        aria-label="恢複"
                        icon={<RepeatIcon />}
                        size="sm"
                        variant="ghost"
                        colorScheme="blue"
                        onClick={() => {
                          setRestoreVersion(history.version)
                          restoreModal.onOpen()
                        }}
                      />
                    </Tooltip>
                  </HStack>
                </Td>
              </Tr>
            ))}
          </Tbody>
        </Table>
      </TableContainer>

      {/* 查看版本 Modal */}
      <Modal isOpen={viewModal.isOpen} onClose={viewModal.onClose} size="4xl">
        <ModalOverlay />
        <ModalContent maxH="80vh">
          <ModalHeader>
            版本 {viewVersion} 详情
          </ModalHeader>
          <ModalCloseButton />
          <ModalBody>
            {viewLoading ? (
              <Center py={10}>
                <Spinner size="lg" />
              </Center>
            ) : (
              <YamlEditor
                value={viewContent}
                readOnly={true}
                height="50vh"
                showValidationStatus={false}
              />
            )}
          </ModalBody>
          <ModalFooter>
            <Button onClick={viewModal.onClose}>关闭</Button>
          </ModalFooter>
        </ModalContent>
      </Modal>

      {/* 版本选擇 Modal */}
      <Modal isOpen={selectVersionModal.isOpen} onClose={selectVersionModal.onClose} size="md">
        <ModalOverlay />
        <ModalContent>
          <ModalHeader>选擇對比目標</ModalHeader>
          <ModalCloseButton />
          <ModalBody>
            <VStack spacing={4} align="stretch">
              <Text fontSize="sm" color="gray.500">
                將版本 {sourceVersionForDiff} 與以下版本進行對比：
              </Text>
              <Select
                value={selectedVersionForDiff ?? ''}
                onChange={(e) => setSelectedVersionForDiff(Number(e.target.value))}
              >
                <option value={0}>當前配置</option>
                {histories
                  .filter((h) => h.version !== sourceVersionForDiff)
                  .map((h) => (
                    <option key={h.id} value={h.version}>
                      版本 {h.version} - {formatTime(h.created_at)}
                    </option>
                  ))}
              </Select>
            </VStack>
          </ModalBody>
          <ModalFooter>
            <HStack spacing={3}>
              <Button variant="ghost" onClick={selectVersionModal.onClose}>
                取消
              </Button>
              <Button
                colorScheme="blue"
                onClick={handleDiff}
                isLoading={diffLoading}
              >
                對比
              </Button>
            </HStack>
          </ModalFooter>
        </ModalContent>
      </Modal>

      {/* Diff Modal */}
      {diffData && (
        <DiffPreviewModal
          isOpen={diffModal.isOpen}
          onClose={diffModal.onClose}
          oldValue={diffData.source_content}
          newValue={diffData.target_content}
          oldTitle={diffData.source_version === 0 ? '當前配置' : `版本 ${diffData.source_version}`}
          newTitle={diffData.target_version === 0 ? '當前配置' : `版本 ${diffData.target_version}`}
          title="版本對比"
          showConfirmButton={false}
        />
      )}

      {/* 恢複确认 Modal */}
      <Modal isOpen={restoreModal.isOpen} onClose={restoreModal.onClose} size="md">
        <ModalOverlay />
        <ModalContent>
          <ModalHeader>确认恢複</ModalHeader>
          <ModalCloseButton />
          <ModalBody>
            <VStack spacing={4} align="stretch">
              <Alert status="warning" borderRadius="md">
                <AlertIcon />
                <AlertDescription>
                  确定要恢複到版本 {restoreVersion} 吗？當前配置將被覆盖。
                </AlertDescription>
              </Alert>
              <Text fontSize="sm" color="gray.500">
                恢複前會自动保存當前配置的备份，您可以随時恢複。
              </Text>
            </VStack>
          </ModalBody>
          <ModalFooter>
            <HStack spacing={3}>
              <Button variant="ghost" onClick={restoreModal.onClose}>
                取消
              </Button>
              <Button
                colorScheme="blue"
                onClick={handleRestore}
                isLoading={restoreLoading}
                loadingText="恢複中..."
              >
                确认恢複
              </Button>
            </HStack>
          </ModalFooter>
        </ModalContent>
      </Modal>

      {/* 全局加載遮罩 */}
      {diffLoading && (
        <Center
          position="fixed"
          top={0}
          left={0}
          right={0}
          bottom={0}
          bg="blackAlpha.300"
          zIndex={1000}
        >
          <VStack spacing={3} bg="white" p={6} borderRadius="lg" shadow="lg">
            <Spinner size="lg" />
            <Text>加載中...</Text>
          </VStack>
        </Center>
      )}
    </VStack>
  )
}

export default ConfigHistory
