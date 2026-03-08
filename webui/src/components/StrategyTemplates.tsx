import React, { useState, useEffect } from 'react'
import {
  Box,
  VStack,
  HStack,
  Text,
  Button,
  SimpleGrid,
  Card,
  CardBody,
  Badge,
  Heading,
  Input,
  Select,
  HStack,
  Flex,
  Spacer,
  Tooltip,
  useColorModeValue,
  Spinner,
  IconButton,
  Modal,
  ModalOverlay,
  ModalContent,
  ModalHeader,
  ModalFooter,
  ModalBody,
  ModalCloseButton,
  Tabs,
  TabList,
  Tab,
  TabPanels,
  TabPanel,
  Alert,
  AlertIcon,
  AlertTitle,
  AlertDescription,
  Link,
  Divider,
} from '@chakra-ui/react'
import { InfoIcon, CheckIcon, StarIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import { api } from '../services/api'
import { getStrategyTemplatesFull, type StrategyTemplateFull, type TemplateParam } from '../services/strategy'

interface StrategyTemplatesProps {
  onSelectTemplate: (templateId: string) => void
  selectedSymbol?: string
  selectedExchange?: string
}

const StrategyTemplates: React.FC<StrategyTemplatesProps> = ({
  onSelectTemplate,
  selectedSymbol,
  selectedExchange,
}) => {
  const { t } = useTranslation()
  const [templates, setTemplates] = useState<StrategyTemplateFull[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  // 筛选条件
  const [categoryFilter, setCategoryFilter] = useState<string>('all')
  const [difficultyFilter, setDifficultyFilter] = useState<string>('all')
  const [riskFilter, setRiskFilter] = useState<string>('all')
  const [symbolFilter, setSymbolFilter] = useState<string>('all')
  const [searchQuery, setSearchQuery] = useState('')

  // 模态框状态
  const [selectedTemplate, setSelectedTemplate] = useState<StrategyTemplateFull | null>(null)
  const [isModalOpen, setIsModalOpen] = useState(false)

  useEffect(() => {
    loadTemplates()
  }, [])

  const loadTemplates = async () => {
    try {
      setLoading(true)
      const data = await getStrategyTemplatesFull()
      if (data.templates) {
        setTemplates(data.templates)
      }
    } catch (err) {
      setError(t('template.loadFailed'))
      console.error('Failed to load templates:', err)
    } finally {
      setLoading(false)
    }
  }

  // 筛选模板
  const filteredTemplates = templates.filter(template => {
    // 分类筛选
    if (categoryFilter !== 'all' && template.category !== categoryFilter) return false

    // 难度筛选
    if (difficultyFilter !== 'all' && template.difficulty !== difficultyFilter) return false

    // 风险等级筛选
    if (riskFilter !== 'all' && template.risk_level !== riskFilter) return false

    // 币种筛选
    if (symbolFilter !== 'all' && template.symbols && template.symbols.length > 0) {
      if (!template.symbols.includes(symbolFilter)) return false
    }

    // 搜索筛选
    if (searchQuery) {
      const query = searchQuery.toLowerCase()
      if (!template.name.toLowerCase().includes(query) &&
          !template.description.toLowerCase().includes(query)) {
        return false
      }
    }

    return true
  })

  // 分组模板
  const groupedTemplates = filteredTemplates.reduce((acc, template) => {
    const category = template.category || 'other'
    if (!acc[category]) {
      acc[category] = []
    }
    acc[category].push(template)
    return acc
  }, {} as Record<string, StrategyTemplateFull[]>)

  const handleSelectTemplate = (template: StrategyTemplateFull) => {
    setSelectedTemplate(template)
    setIsModalOpen(true)
  }

  const handleConfirmTemplate = () => {
    if (selectedTemplate) {
      onSelectTemplate(selectedTemplate.id)
      setIsModalOpen(false)
    }
  }

  const getDifficultyColor = (difficulty?: string) => {
    switch (difficulty) {
      case 'beginner': return 'green'
      case 'intermediate': return 'yellow'
      case 'advanced': return 'red'
      default: return 'gray'
    }
  }

  const getDifficultyLabel = (difficulty?: string) => {
    switch (difficulty) {
      case 'beginner': return t('template.beginner')
      case 'intermediate': return t('template.intermediate')
      case 'advanced': return t('template.advanced')
      default: return t('template.all')
    }
  }

  const getRiskColor = (risk?: string) => {
    switch (risk) {
      case 'low': return 'green'
      case 'medium': return 'yellow'
      case 'high': return 'red'
      default: return 'gray'
    }
  }

  const getRiskLabel = (risk?: string) => {
    switch (risk) {
      case 'low': return t('template.riskLow')
      case 'medium': return t('template.riskMedium')
      case 'high': return t('template.riskHigh')
      default: return t('template.all')
    }
  }

  const getCategoryLabel = (category: string) => {
    switch (category) {
      case 'grid': return t('template.gridStrategy')
      case 'dca': return t('template.dcaStrategy')
      case 'combo': return t('template.comboStrategy')
      default: return category
    }
  }

  const bgColor = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue('gray.200', 'gray.600')

  if (loading) {
    return (
      <Flex justify="center" align="center" minH="400px">
        <VStack spacing={4}>
          <Spinner size="xl" color="blue.500" />
          <Text color="gray.500">{t('template.loading')}</Text>
        </VStack>
      </Flex>
    )
  }

  if (error) {
    return (
      <Alert status="error">
        <AlertIcon />
        <AlertTitle>{t('template.loadFailed')}</AlertTitle>
        <AlertDescription>{error}</AlertDescription>
      </Alert>
    )
  }

  return (
    <Box>
      <VStack align="stretch" spacing={6}>
        {/* 标题和搜索 */}
        <Flex justify="space-between" align="center" wrap="wrap" gap={4}>
          <Heading size="lg">{t('template.selectTemplate')}</Heading>
          <Input
            placeholder={t('template.searchPlaceholder')}
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            maxW="300px"
          />
        </Flex>

        {/* 筛选器 */}
        <HStack spacing={4} wrap="wrap">
          <Select
            placeholder={t('template.category')}
            value={categoryFilter}
            onChange={(e) => setCategoryFilter(e.target.value)}
            w="180px"
          >
            <option value="all">{t('template.all')}</option>
            <option value="grid">{t('template.gridStrategy')}</option>
            <option value="dca">{t('template.dcaStrategy')}</option>
            <option value="combo">{t('template.comboStrategy')}</option>
          </Select>

          <Select
            placeholder={t('template.difficulty')}
            value={difficultyFilter}
            onChange={(e) => setDifficultyFilter(e.target.value)}
            w="180px"
          >
            <option value="all">{t('template.all')}</option>
            <option value="beginner">{t('template.beginner')}</option>
            <option value="intermediate">{t('template.intermediate')}</option>
            <option value="advanced">{t('template.advanced')}</option>
          </Select>

          <Select
            placeholder={t('template.riskLevel')}
            value={riskFilter}
            onChange={(e) => setRiskFilter(e.target.value)}
            w="180px"
          >
            <option value="all">{t('template.all')}</option>
            <option value="low">{t('template.riskLow')}</option>
            <option value="medium">{t('template.riskMedium')}</option>
            <option value="high">{t('template.riskHigh')}</option>
          </Select>

          {selectedSymbol && (
            <Select
              placeholder={t('template.symbol')}
              value={symbolFilter}
              onChange={(e) => setSymbolFilter(e.target.value)}
              w="180px"
            >
              <option value="all">{t('template.all')}</option>
              <option value={selectedSymbol}>{selectedSymbol}</option>
            </Select>
          )}
        </HStack>

        {/* 模板列表 */}
        <Tabs variant="enclosed">
          <TabList>
            <Tab>{t('template.all')} ({filteredTemplates.length})</Tab>
            {Object.entries(groupedTemplates).map(([category, items]) => (
              <Tab key={category}>{getCategoryLabel(category)} ({items.length})</Tab>
            ))}
          </TabList>

          <TabPanels>
            {/* 全部 */}
            <TabPanel>
              <SimpleGrid columns={{ base: 1, md: 2, lg: 3 }} spacing={4}>
                {filteredTemplates.map((template) => (
                  <TemplateCard
                    key={template.id}
                    template={template}
                    onSelect={() => handleSelectTemplate(template)}
                    getDifficultyColor={getDifficultyColor}
                    getDifficultyLabel={getDifficultyLabel}
                    getRiskColor={getRiskColor}
                    getRiskLabel={getRiskLabel}
                  />
                ))}
              </SimpleGrid>
            </TabPanel>

            {/* 分类 */}
            {Object.entries(groupedTemplates).map(([category, items]) => (
              <TabPanel key={category}>
                <SimpleGrid columns={{ base: 1, md: 2, lg: 3 }} spacing={4}>
                  {items.map((template) => (
                    <TemplateCard
                      key={template.id}
                      template={template}
                      onSelect={() => handleSelectTemplate(template)}
                      getDifficultyColor={getDifficultyColor}
                      getDifficultyLabel={getDifficultyLabel}
                      getRiskColor={getRiskColor}
                      getRiskLabel={getRiskLabel}
                    />
                  ))}
                </SimpleGrid>
              </TabPanel>
            ))}
          </TabPanels>
        </Tabs>
      </VStack>

      {/* 确认模态框 */}
      <Modal isOpen={isModalOpen} onClose={() => setIsModalOpen(false)} size="lg">
        <ModalOverlay />
        <ModalContent>
          <ModalHeader>
            {selectedTemplate?.name}
          </ModalHeader>
          <ModalCloseButton />
          <ModalBody pb={6}>
            {selectedTemplate && (
              <VStack align="stretch" spacing={4}>
                {/* 描述 */}
                <Text>{selectedTemplate.description}</Text>

                <Divider />

                {/* 属性 */}
                <SimpleGrid columns={2} spacing={4}>
                  <Box>
                    <Text fontSize="sm" color="gray.500">{t('template.category')}</Text>
                    <Text fontWeight="medium">{getCategoryLabel(selectedTemplate.category)}</Text>
                  </Box>
                  <Box>
                    <Text fontSize="sm" color="gray.500">{t('template.difficulty')}</Text>
                    <Badge colorScheme={getDifficultyColor(selectedTemplate.difficulty)}>
                      {getDifficultyLabel(selectedTemplate.difficulty)}
                    </Badge>
                  </Box>
                  <Box>
                    <Text fontSize="sm" color="gray.500">{t('template.riskLevel')}</Text>
                    <Badge colorScheme={getRiskColor(selectedTemplate.risk_level)}>
                      {getRiskLabel(selectedTemplate.risk_level)}
                    </Badge>
                  </Box>
                  {selectedTemplate.min_capital && (
                    <Box>
                      <Text fontSize="sm" color="gray.500">{t('template.minCapital')}</Text>
                      <Text fontWeight="medium">${selectedTemplate.min_capital} USDT</Text>
                    </Box>
                  )}
                </SimpleGrid>

                {/* 标签 */}
                {selectedTemplate.tags && selectedTemplate.tags.length > 0 && (
                  <Box>
                    <Text fontSize="sm" color="gray.500" mb={2}>{t('template.tags')}</Text>
                    <HStack spacing={2} flexWrap="wrap">
                      {selectedTemplate.tags.map((tag) => (
                        <Badge key={tag} colorScheme="blue" variant="subtle">
                          {tag}
                        </Badge>
                      ))}
                    </HStack>
                  </Box>
                )}

                {/* 推荐币种 */}
                {selectedTemplate.symbols && selectedTemplate.symbols.length > 0 && (
                  <Box>
                    <Text fontSize="sm" color="gray.500" mb={2}>{t('template.recommendedSymbols')}</Text>
                    <HStack spacing={2} flexWrap="wrap">
                      {selectedTemplate.symbols.map((symbol) => (
                        <Badge key={symbol} colorScheme="green" variant="subtle">
                          {symbol}
                        </Badge>
                      ))}
                    </HStack>
                  </Box>
                )}

                {/* 参数列表 */}
                <Box>
                  <Text fontSize="sm" color="gray.500" mb={2}>{t('template.parameters')}</Text>
                  <VStack align="stretch" spacing={2} bg="gray.50" _dark={{ bg: 'gray.700' }} p={4} borderRadius="md">
                    {Object.entries(selectedTemplate.params).map(([key, param]) => (
                      <Box key={key}>
                        <HStack justify="space-between">
                          <Text fontWeight="medium">{param.name}</Text>
                          {param.required && (
                            <Badge size="sm" colorScheme="red">{t('template.required')}</Badge>
                          )}
                        </HStack>
                        <Text fontSize="sm" color="gray.600" _dark={{ color: 'gray.400' }} mt={1}>
                          {param.description}
                        </Text>
                        <HStack mt={2} spacing={4}>
                          <Text fontSize="xs" color="gray.500">
                            {t('template.default')}: {param.default}
                          </Text>
                          {param.min !== undefined && (
                            <Text fontSize="xs" color="gray.500">
                              {t('template.min')}: {param.min}
                            </Text>
                          )}
                          {param.max !== undefined && (
                            <Text fontSize="xs" color="gray.500">
                              {t('template.max')}: {param.max}
                            </Text>
                          )}
                        </HStack>
                      </Box>
                    ))}
                  </VStack>
                </Box>
              </VStack>
            )}
          </ModalBody>
          <ModalFooter>
            <Button variant="ghost" mr={3} onClick={() => setIsModalOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button colorScheme="blue" onClick={handleConfirmTemplate}>
              {t('template.useTemplate')}
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>
    </Box>
  )
}

interface TemplateCardProps {
  template: StrategyTemplateFull
  onSelect: () => void
  getDifficultyColor: (difficulty?: string) => string
  getDifficultyLabel: (difficulty?: string) => string
  getRiskColor: (risk?: string) => string
  getRiskLabel: (risk?: string) => string
}

const TemplateCard: React.FC<TemplateCardProps> = ({
  template,
  onSelect,
  getDifficultyColor,
  getDifficultyLabel,
  getRiskColor,
  getRiskLabel,
}) => {
  const bgColor = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue('gray.200', 'gray.600')

  return (
    <Card
      bg={bgColor}
      borderWidth="1px"
      borderColor={borderColor}
      cursor="pointer"
      onClick={onSelect}
      transition="all 0.2s"
      _hover={{ shadow: 'md', transform: 'translateY(-2px)' }}
    >
      <CardBody>
        <VStack align="stretch" spacing={3}>
          {/* 标题和难度 */}
          <Flex justify="space-between" align="start">
            <Heading size="sm" noOfLines={2}>
              {template.name}
            </Heading>
            {template.difficulty && (
              <Badge size="sm" colorScheme={getDifficultyColor(template.difficulty)}>
                {getDifficultyLabel(template.difficulty)}
              </Badge>
            )}
          </Flex>

          {/* 描述 */}
          <Text fontSize="sm" color="gray.600" _dark={{ color: 'gray.400' }} noOfLines={3}>
            {template.description}
          </Text>

          {/* 标签 */}
          {template.tags && template.tags.length > 0 && (
            <HStack spacing={2} flexWrap="wrap">
              {template.tags.slice(0, 3).map((tag) => (
                <Badge key={tag} size="sm" colorScheme="blue" variant="subtle">
                  {tag}
                </Badge>
              ))}
            </HStack>
          )}

          {/* 底部信息 */}
          <HStack justify="space-between" align="center">
            <HStack spacing={2}>
              {template.risk_level && (
                <Badge size="sm" colorScheme={getRiskColor(template.risk_level)}>
                  {getRiskLabel(template.risk_level)}
                </Badge>
              )}
              {template.min_capital && (
                <Text fontSize="xs" color="gray.500">
                  ${template.min_capital}+
                </Text>
              )}
            </HStack>
            <Button size="sm" colorScheme="blue" variant="outline">
              {t('template.select')}
            </Button>
          </HStack>
        </VStack>
      </CardBody>
    </Card>
  )
}

export default StrategyTemplates
