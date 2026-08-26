// 全局设置页面的轻量 API 封装：可观测性上报 + MCP 服务。
import { fetchWithAuth } from './api'

const API_BASE_URL = `${window.location.origin}/api`

// ——— PostHog / Sentry ————————————————————————————————————————

export interface ObservabilityConfig {
  posthog_enabled: boolean
  posthog_has_project_key: boolean
  posthog_project_key_mask: string
  posthog_host: string
  posthog_default_host: string
  sentry_enabled: boolean
  sentry_has_dsn: boolean
  sentry_dsn_mask: string
  environment: string
  default_environment: string
}

export interface ObservabilityUpdateRequest {
  posthog_project_key?: string
  posthog_host?: string
  posthog_enabled?: boolean
  sentry_dsn?: string
  sentry_enabled?: boolean
  environment?: string
}

export type ObservabilityProvider = 'posthog' | 'sentry'

export async function getObservabilityConfig(): Promise<ObservabilityConfig> {
  return fetchWithAuth(`${API_BASE_URL}/observability/config`)
}

export async function updateObservabilityConfig(
  req: ObservabilityUpdateRequest,
): Promise<ObservabilityConfig> {
  return fetchWithAuth(`${API_BASE_URL}/observability/config`, {
    method: 'PUT',
    body: JSON.stringify(req),
  })
}

export async function testObservabilityConnection(
  payload: {
    provider: ObservabilityProvider
    posthog_project_key?: string
    posthog_host?: string
    sentry_dsn?: string
    environment?: string
  },
): Promise<{ ok: boolean; message?: string; error?: string }> {
  return fetchWithAuth(`${API_BASE_URL}/observability/test`, {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

// ——— MCP —————————————————————————————————————————————————————

export interface MCPConfig {
  enabled: boolean
  has_token: boolean
  token_mask: string
  allow_write: boolean
  tool_count: number
  mount_path: string
}

export interface MCPUpdateRequest {
  enabled?: boolean
  allow_write?: boolean
}

export async function getMCPConfig(): Promise<MCPConfig> {
  return fetchWithAuth(`${API_BASE_URL}/mcp/config`)
}

export async function updateMCPConfig(req: MCPUpdateRequest): Promise<MCPConfig> {
  return fetchWithAuth(`${API_BASE_URL}/mcp/config`, {
    method: 'PUT',
    body: JSON.stringify(req),
  })
}

export async function rotateMCPToken(): Promise<{ ok: boolean; token: string }> {
  return fetchWithAuth(`${API_BASE_URL}/mcp/token/rotate`, { method: 'POST' })
}

export async function clearMCPToken(): Promise<{ ok: boolean }> {
  return fetchWithAuth(`${API_BASE_URL}/mcp/token`, { method: 'DELETE' })
}

export type MCPSnippetStyle = 'claude' | 'cursor' | 'generic'

export async function getMCPClientSnippet(
  style: MCPSnippetStyle = 'claude',
  host?: string,
): Promise<Record<string, unknown>> {
  const params = new URLSearchParams({ style })
  if (host) params.set('host', host)
  return fetchWithAuth(`${API_BASE_URL}/mcp/client-snippet?${params.toString()}`)
}
