import React, { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  Alert,
  AlertIcon,
  Badge,
  Box,
  Button,
  Code,
  Container,
  Divider,
  FormControl,
  FormHelperText,
  FormLabel,
  HStack,
  Heading,
  IconButton,
  Input,
  Link,
  Select,
  Spinner,
  Stack,
  Switch,
  Text,
  Textarea,
  useColorModeValue,
  useToast,
  VStack,
} from '@chakra-ui/react'
import { CopyIcon, RepeatIcon } from '@chakra-ui/icons'

import {
  MCPConfig,
  MCPSnippetStyle,
  ObservabilityConfig,
  clearMCPToken,
  getMCPClientSnippet,
  getMCPConfig,
  getObservabilityConfig,
  rotateMCPToken,
  testObservabilityConnection,
  updateMCPConfig,
  updateObservabilityConfig,
} from '../services/globalSettings'

const SNIPPET_STYLES: MCPSnippetStyle[] = ['claude', 'cursor', 'generic']

const GlobalSettings: React.FC = () => {
  const { t } = useTranslation()
  const toast = useToast()
  const cardBg = useColorModeValue('white', 'gray.800')
  const codeBg = useColorModeValue('gray.50', 'gray.900')


  // ── observability state ─────────────────────────────────────
  const [observabilityLoading, setObservabilityLoading] = useState(true)
  const [observabilityCfg, setObservabilityCfg] = useState<ObservabilityConfig | null>(null)
  const [posthogKeyInput, setPosthogKeyInput] = useState('')
  const [posthogHostInput, setPosthogHostInput] = useState('')
  const [posthogEnabled, setPosthogEnabled] = useState(false)
  const [sentryDsnInput, setSentryDsnInput] = useState('')
  const [sentryEnabled, setSentryEnabled] = useState(false)
  const [observabilityEnvironment, setObservabilityEnvironment] = useState('')
  const [observabilitySaving, setObservabilitySaving] = useState(false)
  const [posthogTesting, setPosthogTesting] = useState(false)
  const [sentryTesting, setSentryTesting] = useState(false)

  // ── mcp state ────────────────────────────────────────────────
  const [mcpLoading, setMcpLoading] = useState(true)
  const [mcpCfg, setMcpCfg] = useState<MCPConfig | null>(null)
  const [mcpAllowWrite, setMcpAllowWrite] = useState(false)
  const [mcpEnabled, setMcpEnabled] = useState(true)
  const [mcpSaving, setMcpSaving] = useState(false)
  const [mcpRotating, setMcpRotating] = useState(false)
  const [mcpNewToken, setMcpNewToken] = useState('') // 仅生成后展示一次
  const [snippetStyle, setSnippetStyle] = useState<MCPSnippetStyle>('claude')
  const [snippetText, setSnippetText] = useState('')
  const [snippetLoading, setSnippetLoading] = useState(false)

  const refreshMCP = useCallback(async () => {
    setMcpLoading(true)
    try {
      const cfg = await getMCPConfig()
      setMcpCfg(cfg)
      setMcpAllowWrite(cfg.allow_write)
      setMcpEnabled(cfg.enabled)
    } catch (err) {
      toast({
        title: t('globalSettings.loadFailed'),
        description: String(err),
        status: 'error',
        duration: 5000,
      })
    } finally {
      setMcpLoading(false)
    }
  }, [t, toast])

  const refreshObservability = useCallback(async () => {
    setObservabilityLoading(true)
    try {
      const cfg = await getObservabilityConfig()
      setObservabilityCfg(cfg)
      setPosthogHostInput(cfg.posthog_host || cfg.posthog_default_host)
      setPosthogEnabled(cfg.posthog_enabled)
      setSentryEnabled(cfg.sentry_enabled)
      setObservabilityEnvironment(cfg.environment || cfg.default_environment)
    } catch (err) {
      toast({
        title: t('globalSettings.loadFailed'),
        description: String(err),
        status: 'error',
        duration: 5000,
      })
    } finally {
      setObservabilityLoading(false)
    }
  }, [t, toast])

  const refreshSnippet = useCallback(
    async (style: MCPSnippetStyle) => {
      setSnippetLoading(true)
      try {
        const data = await getMCPClientSnippet(style)
        const snippet = (data as { snippet?: unknown }).snippet ?? data
        setSnippetText(JSON.stringify(snippet, null, 2))
      } catch (err) {
        setSnippetText('')
        toast({
          title: t('globalSettings.mcp.snippetFailed'),
          description: String(err),
          status: 'error',
          duration: 4000,
        })
      } finally {
        setSnippetLoading(false)
      }
    },
    [t, toast],
  )

  useEffect(() => {
    refreshObservability()
    refreshMCP()
  }, [refreshObservability, refreshMCP])

  useEffect(() => {
    refreshSnippet(snippetStyle)
  }, [snippetStyle, refreshSnippet, mcpCfg?.has_token])

  // ── observability actions ───────────────────────────────────
  const handleObservabilitySave = async () => {
    setObservabilitySaving(true)
    try {
      const payload: {
        posthog_project_key?: string
        posthog_host?: string
        posthog_enabled?: boolean
        sentry_dsn?: string
        sentry_enabled?: boolean
        environment?: string
      } = {
        posthog_host: posthogHostInput,
        posthog_enabled: posthogEnabled,
        sentry_enabled: sentryEnabled,
        environment: observabilityEnvironment,
      }
      if (posthogKeyInput.trim() !== '') {
        payload.posthog_project_key = posthogKeyInput.trim()
      }
      if (sentryDsnInput.trim() !== '') {
        payload.sentry_dsn = sentryDsnInput.trim()
      }
      const cfg = await updateObservabilityConfig(payload)
      setObservabilityCfg(cfg)
      setPosthogKeyInput('')
      setSentryDsnInput('')
      toast({ title: t('globalSettings.saved'), status: 'success', duration: 3000 })
    } catch (err) {
      toast({
        title: t('globalSettings.saveFailed'),
        description: String(err),
        status: 'error',
        duration: 5000,
      })
    } finally {
      setObservabilitySaving(false)
    }
  }

  const handleObservabilityClearPostHogKey = async () => {
    setObservabilitySaving(true)
    try {
      const cfg = await updateObservabilityConfig({
        posthog_project_key: '__clear__',
        posthog_enabled: false,
      })
      setObservabilityCfg(cfg)
      setPosthogKeyInput('')
      setPosthogEnabled(false)
      toast({ title: t('globalSettings.observability.posthog.posthogCleared'), status: 'success', duration: 3000 })
    } catch (err) {
      toast({
        title: t('globalSettings.saveFailed'),
        description: String(err),
        status: 'error',
        duration: 5000,
      })
    } finally {
      setObservabilitySaving(false)
    }
  }

  const handleObservabilityClearSentryDsn = async () => {
    setObservabilitySaving(true)
    try {
      const cfg = await updateObservabilityConfig({
        sentry_dsn: '__clear__',
        sentry_enabled: false,
      })
      setObservabilityCfg(cfg)
      setSentryDsnInput('')
      setSentryEnabled(false)
      toast({ title: t('globalSettings.observability.sentry.sentryCleared'), status: 'success', duration: 3000 })
    } catch (err) {
      toast({
        title: t('globalSettings.saveFailed'),
        description: String(err),
        status: 'error',
        duration: 5000,
      })
    } finally {
      setObservabilitySaving(false)
    }
  }

  const handleObservabilityTest = async (provider: 'posthog' | 'sentry') => {
    const setTesting = provider === 'posthog' ? setPosthogTesting : setSentryTesting
    setTesting(true)
    try {
      const r = await testObservabilityConnection({
        provider,
        posthog_project_key: posthogKeyInput.trim() || undefined,
        posthog_host: posthogHostInput.trim() || undefined,
        sentry_dsn: sentryDsnInput.trim() || undefined,
        environment: observabilityEnvironment.trim() || undefined,
      })
      if (r.ok) {
        toast({
          title: t(`globalSettings.observability.${provider}.testOk`),
          description: r.message,
          status: 'success',
          duration: 4000,
        })
      } else {
        toast({
          title: t(`globalSettings.observability.${provider}.testFailed`),
          description: r.error,
          status: 'error',
          duration: 6000,
        })
      }
    } catch (err) {
      toast({
        title: t(`globalSettings.observability.${provider}.testFailed`),
        description: String(err),
        status: 'error',
        duration: 6000,
      })
    } finally {
      setTesting(false)
    }
  }

  // ── mcp actions ──────────────────────────────────────────────
  const handleMcpSave = async () => {
    setMcpSaving(true)
    try {
      const cfg = await updateMCPConfig({ enabled: mcpEnabled, allow_write: mcpAllowWrite })
      setMcpCfg(cfg)
      toast({ title: t('globalSettings.saved'), status: 'success', duration: 3000 })
    } catch (err) {
      toast({
        title: t('globalSettings.saveFailed'),
        description: String(err),
        status: 'error',
        duration: 5000,
      })
    } finally {
      setMcpSaving(false)
    }
  }

  const handleRotateToken = async () => {
    if (!window.confirm(t('globalSettings.mcp.rotateConfirm'))) return
    setMcpRotating(true)
    try {
      const r = await rotateMCPToken()
      setMcpNewToken(r.token)
      await refreshMCP()
      await refreshSnippet(snippetStyle)
      toast({ title: t('globalSettings.mcp.rotated'), status: 'success', duration: 4000 })
    } catch (err) {
      toast({
        title: t('globalSettings.saveFailed'),
        description: String(err),
        status: 'error',
        duration: 5000,
      })
    } finally {
      setMcpRotating(false)
    }
  }

  const handleClearToken = async () => {
    if (!window.confirm(t('globalSettings.mcp.clearConfirm'))) return
    try {
      await clearMCPToken()
      setMcpNewToken('')
      await refreshMCP()
      await refreshSnippet(snippetStyle)
      toast({ title: t('globalSettings.mcp.cleared'), status: 'success', duration: 3000 })
    } catch (err) {
      toast({
        title: t('globalSettings.saveFailed'),
        description: String(err),
        status: 'error',
        duration: 5000,
      })
    }
  }

  const copyToClipboard = async (s: string, label: string) => {
    try {
      await navigator.clipboard.writeText(s)
      toast({ title: t('globalSettings.copied', { what: label }), status: 'success', duration: 2000 })
    } catch {
      toast({ title: t('globalSettings.copyFailed'), status: 'error', duration: 2000 })
    }
  }

  return (
    <Container maxW="container.lg" py={6}>
      <Heading size="lg" mb={2}>{t('globalSettings.title')}</Heading>
      <Text color="gray.500" mb={6}>{t('globalSettings.subtitle')}</Text>

      {/* —— PostHog / Sentry —————————————————————————————— */}
      <Box bg={cardBg} borderRadius="md" boxShadow="sm" p={6} mb={6}>
        <HStack justify="space-between" mb={3}>
          <Heading size="md">{t('globalSettings.observability.title')}</Heading>
          <HStack>
            {observabilityCfg?.posthog_enabled && observabilityCfg?.posthog_has_project_key && (
              <Badge colorScheme="green">{t('globalSettings.observability.posthogActive')}</Badge>
            )}
            {observabilityCfg?.sentry_enabled && observabilityCfg?.sentry_has_dsn && (
              <Badge colorScheme="purple">{t('globalSettings.observability.sentryActive')}</Badge>
            )}
          </HStack>
        </HStack>
        <Text fontSize="sm" color="gray.500" mb={4}>
          {t('globalSettings.observability.intro')}
        </Text>

        {observabilityLoading ? <Spinner /> : (
          <VStack align="stretch" spacing={4}>
            <FormControl>
              <FormLabel>{t('globalSettings.observability.environmentLabel')}</FormLabel>
              <Input
                value={observabilityEnvironment}
                onChange={(e) => setObservabilityEnvironment(e.target.value)}
                placeholder={observabilityCfg?.default_environment || t('globalSettings.observability.environmentPlaceholder')}
              />
              <FormHelperText>{t('globalSettings.observability.environmentHelp')}</FormHelperText>
            </FormControl>

            <Divider />

            <Heading size="sm">{t('globalSettings.observability.posthog.title')}</Heading>
            <FormControl>
              <FormLabel>{t('globalSettings.observability.posthog.projectKeyLabel')}</FormLabel>
              <Input
                type="password"
                placeholder={observabilityCfg?.posthog_has_project_key
                  ? t('globalSettings.observability.posthog.projectKeyPlaceholderHasValue', {
                    mask: observabilityCfg.posthog_project_key_mask,
                  })
                  : t('globalSettings.observability.posthog.projectKeyPlaceholder')}
                value={posthogKeyInput}
                onChange={(e) => setPosthogKeyInput(e.target.value)}
              />
              <FormHelperText>{t('globalSettings.observability.posthog.projectKeyHelp')}</FormHelperText>
            </FormControl>
            <FormControl>
              <FormLabel>{t('globalSettings.observability.posthog.hostLabel')}</FormLabel>
              <Input
                value={posthogHostInput}
                onChange={(e) => setPosthogHostInput(e.target.value)}
                placeholder={observabilityCfg?.posthog_default_host || t('globalSettings.observability.posthog.hostPlaceholder')}
              />
              <FormHelperText>{t('globalSettings.observability.posthog.hostHelp')}</FormHelperText>
            </FormControl>
            <FormControl display="flex" alignItems="center">
              <FormLabel mb="0">{t('globalSettings.observability.posthog.enabledLabel')}</FormLabel>
              <Switch
                isChecked={posthogEnabled}
                onChange={(e) => setPosthogEnabled(e.target.checked)}
              />
            </FormControl>
            <HStack>
              <Button
                onClick={() => handleObservabilityTest('posthog')}
                isLoading={posthogTesting}
                variant="outline"
              >
                {t('globalSettings.observability.posthog.testButton')}
              </Button>
              {observabilityCfg?.posthog_has_project_key && (
                <Button onClick={handleObservabilityClearPostHogKey} variant="ghost" colorScheme="red">
                  {t('globalSettings.observability.posthog.clearButton')}
                </Button>
              )}
            </HStack>

            <Divider />

            <Heading size="sm">{t('globalSettings.observability.sentry.title')}</Heading>
            <FormControl>
              <FormLabel>{t('globalSettings.observability.sentry.dsnLabel')}</FormLabel>
              <Input
                type="password"
                placeholder={observabilityCfg?.sentry_has_dsn
                  ? t('globalSettings.observability.sentry.dsnPlaceholderHasValue', {
                    mask: observabilityCfg.sentry_dsn_mask,
                  })
                  : t('globalSettings.observability.sentry.dsnPlaceholder')}
                value={sentryDsnInput}
                onChange={(e) => setSentryDsnInput(e.target.value)}
              />
              <FormHelperText>{t('globalSettings.observability.sentry.dsnHelp')}</FormHelperText>
            </FormControl>
            <FormControl display="flex" alignItems="center">
              <FormLabel mb="0">{t('globalSettings.observability.sentry.enabledLabel')}</FormLabel>
              <Switch
                isChecked={sentryEnabled}
                onChange={(e) => setSentryEnabled(e.target.checked)}
              />
            </FormControl>
            <HStack>
              <Button
                onClick={() => handleObservabilityTest('sentry')}
                isLoading={sentryTesting}
                variant="outline"
              >
                {t('globalSettings.observability.sentry.testButton')}
              </Button>
              {observabilityCfg?.sentry_has_dsn && (
                <Button onClick={handleObservabilityClearSentryDsn} variant="ghost" colorScheme="red">
                  {t('globalSettings.observability.sentry.clearButton')}
                </Button>
              )}
            </HStack>

            <HStack>
              <Button colorScheme="blue" onClick={handleObservabilitySave} isLoading={observabilitySaving}>
                {t('globalSettings.save')}
              </Button>
            </HStack>
          </VStack>
        )}
      </Box>

      {/* —— MCP —————————————————————————————————————————— */}
      <Box bg={cardBg} borderRadius="md" boxShadow="sm" p={6} mb={6}>
        <HStack justify="space-between" mb={3}>
          <Heading size="md">{t('globalSettings.mcp.title')}</Heading>
          <HStack>
            {mcpCfg?.has_token ? (
              <Badge colorScheme="green">{t('globalSettings.mcp.tokenIssued')}</Badge>
            ) : (
              <Badge colorScheme="orange">{t('globalSettings.mcp.tokenMissing')}</Badge>
            )}
            {mcpCfg && <Badge>{t('globalSettings.mcp.toolCount', { n: mcpCfg.tool_count })}</Badge>}
          </HStack>
        </HStack>
        <Text fontSize="sm" color="gray.500" mb={4}>
          {t('globalSettings.mcp.intro')}
        </Text>

        {mcpLoading ? <Spinner /> : (
          <VStack align="stretch" spacing={4}>
            <FormControl display="flex" alignItems="center">
              <FormLabel mb="0">{t('globalSettings.mcp.enabledLabel')}</FormLabel>
              <Switch
                isChecked={mcpEnabled}
                onChange={(e) => setMcpEnabled(e.target.checked)}
              />
            </FormControl>

            <FormControl display="flex" alignItems="center">
              <FormLabel mb="0">{t('globalSettings.mcp.allowWriteLabel')}</FormLabel>
              <Switch
                isChecked={mcpAllowWrite}
                onChange={(e) => setMcpAllowWrite(e.target.checked)}
                colorScheme="red"
              />
            </FormControl>
            <Alert status={mcpAllowWrite ? 'warning' : 'info'} fontSize="sm" borderRadius="md">
              <AlertIcon />
              {mcpAllowWrite
                ? t('globalSettings.mcp.allowWriteWarn')
                : t('globalSettings.mcp.allowWriteInfo')}
            </Alert>

            <HStack>
              <Button colorScheme="blue" onClick={handleMcpSave} isLoading={mcpSaving}>
                {t('globalSettings.save')}
              </Button>
            </HStack>

            <Divider />

            <FormControl>
              <FormLabel>{t('globalSettings.mcp.tokenLabel')}</FormLabel>
              <HStack>
                <Input
                  isReadOnly
                  value={
                    mcpNewToken
                      ? mcpNewToken
                      : mcpCfg?.has_token
                        ? (mcpCfg.token_mask || '')
                        : t('globalSettings.mcp.tokenEmpty')
                  }
                  fontFamily="mono"
                />
                {mcpNewToken && (
                  <IconButton
                    aria-label={t('globalSettings.copy')}
                    icon={<CopyIcon />}
                    onClick={() => copyToClipboard(mcpNewToken, 'token')}
                  />
                )}
                <Button
                  leftIcon={<RepeatIcon />}
                  onClick={handleRotateToken}
                  isLoading={mcpRotating}
                  colorScheme={mcpCfg?.has_token ? 'gray' : 'blue'}
                >
                  {mcpCfg?.has_token
                    ? t('globalSettings.mcp.rotateButton')
                    : t('globalSettings.mcp.generateButton')}
                </Button>
                {mcpCfg?.has_token && (
                  <Button variant="ghost" colorScheme="red" onClick={handleClearToken}>
                    {t('globalSettings.mcp.clearButton')}
                  </Button>
                )}
              </HStack>
              {mcpNewToken && (
                <Alert status="warning" mt={2} fontSize="sm" borderRadius="md">
                  <AlertIcon />
                  {t('globalSettings.mcp.tokenShownOnce')}
                </Alert>
              )}
            </FormControl>

            <Divider />

            <FormControl>
              <Stack direction={{ base: 'column', md: 'row' }} justify="space-between" align={{ md: 'center' }} mb={2}>
                <FormLabel mb={0}>{t('globalSettings.mcp.snippetLabel')}</FormLabel>
                <Select
                  size="sm"
                  width={{ base: '100%', md: '180px' }}
                  value={snippetStyle}
                  onChange={(e) => setSnippetStyle(e.target.value as MCPSnippetStyle)}
                >
                  {SNIPPET_STYLES.map((s) => (
                    <option key={s} value={s}>
                      {t(`globalSettings.mcp.snippetStyle.${s}`)}
                    </option>
                  ))}
                </Select>
              </Stack>
              <Box position="relative">
                <Textarea
                  bg={codeBg}
                  fontFamily="mono"
                  fontSize="sm"
                  rows={10}
                  isReadOnly
                  value={snippetLoading ? t('globalSettings.loading') : snippetText}
                />
                <IconButton
                  aria-label={t('globalSettings.copy')}
                  icon={<CopyIcon />}
                  size="sm"
                  position="absolute"
                  top={2}
                  right={2}
                  onClick={() => copyToClipboard(snippetText, t('globalSettings.mcp.snippetLabel'))}
                  isDisabled={!snippetText}
                />
              </Box>
              <FormHelperText>{t('globalSettings.mcp.snippetHelp')}</FormHelperText>
            </FormControl>

            <Box>
              <Text fontSize="sm" color="gray.500">
                {t('globalSettings.mcp.endpointHint')}{' '}
                <Code>{mcpCfg?.mount_path ?? '/mcp'}</Code>
              </Text>
            </Box>
          </VStack>
        )}
      </Box>
    </Container>
  )
}

export default GlobalSettings
