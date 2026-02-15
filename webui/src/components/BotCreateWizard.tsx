import React, { useState, useEffect } from 'react'
import {
  Box,
  Button,
  Card,
  CardBody,
  FormControl,
  FormLabel,
  Heading,
  Input,
  NumberInput,
  NumberInputField,
  Select,
  Stepper,
  Step,
  StepDescription,
  StepIndicator,
  StepNumber,
  StepSeparator,
  StepStatus,
  StepTitle,
  useToast,
  VStack,
  Text,
  HStack,
  Spinner,
} from '@chakra-ui/react'
import { ChevronLeftIcon } from '@chakra-ui/icons'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate } from 'react-router-dom'
import { getConfig, updateConfig, Config, SymbolConfig } from '../services/config'
import { getExchangeSymbols } from '../services/setup'
import { getExchanges, getBots } from '../services/api'

const STEPS = 3

const BotCreateWizard: React.FC = () => {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const toast = useToast()
  const [step, setStep] = useState(0)
  const [config, setConfig] = useState<Config | null>(null)
  const [exchanges, setExchanges] = useState<string[]>([])
  const [symbols, setSymbols] = useState<string[]>([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)

  const [form, setForm] = useState<Partial<SymbolConfig>>({
    exchange: 'binance',
    symbol: '',
    market_type: 'futures',
    name: '',
    enabled: true,
    price_interval: 2,
    order_quantity: 30,
    buy_window_size: 10,
    sell_window_size: 10,
    direction: 'LONG',
  })

  useEffect(() => {
    const load = async () => {
      try {
        const [cfg, exRes] = await Promise.all([
          getConfig().catch(() => null),
          getExchanges().catch(() => ({ exchanges: [] })),
        ])
        setConfig(cfg)
        setExchanges(exRes.exchanges || [])
      } catch (err) {
        console.error(err)
      } finally {
        setLoading(false)
      }
    }
    load()
  }, [])

  useEffect(() => {
    if (!form.exchange || !config?.exchanges) return
    const exCfg = config.exchanges[form.exchange]
    if (!exCfg?.api_key || !exCfg?.secret_key) {
      setSymbols([])
      return
    }
    const load = async () => {
      try {
        const res = await getExchangeSymbols({
          exchange: form.exchange!,
          api_key: exCfg.api_key,
          secret_key: exCfg.secret_key,
          passphrase: exCfg.passphrase || '',
          market_type: form.market_type === 'spot' ? 'spot' : 'futures',
        })
        setSymbols(res.symbols || [])
      } catch {
        setSymbols([])
      }
    }
    load()
  }, [form.exchange, form.market_type, config?.exchanges])

  const handleSubmit = async () => {
    if (!config || !form.exchange || !form.symbol) {
      toast({ title: t('botCreate.fillRequired'), status: 'error', duration: 3000 })
      return
    }
    setSaving(true)
    try {
      const mt = form.market_type || 'futures'
      const symbols = Array.isArray(config.trading?.symbols) ? [...config.trading.symbols] : []
      // 檢查同交易所+交易對+市場類型是否已存在
      const exists = symbols.some(
        (s) =>
          (s.exchange || '').toLowerCase() === (form.exchange || '').toLowerCase() &&
          (s.symbol || '').toUpperCase() === (form.symbol || '').toUpperCase() &&
          (s.market_type || 'futures') === mt
      )
      if (exists) {
        toast({ title: t('botCreate.alreadyExists'), status: 'warning', duration: 3000 })
        setSaving(false)
        return
      }
      const newSymbol: SymbolConfig = {
        ...(form.name?.trim() && { name: form.name.trim() }),
        exchange: form.exchange,
        symbol: form.symbol,
        market_type: mt,
        enabled: form.enabled ?? true,
        price_interval: form.price_interval ?? 2,
        order_quantity: form.order_quantity ?? 30,
        buy_window_size: form.buy_window_size ?? 10,
        sell_window_size: form.sell_window_size ?? 10,
        direction: form.direction || 'LONG',
      }
      symbols.push(newSymbol)
      const updated = { ...config, trading: { ...config.trading, symbols } }
      await updateConfig(updated)
      toast({ title: t('botCreate.success'), status: 'success', duration: 2000 })

      const botsRes = await getBots()
      const bot = (botsRes.bots || []).find(
        (b) =>
          b.exchange === newSymbol.exchange &&
          b.symbol === newSymbol.symbol &&
          b.market_type === (newSymbol.market_type || 'futures')
      )
      if (bot) {
        navigate(`/bots/${bot.bot_id}`)
      } else {
        navigate('/bots')
      }
    } catch (err) {
      toast({ title: t('botCreate.failed'), status: 'error', duration: 3000 })
    } finally {
      setSaving(false)
    }
  }

  if (loading) {
    return (
      <Box textAlign="center" py={12}>
        <Spinner size="lg" />
      </Box>
    )
  }

  return (
    <Box>
      <Button as={Link} to="/bots" leftIcon={<ChevronLeftIcon />} variant="ghost" size="sm" mb={4}>
        {t('common.back')}
      </Button>
      <Heading size="lg" mb={6}>
        {t('botCreate.title')}
      </Heading>

      <Stepper index={step} mb={8}>
        {[0, 1, 2].map((i) => (
          <Step key={i}>
            <StepIndicator>
              <StepStatus complete={<StepNumber />} incomplete={<StepNumber />} active={<StepNumber />} />
            </StepIndicator>
            <Box flexShrink="0">
              <StepTitle>
                {i === 0 && t('botCreate.step1Title')}
                {i === 1 && t('botCreate.step2Title')}
                {i === 2 && t('botCreate.step3Title')}
              </StepTitle>
              <StepDescription>
                {i === 0 && t('botCreate.step1Desc')}
                {i === 1 && t('botCreate.step2Desc')}
                {i === 2 && t('botCreate.step3Desc')}
              </StepDescription>
            </Box>
            <StepSeparator />
          </Step>
        ))}
      </Stepper>

      <Card>
        <CardBody>
          {step === 0 && (
            <VStack spacing={4} align="stretch">
              <FormControl isRequired>
                <FormLabel>{t('configSetup.exchange')}</FormLabel>
                <Select
                  value={form.exchange || ''}
                  onChange={(e) => setForm((f) => ({ ...f, exchange: e.target.value }))}
                >
                  {exchanges.map((ex) => (
                    <option key={ex} value={ex}>
                      {ex.toUpperCase()}
                    </option>
                  ))}
                </Select>
              </FormControl>
              <FormControl isRequired>
                <FormLabel>{t('botCreate.marketType')}</FormLabel>
                <Select
                  value={form.market_type || 'futures'}
                  onChange={(e) => setForm((f) => ({ ...f, market_type: e.target.value as 'spot' | 'futures' }))}
                >
                  <option value="futures">{t('symbolManager.futures')}</option>
                  <option value="spot">{t('symbolManager.spot')}</option>
                </Select>
              </FormControl>
              <FormControl isRequired>
                <FormLabel>{t('configSetup.symbol')}</FormLabel>
                <Select
                  value={form.symbol || ''}
                  onChange={(e) => setForm((f) => ({ ...f, symbol: e.target.value }))}
                  placeholder={t('botCreate.selectSymbol')}
                >
                  {symbols.map((s) => (
                    <option key={s} value={s}>
                      {s}
                    </option>
                  ))}
                </Select>
              </FormControl>
              <FormControl>
                <FormLabel>{t('botCreate.botName')}</FormLabel>
                <Input
                  value={form.name || ''}
                  onChange={(e) => setForm((f) => ({ ...f, name: e.target.value.trim() }))}
                  placeholder={t('botCreate.botNamePlaceholder')}
                />
              </FormControl>
            </VStack>
          )}

          {step === 1 && (
            <VStack spacing={4} align="stretch">
              <FormControl>
                <FormLabel>{t('botCreate.priceInterval')}</FormLabel>
                <NumberInput
                  value={form.price_interval ?? 2}
                  min={0.01}
                  step={0.1}
                  onChange={(_, v) => setForm((f) => ({ ...f, price_interval: v }))}
                >
                  <NumberInputField />
                </NumberInput>
              </FormControl>
              <FormControl>
                <FormLabel>{t('botCreate.orderQuantity')}</FormLabel>
                <NumberInput
                  value={form.order_quantity ?? 30}
                  min={1}
                  onChange={(_, v) => setForm((f) => ({ ...f, order_quantity: v }))}
                >
                  <NumberInputField />
                </NumberInput>
              </FormControl>
              <FormControl>
                <FormLabel>{t('botCreate.buyWindowSize')}</FormLabel>
                <NumberInput
                  value={form.buy_window_size ?? 10}
                  min={1}
                  onChange={(_, v) => setForm((f) => ({ ...f, buy_window_size: v }))}
                >
                  <NumberInputField />
                </NumberInput>
              </FormControl>
              <FormControl>
                <FormLabel>{t('botCreate.sellWindowSize')}</FormLabel>
                <NumberInput
                  value={form.sell_window_size ?? 10}
                  min={1}
                  onChange={(_, v) => setForm((f) => ({ ...f, sell_window_size: v }))}
                >
                  <NumberInputField />
                </NumberInput>
              </FormControl>
            </VStack>
          )}

          {step === 2 && (
            <VStack spacing={4} align="stretch">
              <Text>{t('botCreate.reviewDesc')}</Text>
              <Box bg="gray.50" p={4} borderRadius="md" fontSize="sm">
                {form.name && (
                  <Text><strong>{t('botCreate.botName')}:</strong> {form.name}</Text>
                )}
                <Text><strong>{t('configSetup.exchange')}:</strong> {form.exchange}</Text>
                <Text><strong>{t('configSetup.symbol')}:</strong> {form.symbol}</Text>
                <Text><strong>{t('botCreate.marketType')}:</strong> {form.market_type}</Text>
                <Text><strong>{t('botCreate.priceInterval')}:</strong> {form.price_interval}</Text>
                <Text><strong>{t('botCreate.orderQuantity')}:</strong> {form.order_quantity}</Text>
              </Box>
            </VStack>
          )}

          <HStack mt={6} justify="space-between">
            <Button
              isDisabled={step === 0}
              variant="ghost"
              onClick={() => setStep((s) => Math.max(0, s - 1))}
            >
              {t('common.back')}
            </Button>
            {step < STEPS - 1 ? (
              <Button
                colorScheme="blue"
                onClick={() => setStep((s) => Math.min(STEPS - 1, s + 1))}
                isDisabled={step === 0 && (!form.exchange || !form.symbol)}
              >
                {t('botCreate.next')}
              </Button>
            ) : (
              <Button colorScheme="green" onClick={handleSubmit} isLoading={saving}>
                {t('botCreate.create')}
              </Button>
            )}
          </HStack>
        </CardBody>
      </Card>
    </Box>
  )
}

export default BotCreateWizard
