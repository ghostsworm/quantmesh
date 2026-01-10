import React, { useState } from 'react'
import {
  Button,
  Modal,
  ModalOverlay,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalFooter,
  ModalCloseButton,
  VStack,
  HStack,
  Text,
  Radio,
  RadioGroup,
  Box,
  Alert,
  AlertIcon,
  useDisclosure,
  useToast,
  Icon,
  Divider,
  Badge,
  useColorModeValue,
} from '@chakra-ui/react'
import { RepeatIcon, CheckCircleIcon, WarningIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import { rebalanceCapital } from '../../services/capital'
import type { RebalanceResult, RebalanceChange } from '../../types/capital'

interface RebalanceButtonProps {
  onRebalanceComplete?: () => void
  disabled?: boolean
}

const RebalanceButton: React.FC<RebalanceButtonProps> = ({
  onRebalanceComplete,
  disabled = false,
}) => {
  const { t } = useTranslation()
  const { isOpen, onOpen, onClose } = useDisclosure()
  const toast = useToast()
  const [mode, setMode] = useState<'equal' | 'weighted' | 'priority'>('equal')
  const [loading, setLoading] = useState(false)
  const [previewResult, setPreviewResult] = useState<RebalanceResult | null>(null)
  const [previewLoading, setPreviewLoading] = useState(false)

  const bgColor = useColorModeValue('white', 'gray.800')

  const handlePreview = async () => {
    setPreviewLoading(true)
    try {
      const result = await rebalanceCapital({ mode, dryRun: true })
      setPreviewResult(result)
    } catch (error) {
      toast({
        title: t('capitalManagement.previewError'),
        description: error instanceof Error ? error.message : 'Unknown error',
        status: 'error',
        duration: 3000,
      })
    } finally {
      setPreviewLoading(false)
    }
  }

  const handleRebalance = async () => {
    setLoading(true)
    try {
      const result = await rebalanceCapital({ mode, dryRun: false })
      if (result.success) {
        toast({
          title: t('capitalManagement.rebalanceSuccess'),
          status: 'success',
          duration: 3000,
        })
        onRebalanceComplete?.()
        onClose()
      } else {
        toast({
          title: t('capitalManagement.rebalanceError'),
          description: result.message,
          status: 'error',
          duration: 3000,
        })
      }
    } catch (error) {
      toast({
        title: t('capitalManagement.rebalanceError'),
        description: error instanceof Error ? error.message : 'Unknown error',
        status: 'error',
        duration: 3000,
      })
    } finally {
      setLoading(false)
    }
  }

  const renderChangePreview = (changes: RebalanceChange[]) => {
    return (
      <VStack align="stretch" spacing={2}>
        {changes.map((change) => (
          <HStack key={change.strategyId} justify="space-between" p={2} bg="gray.50" borderRadius="md">
            <Text fontSize="sm" fontWeight="medium">
              {change.strategyId}
            </Text>
            <HStack spacing={2}>
              <Text fontSize="sm" color="gray.500">
                {change.previousAllocation.toFixed(2)}
              </Text>
              <Text fontSize="sm">→</Text>
              <Text
                fontSize="sm"
                fontWeight="bold"
                color={change.difference > 0 ? 'green.500' : change.difference < 0 ? 'red.500' : 'gray.500'}
              >
                {change.newAllocation.toFixed(2)}
              </Text>
              <Badge
                colorScheme={change.difference > 0 ? 'green' : change.difference < 0 ? 'red' : 'gray'}
                fontSize="xs"
              >
                {change.difference > 0 ? '+' : ''}{change.difference.toFixed(2)}
              </Badge>
            </HStack>
          </HStack>
        ))}
      </VStack>
    )
  }

  return (
    <>
      <Button
        leftIcon={<RepeatIcon />}
        colorScheme="blue"
        variant="outline"
        onClick={onOpen}
        isDisabled={disabled}
      >
        {t('capitalManagement.rebalance')}
      </Button>

      <Modal isOpen={isOpen} onClose={onClose} size="lg">
        <ModalOverlay bg="blackAlpha.600" backdropFilter="blur(4px)" />
        <ModalContent bg={bgColor}>
          <ModalHeader>{t('capitalManagement.rebalanceTitle')}</ModalHeader>
          <ModalCloseButton />

          <ModalBody>
            <VStack align="stretch" spacing={4}>
              <Alert status="info" borderRadius="md">
                <AlertIcon />
                <Text fontSize="sm">{t('capitalManagement.rebalanceDescription')}</Text>
              </Alert>

              <Box>
                <Text fontWeight="bold" mb={3}>
                  {t('capitalManagement.rebalanceMode')}
                </Text>
                <RadioGroup value={mode} onChange={(v) => setMode(v as typeof mode)}>
                  <VStack align="stretch" spacing={2}>
                    <Radio value="equal">
                      <VStack align="start" spacing={0}>
                        <Text fontWeight="medium">{t('capitalManagement.modeEqual')}</Text>
                        <Text fontSize="xs" color="gray.500">
                          {t('capitalManagement.modeEqualDesc')}
                        </Text>
                      </VStack>
                    </Radio>
                    <Radio value="weighted">
                      <VStack align="start" spacing={0}>
                        <Text fontWeight="medium">{t('capitalManagement.modeWeighted')}</Text>
                        <Text fontSize="xs" color="gray.500">
                          {t('capitalManagement.modeWeightedDesc')}
                        </Text>
                      </VStack>
                    </Radio>
                    <Radio value="priority">
                      <VStack align="start" spacing={0}>
                        <Text fontWeight="medium">{t('capitalManagement.modePriority')}</Text>
                        <Text fontSize="xs" color="gray.500">
                          {t('capitalManagement.modePriorityDesc')}
                        </Text>
                      </VStack>
                    </Radio>
                  </VStack>
                </RadioGroup>
              </Box>

              {previewResult && (
                <>
                  <Divider />
                  <Box>
                    <HStack mb={3}>
                      <Icon
                        as={previewResult.success ? CheckCircleIcon : WarningIcon}
                        color={previewResult.success ? 'green.500' : 'orange.500'}
                      />
                      <Text fontWeight="bold">{t('capitalManagement.previewResult')}</Text>
                    </HStack>
                    {previewResult.changes.length > 0 ? (
                      renderChangePreview(previewResult.changes)
                    ) : (
                      <Text fontSize="sm" color="gray.500">
                        {t('capitalManagement.noChanges')}
                      </Text>
                    )}
                  </Box>
                </>
              )}
            </VStack>
          </ModalBody>

          <ModalFooter>
            <HStack spacing={3}>
              <Button variant="ghost" onClick={onClose}>
                {t('common.cancel')}
              </Button>
              <Button
                variant="outline"
                colorScheme="blue"
                onClick={handlePreview}
                isLoading={previewLoading}
              >
                {t('capitalManagement.preview')}
              </Button>
              <Button
                colorScheme="blue"
                onClick={handleRebalance}
                isLoading={loading}
                isDisabled={!previewResult || !previewResult.success}
              >
                {t('capitalManagement.confirmRebalance')}
              </Button>
            </HStack>
          </ModalFooter>
        </ModalContent>
      </Modal>
    </>
  )
}

export default RebalanceButton
