package middleware

import (
	"context"
	"net"
	"net/http"
	"strings"
)

type clientIPKey struct{}

func RealIP(trustProxy bool, source string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := resolveClientIP(r, trustProxy, source)
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientIPKey{}, ip)))
		})
	}
}

func resolveClientIP(r *http.Request, trustProxy bool, source string) string {
	if !trustProxy {
		return remoteHost(r)
	}
	switch source {
	case "cloudfront":
		if v := viewerHost(r.Header.Get("CloudFront-Viewer-Address")); v != "" {
			return v
		}
	case "real-ip":
		if v := r.Header.Get("X-Real-IP"); v != "" {
			return v
		}
	case "xff":
		if v := xffLast(joinedXFF(r)); v != "" {
			return v
		}
	case "socket":
	default:
		if v := viewerHost(r.Header.Get("CloudFront-Viewer-Address")); v != "" {
			return v
		}
		if v := r.Header.Get("X-Real-IP"); v != "" {
			return v
		}
		if v := xffLast(joinedXFF(r)); v != "" {
			return v
		}
	}
	return remoteHost(r)
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

func joinedXFF(r *http.Request) string {
	return strings.Join(r.Header.Values("X-Forwarded-For"), ",")
}

func xffLast(v string) string {
	if v == "" {
		return ""
	}
	parts := strings.Split(v, ",")
	last := strings.TrimSpace(parts[len(parts)-1])
	if last == "" {
		return ""
	}
	if net.ParseIP(last) != nil {
		return last
	}
	return viewerHost(last)
}

func remoteHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
