import type { MCPIssuedToken, MCPSnippetStyle } from '../services/globalSettings'

// Use the one-time issuance response for both authentication forms. Persisted
// token metadata cannot reconstruct the secret and must not select a legacy key.
export function buildMCPClientSnippet(
  issued: MCPIssuedToken | null,
  style: MCPSnippetStyle,
): string {
  if (!issued) return ''

  const connection = {
    url: issued.endpoint_url,
    headers: { Authorization: `Bearer ${issued.token}` },
  }
  const snippet = style === 'generic'
    ? connection
    : { mcpServers: { quantmesh: connection } }

  return JSON.stringify(snippet, null, 2)
}
