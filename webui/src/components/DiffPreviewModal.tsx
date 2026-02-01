import React, { useMemo } from 'react'
import {
  Modal,
  ModalOverlay,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalFooter,
  ModalCloseButton,
  Button,
  Box,
  Text,
  HStack,
  VStack,
  Badge,
  useColorMode,
  Tabs,
  TabList,
  Tab,
  TabPanels,
  TabPanel,
} from '@chakra-ui/react'
import { CheckIcon, CloseIcon } from '@chakra-ui/icons'
import ReactDiffViewer, { DiffMethod } from 'react-diff-viewer-continued'

interface DiffPreviewModalProps {
  isOpen: boolean
  onClose: () => void
  onConfirm?: () => void
  oldValue: string
  newValue: string
  oldTitle?: string
  newTitle?: string
  title?: string
  confirmText?: string
  cancelText?: string
  showConfirmButton?: boolean
  isLoading?: boolean
}

const DiffPreviewModal: React.FC<DiffPreviewModalProps> = ({
  isOpen,
  onClose,
  onConfirm,
  oldValue,
  newValue,
  oldTitle = '原配置',
  newTitle = '新配置',
  title = '配置变更預览',
  confirmText = '确认应用',
  cancelText = '取消',
  showConfirmButton = true,
  isLoading = false,
}) => {
  const { colorMode } = useColorMode()

  // 计算变更统计
  const diffStats = useMemo(() => {
    const oldLines = oldValue.split('\n')
    const newLines = newValue.split('\n')
    
    let added = 0
    let removed = 0
    let modified = 0

    // 简單的行级别比较
    const maxLines = Math.max(oldLines.length, newLines.length)
    for (let i = 0; i < maxLines; i++) {
      const oldLine = oldLines[i] || ''
      const newLine = newLines[i] || ''
      
      if (oldLine !== newLine) {
        if (!oldLine && newLine) {
          added++
        } else if (oldLine && !newLine) {
          removed++
        } else {
          modified++
        }
      }
    }

    return { added, removed, modified, total: added + removed + modified }
  }, [oldValue, newValue])

  // 自定义样式
  const diffStyles = useMemo(() => ({
    variables: {
      light: {
        diffViewerBackground: '#fff',
        addedBackground: '#e6ffec',
        addedColor: '#24292f',
        removedBackground: '#ffebe9',
        removedColor: '#24292f',
        wordAddedBackground: '#abf2bc',
        wordRemovedBackground: '#ff8182',
        addedGutterBackground: '#ccffd8',
        removedGutterBackground: '#ffd7d5',
        gutterBackground: '#f6f8fa',
        gutterBackgroundDark: '#f0f1f2',
        highlightBackground: '#fffbdd',
        highlightGutterBackground: '#fff5b1',
        codeFoldGutterBackground: '#dbedff',
        codeFoldBackground: '#f1f8ff',
        emptyLineBackground: '#fafbfc',
      },
      dark: {
        diffViewerBackground: '#1a1a2e',
        addedBackground: '#1a4721',
        addedColor: '#adbac7',
        removedBackground: '#542426',
        removedColor: '#adbac7',
        wordAddedBackground: '#2ea043',
        wordRemovedBackground: '#f85149',
        addedGutterBackground: '#1a4721',
        removedGutterBackground: '#542426',
        gutterBackground: '#22272e',
        gutterBackgroundDark: '#1c2128',
        highlightBackground: '#3d3d0080',
        highlightGutterBackground: '#3d3d0080',
        codeFoldGutterBackground: '#1c2128',
        codeFoldBackground: '#22272e',
        emptyLineBackground: '#22272e',
      },
    },
    line: {
      padding: '4px 8px',
      fontSize: '13px',
      fontFamily: 'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace',
    },
    gutter: {
      padding: '0 8px',
      minWidth: '40px',
    },
    contentText: {
      fontSize: '13px',
      lineHeight: '1.5',
    },
  }), [])

  return (
    <Modal isOpen={isOpen} onClose={onClose} size="6xl" scrollBehavior="inside">
      <ModalOverlay />
      <ModalContent maxW="95vw" maxH="90vh">
        <ModalHeader>
          <HStack justify="space-between" align="center">
            <Text>{title}</Text>
            <HStack spacing={3}>
              {diffStats.added > 0 && (
                <Badge colorScheme="green" fontSize="sm">
                  +{diffStats.added} 新增
                </Badge>
              )}
              {diffStats.removed > 0 && (
                <Badge colorScheme="red" fontSize="sm">
                  -{diffStats.removed} 刪除
                </Badge>
              )}
              {diffStats.modified > 0 && (
                <Badge colorScheme="yellow" fontSize="sm">
                  ~{diffStats.modified} 修改
                </Badge>
              )}
            </HStack>
          </HStack>
        </ModalHeader>
        <ModalCloseButton />
        
        <ModalBody p={0}>
          <Tabs variant="enclosed" size="sm">
            <TabList px={4} pt={2}>
              <Tab>並排對比</Tab>
              <Tab>统一視图</Tab>
            </TabList>
            <TabPanels>
              {/* 並排對比視图 */}
              <TabPanel p={0}>
                <Box 
                  overflowX="auto" 
                  maxH="60vh" 
                  overflowY="auto"
                  borderTop="1px solid"
                  borderColor={colorMode === 'dark' ? 'gray.600' : 'gray.200'}
                >
                  <HStack spacing={0} align="stretch" mb={2} px={4} pt={2}>
                    <Box flex={1} textAlign="center">
                      <Text fontWeight="semibold" fontSize="sm" color="gray.500">
                        {oldTitle}
                      </Text>
                    </Box>
                    <Box flex={1} textAlign="center">
                      <Text fontWeight="semibold" fontSize="sm" color="gray.500">
                        {newTitle}
                      </Text>
                    </Box>
                  </HStack>
                  <ReactDiffViewer
                    oldValue={oldValue}
                    newValue={newValue}
                    splitView={true}
                    showDiffOnly={false}
                    useDarkTheme={colorMode === 'dark'}
                    compareMethod={DiffMethod.LINES}
                    styles={diffStyles}
                    leftTitle=""
                    rightTitle=""
                  />
                </Box>
              </TabPanel>
              
              {/* 统一視图 */}
              <TabPanel p={0}>
                <Box 
                  overflowX="auto" 
                  maxH="60vh" 
                  overflowY="auto"
                  borderTop="1px solid"
                  borderColor={colorMode === 'dark' ? 'gray.600' : 'gray.200'}
                >
                  <ReactDiffViewer
                    oldValue={oldValue}
                    newValue={newValue}
                    splitView={false}
                    showDiffOnly={false}
                    useDarkTheme={colorMode === 'dark'}
                    compareMethod={DiffMethod.LINES}
                    styles={diffStyles}
                    leftTitle={oldTitle}
                    rightTitle={newTitle}
                  />
                </Box>
              </TabPanel>
            </TabPanels>
          </Tabs>
        </ModalBody>

        <ModalFooter borderTop="1px solid" borderColor={colorMode === 'dark' ? 'gray.600' : 'gray.200'}>
          <HStack spacing={3}>
            <Button variant="ghost" onClick={onClose} leftIcon={<CloseIcon />}>
              {cancelText}
            </Button>
            {showConfirmButton && onConfirm && (
              <Button
                colorScheme="blue"
                onClick={onConfirm}
                leftIcon={<CheckIcon />}
                isLoading={isLoading}
                loadingText="应用中..."
              >
                {confirmText}
              </Button>
            )}
          </HStack>
        </ModalFooter>
      </ModalContent>
    </Modal>
  )
}

export default DiffPreviewModal
