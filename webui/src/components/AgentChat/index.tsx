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
} from '@chakra-ui/react'
import { IoSend, IoRefresh, IoStopCircle, IoCheckmarkCircle, IoWarning } from 'react-icons/io5'
import { useTranslation } from 'react-i18next'
import { api } from '../services/api'

interface ChatMessage {
  id: string
  role: 'user' | 'assistant' | 'system'
  content: string
  timestamp: Date
  toolCalls?: ToolCall[]
  metadata?: {
    risk?: string
    confirmations?: Confirmation[]
  }
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

  const messagesEndRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  // 自动滚动到底部
  const scrollToBottom = () => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }

  useEffect(() => {
    scrollToBottom()
  }, [messages])

  // 创建会话
  useEffect(() => {
    createSession()
  }, [botId])

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
    } catch (error) {
      console.error('Failed to create session:', error)
      toast({
        title: t('agent.sessionCreateFailed'),
        status: 'error',
        duration: 3000,
      })
    }
  }

  const sendMessage = async () => {
    const content = input.trim()
    if (!content || !sessionId || isLoading) return

    // 添加用户消息
    const userMessage: ChatMessage = {
      id: generateId(),
      role: 'user',
      content,
      timestamp: new Date(),
    }

    setMessages((prev) => [...prev, userMessage])
    setInput('')
    setIsLoading(true)

    try {
      const response = await fetch(`/api/agent/sessions/${sessionId}/messages`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          content,
          stream: false,
        }),
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
      }

      setMessages((prev) => [...prev, assistantMessage])

      // 更新配置预览
      if (data.config_preview) {
        setConfigPreview(data.config_preview)
      }

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
    <Box height="100%" display="flex" flexDirection="column">
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
        <HStack spacing={2}>
          <Input
            ref={inputRef}
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyPress={handleKeyPress}
            placeholder={t('agent.inputPlaceholder')}
            disabled={isLoading}
            bg="white"
          />

          <Tooltip label={t('agent.sendMessage')}>
            <IconButton
              colorScheme="blue"
              aria-label="Send message"
              icon={<Icon as={IoSend} />}
              onClick={sendMessage}
              isDisabled={isLoading || !input.trim()}
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
  )
}

// 消息气泡组件
const MessageBubble: React.FC<{ message: ChatMessage }> = ({ message }) => {
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
        {/* 消息内容 */}
        <Text whiteSpace="pre-wrap">{message.content}</Text>

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
          {t('agent.strategyConfig')}
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
