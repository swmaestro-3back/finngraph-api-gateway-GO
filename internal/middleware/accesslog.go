package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/swmaestro-3back/finngraph-api-gateway-GO/internal/gwerr"
)

type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (sw *statusWriter) WriteHeader(status int) {
	if !sw.wrote {
		sw.status = status
		sw.wrote = true
	}
	sw.ResponseWriter.WriteHeader(status)
}

func (sw *statusWriter) Write(b []byte) (int, error) {
	if !sw.wrote {
		sw.status = http.StatusOK
		sw.wrote = true
	}
	return sw.ResponseWriter.Write(b)
}

func (sw *statusWriter) Unwrap() http.ResponseWriter {
	return sw.ResponseWriter
}

func AccessLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r = gwerr.WithTag(r)
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()

			next.ServeHTTP(sw, r)

			logger.Info("access",
				"request_id", GetRequestID(r),
				"client_ip", ClientIP(r),
				"method", r.Method,
				"path", r.URL.EscapedPath(),
				"status", sw.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"err", gwerr.Tag(r),
			)
		})
	}
}
