import React, { useEffect, useState } from 'react'
import {
  Box,
  Heading,
  Card,
  CardHeader,
  CardBody,
  Button,
  Textarea,
  VStack,
  HStack,
  Text,
  useToast,
  Spinner,
  Center,
  Divider,
  Badge,
  Alert,
  AlertIcon,
} from '@chakra-ui/react'
import { getAIPrompts, updateAIPrompt, AIPromptTemplate } from '../services/api'

const AIPromptManager: React.FC = () => {
  const [prompts, setPrompts] = useState<Record<string, AIPromptTemplate>>({})
  const [editing, setEditing] = useState<string | null>(null)
  const [editedPrompts, setEditedPrompts] = useState<Record<string, { template: string; systemPrompt: string }>>({})
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState<string | null>(null)
  const toast = useToast()

  const moduleNames: Record<string, string> = {
    market_analysis: '市场分析',
    parameter_optimization: '参数优化',
    risk_analysis: '风险分析',
    sentiment_analysis: '情绪分析',
  }

  useEffect(() => {
    fetchPrompts()
  }, [])

  const fetchPrompts = async () => {
    try {
      setLoading(true)
      const data = await getAIPrompts()
      setPrompts(data.prompts)
      // 初始化编辑状态
      const edited: Record<string, { template: string; systemPrompt: string }> = {}
      Object.entries(data.prompts).forEach(([module, prompt]) => {
        edited[module] = {
          template: prompt.template,
          systemPrompt: prompt.system_prompt || '',
        }
      })
      setEditedPrompts(edited)
    } catch (error) {
      toast({
        title: '获取提示词失败',
        description: error instanceof Error ? error.message : 'Unknown error',
        status: 'error',
        duration: 5000,
        isClosable: true,
      })
    } finally {
      setLoading(false)
    }
  }

  const handleEdit = (module: string) => {
    setEditing(module)
  }

  const handleCancel = () => {
    setEditing(null)
    // 恢复原始值
    fetchPrompts()
  }

  const handleSave = async (module: string) => {
    try {
      setSaving(module)
      const edited = editedPrompts[module]
      if (!edited) {
        return
      }
      await updateAIPrompt(module, edited.template, edited.systemPrompt)
      toast({
        title: '保存成功',
        description: `${moduleNames[module] || module} 提示词已更新`,
        status: 'success',
        duration: 3000,
        isClosable: true,
      })
      setEditing(null)
      await fetchPrompts()
    } catch (error) {
      toast({
        title: '保存失败',
        description: error instanceof Error ? error.message : 'Unknown error',
        status: 'error',
        duration: 5000,
        isClosable: true,
      })
    } finally {
      setSaving(null)
    }
  }

  const handleTemplateChange = (module: string, value: string) => {
    setEditedPrompts({
      ...editedPrompts,
      [module]: {
        ...editedPrompts[module],
        template: value,
      },
    })
  }

  const handleSystemPromptChange = (module: string, value: string) => {
    setEditedPrompts({
      ...editedPrompts,
      [module]: {
        ...editedPrompts[module],
        systemPrompt: value,
      },
    })
  }

  if (loading) {
    return (
      <Center h="400px">
        <Spinner size="xl" />
      </Center>
    )
  }

  return (
    <Box p={6}>
      <Heading size="lg" mb={6}>
        AI提示词管理
      </Heading>

      <Alert status="info" mb={6}>
        <AlertIcon />
        提示词模板支持Go的fmt.Sprintf占位符格式，例如: %s, %.2f, %d等
      </Alert>

      <VStack align="stretch" spacing={6}>
        {Object.keys(prompts).length === 0 ? (
          <Card>
            <CardBody>
              <Center py={8}>
                <VStack spacing={4}>
                  <Alert status="info" maxW="md">
                    <AlertIcon />
                    <Box>
                      <Text fontWeight="bold">暂无提示词数据</Text>
                      <Text fontSize="sm" mt={1}>
                        系统将自动加载默认提示词，请稍候或刷新页面
                      </Text>
                    </Box>
                  </Alert>
                  <Button colorScheme="blue" onClick={fetchPrompts}>
                    刷新数据
                  </Button>
                </VStack>
              </Center>
            </CardBody>
          </Card>
        ) : (
          Object.entries(prompts).map(([module, prompt]) => (
            <Card key={module}>
              <CardHeader>
                <HStack justify="space-between">
                  <Heading size="md">{moduleNames[module] || module}</Heading>
                  {editing === module ? (
                    <HStack>
                      <Button
                        size="sm"
                        colorScheme="green"
                        onClick={() => handleSave(module)}
                        isLoading={saving === module}
                      >
                        保存
                      </Button>
                      <Button size="sm" onClick={handleCancel}>
                        取消
                      </Button>
                    </HStack>
                  ) : (
                    <Button size="sm" colorScheme="blue" onClick={() => handleEdit(module)}>
                      编辑
                    </Button>
                  )}
                </HStack>
              </CardHeader>
              <CardBody>
                <VStack align="stretch" spacing={4}>
                  <Box>
                    <Text fontWeight="bold" mb={2}>系统提示词:</Text>
                    {editing === module ? (
                      <Textarea
                        value={editedPrompts[module]?.systemPrompt || ''}
                        onChange={(e) => handleSystemPromptChange(module, e.target.value)}
                        rows={2}
                        placeholder="系统提示词（可选）"
                      />
                    ) : (
                      <Text p={2} bg="gray.50" borderRadius="md" minH="40px">
                        {prompt.system_prompt || '(未设置)'}
                      </Text>
                    )}
                  </Box>
                  <Divider />
                  <Box>
                    <Text fontWeight="bold" mb={2}>提示词模板:</Text>
                    {editing === module ? (
                      <Textarea
                        value={editedPrompts[module]?.template || ''}
                        onChange={(e) => handleTemplateChange(module, e.target.value)}
                        rows={10}
                        fontFamily="mono"
                        fontSize="sm"
                      />
                    ) : (
                      <Text
                        p={2}
                        bg="gray.50"
                        borderRadius="md"
                        fontFamily="mono"
                        fontSize="sm"
                        whiteSpace="pre-wrap"
                        minH="200px"
                      >
                        {prompt.template}
                      </Text>
                    )}
                  </Box>
                </VStack>
              </CardBody>
            </Card>
          ))
        )}
      </VStack>
    </Box>
  )
}

export default AIPromptManager

