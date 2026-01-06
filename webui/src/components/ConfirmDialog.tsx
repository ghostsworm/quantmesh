import React from 'react'
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
  useColorModeValue,
} from '@chakra-ui/react'
import { WarningIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'

export interface ConfirmDialogProps {
  isOpen: boolean
  onClose: () => void
  onConfirm: () => void | Promise<void>
  title: string
  message: string
  confirmText?: string
  cancelText?: string
  confirmColorScheme?: string
  isLoading?: boolean
}

const ConfirmDialog: React.FC<ConfirmDialogProps> = ({
  isOpen,
  onClose,
  onConfirm,
  title,
  message,
  confirmText,
  cancelText,
  confirmColorScheme = 'red',
  isLoading = false,
}) => {
  const { t } = useTranslation()
  const bgColor = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue('gray.200', 'gray.700')

  const handleConfirm = async () => {
    await onConfirm()
    onClose()
  }

  return (
    <Modal isOpen={isOpen} onClose={onClose} isCentered size="md">
      <ModalOverlay bg="blackAlpha.600" backdropFilter="blur(4px)" />
      <ModalContent bg={bgColor} border="1px solid" borderColor={borderColor} borderRadius="2xl">
        <ModalHeader pb={4}>
          <VStack spacing={3} align="stretch">
            <HStack spacing={3}>
              <Icon as={WarningIcon} color={`${confirmColorScheme}.500`} boxSize={6} />
              <Text fontSize="xl" fontWeight="bold">{title}</Text>
            </HStack>
          </VStack>
        </ModalHeader>
        <ModalBody py={6}>
          <Text color="gray.600" _dark={{ color: 'gray.300' }} fontSize="md" lineHeight="tall">
            {message}
          </Text>
        </ModalBody>
        <ModalFooter gap={3}>
          <Button
            variant="ghost"
            onClick={onClose}
            isDisabled={isLoading}
            borderRadius="lg"
          >
            {cancelText || t('common.cancel')}
          </Button>
          <Button
            colorScheme={confirmColorScheme}
            onClick={handleConfirm}
            isLoading={isLoading}
            borderRadius="lg"
            fontWeight="semibold"
          >
            {confirmText || t('common.confirm')}
          </Button>
        </ModalFooter>
      </ModalContent>
    </Modal>
  )
}

export default ConfirmDialog

