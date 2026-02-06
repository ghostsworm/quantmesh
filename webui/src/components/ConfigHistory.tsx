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
  const { t, i18n } = useTranslation()
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
      setError(err instanceof Error ? err.message : t('configHistory.loadHistoryFailed'))
    } finally {
      setLoading(false)
    }
  }, [t])

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
        title: t('configHistory.loadFailed'),
        description: err instanceof Error ? err.message : t('configHistory.getVersionDetailFailed'),
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
        title: t('configHistory.diffFailed'),
        description: err instanceof Error ? err.message : t('configHistory.getVersionDiffFailed'),
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
        title: t('configHistory.diffFailed'),
        description: err instanceof Error ? err.message : t('configHistory.getVersionDiffFailed'),
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
        title: t('configHistory.restoreSuccess'),
        description: t('configHistory.restoredToVersion', { version: restoreVersion }),
        status: 'success',
        duration: 3000,
      })

      restoreModal.onClose()
      loadHistory() // 刷新列表
      onRestore?.() // 通知父组件
    } catch (err) {
      toast({
        title: t('configHistory.restoreFailed'),
        description: err instanceof Error ? err.message : t('configHistory.restoreVersionFailed'),
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
    return date.toLocaleString(i18n.language, {
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
          <Text color="gray.500">{t('configHistory.loadingHistory')}</Text>
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
          {t('common.retry')}
        </Button>
      </Alert>
    )
  }

  if (histories.length === 0) {
    return (
      <Center py={10}>
        <VStack spacing={3}>
          <TimeIcon boxSize={10} color="gray.400" />
          <Text color="gray.500">{t('configHistory.noHistory')}</Text>
          <Text fontSize="sm" color="gray.400">
            {t('configHistory.autoSaveHint')}
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
          <Text fontWeight="semibold">{t('configHistory.title')}</Text>
          <Badge colorScheme="blue">{t('configHistory.versionCount', { count: total })}</Badge>
        </HStack>
        <Button
          size="sm"
          leftIcon={<RepeatIcon />}
          onClick={loadHistory}
          variant="ghost"
        >
          {t('common.refresh')}
        </Button>
      </HStack>

      {/* 历史記錄表格 */}
      <TableContainer>
        <Table size="sm">
          <Thead>
            <Tr>
              <Th>{t('configHistory.version')}</Th>
              <Th>{t('configHistory.time')}</Th>
              <Th>{t('configHistory.description')}</Th>
              <Th>{t('configHistory.size')}</Th>
              <Th>{t('common.actions')}</Th>
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
                    <Tooltip label={t('configHistory.viewThisVersion')}>
                      <IconButton
                        aria-label={t('configHistory.view')}
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
                        aria-label={t('configHistory.diff')}
                        icon={<InfoIcon />}
                        size="sm"
                        variant="ghost"
                      />
                      <MenuList>
                        <MenuItem onClick={() => handleQuickDiffWithCurrent(history.version)}>
                          {t('configHistory.diffWithCurrent')}
                        </MenuItem>
                        <MenuItem onClick={() => handleOpenDiffSelect(history.version)}>
                          {t('configHistory.diffWithOther')}
                        </MenuItem>
                      </MenuList>
                    </Menu>

                    {/* 恢複按钮 */}
                    <Tooltip label={t('configHistory.restoreToThisVersion')}>
                      <IconButton
                        aria-label={t('configHistory.restore')}
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
            {t('configHistory.versionDetail', { version: viewVersion })}
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
            <Button onClick={viewModal.onClose}>{t('common.close')}</Button>
          </ModalFooter>
        </ModalContent>
      </Modal>

      {/* 版本选擇 Modal */}
      <Modal isOpen={selectVersionModal.isOpen} onClose={selectVersionModal.onClose} size="md">
        <ModalOverlay />
        <ModalContent>
          <ModalHeader>{t('configHistory.selectDiffTarget')}</ModalHeader>
          <ModalCloseButton />
          <ModalBody>
            <VStack spacing={4} align="stretch">
              <Text fontSize="sm" color="gray.500">
                {t('configHistory.diffDescription', { version: sourceVersionForDiff })}
              </Text>
              <Select
                value={selectedVersionForDiff ?? ''}
                onChange={(e) => setSelectedVersionForDiff(Number(e.target.value))}
              >
                <option value={0}>{t('configHistory.currentConfig')}</option>
                {histories
                  .filter((h) => h.version !== sourceVersionForDiff)
                  .map((h) => (
                    <option key={h.id} value={h.version}>
                      {t('configHistory.versionWithTime', { version: h.version, time: formatTime(h.created_at) })}
                    </option>
                  ))}
              </Select>
            </VStack>
          </ModalBody>
          <ModalFooter>
            <HStack spacing={3}>
              <Button variant="ghost" onClick={selectVersionModal.onClose}>
                {t('common.cancel')}
              </Button>
              <Button
                colorScheme="blue"
                onClick={handleDiff}
                isLoading={diffLoading}
              >
                {t('configHistory.diff')}
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
          oldTitle={diffData.source_version === 0 ? t('configHistory.currentConfig') : t('configHistory.versionLabel', { version: diffData.source_version })}
          newTitle={diffData.target_version === 0 ? t('configHistory.currentConfig') : t('configHistory.versionLabel', { version: diffData.target_version })}
          title={t('configHistory.versionDiff')}
          showConfirmButton={false}
        />
      )}

      {/* 恢複确认 Modal */}
      <Modal isOpen={restoreModal.isOpen} onClose={restoreModal.onClose} size="md">
        <ModalOverlay />
        <ModalContent>
          <ModalHeader>{t('configHistory.confirmRestore')}</ModalHeader>
          <ModalCloseButton />
          <ModalBody>
            <VStack spacing={4} align="stretch">
              <Alert status="warning" borderRadius="md">
                <AlertIcon />
                <AlertDescription>
                  {t('configHistory.confirmRestoreMessage', { version: restoreVersion })}
                </AlertDescription>
              </Alert>
              <Text fontSize="sm" color="gray.500">
                {t('configHistory.restoreBackupHint')}
              </Text>
            </VStack>
          </ModalBody>
          <ModalFooter>
            <HStack spacing={3}>
              <Button variant="ghost" onClick={restoreModal.onClose}>
                {t('common.cancel')}
              </Button>
              <Button
                colorScheme="blue"
                onClick={handleRestore}
                isLoading={restoreLoading}
                loadingText={t('configHistory.restoring')}
              >
                {t('configHistory.confirmRestore')}
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
            <Text>{t('common.loading')}</Text>
          </VStack>
        </Center>
      )}
    </VStack>
  )
}

export default ConfigHistory
