export type PnLStatusEvidence = {
  total_pnl: number
  total_pnl_asset?: string
  total_pnl_verified?: boolean
}

export type VerifiedPnL = {
  amount: number
  asset: string
}

export function getVerifiedPnL(status: PnLStatusEvidence): VerifiedPnL | null {
  const asset = status.total_pnl_asset?.trim().toUpperCase()
  if (status.total_pnl_verified !== true || !asset || !Number.isFinite(status.total_pnl)) {
    return null
  }
  return { amount: status.total_pnl, asset }
}
