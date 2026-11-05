import { describe, expect, it } from 'vitest'
import type { MCPIssuedToken, MCPSnippetStyle } from '../services/globalSettings'
import { buildMCPClientSnippet } from './mcpClientSnippet'

const issuedToken = 'fixture+reader&value'
const issued: MCPIssuedToken = {
  ok: true,
  id: 'test-reader',
  name: 'Test reader',
  mode: 'readonly',
  token: issuedToken,
  endpoint_url: `https://example.test/quantmesh/mcp?${new URLSearchParams({ token: issuedToken })}`,
}
const styles: MCPSnippetStyle[] = ['claude', 'cursor', 'generic']

function expectedConfig(token: MCPIssuedToken, style: MCPSnippetStyle) {
  const connection = {
    url: token.endpoint_url,
    headers: { Authorization: `Bearer ${token.token}` },
  }
  return style === 'generic' ? connection : { mcpServers: { quantmesh: connection } }
}

describe('MCP client configuration identity', () => {
  it.each(styles)('%s uses the exact issued endpoint and Bearer identity', (style) => {
    const snippet = buildMCPClientSnippet(issued, style)
    expect(JSON.parse(snippet)).toEqual(expectedConfig(issued, style))
    // Preserve the public origin, proxy prefix and encoded query from issuance.
    expect(snippet).toContain(issued.endpoint_url)
    expect(new URL(issued.endpoint_url).searchParams.get('token')).toBe(issued.token)
  })

  it.each(styles)('%s stays empty before issuance and after revocation or reload', (style) => {
    expect(buildMCPClientSnippet(null, style)).toBe('')
  })

  it('replaces the entire identity when another token is issued', () => {
    const next: MCPIssuedToken = {
      ...issued,
      id: 'test-writer',
      name: 'Test writer',
      mode: 'readwrite',
      token: 'test-only-writer-token',
      endpoint_url: 'https://example.test/quantmesh/mcp?token=test-only-writer-token',
      notify: { channel: 'email', target: 'test@example.test' },
    }
    const snippet = buildMCPClientSnippet(next, 'generic')
    expect(JSON.parse(snippet)).toEqual(expectedConfig(next, 'generic'))
    expect(snippet).not.toContain(issued.token)
    expect(snippet).not.toContain(issued.endpoint_url)
    expect(snippet).not.toContain('test@example.test')
  })
})
