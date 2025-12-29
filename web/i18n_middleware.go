package web

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	qmi18n "quantmesh/i18n"
)

// I18nMiddleware 解析请求的 Accept-Language 头并设置到上下文
func I18nMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 从 Accept-Language 头获取语言
		acceptLang := c.GetHeader("Accept-Language")
		lang := parseAcceptLanguage(acceptLang)
		
		// 存储到上下文
		c.Set("language", lang)
		c.Set("localizer", qmi18n.GetLocalizer(lang))
		
		c.Next()
	}
}

// parseAcceptLanguage 解析 Accept-Language 头
// 示例: "zh-CN,zh;q=0.9,en;q=0.8" -> "zh-CN"
func parseAcceptLanguage(acceptLang string) string {
	if acceptLang == "" {
		return "zh-CN" // 默认中文
	}

	// 分割多个语言选项
	langs := strings.Split(acceptLang, ",")
	if len(langs) == 0 {
		return "zh-CN"
	}

	// 取第一个语言（优先级最高）
	firstLang := strings.TrimSpace(langs[0])
	
	// 去除权重参数 (;q=0.9)
	if idx := strings.Index(firstLang, ";"); idx != -1 {
		firstLang = firstLang[:idx]
	}
	
	firstLang = strings.TrimSpace(firstLang)
	
	// 标准化语言代码
	firstLang = normalizeLanguage(firstLang)
	
	return firstLang
}

// normalizeLanguage 标准化语言代码
func normalizeLanguage(lang string) string {
	lang = strings.ToLower(lang)
	
	// 映射常见的语言代码
	switch {
	case strings.HasPrefix(lang, "zh-tw"), strings.HasPrefix(lang, "zh_tw"), strings.HasPrefix(lang, "zh-hant"):
		return "zh-TW"
	case strings.HasPrefix(lang, "zh-cn"), strings.HasPrefix(lang, "zh_cn"), strings.HasPrefix(lang, "zh-hans"), lang == "zh":
		return "zh-CN"
	case strings.HasPrefix(lang, "en"), strings.HasPrefix(lang, "en-us"), strings.HasPrefix(lang, "en_us"):
		return "en-US"
	case strings.HasPrefix(lang, "fr"):
		return "fr-FR"
	case strings.HasPrefix(lang, "es"):
		return "es-ES"
	case strings.HasPrefix(lang, "ru"):
		return "ru-RU"
	case strings.HasPrefix(lang, "hi"):
		return "hi-IN"
	case strings.HasPrefix(lang, "pt"):
		return "pt-BR"
	case strings.HasPrefix(lang, "de"):
		return "de-DE"
	case strings.HasPrefix(lang, "ko"):
		return "ko-KR"
	case strings.HasPrefix(lang, "ar"):
		return "ar-SA"
	case strings.HasPrefix(lang, "tr"):
		return "tr-TR"
	case strings.HasPrefix(lang, "vi"):
		return "vi-VN"
	case strings.HasPrefix(lang, "it"):
		return "it-IT"
	case strings.HasPrefix(lang, "id"):
		return "id-ID"
	case strings.HasPrefix(lang, "nl"):
		return "nl-NL"
	default:
		return "zh-CN" // 默认中文
	}
}

// GetLocalizer 从上下文获取 Localizer
func GetLocalizer(c *gin.Context) *i18n.Localizer {
	if localizer, exists := c.Get("localizer"); exists {
		if l, ok := localizer.(*i18n.Localizer); ok {
			return l
		}
	}
	// 回退到默认语言
	return qmi18n.GetLocalizer("zh-CN")
}

// GetLanguage 从上下文获取语言
func GetLanguage(c *gin.Context) string {
	if lang, exists := c.Get("language"); exists {
		if l, ok := lang.(string); ok {
			return l
		}
	}
	return "zh-CN"
}

// T 翻译消息（从上下文获取语言）
func T(c *gin.Context, key string, data ...interface{}) string {
	lang := GetLanguage(c)
	return qmi18n.TWithLang(lang, key, data...)
}

