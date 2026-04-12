package kraken

import (
	"fmt"
	"strings"
)

// mapKrakenTransferToCalls 判斷劃轉方向：spotToFutures=true 走現貨 WalletTransfer；false 走期貨 withdrawal
func mapKrakenTransferToCalls(fromAccount, toAccount string) (spotToFutures bool, err error) {
	f := strings.ToUpper(strings.TrimSpace(fromAccount))
	t := strings.ToUpper(strings.TrimSpace(toAccount))
	switch {
	case (f == "SPOT" || f == "MAIN") && (t == "UMFUTURE" || t == "CONTRACT" || t == "FUTURES"):
		return true, nil
	case (f == "UMFUTURE" || f == "CONTRACT" || f == "FUTURES") && (t == "SPOT" || t == "MAIN"):
		return false, nil
	default:
		return false, fmt.Errorf("Kraken 不支援的劃轉: %s -> %s（僅支援現貨錢包與期貨錢包互轉）", fromAccount, toAccount)
	}
}
