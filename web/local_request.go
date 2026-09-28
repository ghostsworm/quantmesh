package web

import (
	"net"
	"net/http"
	"strings"
)

// Authentication bypass must use the TCP peer, never a client-controlled
// forwarded address. Proxied requests require normal authentication even when
// the reverse proxy itself runs on localhost.
func isDirectLoopbackRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return false
	}
	for _, header := range []string{"Forwarded", "X-Forwarded-For", "X-Real-IP", "X-Forwarded-Host", "X-Forwarded-Proto"} {
		if len(r.Header.Values(header)) > 0 {
			return false
		}
	}
	return true
}

func isLoopbackBindHost(host string) bool {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
