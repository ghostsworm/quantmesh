// 全局设置页面的轻量 API 封装：aipipe 错误上报 + MCP 服务。
import { fetchWithAuth } from './api'

const API_BASE_URL = `${window.location.origin}/api`

// ——— aipipe —————————————————————————————————————————————————

export interface AipipeConfig {
  enabled: boolean
  has_api_key: boolean
  api_key_mask: string
  endpoint: string
  default_endpoint: string
}

export interface AipipeUpdateRequest {
  api_key?: string
  endpoint?: string
  enabled?: boolean
}

export async function getAipipeConfig(): Promise<AipipeConfig> {
  return fetchWithAuth(`${API_BASE_URL}/aipipe/config`)
}

export async function updateAipipeConfig(req: AipipeUpdateRequest): Promise<AipipeConfig> {
  return fetchWithAuth(`${API_BASE_URL}/aipipe/config`, {
    method: 'PUT',
    body: JSON.stringify(req),
  })
}

export async function testAipipeConnection(
  payload: { api_key?: string; endpoint?: string }
): Promise<{ ok: boolean; message?: string; error?: string }> {
  return fetchWithAuth(`${API_BASE_URL}/aipipe/test`, {
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
