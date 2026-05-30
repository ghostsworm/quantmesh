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
  AipipeConfig,
  MCPConfig,
  MCPSnippetStyle,
  clearMCPToken,
  getAipipeConfig,
  getMCPClientSnippet,
  getMCPConfig,
  rotateMCPToken,
  testAipipeConnection,
  updateAipipeConfig,
  updateMCPConfig,
} from '../services/globalSettings'

const SNIPPET_STYLES: MCPSnippetStyle[] = ['claude', 'cursor', 'generic']

const GlobalSettings: React.FC = () => {
  const { t } = useTranslation()
  const toast = useToast()
  const cardBg = useColorModeValue('white', 'gray.800')
  const codeBg = useColorModeValue('gray.50', 'gray.900')

  // ── aipipe state ─────────────────────────────────────────────
  const [aipipeLoading, setAipipeLoading] = useState(true)
  const [aipipeCfg, setAipipeCfg] = useState<AipipeConfig | null>(null)
  const [aipipeKeyInput, setAipipeKeyInput] = useState('')
  const [aipipeEndpointInput, setAipipeEndpointInput] = useState('')
  const [aipipeEnabled, setAipipeEnabled] = useState(false)
  const [aipipeSaving, setAipipeSaving] = useState(false)
  const [aipipeTesting, setAipipeTesting] = useState(false)

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

  const refreshAipipe = useCallback(async () => {
    setAipipeLoading(true)
    try {
      const cfg = await getAipipeConfig()
      setAipipeCfg(cfg)
      setAipipeEndpointInput(cfg.endpoint || cfg.default_endpoint)
      setAipipeEnabled(cfg.enabled)
    } catch (err) {
      toast({
        title: t('globalSettings.loadFailed'),
        description: String(err),
        status: 'error',
        duration: 5000,
      })
    } finally {
      setAipipeLoading(false)
    }
  }, [t, toast])

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
    refreshAipipe()
    refreshMCP()
  }, [refreshAipipe, refreshMCP])

  useEffect(() => {
    refreshSnippet(snippetStyle)
  }, [snippetStyle, refreshSnippet, mcpCfg?.has_token])

  // ── aipipe actions ───────────────────────────────────────────
  const handleAipipeSave = async () => {
    setAipipeSaving(true)
    try {
      const payload: { api_key?: string; endpoint?: string; enabled?: boolean } = {
        endpoint: aipipeEndpointInput,
        enabled: aipipeEnabled,
      }
      if (aipipeKeyInput.trim() !== '') {
        payload.api_key = aipipeKeyInput.trim()
      }
      const cfg = await updateAipipeConfig(payload)
      setAipipeCfg(cfg)
      setAipipeKeyInput('')
      toast({ title: t('globalSettings.saved'), status: 'success', duration: 3000 })
    } catch (err) {
      toast({
        title: t('globalSettings.saveFailed'),
        description: String(err),
        status: 'error',
        duration: 5000,
      })
    } finally {
      setAipipeSaving(false)
    }
  }

  const handleAipipeClearKey = async () => {
    setAipipeSaving(true)
    try {
      const cfg = await updateAipipeConfig({ api_key: '__clear__', enabled: false })
      setAipipeCfg(cfg)
      setAipipeKeyInput('')
      setAipipeEnabled(false)
      toast({ title: t('globalSettings.aipipe.cleared'), status: 'success', duration: 3000 })
    } catch (err) {
      toast({
        title: t('globalSettings.saveFailed'),
        description: String(err),
        status: 'error',
        duration: 5000,
      })
    } finally {
      setAipipeSaving(false)
    }
  }

  const handleAipipeTest = async () => {
    setAipipeTesting(true)
    try {
      const r = await testAipipeConnection({
        api_key: aipipeKeyInput.trim() || undefined,
        endpoint: aipipeEndpointInput.trim() || undefined,
      })
      if (r.ok) {
        toast({
          title: t('globalSettings.aipipe.testOk'),
          description: r.message,
          status: 'success',
          duration: 4000,
        })
      } else {
        toast({
          title: t('globalSettings.aipipe.testFailed'),
          description: r.error,
          status: 'error',
          duration: 6000,
        })
      }
    } catch (err) {
      toast({
        title: t('globalSettings.aipipe.testFailed'),
        description: String(err),
        status: 'error',
        duration: 6000,
      })
    } finally {
      setAipipeTesting(false)
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

      {/* —— aipipe —————————————————————————————————————— */}
      <Box bg={cardBg} borderRadius="md" boxShadow="sm" p={6} mb={6}>
        <HStack justify="space-between" mb={3}>
          <Heading size="md">{t('globalSettings.aipipe.title')}</Heading>
          {aipipeCfg?.enabled && aipipeCfg?.has_api_key && (
            <Badge colorScheme="green">{t('globalSettings.aipipe.active')}</Badge>
          )}
        </HStack>
        <Text fontSize="sm" color="gray.500" mb={4}>
          {t('globalSettings.aipipe.intro')}
          {' '}
          <Link href="https://17push.com" isExternal color="blue.500">
            17push.com
          </Link>
        </Text>

        {aipipeLoading ? <Spinner /> : (
          <VStack align="stretch" spacing={4}>
            <FormControl>
              <FormLabel>{t('globalSettings.aipipe.apiKeyLabel')}</FormLabel>
              <Input
                type="password"
                placeholder={aipipeCfg?.has_api_key
                  ? t('globalSettings.aipipe.apiKeyPlaceholderHasValue', { mask: aipipeCfg.api_key_mask })
                  : t('globalSettings.aipipe.apiKeyPlaceholder')}
                value={aipipeKeyInput}
                onChange={(e) => setAipipeKeyInput(e.target.value)}
              />
              <FormHelperText>{t('globalSettings.aipipe.apiKeyHelp')}</FormHelperText>
            </FormControl>

            <FormControl>
              <FormLabel>{t('globalSettings.aipipe.endpointLabel')}</FormLabel>
              <Input
                value={aipipeEndpointInput}
                onChange={(e) => setAipipeEndpointInput(e.target.value)}
                placeholder={aipipeCfg?.default_endpoint || 'https://17push.com/api/v1'}
              />
              <FormHelperText>{t('globalSettings.aipipe.endpointHelp')}</FormHelperText>
            </FormControl>

            <FormControl display="flex" alignItems="center">
              <FormLabel mb="0">{t('globalSettings.aipipe.enabledLabel')}</FormLabel>
              <Switch
                isChecked={aipipeEnabled}
                onChange={(e) => setAipipeEnabled(e.target.checked)}
              />
            </FormControl>

            <HStack>
              <Button colorScheme="blue" onClick={handleAipipeSave} isLoading={aipipeSaving}>
                {t('globalSettings.save')}
              </Button>
              <Button onClick={handleAipipeTest} isLoading={aipipeTesting} variant="outline">
                {t('globalSettings.aipipe.testButton')}
              </Button>
              {aipipeCfg?.has_api_key && (
                <Button onClick={handleAipipeClearKey} variant="ghost" colorScheme="red">
                  {t('globalSettings.aipipe.clearButton')}
                </Button>
              )}
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

            <Box>
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
            </Box>

            <Divider />

            <Box>
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
            </Box>

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
