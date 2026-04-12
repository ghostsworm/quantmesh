package huobi

import (
	"fmt"
	"strings"
)

// mapHuobiTransferDirection 將統一帳戶標籤映射為現貨↔U 本位永續劃轉方向（true = 現貨→永續）
func mapHuobiTransferDirection(fromAccount, toAccount string) (fromSpotToFutures bool, err error) {
	f := strings.ToUpper(strings.TrimSpace(fromAccount))
	t := strings.ToUpper(strings.TrimSpace(toAccount))
	switch {
	case (f == "SPOT" || f == "MAIN") && (t == "UMFUTURE" || t == "CONTRACT" || t == "FUTURES"):
		return true, nil
	case (f == "UMFUTURE" || f == "CONTRACT" || f == "FUTURES") && (t == "SPOT" || t == "MAIN"):
		return false, nil
	default:
		return false, fmt.Errorf("Huobi 不支援的劃轉: %s -> %s（僅支援現貨與 U 本位永續互轉）", fromAccount, toAccount)
	}
}
