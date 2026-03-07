import React, { useState } from 'react'
import {
  Box,
  Button,
  Card,
  CardBody,
  CardHeader,
  VStack,
  HStack,
  Text,
  Heading,
  useToast,
  Alert,
  AlertIcon,
  FormControl,
  FormLabel,
  Input,
  Textarea,
  Modal,
  ModalOverlay,
  ModalContent,
  ModalHeader,
  ModalFooter,
  ModalBody,
  ModalCloseButton,
  Flex,
  Spacer,
  IconButton,
  Divider,
  Code,
  useDisclosure,
} from '@chakra-ui/react'
import { DownloadIcon, UploadIcon, CopyIcon, CheckIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import {
  exportBotConfig,
  importBotConfig,
  BotConfigFile,
} from '../services/api'

interface ConfigImportExportProps {
  botId: string
  botName: string
  onConfigUpdate?: () => void
}

const ConfigImportExport: React.FC<ConfigImportExportProps> = ({ botId, botName, onConfigUpdate }) => {
  const { t } = useTranslation()
  const toast = useToast()
  const { isOpen: isExportOpen, onOpen: onExportOpen, onClose: onExportClose } = useDisclosure()
  const { isOpen: isImportOpen, onOpen: onImportOpen, onClose: onImportClose } = useDisclosure()

  const [exportedConfig, setExportedConfig] = useState<string>('')
  const [importConfig, setImportConfig] = useState<string>('')
  const [copied, setCopied] = useState(false)
  const [loading, setLoading] = useState(false)

  // 导出配置
  const handleExport = async () => {
    try {
      setLoading(true)
      const configJson = await exportBotConfig(botId)
      setExportedConfig(configJson)
      onExportOpen()
    } catch (error) {
      console.error('Failed to export config:', error)
      toast({
        title: t('bot.export_failed'),
        description: error instanceof Error ? error.message : String(error),
        status: 'error',
        duration: 5000,
        isClosable: true,
      })
    } finally {
      setLoading(false)
    }
  }

  // 复制到剪贴板
  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(exportedConfig)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
      toast({
        title: t('bot.config_copied'),
        status: 'success',
        duration: 2000,
        isClosable: true,
      })
    } catch (error) {
      toast({
        title: t('bot.copy_failed'),
        status: 'error',
        duration: 3000,
        isClosable: true,
      })
    }
  }

  // 下载配置文件
  const handleDownload = () => {
    const blob = new Blob([exportedConfig], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `bot_${botId}_config.json`
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(url)

    toast({
      title: t('bot.config_downloaded'),
      status: 'success',
      duration: 2000,
      isClosable: true,
    })
  }

  // 导入配置
  const handleImport = async () => {
    if (!importConfig.trim()) {
      toast({
        title: t('bot.please_input_config'),
        status: 'warning',
        duration: 3000,
        isClosable: true,
      })
      return
    }

    try {
      setLoading(true)

      // 验证 JSON 格式
      let parsedConfig: BotConfigFile
      try {
        parsedConfig = JSON.parse(importConfig)
      } catch (error) {
        throw new Error(t('bot.invalid_json_format'))
      }

      // 验证配置结构
      if (!parsedConfig.bot_id || !parsedConfig.strategies || !Array.isArray(parsedConfig.strategies)) {
        throw new Error(t('bot.invalid_config_structure'))
      }

      await importBotConfig(botId, importConfig)

      toast({
        title: t('bot.config_imported'),
        description: t('bot.config_imported_desc'),
        status: 'success',
        duration: 3000,
        isClosable: true,
      })

      setImportConfig('')
      onImportClose()
      onConfigUpdate?.()
    } catch (error) {
      console.error('Failed to import config:', error)
      toast({
        title: t('bot.import_failed'),
        description: error instanceof Error ? error.message : String(error),
        status: 'error',
        duration: 5000,
        isClosable: true,
      })
    } finally {
      setLoading(false)
    }
  }

  // 从文件加载
  const handleFileLoad = (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0]
    if (!file) return

    const reader = new FileReader()
    reader.onload = (e) => {
      const content = e.target?.result as string
      setImportConfig(content)
    }
    reader.onerror = () => {
      toast({
        title: t('bot.file_read_failed'),
        status: 'error',
        duration: 3000,
        isClosable: true,
      })
    }
    reader.readAsText(file)
  }

  return (
    <>
      <Card>
        <CardHeader>
          <Heading size="md">{t('bot.config_import_export')}</Heading>
          <Text fontSize="sm" color="gray.500" mt={1}>
            {t('bot.config_import_export_desc')}
          </Text>
        </CardHeader>

        <CardBody>
          <VStack spacing={4}>
            {/* 导入导出说明 */}
            <Alert status="info">
              <AlertIcon />
              <Box>
                <Text fontWeight="bold">{t('bot.config_backup_title')}</Text>
                <Text fontSize="sm">{t('bot.config_backup_desc')}</Text>
              </Box>
            </Alert>

            {/* 操作按钮 */}
            <HStack spacing={4} width="100%">
              <Button
                leftIcon={<DownloadIcon />}
                onClick={handleExport}
                isLoading={loading}
                flex={1}
                colorScheme="blue"
              >
                {t('bot.export_config')}
              </Button>
              <Button
                leftIcon={<UploadIcon />}
                onClick={onImportOpen}
                flex={1}
                colorScheme="green"
              >
                {t('bot.import_config')}
              </Button>
            </HStack>
          </VStack>
        </CardBody>
      </Card>

      {/* 导出对话框 */}
      <Modal isOpen={isExportOpen} onClose={onExportClose} size="xl">
        <ModalOverlay />
        <ModalContent>
          <ModalHeader>
            {t('bot.export_config')} - {botName}
          </ModalHeader>
          <ModalCloseButton />

          <ModalBody>
            <VStack spacing={4} align="stretch">
              <Alert status="success">
                <AlertIcon />
                <Box>
                  <Text fontWeight="bold">{t('bot.config_exported')}</Text>
                  <Text fontSize="sm">{t('bot.config_exported_desc')}</Text>
                </Box>
              </Alert>

              <FormControl>
                <FormLabel>{t('bot.config_json')}</FormLabel>
                <Box position="relative">
                  <Code
                    display="block"
                    width="100%"
                    p={4}
                    bg="gray.50"
                    borderRadius="md"
                    fontSize="sm"
                    maxHeight="400px"
                    overflow="auto"
                    whiteSpace="pre"
                  >
                    {exportedConfig}
                  </Code>
                  <IconButton
                    icon={copied ? <CheckIcon /> : <CopyIcon />}
                    aria-label={t('bot.copy')}
                    position="absolute"
                    top={2}
                    right={2}
                    size="sm"
                    colorScheme={copied ? 'green' : 'gray'}
                    onClick={handleCopy}
                  />
                </Box>
              </FormControl>
            </VStack>
          </ModalBody>

          <ModalFooter>
            <Button variant="ghost" mr={3} onClick={onExportClose}>
              {t('common.close')}
            </Button>
            <Button colorScheme="blue" onClick={handleDownload} leftIcon={<DownloadIcon />}>
              {t('bot.download')}
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>

      {/* 导入对话框 */}
      <Modal isOpen={isImportOpen} onClose={onImportClose} size="xl">
        <ModalOverlay />
        <ModalContent>
          <ModalHeader>
            {t('bot.import_config')} - {botName}
          </ModalHeader>
          <ModalCloseButton />

          <ModalBody>
            <VStack spacing={4} align="stretch">
              <Alert status="warning">
                <AlertIcon />
                <Box>
                  <Text fontWeight="bold">{t('bot.import_warning_title')}</Text>
                  <Text fontSize="sm">{t('bot.import_warning_desc')}</Text>
                </Box>
              </Alert>

              {/* 文件上传 */}
              <FormControl>
                <FormLabel>{t('bot.load_from_file')}</FormLabel>
                <Input
                  type="file"
                  accept=".json"
                  onChange={handleFileLoad}
                  size="sm"
                />
              </FormControl>

              <Divider />

              {/* 手动输入 */}
              <FormControl>
                <FormLabel>{t('bot.paste_config_json')}</FormLabel>
                <Textarea
                  value={importConfig}
                  onChange={(e) => setImportConfig(e.target.value)}
                  placeholder={t('bot.paste_config_placeholder')}
                  height="300px"
                  fontFamily="mono"
                  fontSize="sm"
                />
              </FormControl>
            </VStack>
          </ModalBody>

          <ModalFooter>
            <Button variant="ghost" mr={3} onClick={onImportClose}>
              {t('common.cancel')}
            </Button>
            <Button
              colorScheme="green"
              onClick={handleImport}
              isLoading={loading}
              leftIcon={<UploadIcon />}
            >
              {t('bot.import')}
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>
    </>
  )
}

export default ConfigImportExport
