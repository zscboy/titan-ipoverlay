package handler

import (
	"net"
	"net/http"
	"strings"
)

func getClientIP(r *http.Request) string {
	remoteIP, _, _ := net.SplitHostPort(r.RemoteAddr)
	ip := net.ParseIP(remoteIP)
	if ip == nil {
		return remoteIP
	}

	if ip.IsLoopback() || ip.IsPrivate() {
		ip := r.Header.Get("X-Forwarded-For")
		if ip != "" {
			ips := strings.Split(ip, ",")
			return strings.TrimSpace(ips[0])
		}

		ip = r.Header.Get("X-Real-IP")
		if ip != "" {
			return ip
		}

	}

	return remoteIP
}
