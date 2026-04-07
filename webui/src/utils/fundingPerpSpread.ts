import type { BotDetailInfo, FundingPerpSpreadConfigApi } from '../services/api'

export function parseFundingPerpSpread(bot: BotDetailInfo | null): FundingPerpSpreadConfigApi | null {
  if (!bot || bot.market_type !== 'funding_perp_spread') return null
  const fp = bot.config?.funding_perp_spread
  if (!fp?.leg_a?.exchange || !fp?.leg_a?.symbol || !fp?.leg_b?.exchange || !fp?.leg_b?.symbol) {
    return null
  }
  return fp
}
