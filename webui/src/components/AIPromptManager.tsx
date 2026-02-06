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
import { useTranslation } from 'react-i18next'
import { getAIPrompts, updateAIPrompt, AIPromptTemplate } from '../services/api'

const AIPromptManager: React.FC = () => {
  const { t } = useTranslation()
  const [prompts, setPrompts] = useState<Record<string, AIPromptTemplate>>({})
  const [editing, setEditing] = useState<string | null>(null)
  const [editedPrompts, setEditedPrompts] = useState<Record<string, { template: string; systemPrompt: string }>>({})
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState<string | null>(null)
  const toast = useToast()

  const getModuleName = (module: string): string => {
    return t(`aiPromptManager.moduleNames.${module}`, { defaultValue: module })
  }

  useEffect(() => {
    fetchPrompts()
  }, [])

  const fetchPrompts = async () => {
    try {
      setLoading(true)
      const data = await getAIPrompts()
      setPrompts(data.prompts)
      // 初始化编辑状態
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
        title: t('aiPromptManager.fetchFailed'),
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
    // 恢複原始值
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
        title: t('aiPromptManager.saveSuccess'),
        description: t('aiPromptManager.promptUpdated', { module: getModuleName(module) }),
        status: 'success',
        duration: 3000,
        isClosable: true,
      })
      setEditing(null)
      await fetchPrompts()
    } catch (error) {
      toast({
        title: t('aiPromptManager.saveFailed'),
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
        {t('aiPromptManager.title')}
      </Heading>

      <Alert status="info" mb={6}>
        <AlertIcon />
        {t('aiPromptManager.templateHint')}
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
                      <Text fontWeight="bold">{t('aiPromptManager.noData')}</Text>
                      <Text fontSize="sm" mt={1}>
                        {t('aiPromptManager.autoLoadHint')}
                      </Text>
                    </Box>
                  </Alert>
                  <Button colorScheme="blue" onClick={fetchPrompts}>
                    {t('aiPromptManager.refreshData')}
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
                  <Heading size="md">{getModuleName(module)}</Heading>
                  {editing === module ? (
                    <HStack>
                      <Button
                        size="sm"
                        colorScheme="green"
                        onClick={() => handleSave(module)}
                        isLoading={saving === module}
                      >
                        {t('common.save')}
                      </Button>
                      <Button size="sm" onClick={handleCancel}>
                        {t('common.cancel')}
                      </Button>
                    </HStack>
                  ) : (
                    <Button size="sm" colorScheme="blue" onClick={() => handleEdit(module)}>
                      {t('aiPromptManager.edit')}
                    </Button>
                  )}
                </HStack>
              </CardHeader>
              <CardBody>
                <VStack align="stretch" spacing={4}>
                  <Box>
                    <Text fontWeight="bold" mb={2}>{t('aiPromptManager.systemPrompt')}</Text>
                    {editing === module ? (
                      <Textarea
                        value={editedPrompts[module]?.systemPrompt || ''}
                        onChange={(e) => handleSystemPromptChange(module, e.target.value)}
                        rows={2}
                        placeholder={t('aiPromptManager.systemPromptPlaceholder')}
                      />
                    ) : (
                      <Text p={2} bg="gray.50" borderRadius="md" minH="40px">
                        {prompt.system_prompt || t('aiPromptManager.notSet')}
                      </Text>
                    )}
                  </Box>
                  <Divider />
                  <Box>
                    <Text fontWeight="bold" mb={2}>{t('aiPromptManager.promptTemplate')}</Text>
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
