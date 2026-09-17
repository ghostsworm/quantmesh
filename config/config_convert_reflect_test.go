package config

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"testing"

	"gopkg.in/yaml.v3"
)

// 反射式轉換完整性測試：防止 SymbolConfig ↔ BotConfig ↔ BotConfigFile 轉換時靜默丟字段。
// 新增字段若未在轉換函數中複製，這裡會直接失敗；確屬有意不複製時，須加入對應 allowlist 並寫明原因。

const fillMaxDepth = 6

// fillNonZero 將 v（可設置的值）遞歸填充為非零值；seed 用於區分不同字段的字符串內容。
func fillNonZero(v reflect.Value, seed string, depth int) {
	if depth > fillMaxDepth || !v.CanSet() {
		return
	}
	switch v.Kind() {
	case reflect.String:
		v.SetString("s_" + seed)
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(int64(len(seed)%97 + 1))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(uint64(len(seed)%97 + 1))
	case reflect.Float32, reflect.Float64:
		v.SetFloat(float64(len(seed)%97) + 0.5)
	case reflect.Ptr:
		p := reflect.New(v.Type().Elem())
		fillNonZero(p.Elem(), seed, depth+1)
		v.Set(p)
	case reflect.Slice:
		s := reflect.MakeSlice(v.Type(), 1, 1)
		fillNonZero(s.Index(0), seed+"[0]", depth+1)
		v.Set(s)
	case reflect.Map:
		m := reflect.MakeMap(v.Type())
		k := reflect.New(v.Type().Key()).Elem()
		fillNonZero(k, seed+"_key", depth+1)
		val := reflect.New(v.Type().Elem()).Elem()
		fillNonZero(val, seed+"_val", depth+1)
		m.SetMapIndex(k, val)
		v.Set(m)
	case reflect.Interface:
		if v.NumMethod() == 0 {
			v.Set(reflect.ValueOf("i_" + seed))
		}
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			if t.Field(i).IsExported() {
				fillNonZero(v.Field(i), seed+"."+t.Field(i).Name, depth+1)
			}
		}
	}
}

// diffFields 逐字段（結構體遞歸到葉子，指針/切片/映射整體比較）返回 a、b 不相等的路徑。
func diffFields(a, b reflect.Value, prefix string, out *[]string) {
	t := a.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		path := f.Name
		if prefix != "" {
			path = prefix + "." + f.Name
		}
		av, bv := a.Field(i), b.Field(i)
		if f.Type.Kind() == reflect.Struct {
			diffFields(av, bv, path, out)
			continue
		}
		if !reflect.DeepEqual(av.Interface(), bv.Interface()) {
			*out = append(*out, path)
		}
	}
}

// commonFieldDiffs 對兩個不同類型結構體，比較同名且同類型的頂層字段，返回不相等的字段名。
func commonFieldDiffs(src, dst reflect.Value) []string {
	var diffs []string
	st := src.Type()
	for i := 0; i < st.NumField(); i++ {
		sf := st.Field(i)
		if !sf.IsExported() {
			continue
		}
		df, ok := dst.Type().FieldByName(sf.Name)
		if !ok {
			continue
		}
		if df.Type != sf.Type {
			diffs = append(diffs, sf.Name+"(type mismatch)")
			continue
		}
		if !reflect.DeepEqual(src.Field(i).Interface(), dst.FieldByName(sf.Name).Interface()) {
			diffs = append(diffs, sf.Name)
		}
	}
	return diffs
}

func assertNoUnexpectedDiffs(t *testing.T, name string, diffs []string, allow map[string]string) {
	t.Helper()
	var bad []string
	for _, d := range diffs {
		if _, ok := allow[d]; !ok {
			bad = append(bad, d)
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Fatalf("%s 丟失/改變了字段（若為有意，請加入 allowlist 並註明原因）: %v", name, bad)
	}
}

func filledSymbolConfig() SymbolConfig {
	var sc SymbolConfig
	fillNonZero(reflect.ValueOf(&sc).Elem(), "sym", 0)
	// 以下字段在轉換中會被規範化，填入已規範化的合法值，使同名比較仍然嚴格成立
	sc.MarketType = "spot" // + UseSpotMargin=true → 有效類型 spot_margin
	sc.Direction = "SHORT"
	sc.SpotInventoryPolicy = SpotInventoryPolicyAdoptAll
	return sc
}

func filledBotConfig() BotConfig {
	var bc BotConfig
	fillNonZero(reflect.ValueOf(&bc).Elem(), "bot", 0)
	return bc
}

func TestSymbolConfigToBotConfig_CopiesAllCommonFields(t *testing.T) {
	sc := filledSymbolConfig()
	bc := SymbolConfigToBotConfig(sc, true)
	// 同名字段必須全部保留；目前無需 allowlist
	allow := map[string]string{}
	assertNoUnexpectedDiffs(t, "SymbolConfigToBotConfig", commonFieldDiffs(reflect.ValueOf(sc), reflect.ValueOf(bc)), allow)
	if got, want := bc.GetMarketType(), sc.GetMarketType(); got != want || want != "spot_margin" {
		t.Fatalf("有效市場類型不一致: bot=%q symbol=%q", got, want)
	}
}

func TestBotConfigToSymbolConfig_CopiesAllCommonFields(t *testing.T) {
	bc := filledBotConfig()
	sc := BotConfigToSymbolConfig(bc)
	allow := map[string]string{}
	assertNoUnexpectedDiffs(t, "BotConfigToSymbolConfig", commonFieldDiffs(reflect.ValueOf(bc), reflect.ValueOf(sc)), allow)
}

// symbolConfigMissingBotFields 是 BotConfig 有而 SymbolConfig 有意不設的字段。
// CloseOnStopConfig/SlotFilter/AutoRebuild 已補到 SymbolConfig，不再列入。
var symbolConfigMissingBotFields = map[string]string{
	"Testnet":   "由主配置 exchanges[ex].testnet 決定，SymbolConfigToBotConfig 以參數傳入",
	"CreatedAt": "Symbol 配置無創建時間概念",
}

// BotConfig 的每個字段都必須在 SymbolConfig 中存在（同類型、同 tag），否則 BotConfigToSymbolConfig 會靜默丟字段；
// commonFieldDiffs 只比較同名字段，無法發現這類缺失，故單獨檢查。
func TestSymbolConfig_HasAllBotConfigFields(t *testing.T) {
	bt := reflect.TypeOf(BotConfig{})
	st := reflect.TypeOf(SymbolConfig{})
	var missing []string
	for i := 0; i < bt.NumField(); i++ {
		bf := bt.Field(i)
		if _, ok := symbolConfigMissingBotFields[bf.Name]; ok {
			if _, exists := st.FieldByName(bf.Name); exists {
				t.Errorf("%s 已存在於 SymbolConfig，請從 allowlist 移除", bf.Name)
			}
			continue
		}
		sf, ok := st.FieldByName(bf.Name)
		if !ok {
			missing = append(missing, bf.Name)
			continue
		}
		if sf.Type != bf.Type {
			t.Errorf("%s 類型不一致: symbol=%v bot=%v", bf.Name, sf.Type, bf.Type)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("SymbolConfig 缺少 BotConfig 字段（若為有意，加入 allowlist 並註明原因）: %v", missing)
	}
	for _, name := range []string{"CloseOnStopConfig", "SlotFilter", "AutoRebuild"} {
		bf, _ := bt.FieldByName(name)
		sf, _ := st.FieldByName(name)
		if bf.Tag.Get("yaml") != sf.Tag.Get("yaml") || bf.Tag.Get("json") != sf.Tag.Get("json") {
			t.Errorf("%s tag 不一致: symbol=%q bot=%q", name, sf.Tag, bf.Tag)
		}
	}
}

func TestBotSymbolBot_RoundTrip(t *testing.T) {
	bc := filledBotConfig()
	bc.MarketType = "spot"
	bc.Direction = "SHORT"
	bc.SpotInventoryPolicy = SpotInventoryPolicyAdoptAll
	back := SymbolConfigToBotConfig(BotConfigToSymbolConfig(bc), bc.Testnet)
	var diffs []string
	diffFields(reflect.ValueOf(bc), reflect.ValueOf(back), "", &diffs)
	assertNoUnexpectedDiffs(t, "BotConfig→SymbolConfig→BotConfig", diffs, symbolConfigMissingBotFields)
	if back.AutoRebuild != bc.AutoRebuild || !reflect.DeepEqual(back.SlotFilter, bc.SlotFilter) ||
		back.CloseOnStopConfig != bc.CloseOnStopConfig {
		t.Fatalf("CloseOnStopConfig/SlotFilter/AutoRebuild 往返丟失")
	}
}

// MergeBotConfigFileInto 必須保留 BotConfigFile 無法承載的字段（與 BotConfig→File→BotConfig 往返 allowlist 對齊），
// 其餘字段以 BotConfigFile 為準。
func TestMergeBotConfigFileInto_PreservesUncarriedFields(t *testing.T) {
	existing := filledBotConfig()
	existing.CreatedAt = "2026-01-01T00:00:00Z"
	incoming := ConvertFromBotConfig(filledBotConfig())
	incoming.Name = "renamed"
	incoming.Grid.PriceInterval = 999
	incoming.CreatedAt = "" // 請求未帶 created_at
	incoming.BotID = ""

	merged := MergeBotConfigFileInto(existing, incoming)
	for name := range botFileRoundTripAllow {
		if !reflect.DeepEqual(reflect.ValueOf(merged).FieldByName(name).Interface(),
			reflect.ValueOf(existing).FieldByName(name).Interface()) {
			t.Errorf("BotConfigFile 不承載的字段 %s 在合併後被改寫", name)
		}
	}
	if merged.CreatedAt != existing.CreatedAt || merged.ID != existing.ID {
		t.Fatalf("空 CreatedAt/ID 應沿用原值: %q %q", merged.CreatedAt, merged.ID)
	}
	if merged.Name != "renamed" || merged.PriceInterval != 999 {
		t.Fatalf("更新字段未生效: %q %v", merged.Name, merged.PriceInterval)
	}

	// 請求顯式帶 CreatedAt 時以請求為準；existing.Enabled 為 nil 時保持 nil
	incoming.CreatedAt = "2026-09-17T00:00:00Z"
	merged = MergeBotConfigFileInto(BotConfig{ID: "x"}, incoming)
	if merged.CreatedAt != incoming.CreatedAt || merged.Enabled != nil {
		t.Fatalf("顯式 CreatedAt 或 nil Enabled 處理錯誤: %q %v", merged.CreatedAt, merged.Enabled)
	}
}

func TestSymbolBotSymbol_RoundTripSpotMargin(t *testing.T) {
	sc := filledSymbolConfig()
	back := BotConfigToSymbolConfig(SymbolConfigToBotConfig(sc, false))
	var diffs []string
	diffFields(reflect.ValueOf(sc), reflect.ValueOf(back), "", &diffs)
	assertNoUnexpectedDiffs(t, "SymbolConfig→BotConfig→SymbolConfig", diffs, map[string]string{})
	if back.GetMarketType() != "spot_margin" {
		t.Fatalf("spot_margin 往返後變為 %q", back.GetMarketType())
	}
}

func TestBotConfigFile_RoundTripFromBotConfig(t *testing.T) {
	bc := filledBotConfig()
	back := ConvertToBotConfig(ConvertFromBotConfig(bc))
	var diffs []string
	diffFields(reflect.ValueOf(bc), reflect.ValueOf(back), "", &diffs)
	assertNoUnexpectedDiffs(t, "BotConfig→BotConfigFile→BotConfig", diffs, botFileRoundTripAllow)
}

// botFileRoundTripAllow 是 BotConfigFile 無法承載的 BotConfig 字段；更新主配置時由 MergeBotConfigFileInto 保留。
var botFileRoundTripAllow = map[string]string{
	// BotConfigFile 不含 enabled：啟停狀態以 DB bots 表為準（BotManager.isBotEnabledInDB），不隨配置文檔存儲
	"Enabled": "啟停狀態由 DB bots 表維護",
}

func TestBotConfigFile_RoundTripThroughBotConfig(t *testing.T) {
	var bf BotConfigFile
	fillNonZero(reflect.ValueOf(&bf).Elem(), "file", 0)
	back := ConvertFromBotConfig(ConvertToBotConfig(&bf))
	var diffs []string
	diffFields(reflect.ValueOf(bf), reflect.ValueOf(*back), "", &diffs)
	// 以下均為 BotConfigFile 獨有、BotConfig 無對應字段的項；BotConfig 路徑本來就無法承載，
	// 持久化時直接保存 BotConfigFile 本身（web/api_bot_config.go），不經 BotConfig 往返。
	allow := map[string]string{
		"UpdatedAt":                    "由保存路徑在寫入時設置",
		"StrategyMode":                 "由 Strategies 數量推導（single/multi）",
		"Strategies":                   "BotStrategyConfig.Enabled/Settings 在 StrategyInstance 中無對應，轉回時 Enabled=true、Settings 為空",
		"HybridStrategy":               "BotConfig 無混合策略字段",
		"Capital.PerStrategy":          "BotConfig 無對應字段",
		"RiskControl.OptionHedge":      "BotConfig 無對應字段",
		"RiskControl.MaxDrawdownRatio": "BotConfig 無對應字段",
		"RiskControl.StopLossRatio":    "BotConfig 無對應字段",
		"RiskControl.TakeProfitRatio":  "BotConfig 無對應字段",
		"Hedge":                        "對沖組信息僅存於 Bot 配置文件",
	}
	assertNoUnexpectedDiffs(t, "BotConfigFile→BotConfig→BotConfigFile", diffs, allow)
}

// 同名字段在 BotConfig 與 BotConfigFile 上的 yaml/json tag 須一致，保證文檔互通
func TestBotConfigFile_SharedFieldTagsMatchBotConfig(t *testing.T) {
	allow := map[string]string{
		// BotConfigFile.created_at 歷史上無 omitempty，保持向後兼容不改
		"CreatedAt": "歷史 tag 差異（omitempty），不影響讀寫",
	}
	bt := reflect.TypeOf(BotConfig{})
	ft := reflect.TypeOf(BotConfigFile{})
	for i := 0; i < ft.NumField(); i++ {
		ff := ft.Field(i)
		bf, ok := bt.FieldByName(ff.Name)
		if !ok {
			continue
		}
		if _, skip := allow[ff.Name]; skip {
			continue
		}
		if ff.Tag.Get("yaml") != bf.Tag.Get("yaml") || ff.Tag.Get("json") != bf.Tag.Get("json") {
			t.Errorf("%s tag 不一致: file=%q bot=%q", ff.Name, ff.Tag, bf.Tag)
		}
	}
}

func TestBotConfigFile_NewFieldsSerialize(t *testing.T) {
	bc := BotConfig{
		ID:                  "b1",
		MarketType:          "spot",
		UseSpotMargin:       true,
		SpotInventoryPolicy: SpotInventoryPolicyAdoptAll,
		FundingPerpSpread:   &FundingPerpSpreadConfig{MinFundingSpread: 0.0002, MaxBasisPct: 0.5},
	}
	bf := ConvertFromBotConfig(bc)
	for _, codec := range []struct {
		name      string
		marshal   func(interface{}) ([]byte, error)
		unmarshal func([]byte, interface{}) error
	}{
		{"json", json.Marshal, json.Unmarshal},
		{"yaml", yaml.Marshal, yaml.Unmarshal},
	} {
		data, err := codec.marshal(bf)
		if err != nil {
			t.Fatalf("%s marshal: %v", codec.name, err)
		}
		var decoded BotConfigFile
		if err := codec.unmarshal(data, &decoded); err != nil {
			t.Fatalf("%s unmarshal: %v", codec.name, err)
		}
		got := ConvertToBotConfig(&decoded)
		if !got.UseSpotMargin || got.SpotInventoryPolicy != SpotInventoryPolicyAdoptAll ||
			!reflect.DeepEqual(got.FundingPerpSpread, bc.FundingPerpSpread) || got.GetMarketType() != "spot_margin" {
			t.Fatalf("%s 往返丟字段: %s", codec.name, fmt.Sprintf("%+v", got))
		}
	}
	// omitempty：零值時不輸出新字段，保持舊文檔格式
	empty, err := json.Marshal(ConvertFromBotConfig(BotConfig{ID: "x"}))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(empty, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"spot_inventory_policy", "funding_perp_spread", "use_spot_margin"} {
		if _, ok := m[k]; ok {
			t.Errorf("零值字段 %s 不應輸出", k)
		}
	}
}
