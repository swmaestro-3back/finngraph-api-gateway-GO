package middleware

import (
	"context"
	"net"
	"net/http"
)

type clientIPKey struct{}

func RealIP(trustProxy bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := remoteHost(r)
			if trustProxy {
				if v := r.Header.Get("X-Real-IP"); v != "" {
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

func remoteHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
