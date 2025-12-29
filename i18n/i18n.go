package i18n

import (
	"embed"
	"fmt"
	"sync"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
	"gopkg.in/yaml.v3"
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

	// 设置默认语言
	if lang == "" {
		lang = defaultLang
	}
	systemLanguage = lang

	// 创建 bundle
	bundle = i18n.NewBundle(language.Chinese)
	bundle.RegisterUnmarshalFunc("toml", yaml.Unmarshal)

	// 加载翻译文件
	supportedLangs := []string{"zh-CN", "en-US"}
	for _, l := range supportedLangs {
		filename := fmt.Sprintf("locales/%s.toml", l)
		if _, err := bundle.LoadMessageFileFS(localeFS, filename); err != nil {
			// 如果加载失败，记录但继续（至少保证默认语言可用）
			fmt.Printf("[WARN] Failed to load translation file %s: %v\n", filename, err)
		}
	}

	return nil
}

// GetLocalizer 获取指定语言的 Localizer
func GetLocalizer(lang string) *i18n.Localizer {
	mu.RLock()
	defer mu.RUnlock()

	if bundle == nil {
		// 如果未初始化，返回 nil（调用者应处理）
		return nil
	}

	if lang == "" {
		lang = systemLanguage
	}

	return i18n.NewLocalizer(bundle, lang)
}

// T 翻译消息（使用系统默认语言）
func T(key string, data ...interface{}) string {
	mu.RLock()
	lang := systemLanguage
	mu.RUnlock()

	return TWithLang(lang, key, data...)
}

// TWithLang 翻译消息（指定语言）
func TWithLang(lang string, key string, data ...interface{}) string {
	localizer := GetLocalizer(lang)
	if localizer == nil {
		// 未初始化，返回 key
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
		// 翻译失败，返回 key（向后兼容）
		return key
	}

	return msg
}

// SetSystemLanguage 设置系统默认语言
func SetSystemLanguage(lang string) {
	mu.Lock()
	defer mu.Unlock()
	systemLanguage = lang
}

// GetSystemLanguage 获取系统默认语言
func GetSystemLanguage() string {
	mu.RLock()
	defer mu.RUnlock()
	return systemLanguage
}

