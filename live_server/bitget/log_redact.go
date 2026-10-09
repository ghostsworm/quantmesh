package main

import (
	"io"
	"log"
	"regexp"
)

type redactingLogWriter struct{ destination io.Writer }

var completeCookieHeaderPattern = regexp.MustCompile(`(?i)\b(?:set-cookie|cookie)["']?\s*[:=]\s*[^\r\n]+`)
var proxyAuthorizationPattern = regexp.MustCompile(`(?i)(proxy[_-]?authorization\s*[:=]\s*)[^\r\n]+`)
var sensitiveTextPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)([?&](?:signature|sign|x-mbx[-_]?signature|api[_-]?key|x-mbx[-_]?api[-_]?key|api[_-]?secret|access[_-]?key|client[_-]?(?:key|secret)|secret[_-]?key|access[_-]?token|refresh[_-]?token|token|password|passphrase|listen[_-]?key)=)[^&#\s"'<>]+`),
	regexp.MustCompile(`(?i)(["']?(?:api[_-]?key|x-mbx[-_]?api[-_]?key|api[_-]?secret|access[_-]?key|client[_-]?(?:key|secret)|secret[_-]?key|secret|signature|x-mbx[-_]?signature|access[_-]?token|refresh[_-]?token|token|password|passphrase|private[_-]?key|authorization|listen[_-]?key|webhook(?:[_-]?url)?|cookie|dsn)["']?\s*[:=]\s*)("[^"]*"|'[^']*'|(?:Bearer|Basic)\s+[^\s,;}&]+|[^\s,;}&]+)`),
	regexp.MustCompile(`(?i)(\bBearer\s+)[A-Za-z0-9._~+/-]+=*`),
	regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^/\s@]+@`),
	regexp.MustCompile(`-----BEGIN [^-]*PRIVATE KEY-----.*?-----END [^-]*PRIVATE KEY-----`),
}

func sanitizeSensitiveText(message []byte) []byte {
	text := completeCookieHeaderPattern.ReplaceAllString(string(message), "[REDACTED]")
	text = proxyAuthorizationPattern.ReplaceAllString(text, "$1[REDACTED]")
	for index, pattern := range sensitiveTextPatterns {
		switch index {
		case 0, 1, 2:
			text = pattern.ReplaceAllString(text, `${1}[REDACTED]`)
		case 3:
			text = pattern.ReplaceAllString(text, `${1}[REDACTED]@`)
		default:
			text = pattern.ReplaceAllString(text, `[REDACTED PRIVATE KEY]`)
		}
	}
	return []byte(text)
}

func (writer redactingLogWriter) Write(message []byte) (int, error) {
	sanitized := sanitizeSensitiveText(message)
	written, err := writer.destination.Write(sanitized)
	if err != nil {
		return 0, err
	}
	if written != len(sanitized) {
		return 0, io.ErrShortWrite
	}
	return len(message), nil
}

func installStandardLogRedaction() {
	if _, installed := log.Writer().(redactingLogWriter); installed {
		return
	}
	log.SetOutput(redactingLogWriter{destination: log.Writer()})
}
