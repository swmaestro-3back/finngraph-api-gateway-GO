package middleware

import (
	"net/http"
	"strings"

	"github.com/swmaestro-3back/finngraph-api-gateway-GO/internal/gwerr"
)

const (
	corsAllowMethods  = "GET, POST, PUT, DELETE, PATCH"
	corsAllowHeaders  = "Authorization, Content-Type"
	corsMaxAge        = "600"
	corsVaryActual    = "Origin"
	corsVaryPreflight = "Origin, Access-Control-Request-Method, Access-Control-Request-Headers"
)

func CORS(origins []string) func(http.Handler) http.Handler {
	if len(origins) == 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	allowed := make(map[string]struct{}, len(origins))
	for _, o := range origins {
		allowed[o] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}
			_, ok := allowed[strings.ToLower(origin)]
			preflight := r.Method == http.MethodOptions &&
				r.Header.Get("Access-Control-Request-Method") != ""

			if preflight {
				w.Header().Set("Vary", corsVaryPreflight)
				if ok {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Access-Control-Allow-Credentials", "true")
					w.Header().Set("Access-Control-Allow-Methods", corsAllowMethods)
					w.Header().Set("Access-Control-Allow-Headers", corsAllowHeaders)
					w.Header().Set("Access-Control-Max-Age", corsMaxAge)
				} else {
					gwerr.SetTag(r, "cors_denied")
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}

			w.Header().Set("Vary", corsVaryActual)
			if ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			} else {
				gwerr.SetTag(r, "cors_denied")
			}
			next.ServeHTTP(w, r)
		})
	}
}
