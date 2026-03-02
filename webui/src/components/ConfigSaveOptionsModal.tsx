import React, { useState } from 'react'
import {
  Modal,
  ModalOverlay,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalFooter,
  ModalCloseButton,
  Button,
  Text,
  VStack,
  HStack,
  Checkbox,
  useColorModeValue,
} from '@chakra-ui/react'
import { useTranslation } from 'react-i18next'

export interface SymbolTarget {
  exchange: string
  symbol: string
}

export interface ConfigSaveOptionsModalProps {
  isOpen: boolean
  onClose: () => void
  onConfirm: (options: { cancelOrders: boolean; closePositions: boolean }) => Promise<void>
  targets: SymbolTarget[]
  isLoading?: boolean
}

const ConfigSaveOptionsModal: React.FC<ConfigSaveOptionsModalProps> = ({
  isOpen,
  onClose,
  onConfirm,
  targets,
  isLoading = false,
}) => {
  const { t } = useTranslation()
  const [cancelOrders, setCancelOrders] = useState(false)
  const [closePositions, setClosePositions] = useState(false)
  const [executing, setExecuting] = useState(false)
  const bgColor = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue('gray.200', 'gray.700')

  const handleConfirm = async () => {
    setExecuting(true)
    try {
      await onConfirm({ cancelOrders, closePositions })
      onClose()
    } finally {
      setExecuting(false)
    }
  }

  const targetLabel =
    targets.length === 1
      ? `${targets[0].exchange}:${targets[0].symbol}`
      : t('configuration.saveOptionsTargetsAll', { count: targets.length })

  return (
    <Modal isOpen={isOpen} onClose={onClose} isCentered size="md">
      <ModalOverlay bg="blackAlpha.600" backdropFilter="blur(4px)" />
      <ModalContent bg={bgColor} border="1px solid" borderColor={borderColor} borderRadius="2xl">
        <ModalHeader pb={2}>
          <Text fontSize="lg" fontWeight="bold">
            {t('configuration.saveOptionsTitle')}
          </Text>
        </ModalHeader>
        <ModalCloseButton />

        <ModalBody py={4}>
          <VStack align="stretch" spacing={4}>
            <Text fontSize="sm" color="gray.600" _dark={{ color: 'gray.300' }}>
              {t('configuration.saveOptionsDesc', { target: targetLabel })}
            </Text>

            <VStack align="stretch" spacing={3} pl={2}>
              <Checkbox
                isChecked={cancelOrders}
                onChange={(e) => setCancelOrders(e.target.checked)}
                colorScheme="blue"
              >
                <Text fontSize="sm">{t('configuration.saveOptionsCancelOrders')}</Text>
              </Checkbox>
              <Text fontSize="xs" color="gray.500" pl={6}>
                {t('configuration.saveOptionsCancelOrdersDesc')}
              </Text>

              <Checkbox
                isChecked={closePositions}
                onChange={(e) => setClosePositions(e.target.checked)}
                colorScheme="blue"
              >
                <Text fontSize="sm">{t('configuration.saveOptionsClosePositions')}</Text>
              </Checkbox>
              <Text fontSize="xs" color="gray.500" pl={6}>
                {t('configuration.saveOptionsClosePositionsDesc')}
              </Text>
            </VStack>
          </VStack>
        </ModalBody>

        <ModalFooter gap={3}>
          <Button variant="ghost" onClick={onClose} isDisabled={executing || isLoading}>
            {t('common.cancel')}
          </Button>
          <Button
            colorScheme="blue"
            onClick={handleConfirm}
            isLoading={executing || isLoading}
            borderRadius="lg"
          >
            {t('configuration.saveOptionsConfirm')}
          </Button>
        </ModalFooter>
      </ModalContent>
    </Modal>
  )
}

export default ConfigSaveOptionsModal
