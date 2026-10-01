package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// sameOriginWebSocketRequest applies a scheme, host, and effective-port check
// to browser WebSocket handshakes. Requests without Origin are allowed for
// non-browser clients; authenticated routes still enforce their credentials.
func sameOriginWebSocketRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	originHeader := strings.TrimSpace(r.Header.Get("Origin"))
	if originHeader == "" {
		return true
	}
	origin, err := url.Parse(originHeader)
	if err != nil || origin.User != nil || origin.Host == "" || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
		return false
	}
	requestScheme := "http"
	if r.TLS != nil {
		requestScheme = "https"
	} else if scheme := strings.ToLower(strings.TrimSpace(r.URL.Scheme)); scheme == "http" || scheme == "https" {
		requestScheme = scheme
	}
	if forwardedProto := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0])); forwardedProto == "http" || forwardedProto == "https" {
		requestScheme = forwardedProto
	}
	if !strings.EqualFold(origin.Scheme, requestScheme) {
		return false
	}
	originHost, err := normalizedOriginHost(origin.Host, requestScheme)
	if err != nil {
		return false
	}
	requestHost, err := normalizedOriginHost(r.Host, requestScheme)
	return err == nil && strings.EqualFold(originHost, requestHost)
}

func normalizedOriginHost(rawHost, scheme string) (string, error) {
	parsed, err := url.Parse("//" + rawHost)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Path != "" {
		return "", fmt.Errorf("invalid WebSocket origin host")
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return "", fmt.Errorf("invalid WebSocket origin host")
	}
	port := parsed.Port()
	if port == "" {
		if scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return host + ":" + port, nil
}
