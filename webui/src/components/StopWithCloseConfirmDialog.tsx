import React, { useState } from 'react'
import {
  Modal,
  ModalOverlay,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalFooter,
  Button,
  Text,
  VStack,
  HStack,
  Icon,
  Radio,
  RadioGroup,
  Stack,
  FormControl,
  FormLabel,
  Select,
  NumberInput,
  NumberInputField,
  NumberInputStepper,
  NumberIncrementStepper,
  NumberDecrementStepper,
  Switch,
  useColorModeValue,
} from '@chakra-ui/react'
import { WarningIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import type { ClosePositionsV2Request } from '../services/api'

export type StopAction = 'stop_only' | 'stop_and_close'

export interface StopWithCloseConfirmDialogProps {
  isOpen: boolean
  onClose: () => void
  /** 仅停止（不平仓） */
  onStopOnly: () => Promise<void>
  /** 停止并平仓 */
  onStopAndClose: (req: ClosePositionsV2Request) => Promise<void>
  botId: string
  botName?: string
}

const StopWithCloseConfirmDialog: React.FC<StopWithCloseConfirmDialogProps> = ({
  isOpen,
  onClose,
  onStopOnly,
  onStopAndClose,
  botId,
  botName,
}) => {
  const { t } = useTranslation()
  const bgColor = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue('gray.200', 'gray.700')

  const [action, setAction] = useState<StopAction>('stop_only')
  const [method, setMethod] = useState<'market' | 'limit'>('market')
  const [priceOffset, setPriceOffset] = useState<number>(-0.1)
  const [timeoutSec, setTimeoutSec] = useState<number>(3600)
  const [autoRetry, setAutoRetry] = useState<boolean>(true)
  const [isLoading, setIsLoading] = useState(false)

  const handleConfirm = async () => {
    setIsLoading(true)
    try {
      if (action === 'stop_only') {
        await onStopOnly()
      } else {
        const req: ClosePositionsV2Request = {
          method,
          price_offset: method === 'limit' ? priceOffset : undefined,
          timeout_sec: timeoutSec,
          auto_retry: autoRetry,
        }
        await onStopAndClose(req)
      }
      onClose()
    } finally {
      setIsLoading(false)
    }
  }

  const displayName = botName || botId

  return (
    <Modal isOpen={isOpen} onClose={onClose} isCentered size="md">
      <ModalOverlay bg="blackAlpha.600" backdropFilter="blur(4px)" />
      <ModalContent bg={bgColor} border="1px solid" borderColor={borderColor} borderRadius="2xl">
        <ModalHeader pb={4}>
          <VStack spacing={3} align="stretch">
            <HStack spacing={3}>
              <Icon as={WarningIcon} color="orange.500" boxSize={6} />
              <Text fontSize="xl" fontWeight="bold">
                {t('globalDashboard.stopConfirm.title')}
              </Text>
            </HStack>
          </VStack>
        </ModalHeader>
        <ModalBody py={4}>
          <VStack align="stretch" spacing={4}>
            <Text color="gray.600" _dark={{ color: 'gray.300' }} fontSize="md">
              {t('globalDashboard.stopConfirm.message', { name: displayName })}
            </Text>

            <RadioGroup value={action} onChange={(v) => setAction(v as StopAction)}>
              <Stack spacing={3}>
                <Radio value="stop_only" colorScheme="orange">
                  {t('globalDashboard.stopConfirm.stopOnly')}
                </Radio>
                <Radio value="stop_and_close" colorScheme="orange">
                  {t('globalDashboard.stopConfirm.stopAndClose')}
                </Radio>
              </Stack>
            </RadioGroup>

            {action === 'stop_and_close' && (
              <VStack align="stretch" spacing={4} pt={2} pl={6} borderLeftWidth="2px" borderColor="orange.200" _dark={{ borderColor: 'orange.700' }}>
                <FormControl>
                  <FormLabel fontSize="sm">{t('globalDashboard.closePositions.method')}</FormLabel>
                  <Select
                    value={method}
                    onChange={(e) => setMethod(e.target.value as 'market' | 'limit')}
                    size="sm"
                  >
                    <option value="market">{t('globalDashboard.closePositions.market')}</option>
                    <option value="limit">{t('globalDashboard.closePositions.limit')}</option>
                  </Select>
                </FormControl>

                {method === 'limit' && (
                  <>
                    <Text fontSize="xs" color="orange.600" _dark={{ color: 'orange.400' }}>
                      {t('globalDashboard.stopConfirm.limitHint')}
                    </Text>
                    <FormControl>
                      <FormLabel fontSize="sm">{t('globalDashboard.closePositions.priceOffset')}</FormLabel>
                      <NumberInput
                        value={priceOffset}
                        onChange={(_, value) => setPriceOffset(value ?? -0.1)}
                        step={0.05}
                        size="sm"
                      >
                        <NumberInputField />
                        <NumberInputStepper>
                          <NumberIncrementStepper />
                          <NumberDecrementStepper />
                        </NumberInputStepper>
                      </NumberInput>
                      <Text fontSize="xs" color="gray.500" mt={1}>
                        {t('globalDashboard.closePositions.priceOffsetDesc')}
                      </Text>
                    </FormControl>
                  </>
                )}

                <FormControl>
                  <FormLabel fontSize="sm">{t('globalDashboard.closePositions.timeout')}</FormLabel>
                  <NumberInput
                    value={timeoutSec}
                    onChange={(_, value) => setTimeoutSec(value ?? 3600)}
                    min={0}
                    max={86400}
                    size="sm"
                  >
                    <NumberInputField />
                    <NumberInputStepper>
                      <NumberIncrementStepper />
                      <NumberDecrementStepper />
                    </NumberInputStepper>
                  </NumberInput>
                </FormControl>

                <FormControl display="flex" alignItems="center">
                  <FormLabel htmlFor="auto-retry" mb="0" fontSize="sm">
                    {t('globalDashboard.closePositions.autoRetry')}
                  </FormLabel>
                  <Switch
                    id="auto-retry"
                    isChecked={autoRetry}
                    onChange={(e) => setAutoRetry(e.target.checked)}
                    colorScheme="orange"
                  />
                </FormControl>
              </VStack>
            )}
          </VStack>
        </ModalBody>
        <ModalFooter gap={3}>
          <Button variant="ghost" onClick={onClose} isDisabled={isLoading} borderRadius="lg">
            {t('common.cancel')}
          </Button>
          <Button
            colorScheme="orange"
            onClick={handleConfirm}
            isLoading={isLoading}
            borderRadius="lg"
            fontWeight="semibold"
          >
            {action === 'stop_only'
              ? t('globalDashboard.stopConfirm.confirmStopOnly')
              : t('globalDashboard.stopConfirm.confirmStopAndClose')}
          </Button>
        </ModalFooter>
      </ModalContent>
    </Modal>
  )
}

export default StopWithCloseConfirmDialog
