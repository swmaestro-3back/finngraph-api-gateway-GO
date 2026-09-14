package middleware

import (
	"context"
	"net"
	"net/http"
	"strings"
)

type clientIPKey struct{}

func RealIP(trustProxy bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := remoteHost(r)
			if trustProxy {
				if v := viewerHost(r.Header.Get("CloudFront-Viewer-Address")); v != "" {
					ip = v
				} else if v := r.Header.Get("X-Real-IP"); v != "" {
					ip = v
				}
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientIPKey{}, ip)))
		})
	}
}

func ClientIP(r *http.Request) string {
	if v, ok := r.Context().Value(clientIPKey{}).(string); ok {
		return v
	}
	return remoteHost(r)
}

func viewerHost(v string) string {
	if v == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(v); err == nil && net.ParseIP(host) != nil {
		return host
	}
	if i := strings.LastIndex(v, ":"); i > 0 {
		if host := v[:i]; net.ParseIP(host) != nil {
			return host
		}
	}
	return ""
}

func remoteHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
