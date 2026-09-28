package position

import "strconv"

// Grid lots are keyed by the inventory slot, never by the working order price
// (which can change on maker retries, skew, profit spread or liquidation).
// Account/Bot/symbol scope belongs to the containing execution ledger.
func gridExposureKey(slotPrice float64, leg string) string {
	return "grid:" + leg + ":" + strconv.FormatFloat(slotPrice, 'f', -1, 64)
}

func gridExposureLeg(side string, opening bool) string {
	if (opening && side == "SELL") || (!opening && side == "BUY") {
		return PositionLegShort
	}
	return PositionLegLong
}
