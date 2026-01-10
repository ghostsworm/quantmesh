import React from 'react'
import {
  Modal,
  ModalOverlay,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalFooter,
  ModalCloseButton,
  Box,
  Flex,
  HStack,
  VStack,
  Text,
  Badge,
  Button,
  Icon,
  Stat,
  StatLabel,
  StatNumber,
  StatHelpText,
  StatArrow,
  SimpleGrid,
  Divider,
  Tabs,
  TabList,
  TabPanels,
  Tab,
  TabPanel,
  Table,
  Thead,
  Tbody,
  Tr,
  Th,
  Td,
  useColorModeValue,
} from '@chakra-ui/react'
import {
  LockIcon,
  CheckCircleIcon,
  WarningIcon,
  InfoOutlineIcon,
  StarIcon,
} from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import type { StrategyDetailInfo, RiskLevel } from '../../types/strategy'

interface StrategyDetailModalProps {
  isOpen: boolean
  onClose: () => void
  strategy: StrategyDetailInfo | null
  onEnable: (strategyId: string) => void
  onDisable: (strategyId: string) => void
  onConfigure: (strategyId: string) => void
}

const getRiskLevelColor = (level: RiskLevel) => {
  switch (level) {
    case 'low':
      return 'green'
    case 'medium':
      return 'orange'
    case 'high':
      return 'red'
    default:
      return 'gray'
  }
}

const StrategyDetailModal: React.FC<StrategyDetailModalProps> = ({
  isOpen,
  onClose,
  strategy,
  onEnable,
  onDisable,
  onConfigure,
}) => {
  const { t } = useTranslation()
  const bgColor = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue('gray.200', 'gray.600')

  if (!strategy) return null

  return (
    <Modal isOpen={isOpen} onClose={onClose} size="4xl" scrollBehavior="inside">
      <ModalOverlay bg="blackAlpha.600" backdropFilter="blur(4px)" />
      <ModalContent bg={bgColor} borderRadius="xl">
        <ModalHeader pb={2}>
          <Flex justify="space-between" align="center">
            <VStack align="start" spacing={1}>
              <HStack>
                <Text fontSize="xl" fontWeight="bold">
                  {strategy.name}
                </Text>
                {strategy.isPremium && (
                  <Badge
                    colorScheme="purple"
                    variant="solid"
                    fontSize="xs"
                    borderRadius="full"
                    px={2}
                  >
                    <HStack spacing={1}>
                      <Icon as={StarIcon} boxSize={3} />
                      <Text>PRO</Text>
                    </HStack>
                  </Badge>
                )}
              </HStack>
              <HStack spacing={2}>
                <Badge colorScheme="blue" fontSize="xs" borderRadius="full" px={2}>
                  {t(`strategyMarket.types.${strategy.type}`, strategy.type)}
                </Badge>
                <Badge
                  colorScheme={getRiskLevelColor(strategy.riskLevel)}
                  fontSize="xs"
                  borderRadius="full"
                  px={2}
                >
                  {t(`strategyMarket.riskLevels.${strategy.riskLevel}`)}
                </Badge>
                {strategy.isEnabled && (
                  <Badge colorScheme="green" fontSize="xs" borderRadius="full" px={2}>
                    {t('strategyMarket.enabled')}
                  </Badge>
                )}
              </HStack>
            </VStack>
          </Flex>
        </ModalHeader>
        <ModalCloseButton />

        <ModalBody>
          <Tabs variant="enclosed" colorScheme="blue">
            <TabList>
              <Tab>{t('strategyMarket.detail.overview')}</Tab>
              <Tab>{t('strategyMarket.detail.parameters')}</Tab>
              <Tab>{t('strategyMarket.detail.performance')}</Tab>
              <Tab>{t('strategyMarket.detail.risk')}</Tab>
            </TabList>

            <TabPanels>
              {/* Overview Tab */}
              <TabPanel>
                <VStack align="stretch" spacing={4}>
                  <Box>
                    <Text fontWeight="bold" mb={2}>
                      {t('strategyMarket.detail.description')}
                    </Text>
                    <Text color="gray.600" whiteSpace="pre-wrap">
                      {strategy.longDescription || strategy.description}
                    </Text>
                  </Box>

                  <Divider />

                  <Box>
                    <Text fontWeight="bold" mb={2}>
                      {t('strategyMarket.detail.features')}
                    </Text>
                    <HStack spacing={2} flexWrap="wrap">
                      {strategy.features.map((feature, index) => (
                        <Badge
                          key={index}
                          colorScheme="blue"
                          variant="subtle"
                          fontSize="sm"
                          borderRadius="full"
                          px={3}
                          py={1}
                          mb={2}
                        >
                          <Icon as={CheckCircleIcon} mr={1} />
                          {feature}
                        </Badge>
                      ))}
                    </HStack>
                  </Box>

                  <Divider />

                  <SimpleGrid columns={{ base: 2, md: 4 }} spacing={4}>
                    <Stat>
                      <StatLabel>{t('strategyMarket.minCapital')}</StatLabel>
                      <StatNumber fontSize="lg">{strategy.minCapital} USDT</StatNumber>
                    </Stat>
                    <Stat>
                      <StatLabel>{t('strategyMarket.recommended')}</StatLabel>
                      <StatNumber fontSize="lg">{strategy.recommendedCapital} USDT</StatNumber>
                    </Stat>
                    <Stat>
                      <StatLabel>{t('strategyMarket.detail.version')}</StatLabel>
                      <StatNumber fontSize="lg">{strategy.version || '1.0.0'}</StatNumber>
                    </Stat>
                    <Stat>
                      <StatLabel>{t('strategyMarket.detail.author')}</StatLabel>
                      <StatNumber fontSize="lg">{strategy.author || 'QuantMesh'}</StatNumber>
                    </Stat>
                  </SimpleGrid>
                </VStack>
              </TabPanel>

              {/* Parameters Tab */}
              <TabPanel>
                <Table variant="simple" size="sm">
                  <Thead>
                    <Tr>
                      <Th>{t('strategyMarket.detail.paramName')}</Th>
                      <Th>{t('strategyMarket.detail.paramType')}</Th>
                      <Th>{t('strategyMarket.detail.paramDefault')}</Th>
                      <Th>{t('strategyMarket.detail.paramDescription')}</Th>
                    </Tr>
                  </Thead>
                  <Tbody>
                    {strategy.parameters?.map((param) => (
                      <Tr key={param.key}>
                        <Td fontWeight="medium">{param.name}</Td>
                        <Td>
                          <Badge colorScheme="gray" fontSize="xs">
                            {param.type}
                          </Badge>
                        </Td>
                        <Td>{String(param.defaultValue)}</Td>
                        <Td fontSize="sm" color="gray.600">
                          {param.description}
                        </Td>
                      </Tr>
                    ))}
                    {(!strategy.parameters || strategy.parameters.length === 0) && (
                      <Tr>
                        <Td colSpan={4} textAlign="center" color="gray.500">
                          {t('strategyMarket.detail.noParameters')}
                        </Td>
                      </Tr>
                    )}
                  </Tbody>
                </Table>
              </TabPanel>

              {/* Performance Tab */}
              <TabPanel>
                {strategy.historicalPerformance ? (
                  <VStack align="stretch" spacing={4}>
                    <SimpleGrid columns={{ base: 2, md: 3 }} spacing={4}>
                      <Stat>
                        <StatLabel>{t('strategyMarket.detail.totalPnL')}</StatLabel>
                        <StatNumber
                          fontSize="lg"
                          color={strategy.historicalPerformance.totalPnL >= 0 ? 'green.500' : 'red.500'}
                        >
                          {strategy.historicalPerformance.totalPnL >= 0 ? '+' : ''}
                          {strategy.historicalPerformance.totalPnL.toFixed(2)} USDT
                        </StatNumber>
                        <StatHelpText>
                          <StatArrow
                            type={strategy.historicalPerformance.totalPnL >= 0 ? 'increase' : 'decrease'}
                          />
                          {strategy.historicalPerformance.period}
                        </StatHelpText>
                      </Stat>
                      <Stat>
                        <StatLabel>{t('strategyMarket.detail.winRate')}</StatLabel>
                        <StatNumber fontSize="lg">
                          {(strategy.historicalPerformance.winRate * 100).toFixed(1)}%
                        </StatNumber>
                      </Stat>
                      <Stat>
                        <StatLabel>{t('strategyMarket.detail.sharpeRatio')}</StatLabel>
                        <StatNumber fontSize="lg">
                          {strategy.historicalPerformance.sharpeRatio.toFixed(2)}
                        </StatNumber>
                      </Stat>
                      <Stat>
                        <StatLabel>{t('strategyMarket.detail.maxDrawdown')}</StatLabel>
                        <StatNumber fontSize="lg" color="red.500">
                          {(strategy.historicalPerformance.maxDrawdown * 100).toFixed(1)}%
                        </StatNumber>
                      </Stat>
                      <Stat>
                        <StatLabel>{t('strategyMarket.detail.tradeCount')}</StatLabel>
                        <StatNumber fontSize="lg">
                          {strategy.historicalPerformance.tradeCount}
                        </StatNumber>
                      </Stat>
                    </SimpleGrid>
                  </VStack>
                ) : (
                  <Box textAlign="center" py={8} color="gray.500">
                    <Icon as={InfoOutlineIcon} boxSize={8} mb={2} />
                    <Text>{t('strategyMarket.detail.noPerformanceData')}</Text>
                  </Box>
                )}
              </TabPanel>

              {/* Risk Tab */}
              <TabPanel>
                {strategy.riskMetrics ? (
                  <SimpleGrid columns={{ base: 2, md: 4 }} spacing={4}>
                    <Stat>
                      <StatLabel>{t('strategyMarket.detail.volatility')}</StatLabel>
                      <StatNumber fontSize="lg">
                        {(strategy.riskMetrics.volatility * 100).toFixed(2)}%
                      </StatNumber>
                    </Stat>
                    <Stat>
                      <StatLabel>VaR (95%)</StatLabel>
                      <StatNumber fontSize="lg" color="red.500">
                        {(strategy.riskMetrics.var95 * 100).toFixed(2)}%
                      </StatNumber>
                    </Stat>
                    <Stat>
                      <StatLabel>CVaR (95%)</StatLabel>
                      <StatNumber fontSize="lg" color="red.500">
                        {(strategy.riskMetrics.cvar95 * 100).toFixed(2)}%
                      </StatNumber>
                    </Stat>
                    <Stat>
                      <StatLabel>Beta</StatLabel>
                      <StatNumber fontSize="lg">{strategy.riskMetrics.beta.toFixed(2)}</StatNumber>
                    </Stat>
                  </SimpleGrid>
                ) : (
                  <Box textAlign="center" py={8} color="gray.500">
                    <Icon as={WarningIcon} boxSize={8} mb={2} />
                    <Text>{t('strategyMarket.detail.noRiskData')}</Text>
                  </Box>
                )}
              </TabPanel>
            </TabPanels>
          </Tabs>
        </ModalBody>

        <ModalFooter borderTop="1px" borderColor={borderColor}>
          <HStack spacing={3}>
            <Button variant="ghost" onClick={onClose}>
              {t('common.close')}
            </Button>
            {strategy.isEnabled ? (
              <>
                <Button
                  colorScheme="blue"
                  onClick={() => {
                    onConfigure(strategy.id)
                    onClose()
                  }}
                >
                  {t('strategyMarket.configure')}
                </Button>
                <Button
                  variant="outline"
                  colorScheme="red"
                  onClick={() => {
                    onDisable(strategy.id)
                    onClose()
                  }}
                >
                  {t('strategyMarket.disable')}
                </Button>
              </>
            ) : (
              <Button
                colorScheme="blue"
                leftIcon={strategy.isPremium ? <LockIcon /> : undefined}
                onClick={() => {
                  onEnable(strategy.id)
                  onClose()
                }}
              >
                {strategy.isPremium ? t('strategyMarket.unlock') : t('strategyMarket.enable')}
              </Button>
            )}
          </HStack>
        </ModalFooter>
      </ModalContent>
    </Modal>
  )
}

export default StrategyDetailModal
