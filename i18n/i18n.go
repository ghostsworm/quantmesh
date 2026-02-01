package i18n

import (
	"embed"
	"fmt"
	"sync"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/pelletier/go-toml/v2"
	"golang.org/x/text/language"
)

//go:embed locales/*.toml
var localeFS embed.FS

var (
	bundle         *i18n.Bundle
	defaultLang    = "zh-CN"
	mu             sync.RWMutex
	systemLanguage string
)

// Init 初始化 i18n 系统
func Init(lang string) error {
	mu.Lock()
	defer mu.Unlock()

	// 設置默认语言
	if lang == "" {
		lang = defaultLang
	}
	systemLanguage = lang

	// 創建 bundle
	bundle = i18n.NewBundle(language.Chinese)
	bundle.RegisterUnmarshalFunc("toml", toml.Unmarshal)

	// 加載翻譯文件（zh-TW 用於日誌輸出繁體中文）
	supportedLangs := []string{"zh-CN", "zh-TW", "en-US"}
	for _, l := range supportedLangs {
		filename := fmt.Sprintf("locales/%s.toml", l)
		if _, err := bundle.LoadMessageFileFS(localeFS, filename); err != nil {
			// 如果加載失败，記錄但继续（至少保证默认语言可用）
			fmt.Printf("[WARN] Failed to load translation file %s: %v\n", filename, err)
		}
	}

	return nil
}

// GetLocalizer 獲取指定语言的 Localizer
func GetLocalizer(lang string) *i18n.Localizer {
	mu.RLock()
	defer mu.RUnlock()

	if bundle == nil {
		// 如果未初始化，回傳 nil（調用者应处理）
		return nil
	}

	if lang == "" {
		lang = systemLanguage
	}

	return i18n.NewLocalizer(bundle, lang)
}

// T 翻譯消息（使用系统默认语言）
func T(key string, data ...interface{}) string {
	mu.RLock()
	lang := systemLanguage
	mu.RUnlock()

	return TWithLang(lang, key, data...)
}

// TWithLang 翻譯消息（指定语言）
func TWithLang(lang string, key string, data ...interface{}) string {
	localizer := GetLocalizer(lang)
	if localizer == nil {
		// 未初始化，回傳 key
		return key
	}

	var templateData map[string]interface{}
	if len(data) > 0 {
		if m, ok := data[0].(map[string]interface{}); ok {
			templateData = m
		}
	}

	msg, err := localizer.Localize(&i18n.LocalizeConfig{
		MessageID:    key,
		TemplateData: templateData,
	})

	if err != nil {
		// 翻譯失败，回傳 key（向后兼容）
		return key
	}

	return msg
}

// SetSystemLanguage 設置系统默认语言
func SetSystemLanguage(lang string) {
	mu.Lock()
	defer mu.Unlock()
	systemLanguage = lang
}

// GetSystemLanguage 獲取系统默认语言
func GetSystemLanguage() string {
	mu.RLock()
	defer mu.RUnlock()
	return systemLanguage
}
