import React, { useState, useEffect } from 'react'
import {
  Modal,
  ModalOverlay,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalFooter,
  ModalCloseButton,
  Button,
  VStack,
  HStack,
  Text,
  Input,
  InputGroup,
  InputRightAddon,
  Select,
  FormControl,
  FormLabel,
  FormHelperText,
  Alert,
  AlertIcon,
  Divider,
  Box,
  useToast,
  useColorModeValue,
} from '@chakra-ui/react'
import { useTranslation } from 'react-i18next'
import { withdrawProfit, estimateWithdrawFee } from '../../services/profit'
import type { ManualWithdrawRequest, WithdrawDestination, StrategyProfit } from '../../types/profit'

interface WithdrawDialogProps {
  isOpen: boolean
  onClose: () => void
  strategyProfits: StrategyProfit[]
  availableToWithdraw: number
  onWithdrawComplete?: () => void
}

const WithdrawDialog: React.FC<WithdrawDialogProps> = ({
  isOpen,
  onClose,
  strategyProfits,
  availableToWithdraw,
  onWithdrawComplete,
}) => {
  const { t } = useTranslation()
  const toast = useToast()
  const bgColor = useColorModeValue('white', 'gray.800')

  const [strategyId, setStrategyId] = useState<string>('')
  const [amount, setAmount] = useState<string>('')
  const [destination, setDestination] = useState<WithdrawDestination>('account')
  const [walletAddress, setWalletAddress] = useState<string>('')
  const [loading, setLoading] = useState(false)
  const [estimating, setEstimating] = useState(false)
  const [estimate, setEstimate] = useState<{
    fee: number
    netAmount: number
    estimatedArrival: string
  } | null>(null)

  const selectedStrategy = strategyProfits.find((s) => s.strategyId === strategyId)
  const maxAmount = strategyId
    ? selectedStrategy?.availableToWithdraw || 0
    : availableToWithdraw

  useEffect(() => {
    setEstimate(null)
  }, [amount, strategyId, destination])

  const handleEstimate = async () => {
    const numAmount = parseFloat(amount) || 0
    if (numAmount <= 0 || numAmount > maxAmount) {
      toast({
        title: t('profitManagement.invalidAmount'),
        status: 'error',
        duration: 3000,
      })
      return
    }

    if (destination === 'wallet' && !walletAddress) {
      toast({
        title: t('profitManagement.walletAddressRequired'),
        status: 'error',
        duration: 3000,
      })
      return
    }

    setEstimating(true)
    try {
      const request: ManualWithdrawRequest = {
        strategyId: strategyId || undefined,
        amount: numAmount,
        destination,
        walletAddress: destination === 'wallet' ? walletAddress : undefined,
      }
      const result = await estimateWithdrawFee(request)
      setEstimate(result)
    } catch (error) {
      toast({
        title: t('profitManagement.estimateError'),
        description: error instanceof Error ? error.message : 'Unknown error',
        status: 'error',
        duration: 3000,
      })
    } finally {
      setEstimating(false)
    }
  }

  const handleWithdraw = async () => {
    const numAmount = parseFloat(amount) || 0
    if (numAmount <= 0 || numAmount > maxAmount) {
      toast({
        title: t('profitManagement.invalidAmount'),
        status: 'error',
        duration: 3000,
      })
      return
    }

    setLoading(true)
    try {
      const request: ManualWithdrawRequest = {
        strategyId: strategyId || undefined,
        amount: numAmount,
        destination,
        walletAddress: destination === 'wallet' ? walletAddress : undefined,
      }
      const result = await withdrawProfit(request)
      if (result.success) {
        toast({
          title: t('profitManagement.withdrawSuccess'),
          description: result.message,
          status: 'success',
          duration: 5000,
        })
        onWithdrawComplete?.()
        onClose()
      } else {
        toast({
          title: t('profitManagement.withdrawError'),
          description: result.message,
          status: 'error',
          duration: 5000,
        })
      }
    } catch (error) {
      toast({
        title: t('profitManagement.withdrawError'),
        description: error instanceof Error ? error.message : 'Unknown error',
        status: 'error',
        duration: 5000,
      })
    } finally {
      setLoading(false)
    }
  }

  const handleMaxClick = () => {
    setAmount(maxAmount.toFixed(2))
  }

  return (
    <Modal isOpen={isOpen} onClose={onClose} size="md">
      <ModalOverlay bg="blackAlpha.600" backdropFilter="blur(4px)" />
      <ModalContent bg={bgColor}>
        <ModalHeader>{t('profitManagement.withdrawTitle')}</ModalHeader>
        <ModalCloseButton />

        <ModalBody>
          <VStack align="stretch" spacing={4}>
            <Alert status="info" borderRadius="md">
              <AlertIcon />
              <Text fontSize="sm">
                {t('profitManagement.availableToWithdraw')}: {(availableToWithdraw || 0).toFixed(2)} USDT
              </Text>
            </Alert>

            <FormControl>
              <FormLabel>{t('profitManagement.selectStrategy')}</FormLabel>
              <Select
                value={strategyId}
                onChange={(e) => setStrategyId(e.target.value)}
                placeholder={t('profitManagement.allStrategies')}
              >
                {strategyProfits.map((sp) => (
                  <option key={sp.strategyId} value={sp.strategyId}>
                    {sp.strategyName} ({(sp.availableToWithdraw || 0).toFixed(2)} USDT)
                  </option>
                ))}
              </Select>
              <FormHelperText>{t('profitManagement.strategySelectHelp')}</FormHelperText>
            </FormControl>

            <FormControl>
              <FormLabel>{t('profitManagement.withdrawAmount')}</FormLabel>
              <InputGroup>
                <Input
                  type="number"
                  value={amount}
                  onChange={(e) => setAmount(e.target.value)}
                  placeholder="0.00"
                />
                <InputRightAddon>
                  <HStack spacing={2}>
                    <Text>USDT</Text>
                    <Button size="xs" variant="ghost" onClick={handleMaxClick}>
                      MAX
                    </Button>
                  </HStack>
                </InputRightAddon>
              </InputGroup>
              <FormHelperText>
                {t('profitManagement.maxWithdraw')}: {(maxAmount || 0).toFixed(2)} USDT
              </FormHelperText>
            </FormControl>

            <FormControl>
              <FormLabel>{t('profitManagement.destination')}</FormLabel>
              <Select
                value={destination}
                onChange={(e) => setDestination(e.target.value as WithdrawDestination)}
              >
                <option value="account">{t('profitManagement.toAccount')}</option>
                <option value="wallet">{t('profitManagement.toWallet')}</option>
              </Select>
            </FormControl>

            {destination === 'wallet' && (
              <FormControl>
                <FormLabel>{t('profitManagement.walletAddress')}</FormLabel>
                <Input
                  value={walletAddress}
                  onChange={(e) => setWalletAddress(e.target.value)}
                  placeholder="0x..."
                />
              </FormControl>
            )}

            {estimate && (
              <>
                <Divider />
                <Box p={4} bg="gray.50" borderRadius="md">
                  <VStack align="stretch" spacing={2}>
                    <HStack justify="space-between">
                      <Text fontSize="sm" color="gray.600">
                        {t('profitManagement.withdrawAmount')}
                      </Text>
                      <Text fontWeight="medium">{amount} USDT</Text>
                    </HStack>
                    <HStack justify="space-between">
                      <Text fontSize="sm" color="gray.600">
                        {t('profitManagement.fee')}
                      </Text>
                      <Text fontWeight="medium" color="orange.500">
                        -{(estimate.fee || 0).toFixed(2)} USDT
                      </Text>
                    </HStack>
                    <Divider />
                    <HStack justify="space-between">
                      <Text fontWeight="bold">{t('profitManagement.netAmount')}</Text>
                      <Text fontWeight="bold" color="green.500">
                        {(estimate.netAmount || 0).toFixed(2)} USDT
                      </Text>
                    </HStack>
                    <Text fontSize="xs" color="gray.500" textAlign="right">
                      {t('profitManagement.estimatedArrival')}: {estimate.estimatedArrival}
                    </Text>
                  </VStack>
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
              onClick={handleEstimate}
              isLoading={estimating}
              isDisabled={!amount || parseFloat(amount) <= 0}
            >
              {t('profitManagement.estimateFee')}
            </Button>
            <Button
              colorScheme="blue"
              onClick={handleWithdraw}
              isLoading={loading}
              isDisabled={!estimate || loading}
            >
              {t('profitManagement.confirmWithdraw')}
            </Button>
          </HStack>
        </ModalFooter>
      </ModalContent>
    </Modal>
  )
}

export default WithdrawDialog
