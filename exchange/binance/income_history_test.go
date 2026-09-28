package binance

import (
	"testing"

	"github.com/adshao/go-binance/v2/futures"
)

func TestNormalizeIncomeRecordValidatesCashflowEvidence(t *testing.T) {
	const start, end = int64(1000), int64(2000)
	base := &futures.IncomeHistory{Asset: "USDT", Income: "-0.25", IncomeType: "FUNDING_FEE", Symbol: "BTCUSDT", Time: 1500, TranID: 42}
	got, err := normalizeIncomeRecord(base, "BTCUSDT", "FUNDING_FEE", start, end)
	if err != nil || got == nil || got.Income != -0.25 || got.TransactionID != 42 {
		t.Fatalf("valid income evidence rejected: record=%+v err=%v", got, err)
	}

	cases := []struct {
		name   string
		mutate func(*futures.IncomeHistory)
	}{
		{name: "invalid amount", mutate: func(row *futures.IncomeHistory) { row.Income = "not-a-number" }},
		{name: "nan", mutate: func(row *futures.IncomeHistory) { row.Income = "NaN" }},
		{name: "infinity", mutate: func(row *futures.IncomeHistory) { row.Income = "Inf" }},
		{name: "missing transaction ID", mutate: func(row *futures.IncomeHistory) { row.TranID = 0 }},
		{name: "missing asset", mutate: func(row *futures.IncomeHistory) { row.Asset = " " }},
		{name: "wrong symbol", mutate: func(row *futures.IncomeHistory) { row.Symbol = "ETHUSDT" }},
		{name: "wrong type", mutate: func(row *futures.IncomeHistory) { row.IncomeType = "COMMISSION" }},
		{name: "before interval", mutate: func(row *futures.IncomeHistory) { row.Time = start - 1 }},
		{name: "after interval", mutate: func(row *futures.IncomeHistory) { row.Time = end + 1 }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			row := *base
			test.mutate(&row)
			if got, err := normalizeIncomeRecord(&row, "BTCUSDT", "FUNDING_FEE", start, end); err == nil || got != nil {
				t.Fatalf("invalid income evidence accepted: record=%+v err=%v", got, err)
			}
		})
	}
	if _, err := normalizeIncomeRecord(nil, "BTCUSDT", "FUNDING_FEE", start, end); err == nil {
		t.Fatal("nil income evidence must be rejected")
	}
}
