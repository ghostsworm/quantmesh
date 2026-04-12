import React, { useState, useRef, useEffect } from 'react'
import {
  Box,
  Input,
  Button,
  VStack,
  HStack,
  Text,
  Flex,
  Spinner,
  Icon,
  useToast,
  IconButton,
  Tooltip,
  Image,
  CloseButton,
  List,
  ListItem,
  ListIcon,
  Divider,
  Modal,
  ModalOverlay,
  ModalContent,
  ModalBody,
  ModalCloseButton,
  useDisclosure,
} from '@chakra-ui/react'
import { IoSend, IoRefresh, IoCheckmarkCircle, IoWarning, IoImage, IoChatbubblesOutline, IoAddCircle, IoTrashOutline, IoDownloadOutline } from 'react-icons/io5'
import { MdFullscreen, MdPictureInPicture } from 'react-icons/md'
import { useTranslation } from 'react-i18next'
import { downloadMediaFromUrl, togglePictureInPicture } from './mediaUtils'

interface ChatMessage {
  id: string
  role: 'user' | 'assistant' | 'system'
  content: string
  timestamp: Date
  images?: ImageData[]
  generatedFiles?: GeneratedFile[]
  toolCalls?: ToolCall[]
  metadata?: {
    risk?: string
    confirmations?: Confirmation[]
  }
}

interface GeneratedFile {
  type: string // 'image', 'video', 'chart'
  url: string
  path: string
  filename: string
  size: number
  mime_type: string
}

interface ImageData {
  mime_type: string
  data: string // base64
}

interface ToolCall {
  id: string
  name: string
  arguments: Record<string, any>
  status: 'pending' | 'executing' | 'completed' | 'failed'
  result?: any
  error?: string
}

interface Confirmation {
  type: string
  message: string
  risk_level: string
}

interface ChatSession {
  id: string
  created_at: string
  updated_at: string
  message_count: number
}

interface AgentChatProps {
  botId?: string
  onConfigApplied?: (config: any) => void
}

const AgentChat: React.FC<AgentChatProps> = ({ botId, onConfigApplied }) => {
  const { t } = useTranslation()
  const toast = useToast()

  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [input, setInput] = useState('')
  const [isLoading, setIsLoading] = useState(false)
  const [sessionId, setSessionId] = useState<string | null>(null)
  const [configPreview, setConfigPreview] = useState<any>(null)
  const [selectedImages, setSelectedImages] = useState<ImageData[]>([])
  const [chatSessions, setChatSessions] = useState<ChatSession[]>([])
  const [historyPanelOpen, setHistoryPanelOpen] = useState(true) // 默认打开历史面板

  const messagesEndRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)

  // 自动滚动到底部
  const scrollToBottom = () => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }

  useEffect(() => {
    scrollToBottom()
  }, [messages])

  // 加载会话列表
  useEffect(() => {
    loadSessions()
  }, [])

  // 创建会话
  useEffect(() => {
    if (!sessionId) {
      createSession()
    }
  }, [])

  // 加载会话列表
  const loadSessions = async () => {
    try {
      const response = await fetch('/api/agent/sessions')
      const data = await response.json()
      setChatSessions(data.sessions || [])
    } catch (error) {
      console.error('Failed to load sessions:', error)
    }
  }

  // 切换会话
  const switchSession = async (newSessionId: string) => {
    try {
      const response = await fetch(`/api/agent/sessions/${newSessionId}/history`)
      const data = await response.json()

      setSessionId(newSessionId)

      // 转换消息格式
      const historyMessages: ChatMessage[] = data.messages.map((msg: any) => ({
        id: msg.id || generateId(),
        role: msg.role,
        content: msg.content,
        timestamp: new Date(msg.timestamp || Date.now()),
        images: msg.images,
      }))

      setMessages(historyMessages)
    } catch (error) {
      console.error('Failed to switch session:', error)
      toast({
        title: t('agent.sessionLoadFailed'),
        status: 'error',
        duration: 3000,
      })
    }
  }

  // 删除会话
  const deleteSession = async (sessionIdToDelete: string, event: React.MouseEvent) => {
    event.stopPropagation()

    try {
      const response = await fetch(`/api/agent/sessions/${sessionIdToDelete}`, {
        method: 'DELETE',
      })
      if (!response.ok) {
        throw new Error(`DELETE sessions failed: ${response.status}`)
      }

      // 若删除的是当前会话：不要立刻新建会话，否则列表会马上多一条「空聊天」，
      // 用户会误以为删除未生效。清空状态后由用户点「新建聊天」再创建。
      if (sessionIdToDelete === sessionId) {
        setSessionId(null)
        setMessages([])
        setConfigPreview(null)
      }

      await loadSessions()
      toast({
        title: t('agent.sessionDeleted'),
        status: 'success',
        duration: 2000,
      })
    } catch (error) {
      console.error('Failed to delete session:', error)
      toast({
        title: t('agent.sessionDeleteFailed'),
        status: 'error',
        duration: 3000,
      })
    }
  }

  const createSession = async () => {
    try {
      const response = await fetch('/api/agent/sessions', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          bot_id: botId,
          context: {},
        }),
      })

      const data = await response.json()
      setSessionId(data.session_id)
      setMessages([])
      setConfigPreview(null)
      await loadSessions()
    } catch (error) {
      console.error('Failed to create session:', error)
      toast({
        title: t('agent.sessionCreateFailed'),
        status: 'error',
        duration: 3000,
      })
    }
  }

  // 图片选择处理
  const handleImageSelect = (event: React.ChangeEvent<HTMLInputElement>) => {
    const files = event.target.files
    if (!files) return

    const allowedTypes = ['image/png', 'image/jpeg', 'image/gif', 'image/webp']
    const maxFileSize = 10 * 1024 * 1024 // 10MB

    Array.from(files).forEach((file) => {
      if (!allowedTypes.includes(file.type)) {
        toast({
          title: t('agent.invalidImageType'),
          description: t('agent.allowedImageTypes'),
          status: 'error',
          duration: 3000,
        })
        return
      }

      if (file.size > maxFileSize) {
        toast({
          title: t('agent.imageTooLarge'),
          description: t('agent.maxImageSize'),
          status: 'error',
          duration: 3000,
        })
        return
      }

      const reader = new FileReader()
      reader.onload = (e) => {
        const result = e.target?.result as string
        const base64Data = result.split(',')[1] // Remove data URL prefix

        const imageData: ImageData = {
          mime_type: file.type,
          data: base64Data,
        }

        setSelectedImages((prev) => [...prev, imageData])
      }
      reader.readAsDataURL(file)
    })

    // Reset input
    event.target.value = ''
  }

  // 移除图片
  const removeImage = (index: number) => {
    setSelectedImages((prev) => prev.filter((_, i) => i !== index))
  }

  const sendMessage = async () => {
    const content = input.trim()
    if ((!content && selectedImages.length === 0) || !sessionId || isLoading) return

    // 添加用户消息
    const userMessage: ChatMessage = {
      id: generateId(),
      role: 'user',
      content,
      timestamp: new Date(),
      images: selectedImages.length > 0 ? [...selectedImages] : undefined,
    }

    setMessages((prev) => [...prev, userMessage])
    setInput('')
    const imagesToSend = [...selectedImages]
    setSelectedImages([])
    setIsLoading(true)

    try {
      const requestBody: any = {
        content,
        stream: false,
      }

      // 如果有图片，添加到请求中
      if (imagesToSend.length > 0) {
        requestBody.images = imagesToSend
      }

      const response = await fetch(`/api/agent/sessions/${sessionId}/messages`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(requestBody),
      })

      const data = await response.json()

      // 添加助手消息
      const assistantMessage: ChatMessage = {
        id: generateId(),
        role: 'assistant',
        content: data.message || '',
        timestamp: new Date(),
        toolCalls: data.tool_calls?.map((tc: any) => ({
          ...tc,
          status: 'completed',
        })),
        metadata: data.metadata,
        images: data.images,
        generatedFiles: data.files,
      }

      setMessages((prev) => [...prev, assistantMessage])

      // 更新配置预览
      if (data.config_preview) {
        setConfigPreview(data.config_preview)
      }

      // 更新会话列表
      await loadSessions()

    } catch (error) {
      console.error('Failed to send message:', error)
      toast({
        title: t('agent.sendMessageFailed'),
        status: 'error',
        duration: 3000,
      })
    } finally {
      setIsLoading(false)
      inputRef.current?.focus()
    }
  }

  const handleKeyPress = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      sendMessage()
    }
  }

  const resetConversation = async () => {
    try {
      if (sessionId) {
        await fetch(`/api/agent/sessions/${sessionId}`, {
          method: 'DELETE',
        })
      }

      setMessages([])
      setConfigPreview(null)
      await createSession()

      toast({
        title: t('agent.conversationReset'),
        status: 'success',
        duration: 2000,
      })
    } catch (error) {
      console.error('Failed to reset conversation:', error)
    }
  }

  const applyConfig = async () => {
    if (!configPreview) return

    try {
      await fetch(`/api/agent/sessions/${sessionId}/apply`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
      })

      toast({
        title: t('agent.configApplied'),
        status: 'success',
        duration: 3000,
      })

      onConfigApplied?.(configPreview)
    } catch (error) {
      console.error('Failed to apply config:', error)
      toast({
        title: t('agent.configApplyFailed'),
        status: 'error',
        duration: 3000,
      })
    }
  }

  return (
    <Flex height="100%" gap={4}>
      {/* 左侧历史面板 */}
      <Box
        width="280px"
        flexShrink={0}
        borderRight="1px solid"
        borderColor="gray.200"
        pr={4}
        display={historyPanelOpen ? 'block' : 'none'}
      >
        <VStack spacing={3} align="stretch">
          {/* 标题栏 */}
          <HStack justify="space-between" align="center">
            <HStack spacing={2}>
              <Icon as={IoChatbubblesOutline} />
              <Text fontWeight="bold">{t('agent.chatHistory')}</Text>
            </HStack>
            <IconButton
              aria-label="Toggle history"
              icon={<Icon as={historyPanelOpen ? IoChatbubblesOutline : IoAddCircle} />}
              size="sm"
              variant="ghost"
              onClick={() => setHistoryPanelOpen(!historyPanelOpen)}
            />
          </HStack>

          {/* 新建聊天按钮 */}
          <Button
            leftIcon={<Icon as={IoAddCircle} />}
            colorScheme="blue"
            onClick={() => createSession()}
            width="full"
            size="sm"
          >
            {t('agent.newChat')}
          </Button>

          <Divider />

          {/* 聊天历史列表 */}
          <Box overflowY="auto" flex={1} maxHeight="calc(100vh - 200px)">
            <VStack spacing={2} align="stretch">
              {chatSessions.length === 0 ? (
                <Text color="gray.500" textAlign="center" py={4} fontSize="sm">
                  {t('agent.noHistory')}
                </Text>
              ) : (
                chatSessions
                  .sort((a, b) => new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime())
                  .map((session) => (
                    <Box
                      key={session.id}
                      onClick={() => switchSession(session.id)}
                      cursor="pointer"
                      _hover={{ bg: 'gray.100' }}
                      p={3}
                      borderRadius="md"
                      bg={session.id === sessionId ? 'blue.50' : 'transparent'}
                      transition="background 0.2s"
                    >
                      <HStack justify="space-between" align="start">
                        <VStack align="start" spacing={1} flex={1}>
                          <Text fontSize="sm" fontWeight="medium" noOfLines={1}>
                            {t('agent.chatSession')} - {new Date(session.created_at).toLocaleDateString()}
                          </Text>
                          <Text fontSize="xs" color="gray.500">
                            {new Date(session.created_at).toLocaleTimeString()}
                          </Text>
                          <Text fontSize="xs" color="gray.400">
                            {t('agent.messageCount', { count: session.message_count })}
                          </Text>
                        </VStack>
                        <IconButton
                          aria-label="Delete session"
                          icon={<Icon as={IoTrashOutline} />}
                          size="xs"
                          variant="ghost"
                          colorScheme="red"
                          onClick={(e) => deleteSession(session.id, e)}
                        />
                      </HStack>
                    </Box>
                  ))
                )}
              </VStack>
          </Box>
        </VStack>
      </Box>

      {/* 主聊天区域 */}
      <Box flex={1} display="flex" flexDirection="column" minWidth={0}>
        {/* 顶部工具栏 */}
        <HStack mb={2} justify="space-between">
          {!historyPanelOpen && (
            <Button
              leftIcon={<Icon as={IoChatbubblesOutline} />}
              variant="ghost"
              size="sm"
              onClick={() => setHistoryPanelOpen(true)}
            >
              {t('agent.chatHistory')}
            </Button>
          )}
          <Text fontSize="sm" color="gray.500" ml="auto">
            {t('agent.sessionId')}: {sessionId ? sessionId.slice(-8) : '—'}
          </Text>
        </HStack>

        {/* 消息列表 */}
        <Box
          flex={1}
          overflowY="auto"
          p={4}
          bg="gray.50"
          borderRadius="md"
        >
        <VStack spacing={4} align="stretch">
          {messages.length === 0 && (
            <Box
              textAlign="center"
              py={20}
              color="gray.500"
            >
              <Text fontSize="lg" mb={2}>
                {t('agent.welcomeMessage')}
              </Text>
              <Text fontSize="sm">
                {t('agent.welcomeHint')}
              </Text>
            </Box>
          )}

          {messages.map((message) => (
            <MessageBubble
              key={message.id}
              message={message}
            />
          ))}

          {isLoading && (
            <Flex justify="center">
              <Spinner size="sm" />
            </Flex>
          )}

          <div ref={messagesEndRef} />
        </VStack>
      </Box>

      {/* 配置预览 */}
      {configPreview && (
        <Box
          p={4}
          bg="blue.50"
          borderRadius="md"
          mt={2}
        >
          <HStack justify="space-between" mb={2}>
            <Text fontWeight="bold" fontSize="sm">
              {t('agent.configPreview')}
            </Text>
            <HStack spacing={2}>
              <Button
                size="sm"
                colorScheme="green"
                leftIcon={<Icon as={IoCheckmarkCircle} />}
                onClick={applyConfig}
              >
                {t('agent.applyConfig')}
              </Button>
              <Button
                size="sm"
                variant="ghost"
                onClick={() => setConfigPreview(null)}
              >
                {t('agent.dismiss')}
              </Button>
            </HStack>
          </HStack>
          <ConfigPreviewCard config={configPreview} />
        </Box>
      )}

      {/* 输入区域 */}
      <Box mt={4}>
        {/* 图片预览 */}
        {selectedImages.length > 0 && (
          <Flex gap={2} mb={2} flexWrap="wrap">
            {selectedImages.map((image, idx) => (
              <Box key={idx} position="relative">
                <Box border="2px solid" borderColor="blue.200" borderRadius="md" overflow="hidden">
                  <ImageWithLightbox
                    src={`data:${image.mime_type};base64,${image.data}`}
                    alt={`Preview ${idx + 1}`}
                    boxSize="60px"
                    objectFit="cover"
                  />
                </Box>
                <CloseButton
                  position="absolute"
                  top="-8px"
                  right="-8px"
                  size="sm"
                  bg="red.500"
                  color="white"
                  borderRadius="full"
                  onClick={() => removeImage(idx)}
                  _hover={{ bg: 'red.600' }}
                />
              </Box>
            ))}
          </Flex>
        )}

        <HStack spacing={2}>
          <input
            ref={fileInputRef}
            type="file"
            accept="image/png,image/jpeg,image/gif,image/webp"
            multiple
            style={{ display: 'none' }}
            onChange={handleImageSelect}
          />

          <Tooltip label={t('agent.uploadImage')}>
            <IconButton
              aria-label="Upload image"
              icon={<Icon as={IoImage} />}
              onClick={() => fileInputRef.current?.click()}
              isDisabled={isLoading || !sessionId}
            />
          </Tooltip>

          <Input
            ref={inputRef}
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyPress={handleKeyPress}
            placeholder={sessionId ? t('agent.inputPlaceholder') : t('agent.createSessionToContinue')}
            disabled={isLoading || !sessionId}
            bg="white"
          />

          <Tooltip label={t('agent.sendMessage')}>
            <IconButton
              colorScheme="blue"
              aria-label="Send message"
              icon={<Icon as={IoSend} />}
              onClick={sendMessage}
              isDisabled={
                isLoading ||
                !sessionId ||
                (!input.trim() && selectedImages.length === 0)
              }
            />
          </Tooltip>

          <Tooltip label={t('agent.resetConversation')}>
            <IconButton
              aria-label="Reset conversation"
              icon={<Icon as={IoRefresh} />}
              onClick={resetConversation}
              isDisabled={isLoading}
            />
          </Tooltip>
        </HStack>

        {/* 建议操作 */}
        {messages.length > 0 && !isLoading && (
          <HStack spacing={2} mt={2}>
            <SuggestionChips
              suggestions={[
                t('agent.suggestionApply'),
                t('agent.suggestionBacktest'),
                t('agent.suggestionExplain'),
              ]}
              onSelect={setInput}
            />
          </HStack>
        )}
      </Box>
    </Box>
    </Flex>
  )
}

/** 缩略图点击打开全屏遮罩预览，右上角关闭 */
const ImageWithLightbox: React.FC<{
  src: string
  alt: string
  maxW?: string
  maxH?: string
  boxSize?: string
  objectFit?: 'contain' | 'cover'
}> = ({ src, alt, maxW = '300px', maxH = '300px', boxSize, objectFit = 'contain' }) => {
  const { t } = useTranslation()
  const { isOpen, onOpen, onClose } = useDisclosure()

  return (
    <>
      <Image
        src={src}
        alt={alt}
        {...(boxSize ? { boxSize } : { maxW, maxH })}
        objectFit={objectFit}
        borderRadius="md"
        cursor="pointer"
        onClick={onOpen}
        role="button"
        tabIndex={0}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault()
            onOpen()
          }
        }}
        aria-label={t('agent.clickToEnlargeImage')}
      />
      <Modal isOpen={isOpen} onClose={onClose} size="full" isCentered>
        <ModalOverlay bg="blackAlpha.800" />
        <ModalContent maxW="100vw" m={0} bg="transparent" boxShadow="none">
          <ModalCloseButton
            color="white"
            size="lg"
            zIndex={2}
            top={4}
            right={4}
            aria-label={t('agent.closePreview')}
          />
          <ModalBody p={6} display="flex" justifyContent="center" alignItems="center" minH="100vh">
            <Image src={src} alt={alt} maxW="95vw" maxH="90vh" objectFit="contain" />
          </ModalBody>
        </ModalContent>
      </Modal>
    </>
  )
}

const GeneratedVideoBlock: React.FC<{ file: GeneratedFile; isUser: boolean }> = ({
  file,
  isUser,
}) => {
  const { t } = useTranslation()
  const toast = useToast()
  const inlineRef = useRef<HTMLVideoElement>(null)
  const modalRef = useRef<HTMLVideoElement>(null)
  const { isOpen, onOpen, onClose } = useDisclosure()

  useEffect(() => {
    if (!isOpen) {
      modalRef.current?.pause()
    }
  }, [isOpen])

  const runPiP = async (ref: React.RefObject<HTMLVideoElement>) => {
    const v = ref.current
    if (!v) return
    try {
      await togglePictureInPicture(v)
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : String(e)
      toast({
        title: t('agent.pipFailed'),
        description: msg,
        status: 'error',
        duration: 4000,
      })
    }
  }

  const runDownload = async () => {
    const name = file.filename?.trim() || 'video.mp4'
    const result = await downloadMediaFromUrl(file.url, name)
    if (result.ok) return
    window.open(file.url, '_blank', 'noopener,noreferrer')
    toast({
      title: t('agent.downloadFailed'),
      description: t('agent.downloadOpenedNewTabHint'),
      status: 'info',
      duration: 5000,
    })
  }

  const btnColor = isUser ? 'blue.100' : 'gray.600'
  const btnHover = isUser ? 'blue.50' : 'gray.100'

  return (
    <Box>
      <video
        ref={inlineRef}
        src={file.url}
        controls
        playsInline
        preload="metadata"
        style={{ maxWidth: '300px', maxHeight: '300px', borderRadius: '8px', display: 'block' }}
      />
      <HStack mt={2} spacing={1} flexWrap="wrap">
        <Tooltip label={t('agent.enlargePlay')}>
          <IconButton
            aria-label={t('agent.enlargePlay')}
            size="sm"
            variant="ghost"
            color={btnColor}
            _hover={{ bg: btnHover }}
            icon={<Icon as={MdFullscreen} />}
            onClick={onOpen}
          />
        </Tooltip>
        <Tooltip label={t('agent.pictureInPicture')}>
          <IconButton
            aria-label={t('agent.pictureInPicture')}
            size="sm"
            variant="ghost"
            color={btnColor}
            _hover={{ bg: btnHover }}
            icon={<Icon as={MdPictureInPicture} />}
            onClick={() => runPiP(inlineRef)}
          />
        </Tooltip>
        <Tooltip label={t('agent.downloadMedia')}>
          <IconButton
            aria-label={t('agent.downloadMedia')}
            size="sm"
            variant="ghost"
            color={btnColor}
            _hover={{ bg: btnHover }}
            icon={<Icon as={IoDownloadOutline} />}
            onClick={runDownload}
          />
        </Tooltip>
      </HStack>

      <Modal isOpen={isOpen} onClose={onClose} size="6xl" isCentered>
        <ModalOverlay bg="blackAlpha.700" />
        <ModalContent bg="black" maxW="min(96vw, 1200px)">
          <ModalCloseButton color="white" zIndex={2} aria-label={t('agent.closePreview')} />
          <ModalBody p={4} pt={10}>
            <video
              ref={modalRef}
              src={file.url}
              controls
              playsInline
              autoPlay
              style={{ width: '100%', maxHeight: 'min(85vh, 800px)', borderRadius: '8px' }}
            />
            <HStack mt={3} spacing={2} justify="center">
              <Tooltip label={t('agent.pictureInPicture')}>
                <IconButton
                  aria-label={t('agent.pictureInPicture')}
                  colorScheme="whiteAlpha"
                  icon={<Icon as={MdPictureInPicture} />}
                  onClick={() => runPiP(modalRef)}
                />
              </Tooltip>
              <Tooltip label={t('agent.downloadMedia')}>
                <IconButton
                  aria-label={t('agent.downloadMedia')}
                  colorScheme="whiteAlpha"
                  icon={<Icon as={IoDownloadOutline} />}
                  onClick={runDownload}
                />
              </Tooltip>
            </HStack>
          </ModalBody>
        </ModalContent>
      </Modal>
    </Box>
  )
}

// 消息气泡组件
const MessageBubble: React.FC<{ message: ChatMessage }> = ({ message }) => {
  const { t } = useTranslation()
  const isUser = message.role === 'user'

  return (
    <Flex justify={isUser ? 'flex-end' : 'flex-start'}>
      <Box
        maxW="80%"
        bg={isUser ? 'blue.500' : 'white'}
        color={isUser ? 'white' : 'gray.800'}
        p={4}
        borderRadius="lg"
        shadow="sm"
      >
        {/* 用户上传的图片显示 */}
        {message.images && message.images.length > 0 && (
          <Flex gap={2} mb={2} flexWrap="wrap">
            {message.images.map((image, idx) => (
              <ImageWithLightbox
                key={idx}
                src={`data:${image.mime_type};base64,${image.data}`}
                alt={`Uploaded image ${idx + 1}`}
                maxW="200px"
                maxH="200px"
                objectFit="cover"
              />
            ))}
          </Flex>
        )}

        {/* AI 生成的文件显示（图片、视频、图表） */}
        {message.generatedFiles && message.generatedFiles.length > 0 && (
          <Flex gap={2} mb={2} flexWrap="wrap" direction="column">
            {message.generatedFiles.map((file, idx) => (
              <Box key={idx} bg={isUser ? 'blue.400' : 'gray.50'} p={2} borderRadius="md">
                {file.type === 'image' ? (
                  <ImageWithLightbox
                    src={file.url}
                    alt={`Generated image ${idx + 1}`}
                    maxW="300px"
                    maxH="300px"
                    objectFit="contain"
                  />
                ) : file.type === 'video' ? (
                  <GeneratedVideoBlock file={file} isUser={isUser} />
                ) : file.type === 'chart' ? (
                  <Box>
                    <ImageWithLightbox
                      src={file.url}
                      alt={`Chart ${idx + 1}`}
                      maxW="300px"
                      maxH="300px"
                      objectFit="contain"
                    />
                    <Text fontSize="xs" mt={1} color={isUser ? 'blue.100' : 'gray.500'}>
                      {file.filename}
                    </Text>
                  </Box>
                ) : (
                  <Text fontSize="sm">
                    <a href={file.url} target="_blank" rel="noopener noreferrer" style={{ color: isUser ? 'white' : 'blue' }}>
                      {file.filename}
                    </a>
                  </Text>
                )}
              </Box>
            ))}
          </Flex>
        )}

        {/* 消息内容 */}
        {message.content && (
          <Text whiteSpace="pre-wrap">{message.content}</Text>
        )}

        {/* 工具调用 */}
        {message.toolCalls && message.toolCalls.length > 0 && (
          <VStack mt={3} spacing={2} align="stretch">
            {message.toolCalls.map((toolCall) => (
              <ToolCallCard key={toolCall.id} toolCall={toolCall} />
            ))}
          </VStack>
        )}

        {/* 风险提示 */}
        {message.metadata?.confirmations && (
          <Box
            mt={3}
            p={2}
            bg="yellow.100"
            borderRadius="md"
          >
            <HStack spacing={2}>
              <Icon as={IoWarning} color="yellow.600" />
              <Text fontSize="sm" color="yellow.800">
                {t('agent.riskWarning')}
              </Text>
            </HStack>
          </Box>
        )}

        {/* 时间戳 */}
        <Text
          fontSize="xs"
          color={isUser ? 'blue.100' : 'gray.500'}
          mt={2}
        >
          {formatTime(message.timestamp)}
        </Text>
      </Box>
    </Flex>
  )
}

// 工具调用卡片
const ToolCallCard: React.FC<{ toolCall: ToolCall }> = ({ toolCall }) => {
  const { t } = useTranslation()
  const statusColor = {
    pending: 'gray',
    executing: 'blue',
    completed: 'green',
    failed: 'red',
  }[toolCall.status]

  return (
    <Box
      p={3}
      bg="gray.100"
      borderRadius="md"
      borderLeft={`4px solid ${statusColor}`}
    >
      <HStack justify="space-between" mb={1}>
        <Text fontWeight="bold" fontSize="sm">
          {toolCall.name}
        </Text>
        <Text fontSize="xs" color={`${statusColor}.600`}>
          {toolCall.status}
        </Text>
      </HStack>

      {/* 参数 */}
      <Text fontSize="xs" color="gray.600" mb={2}>
        {JSON.stringify(toolCall.arguments, null, 2)}
      </Text>

      {/* 结果 */}
      {toolCall.result && (
        <Text fontSize="xs" color="green.700">
          ✓ {t('agent.toolCompleted')}
        </Text>
      )}

      {/* 错误 */}
      {toolCall.error && (
        <Text fontSize="xs" color="red.600">
          ✗ {toolCall.error}
        </Text>
      )}
    </Box>
  )
}

// 配置预览卡片
const ConfigPreviewCard: React.FC<{ config: any }> = ({ config }) => {
  return (
    <Box
      p={3}
      bg="white"
      borderRadius="md"
      border="1px solid"
      borderColor="gray.200"
    >
      <VStack align="stretch" spacing={2}>
        <Text fontWeight="bold" fontSize="sm">
          策略配置
        </Text>

        {Object.entries(config).map(([key, value]) => (
          <HStack key={key} justify="space-between" fontSize="sm">
            <Text color="gray.600">{key}:</Text>
            <Text fontWeight="medium">
              {String(value)}
            </Text>
          </HStack>
        ))}
      </VStack>
    </Box>
  )
}

// 建议标签
const SuggestionChips: React.FC<{
  suggestions: string[]
  onSelect: (value: string) => void
}> = ({ suggestions, onSelect }) => {
  return (
    <HStack spacing={2} flexWrap="wrap">
      {suggestions.map((suggestion) => (
        <Button
          key={suggestion}
          size="sm"
          variant="outline"
          borderRadius="full"
          onClick={() => onSelect(suggestion)}
        >
          {suggestion}
        </Button>
      ))}
    </HStack>
  )
}

// 辅助函数
function generateId(): string {
  return `${Date.now()}-${Math.random().toString(36).substr(2, 9)}`
}

function formatTime(date: Date): string {
  return new Date(date).toLocaleTimeString()
}

export default AgentChat
