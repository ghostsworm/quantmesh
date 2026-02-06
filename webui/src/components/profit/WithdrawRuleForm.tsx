import React, { useEffect, useState } from 'react'
import {
  Box,
  VStack,
  HStack,
  Text,
  Button,
  Input,
  InputGroup,
  InputRightAddon,
  Select,
  Switch,
  FormControl,
  FormLabel,
  FormHelperText,
  IconButton,
  Collapse,
  useDisclosure,
  useToast,
  Divider,
  Badge,
  useColorModeValue,
  Menu,
  MenuButton,
  MenuList,
  MenuItem,
} from '@chakra-ui/react'
import { ChevronDownIcon, ChevronUpIcon, DeleteIcon, AddIcon, DownloadIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import type { ProfitWithdrawRule, WithdrawFrequency, WithdrawDestination } from '../../types/profit'

interface WithdrawRuleFormProps {
  rules: ProfitWithdrawRule[]
  strategyOptions: { id: string; name: string }[]
  onSave: (rules: ProfitWithdrawRule[]) => Promise<void>
  loading?: boolean
  activeExchange?: string
  exchangeOptions?: string[]
}

const DEFAULT_RULE: Omit<ProfitWithdrawRule, 'id' | 'createdAt' | 'updatedAt'> = {
  exchangeId: '',
  strategyId: '',
  enabled: true,
  triggerAmount: 100,
  withdrawRatio: 0.5,
  frequency: 'daily',
  destination: 'account',
  minWithdrawAmount: 10,
}

// 預設规则模板
const PRESET_RULES: Array<Omit<ProfitWithdrawRule, 'id' | 'createdAt' | 'updatedAt' | 'exchangeId'>> = [
  {
    // 保守型：盈利達到100 USDT時，提取50%，最小提取10 USDT
    strategyId: '',
    enabled: true,
    triggerAmount: 100,
    withdrawRatio: 0.5, // 50%
    frequency: 'immediate',
    destination: 'account',
    minWithdrawAmount: 10,
  },
  {
    // 激進型：盈利達到200 USDT時，提取30%，最小提取20 USDT
    strategyId: '',
    enabled: true,
    triggerAmount: 200,
    withdrawRatio: 0.3, // 30%
    frequency: 'daily',
    destination: 'account',
    minWithdrawAmount: 20,
  },
]

const WithdrawRuleForm: React.FC<WithdrawRuleFormProps> = ({
  rules,
  strategyOptions,
  onSave,
  loading,
  activeExchange,
  exchangeOptions,
}) => {
  const { t } = useTranslation()
  const toast = useToast()
  const { isOpen, onToggle } = useDisclosure({ defaultIsOpen: true })
  const [localRules, setLocalRules] = useState<ProfitWithdrawRule[]>(rules)
  const [saving, setSaving] = useState(false)
  const [hasChanges, setHasChanges] = useState(false)

  const bgColor = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue('gray.200', 'gray.600')

  useEffect(() => {
    setLocalRules(rules)
    setHasChanges(false)
  }, [rules])

  const formatExchangeName = (ex: string) => {
    const v = (ex || '').toLowerCase()
    if (v === 'binance') return 'Binance'
    if (v === 'gate') return 'Gate.io'
    if (v === 'okx') return 'OKX'
    return ex ? ex.charAt(0).toUpperCase() + ex.slice(1) : '-'
  }

  const getDefaultExchangeId = () => {
    if (activeExchange && activeExchange !== 'all') return activeExchange
    if (localRules.length > 0 && localRules[0].exchangeId) return localRules[0].exchangeId
    if (rules.length > 0 && rules[0].exchangeId) return rules[0].exchangeId
    return ''
  }

  const resolvedExchangeOptions =
    exchangeOptions && exchangeOptions.length > 0
      ? exchangeOptions
      : Array.from(new Set(localRules.map((r) => r.exchangeId).filter(Boolean)))

  const handleAddRule = () => {
    const newRule: ProfitWithdrawRule = {
      ...DEFAULT_RULE,
      exchangeId: getDefaultExchangeId(),
      id: `temp-${Date.now()}`,
      createdAt: new Date().toISOString(),
      updatedAt: new Date().toISOString(),
    }
    setLocalRules([...localRules, newRule])
    setHasChanges(true)
  }

  const handleImportPreset = (presetIndex: number) => {
    const preset = PRESET_RULES[presetIndex]
    if (!preset) return

    const exchangeId = getDefaultExchangeId()

    const newRule: ProfitWithdrawRule = {
      ...preset,
      exchangeId,
      id: `temp-${Date.now()}-${presetIndex}`,
      createdAt: new Date().toISOString(),
      updatedAt: new Date().toISOString(),
    }
    setLocalRules([...localRules, newRule])
    setHasChanges(true)
    
    toast({
      title: presetIndex === 0 ? t('profitManagement.importedConservative') : t('profitManagement.importedAggressive'),
      description: presetIndex === 0 
        ? t('profitManagement.importedConservativeDesc')
        : t('profitManagement.importedAggressiveDesc'),
      status: 'success',
      duration: 3000,
    })
  }

  const handleImportBothPresets = () => {
    const exchangeId = getDefaultExchangeId()

    const newRules: ProfitWithdrawRule[] = PRESET_RULES.map((preset, index) => ({
      ...preset,
      exchangeId,
      id: `temp-${Date.now()}-${index}`,
      createdAt: new Date().toISOString(),
      updatedAt: new Date().toISOString(),
    }))
    setLocalRules([...localRules, ...newRules])
    setHasChanges(true)
    
    toast({
      title: t('profitManagement.importedBothPresets'),
      description: t('profitManagement.importedBothPresetsDesc'),
      status: 'success',
      duration: 3000,
    })
  }

  const handleRemoveRule = (ruleId: string) => {
    setLocalRules(localRules.filter((r) => r.id !== ruleId))
    setHasChanges(true)
  }

  const handleUpdateRule = (ruleId: string, updates: Partial<ProfitWithdrawRule>) => {
    setLocalRules(
      localRules.map((r) =>
        r.id === ruleId ? { ...r, ...updates, updatedAt: new Date().toISOString() } : r
      )
    )
    setHasChanges(true)
  }

  const handleSave = async () => {
    setSaving(true)
    try {
      await onSave(localRules)
      toast({
        title: t('profitManagement.rulesSaved'),
        status: 'success',
        duration: 3000,
      })
      setHasChanges(false)
    } catch (error) {
      toast({
        title: t('profitManagement.saveError'),
        description: error instanceof Error ? error.message : 'Unknown error',
        status: 'error',
        duration: 3000,
      })
    } finally {
      setSaving(false)
    }
  }

  return (
    <Box
      p={6}
      borderWidth="1px"
      borderRadius="xl"
      borderColor={borderColor}
      bg={bgColor}
    >
      <HStack justify="space-between" mb={4}>
        <HStack>
          <Text fontWeight="bold" fontSize="lg">
            {t('profitManagement.autoWithdrawRules')}
          </Text>
          <Badge colorScheme="blue">{localRules.length}</Badge>
        </HStack>
        <HStack>
          <Button
            size="sm"
            leftIcon={<AddIcon />}
            variant="outline"
            onClick={handleAddRule}
          >
            {t('profitManagement.addRule')}
          </Button>
          <Menu>
            <MenuButton
              as={Button}
              size="sm"
              leftIcon={<DownloadIcon />}
              variant="outline"
              colorScheme="blue"
            >
              {t('profitManagement.quickImport')}
            </MenuButton>
            <MenuList>
              <MenuItem onClick={() => handleImportPreset(0)}>
                {t('profitManagement.conservativeRule')}
              </MenuItem>
              <MenuItem onClick={() => handleImportPreset(1)}>
                {t('profitManagement.aggressiveRule')}
              </MenuItem>
              <MenuItem onClick={handleImportBothPresets}>
                {t('profitManagement.importBothPresets')}
              </MenuItem>
            </MenuList>
          </Menu>
          <IconButton
            aria-label="Toggle rules"
            icon={isOpen ? <ChevronUpIcon /> : <ChevronDownIcon />}
            size="sm"
            variant="ghost"
            onClick={onToggle}
          />
        </HStack>
      </HStack>

      <Collapse in={isOpen}>
        <VStack align="stretch" spacing={4}>
          {localRules.length === 0 ? (
            <Box textAlign="center" py={8} color="gray.500">
              <Text>{t('profitManagement.noRules')}</Text>
              <Button
                mt={4}
                size="sm"
                leftIcon={<AddIcon />}
                onClick={handleAddRule}
              >
                {t('profitManagement.createFirstRule')}
              </Button>
            </Box>
          ) : (
            localRules.map((rule, index) => (
              <Box
                key={rule.id}
                p={4}
                borderWidth="1px"
                borderRadius="lg"
                borderColor={rule.enabled ? 'blue.200' : 'gray.200'}
                bg={rule.enabled ? 'blue.50' : 'gray.50'}
                position="relative"
              >
                <IconButton
                  aria-label="Delete rule"
                  icon={<DeleteIcon />}
                  size="sm"
                  colorScheme="red"
                  variant="ghost"
                  position="absolute"
                  top={2}
                  right={2}
                  onClick={() => handleRemoveRule(rule.id)}
                />

                <VStack align="stretch" spacing={3}>
                  <HStack justify="space-between">
                    <FormControl display="flex" alignItems="center" w="auto">
                      <Switch
                        isChecked={rule.enabled}
                        onChange={(e) =>
                          handleUpdateRule(rule.id, { enabled: e.target.checked })
                        }
                      />
                      <FormLabel mb={0} ml={2}>
                        {rule.enabled
                          ? t('profitManagement.ruleEnabled')
                          : t('profitManagement.ruleDisabled')}
                      </FormLabel>
                    </FormControl>
                    <HStack spacing={2} mr={10}>
                      <Badge colorScheme="purple">
                        {formatExchangeName(rule.exchangeId)}
                      </Badge>
                      <Badge colorScheme={rule.enabled ? 'green' : 'gray'}>
                        {t('profitManagement.rule')} #{index + 1}
                      </Badge>
                    </HStack>
                  </HStack>

                  <HStack spacing={4} flexWrap="wrap">
                    <FormControl flex={1} minW="200px">
                      <FormLabel fontSize="sm">{t('profitManagement.exchangeLabel')}</FormLabel>
                      <Select
                        size="sm"
                        value={rule.exchangeId || ''}
                        onChange={(e) => handleUpdateRule(rule.id, { exchangeId: e.target.value })}
                        isDisabled={!!activeExchange && activeExchange !== 'all'}
                      >
                        {(!activeExchange || activeExchange === 'all') && (
                          <option value="">{t('profitManagement.selectExchange')}</option>
                        )}
                        {(activeExchange && activeExchange !== 'all'
                          ? [activeExchange]
                          : resolvedExchangeOptions
                        ).map((ex) => (
                          <option key={ex} value={ex}>
                            {formatExchangeName(ex)}
                          </option>
                        ))}
                      </Select>
                      <FormHelperText>
                        {activeExchange === 'all'
                          ? t('profitManagement.ruleHintAll')
                          : t('profitManagement.ruleHintExchange')}
                      </FormHelperText>
                    </FormControl>
                  </HStack>

                  <HStack spacing={4} flexWrap="wrap">
                    <FormControl flex={1} minW="200px">
                      <FormLabel fontSize="sm">{t('profitManagement.strategy')}</FormLabel>
                      <Select
                        size="sm"
                        value={rule.strategyId}
                        onChange={(e) =>
                          handleUpdateRule(rule.id, { strategyId: e.target.value })
                        }
                      >
                        <option value="">{t('profitManagement.allStrategies')}</option>
                        {strategyOptions.map((s) => (
                          <option key={s.id} value={s.id}>
                            {s.name}
                          </option>
                        ))}
                      </Select>
                    </FormControl>

                    <FormControl flex={1} minW="150px">
                      <FormLabel fontSize="sm">{t('profitManagement.triggerAmount')}</FormLabel>
                      <InputGroup size="sm">
                        <Input
                          type="number"
                          value={rule.triggerAmount}
                          onChange={(e) =>
                            handleUpdateRule(rule.id, {
                              triggerAmount: parseFloat(e.target.value) || 0,
                            })
                          }
                        />
                        <InputRightAddon>USDT</InputRightAddon>
                      </InputGroup>
                    </FormControl>

                    <FormControl flex={1} minW="150px">
                      <FormLabel fontSize="sm">{t('profitManagement.withdrawRatio')}</FormLabel>
                      <InputGroup size="sm">
                        <Input
                          type="number"
                          value={((rule.withdrawRatio || 0) * 100).toFixed(0)}
                          onChange={(e) =>
                            handleUpdateRule(rule.id, {
                              withdrawRatio: (parseFloat(e.target.value) || 0) / 100,
                            })
                          }
                          min={0}
                          max={100}
                        />
                        <InputRightAddon>%</InputRightAddon>
                      </InputGroup>
                    </FormControl>
                  </HStack>

                  <HStack spacing={4} flexWrap="wrap">
                    <FormControl flex={1} minW="150px">
                      <FormLabel fontSize="sm">{t('profitManagement.frequency')}</FormLabel>
                      <Select
                        size="sm"
                        value={rule.frequency}
                        onChange={(e) =>
                          handleUpdateRule(rule.id, {
                            frequency: e.target.value as WithdrawFrequency,
                          })
                        }
                      >
                        <option value="immediate">{t('profitManagement.freqImmediate')}</option>
                        <option value="daily">{t('profitManagement.freqDaily')}</option>
                        <option value="weekly">{t('profitManagement.freqWeekly')}</option>
                      </Select>
                    </FormControl>

                    <FormControl flex={1} minW="150px">
                      <FormLabel fontSize="sm">{t('profitManagement.destination')}</FormLabel>
                      <Select
                        size="sm"
                        value={rule.destination}
                        onChange={(e) =>
                          handleUpdateRule(rule.id, {
                            destination: e.target.value as WithdrawDestination,
                          })
                        }
                      >
                        <option value="account">{t('profitManagement.toAccount')}</option>
                        <option value="wallet">{t('profitManagement.toWallet')}</option>
                      </Select>
                    </FormControl>

                    <FormControl flex={1} minW="150px">
                      <FormLabel fontSize="sm">{t('profitManagement.minWithdraw')}</FormLabel>
                      <InputGroup size="sm">
                        <Input
                          type="number"
                          value={rule.minWithdrawAmount}
                          onChange={(e) =>
                            handleUpdateRule(rule.id, {
                              minWithdrawAmount: parseFloat(e.target.value) || 0,
                            })
                          }
                        />
                        <InputRightAddon>USDT</InputRightAddon>
                      </InputGroup>
                    </FormControl>
                  </HStack>

                  {rule.destination === 'wallet' && (
                    <FormControl>
                      <FormLabel fontSize="sm">{t('profitManagement.walletAddress')}</FormLabel>
                      <Input
                        size="sm"
                        value={rule.walletAddress || ''}
                        onChange={(e) =>
                          handleUpdateRule(rule.id, { walletAddress: e.target.value })
                        }
                        placeholder="0x..."
                      />
                    </FormControl>
                  )}
                </VStack>
              </Box>
            ))
          )}

          {hasChanges && (
            <>
              <Divider />
              <HStack justify="flex-end">
                <Button
                  colorScheme="blue"
                  onClick={handleSave}
                  isLoading={saving || loading}
                >
                  {t('profitManagement.saveRules')}
                </Button>
              </HStack>
            </>
          )}
        </VStack>
      </Collapse>
    </Box>
  )
}

export default WithdrawRuleForm
