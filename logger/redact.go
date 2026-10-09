package logger

import (
	"io"
	"log"
	"regexp"
	"sync"
)

type standardLogRedactingWriter struct {
	destination io.Writer
}

func (writer standardLogRedactingWriter) Write(message []byte) (int, error) {
	sanitized := []byte(redactSensitiveText(string(message)))
	written, err := writer.destination.Write(sanitized)
	if err != nil {
		return 0, err
	}
	if written != len(sanitized) {
		return 0, io.ErrShortWrite
	}
	return len(message), nil
}

var standardLogRedactionInstallMu sync.Mutex

// InstallStandardLogRedaction sanitizes messages written through the process-wide
// standard logger. Call it once during application startup, before logging secrets.
func InstallStandardLogRedaction() {
	standardLogRedactionInstallMu.Lock()
	defer standardLogRedactionInstallMu.Unlock()

	writer := log.Writer()
	if _, alreadyInstalled := writer.(standardLogRedactingWriter); alreadyInstalled {
		return
	}
	log.SetOutput(standardLogRedactingWriter{destination: writer})
}

var completeCookieHeaderPattern = regexp.MustCompile("(?i)\\b(?:set-cookie|cookie)[\"']?\\s*[:=]\\s*[^\\r\\n]+")
var proxyAuthorizationPattern = regexp.MustCompile("(?i)(proxy[_-]?authorization\\s*[:=]\\s*)[^\\r\\n]+")

var sensitiveTextPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)([?&](?:signature|sign|x-mbx[-_]?signature|api[_-]?key|x-mbx[-_]?api[_-]?key|api[_-]?secret|access[_-]?key|client[_-]?(?:key|secret)|secret[_-]?key|access[_-]?token|refresh[_-]?token|token|password|passphrase|listen[_-]?key)=)[^&#\s"'<>]+`),
	regexp.MustCompile(`(?i)(["']?(?:api[_-]?key|x-mbx[-_]?api[_-]?key|api[_-]?secret|access[_-]?key|client[_-]?(?:key|secret)|secret[_-]?key|secret|signature|x-mbx[-_]?signature|access[_-]?token|refresh[_-]?token|token|password|passphrase|private[_-]?key|authorization|listen[_-]?key|webhook(?:[_-]?url)?|cookie|dsn)["']?\s*[:=]\s*)("[^"]*"|'[^']*'|(?:Bearer|Basic)\s+[^\s,;}&]+|[^\s,;}&]+)`),
	regexp.MustCompile(`(?i)(\bBearer\s+)[A-Za-z0-9._~+/-]+=*`),
	regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^/\s@]+@`),
	regexp.MustCompile(`-----BEGIN [^-]*PRIVATE KEY-----.*?-----END [^-]*PRIVATE KEY-----`),
}

// redactSensitiveText removes common credential forms before a message reaches
// console, file, database, or error-hook log sinks.
func redactSensitiveText(message string) string {
	message = completeCookieHeaderPattern.ReplaceAllString(message, "[REDACTED]")
	message = proxyAuthorizationPattern.ReplaceAllString(message, "$1[REDACTED]")
	for index, pattern := range sensitiveTextPatterns {
		switch index {
		case 0, 1, 2:
			message = pattern.ReplaceAllString(message, `${1}[REDACTED]`)
		case 3:
			message = pattern.ReplaceAllString(message, `${1}[REDACTED]@`)
		default:
			message = pattern.ReplaceAllString(message, `[REDACTED PRIVATE KEY]`)
		}
	}
	return message
}

// SanitizeSensitiveText applies credential redaction before external log sinks.
func SanitizeSensitiveText(message string) string {
	return redactSensitiveText(message)
}
