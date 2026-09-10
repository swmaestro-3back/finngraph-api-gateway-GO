package middleware

import (
	"net/http"

	"github.com/swmaestro-3back/finngraph-api-gateway-GO/internal/gwerr"
)

func BodyLimit(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > limit {
				gwerr.SetTag(r, "blocked_path")
				gwerr.Write(w, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE",
					"요청 본문이 너무 큽니다.")
				return
			}
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, limit)
			}
			next.ServeHTTP(w, r)
		})
	}
}
